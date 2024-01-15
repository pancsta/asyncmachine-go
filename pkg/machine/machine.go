// Package machine is a minimal implementation of AsyncMachine [1]  in Golang
// using channels and context. It aims at simplicity and speed.
//
// It can be used as a lightweight in-memory Temporal [2] alternative, worker
// for Asynq [3], or to write simple consensus engines, stateful firewalls,
// telemetry, bots, etc.
//
// AsyncMachine is a relational state machine which never blocks.
//
// [1]: https://github.com/TobiaszCudnik/asyncmachine
// [2]: https://github.com/temporalio/temporal
// [3]: https://github.com/hibiken/asynq
package machine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

// Machine represent states, provides mutation methods, helpers methods and
// info about the current and scheduled transitions (if any).
// See https://github.com/pancsta/asyncmachine-go/blob/main/docs/manual.md
type Machine struct {
	// Unique ID of this machine. Default: random UUID.
	ID string
	// Time for a handler to execute. Default: time.Second
	HandlerTimeout time.Duration
	// If true, the machine will print all exceptions to stdout. Default: true.
	// Requires an ExceptionHandler binding and Machine.PanicToException set.
	PrintExceptions bool
	// If true, the machine will catch panic and trigger the Exception state.
	// Default: true.
	PanicToException bool
	// If true, the machine will prefix its logs with the machine ID (5 chars).
	// Default: true.
	LogID bool
	// Relations resolver, used to produce target states of a transition.
	// Default: *DefaultRelationsResolver.
	Resolver RelationsResolver
	// Logging level of the machine.
	LogLevel LogLevel
	// Ctx is the context of the machine.
	Ctx context.Context
	// Err is the last error that occurred.
	Err error
	// States is a map of state names to state definitions.
	States States
	// Queue of mutations to be executed.
	Queue []Mutation
	// Currently executing transition (if any).
	Transition *Transition
	// All states that are currently active.
	ActiveStates S
	// List of all the registered state names.
	StateNames S

	emitters []*emitter
	clock    map[string]uint64
	cancel   context.CancelFunc
	logLevel LogLevel
	logger   Logger
	// Queue mutex.
	queueLock        sync.Mutex
	activeStatesLock sync.Mutex
	recoveryCh       chan bool
	disposed         bool
	indexWhen        indexWhen
	indexStateCtx    indexStateCtx
}

// New creates a new Machine instance, bound to context and modified with
// optional Opts
func New(ctx context.Context, states States, opts *Opts) *Machine {
	m := &Machine{
		ID:               uuid.New().String(),
		HandlerTimeout:   time.Second,
		States:           states,
		clock:            map[string]uint64{},
		emitters:         []*emitter{},
		PrintExceptions:  true,
		PanicToException: true,
		LogID:            true,
		recoveryCh:       make(chan bool, 1),
		indexWhen:        indexWhen{},
		indexStateCtx:    indexStateCtx{},
	}
	if opts != nil {
		if opts.ID != "" {
			m.ID = opts.ID
		}
		if opts.HandlerTimeout != 0 {
			m.HandlerTimeout = opts.HandlerTimeout
		}
		if opts.DontPanicToException {
			m.PanicToException = false
		}
		if opts.DontPrintExceptions {
			m.PrintExceptions = false
		}
		if opts.DontLogID {
			m.LogID = false
		}
		if opts.Resolver != nil {
			m.Resolver = opts.Resolver
		}
		if opts.LogLevel != LogNothing {
			m.SetLogLevel(opts.LogLevel)
		}
	}
	// default resolver
	if m.Resolver == nil {
		m.Resolver = &DefaultRelationsResolver{
			Machine: m,
		}
	}
	// define the exception state (if missing)
	if _, ok := m.States["Exception"]; !ok {
		m.States["Exception"] = State{
			Multi: true,
		}
	}
	for name := range m.States {
		m.StateNames = append(m.StateNames, name)
		m.clock[name] = 0
	}
	// init context
	m.Ctx, m.cancel = context.WithCancel(ctx)
	go func() {
		<-m.Ctx.Done()
		if m.Err == nil {
			m.Err = m.Ctx.Err()
		}
		m.Dispose()
	}()
	return m
}

// Dispose disposes the machine and all its emitters.
func (m *Machine) Dispose() {
	if m.disposed {
		return
	}
	m.disposed = true
	m.log(LogEverything, "[end] dispose")
	m.cancel()
	for _, e := range m.emitters {
		m.disposeEmitter(e)
	}
	m.logger = nil
	// state contexts get cancelled automatically
	m.indexStateCtx = nil
	// channels need to be closed manually
	for s := range m.indexWhen {
		for k := range m.indexWhen[s] {
			closeSafe(m.indexWhen[s][k].ch)
		}
		m.indexWhen[s] = nil
	}
	m.indexWhen = nil
}

// disposeEmitter detaches the emitter from the machine and disposes it.
func (m *Machine) disposeEmitter(emitter *emitter) {
	m.log(LogEverything, "[end] emitter %s", emitter.ID)
	m.emitters = lo.Without(m.emitters, emitter)
	emitter.dispose()
}

// WhenErr returns a channel that will be closed when the machine is in the
// Exception state.
// ctx: optional context defaults to the machine's context.
func (m *Machine) WhenErr(ctx context.Context) chan struct{} {
	// handle with a shared channel with broadcast via close
	return m.When([]string{"Exception"}, ctx)
}

// GetRelationsBetween returns a list of relation types between the given
// states.
func (m *Machine) GetRelationsBetween(fromState, toState string) []Relation {
	m.MustParseStates(S{fromState})
	m.MustParseStates(S{toState})
	state := m.States[fromState]
	var relations []Relation
	if state.Add != nil && lo.Contains(state.Add, toState) {
		relations = append(relations, RelationAdd)
	}
	if state.Require != nil && lo.Contains(state.Require, toState) {
		relations = append(relations, RelationRequire)
	}
	if state.Remove != nil && lo.Contains(state.Remove, toState) {
		relations = append(relations, RelationRemove)
	}
	if state.After != nil && lo.Contains(state.After, toState) {
		relations = append(relations, RelationAfter)
	}
	return relations
}

// GetRelationsOf returns a list of relation types of the given state.
func (m *Machine) GetRelationsOf(fromState string) []Relation {
	m.MustParseStates(S{fromState})
	state := m.States[fromState]
	var relations []Relation
	if state.Add != nil {
		relations = append(relations, RelationAdd)
	}
	if state.Require != nil {
		relations = append(relations, RelationRequire)
	}
	if state.Remove != nil {
		relations = append(relations, RelationRemove)
	}
	if state.After != nil {
		relations = append(relations, RelationAfter)
	}
	return relations
}

// When returns a channel that will be closed when all the passed states
// become active or the machine gets disposed.
// ctx: optional context that will close the channel when done. Useful when
// listening on 2 When() channels within the same `select` to GC the 2nd one.
func (m *Machine) When(states []string, ctx context.Context) chan struct{} {
	ch := make(chan struct{})
	if ctx == nil {
		ctx = m.Ctx
	}

	// if all active, close early
	if m.Is(states) {
		close(ch)
		return ch
	}

	m.activeStatesLock.Lock()
	setMap := stateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = m.Is(S{s})
		if setMap[s] {
			matched++
		}
	}
	// add the binding to an index of each state
	binding := &whenBinding{
		ch:       ch,
		negation: false,
		states:   setMap,
		total:    len(states),
		matched:  matched,
	}
	for _, s := range states {
		if _, ok := m.indexWhen[s]; !ok {
			m.indexWhen[s] = []*whenBinding{binding}
		} else {
			m.indexWhen[s] = append(m.indexWhen[s], binding)
		}
	}
	go func() {
		// dispose the binding on ctx.Done() and m.Ctx.Done()
		select {
		case <-ctx.Done():
		case <-m.Ctx.Done():
			m.activeStatesLock.Lock()
			for _, s := range states {
				if _, ok := m.indexWhen[s]; ok {
					m.indexWhen[s] = lo.Without(m.indexWhen[s], binding)
				}
			}
			m.activeStatesLock.Unlock()
		}
	}()
	m.activeStatesLock.Unlock()

	return ch
}

// WhenNot returns a channel that will be closed when all the passed states
// become inactive or the machine gets disposed.
// ctx: optional context that will close the channel when done. Useful when
// listening on 2 WhenNot() channels within the same `select` to GC the 2nd one.
func (m *Machine) WhenNot(states []string, ctx context.Context) chan struct{} {
	ch := make(chan struct{})
	if ctx == nil {
		ctx = m.Ctx
	}

	// if all active, close early
	if m.Not(states) {
		close(ch)
		return ch
	}

	m.activeStatesLock.Lock()
	setMap := stateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = m.Is(S{s})
		if !setMap[s] {
			matched++
		}
	}
	// add the binding to an index of each state
	binding := &whenBinding{
		ch:       ch,
		negation: true,
		states:   setMap,
		total:    len(states),
		matched:  matched,
	}
	for _, s := range states {
		if _, ok := m.indexWhen[s]; !ok {
			m.indexWhen[s] = []*whenBinding{binding}
		} else {
			m.indexWhen[s] = append(m.indexWhen[s], binding)
		}
	}
	go func() {
		// dispose the binding on ctx.Done() and m.Ctx.Done()
		select {
		case <-ctx.Done():
		case <-m.Ctx.Done():
			m.activeStatesLock.Lock()
			for _, s := range states {
				if _, ok := m.indexWhen[s]; ok {
					m.indexWhen[s] = lo.Without(m.indexWhen[s], binding)
				}
			}
			m.activeStatesLock.Unlock()
		}
	}()
	m.activeStatesLock.Unlock()

	return ch
}

// Time returns a list of logical clocks of specified states (or all the states
// if nil).
// states: optionally passing a list of states param guarantees a deterministic
// order of the result.
func (m *Machine) Time(states S) T {
	if states == nil {
		states = m.StateNames
	}
	return lo.Map(states, func(state string, i int) uint64 {
		return m.clock[state]
	})
}

// TimeSum returns the sum of logical clocks of specified states (or all states
// if nil). It's a very inaccurate, yet simple way to measure the machine's
// time.
func (m *Machine) TimeSum(states S) uint64 {
	return lo.Reduce(m.Time(states), func(acc uint64, clock uint64,
		i int,
	) uint64 {
		return acc + clock
	}, 0)
}

// Add activates a list of states in the machine, returning the result of the
// transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) Add(states S, args A) Result {
	m.queueMutation(MutationTypeAdd, states, args)
	return m.processQueue()
}

// AddErr is a shorthand method to add the Exception state with the passed
// error.
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) AddErr(err error) Result {
	// TODO test .Err
	m.Err = err
	return m.Add(S{"Exception"}, A{"err": err})
}

// AddErrStr is a shorthand method to add the Exception state with the passed
// error string.
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) AddErrStr(err string) Result {
	return m.AddErr(errors.New(err))
}

// IsErr checks if the machine has the Exception state currently active.
func (m *Machine) IsErr() bool {
	return m.Is(S{"Exception"})
}

// Remove de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) Remove(states S, args A) Result {
	m.queueMutation(MutationTypeRemove, states, args)
	return m.processQueue()
}

// Set de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) Set(states S, args A) Result {
	m.queueMutation(MutationTypeSet, states, args)
	return m.processQueue()
}

// Is checks if all the passed states are currently active.
//
// ```
// machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
// machine.Add(S{"Foo"})
// machine.Is(S{"Foo"}) // true
// machine.Is(S{"Foo", "Bar"}) // false
// ```
func (m *Machine) Is(states S) bool {
	return lo.Every(m.ActiveStates, m.MustParseStates(states))
}

// Not checks if none of the passed states are currently active.
//
// ```
// machine.StringAll() // ()[A:0 B:0 C:0 D:0]
// machine.Add(S{"A", "B"})
// // not(A) and not(C)
// machine.Not(S{"A", "C"}) // false
// // not(C) and not(D)
// machine.Not(S{"C", "D"}) // true
// ```
func (m *Machine) Not(states S) bool {
	return lo.None(m.MustParseStates(states), m.ActiveStates)
}

// Any is group call to `Is`, returns true if any of the params return true
// from Is.
//
// ```
// machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
// machine.Add(S{"Foo"})
// // is(Foo, Bar) or is(Bar)
// machine.Any(S{"Foo", "Bar"}, S{"Bar"}) // false
// // is(Foo) or is(Bar)
// machine.Any(S{"Foo"}, S{"Bar"}) // true
// ```
func (m *Machine) Any(states ...S) bool {
	return lo.SomeBy(states, func(item S) bool {
		return m.Is(item)
	})
}

// IsClock checks if the passed state's clock equals the passed tick.
//
// ```
// tick := m.Clock("A")
// m.Remove("A")
// m.Add("A")
// m.Is("A", tick) // -> false
// ```
func (m *Machine) IsClock(state string, tick uint64) bool {
	return m.clock[state] == tick
}

// queueMutation queues a mutation to be executed.
func (m *Machine) queueMutation(mutationType MutationType, states S, args A) {
	statesParsed := m.MustParseStates(states)
	// Detect duplicates and avoid queueing them.
	if len(args) == 0 && m.detectQueueDuplicates(mutationType, statesParsed) {
		m.log(LogOps, "[queue:skipped] Duplicate detected for [%s] '%s'",
			mutationType, j(statesParsed))
		return
	}

	if m.DuringTransition() {
		m.log(LogOps, "[queue:%s] %s", mutationType, j(statesParsed))
	}

	m.Queue = append(m.Queue, Mutation{
		Type:         mutationType,
		CalledStates: statesParsed,
		Args:         args,
		Auto:         false,
	})
}

// GetStateCtx returns a context, bound to the current clock tick of the
// passed state.
//
// Context cancels when the state has been de-activated, or right away,
// if it isn't currently active.
func (m *Machine) GetStateCtx(state string) context.Context {
	// TODO handle cancellation while parsing the queue
	stateCtx, cancel := context.WithCancel(m.Ctx)
	// close early
	if !m.Is(S{state}) {
		cancel()
		return stateCtx
	}
	m.activeStatesLock.Lock()
	// add an index
	if _, ok := m.indexStateCtx[state]; !ok {
		m.indexStateCtx[state] = []context.CancelFunc{cancel}
	} else {
		m.indexStateCtx[state] = append(m.indexStateCtx[state], cancel)
	}
	m.activeStatesLock.Unlock()
	return stateCtx
}

// SetLogLevel sets the log level of the machine.
func (m *Machine) SetLogLevel(level LogLevel) {
	m.logLevel = level
}

// BindHandlers binds a struct of handler methods to the machine's states.
// Returns a HandlerBinding object, which signals when the binding is ready.
func (m *Machine) BindHandlers(handlers any) (*HandlerBinding, error) {
	binding := &HandlerBinding{
		Ready: make(chan struct{}),
	}
	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		fmt.Println("Function expects a pointer to a struct")
		return nil, errors.New("BindHandlers expects a pointer to a struct")
	}
	name := reflect.TypeOf(handlers).Elem().Name()
	// spin up an emitter loop
	emitter := m.newEmitter(name)
	go m.handleEmitterLoop(emitter, v, binding)
	return binding, nil
}

// On returns a channel that will be notified with *Event, when any of the
// passed events happen. It's quick substitute for a predefined transition
// handler, although it does not guarantee a deterministic order of execution.
// ctx: optional context to dispose the emitter earlier.
func (m *Machine) On(events []string, ctx context.Context) chan *Event {
	ch := make(chan *Event)
	emitter := m.newEmitter(fmt.Sprintf("ON %s", j(events)))
	if ctx == nil {
		ctx = m.Ctx
	}
	go func() {
		defer closeSafe(ch)
		defer m.disposeEmitter(emitter)
	emitterloop:
		for {
			select {
			case e, ok := <-emitter.startHandler():
				if !ok {
					m.log(LogEverything, "[end] channel closed")
					break emitterloop
				}
				if lo.Contains(events, e.Name) {
					ch <- e
				}
				// end the handler (always accept)
				emitter.endHandler(true)
			case <-ctx.Done():
				// ctx disposed
				break emitterloop
			case <-m.Ctx.Done():
				// machine disposed
				break emitterloop
			}
		}
	}()
	return ch
}

func (m *Machine) handleEmitterLoop(emitter *emitter,
	handlerMethods reflect.Value, binding *HandlerBinding,
) {
	internalEvents := []string{
		"queue-end",
		"transition-start",
		"transition-end",
		"transition-cancel",
		"tick",
	}
	if m.PanicToException {
		// catch panics and restart the emitter
		defer m.log(LogEverything, "[end] handleEmitterLoop %s", emitter.ID)
		defer m.recoverToErr(emitter, handlerMethods, binding)
	} else {
		// dispose and exit
		defer m.disposeEmitter(emitter)
		defer closeSafe(binding.Ready)
		defer m.log(LogEverything, "[end] handleEmitterLoop %s", emitter.ID)
	}

	// binding is ready, notify AFTER starting listening on the emitter
	go func() {
		m.log(LogEverything, "[start] handleEmitterLoop %s", emitter.ID)
		binding.Ready <- struct{}{}
	}()
emitterloop:
	for {
		select {
		// handle an event
		case e, ok := <-emitter.startHandler():
			if !ok {
				m.log(LogEverything, "[end] channel closed")
				break emitterloop
			}
			method := e.Name
			if lo.Contains(internalEvents, method) {
				emitter.endHandler(true)
				continue
			}
			// if no handler, log and skip
			if !handlerMethods.MethodByName(method).IsValid() {
				emitter.endHandler(true)
				continue
			}
			m.log(LogOps, "[handler] %s", method)
			if e.step != nil {
				// TODO pointer
				m.Transition.addSteps(*e.step)
			}
			// call the handler
			callRet := handlerMethods.MethodByName(e.Name).Call(
				[]reflect.Value{reflect.ValueOf(e)})
			var ret bool
			switch {
			// returns from these handlers are ignored
			case strings.HasSuffix(e.Name, "State"):
			case strings.HasSuffix(e.Name, "End"):
				ret = true
			default:
				if len(callRet) > 0 {
					ret = callRet[0].Interface().(bool)
				} else {
					// no return value, assume true
					ret = true
				}
			}
			emitter.endHandler(ret)

		// dispose and emitExitEvents
		case <-m.Ctx.Done():
			m.log(LogEverything, "[end] ctx done")
			break emitterloop
		}
	}
}

// recoverToErr recovers to the Exception state by catching panics.
// TODO refresh `m.indexWhen[]states` stateIsActive map
func (m *Machine) recoverToErr(emitter *emitter, handlerMethods reflect.Value,
	binding *HandlerBinding,
) {
	if m.Ctx.Err() != nil {
		return
	}
	r := recover()
	if r == nil {
		return
	}
	t := m.Transition
	// dont double handle an exception (no nesting)
	if lo.Contains(t.Mutation.CalledStates, "Exception") {
		return
	}
	m.log(LogOps, "[recover] handling panic...")
	defer func() {
		// re-bind the handlers
		if emitter.EventChLocked {
			emitter.EventChMutex.Unlock()
			emitter.EventChLocked = false
		}
		go m.handleEmitterLoop(emitter, handlerMethods, binding)
		m.log(LogEverything, "[recover] %s during (%s)", emitter.ID,
			j(t.Mutation.CalledStates))
		<-binding.Ready
		m.log(LogEverything, "[recover] new binding ready")
		// continue the queue
		if m.Ctx.Err() == nil {
			m.recoveryCh <- true
		}
	}()
	err, ok := r.(error)
	if !ok {
		err = errors.New(fmt.Sprint(r))
	}
	m.Err = err
	// final phase, trouble...
	if t.latestStep != nil && t.latestStep.IsFinal {
		// try to fix active states
		finals := S{}
		finals = append(finals, t.Exits...)
		finals = append(finals, t.Enters...)
		activeStates := m.ActiveStates
		found := false
		// walk over enter/exits and remove states after the last step,
		// as their final handlers haven't been executed
		for _, s := range finals {
			if t.latestStep.FromState == s {
				found = true
			}
			if !found {
				continue
			}
			if t.latestStep.IsEnter {
				activeStates = lo.Without(activeStates, s)
			} else {
				activeStates = append(activeStates, s)
			}
		}
		m.log(LogOps, "[recover] partial final states as (%s)",
			j(activeStates))
		m.setActiveStates(t.CalledStates(), activeStates, t.IsAuto())
		t.IsCompleted = true
	}
	if t.Mutation == nil {
		// TODO can this even happen?
		panic(fmt.Sprintf("no mutation panic in %s: %s", emitter.ID, err))
	}
	// negotiation phase, simply cancel and...
	// unshift add:Exception to the beginning of the queue
	exception := Mutation{
		Type:         MutationTypeAdd,
		CalledStates: S{"Exception"},
		Args: A{
			"err": err,
			"panic": &ExceptionArgsPanic{
				CalledStates: t.Mutation.CalledStates,
				StatesBefore: t.StatesBefore,
				Transition:   t,
				LastStep:     t.latestStep,
				StackTrace:   debug.Stack(),
			},
		},
	}
	m.Queue = append([]Mutation{exception}, m.Queue...)
}

// MustParseStates parses the states and returns them as a list.
// Panics when a state is not defined.
func (m *Machine) MustParseStates(states S) S {
	// check if all FromState are defined in m.FromState
	for _, s := range states {
		if _, ok := m.States[s]; !ok {
			panic(fmt.Sprintf("state %s is not defined", s))
		}
	}
	return states
}

// setActiveStates sets the new active states incrementing the counters and
// returning the previously active states.
func (m *Machine) setActiveStates(calledStates S, targetStates S,
	isAuto bool,
) S {
	m.activeStatesLock.Lock()
	previous := m.ActiveStates
	newStates := DiffStates(targetStates, m.ActiveStates)
	removedStates := DiffStates(m.ActiveStates, targetStates)
	noChangeStates := DiffStates(targetStates, newStates)
	m.ActiveStates = targetStates
	// Tick all new states and called multi states
	for _, state := range targetStates {
		data := m.States[state]
		if !lo.Contains(previous, state) ||
			(lo.Contains(calledStates, state) && data.Multi) {
			m.clock[state]++
		}
	}

	// construct a logging msg
	logMsg := ""
	if len(newStates) > 0 {
		logMsg += " +" + strings.Join(newStates, " +")
	}
	if len(removedStates) > 0 {
		logMsg += " -" + strings.Join(removedStates, " -")
	}
	if len(noChangeStates) > 0 && m.logLevel > LogDecisions {
		logMsg += " " + j(noChangeStates)
	}
	if len(logMsg) > 0 {
		autoLabel := ""
		if isAuto {
			autoLabel = ":auto"
		}
		m.log(LogChanges, "[state%s]"+logMsg, autoLabel)
	}

	m.activeStatesLock.Unlock()
	return previous
}

// processQueue processes the queue of mutations. It's the main loop of the
// machine.
func (m *Machine) processQueue() Result {
	lenQueue := len(m.Queue)
	// empty queue
	if lenQueue == 0 {
		return Canceled
	}
	// acquire the mutex or log and return
	if !m.queueLock.TryLock() {
		label := "items"
		if lenQueue == 1 {
			label = "item"
		}
		m.log(LogOps, "[postpone] queue running (%d %s)", lenQueue, label)
		return Queued
	}

	var ret []Result
	defer func() {
		m.emit("queue-end", nil, nil)
		m.Transition = nil
		m.queueLock.Unlock()
	}()

	// execute the queue
	for len(m.Queue) > 0 {
		// shift the queue
		item := &m.Queue[0]
		m.Queue = m.Queue[1:]
		m.Transition = newTransition(m, item)
		// execute the transition
		ret = append(ret, m.Transition.emitEvents())
		m.processWhenBindings()
		m.processStateCtxBindings()
	}
	if len(ret) == 0 {
		return Canceled
	}
	return ret[0]
}

func (m *Machine) processStateCtxBindings() {
	deactivated := DiffStates(m.Transition.StatesBefore, m.ActiveStates)
	m.activeStatesLock.Lock()
	var toCancel []context.CancelFunc
	for _, s := range deactivated {
		for _, cancel := range m.indexStateCtx[s] {
			toCancel = append(toCancel, cancel)
		}
		delete(m.indexStateCtx, s)
	}
	m.activeStatesLock.Unlock()
	// cancel all the state contexts outside the critical zone
	for _, cancel := range toCancel {
		cancel()
	}
}

func (m *Machine) processWhenBindings() {
	activated := DiffStates(m.ActiveStates, m.Transition.StatesBefore)
	deactivated := DiffStates(m.Transition.StatesBefore, m.ActiveStates)
	all := S{}
	all = append(all, activated...)
	all = append(all, deactivated...)
	var toClose []chan struct{}
	m.activeStatesLock.Lock()
	for _, s := range all {
		for k, binding := range m.indexWhen[s] {
			if lo.Contains(activated, s) {
				// activated
				if !binding.negation {
					// When(
					if !binding.states[s] {
						binding.matched++
					}
				} else {
					// WhenNot(
					if !binding.states[s] {
						binding.matched--
					}
				}
				// mark as active
				binding.states[s] = true
			} else {
				// deactivated
				if !binding.negation {
					// When(
					if binding.states[s] {
						binding.matched--
					}
				} else {
					// WhenNot(
					if binding.states[s] {
						binding.matched++
					}
				}
				// mark as inactive
				binding.states[s] = false
			}
			if binding.matched < binding.total {
				continue
			}
			// completed - close and delete indexes for all involved states
			for state := range binding.states {
				if len(m.indexWhen[state]) == 1 {
					delete(m.indexWhen, state)
					continue
				}
				if state == s {
					m.indexWhen[s] = append(m.indexWhen[s][:k], m.indexWhen[s][k+1:]...)
					continue
				}
				// TODO slow?
				m.indexWhen[state] = lo.Without(m.indexWhen[state], binding)
			}
			// close outside the critical zone
			toClose = append(toClose, binding.ch)
		}
	}
	m.activeStatesLock.Unlock()
	for ch := range toClose {
		closeSafe(toClose[ch])
	}
}

// Log logs an [external] message with the LogChanges level (highest one).
// Optionally redirects to a custom logger from SetLogger.
func (m *Machine) Log(msg string, args ...any) {
	m.log(LogChanges, "[external] "+msg, args...)
}

// log logs a message if the log level is high enough.
// Optionally redirects to a custom logger from SetLogger.
func (m *Machine) log(level LogLevel, msg string, args ...any) {
	if level > m.logLevel || m.Ctx.Err() != nil {
		return
	}
	if m.LogID {
		msg = "[" + m.ID[:5] + "] " + msg
	}
	if m.logger != nil {
		m.logger(level, msg, args...)
		return
	}
	fmt.Printf(msg+"\n", args...)
}

// SetLogger sets a custom logger function.
func (m *Machine) SetLogger(fn Logger) {
	m.logger = fn
}

// emit is a synchronous (blocking) emit with cancellation via a return channel.
// Can block indefinitely if the handler doesn't return or the emitter isn't
// accepting events.
func (m *Machine) emit(name string, args A, step *TransitionStep) Result {
	for _, emitter := range m.emitters {
		e := &Event{
			Name:    name,
			Machine: m,
			Args:    args,
			step:    step,
		}
		t := m.Transition
		// queue-end lacks a transition
		targetStates := "---"
		if t != nil {
			targetStates = j(t.TargetStates)
		}
		stepID := name
		if step != nil {
			stepID = step.ID[:5]
		}
		emitter.EventChMutex.Lock()
		emitter.EventChLocked = true
		// cancel when disposed
		if emitter.Disposed || m.Ctx.Err() != nil {
			m.log(LogOps, "[cancel:%s] (%s) by disposed emitter", stepID,
				targetStates)
			return Canceled
		}
		if step != nil {
			emitterID := emitter.ID
			if len(emitterID) > 15 {
				emitterID = emitterID[:15]
			}
			emitterID = padString(strings.ReplaceAll(emitterID, " ", "_"), 15, "_")
			m.log(LogEverything, "[emit:%-15s:%s] %s", emitterID,
				stepID, name)
		}
		// call the handlers (with timeouts)
		select {
		// possible panic in the handler
		case emitter.EventCh <- e:
			// OK
		case <-m.Ctx.Done():
			m.log(LogOps, "[cancel:%s] (%s) by context", stepID, targetStates)
			return Canceled
		case <-time.After(m.HandlerTimeout):
			m.log(LogOps, "[cancel:%s] (%s) by timeout", stepID, targetStates)
			return Canceled
		}
		emitter.EventChLocked = false
		emitter.EventChMutex.Unlock()
		// wait for the handler to finish
		var res bool
		select {
		case res = <-emitter.ReturnCh:
			// OK
		case <-m.Ctx.Done():
			m.log(LogOps, "[cancel:%s] (%s) by context", stepID, targetStates)
			return Canceled
		case <-time.After(m.HandlerTimeout):
			m.log(LogOps, "[cancel:%s] (%s) by timeout", stepID, targetStates)
			return Canceled
		// recovery in case of panic
		case <-m.recoveryCh:
			m.log(LogOps, "[cancel:%s] (%s) by recover", stepID,
				targetStates)
			return Canceled
		}
		// check if this is an internal event
		if step == nil {
			continue
		}
		if !step.IsFinal && !res {
			var self string
			if step.IsSelf {
				self = ":self"
			}
			m.log(LogOps, "[cancel%s:%s] (%s) by %s", self, stepID,
				targetStates, name)
			// queue-end lacks a transition
			if t != nil {
				t.addSteps(*step)
			}
			return Canceled
		}
	}
	return Executed
}

// detectQueueDuplicates checks for duplicated mutations without params.
// 1. Check if a mutation is scheduled
// 2. Check if a counter mutation isn't scheduled later
func (m *Machine) detectQueueDuplicates(mutationType MutationType,
	parsed S,
) bool {
	// check if this mutation is already scheduled
	index := m.IsQueued(mutationType, parsed, true, true, 0)
	if index == -1 {
		return false
	}
	var counterMutationType MutationType
	switch mutationType {
	case MutationTypeAdd:
		counterMutationType = MutationTypeRemove
	case MutationTypeRemove:
		counterMutationType = MutationTypeAdd
	default:
	case MutationTypeSet:
		// avoid duplicating `set` only if at the end of the queue
		return index > 0 && len(m.Queue)-1 > 0
	}
	// Check if a counter mutation is scheduled and broaden the match
	// - with or without params
	// - state sets same or bigger than `states`
	return m.IsQueued(counterMutationType, parsed, false, false, index+1) == -1
}

// DuringTransition checks if the machine is currently during a transition.
func (m *Machine) DuringTransition() bool {
	return m.Transition != nil
}

// Clock return the current tick for a state.
func (m *Machine) Clock(state string) uint64 {
	return m.clock[state]
}

// From returns the states from before the transition. Assumes DuringTransition.
func (m *Machine) From() S {
	return m.Transition.StatesBefore
}

// To returns the target states of the transition. Assumes DuringTransition.
func (m *Machine) To() S {
	return m.Transition.TargetStates
}

// IsQueued checks if a particular mutation has been queued. Returns
// an index of the match or -1 if not found.
//
// mutationType: add, remove, set
// states: list of states used in the mutation
// withoutParamsOnly: matches only mutation without the arguments object
// statesStrictEqual: states of the mutation have to be exactly like `states`
// and not a superset.
func (m *Machine) IsQueued(mutationType MutationType, states S,
	withoutArgsOnly bool, statesStrictEqual bool, startIndex int,
) int {
	for index, item := range m.Queue {
		if index >= startIndex &&
			item.Type == mutationType &&
			((withoutArgsOnly && len(item.Args) == 0) || !withoutArgsOnly) &&
			// target states have to be at least as long as the checked ones
			// or exactly the same in case of a strict_equal
			((statesStrictEqual &&
				len(item.CalledStates) == len(states)) ||
				(!statesStrictEqual &&
					len(item.CalledStates) >= len(states))) &&
			// and all the checked ones have to be included in the target ones
			lo.Every(item.CalledStates, states) {
			return index
		}
	}
	return -1
}

// Has return true is passed states are registered in the machine.
func (m *Machine) Has(states S) bool {
	return lo.Every(m.StateNames, states)
}

// HasStateChanged checks current active states have changed from the passed
// ones.
func (m *Machine) HasStateChanged(before S) bool {
	lenEqual := len(before) == len(m.ActiveStates)
	return !lenEqual || len(DiffStates(before, m.ActiveStates)) > 0
}

// String returns a one line representation of the currently active states,
// with their clock values. Inactive states are omitted.
// Eg: (Foo:2 Bar:1)
func (m *Machine) String() string {
	ret := "("
	for _, state := range m.ActiveStates {
		if ret != "(" {
			ret += " "
		}
		ret += fmt.Sprintf("%s:%d", state, m.clock[state])
	}
	return ret + ")"
}

// StringAll returns a one line representation of all the states, with their
// clock values. Inactive states are in square brackets.
// Eg: (Foo:2 Bar:1)[Baz:0]
func (m *Machine) StringAll() string {
	ret := "("
	ret2 := "["
	for _, state := range m.StateNames {
		if lo.Contains(m.ActiveStates, state) {
			if ret != "(" {
				ret += " "
			}
			ret += fmt.Sprintf("%s:%d", state, m.clock[state])
			continue
		}
		if ret2 != "[" {
			ret2 += " "
		}
		ret2 += fmt.Sprintf("%s:%d", state, m.clock[state])
	}
	return ret + ")" + ret2 + "]"
}

// Inspect returns a multi-line string representation of the machine (states,
// relations, clocks).
// states: param for ordered or partial results.
func (m *Machine) Inspect(states S) string {
	if states == nil {
		states = m.StateNames
	}
	ret := ""
	for _, name := range states {
		state := m.States[name]
		active := "false"
		if lo.Contains(m.ActiveStates, name) {
			active = "true"
		}
		ret += name + ":\n"
		ret += fmt.Sprintf("  State:   %s %d\n", active, m.clock[name])
		if state.Auto {
			ret += "  Auto:    true\n"
		}
		if state.Multi {
			ret += "  Multi:   true\n"
		}
		if state.Add != nil {
			ret += "  Add:     " + j(state.Add) + "\n"
		}
		if state.Require != nil {
			ret += "  Require: " + j(state.Require) + "\n"
		}
		if state.Remove != nil {
			ret += "  Remove:  " + j(state.Remove) + "\n"
		}
		if state.After != nil {
			ret += "  After:   " + j(state.After) + "\n"
		}
		ret += "\n"
	}
	return ret
}
