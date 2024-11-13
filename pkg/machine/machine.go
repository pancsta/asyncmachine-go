// Package machine is a general purpose state machine for managing complex
// async workflows in a safe and structured way.
//
// It's a dependency-free implementation of AsyncMachine in Golang
// using channels and context. It aims at simplicity and speed.
package machine // import "github.com/pancsta/asyncmachine-go/pkg/machine"

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Machine represent states, provides mutation methods, helpers methods and
// info about the current and scheduled transitions (if any).
type Machine struct {
	// Unique ID of this machine. Default: random ID. Read-only.
	ID string
	// Time for a handler to execute. Default: time.Second
	HandlerTimeout time.Duration
	// If true, the machine will print all exceptions to stdout. Default: true.
	// Requires an ExceptionHandler binding and Machine.PanicToException set.
	LogStackTrace bool
	// If true, the machine will catch panic and trigger the Exception state.
	// Default: true.
	PanicToException bool
	// If true, logs will start with machine's ID (5 chars).
	// Default: true.
	LogID bool
	// Ctx is the context of the machine. Read-only.
	Ctx context.Context
	// Maximum number of mutations that can be queued. Default: 1000.
	QueueLimit int
	// Confirms the state names have been ordered using VerifyStates. Read-only.
	StatesVerified bool
	// Tracers are optional tracers for telemetry integrations.
	Tracers []Tracer
	// If true, the machine has been Disposed and is no-op. Read-only.
	Disposed atomic.Bool
	// ParentID is the ID of the parent machine (if any).
	ParentID string
	// Tags are optional tags for telemetry integrations etc.
	Tags []string

	// Err is the last error that occurred.
	err atomic.Value
	// Currently executing transition (if any).
	t              atomic.Pointer[Transition]
	transitionLock sync.RWMutex
	// states is a map of state names to state definitions.
	states     Struct
	statesLock sync.RWMutex
	// activeStates is list of currently active states.
	activeStates     S
	activeStatesLock sync.RWMutex
	// queue of mutations to be executed.
	queue           []*Mutation
	queueLock       sync.RWMutex
	queueProcessing atomic.Bool
	queueLen        atomic.Int32
	queueRunning    atomic.Bool
	// Relations resolver, used to produce target states of a transition.
	// Default: *DefaultRelationsResolver.
	resolver RelationsResolver
	// List of all the registered state names.
	stateNames S
	// disposeLock is used to lock the disposal process.
	disposeLock sync.RWMutex
	handlers    []*handler
	clock       Clock
	cancel      context.CancelFunc
	logLevel    LogLevel
	logger      atomic.Pointer[Logger]
	panicCaught bool
	// unlockDisposed means that disposal is in progress and holding the queueLock
	unlockDisposed     atomic.Bool
	indexWhen          IndexWhen
	indexWhenTime      IndexWhenTime
	indexWhenArgs      IndexWhenArgs
	indexWhenArgsLock  sync.RWMutex
	indexStateCtx      IndexStateCtx
	indexWhenQueue     []whenQueueBinding
	indexWhenQueueLock sync.Mutex
	tracersLock        sync.RWMutex
	handlerStart       chan *handlerCall
	handlerEnd         chan bool
	handlerTimeout     chan struct{}
	handlerPanic       chan recoveryData
	handlerTimer       *time.Timer
	handlerLoopRunning bool
	logEntriesLock     sync.Mutex
	logEntries         []*LogEntry
	logArgs            func(args A) map[string]string
	currentHandler     atomic.Value
	disposeHandlers    []func()
	timeLast           atomic.Pointer[Time]
	// Channel closing when the machine finished disposal. Read-only.
	whenDisposed chan struct{}
	detectEval   bool
}

// NewCommon creates a new Machine instance with all the common options set.
func NewCommon(
	ctx context.Context, id string, statesStruct Struct, stateNames S,
	handlers any, parent *Machine, opts *Opts,
) (*Machine, error) {
	machOpts := &Opts{ID: id}

	if opts != nil {
		machOpts = opts
		machOpts.ID = id
	}

	if os.ExpandEnv("AM_DEBUG") != "" {
		machOpts = OptsWithDebug(machOpts)
	}

	if parent != nil {
		machOpts.Parent = parent
	}

	mach := New(ctx, statesStruct, machOpts)
	err := mach.VerifyStates(stateNames)
	if err != nil {
		return nil, err
	}

	if handlers != nil {
		err = mach.BindHandlers(handlers)
		if err != nil {
			return nil, err
		}
	}

	return mach, nil
}

// New creates a new Machine instance, bound to context and modified with
// optional Opts.
func New(ctx context.Context, statesStruct Struct, opts *Opts) *Machine {
	// parse relations
	parsedStates := parseStruct(statesStruct)

	m := &Machine{
		ID:               randID(),
		HandlerTimeout:   100 * time.Millisecond,
		states:           parsedStates,
		clock:            Clock{},
		handlers:         []*handler{},
		LogStackTrace:    true,
		PanicToException: true,
		LogID:            true,
		QueueLimit:       1000,
		indexWhen:        IndexWhen{},
		indexWhenTime:    IndexWhenTime{},
		indexWhenArgs:    IndexWhenArgs{},
		indexStateCtx:    IndexStateCtx{},
		handlerStart:     make(chan *handlerCall),
		handlerEnd:       make(chan bool),
		handlerTimeout:   make(chan struct{}),
		handlerPanic:     make(chan recoveryData),
		handlerTimer:     time.NewTimer(24 * time.Hour),
		whenDisposed:     make(chan struct{}),
	}

	m.timeLast.Store(&Time{})

	// parse opts
	// TODO extract
	opts = cloneOptions(opts)
	var parent *Machine
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
		if opts.DontLogStackTrace {
			m.LogStackTrace = false
		}
		if opts.DontLogID {
			m.LogID = false
		}
		if opts.Resolver != nil {
			m.resolver = opts.Resolver
		}
		if opts.LogLevel != LogNothing {
			m.SetLogLevel(opts.LogLevel)
		}
		if opts.Tracers != nil {
			m.Tracers = opts.Tracers
		}
		if opts.LogArgs != nil {
			m.logArgs = opts.LogArgs
		}
		if opts.QueueLimit != 0 {
			m.QueueLimit = opts.QueueLimit
		}
		m.detectEval = opts.DetectEval
		parent = opts.Parent
		m.ParentID = opts.ParentID
		m.Tags = opts.Tags
	}

	// default resolver
	if m.resolver == nil {
		m.resolver = &DefaultRelationsResolver{
			Machine: m,
		}
	}

	// define the exception state (if missing)
	if _, ok := m.states[Exception]; !ok {
		m.states[Exception] = State{
			Multi: true,
		}
	}

	// infer and sort states for defaults
	for name := range m.states {
		m.stateNames = append(m.stateNames, name)
		slices.Sort(m.stateNames)
		m.clock[name] = 0
	}

	// init context (support nil for examples)
	if ctx == nil {
		ctx = context.TODO()
	}
	if parent != nil {
		m.ParentID = parent.ID

		// info the tracers about this being a submachine
		parent.tracersLock.RLock()
		for i := range parent.Tracers {
			if !parent.Tracers[i].Inheritable() {
				continue
			}
			parent.Tracers[i].NewSubmachine(parent, m)
		}
		parent.tracersLock.RUnlock()
	}
	m.Ctx, m.cancel = context.WithCancel(ctx)

	// tracers
	for i := range m.Tracers {
		m.Tracers[i].MachineInit(m)
	}

	return m
}

// Dispose disposes the machine and all its emitters. You can wait for the
// completion of the disposal with `<-mach.WhenDisposed`.
func (m *Machine) Dispose() {
	// dispose in a goroutine to avoid a deadlock when called from within a
	// handler

	go func() {
		if m.Disposed.Load() {
			m.log(LogDecisions, "[dispose] already disposed")
			return
		}
		m.queueProcessing.Store(false)
		m.unlockDisposed.Store(true)
		m.dispose(false)
	}()
}

// DisposeForce disposes the machine and all its emitters, without waiting for
// the queue to drain. Will cause panics.
func (m *Machine) DisposeForce() {
	m.dispose(true)
}

func (m *Machine) dispose(force bool) {
	if !m.Disposed.CompareAndSwap(false, true) {
		// already disposed
		return
	}

	m.tracersLock.RLock()
	for i := range m.Tracers {
		m.Tracers[i].MachineDispose(m.ID)
	}
	m.tracersLock.RUnlock()

	// skip the locks when forcing
	if !force {
		m.activeStatesLock.Lock()
		defer m.activeStatesLock.Unlock()
		m.indexWhenArgsLock.Lock()
		defer m.indexWhenArgsLock.Unlock()
		m.tracersLock.Lock()
		defer m.tracersLock.Unlock()
		m.disposeLock.Lock()
		defer m.disposeLock.Unlock()
	}

	m.log(LogEverything, "[end] dispose")
	m.cancel()
	if m.Err() == nil && m.Ctx.Err() != nil {
		m.err.Store(m.Ctx.Err())
	}
	for _, e := range m.handlers {
		m.disposeHandler(e)
	}
	m.logger.Store(nil)

	// state contexts get cancelled automatically
	m.indexStateCtx = nil

	// channels need to be closed manually
	for s := range m.indexWhen {
		for k := range m.indexWhen[s] {
			closeSafe(m.indexWhen[s][k].Ch)
		}
		m.indexWhen[s] = nil
	}
	m.indexWhen = nil

	for s := range m.indexWhenArgs {
		for k := range m.indexWhen[s] {
			closeSafe(m.indexWhenArgs[s][k].ch)
		}
		m.indexWhenArgs[s] = nil
	}
	m.indexWhenArgs = nil

	m.Tracers = nil
	closeSafe(m.handlerEnd)
	closeSafe(m.handlerPanic)
	closeSafe(m.handlerStart)

	if m.handlerTimer != nil {
		m.handlerTimer.Stop()
		m.handlerTimer = nil
	}

	m.clock = nil

	// release the queue lock
	if m.unlockDisposed.Load() {
		m.unlockDisposed.Store(false)
		m.queueProcessing.Store(false)
	}

	// run dispose handlers
	// TODO timeouts?
	for _, fn := range m.disposeHandlers {
		fn()
	}
	m.disposeHandlers = nil

	// the end
	closeSafe(m.whenDisposed)
}

// disposeHandler detaches the handler binding from the machine and disposes it.
func (m *Machine) disposeHandler(handler *handler) {
	m.log(LogEverything, "[end] handler %s", handler.name)
	m.handlers = slicesWithout(m.handlers, handler)
	handler.dispose()
}

// WhenErr returns a channel that will be closed when the machine is in the
// Exception state.
//
// ctx: optional context defaults to the machine's context.
func (m *Machine) WhenErr(ctx context.Context) <-chan struct{} {
	return m.When([]string{Exception}, ctx)
}

// When returns a channel that will be closed when all the passed states
// become active or the machine gets disposed.
//
// ctx: optional context that will close the channel when done. Useful when
// listening on 2 When() channels within the same `select` to GC the 2nd one.
// TODO re-use channels with the same state set and context
func (m *Machine) When(states S, ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	if m.Disposed.Load() {
		close(ch)
		return ch
	}

	// lock
	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()

	// if all active, close early
	if m.is(states) {
		close(ch)
		return ch
	}

	setMap := StateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = m.is(S{s})
		if setMap[s] {
			matched++
		}
	}

	// add the binding to an index of each state
	binding := &WhenBinding{
		Ch:       ch,
		Negation: false,
		States:   setMap,
		Total:    len(states),
		Matched:  matched,
	}

	// dispose with context
	disposeWithCtx(m, ctx, ch, states, binding, &m.activeStatesLock, m.indexWhen)

	// insert the binding
	for _, s := range states {
		if _, ok := m.indexWhen[s]; !ok {
			m.indexWhen[s] = []*WhenBinding{binding}
		} else {
			m.indexWhen[s] = append(m.indexWhen[s], binding)
		}
	}

	return ch
}

// When1 is an alias to When() for a single state.
// See When.
func (m *Machine) When1(state string, ctx context.Context) <-chan struct{} {
	return m.When(S{state}, ctx)
}

// WhenNot returns a channel that will be closed when all the passed states
// become inactive or the machine gets disposed.
//
// ctx: optional context that will close the channel when done. Useful when
// listening on 2 WhenNot() channels within the same `select` to GC the 2nd one.
func (m *Machine) WhenNot(states S, ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	if m.Disposed.Load() {
		close(ch)
		return ch
	}

	// if all active, close early
	if m.Not(states) {
		close(ch)
		return ch
	}

	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()

	setMap := StateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = m.is(S{s})
		if !setMap[s] {
			matched++
		}
	}

	// add the binding to an index of each state
	binding := &WhenBinding{
		Ch:       ch,
		Negation: true,
		States:   setMap,
		Total:    len(states),
		Matched:  matched,
	}

	// dispose with context
	disposeWithCtx(m, ctx, ch, states, binding, &m.activeStatesLock, m.indexWhen)

	// insert the binding
	for _, s := range states {
		if _, ok := m.indexWhen[s]; !ok {
			m.indexWhen[s] = []*WhenBinding{binding}
		} else {
			m.indexWhen[s] = append(m.indexWhen[s], binding)
		}
	}

	return ch
}

// WhenNot1 is an alias to WhenNot() for a single state.
// See WhenNot.
func (m *Machine) WhenNot1(state string, ctx context.Context) <-chan struct{} {
	return m.WhenNot(S{state}, ctx)
}

// WhenArgs returns a channel that will be closed when the passed state
// becomes active with all the passed args. Args are compared using the native
// '=='. It's meant to be used with async Multi states, to filter out
// a specific completion.
func (m *Machine) WhenArgs(
	state string, args A, ctx context.Context,
) <-chan struct{} {
	// TODO better val comparisons
	//  support regexp for strings
	ch := make(chan struct{})

	if m.Disposed.Load() {
		close(ch)

		return ch
	}

	m.MustParseStates(S{state})
	name := state + "State"

	// locks
	m.indexWhenArgsLock.Lock()
	defer m.indexWhenArgsLock.Unlock()

	// log
	m.log(LogDecisions, "[when:args] new matcher for %s", state)

	// try to reuse an existing channel
	for _, binding := range m.indexWhenArgs[name] {
		if compareArgs(binding.args, args) {
			return binding.ch
		}
	}

	binding := &WhenArgsBinding{
		ch:   ch,
		args: args,
	}

	// dispose with context
	disposeWithCtx(m, ctx, ch, S{name}, binding, &m.indexWhenArgsLock,
		m.indexWhenArgs)

	// insert the binding
	if _, ok := m.indexWhen[name]; !ok {
		m.indexWhenArgs[name] = []*WhenArgsBinding{binding}
	} else {
		m.indexWhenArgs[name] = append(m.indexWhenArgs[name], binding)
	}

	return ch
}

// WhenTime returns a channel that will be closed when all the passed states
// have passed the specified time. The time is a logical clock of the state.
// Machine time can be sourced from the Time() method, or Clock() for a specific
// state.
func (m *Machine) WhenTime(
	states S, times Time, ctx context.Context,
) <-chan struct{} {
	ch := make(chan struct{})
	valid := len(states) == len(times)
	m.MustParseStates(states)
	indexWhenTime := m.indexWhenTime

	// close early
	if m.Disposed.Load() || !valid {
		if !valid {
			m.log(LogDecisions, "[when:time] times for all passed stated required")
		}
		close(ch)
		return ch
	}

	// locks
	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()

	// if all times passed, close early
	passed := true
	for i, s := range states {
		if m.clock[s] < times[i] {
			passed = false
			break
		}
	}
	if passed {
		close(ch)
		return ch
	}

	completed := StateIsActive{}
	matched := 0
	index := map[string]int{}
	for i, s := range states {
		completed[s] = m.clock[s] >= times[i]
		if completed[s] {
			matched++
		}
		index[s] = i
	}

	// add the binding to an index of each state
	binding := &WhenTimeBinding{
		Ch:        ch,
		Index:     index,
		Completed: completed,
		Total:     len(states),
		Matched:   matched,
		Times:     times,
	}

	// dispose with context
	disposeWithCtx(m, ctx, ch, states, binding, &m.activeStatesLock,
		m.indexWhenTime)

	// insert the binding
	for _, s := range states {
		if _, ok := indexWhenTime[s]; !ok {
			indexWhenTime[s] = []*WhenTimeBinding{binding}
		} else {
			indexWhenTime[s] = append(indexWhenTime[s], binding)
		}
	}

	return ch
}

// WhenTicks waits N ticks of a single state (relative to now). Uses WhenTime
// underneath.
func (m *Machine) WhenTicks(
	state string, ticks int, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(S{state}, Time{uint64(ticks) + m.Tick(state)}, ctx)
}

// WhenTicksEq waits till ticks for a single state equal the given absolute
// value (or more). Uses WhenTime underneath.
func (m *Machine) WhenTicksEq(
	state string, ticks uint64, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(S{state}, Time{ticks}, ctx)
}

// WhenQueueEnds closes every time the queue ends, or the optional ctx expires.
func (m *Machine) WhenQueueEnds(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})

	if m.Disposed.Load() {
		close(ch)

		return ch
	}

	// locks
	m.indexWhenQueueLock.Lock()
	defer m.indexWhenQueueLock.Unlock()

	// finish early
	if !m.queueRunning.Load() {
		close(ch)

		return ch
	}

	// add the binding to an index of each state
	binding := whenQueueBinding{
		ch: ch,
	}

	// log
	m.log(LogDecisions, "[when:queue] new wait")

	// dispose with context
	// TODO extract
	if ctx != nil {
		go func() {
			select {
			case <-ch:
				return
			case <-m.Ctx.Done():
				return
			case <-ctx.Done():
			}
			// GC only if needed
			if m.Disposed.Load() {
				return
			}

			// TODO track
			closeSafe(ch)

			m.indexWhenQueueLock.Lock()
			defer m.indexWhenQueueLock.Unlock()

			m.indexWhenQueue = slicesWithout(m.indexWhenQueue, binding)
		}()
	}

	// insert the binding
	m.indexWhenQueue = append(m.indexWhenQueue, binding)

	return ch
}

// WhenDisposed returns a channel that will be closed when the machine is
// disposed. Requires bound handlers. Use Machine.Disposed in case no handlers
// have been bound.
func (m *Machine) WhenDisposed() <-chan struct{} {
	return m.whenDisposed
}

// Time returns machine's time, a list of ticks per state. Returned value
// includes the specified states, or all the states if nil.
func (m *Machine) Time(states S) Time {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	return m.time(states)
}

func (m *Machine) time(states S) Time {
	if states == nil {
		states = m.stateNames
	}

	m.disposeLock.RLock()
	defer m.disposeLock.RUnlock()

	ret := make(Time, len(states))
	for i, s := range states {
		ret[i] = m.clock[s]
	}

	return ret
}

// TimeSum returns the sum of machine's time (ticks per state).
// Returned value includes the specified states, or all the states if nil.
// It's a very inaccurate, yet simple way to measure the machine's
// time.
// TODO handle overflow
func (m *Machine) TimeSum(states S) uint64 {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	if states == nil {
		states = m.stateNames
	}

	ret := uint64(0)
	for _, s := range states {
		ret += m.clock[s]
	}

	return ret
}

// Add activates a list of states in the machine, returning the result of the
// transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) Add(states S, args A) Result {
	if m.Disposed.Load() || int(m.queueLen.Load()) >= m.QueueLimit {
		return Canceled
	}
	m.queueMutation(MutationAdd, states, args)

	return m.processQueue()
}

// Add1 is a shorthand method to add a single state with the passed args.
func (m *Machine) Add1(state string, args A) Result {
	return m.Add(S{state}, args)
}

// AddErr is a dedicated method to add the Exception state with the passed
// error and optional arguments.
// Like every mutation method, it will resolve relations and trigger handlers.
// AddErr produces a stack trace of the error, if LogStackTrace is enabled.
func (m *Machine) AddErr(err error, args A) Result {
	return m.AddErrState(Exception, err, args)
}

// AddErrState adds a dedicated error state, along with the build in Exception
// state.
// Like every mutation method, it will resolve relations and trigger handlers.
// AddErrState produces a stack trace of the error, if LogStackTrace is enabled.
func (m *Machine) AddErrState(state string, err error, args A) Result {
	if m.Disposed.Load() {
		return Canceled
	}
	// TODO test Err()
	m.err.Store(err)

	var trace string
	if m.LogStackTrace {
		trace = captureStackTrace()
	}

	// build args
	if args == nil {
		args = A{}
	} else {
		args = maps.Clone(args)
	}
	args["err"] = err
	args["err.trace"] = trace

	// TODO prepend to the queue? what effects / benefits
	return m.Add(S{state, Exception}, args)
}

// PanicToErr will catch a panic and add the Exception state. Needs to
// be called in a defer statement, just like a recover() call.
func (m *Machine) PanicToErr(args A) {
	r := recover()
	if r == nil {
		return
	}

	if err, ok := r.(error); ok {
		m.AddErr(err, args)
	} else {
		m.AddErr(fmt.Errorf("%v", err), args)
	}
}

// PanicToErrState will catch a panic and add the Exception state, along with
// the passed state. Needs to be called in a defer statement, just like a
// recover() call.
func (m *Machine) PanicToErrState(state string, args A) {
	r := recover()
	if r == nil {
		return
	}

	if err, ok := r.(error); ok {
		m.AddErrState(state, err, args)
	} else {
		m.AddErrState(state, fmt.Errorf("%v", err), args)
	}
}

// IsErr checks if the machine has the Exception state currently active.
func (m *Machine) IsErr() bool {
	return m.Is(S{Exception})
}

// Err returns the last error.
func (m *Machine) Err() error {
	err := m.err.Load()
	if err == nil {
		return nil
	}

	return err.(error)
}

// Remove de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) Remove(states S, args A) Result {
	if m.Disposed.Load() || int(m.queueLen.Load()) >= m.QueueLimit {
		return Canceled
	}

	// return early if none of the states is active
	m.queueLock.RLock()
	lenQueue := len(m.queue)

	// try ignoring this mutation, if none of the states is currently active
	var statesAny []S
	for _, name := range states {
		statesAny = append(statesAny, S{name})
	}

	if lenQueue == 0 && m.Transition() != nil && !m.Any(statesAny...) {
		m.queueLock.RUnlock()
		return Executed
	}

	m.queueLock.RUnlock()
	m.queueMutation(MutationRemove, states, args)

	return m.processQueue()
}

// Remove1 is a shorthand method to remove a single state with the passed args.
// See Remove().
func (m *Machine) Remove1(state string, args A) Result {
	return m.Remove(S{state}, args)
}

// Set de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) Set(states S, args A) Result {
	if m.Disposed.Load() || int(m.queueLen.Load()) >= m.QueueLimit {
		return Canceled
	}
	m.queueMutation(MutationSet, states, args)

	return m.processQueue()
}

// Is checks if all the passed states are currently active.
//
//	machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
//	machine.Add(S{"Foo"})
//	machine.Is(S{"Foo"}) // true
//	machine.Is(S{"Foo", "Bar"}) // false
func (m *Machine) Is(states S) bool {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	return m.is(states)
}

// Is1 is a shorthand method to check if a single state is currently active.
// See Is().
func (m *Machine) Is1(state string) bool {
	return m.Is(S{state})
}

// is is an unsafe version of Is(), make sure to acquire m.activeStatesLock
func (m *Machine) is(states S) bool {
	for _, s := range states {
		if !slices.Contains(m.activeStates, s) {
			return false
		}
	}

	return true
}

// Not checks if **none** of the passed states are currently active.
//
//	machine.StringAll()
//	// -> ()[A:0 B:0 C:0 D:0]
//	machine.Add(S{"A", "B"})
//
//	// not(A) and not(C)
//	machine.Not(S{"A", "C"})
//	// -> false
//
//	// not(C) and not(D)
//	machine.Not(S{"C", "D"})
//	// -> true
func (m *Machine) Not(states S) bool {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	return slicesNone(m.MustParseStates(states), m.activeStates)
}

// Not1 is a shorthand method to check if a single state is currently inactive.
// See Not().
func (m *Machine) Not1(state string) bool {
	return m.Not(S{state})
}

// Any is group call to Is, returns true if any of the params return true
// from Is.
//
//	machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
//	machine.Add(S{"Foo"})
//	// is(Foo, Bar) or is(Bar)
//	machine.Any(S{"Foo", "Bar"}, S{"Bar"}) // false
//	// is(Foo) or is(Bar)
//	machine.Any(S{"Foo"}, S{"Bar"}) // true
func (m *Machine) Any(states ...S) bool {
	for _, s := range states {
		if m.Is(s) {
			return true
		}
	}
	return false
}

// Any1 is group call to Is1(), returns true if any of the params return true
// from Is1().
func (m *Machine) Any1(states ...string) bool {
	for _, s := range states {
		if m.Is1(s) {
			return true
		}
	}
	return false
}

// queueMutation queues a mutation to be executed.
func (m *Machine) queueMutation(mutationType MutationType, states S, args A) {
	statesParsed := m.MustParseStates(states)
	// Detect duplicates and avoid queueing them.
	if len(args) == 0 && m.detectQueueDuplicates(mutationType, statesParsed) {
		m.log(LogOps, "[queue:skipped] Duplicate detected for [%s] %s",
			mutationType, j(statesParsed))
		return
	}

	m.log(LogOps, "[queue:%s] %s", mutationType, j(statesParsed))

	// args should always be initialized
	if args == nil {
		args = A{}
	}

	m.queueLock.Lock()
	m.queue = append(m.queue, &Mutation{
		Type:         mutationType,
		CalledStates: statesParsed,
		Args:         args,
		Auto:         false,
	})
	m.queueLen.Store(int32(len(m.queue)))
	m.queueLock.Unlock()
}

// Eval executes a function on the machine's queue, allowing to avoid using
// locks for non-handler code. Blocking code should NOT be scheduled here.
// Eval cannot be called within a handler's critical section, as both are using
// the same serial queue and will deadlock. Eval has a timeout of
// HandlerTimeout/2 and will return false in case it happens.
//
// ctx: nil context defaults to machine's context.
//
// Note: usage of Eval is discouraged. Consider using AM_DETECT_EVAL in tests.
func (m *Machine) Eval(source string, fn func(), ctx context.Context) bool {
	if source == "" {
		panic("error: source of eval is required")
	}
	if m.detectEval {
		// check every method of every handler against the stack trace
		trace := captureStackTrace()

		for i := 0; !m.Disposed.Load() && i < len(m.handlers); i++ {
			handler := m.handlers[i]

			for _, method := range handler.methodNames {
				match := fmt.Sprintf(".(*%s).%s", handler.name, method)

				for _, line := range strings.Split(trace, "\n") {
					if strings.Contains(line, match) {
						panic("error: no Eval() within a handler")
					}
				}
			}

		}
	}
	m.log(LogOps, "[eval] %s", source)

	// wrap the func with a done channel
	done := make(chan struct{})
	canceled := atomic.Bool{}
	wrap := func() {
		defer close(done)
		if canceled.Load() {
			return
		}
		fn()
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// TODO detect when called from a handler, skip the queue and avoid deadlocks
	m.queueLock.Lock()

	// prepend to the queue
	m.queue = append([]*Mutation{{
		Type: MutationEval,
		Eval: wrap,
		Ctx:  ctx,
	}}, m.queue...)
	m.queueLen.Store(int32(len(m.queue)))
	m.queueLock.Unlock()

	// process the queue if needed, but ignore the result
	m.processQueue()

	// wait with a timeout
	select {

	case <-time.After(m.HandlerTimeout / 2):
		canceled.Store(true)
		m.log(LogOps, "[eval:timeout] %s", source[0])
		return false

	case <-m.Ctx.Done():
		return false

	case <-ctx.Done():
		m.log(LogDecisions, "[eval:ctxdone] %s", source[0])
		return false

	case <-done:
	}

	m.log(LogEverything, "[eval:end] %s", source)
	return true
}

// NewStateCtx returns a new sub-context, bound to the current clock's tick of
// the passed state.
//
// Context cancels when the state has been de-activated, or right away,
// if it isn't currently active.
//
// State contexts are used to check state expirations and should be checked
// often inside goroutines.
// TODO reuse existing ctxs
func (m *Machine) NewStateCtx(state string) context.Context {
	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()

	// TODO handle cancelation while parsing the queue
	// TODO include current clocks as context values
	stateCtx, cancel := context.WithCancel(m.Ctx)

	// close early
	if !m.is(S{state}) {
		cancel()
		return stateCtx
	}

	// add an index
	if _, ok := m.indexStateCtx[state]; !ok {
		m.indexStateCtx[state] = []context.CancelFunc{cancel}
	} else {
		m.indexStateCtx[state] = append(m.indexStateCtx[state], cancel)
	}

	return stateCtx
}

// TODO NewTransitionCtx...
// func (m *Machine) NewTransitionCtx(state string, ev Event) context.Context {
//
// }

// BindHandlers binds a struct of handler methods to the machine's states.
// Returns a HandlerBinding object, which signals when the binding is ready.
// TODO detect handlers for unknown states
func (m *Machine) BindHandlers(handlers any) error {
	if !m.handlerLoopRunning {
		m.handlerLoopRunning = true

		// start the handler loop
		go m.handlerLoop()
	}

	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindHandlers expects a pointer to a struct")
	}

	name := reflect.TypeOf(handlers).Elem().Name()

	// detect methods
	var methodNames []string
	if m.detectEval {
		t := reflect.TypeOf(handlers)
		// TODO prevent using these names as state names
		suffixes := []string{"Enter", "Exit", "State", "End", "Any"}
		for i := 0; i < t.NumMethod(); i++ {
			method := t.Method(i).Name
			match := false

			// check the format
			for _, s := range m.stateNames {
				if strings.HasPrefix(method, s) {
					for _, ss := range m.stateNames {
						if s+ss == method {
							match = true
							break
						}
					}

					for _, suffix := range suffixes {
						if s+suffix == method {
							match = true
							break
						}
					}

				} else if "Any"+s == method {
					match = true
				}
			}

			if "AnyAny" == method {
				match = true
			}

			if match {
				methodNames = append(methodNames, method)
				// TODO verify method signatures early (returns and params)
			}
		}
	}

	// register the handler binding
	m.newHandler(name, &v, methodNames)

	return nil
}

// recoverToErr recovers to the Exception state by catching panics.
func (m *Machine) recoverToErr(handler *handler, r recoveryData) {
	if m.Ctx.Err() != nil {
		return
	}

	m.panicCaught = true
	m.currentHandler.Store("")
	t := m.t.Load()

	// dont double handle an exception (no nesting)
	if slices.Contains(t.Mutation.CalledStates, "Exception") {
		return
	}

	m.log(LogOps, "[recover] handling panic...")
	err := fmt.Errorf("%s", r.err)
	m.err.Store(err)

	// final phase, trouble...
	if t.latestStep != nil && t.latestStep.IsFinal {

		// try to fix active states
		finals := S{}
		finals = append(finals, t.Exits...)
		finals = append(finals, t.Enters...)
		m.activeStatesLock.RLock()
		activeStates := m.activeStates
		m.activeStatesLock.RUnlock()
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
				activeStates = slicesWithout(activeStates, s)
			} else {
				activeStates = append(activeStates, s)
			}
		}

		m.log(LogOps, "[recover] partial final states as (%s)",
			j(activeStates))
		m.setActiveStates(t.CalledStates(), activeStates, t.IsAuto())
		t.isCompleted.Store(true)
	}

	m.log(LogOps, "[cancel] (%s) by recover", j(t.TargetStates))
	if t.Mutation == nil {
		// TODO can this even happen?
		panic(fmt.Sprintf("no mutation panic in %s: %s", handler.name, err))
	}

	// negotiation phase, simply cancel and...
	// prepend add:Exception to the beginning of the queue
	exception := &Mutation{
		Type:         MutationAdd,
		CalledStates: S{Exception},
		Args: A{
			"err": err,
			"panic": &ExceptionArgsPanic{
				CalledStates: t.Mutation.CalledStates,
				StatesBefore: t.StatesBefore,
				Transition:   t,
				LastStep:     t.latestStep,
				StackTrace:   r.stack,
			},
		},
	}

	// prepend the exception to the queue
	m.queue = append([]*Mutation{exception}, m.queue...)
	m.queueLen.Store(int32(len(m.queue)))

	// restart the handler loop
	go m.handlerLoop()
}

// MustParseStates parses the states and returns them as a list.
// Panics when a state is not defined. It's an usafe equivalent of VerifyStates.
func (m *Machine) MustParseStates(states S) S {
	// check if all states are defined in m.Struct
	for _, s := range states {
		if _, ok := m.states[s]; !ok {
			panic(fmt.Sprintf("state %s is not defined", s))
		}
	}

	return slicesUniq(states)
}

// VerifyStates verifies an array of state names and returns an error in case
// at least one isn't defined. It also retains the order and uses it for
// StateNames (only if all states have been passed). Not thread-safe.
func (m *Machine) VerifyStates(states S) error {
	var errs []error
	var checked []string

	for _, s := range states {

		if slices.Contains(checked, s) {
			errs = append(errs, fmt.Errorf("state %s duplicated", s))
			continue
		}

		if _, ok := m.states[s]; !ok {
			errs = append(errs, fmt.Errorf("state %s is not defined", s))
			continue
		}

		// TODO verify references, unknown fields

		checked = append(checked, s)
	}

	if len(errs) > 1 {
		return errors.Join(errs...)
	} else if len(errs) == 1 {
		return errs[0]
	}

	if len(m.stateNames) > len(states) {
		missing := DiffStates(m.stateNames, checked)
		return fmt.Errorf("undefined states: %s", j(missing))
	}

	// memorize the state names order
	m.stateNames = slices.Clone(states)
	m.StatesVerified = true

	// tracers
	m.tracersLock.RLock()
	for i := 0; !m.Disposed.Load() && i < len(m.Tracers); i++ {
		m.Tracers[i].VerifyStates(m)
	}
	m.tracersLock.RUnlock()

	return nil
}

// setActiveStates sets the new active states incrementing the counters and
// returning the previously active states.
func (m *Machine) setActiveStates(calledStates S, targetStates S,
	isAuto bool,
) S {
	if m.Disposed.Load() {
		// no-op
		return S{}
	}

	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()

	previous := m.activeStates
	newStates := DiffStates(targetStates, m.activeStates)
	removedStates := DiffStates(m.activeStates, targetStates)
	noChangeStates := DiffStates(targetStates, newStates)
	m.activeStates = slices.Clone(targetStates)

	// Tick all new states by +1 and already active and called multi states by +2
	for _, state := range targetStates {

		data := m.states[state]
		if !slices.Contains(previous, state) {
			// tick by +1
			// TODO wrap on overflow
			m.clock[state]++
		} else if slices.Contains(calledStates, state) && data.Multi {

			// tick by +2 to indicate a new instance
			// TODO wrap on overflow
			m.clock[state] += 2
			// treat prev active multi states as new states, for logging
			newStates = append(newStates, state)
		}
	}

	// tick de-activated states by +1
	for _, state := range removedStates {
		m.clock[state]++
	}

	// construct a logging msg
	if m.logLevel > LogNothing {
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

			args := m.t.Load().LogArgs()
			m.log(LogChanges, "[state%s]"+logMsg+args, autoLabel)
		}
	}

	return previous
}

// processQueue processes the queue of mutations. It's the main loop of the
// machine.
func (m *Machine) processQueue() Result {
	// empty queue
	if m.queueLen.Load() == 0 || m.Disposed.Load() {
		return Canceled
	}

	// try to acquire the lock
	if !m.queueProcessing.CompareAndSwap(false, true) {

		m.queueLock.Lock()
		defer m.queueLock.Unlock()

		label := "items"
		if len(m.queue) == 1 {
			label = "item"
		}
		m.log(LogOps, "[postpone] queue running (%d %s)", len(m.queue), label)

		return Queued
	}

	var ret []Result

	// execute the queue
	m.queueRunning.Store(false)
	for m.queueLen.Load() > 0 {
		m.queueRunning.Store(true)

		if m.Disposed.Load() {
			return Canceled
		}

		// shift the queue
		m.queueLock.Lock()
		item := m.queue[0]
		m.queue = m.queue[1:]
		m.queueLen.Store(int32(len(m.queue)))
		m.queueLock.Unlock()

		// support for context cancelation
		if item.Ctx != nil && item.Ctx.Err() != nil {
			ret = append(ret, Executed)
			continue
		}

		// special case for Eval mutations
		if item.Type == MutationEval {
			item.Eval()
			continue
		}
		newTransition(m, item)

		// execute the transition
		ret = append(ret, m.t.Load().emitEvents())

		m.timeLast.Store(&m.t.Load().TimeAfter)

		// process flow methods
		m.processWhenBindings()
		m.processWhenTimeBindings()
		m.processStateCtxBindings()
	}

	// release the locks
	m.transitionLock.Lock()
	m.t.Store(nil)
	m.transitionLock.Unlock()
	m.queueProcessing.Store(false)
	m.queueRunning.Store(false)

	// tracers
	m.tracersLock.RLock()
	for i := 0; !m.Disposed.Load() && i < len(m.Tracers); i++ {
		m.Tracers[i].QueueEnd(m)
	}
	m.tracersLock.RUnlock()
	m.processWhenQueueBindings()

	if len(ret) == 0 {
		return Canceled
	}
	return ret[0]
}

func (m *Machine) processStateCtxBindings() {
	m.activeStatesLock.RLock()
	deactivated := DiffStates(m.t.Load().StatesBefore, m.activeStates)

	var toCancel []context.CancelFunc
	for _, s := range deactivated {

		toCancel = append(toCancel, m.indexStateCtx[s]...)
		delete(m.indexStateCtx, s)
	}

	m.activeStatesLock.RUnlock()

	// cancel all the state contexts outside the critical zone
	for _, cancel := range toCancel {
		cancel()
	}
}

func (m *Machine) processWhenBindings() {
	m.activeStatesLock.Lock()

	// calculate activated and deactivated states
	activated := DiffStates(m.activeStates, m.t.Load().StatesBefore)
	deactivated := DiffStates(m.t.Load().StatesBefore, m.activeStates)

	// merge all states
	all := S{}
	all = append(all, activated...)
	all = append(all, deactivated...)

	var toClose []chan struct{}
	for _, s := range all {
		for _, binding := range m.indexWhen[s] {

			if slices.Contains(activated, s) {

				// state activated, check the index
				if !binding.Negation {
					// match for When(
					if !binding.States[s] {
						binding.Matched++
					}
				} else {
					// match for WhenNot(
					if !binding.States[s] {
						binding.Matched--
					}
				}

				// update index: mark as active
				binding.States[s] = true
			} else {

				// state deactivated
				if !binding.Negation {
					// match for When(
					if binding.States[s] {
						binding.Matched--
					}
				} else {
					// match for WhenNot(
					if binding.States[s] {
						binding.Matched++
					}
				}

				// update index: mark as inactive
				binding.States[s] = false
			}

			// if not all matched, ignore for now
			if binding.Matched < binding.Total {
				continue
			}

			// completed - close and delete indexes for all involved states
			var names []string
			for state := range binding.States {
				names = append(names, state)

				if len(m.indexWhen[state]) == 1 {
					delete(m.indexWhen, state)
					continue
				}

				// delete with a lookup TODO optimize
				m.indexWhen[state] = slicesWithout(m.indexWhen[state], binding)
			}

			m.log(LogDecisions, "[when] match for (%s)", j(names))
			// close outside the critical zone
			toClose = append(toClose, binding.Ch)
		}
	}
	m.activeStatesLock.Unlock()

	// notify outside the critical zone
	for ch := range toClose {
		closeSafe(toClose[ch])
	}
}

func (m *Machine) processWhenTimeBindings() {
	m.activeStatesLock.Lock()
	indexWhenTime := m.indexWhenTime
	var toClose []chan struct{}

	// collect all the ticked states
	all := S{}
	for state, t := range m.t.Load().ClockBefore() {
		// if changed, collect to check
		if m.clock[state] != t {
			all = append(all, state)
		}
	}

	// check all the bindings for all the ticked states
	for _, s := range all {
		for _, binding := range indexWhenTime[s] {

			// check if the requested time has passed
			if !binding.Completed[s] &&
				m.clock[s] >= binding.Times[binding.Index[s]] {
				binding.Matched++
				// mark in the index as completed
				binding.Completed[s] = true
			}

			// if not all matched, ignore for now
			if binding.Matched < binding.Total {
				continue
			}

			// completed - close and delete indexes for all involved states
			var names []string
			for state := range binding.Index {
				names = append(names, state)
				if len(indexWhenTime[state]) == 1 {
					delete(indexWhenTime, state)
					continue
				}

				// delete with a lookup TODO optimizes
				m.indexWhenTime[state] = slicesWithout(m.indexWhenTime[state], binding)
			}

			m.log(LogDecisions, "[when:time] match for (%s)", j(names))
			// close outside the critical zone
			toClose = append(toClose, binding.Ch)
		}
	}
	m.activeStatesLock.Unlock()

	// notify outside the critical zone
	for ch := range toClose {
		closeSafe(toClose[ch])
	}
}

func (m *Machine) processWhenQueueBindings() {
	m.indexWhenQueueLock.Lock()
	toPush := slices.Clone(m.indexWhenQueue)
	m.indexWhenQueue = nil
	m.indexWhenQueueLock.Unlock()

	for _, binding := range toPush {
		closeSafe(binding.ch)
	}
}

// SetLogArgs accepts a function which decides which mutation arguments to log.
// See NewArgsMapper or create your own manually.
func (m *Machine) SetLogArgs(matcher func(args A) map[string]string) {
	m.logArgs = matcher
}

// GetLogArgs returns the current log args function.
func (m *Machine) GetLogArgs() func(args A) map[string]string {
	return m.logArgs
}

func (m *Machine) getCurrentHandler() string {
	v := m.currentHandler.Load()

	if v == nil {
		return ""
	}

	return v.(string)
}

// Log logs an [extern] message unless LogNothing is set (default).
// Optionally redirects to a custom logger from SetLogger.
func (m *Machine) Log(msg string, args ...any) {
	prefix := "[extern"

	// try to prefix with the current handler, if present
	handler := m.getCurrentHandler()
	if handler != "" {
		prefix += ":" + truncateStr(handler, 15)
	}

	// single lines only
	msg = strings.ReplaceAll(msg, "\n", " ")

	m.log(LogChanges, prefix+"] "+msg, args...)
}

// log logs a message if the log level is high enough.
// Optionally redirects to a custom logger from SetLogger.
func (m *Machine) log(level LogLevel, msg string, args ...any) {
	if level > m.logLevel || m.Disposed.Load() {
		return
	}

	if m.LogID {
		id := m.ID
		if len(id) > 5 {
			id = id[:5]
		}
		msg = "[" + id + "] " + msg
	}

	out := fmt.Sprintf(msg, args...)
	logger := m.GetLogger()
	if logger != nil {
		(*logger)(level, msg, args...)
	} else {
		fmt.Println(out)
	}

	// dont modify completed transitions
	t := m.Transition()
	if t != nil && !t.IsCompleted() {
		// append the log msg to the current transition
		t.logEntriesLock.Lock()
		defer t.logEntriesLock.Unlock()
		t.LogEntries = append(t.LogEntries, &LogEntry{level, out})

	} else {
		// append the log msg the machine and collect at the end of the next
		// transition
		m.logEntriesLock.Lock()
		defer m.logEntriesLock.Unlock()

		m.logEntries = append(m.logEntries, &LogEntry{
			Level: level,
			Text:  out,
		})
	}
}

// SetLoggerSimple takes log.Printf and sets the log level in one
// call. Useful for testing. Requires LogChanges log level to produce any
// output.
func (m *Machine) SetLoggerSimple(
	logf func(format string, args ...any), level LogLevel,
) {
	if logf == nil {
		panic("logf cannot be nil")
	}

	var logger Logger = func(_ LogLevel, msg string, args ...any) {
		logf(msg, args...)
	}
	m.logger.Store(&logger)
	m.logLevel = level
}

// SetLoggerEmpty creates an empty logger that does nothing and sets the log
// level in one call. Useful when combined with am-dbg. Requires LogChanges log
// level to produce any output.
func (m *Machine) SetLoggerEmpty(level LogLevel) {
	var logger Logger = func(_ LogLevel, msg string, args ...any) {
		// no-op
	}
	m.logger.Store(&logger)
	m.logLevel = level
}

// SetLogger sets a custom logger function.
func (m *Machine) SetLogger(fn Logger) {
	if fn == nil {
		m.logger.Store(nil)

		return
	}
	m.logger.Store(&fn)
}

// GetLogger returns the current custom logger function, or nil.
func (m *Machine) GetLogger() *Logger {
	return m.logger.Load()
}

// SetLogLevel sets the log level of the machine.
func (m *Machine) SetLogLevel(level LogLevel) {
	m.logLevel = level
}

// GetLogLevel returns the log level of the machine.
func (m *Machine) GetLogLevel() LogLevel {
	return m.logLevel
}

// handle triggers methods on handlers structs.
// locked: transition lock currently held
func (m *Machine) handle(
	name string, args A, step *Step, locked bool,
) (Result, bool) {
	e := &Event{
		Name:    name,
		Machine: m,
		Args:    args,
		step:    step,
	}

	// queue-end lacks a transition
	targetStates := "---"
	if !locked {
		m.transitionLock.RLock()
	}
	t := m.t.Load()
	if t != nil && t.TargetStates != nil {
		targetStates = j(t.TargetStates)
	}
	if !locked {
		m.transitionLock.RUnlock()
	}

	// call the handlers
	res, handlerCalled := m.processHandlers(e)
	if m.panicCaught {
		res = Canceled
		m.panicCaught = false
	}

	// check if this is an internal event
	if step == nil {
		// TODO return res?
		return Executed, handlerCalled
	}

	// negotiation support
	if !step.IsFinal && res == Canceled {
		var self string
		if step.IsSelf {
			self = ":self"
		}
		m.log(LogOps, "[cancel%s] (%s) by %s", self,
			targetStates, name)

		return Canceled, handlerCalled
	}

	return res, handlerCalled
}

func (m *Machine) processHandlers(e *Event) (Result, bool) {
	m.disposeLock.RLock()
	defer m.disposeLock.RUnlock()

	handlerCalled := false
	for i := 0; !m.Disposed.Load() && i < len(m.handlers); i++ {
		h := m.handlers[i]

		// internal event
		if e.step == nil {
			break
		}

		name := e.Name

		if m.logLevel >= LogEverything {

			emitterID := truncateStr(h.name, 15)
			emitterID = padString(strings.ReplaceAll(emitterID, " ", "_"), 15, "_")
			m.log(LogEverything, "[handle:%-15s] %s", emitterID, name)
		}

		// cache
		method, ok := h.methodCache[name]
		if !ok {
			method = h.methods.MethodByName(name)
			if !method.IsValid() {
				continue
			}
			h.methodCache[name] = method
		}

		// call the handler
		m.log(LogOps, "[handler] %s", name)
		m.currentHandler.Store(name)
		var ret bool
		var timeout bool
		handlerCalled = true

		// tracers
		m.tracersLock.RLock()
		for i := range m.Tracers {
			m.Tracers[i].HandlerStart(m.t.Load(), h.name, name)
		}
		m.tracersLock.RUnlock()
		handlerCall := &handlerCall{
			fn:      method,
			event:   e,
			timeout: false,
		}

		// reuse the timer each time
		m.handlerTimer.Reset(m.HandlerTimeout)
		select {
		case <-m.Ctx.Done():
			break
		case m.handlerStart <- handlerCall:
		}

		// wait on the result / timeout / context
		select {

		case <-m.Ctx.Done():

		case <-m.handlerTimer.C:
			// notify the handler loop
			m.handlerTimeout <- struct{}{}
			m.log(LogOps, "[cancel] (%s) by timeout",
				j(m.t.Load().TargetStates))
			timeout = true

		case r := <-m.handlerPanic:
			// recover partial state
			m.recoverToErr(h, r)

		case ret = <-m.handlerEnd:
			m.log(LogEverything, "[handler:end] %s", e.Name)
			// ok
		}

		m.handlerTimer.Stop()
		m.currentHandler.Store("")

		// tracers
		m.tracersLock.RLock()
		for i := range m.Tracers {
			m.Tracers[i].HandlerEnd(m.t.Load(), h.name, name)
		}
		m.tracersLock.RUnlock()

		// handle negotiation
		switch {
		case timeout:

			return Canceled, handlerCalled
		case strings.HasSuffix(e.Name, "State"):
		case strings.HasSuffix(e.Name, "End"):
			// returns from State and End handlers are ignored
		default:
			if !ret {
				return Canceled, handlerCalled
			}
		}
	}

	// state args matchers
	m.processWhenArgs(e)

	return Executed, handlerCalled
}

func (m *Machine) handlerLoop() {
	// wait on the result / timeout / context
	for {
		select {

		case <-m.Ctx.Done():
			// dispose with context
			m.Dispose()
			return

		case call, ok := <-m.handlerStart:
			if !ok {
				return
			}
			ret := true
			retCh := make(chan struct{})

			// fork for timeout
			go func() {
				// catch panics and fwd
				if m.PanicToException {
					defer func() {
						err := recover()
						if err == nil {
							return
						}

						if !m.Disposed.Load() {
							// TODO support LogStackTrace
							m.handlerPanic <- recoveryData{
								err:   err,
								stack: captureStackTrace(),
							}
						}
					}()
				}

				// handler signature: FooState(e *am.Event)
				callRet := call.fn.Call([]reflect.Value{reflect.ValueOf(call.event)})
				if len(callRet) > 0 {
					ret = callRet[0].Interface().(bool)
				}
				close(retCh)
			}()

			select {
			case <-m.Ctx.Done():
				// dispose with context
				m.Dispose()
				continue
			case <-m.handlerTimeout:
				continue
			case <-retCh:
				// pass
			}

			select {
			case <-m.Ctx.Done():
				// dispose with context
				m.Dispose()
				return

			case m.handlerEnd <- ret:
			}
		}
	}
}

func (m *Machine) processWhenArgs(e *Event) {
	// check if a final entry handler (FooState)
	if e.step == nil || !e.step.IsFinal || !e.step.IsEnter {
		return
	}

	// process args channels
	m.indexWhenArgsLock.Lock()
	var chToClose []chan struct{}
	for _, binding := range m.indexWhenArgs[e.Name] {
		if !compareArgs(e.Args, binding.args) {
			continue
		}

		m.log(LogDecisions, "[when:args] match for %s", e.Name)
		// args match - dispose and close outside the mutex
		chToClose = append(chToClose, binding.ch)

		// GC
		if len(m.indexWhenArgs[e.Name]) == 1 {
			delete(m.indexWhenArgs, e.Name)
		} else {
			m.indexWhenArgs[e.Name] = slicesWithout(m.indexWhenArgs[e.Name], binding)
		}
	}
	m.indexWhenArgsLock.Unlock()

	for _, ch := range chToClose {
		closeSafe(ch)
	}
}

// detectQueueDuplicates checks for duplicated mutations
// 1. Check if a mutation is scheduled (without params)
// 2. Check if a counter mutation isn't scheduled later (any params)
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
	case MutationAdd:
		counterMutationType = MutationRemove
	case MutationRemove:
		counterMutationType = MutationAdd
	default:
	case MutationSet:
		// avoid duplicating `set` only if at the end of the queue
		return index > 0 && len(m.queue)-1 > 0
	}
	// Check if a counter mutation is scheduled and broaden the match
	// - with or without params
	// - state sets same or bigger than `states`
	return m.IsQueued(counterMutationType, parsed, false, false, index+1) == -1
}

// Transition returns the current transition, if any.
func (m *Machine) Transition() *Transition {
	return m.t.Load()
}

// Clock returns current machine's clock, a state-keyed map of ticks. If states
// are passed, only the ticks of the passed states are returned.
func (m *Machine) Clock(states S) Clock {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	if states == nil {
		states = m.stateNames
	}
	ret := make(Clock)
	for _, state := range states {
		ret[state] = m.clock[state]
	}

	return ret
}

// Tick return the current tick for a given state.
func (m *Machine) Tick(state string) uint64 {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	return m.clock[state]
}

// IsQueued checks if a particular mutation has been queued. Returns
// an index of the match or -1 if not found.
//
// mutationType: add, remove, set
//
// states: list of states used in the mutation
//
// withoutParamsOnly: matches only mutation without the arguments object
//
// statesStrictEqual: states of the mutation have to be exactly like `states`
// and not a superset.
// TODO test
func (m *Machine) IsQueued(mutationType MutationType, states S,
	withoutArgsOnly bool, statesStrictEqual bool, startIndex int,
) int {
	m.queueLock.RLock()
	defer m.queueLock.RUnlock()

	for index, item := range m.queue {
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
			slicesEvery(item.CalledStates, states) {
			return index
		}
	}
	return -1
}

// Has return true is passed states are registered in the machine.
// TODO test
func (m *Machine) Has(states S) bool {
	return slicesEvery(m.stateNames, states)
}

// Has1 is a shorthand for Has. It returns true if the passed state is
// registered in the machine.
func (m *Machine) Has1(state string) bool {
	return m.Has(S{state})
}

// IsClock checks if the machine has changed since the passed
// clock. Returns true if at least one state has changed.
func (m *Machine) IsClock(clock Clock) bool {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	for state, tick := range clock {
		if m.clock[state] != tick {
			return false
		}
	}

	return true
}

// IsTime checks if the machine has changed since the passed
// time (list of ticks). Returns true if at least one state has changed. The
// states param is optional and can be used to check only a subset of states.
func (m *Machine) IsTime(t Time, states S) bool {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	if states == nil {
		states = m.stateNames
	}

	for i, tick := range t {
		if m.clock[states[i]] != tick {
			return false
		}
	}

	return true
}

// String returns a one line representation of the currently active states,
// with their clock values. Inactive states are omitted.
// Eg: (Foo:1 Bar:3)
func (m *Machine) String() string {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	ret := "("
	for _, state := range m.stateNames {
		if !slices.Contains(m.activeStates, state) {
			continue
		}

		if ret != "(" {
			ret += " "
		}
		ret += fmt.Sprintf("%s:%d", state, m.clock[state])
	}

	return ret + ")"
}

// StringAll returns a one line representation of all the states, with their
// clock values. Inactive states are in square brackets.
// Eg: (Foo:1 Bar:3)[Baz:2]
func (m *Machine) StringAll() string {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	ret := "("
	ret2 := "["
	for _, state := range m.stateNames {
		if slices.Contains(m.activeStates, state) {
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
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	if states == nil {
		states = m.stateNames
	}

	ret := ""
	for _, name := range states {

		state := m.states[name]
		active := "false"
		if slices.Contains(m.activeStates, name) {
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

// Switch returns the first state from the passed list that is currently active,
// making it useful for switch statements.
//
//	switch mach.Switch(ss.GroupPlaying...) {
//	case "Playing":
//	case "Paused":
//	case "Stopped":
//	}
func (m *Machine) Switch(states ...string) string {
	activeStates := m.ActiveStates()

	for _, state := range states {
		if slices.Contains(activeStates, state) {
			return state
		}
	}

	return ""
}

// RegisterDisposalHandler adds a function to be called when the machine is
// disposed. This function will block before mach.WhenDispose is closed.
func (m *Machine) RegisterDisposalHandler(fn func()) {
	m.disposeHandlers = append(m.disposeHandlers, fn)
}

// ActiveStates returns a copy of the currently active states.
func (m *Machine) ActiveStates() S {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	return slices.Clone(m.activeStates)
}

// StateNames returns a copy of all the state names.
func (m *Machine) StateNames() S {
	return slices.Clone(m.stateNames)
}

// Queue returns a copy of the currently active states.
func (m *Machine) Queue() []*Mutation {
	m.queueLock.RLock()
	defer m.queueLock.RUnlock()

	return slices.Clone(m.queue)
}

// GetStruct returns a copy of machine's state structure.
func (m *Machine) GetStruct() Struct {
	m.statesLock.RLock()
	defer m.statesLock.RUnlock()

	return maps.Clone(m.states)
}

// SetStruct sets the machine's state structure. It will automatically call
// VerifyStates with the names param and handle EventStructChange if successful.
// Note: it's not recommended to change the states structure of a machine which
// has already produced transitions.
func (m *Machine) SetStruct(statesStruct Struct, names S) error {
	m.statesLock.RLock()
	defer m.statesLock.RUnlock()
	m.queueLock.RLock()
	defer m.queueLock.RUnlock()

	old := m.states
	m.states = parseStruct(statesStruct)

	err := m.VerifyStates(names)
	if err != nil {
		return err
	}

	// tracers
	m.tracersLock.RLock()
	for i := 0; !m.Disposed.Load() && i < len(m.Tracers); i++ {
		m.Tracers[i].StructChange(m, old)
	}
	m.tracersLock.RUnlock()

	return nil
}

// newHandler creates a new handler for Machine.
// Each handler should be consumed by one receiver only to guarantee the
// delivery of all events.
func (m *Machine) newHandler(
	name string, methods *reflect.Value, methodNames []string,
) *handler {
	e := &handler{
		name:        name,
		methods:     methods,
		methodNames: methodNames,
		methodCache: make(map[string]reflect.Value),
	}
	// TODO handler mutex?
	m.handlers = append(m.handlers, e)

	return e
}

// AddFromEv - planned.
// TODO AddFromEv
// TODO AddFromCtx using state ctx?
func (m *Machine) AddFromEv(states S, event *Event, args A) Result {
	panic("AddFromEv not implemented; github.com/pancsta/asyncmachine-go/pulls")
}

// RemoveFromEv TODO RemoveFromEv
// Planned.
func (m *Machine) RemoveFromEv(states S, event *Event, args A) Result {
	panic(
		"RemoveFromEv not implemented; github.com/pancsta/asyncmachine-go/pulls")
}

// SetFromEv TODO SetFromEv
// Planned.
func (m *Machine) SetFromEv(states S, event *Event, args A) Result {
	panic("SetFromEv not implemented; github.com/pancsta/asyncmachine-go/pulls")
}

// CanAdd TODO CanAdd
// Planned.
func (m *Machine) CanAdd(states S) bool {
	panic("CanAdd not implemented; github.com/pancsta/asyncmachine-go/pulls")
}

// CanSet TODO CanSet
// Planned.
func (m *Machine) CanSet(states S) bool {
	panic("CanSet not implemented; github.com/pancsta/asyncmachine-go/pulls")
}

// CanRemove TODO CanRemove
// Planned.
func (m *Machine) CanRemove(states S) bool {
	panic("CanRemove not implemented; github.com/pancsta/asyncmachine-go/pulls")
}

// Export exports the machine state: ID, time and state names.
func (m *Machine) Export() *Serialized {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	if !m.StatesVerified {
		panic("can't export - call VerifyStates first")
	}

	m.log(LogChanges, "[import] exported at %d ticks", m.time(nil))

	return &Serialized{
		ID:         m.ID,
		Time:       m.time(nil),
		StateNames: m.stateNames,
	}
}

// Import imports the machine state: ID, time and state names. It's not safe to
// import into a machine which has already produces transitions and/or has
// telemetry connected.
func (m *Machine) Import(data *Serialized) error {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	// restore active states and clocks
	var t uint64
	m.activeStates = nil
	for idx, v := range data.Time {
		state := data.StateNames[idx]
		t += v

		if !slices.Contains(m.stateNames, state) {
			return fmt.Errorf("%w: %s", ErrStateUnknown, state)
		}
		if IsActiveTick(v) {
			m.activeStates = append(m.activeStates, state)
		}

		m.clock[state] = v
	}

	// restore ID and state names
	m.stateNames = data.StateNames
	m.ID = data.ID
	m.StatesVerified = true

	m.log(LogChanges, "[import] imported to %d ticks", t)
	return nil
}

// Index returns the index of a state in the machine's StateNames() list.
func (m *Machine) Index(state string) int {
	return slices.Index(m.StateNames(), state)
}

// Resolver return the relation resolver, used to produce target states of
// transitions.
func (m *Machine) Resolver() RelationsResolver {
	return m.resolver
}
