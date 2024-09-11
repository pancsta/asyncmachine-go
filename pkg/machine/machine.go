// Package machine is a general purpose state machine for managing complex
// async workflows in a safe and structured way.
//
// It's a dependency-free implementation of AsyncMachine in Golang
// using channels and context. It aims at simplicity and speed.
package machine // import "github.com/pancsta/asyncmachine-go/pkg/machine"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"runtime/debug"
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
	PrintExceptions bool
	// If true, the machine will catch panic and trigger the Exception state.
	// Default: true.
	PanicToException bool
	// If true, logs will start with machine's ID (5 chars).
	// Default: true.
	LogID bool
	// Relations resolver, used to produce target states of a transition.
	// Default: *DefaultRelationsResolver. Read-only.
	Resolver RelationsResolver
	// Ctx is the context of the machine. Read-only.
	Ctx context.Context
	// Err is the last error that occurred. Read-only.
	Err error
	// Maximum number of mutations that can be queued. Default: 1000.
	QueueLimit int
	// Currently executing transition (if any). Read-only.
	Transition *Transition
	// List of all the registered state names. Read-only.
	StateNames S
	// Confirms the states have been ordered using VerifyStates. Read-only.
	StatesVerified bool
	// Tracers are optional tracers for telemetry integrations.
	Tracers []Tracer
	// If true, the machine has been Disposed and is no-op. Read-only.
	Disposed atomic.Bool
	// ParentID is the ID of the parent machine (if any).
	ParentID string
	// Tags are optional tags for telemetry integrations etc.
	Tags []string

	// states is a map of state names to state definitions.
	states     Struct
	statesLock sync.RWMutex
	// activeStates is list of currently active states.
	activeStates     S
	activeStatesLock sync.RWMutex
	// queue of mutations to be executed.
	queue           []*Mutation
	queueLock       sync.RWMutex
	queueProcessing sync.Mutex
	queueLen        int
	queueRunning    bool

	emitters    []*emitter
	clock       Clocks
	cancel      context.CancelFunc
	logLevel    LogLevel
	logger      Logger
	panicCaught bool
	// unlockDisposed means that disposal is in progress and holding the queueLock
	unlockDisposed     bool
	indexWhen          indexWhen
	indexWhenTime      indexWhenTime
	indexWhenArgs      indexWhenArgs
	indexWhenArgsLock  sync.RWMutex
	indexStateCtx      indexStateCtx
	indexEventCh       indexEventCh
	indexEventChLock   sync.Mutex
	indexWhenQueue     []whenQueueBinding
	indexWhenQueueLock sync.Mutex
	handlerStart       chan *handlerCall
	handlerEnd         chan bool
	handlerPanic       chan any
	handlerTimer       *time.Timer
	handlerLoopRunning bool
	logEntriesLock     sync.Mutex
	logEntries         []*LogEntry
	logArgs            func(args A) map[string]string
	currentHandler     string
	disposeHandlers    []func()
	timeLast           T
	// Channel closing when the machine finished disposal. Read-only.
	whenDisposed chan struct{}
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
		if parent != nil {
			machOpts = OptsWithParentTracers(machOpts, parent)
		}
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
		clock:            Clocks{},
		emitters:         []*emitter{},
		PrintExceptions:  true,
		PanicToException: true,
		LogID:            true,
		QueueLimit:       1000,
		indexWhen:        indexWhen{},
		indexWhenTime:    indexWhenTime{},
		indexWhenArgs:    indexWhenArgs{},
		indexStateCtx:    indexStateCtx{},
		indexEventCh:     indexEventCh{},
		handlerStart:     make(chan *handlerCall),
		handlerEnd:       make(chan bool),
		handlerPanic:     make(chan any),
		handlerTimer:     time.NewTimer(24 * time.Hour),
		whenDisposed:     make(chan struct{}),
	}

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
	if m.Resolver == nil {
		m.Resolver = &DefaultRelationsResolver{
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
		m.StateNames = append(m.StateNames, name)
		slices.Sort(m.StateNames)
		m.clock[name] = 0
	}

	// init context (support nil for examples)
	if ctx == nil {
		ctx = context.TODO()
	}
	if parent != nil {
		m.ParentID = parent.ID

		// info the tracers about this being a submachine
		for i := range parent.Tracers {
			if !parent.Tracers[i].Inheritable() {
				continue
			}
			parent.Tracers[i].NewSubmachine(parent, m)
		}
	}
	m.Ctx, m.cancel = context.WithCancel(ctx)

	// tracers
	for i := range m.Tracers {
		m.Tracers[i].MachineInit(m)
	}

	return m
}

func cloneOptions(opts *Opts) *Opts {
	if opts == nil {
		return &Opts{}
	}

	return &Opts{
		ID:                   opts.ID,
		HandlerTimeout:       opts.HandlerTimeout,
		DontPanicToException: opts.DontPanicToException,
		DontPrintExceptions:  opts.DontPrintExceptions,
		DontLogID:            opts.DontLogID,
		Resolver:             opts.Resolver,
		LogLevel:             opts.LogLevel,
		Tracers:              opts.Tracers,
		LogArgs:              opts.LogArgs,
		QueueLimit:           opts.QueueLimit,
		Parent:               opts.Parent,
		ParentID:             opts.ParentID,
		Tags:                 opts.Tags,
	}
}

// Dispose disposes the machine and all its emitters. You can wait for the
// completion of the disposal with `<-mach.WhenDisposed`.
func (m *Machine) Dispose() {
	// dispose in a goroutine to avoid a deadlock when called from within a
	// handler

	go func() {
		if m.Disposed.Load() {
			return
		}
		m.queueProcessing.Lock()
		m.unlockDisposed = true
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

	for i := range m.Tracers {
		m.Tracers[i].MachineDispose(m.ID)
	}

	// skip the locks when forcing
	if !force {
		m.activeStatesLock.Lock()
		defer m.activeStatesLock.Unlock()
		m.indexWhenArgsLock.Lock()
		defer m.indexWhenArgsLock.Unlock()
		m.indexEventChLock.Lock()
		defer m.indexEventChLock.Unlock()
	}

	m.log(LogEverything, "[end] dispose")
	m.cancel()
	if m.Err == nil && m.Ctx.Err() != nil {
		m.Err = m.Ctx.Err()
	}
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

	for s := range m.indexWhenArgs {
		for k := range m.indexWhen[s] {
			closeSafe(m.indexWhenArgs[s][k].ch)
		}
		m.indexWhenArgs[s] = nil
	}
	m.indexWhenArgs = nil

	for e := range m.indexEventCh {
		for k := range m.indexEventCh[e] {
			closeSafe(m.indexEventCh[e][k])
		}
		m.indexEventCh[e] = nil
	}
	m.indexEventCh = nil

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
	if m.unlockDisposed {
		m.unlockDisposed = false
		m.queueProcessing.Unlock()
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

// disposeEmitter detaches the emitter from the machine and disposes it.
func (m *Machine) disposeEmitter(emitter *emitter) {
	m.log(LogEverything, "[end] emitter %s", emitter.id)
	m.emitters = slicesWithout(m.emitters, emitter)
	emitter.dispose()
}

// WhenErr returns a channel that will be closed when the machine is in the
// Exception state.
//
// ctx: optional context defaults to the machine's context.
func (m *Machine) WhenErr(ctx context.Context) <-chan struct{} {
	// handle with a shared channel with broadcast via close
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

	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()

	// if all active, close early
	if m.is(states) {
		close(ch)
		return ch
	}

	setMap := stateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = m.is(S{s})
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

	// dispose with context
	diposeWithCtx(m, ctx, ch, states, binding, &m.activeStatesLock, m.indexWhen)

	// insert the binding
	for _, s := range states {
		if _, ok := m.indexWhen[s]; !ok {
			m.indexWhen[s] = []*whenBinding{binding}
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

	setMap := stateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = m.is(S{s})
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

	// dispose with context
	diposeWithCtx(m, ctx, ch, states, binding, &m.activeStatesLock, m.indexWhen)

	// insert the binding
	for _, s := range states {
		if _, ok := m.indexWhen[s]; !ok {
			m.indexWhen[s] = []*whenBinding{binding}
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
// TODO better val comparisons
func (m *Machine) WhenArgs(
	state string, args A, ctx context.Context,
) <-chan struct{} {
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

	binding := &whenArgsBinding{
		ch:   ch,
		args: args,
	}

	// dispose with context
	diposeWithCtx(m, ctx, ch, S{name}, binding, &m.indexWhenArgsLock,
		m.indexWhenArgs)

	// insert the binding
	if _, ok := m.indexWhen[name]; !ok {
		m.indexWhenArgs[name] = []*whenArgsBinding{binding}
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
	states S, times T, ctx context.Context,
) <-chan struct{} {
	ch := make(chan struct{})
	valid := len(states) == len(times)
	if m.Disposed.Load() || !valid {
		if !valid {
			m.log(LogDecisions, "[when:time] times for all passed stated required")
		}
		close(ch)
		return ch
	}
	m.MustParseStates(states)

	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()
	indexWhenTime := m.indexWhenTime

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

	completed := stateIsActive{}
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
	binding := &whenTimeBinding{
		ch:        ch,
		index:     index,
		completed: completed,
		total:     len(states),
		matched:   matched,
		times:     times,
	}

	// dispose with context
	diposeWithCtx(m, ctx, ch, states, binding, &m.activeStatesLock,
		m.indexWhenTime)

	// insert the binding
	for _, s := range states {
		if _, ok := indexWhenTime[s]; !ok {
			indexWhenTime[s] = []*whenTimeBinding{binding}
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
	return m.WhenTime(S{state}, T{uint64(ticks) + m.Clock(state)}, ctx)
}

// WhenTicksEq waits till ticks for a single state equal the given absolute
// value (or more). Uses WhenTime underneath.
func (m *Machine) WhenTicksEq(
	state string, ticks int, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(S{state}, T{uint64(ticks)}, ctx)
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
	if !m.queueRunning {
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

func (m *Machine) WhenDisposed() <-chan struct{} {
	return m.whenDisposed
}

// Time returns a list of logical clocks of specified states (or all the states
// if nil).
// states: optionally passing a list of states param guarantees a deterministic
// order of the result.
func (m *Machine) Time(states S) T {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	return m.time(states)
}

func (m *Machine) time(states S) T {
	if states == nil {
		states = m.StateNames
	}

	ret := make(T, len(states))
	for i, s := range states {
		ret[i] = m.clock[s]
	}

	return ret
}

// TimeSum returns the sum of logical clocks of specified states (or all states
// if nil). It's a very inaccurate, yet simple way to measure the machine's
// time.
// TODO handle overflow
func (m *Machine) TimeSum(states S) uint64 {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	if states == nil {
		states = m.StateNames
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
	if m.Disposed.Load() || m.queueLen >= m.QueueLimit {
		return Canceled
	}
	m.queueMutation(MutationAdd, states, args)

	return m.processQueue()
}

// Add1 is a shorthand method to add a single state with the passed args.
func (m *Machine) Add1(state string, args A) Result {
	return m.Add(S{state}, args)
}

// AddErr is a shorthand method to add the Exception state with the passed
// error.
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) AddErr(err error) Result {
	if m.Disposed.Load() {
		return Canceled
	}
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
	if m.Disposed.Load() || m.queueLen >= m.QueueLimit {
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

	if lenQueue == 0 && !m.DuringTransition() && !m.Any(statesAny...) {
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
	if m.Disposed.Load() || m.queueLen >= m.QueueLimit {
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
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	return slicesNone(m.MustParseStates(S{state}), m.activeStates)
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

// IsClock checks if the passed state's clock equals the passed tick.
//
//	tick := m.Clock("A")
//	m.Remove("A")
//	m.Add("A")
//	m.Is("A", tick) // -> false
func (m *Machine) IsClock(state string, tick uint64) bool {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	return m.clock[state] == tick
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

	if m.DuringTransition() {
		m.log(LogOps, "[queue:%s] %s", mutationType, j(statesParsed))
	}

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
	m.queueLen = len(m.queue)
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
	canceled := false
	wrap := func() {
		defer close(done)
		if canceled {
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
	m.queueLen = len(m.queue)
	m.queueLock.Unlock()

	// process the queue if needed, but ignore the result
	m.processQueue()

	// wait with a timeout
	select {

	case <-time.After(m.HandlerTimeout / 2):
		canceled = true
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
	// TODO handle cancellation while parsing the queue
	stateCtx, cancel := context.WithCancel(m.Ctx)

	// close early
	if !m.Is1(state) {
		cancel()
		return stateCtx
	}

	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()

	// add an index
	if _, ok := m.indexStateCtx[state]; !ok {
		m.indexStateCtx[state] = []context.CancelFunc{cancel}
	} else {
		m.indexStateCtx[state] = append(m.indexStateCtx[state], cancel)
	}

	return stateCtx
}

// SetLogLevel sets the log level of the machine.
func (m *Machine) SetLogLevel(level LogLevel) {
	m.logLevel = level
}

// BindHandlers binds a struct of handler methods to the machine's states.
// Returns a HandlerBinding object, which signals when the binding is ready.
// TODO verify method signatures early
// TODO detect handlers for unknown states
func (m *Machine) BindHandlers(handlers any) error {
	if !m.handlerLoopRunning {
		m.handlerLoopRunning = true

		// start the handler loop
		go m.handlerLoop()
	}

	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		fmt.Println("Function expects a pointer to a struct")
		return errors.New("BindHandlers expects a pointer to a struct")
	}

	// register the new emitter
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

// OnEvent returns a channel that will be notified with *Event, when any of
// the passed events happen. It's a quick substitute for predefined transition
// handlers, although it does not support negotiation.
//
// ctx: optional context to dispose the emitter early.
//
// It's not supported to nest OnEvent() calls, as it would cause a deadlock.
// Using OnEvent is recommended only in special cases, like test assertions.
// The Tracer API is a better way to event feeds.
func (m *Machine) OnEvent(events []string, ctx context.Context) chan *Event {
	ch := make(chan *Event, 50)
	if m.Disposed.Load() {
		ch := make(chan *Event)
		close(ch)
		return ch
	}

	m.indexEventChLock.Lock()
	defer m.indexEventChLock.Unlock()

	for _, e := range events {
		if _, ok := m.indexEventCh[e]; !ok {
			m.indexEventCh[e] = []chan *Event{ch}
		} else {
			m.indexEventCh[e] = append(m.indexEventCh[e], ch)
		}
	}

	// dispose with context
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

			m.indexEventChLock.Lock()
			for _, e := range events {
				if _, ok := m.indexEventCh[e]; ok {
					if len(m.indexEventCh[e]) == 1 {
						// delete the whole map, it's the last one
						delete(m.indexEventCh, e)
					} else {
						m.indexEventCh[e] = slicesWithout(m.indexEventCh[e], ch)
					}
				}
			}

			m.indexEventChLock.Unlock()
		}()
	}

	return ch
}

// recoverToErr recovers to the Exception state by catching panics.
func (m *Machine) recoverToErr(emitter *emitter, r any) {
	if m.Ctx.Err() != nil {
		return
	}

	m.panicCaught = true
	m.currentHandler = ""
	t := m.Transition

	// dont double handle an exception (no nesting)
	if slices.Contains(t.Mutation.CalledStates, "Exception") {
		return
	}

	m.log(LogOps, "[recover] handling panic...")
	err, ok := r.(error)
	if !ok {
		err = fmt.Errorf("%s", r)
	}
	m.Err = err

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
		t.IsCompleted = true
	}

	m.log(LogOps, "[cancel] (%s) by recover", j(t.TargetStates))
	if t.Mutation == nil {
		// TODO can this even happen?
		panic(fmt.Sprintf("no mutation panic in %s: %s", emitter.id, err))
	}

	// negotiation phase, simply cancel and...
	// prepend add:Exception to the beginning of the queue
	exception := &Mutation{
		Type:         MutationAdd,
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

	m.queue = append([]*Mutation{exception}, m.queue...)
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

	if len(m.StateNames) > len(states) {
		missing := DiffStates(m.StateNames, checked)
		return fmt.Errorf("undefined states: %s", j(missing))
	}

	// memorize the state names order
	m.StateNames = slices.Clone(states)
	m.StatesVerified = true

	return nil
}

// setActiveStates sets the new active states incrementing the counters and
// returning the previously active states.
func (m *Machine) setActiveStates(calledStates S, targetStates S,
	isAuto bool,
) S {
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

		args := m.Transition.LogArgs()
		m.log(LogChanges, "[state%s]"+logMsg+args, autoLabel)
	}

	return previous
}

// processQueue processes the queue of mutations. It's the main loop of the
// machine.
func (m *Machine) processQueue() Result {
	// empty queue
	if len(m.queue) == 0 || m.Disposed.Load() {
		return Canceled
	}

	// try to acquire the lock
	acquired := m.queueProcessing.TryLock()
	if !acquired {

		m.queueLock.Lock()
		defer m.queueLock.Unlock()

		label := "items"
		if len(m.queue) == 1 {
			label = "item"
		}
		m.log(LogOps, "[postpone] queue running (%d %s)", len(m.queue), label)
		m.emit(EventQueueAdd, A{"queue_len": len(m.queue)}, nil)

		return Queued
	}

	var ret []Result

	// execute the queue
	m.queueRunning = false
	for len(m.queue) > 0 {
		m.queueRunning = true

		if m.Disposed.Load() {
			return Canceled
		}
		if len(m.queue) == 0 {
			break
		}

		// shift the queue
		m.queueLock.Lock()
		item := m.queue[0]
		m.queue = m.queue[1:]
		m.queueLen = len(m.queue)
		m.queueLock.Unlock()

		// support for context cancellation
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
		ret = append(ret, m.Transition.emitEvents())

		m.timeLast = m.Transition.TAfter

		// process flow methods
		m.processWhenBindings()
		m.processWhenTimeBindings()
		m.processStateCtxBindings()
	}

	// release the atomic lock
	m.Transition = nil
	m.queueProcessing.Unlock()
	m.queueRunning = false
	for i := range m.Tracers {
		m.Tracers[i].QueueEnd(m)
	}
	m.emit(EventQueueEnd, A{"T": m.timeLast}, nil)
	m.processWhenQueueBindings()

	if len(ret) == 0 {
		return Canceled
	}
	return ret[0]
}

func (m *Machine) processStateCtxBindings() {
	m.activeStatesLock.RLock()
	deactivated := DiffStates(m.Transition.StatesBefore, m.activeStates)

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
	activated := DiffStates(m.activeStates, m.Transition.StatesBefore)
	deactivated := DiffStates(m.Transition.StatesBefore, m.activeStates)

	// merge all states
	all := S{}
	all = append(all, activated...)
	all = append(all, deactivated...)

	var toClose []chan struct{}
	for _, s := range all {
		for k, binding := range m.indexWhen[s] {

			if slices.Contains(activated, s) {

				// state activated, check the index
				if !binding.negation {
					// match for When(
					if !binding.states[s] {
						binding.matched++
					}
				} else {
					// match for WhenNot(
					if !binding.states[s] {
						binding.matched--
					}
				}

				// update index: mark as active
				binding.states[s] = true
			} else {

				// state deactivated
				if !binding.negation {
					// match for When(
					if binding.states[s] {
						binding.matched--
					}
				} else {
					// match for WhenNot(
					if binding.states[s] {
						binding.matched++
					}
				}

				// update index: mark as inactive
				binding.states[s] = false
			}

			// if not all matched, ignore for now
			if binding.matched < binding.total {
				continue
			}

			// completed - close and delete indexes for all involved states
			var names []string
			for state := range binding.states {
				names = append(names, state)

				if len(m.indexWhen[state]) == 1 {
					delete(m.indexWhen, state)
					continue
				}

				if state == s {
					m.indexWhen[s] = append(m.indexWhen[s][:k], m.indexWhen[s][k+1:]...)
					continue
				}

				m.indexWhen[state] = slices.Delete(m.indexWhen[state], k, k+1)
			}

			m.log(LogDecisions, "[when] match for (%s)", j(names))
			// close outside the critical zone
			toClose = append(toClose, binding.ch)
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
	for state, t := range m.Transition.ClocksBefore {
		// if changed, collect to check
		if m.clock[state] != t {
			all = append(all, state)
		}
	}

	// check all the bindings for all the ticked states
	for _, s := range all {
		for k, binding := range indexWhenTime[s] {

			// check if the requested time has passed
			if !binding.completed[s] &&
				m.clock[s] >= binding.times[binding.index[s]] {
				binding.matched++
				// mark in the index as completed
				binding.completed[s] = true
			}

			// if not all matched, ignore for now
			if binding.matched < binding.total {
				continue
			}

			// completed - close and delete indexes for all involved states
			var names []string
			for state := range binding.index {
				names = append(names, state)
				if len(indexWhenTime[state]) == 1 {
					delete(indexWhenTime, state)
					continue
				}
				if state == s {
					indexWhenTime[s] = append(indexWhenTime[s][:k],
						indexWhenTime[s][k+1:]...)
					continue
				}

				indexWhenTime[state] = slices.Delete(indexWhenTime[state], k, k+1)
			}

			m.log(LogDecisions, "[when:time] match for (%s)", j(names))
			// close outside the critical zone
			toClose = append(toClose, binding.ch)
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

// Log logs an [extern] message unless LogNothing is set (default).
// Optionally redirects to a custom logger from SetLogger.
func (m *Machine) Log(msg string, args ...any) {
	prefix := "[extern"

	// try to prefix with the current handler, if present
	handler := m.currentHandler
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
	if level > m.logLevel || m.Ctx.Err() != nil {
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
	if m.logger != nil {
		m.logger(level, msg, args...)
	} else {
		fmt.Println(out)
	}

	t := m.Transition
	if t != nil {
		// append the log msg to the current transition
		t.LogEntriesLock.Lock()
		defer t.LogEntriesLock.Unlock()
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

// SetLogger sets a custom logger function.
func (m *Machine) SetLogger(fn Logger) {
	m.logger = fn
}

// SetTestLogger hooks into the testing.Logf and set the log level in one call.
// Useful for testing. Requires LogChanges log level to produce any output.
func (m *Machine) SetTestLogger(
	logf func(format string, args ...any), level LogLevel,
) {
	if logf == nil {
		panic("logf cannot be nil")
	}
	m.logger = func(_ LogLevel, msg string, args ...any) {
		logf(msg, args...)
	}
	m.logLevel = level
}

// GetLogger returns the current custom logger function, or nil.
func (m *Machine) GetLogger() Logger {
	return m.logger
}

// emit is a synchronous (blocking) emit with cancellation via a return channel.
// Can block indefinitely if the handler doesn't return or the emitter isn't
// accepting events.
func (m *Machine) emit(
	name string, args A, step *Step,
) (Result, bool) {
	e := &Event{
		Name:    name,
		Machine: m,
		Args:    args,
		step:    step,
	}

	// queue-end lacks a transition
	targetStates := "---"
	if m.Transition != nil && m.Transition.TargetStates != nil {
		targetStates = j(m.Transition.TargetStates)
	}

	// call the handlers
	res, handlerCalled := m.processEmitters(e)
	if m.panicCaught {
		res = Canceled
		m.panicCaught = false
	}

	// check if this is an internal event
	if step == nil {
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

	return Executed, handlerCalled
}

func (m *Machine) processEmitters(e *Event) (Result, bool) {
	var emitter *emitter
	handlerCalled := false
	for _, emitter = range m.emitters {
		// disposed
		if m.Disposed.Load() {
			break
		}
		// internal event
		if e.step == nil {
			break
		}

		method := e.Name

		// format a log msg
		emitterID := truncateStr(emitter.id, 15)
		emitterID = padString(strings.ReplaceAll(emitterID, " ", "_"), 15, "_")
		// TODO confirm this is useful
		m.log(LogEverything, "[emit:%-15s] %s", emitterID, method)

		// TODO cache
		if !emitter.methods.MethodByName(method).IsValid() {
			continue
		}

		// call the handler
		m.log(LogOps, "[handler] %s", method)
		m.currentHandler = method
		var ret bool
		handlerCalled = true
		// tracers
		for i := range m.Tracers {
			m.Tracers[i].HandlerStart(m.Transition, emitter.id, method)
		}
		handler := &handlerCall{
			fn:      emitter.methods.MethodByName(e.Name),
			event:   e,
			timeout: false,
		}
		m.handlerTimer.Reset(m.HandlerTimeout)
		select {
		case <-m.Ctx.Done():
			break
		case m.handlerStart <- handler:
		}

		// wait on the result / timeout / context
		select {

		case <-m.Ctx.Done():
			break

		case <-m.handlerTimer.C:
			m.log(LogOps, "[cancel] (%s) by timeout",
				j(m.Transition.TargetStates))
			break

		case r := <-m.handlerPanic:
			// recover partial state
			m.recoverToErr(emitter, r)
			ret = false

		case ret = <-m.handlerEnd:
			m.log(LogEverything, "[handler:end] %s", e.Name)
			// ok
		}

		m.handlerTimer.Stop()
		m.currentHandler = ""

		// tracers
		for i := range m.Tracers {
			m.Tracers[i].HandlerEnd(m.Transition, emitter.id, method)
		}

		// handle negotiation
		switch {
		case strings.HasSuffix(e.Name, "State"):
		case strings.HasSuffix(e.Name, "End"):
			// returns from State and End handlers are ignored
		default:
			if !ret {
				return Canceled, handlerCalled
			}
		}
	}

	// dynamic handlers
	m.processEventChs(e)
	// state args matchers
	m.processWhenArgs(e)

	return Executed, handlerCalled
}

func (m *Machine) handlerLoop() {
	if m.PanicToException {
		// catch panics and fwd
		defer func() {
			r := recover()
			if r != nil && !m.Disposed.Load() {
				m.handlerPanic <- r
			}
		}()
	}

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

			// handler signature: FooState(e *am.Event)
			callRet := call.fn.Call([]reflect.Value{reflect.ValueOf(call.event)})
			if len(callRet) > 0 {
				ret = callRet[0].Interface().(bool)
			}

			if call.timeout || m.Disposed.Load() {
				continue
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

// processEventChs sends the event to all OnEvent() dynamic handlers.
func (m *Machine) processEventChs(e *Event) {
	m.indexEventChLock.Lock()
	defer m.indexEventChLock.Unlock()

	for _, ch := range m.indexEventCh[e.Name] {
		select {

		case ch <- e:

		case <-time.After(m.HandlerTimeout):
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

// DuringTransition checks if the machine is currently during a transition.
func (m *Machine) DuringTransition() bool {
	return m.Transition != nil
}

// Clock return the current tick for a state.
func (m *Machine) Clock(state string) uint64 {
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
	return slicesEvery(m.StateNames, states)
}

// Has1 is a shorthand for Has. It returns true if the passed state is
// registered in the machine.
func (m *Machine) Has1(state string) bool {
	return m.Has(S{state})
}

// HasStateChanged checks current active states have changed from the passed
// ones.
func (m *Machine) HasStateChanged(before S) bool {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	lenEqual := len(before) == len(m.activeStates)
	if !lenEqual || len(DiffStates(before, m.activeStates)) > 0 {
		return true
	}

	return false
}

// HasStateChangedSince checks if the machine has changed since the passed
// clocks. Returns true if at least one state has changed.
func (m *Machine) HasStateChangedSince(clocks Clocks) bool {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	for state, tick := range clocks {
		if m.clock[state] != tick {
			return true
		}
	}

	return false
}

// String returns a one line representation of the currently active states,
// with their clock values. Inactive states are omitted.
// Eg: (Foo:1 Bar:3)
func (m *Machine) String() string {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	ret := "("
	for _, state := range m.StateNames {
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
	for _, state := range m.StateNames {
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
		states = m.StateNames
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

// GetLogLevel returns the log level of the machine.
func (m *Machine) GetLogLevel() LogLevel {
	return m.logLevel
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
// VerifyStates with the names param and emit EventStructChange if successful.
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

	m.emit(EventStructChange, A{
		"states_old": old,
		"states_new": CloneStates(m.states),
	}, nil)

	return nil
}

// newEmitter creates a new emitter for Machine.
// Each emitter should be consumed by one receiver only to guarantee the
// delivery of all events.
func (m *Machine) newEmitter(name string, methods *reflect.Value) *emitter {
	e := &emitter{
		id:      name,
		methods: methods,
	}
	// TODO emitter mutex?
	m.emitters = append(m.emitters, e)

	return e
}

// AddFromEv TODO AddFromEv
// Planned.
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

// Clocks returns the map of specified clocks or all clocks if states is nil.
func (m *Machine) Clocks(states S) Clocks {
	if states == nil {
		return maps.Clone(m.clock)
	}

	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	ret := Clocks{}
	for _, state := range states {
		ret[state] = m.clock[state]
	}

	return ret
}

// Export exports the machine clock, ID and state names into a json file.
func (m *Machine) Export(filepath string) error {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	data := Export{
		ID:         m.ID,
		Clocks:     m.clock,
		StateNames: m.StateNames,
	}

	// save json file
	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)

	var t uint64
	for _, v := range m.clock {
		t += v
	}
	m.log(LogChanges, "[import] exported ID:%s and Time:%d", data.ID, t)

	return enc.Encode(data)
}

// Import imports a previously exported file via Machine.Export. Restores clock
// values and ID. It's not safe to import into a machine which has already
// produces transitions and/or has telemetry connected.
func (m *Machine) Import(filepath string) error {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	data := Export{}

	// Read the JSON file
	filePath := filepath
	file, err := os.Open(filePath)
	if err != nil {
		m.log(LogOps, "error opening file: %s", err)
		return err
	}
	defer file.Close()

	// Read the file content
	bj, err := io.ReadAll(file)
	if err != nil {
		m.log(LogOps, "error reading file: %s", err)
		return err
	}

	// Unmarshal the JSON data into the struct
	err = json.Unmarshal(bj, &data)
	if err != nil {
		m.log(LogOps, "error unmarshalling file: %s", err)
		return err
	}

	// restore active states and clocks
	var t uint64
	m.activeStates = nil
	for state, v := range data.Clocks {
		t += v
		if !slices.Contains(m.StateNames, state) {
			return fmt.Errorf("%w: %s", ErrStateUnknown, state)
		}
		if IsActiveTick(v) {
			m.activeStates = append(m.activeStates, state)
		}

		m.clock[state] = v
	}

	// restore ID and state names
	m.StateNames = data.StateNames
	m.ID = data.ID

	m.log(LogChanges, "[import] imported ID:%s and Time:%d", data.ID, t)
	return nil
}
