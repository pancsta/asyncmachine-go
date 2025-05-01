// Package machine is a nondeterministic, multi-state, clock-based, relational,
// optionally-accepting, and non-blocking state machine.
package machine // import "github.com/pancsta/asyncmachine-go/pkg/machine"

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var _ Api = &Machine{}

// Machine provides a state machine API with mutations, state schema, handlers,
// subscriptions, tracers, and helpers methods. It also holds a queue of
// mutations to execute.
type Machine struct {
	// Maximum number of mutations that can be queued. Default: 1000.
	QueueLimit int
	// HandlerTimeout defined the time for a handler to execute before it causes
	// an Exception. Default: 1s. See also Opts.HandlerTimeout.
	// Using HandlerTimeout can cause race conditions, see Event.IsValid().
	HandlerTimeout time.Duration
	// EvalTimeout is the time the machine will try to execute an eval func.
	// Like any other handler, eval func also has HandlerTimeout. Default: 1s.
	EvalTimeout time.Duration
	// If true, the machine will print all exceptions to stdout. Default: true.
	// Requires an ExceptionHandler binding and Machine.PanicToException set.
	LogStackTrace bool
	// If true, the machine will catch panic and trigger the Exception state.
	// Default: true.
	PanicToException bool
	// DisposeTimeout specifies the duration to wait for the queue to drain during
	// disposal. Default 1s.
	DisposeTimeout time.Duration

	panicCaught bool
	// If true, logs will start with machine's id (5 chars).
	// Default: true.
	logID bool
	// statesVerified assures the state names have been ordered using VerifyStates
	statesVerified atomic.Bool
	// Unique ID of this machine. Default: random.
	id string
	// ctx is the context of the machine.
	ctx context.Context
	// parentId is the id of the parent machine (if any).
	parentId string
	// disposing disabled auto states
	disposing atomic.Bool
	// disposed tells if the machine has been disposed and is no-op.
	disposed     atomic.Bool
	queueRunning atomic.Bool
	// tags are short strings describing the machine.
	tags atomic.Pointer[[]string]
	// tracers are optional tracers for telemetry integrations.
	tracers     []Tracer
	tracersLock sync.RWMutex
	// Err is the last error that occurred.
	err atomic.Pointer[error]
	// Currently executing transition (if any).
	t              atomic.Pointer[Transition]
	transitionLock sync.RWMutex
	// states is a map of state names to state definitions.
	states     Schema
	schemaVer  atomic.Int32
	statesLock sync.RWMutex
	// activeStates is a list of currently active states.
	activeStates     S
	activeStatesLock sync.RWMutex
	// queue of mutations to be executed.
	queue           []*Mutation
	queueLock       sync.RWMutex
	queueProcessing atomic.Bool
	queueLen        atomic.Int32
	// Relation resolver, used to produce target states of a transition.
	// Default: *DefaultRelationsResolver.
	resolver RelationsResolver
	// List of all the registered state names.
	stateNames         S
	handlersLock       sync.Mutex
	handlers           []*handler
	clock              Clock
	cancel             context.CancelFunc
	logLevel           atomic.Pointer[LogLevel]
	logger             atomic.Pointer[Logger]
	indexWhen          IndexWhen
	indexWhenTime      IndexWhenTime
	indexWhenArgs      IndexWhenArgs
	indexWhenArgsLock  sync.RWMutex
	indexStateCtx      IndexStateCtx
	indexWhenQueue     []whenQueueBinding
	indexWhenQueueLock sync.Mutex
	handlerStart       chan *handlerCall
	handlerEnd         chan bool
	handlerTimeout     chan struct{}
	handlerPanic       chan recoveryData
	handlerTimer       *time.Timer
	logEntriesLock     sync.Mutex
	logEntries         []*LogEntry
	logArgs            func(args A) map[string]string
	currentHandler     atomic.Value
	disposeHandlers    []HandlerDispose
	timeLast           atomic.Pointer[Time]
	// Channel closing when the machine finished disposal. Read-only.
	whenDisposed chan struct{}
	// TODO atomic
	handlerLoopRunning bool
	detectEval         bool
	// unlockDisposed means that disposal is in progress and holding the queueLock
	unlockDisposed atomic.Bool
	// breakpoints are a list of breakpoints for debugging. [][added, removed]
	breakpoints [][2]S
}

// NewCommon creates a new Machine instance with all the common options set.
func NewCommon(
	ctx context.Context, id string, stateSchema Schema, stateNames S,
	handlers any, parent Api, opts *Opts,
) (*Machine, error) {
	machOpts := &Opts{ID: id}

	if opts != nil {
		machOpts = opts
		machOpts.ID = id
	}

	if os.Getenv(EnvAmDebug) != "" {
		machOpts = OptsWithDebug(machOpts)
	} else if os.Getenv("AM_TEST_RUNNER") != "" {
		machOpts.HandlerTimeout = 1 * time.Second
	}

	if parent != nil {
		machOpts.Parent = parent
	}

	if machOpts.LogArgs == nil {
		machOpts.LogArgs = NewArgsMapper(LogArgs, 0)
	}

	mach := New(ctx, stateSchema, machOpts)
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
func New(ctx context.Context, statesStruct Schema, opts *Opts) *Machine {
	// parse relations
	parsedStates := parseSchema(statesStruct)

	m := &Machine{
		HandlerTimeout:   100 * time.Millisecond,
		EvalTimeout:      time.Second,
		LogStackTrace:    true,
		PanicToException: true,
		QueueLimit:       1000,
		DisposeTimeout:   time.Second,

		id:             randId(),
		states:         parsedStates,
		clock:          Clock{},
		handlers:       []*handler{},
		logID:          true,
		indexWhen:      IndexWhen{},
		indexWhenTime:  IndexWhenTime{},
		indexWhenArgs:  IndexWhenArgs{},
		indexStateCtx:  IndexStateCtx{},
		handlerStart:   make(chan *handlerCall),
		handlerEnd:     make(chan bool),
		handlerTimeout: make(chan struct{}),
		handlerPanic:   make(chan recoveryData),
		handlerTimer:   time.NewTimer(24 * time.Hour),
		whenDisposed:   make(chan struct{}),
	}

	m.timeLast.Store(&Time{})
	lvl := LogNothing
	m.logLevel.Store(&lvl)

	// parse opts
	// TODO extract
	opts = cloneOptions(opts)
	var parent Api
	if opts != nil {
		if opts.ID != "" {
			m.id = opts.ID
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
			m.logID = false
		}
		if opts.Resolver != nil {
			m.resolver = opts.Resolver
		}
		if opts.LogLevel != LogNothing {
			m.SetLogLevel(opts.LogLevel)
		}
		if opts.Tracers != nil {
			m.tracers = opts.Tracers
		}
		if opts.LogArgs != nil {
			m.logArgs = opts.LogArgs
		}
		if opts.QueueLimit != 0 {
			m.QueueLimit = opts.QueueLimit
		}
		if len(opts.Tags) > 0 {
			tags := slicesUniq(opts.Tags)
			m.tags.Store(&tags)
		}
		m.detectEval = opts.DetectEval
		parent = opts.Parent
		m.parentId = opts.ParentID
	}

	if os.Getenv(EnvAmDetectEval) != "" {
		m.detectEval = true
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

	// notify the resolver
	m.resolver.NewStruct()

	// init context (support nil for examples)
	if ctx == nil {
		ctx = context.TODO()
	}
	m.ctx, m.cancel = context.WithCancel(ctx)

	if parent != nil {
		m.parentId = parent.Id()

		pTracers := parent.Tracers()

		// info the tracers about this being a submachine
		for _, t := range pTracers {
			// TODO support inheritable?
			t.NewSubmachine(parent, m)
		}
	}

	// tracers
	for i := range m.tracers {
		ctxTr := m.tracers[i].MachineInit(m)
		// TODO check that ctxTr is a child of ctx
		if ctxTr != nil {
			m.ctx = ctxTr
		}
	}

	return m
}

// Dispose disposes the machine and all its emitters. You can wait for the
// completion of the disposal with `<-mach.WhenDisposed`.
func (m *Machine) Dispose() {
	// doDispose in a goroutine to avoid a deadlock when called from within a
	// handler

	go func() {
		if m.disposed.Load() {
			m.log(LogDecisions, "[Dispose] already disposed")
			return
		}
		m.queueProcessing.Store(false)
		m.unlockDisposed.Store(true)
		m.doDispose(false)
	}()
}

// IsDisposed returns true if the machine has been disposed.
func (m *Machine) IsDisposed() bool {
	return m.disposed.Load()
}

// DisposeForce disposes the machine and all its emitters, without waiting for
// the queue to drain. Will cause panics.
func (m *Machine) DisposeForce() {
	m.doDispose(true)
}

func (m *Machine) doDispose(force bool) {
	if m.disposed.Load() {
		// already disposed
		return
	}
	m.disposing.Store(true)
	m.cancel()
	if !force {
		whenIdle := m.WhenQueueEnds(context.Background())
		select {
		case <-time.After(m.DisposeTimeout):
			m.log(LogDecisions, "[doDispose] timeout waiting for queue to drain")
		case <-whenIdle:
		}
	}
	if !m.disposed.CompareAndSwap(false, true) {
		// already disposed
		return
	}

	// TODO needed?
	// time.Sleep(100 * time.Millisecond)

	m.tracersLock.RLock()
	for i := range m.tracers {
		m.tracers[i].MachineDispose(m.Id())
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
		m.handlersLock.Lock()
		defer m.handlersLock.Unlock()
	}

	m.log(LogEverything, "[end] doDispose")
	if m.Err() == nil && m.ctx.Err() != nil {
		err := m.ctx.Err()
		m.err.Store(&err)
	}
	m.handlers = nil

	m.tracers = nil
	go func() {
		time.Sleep(100 * time.Millisecond)
		closeSafe(m.handlerEnd)
		closeSafe(m.handlerPanic)
		closeSafe(m.handlerStart)
	}()

	if m.handlerTimer != nil {
		m.handlerTimer.Stop()
		// m.handlerTimer = nil
	}

	// release the queue lock
	if m.unlockDisposed.Load() {
		m.unlockDisposed.Store(false)
		m.queueProcessing.Store(false)
	}

	// run doDispose handlers
	// TODO timeouts?
	for _, fn := range m.disposeHandlers {
		fn(m.id, m.ctx)
	}
	// TODO disposeHandlers refs to other machines
	// m.disposeHandlers = nil

	// the end
	closeSafe(m.whenDisposed)
}

func (m *Machine) getHandlers(unsafe bool) []*handler {
	if !unsafe {
		m.handlersLock.Lock()
		defer m.handlersLock.Unlock()
	}

	return slices.Clone(m.handlers)
}

func (m *Machine) setHandlers(unsafe bool, handlers []*handler) {
	if !unsafe {
		m.handlersLock.Lock()
		defer m.handlersLock.Unlock()
	}

	m.handlers = handlers
}

// WhenErr returns a channel that will be closed when the machine is in the
// Exception state.
//
// ctx: optional context that will close the channel when handlerLoopDone.
func (m *Machine) WhenErr(disposeCtx context.Context) <-chan struct{} {
	return m.When([]string{Exception}, disposeCtx)
}

// When returns a channel that will be closed when all the passed states
// become active or the machine gets disposed.
//
// ctx: optional context that will close the channel when handlerLoopDone.
func (m *Machine) When(states S, ctx context.Context) <-chan struct{} {
	// TODO re-use channels with the same state set and context
	ch := make(chan struct{})
	if m.disposed.Load() {
		close(ch)
		return ch
	}

	// lock
	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()

	// if all active, close early
	if m.is(states) {
		// TODO decision msg
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
	m.log(LogOps, "[when:new] %s", j(states))

	// doDispose with context
	disposeWithCtx(m, ctx, ch, states, binding, &m.activeStatesLock, m.indexWhen,
		fmt.Sprintf("[when:match] %s", j(states)))

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
// ctx: optional context that will close the channel when handlerLoopDone.
func (m *Machine) WhenNot(states S, ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	if m.disposed.Load() {
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
	m.log(LogOps, "[whenNot:new] %s", j(states))

	// doDispose with context
	disposeWithCtx(m, ctx, ch, states, binding, &m.activeStatesLock, m.indexWhen,
		fmt.Sprintf("[whenNot:match] %s", j(states)))

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
// a specific call.
//
// ctx: optional context that will close the channel when handler loop ends.
func (m *Machine) WhenArgs(
	state string, args A, ctx context.Context,
) <-chan struct{} {
	// TODO better val comparisons
	//  support regexp for strings

	ch := make(chan struct{})
	if m.disposed.Load() {
		close(ch)
		return ch
	}

	m.MustParseStates(S{state})
	name := state + SuffixState

	// locks
	m.indexWhenArgsLock.Lock()
	defer m.indexWhenArgsLock.Unlock()

	// log TODO pass through logArgs?
	argNames := jw(slices.AppendSeq(S{}, maps.Keys(args)), ",")
	m.log(LogOps, "[whenArgs:new] %s (%s)", state, argNames)

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

	// doDispose with context
	disposeWithCtx(m, ctx, ch, S{name}, binding, &m.indexWhenArgsLock,
		m.indexWhenArgs, fmt.Sprintf("[whenArgs:match] %s (%s)", state, argNames))

	// insert the binding
	if _, ok := m.indexWhenArgs[name]; !ok {
		m.indexWhenArgs[name] = []*WhenArgsBinding{binding}
	} else {
		m.indexWhenArgs[name] = append(m.indexWhenArgs[name], binding)
	}

	return ch
}

// WhenTime returns a channel that will be closed when all the passed states
// have passed the specified time. The time is a logical clock of the state.
// Machine time can be sourced from [Machine.Time](), or [Machine.Clock]().
//
// ctx: optional context that will close the channel when handlerLoopDone.
func (m *Machine) WhenTime(
	states S, times Time, ctx context.Context,
) <-chan struct{} {
	ch := make(chan struct{})
	if m.disposed.Load() {
		close(ch)
		return ch
	}
	valid := len(states) == len(times)
	m.MustParseStates(states)
	indexWhenTime := m.indexWhenTime

	// close early
	if !valid {
		close(ch)
		err := fmt.Errorf(
			"whenTime: states and times must have the same length (%s)", j(states))
		m.AddErr(err, nil)

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
		// TODO decision msg
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
	m.log(LogOps, "[whenTime:new] %s %s", jw(states, ","), times)

	// doDispose with context
	logMsg := fmt.Sprintf("[whenTime:match] %s %s", jw(states, ","), times)
	disposeWithCtx(m, ctx, ch, states, binding, &m.activeStatesLock,
		m.indexWhenTime, logMsg)

	// insert the binding
	for _, s := range states {
		indexWhenTime[s] = append(indexWhenTime[s], binding)
	}

	return ch
}

// WhenTime1 waits till ticks for a single state equal the given value (or
// more).
//
// ctx: optional context that will close the channel when handlerLoopDone.
func (m *Machine) WhenTime1(
	state string, ticks uint64, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(S{state}, Time{ticks}, ctx)
}

// WhenTicks waits N ticks of a single state (relative to now). Uses WhenTime
// underneath.
//
// ctx: optional context that will close the channel when handlerLoopDone.
func (m *Machine) WhenTicks(
	state string, ticks int, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(S{state}, Time{uint64(ticks) + m.Tick(state)}, ctx)
}

// WhenQueueEnds closes every time the queue ends, or the optional ctx expires.
//
// ctx: optional context that will close the channel when handlerLoopDone.
func (m *Machine) WhenQueueEnds(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	if m.disposed.Load() {
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

	// doDispose with context
	// TODO extract
	if ctx != nil {
		go func() {
			select {
			case <-ch:
				return
			case <-m.ctx.Done():
				return
			case <-ctx.Done():
			}
			// GC only if needed
			if m.disposed.Load() {
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
// disposed. Requires bound handlers. Use Machine.disposed in case no handlers
// have been bound.
func (m *Machine) WhenDisposed() <-chan struct{} {
	return m.whenDisposed
}

// Time returns machine's time, a list of ticks per state. Returned value
// includes the specified states, or all the states if nil.
func (m *Machine) Time(states S) Time {
	if m.disposed.Load() {
		return nil
	}
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	return m.time(states)
}

func (m *Machine) time(states S) Time {
	if m.disposed.Load() {
		return nil
	}
	if states == nil {
		states = m.stateNames
	}

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
func (m *Machine) TimeSum(states S) uint64 {
	// TODO handle overflow

	if m.disposed.Load() {
		return 0
	}
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
	if m.disposed.Load() || m.disposing.Load() {
		return Canceled
	}

	// let Exception in even with a full queue, but only once
	if int(m.queueLen.Load()) >= m.QueueLimit {
		if !slices.Contains(states, Exception) || m.IsErr() {
			return Canceled
		}
	}

	m.queueMutation(MutationAdd, states, args, nil)
	m.breakpoint(states, nil)

	return m.processQueue()
}

// Add1 is a shorthand method to add a single state with the passed args.
func (m *Machine) Add1(state string, args A) Result {
	return m.Add(S{state}, args)
}

// Toggle deactivates a list of states in case all are active, or activates
// all otherwise. Returns the result of the transition (Executed, Queued,
// Canceled).
func (m *Machine) Toggle(states S, args A) Result {
	if m.disposed.Load() {
		return Canceled
	}
	if m.Is(states) {
		return m.Remove(states, args)
	} else {
		return m.Add(states, args)
	}
}

// Toggle1 activates or deactivates a single state, depending on its current
// state. Returns the result of the transition (Executed, Queued, Canceled).
func (m *Machine) Toggle1(state string, args A) Result {
	if m.disposed.Load() {
		return Canceled
	}
	if m.Is1(state) {
		return m.Remove1(state, args)
	} else {
		return m.Add1(state, args)
	}
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
	if m.disposed.Load() || m.disposing.Load() {
		return Canceled
	}
	// TODO test Err()
	m.err.Store(&err)

	var trace string
	if m.LogStackTrace {
		trace = captureStackTrace()
	}

	// build args
	argsT := &AT{
		Err:      err,
		ErrTrace: trace,
	}

	// TODO prepend to the queue? what effects / benefits
	return m.Add(S{state, Exception}, PassMerge(args, argsT))
}

// PanicToErr will catch a panic and add the Exception state. Needs to
// be called in a defer statement, just like a recover() call.
func (m *Machine) PanicToErr(args A) {
	if !m.PanicToException || m.disposed.Load() {
		return
	}

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
	if !m.PanicToException || m.disposed.Load() || m.disposing.Load() {
		return
	}
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

	return *err
}

// Remove deactivates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) Remove(states S, args A) Result {
	if m.disposed.Load() || m.disposing.Load() {
		return Canceled
	}

	// let Exception in even with a full queue, but only once
	if int(m.queueLen.Load()) >= m.QueueLimit {
		if !slices.Contains(states, Exception) || !m.IsErr() {
			return Canceled
		}
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
	m.queueMutation(MutationRemove, states, args, nil)
	m.breakpoint(nil, states)

	return m.processQueue()
}

// Remove1 is a shorthand method to remove a single state with the passed args.
// See Remove().
func (m *Machine) Remove1(state string, args A) Result {
	return m.Remove(S{state}, args)
}

// Set deactivates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) Set(states S, args A) Result {
	if m.disposed.Load() || int(m.queueLen.Load()) >= m.QueueLimit {
		return Canceled
	}
	m.queueMutation(MutationSet, states, args, nil)

	return m.processQueue()
}

// TODO Set1Cond(name, args, cond bool)

// Id returns the machine's id.
func (m *Machine) Id() string {
	return m.id
}

// ParentId returns the ID of the parent machine (if any).
func (m *Machine) ParentId() string {
	return m.parentId
}

// Tags returns machine's tags, a list of unstructured strings without spaces.
func (m *Machine) Tags() []string {
	tags := m.tags.Load()
	if tags != nil {
		return slices.Clone(*tags)
	}
	return nil
}

// Tags returns machine's tags, a list of unstructured strings without spaces.
func (m *Machine) SetTags(tags []string) {
	m.tags.Store(&tags)
}

// Is checks if all the passed states are currently active.
//
//	machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
//	machine.Add(S{"Foo"})
//	machine.Is(S{"Foo"}) // true
//	machine.Is(S{"Foo", "Bar"}) // false
func (m *Machine) Is(states S) bool {
	if m.disposed.Load() {
		return false
	}
	// TODO optimize? lock unnecessary?
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
	if m.disposed.Load() {
		return false
	}
	activeStates := m.activeStates

	for _, s := range states {
		if !slices.Contains(m.stateNames, s) {
			m.log(LogDecisions, "[is] state %s not found", s)
			return false
		}
		if !slices.Contains(activeStates, s) {
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
	if m.disposed.Load() {
		return false
	}
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
func (m *Machine) queueMutation(
	mutationType MutationType, states S, args A, event *Event,
) {
	if m.disposed.Load() {
		return
	}
	statesParsed := m.MustParseStates(states)
	multi := false
	for _, state := range statesParsed {
		if m.states[state].Multi {
			multi = true
			break
		}
	}

	// Detect duplicates and avoid queueing them, but not for multi states.
	if !multi && len(args) == 0 &&
		m.detectQueueDuplicates(mutationType, statesParsed) {
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
	var source *MutSource
	if event != nil {
		tx := event.Transition()
		source = &MutSource{
			MachId: event.MachineId,
			TxId:   event.TransitionId,
		}
		if tx != nil {
			source.MachTime = tx.TimeBefore.Sum()
		}
	}
	m.queue = append(m.queue, &Mutation{
		Type:         mutationType,
		CalledStates: statesParsed,
		Args:         args,
		Auto:         false,
		Source:       source,
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
// Note: usage of Eval is discouraged. But if you have to, use AM_DETECT_EVAL in
// tests for deadlock detection. Most usages of eval can be replaced with
// atomics or returning from mutation via channels.
func (m *Machine) Eval(source string, fn func(), ctx context.Context) bool {
	if m.disposed.Load() {
		return false
	}
	if source == "" {
		panic("error: source of eval is required")
	}
	if m.detectEval {
		// check every method of every handler against the stack trace
		trace := captureStackTrace()

		for i := 0; !m.disposed.Load() && i < len(m.handlers); i++ {
			handler := m.handlers[i]

			for _, method := range handler.methodNames {
				// skip " in goroutine N" entries
				match := fmt.Sprintf(".(*%s).%s(", handler.name, method)

				for _, line := range strings.Split(trace, "\n") {
					if !strings.Contains(line, match) {
						continue
					}
					msg := fmt.Sprintf("error: Eval() called directly in handler %s.%s",
						handler.name, method)
					panic(msg)
				}
			}

		}
	}
	m.log(LogOps, "[eval] %s", source)

	// wrap the func with a handlerLoopDone channel
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

	m.queueLock.Lock()

	// prepend to the queue
	m.queue = append([]*Mutation{{
		Type: mutationEval,
		eval: wrap,
		ctx:  ctx,
	}}, m.queue...)
	m.queueLen.Store(int32(len(m.queue)))
	m.queueLock.Unlock()

	// process the queue if needed, but ignore the result
	m.processQueue()

	// wait with a timeout
	select {

	case <-time.After(m.EvalTimeout):
		canceled.Store(true)
		m.log(LogOps, "[eval:timeout] %s", source)
		m.AddErr(fmt.Errorf("%w: eval:%s", ErrEvalTimeout, source), nil)
		return false

	case <-m.ctx.Done():
		canceled.Store(true)
		return false

	case <-ctx.Done():
		canceled.Store(true)
		m.log(LogDecisions, "[eval:ctxdone] %s", source)
		return false

	case <-done:
	}

	m.log(LogEverything, "[eval:end] %s", source)
	return true
}

// NewStateCtx returns a new sub-context, bound to the current clock's tick of
// the passed state.
//
// Context cancels when the state has been deactivated, or right away,
// if it isn't currently active.
//
// State contexts are used to check state expirations and should be checked
// often inside goroutines.
func (m *Machine) NewStateCtx(state string) context.Context {
	if m.disposed.Load() {
		return context.TODO()
	}
	// TODO handle cancelation while parsing the queue
	m.MustParseStates(S{state})
	m.activeStatesLock.Lock()
	defer m.activeStatesLock.Unlock()

	if _, ok := m.indexStateCtx[state]; ok {
		return m.indexStateCtx[state].Ctx
	}

	v := CtxValue{
		Id:    m.id,
		State: state,
		Tick:  m.clock[state],
	}
	stateCtx, cancel := context.WithCancel(context.WithValue(m.ctx, CtxKey, v))

	// cancel early
	if !m.is(S{state}) {
		// TODO decision msg
		cancel()
		return stateCtx
	}

	binding := &CtxBinding{
		Ctx:    stateCtx,
		Cancel: cancel,
	}

	// add an index
	m.indexStateCtx[state] = binding
	m.log(LogOps, "[ctx:new] %s", state)

	return stateCtx
}

// TODO NewTransitionCtx...
// func (m *Machine) NewTransitionCtx(state string, ev Event) context.Context {
//
// }

// MustBindHandlers is a panicking version of BindHandlers, useful in tests.
func (m *Machine) MustBindHandlers(handlers any) {
	if err := m.BindHandlers(handlers); err != nil {
		panic(err)
	}
}

// TODO move
var emitterNameRe = regexp.MustCompile(`/\w+\.go:\d+`)

// BindHandlers binds a struct of handler methods to machine's states, based on
// the naming convention, eg `FooState(e *Event)`. Negotiation handlers can
// optional return bool.
func (m *Machine) BindHandlers(handlers any) error {
	if m.disposed.Load() {
		return nil
	}
	if !m.handlerLoopRunning {
		m.handlerLoopRunning = true

		// start the handler loop
		go m.handlerLoop()
	}

	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindHandlers expects a pointer to a struct")
	}

	// extract the name
	name := reflect.TypeOf(handlers).Elem().Name()
	if name == "" {
		name = "anon"
		if os.Getenv(EnvAmDebug) != "" {
			buf := make([]byte, 4024)
			n := runtime.Stack(buf, false)
			stack := string(buf[:n])
			lines := strings.Split(stack, "\n")
			name = lines[len(lines)-2]
			name = strings.TrimLeft(emitterNameRe.FindString(name), "/")
		}
	}

	// detect methods
	var methodNames []string
	if m.detectEval || os.Getenv(EnvAmDebug) != "" {
		var err error
		methodNames, err = ListHandlers(handlers, m.stateNames)
		if err != nil {
			return fmt.Errorf("error listing handlers: %w", err)
		}
	}

	h := m.newHandler(handlers, name, &v, methodNames)
	old := m.getHandlers(false)
	m.setHandlers(false, append(old, h))
	if name != "" {
		m.log(LogOps, "[handlers] bind %s", name)
	} else {
		// index for anon handlers
		m.log(LogOps, "[handlers] bind %d", len(old))
	}

	return nil
}

// DetachHandlers detaches previously bound machine handlers.
func (m *Machine) DetachHandlers(handlers any) error {
	if m.disposing.Load() {
		return nil
	}

	m.handlersLock.Lock()
	defer m.handlersLock.Unlock()

	old := m.getHandlers(true)
	var match *handler
	var matchIndex int
	for i, h := range old {
		if h.h == handlers {
			match = h
			matchIndex = i
			break
		}
	}

	if match == nil {
		return errors.New("handlers not bound")
	}

	m.setHandlers(true, slices.Delete(old, matchIndex, matchIndex+1))
	match.dispose()
	m.log(LogOps, "[handlers] detach %s", match.name)

	return nil
}

// newHandler creates a new handler for Machine.
// Each handler should be consumed by one receiver only to guarantee the
// delivery of all events.
func (m *Machine) newHandler(
	handlers any, name string, methods *reflect.Value, methodNames []string,
) *handler {
	if m.disposed.Load() {
		return &handler{}
	}
	e := &handler{
		name:         name,
		h:            handlers,
		methods:      methods,
		methodNames:  methodNames,
		methodCache:  make(map[string]reflect.Value),
		missingCache: make(map[string]struct{}),
	}

	return e
}

// recoverToErr recovers to the Exception state by catching panics.
func (m *Machine) recoverToErr(handler *handler, r recoveryData) {
	if m.disposed.Load() {
		return
	}

	m.panicCaught = true
	m.currentHandler.Store("")
	t := m.t.Load()

	// dont double handle an exception (no nesting)
	if slices.Contains(t.Mutation.CalledStates, Exception) {
		return
	}

	m.log(LogOps, "[recover] handling panic...")
	err := fmt.Errorf("%s", r.err)
	m.err.Store(&err)

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

			// TODO use fromState
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

	m.log(LogOps, "[cancel] (%s) by recover", j(t.TargetStates()))
	if t.Mutation == nil {
		// TODO can this even happen?
		panic(fmt.Sprintf("no mutation panic in %s: %s", handler.name, err))
	}

	// negotiation phase, simply cancel and...
	// prepend add:Exception to the beginning of the queue
	exception := &Mutation{
		Type:         MutationAdd,
		CalledStates: S{Exception},
		Args: Pass(&AT{
			Err:      err,
			ErrTrace: r.stack,
			Panic: &ExceptionArgsPanic{
				CalledStates: t.Mutation.CalledStates,
				StatesBefore: t.StatesBefore(),
				Transition:   t,
				LastStep:     t.latestStep,
			},
		}),
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
	if m.disposed.Load() {
		return nil
	}
	// check if all states are defined in m.Struct
	for _, s := range states {
		if _, ok := m.states[s]; !ok {
			panic(fmt.Errorf(
				"%w: %s not defined in struct for %s", ErrStateMissing, s, m.id))
		}
	}

	return slicesUniq(states)
}

// VerifyStates verifies an array of state names and returns an error in case
// at least one isn't defined. It also retains the order and uses it for
// StateNames. Verification can be checked via Machine.StatesVerified. Not
// thread-safe.
func (m *Machine) VerifyStates(states S) error {
	if m.disposed.Load() {
		return nil
	}
	var errs []error
	var checked []string

	for _, s := range states {

		if _, ok := m.states[s]; !ok {
			errs = append(errs, fmt.Errorf("state %s is not defined for %s", s, m.id))
			continue
		}

		checked = append(checked, s)
	}

	if len(errs) > 1 {
		return errors.Join(errs...)
	} else if len(errs) == 1 {
		return errs[0]
	}

	if len(m.stateNames) > len(states) {
		missing := DiffStates(m.stateNames, checked)
		return fmt.Errorf(
			"error: trying to verify more states than registered: %s", j(missing))
	}

	// memorize the state names order
	m.stateNames = slices.Clone(states)
	m.statesVerified.Store(true)

	// tracers
	m.tracersLock.RLock()
	for i := 0; !m.disposed.Load() && i < len(m.tracers); i++ {
		m.tracers[i].VerifyStates(m)
	}
	m.tracersLock.RUnlock()

	return nil
}

// StatesVerified returns true if the state names have been ordered
// using VerifyStates.
func (m *Machine) StatesVerified() bool {
	return m.statesVerified.Load()
}

// setActiveStates sets the new active states incrementing the counters and
// returning the previously active states.
func (m *Machine) setActiveStates(calledStates S, targetStates S,
	isAuto bool,
) S {
	if m.disposed.Load() {
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

	// tick deactivated states by +1
	for _, state := range removedStates {
		m.clock[state]++
	}

	// construct a logging msg
	if m.GetLogLevel() > LogNothing {
		logMsg := ""
		if len(newStates) > 0 {
			logMsg += " +" + strings.Join(newStates, " +")
		}
		if len(removedStates) > 0 {
			logMsg += " -" + strings.Join(removedStates, " -")
		}
		if len(noChangeStates) > 0 && m.GetLogLevel() > LogDecisions {
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

// AddBreakpoint adds a breakpoint for an outcome of mutation (added and
// removed states). Once such mutation happens, a log message will be printed
// out. You can set an IDE's breakpoint on this line and see the mutation's sync
// stack trace. When Machine.LogStackTrace is set, the stack trace will be
// printed out as well. Many breakpoints can be added, but none removed.
func (m *Machine) AddBreakpoint(added S, removed S) {
	m.breakpoints = append(m.breakpoints, [2]S{added, removed})
}

func (m *Machine) breakpoint(added S, removed S) {
	found := false
	for _, bp := range m.breakpoints {

		// check if the breakpoint matches
		if len(added) > 0 && !slices.Equal(bp[0], added) {
			continue
		}
		if len(removed) > 0 && !slices.Equal(bp[1], removed) {
			continue
		}
		found = true
	}

	if !found {
		return
	}

	if m.LogStackTrace {
		m.log(LogChanges, "[breakpoint] Machine.breakpoint\n%s",
			captureStackTrace())
	} else {
		m.log(LogChanges, "[breakpoint] Machine.breakpoint")
	}
}

// processQueue processes the queue of mutations. It's the main loop of the
// machine.
func (m *Machine) processQueue() Result {
	// empty queue
	if m.queueLen.Load() == 0 || m.disposed.Load() {
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

		if m.disposed.Load() {
			return Canceled
		}

		// shift the queue
		m.queueLock.Lock()
		item := m.queue[0]
		m.queue = m.queue[1:]
		m.queueLen.Store(int32(len(m.queue)))
		m.queueLock.Unlock()

		// support for context cancelation
		if item.ctx != nil && item.ctx.Err() != nil {
			ret = append(ret, Executed)
			continue
		}

		// special case for Eval mutations
		if item.Type == mutationEval {
			item.eval()
			continue
		}
		t := newTransition(m, item)

		// execute the transition
		ret = append(ret, t.emitEvents())
		m.timeLast.Store(&t.TimeAfter)

		// process flow methods
		m.processWhenBindings(t)
		m.processWhenTimeBindings(t)
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
	for i := 0; !m.disposed.Load() && i < len(m.tracers); i++ {
		m.tracers[i].QueueEnd(m)
	}
	m.tracersLock.RUnlock()
	m.processWhenQueueBindings()

	if len(ret) == 0 {
		return Canceled
	}
	return ret[0]
}

func (m *Machine) processStateCtxBindings() {
	if m.disposed.Load() {
		return
	}
	m.activeStatesLock.RLock()
	deactivated := DiffStates(m.t.Load().StatesBefore(), m.activeStates)

	var toCancel []context.CancelFunc
	for _, s := range deactivated {
		if _, ok := m.indexStateCtx[s]; !ok {
			continue
		}

		toCancel = append(toCancel, m.indexStateCtx[s].Cancel)
		m.log(LogOps, "[ctx:match] %s", s)
		delete(m.indexStateCtx, s)
	}

	m.activeStatesLock.RUnlock()

	// cancel all the state contexts outside the critical section
	for _, cancel := range toCancel {
		cancel()
	}
}

func (m *Machine) processWhenBindings(t *Transition) {
	if m.disposed.Load() {
		return
	}
	m.activeStatesLock.Lock()

	// calculate activated and deactivated states
	activated := DiffStates(m.activeStates, t.StatesBefore())
	deactivated := DiffStates(t.StatesBefore(), m.activeStates)

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

			if binding.Negation {
				m.log(LogOps, "[whenNot:match] %s", j(names))
			} else {
				m.log(LogOps, "[when:match] %s", j(names))
			}
			// close outside the critical section
			toClose = append(toClose, binding.Ch)
		}
	}
	m.activeStatesLock.Unlock()

	// notify outside the critical section
	for ch := range toClose {
		closeSafe(toClose[ch])
	}
}

func (m *Machine) processWhenTimeBindings(t *Transition) {
	if m.disposed.Load() {
		return
	}
	m.activeStatesLock.Lock()
	var toClose []chan struct{}

	// collect all the ticked states
	allTicked := S{}
	for state, t := range t.ClockBefore() {
		// if changed, collect to check
		if m.clock[state] != t {
			allTicked = append(allTicked, state)
		}
	}

	// check all the bindings for all the ticked states
	for _, s := range allTicked {
		for _, binding := range m.indexWhenTime[s] {

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
				if len(m.indexWhenTime[state]) == 1 {
					delete(m.indexWhenTime, state)
					continue
				}

				// delete with a lookup TODO optimizes
				m.indexWhenTime[state] = slicesWithout(m.indexWhenTime[state], binding)
			}

			m.log(LogOps, "[whenTime:match] %s %d", j(names), binding.Times)
			// close outside the critical section
			toClose = append(toClose, binding.Ch)
		}
	}
	m.activeStatesLock.Unlock()

	// notify outside the critical section
	for ch := range toClose {
		closeSafe(toClose[ch])
	}
}

func (m *Machine) processWhenQueueBindings() {
	if m.disposed.Load() {
		return
	}
	m.indexWhenQueueLock.Lock()
	toPush := slices.Clone(m.indexWhenQueue)
	m.indexWhenQueue = nil
	m.indexWhenQueueLock.Unlock()

	for _, binding := range toPush {
		closeSafe(binding.ch)
	}
}

func (m *Machine) processWhenArgs(e *Event) {
	if m.disposed.Load() {
		return
	}
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

		argNames := jw(slices.AppendSeq(S{}, maps.Keys(binding.args)), ",")
		// FooState -> Foo
		name := e.Name[0 : len(e.Name)-len(SuffixState)]
		m.log(LogOps, "[whenArgs:match] %s (%s)", name, argNames)
		// args match - doDispose and close outside the mutex
		chToClose = append(chToClose, binding.ch)

		// GC
		if len(m.indexWhenArgs[e.Name]) == 1 {
			delete(m.indexWhenArgs, e.Name)
		} else {
			// TODO optimize
			m.indexWhenArgs[e.Name] = slicesWithout(m.indexWhenArgs[e.Name], binding)
		}
	}
	m.indexWhenArgsLock.Unlock()

	for _, ch := range chToClose {
		closeSafe(ch)
	}
}

// SetLogArgs accepts a function which decides which mutation arguments to log.
// See NewArgsMapper or create your own manually.
func (m *Machine) SetLogArgs(mapper LogArgsMapper) {
	m.logArgs = mapper
}

// GetLogArgs returns the current log args function.
func (m *Machine) GetLogArgs() LogArgsMapper {
	return m.logArgs
}

// SetLogId enables or disables the logging of the machine's ID in log messages.
func (m *Machine) SetLogId(val bool) {
	m.logID = val
}

// GetLogId returns the current state of the log id setting.
func (m *Machine) GetLogId() bool {
	return m.logID
}

// Ctx return machine's root context.
func (m *Machine) Ctx() context.Context {
	return m.ctx
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
	if m.disposed.Load() {
		return
	}
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

// LogLvl adds an internal log entry from the outside. It should be used only
// by packages extending pkg/machine. Use Log instead.
func (m *Machine) LogLvl(lvl LogLevel, msg string, args ...any) {
	// TODO refac: SysLog
	if m.disposed.Load() {
		return
	}

	// single lines only
	msg = strings.ReplaceAll(msg, "\n", " ")

	m.log(lvl, msg, args...)
}

// log logs a message if the log level is high enough.
// Optionally redirects to a custom logger from SetLogger.
func (m *Machine) log(level LogLevel, msg string, args ...any) {
	if level > m.GetLogLevel() || m.disposed.Load() {
		return
	}

	if m.logID {
		id := m.id
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
		t.LogEntriesLock.Lock()
		defer t.LogEntriesLock.Unlock()
		t.LogEntries = append(t.LogEntries, &LogEntry{level, out})

	} else {
		// append the log msg the machine and collect at the end of the next
		// transition
		m.logEntriesLock.Lock()
		defer m.logEntriesLock.Unlock()

		// prevent dups
		if len(m.logEntries) > 0 && m.logEntries[len(m.logEntries)-1].Text == out {
			return
		}

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
	m.logLevel.Store(&level)
}

// SetLoggerEmpty creates an empty logger that does nothing and sets the log
// level in one call. Useful when combined with am-dbg. Requires LogChanges log
// level to produce any output.
func (m *Machine) SetLoggerEmpty(level LogLevel) {
	var logger Logger = func(_ LogLevel, msg string, args ...any) {
		// no-op
	}
	m.logger.Store(&logger)
	m.logLevel.Store(&level)
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
	// TODO should return `Logger` not `*Logger`?
	return m.logger.Load()
}

// SetLogLevel sets the log level of the machine.
func (m *Machine) SetLogLevel(level LogLevel) {
	m.logLevel.Store(&level)
}

// GetLogLevel returns the log level of the machine.
func (m *Machine) GetLogLevel() LogLevel {
	// TODO refac: LogLevel()
	return *m.logLevel.Load()
}

// handle triggers methods on handlers structs.
// locked: transition lock currently held
func (m *Machine) handle(
	name string, args A, step *Step, locked bool,
) (Result, bool) {
	if m.disposing.Load() {
		return Canceled, false
	}
	e := &Event{
		Name:         name,
		machine:      m,
		Args:         args,
		TransitionId: m.t.Load().ID,
		MachineId:    m.Id(),
		step:         step,
	}

	// always init args
	if e.Args == nil {
		e.Args = A{}
	}

	// queue-end lacks a transition
	targetStates := "---"
	if !locked {
		m.transitionLock.RLock()
	}
	t := m.t.Load()
	if t != nil {
		targetStates = j(t.TargetStates())
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
	if m.disposing.Load() {
		return Canceled, false
	}

	handlerCalled := false
	handlers := m.getHandlers(false)
	for i := 0; !m.disposing.Load() && i < len(handlers); i++ {
		// internal event
		if e.step == nil {
			break
		}

		h := handlers[i]
		h.mx.Lock()
		methodName := e.Name
		// TODO descriptive name
		handlerName := strconv.Itoa(i) + ":" + h.name

		if m.GetLogLevel() >= LogEverything {
			emitterID := truncateStr(handlerName, 15)
			emitterID = padString(strings.ReplaceAll(emitterID, " ", "_"), 15, "_")
			m.log(LogEverything, "[handle:%-15s] %s", emitterID, methodName)
		}

		// cache
		_, ok := h.missingCache[methodName]
		if ok {
			h.mx.Unlock()
			continue
		}
		method, ok := h.methodCache[methodName]
		if !ok {
			method = h.methods.MethodByName(methodName)

			// support field handlers
			if !method.IsValid() {
				method = h.methods.Elem().FieldByName(methodName)
			}
			if !method.IsValid() {
				h.missingCache[methodName] = struct{}{}
				h.mx.Unlock()
				continue
			}
			h.methodCache[methodName] = method
		}
		h.mx.Unlock()

		// call the handler
		m.log(LogOps, "[handler:%d] %s", i, methodName)
		m.currentHandler.Store(methodName)
		var ret bool
		var timeout bool
		handlerCalled = true

		// tracers
		m.tracersLock.RLock()
		tx := m.t.Load()
		for i := range m.tracers {
			m.tracers[i].HandlerStart(tx, handlerName, methodName)
		}
		m.tracersLock.RUnlock()
		handlerCall := &handlerCall{
			fn:      method,
			name:    methodName,
			event:   e,
			timeout: false,
		}

		select {
		case <-m.ctx.Done():
			break
		case m.handlerStart <- handlerCall:
		}

		// reuse the timer each time
		m.handlerTimer.Reset(m.HandlerTimeout)

		// wait on the result / timeout / context
		select {

		case <-m.ctx.Done():

		case <-m.handlerTimer.C:
			// notify the handler loop
			// TODO wait for [Event.AcceptTimeout]
			m.handlerTimeout <- struct{}{}
			m.log(LogOps, "[cancel] (%s) by timeout", j(tx.TargetStates()))
			err := fmt.Errorf("%w: %s", ErrHandlerTimeout, methodName)
			m.EvAddErr(e, err, Pass(&AT{
				TargetStates: tx.TargetStates(),
				CalledStates: tx.CalledStates(),
				TimeBefore:   tx.TimeBefore,
				TimeAfter:    tx.TimeAfter,
				Event:        e.Clone(),
			}))
			timeout = true

		case r := <-m.handlerPanic:
			// recover partial state
			// TODO pass tx info via &AT{}
			m.recoverToErr(h, r)

		case ret = <-m.handlerEnd:
			m.log(LogEverything, "[handler:end] %s", e.Name)
			// ok
		}

		m.handlerTimer.Stop()
		m.currentHandler.Store("")

		// tracers
		m.tracersLock.RLock()
		for i := range m.tracers {
			m.tracers[i].HandlerEnd(tx, handlerName, methodName)
		}
		m.tracersLock.RUnlock()

		// handle negotiation
		switch {
		case timeout:

			return Canceled, handlerCalled
		case strings.HasSuffix(e.Name, SuffixState):
		case strings.HasSuffix(e.Name, SuffixEnd):
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
	catch := func() {
		err := recover()
		if err == nil {
			return
		}

		if !m.disposed.Load() {
			m.handlerPanic <- recoveryData{
				err:   err,
				stack: captureStackTrace(),
			}
		}
	}

	// catch panics and fwd
	if m.PanicToException {
		defer catch()
	}

	// wait for a handler call or context
	for {
		select {

		case <-m.ctx.Done():
			m.handlerLoopDone()
			return

		case call, ok := <-m.handlerStart:
			if !ok {
				return
			}
			ret := true
			endCh := make(chan struct{})
			timeout := atomic.Bool{}

			// fork for timeout
			go func() {
				// confirm the event is still valid
				if !call.event.IsValid() {
					close(endCh)
					return
				}
				// check timeout
				if timeout.Load() {
					close(endCh)
					return
				}

				// catch panics and fwd
				if m.PanicToException {
					defer catch()
				}

				// handler signature: FooState(e *am.Event)
				// TODO optimize https://github.com/golang/go/issues/7818
				callRet := call.fn.Call([]reflect.Value{reflect.ValueOf(call.event)})
				if len(callRet) > 0 {
					ret = callRet[0].Interface().(bool)
				}
				close(endCh)
			}()

			// wait for result / timeout / context
			select {
			case <-m.ctx.Done():
				m.handlerLoopDone()
				return
			case <-m.handlerTimeout:
				timeout.Store(true)
				continue
			case <-endCh:
				// pass
			}

			// needed to avoid racing
			if m.ctx.Err() != nil {
				m.handlerLoopDone()
				return
			}

			// pass the result to handlerLoop
			select {
			case <-m.ctx.Done():
				m.handlerLoopDone()
				return

			case m.handlerEnd <- ret:
				// pass
			}
		}
	}
}

func (m *Machine) handlerLoopDone() {
	// doDispose with context
	v, _ := m.ctx.Value(CtxKey).(CtxValue)
	m.log(LogOps, "[doDispose] ctx handlerLoopDone %v", v)
	m.Dispose()
}

// detectQueueDuplicates checks for duplicated mutations
// 1. Check if a mutation is scheduled (without params)
// 2. Check if a counter mutation isn't scheduled later (any params)
func (m *Machine) detectQueueDuplicates(mutationType MutationType,
	parsed S,
) bool {
	if m.disposed.Load() {
		return false
	}
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
	if m.disposed.Load() {
		return Clock{}
	}
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
	if m.disposed.Load() {
		return 0
	}
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
func (m *Machine) IsQueued(mutationType MutationType, states S,
	withoutArgsOnly bool, statesStrictEqual bool, startIndex int,
) int {
	if m.disposed.Load() {
		return -1
	}
	// TODO test
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

// WillBe returns true if the passed states are scheduled to be activated.
// See IsQueued to perform more detailed queries.
func (m *Machine) WillBe(states S) bool {
	// TODO test
	return -1 != m.IsQueued(MutationAdd, states, false, false, 0)
}

// WillBe1 returns true if the passed state is scheduled to be activated.
// See IsQueued to perform more detailed queries.
func (m *Machine) WillBe1(state string) bool {
	// TODO test
	return m.WillBe(S{state})
}

// WillBeRemoved returns true if the passed states are scheduled to be
// deactivated.  See IsQueued to perform more detailed queries.
func (m *Machine) WillBeRemoved(states S) bool {
	// TODO test
	return -1 != m.IsQueued(MutationRemove, states, false, false, 0)
}

// WillBeRemoved1 returns true if the passed state is scheduled to be
// deactivated. See IsQueued to perform more detailed queries.
func (m *Machine) WillBeRemoved1(state string) bool {
	// TODO test
	return m.WillBeRemoved(S{state})
}

// Has return true is passed states are registered in the machine. Useful for
// checking if a machine implements a specific state set.
func (m *Machine) Has(states S) bool {
	if m.disposed.Load() {
		return false
	}
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
	if m.disposed.Load() {
		return false
	}
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
	if m.disposed.Load() {
		return false
	}
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
	if m.disposed.Load() {
		return ""
	}
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
//
//	(Foo:1 Bar:3) [Baz:2]
func (m *Machine) StringAll() string {
	if m.disposed.Load() {
		return ""
	}
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

	return ret + ") " + ret2 + "]"
}

// Inspect returns a multi-line string representation of the machine (states,
// relations, clocks).
// states: param for ordered or partial results.
func (m *Machine) Inspect(states S) string {
	if m.disposed.Load() {
		return ""
	}
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	if states == nil {
		states = m.stateNames
	}

	ret := ""
	for _, name := range states {

		state := m.states[name]
		active := "0"
		if slices.Contains(m.activeStates, name) {
			active = "1"
		}

		ret += fmt.Sprintf("%s %s\n"+
			"    |Time     %d\n", active, name, m.clock[name])
		if state.Auto {
			ret += "    |Auto     true\n"
		}
		if state.Multi {
			ret += "    |Multi    true\n"
		}
		if state.Add != nil {
			ret += "    |Add      " + j(state.Add) + "\n"
		}
		if state.Require != nil {
			ret += "    |Require  " + j(state.Require) + "\n"
		}
		if state.Remove != nil {
			ret += "    |Remove   " + j(state.Remove) + "\n"
		}
		if state.After != nil {
			ret += "    |After    " + j(state.After) + "\n"
		}
	}

	return ret
}

// Switch returns the first state from the passed list that is currently active,
// making it handy for switch statements. Useful for state groups.
//
//	switch mach.Switch(ss.GroupPlaying) {
//	case "Playing":
//	case "Paused":
//	case "Stopped":
//	}
func (m *Machine) Switch(groups ...S) string {
	activeStates := m.ActiveStates()
	for _, states := range groups {
		for _, state := range states {
			if slices.Contains(activeStates, state) {
				return state
			}
		}
	}

	return ""
}

// CountActive returns the amount of active states from a passed list. Useful
// for state groups.
func (m *Machine) CountActive(states S) int {
	activeStates := m.ActiveStates()
	c := 0
	for _, state := range states {
		if slices.Contains(activeStates, state) {
			c++
		}
	}

	return c
}

// HandleDispose adds a function to be called after the machine gets disposed.
// These functions will run synchronously just before WhenDisposed() channel
// gets closed. Considering it's a low-level feature, its advaised to handle
// disposal via dedicated states.
func (m *Machine) HandleDispose(fn HandlerDispose) {
	m.handlersLock.Lock()
	defer m.handlersLock.Unlock()

	m.disposeHandlers = append(m.disposeHandlers, fn)
}

// ActiveStates returns a copy of the currently active states.
func (m *Machine) ActiveStates() S {
	if m.disposed.Load() {
		return S{}
	}
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
	if m.disposed.Load() {
		return nil
	}
	m.queueLock.RLock()
	defer m.queueLock.RUnlock()

	return slices.Clone(m.queue)
}

// GetSchema returns a copy of machine's schema.
func (m *Machine) GetSchema() Schema {
	// TODO refac: Schema
	m.statesLock.RLock()
	defer m.statesLock.RUnlock()

	return maps.Clone(m.states)
}

// Schema is deprecated, use GetSchema,
func (m *Machine) Schema() Schema {
	// TODO remove
	return m.GetSchema()
}

func (m *Machine) SchemaVer() uint8 {
	return uint8(m.schemaVer.Load())
}

// SetSchema sets the machine's schema. It will automatically call
// VerifyStates with the names param and handle EventSchemaChange if successful.
// Note: it's not recommended to change the states structure of a machine which
// has already produced transitions.
func (m *Machine) SetSchema(statesSchema Schema, names S) error {
	m.statesLock.RLock()
	defer m.statesLock.RUnlock()
	m.queueLock.RLock()
	defer m.queueLock.RUnlock()

	old := m.states
	m.states = parseSchema(statesSchema)
	m.schemaVer.Add(1)

	err := m.VerifyStates(names)
	if err != nil {
		return err
	}
	// notify the resolver
	m.resolver.NewStruct()

	// tracers
	m.tracersLock.RLock()
	for i := 0; !m.disposed.Load() && i < len(m.tracers); i++ {
		m.tracers[i].SchemaChange(m, old)
	}
	m.tracersLock.RUnlock()

	return nil
}

// EvAdd is like Add, but passed the source event as the 1st param, which
// results in traceable transitions.
func (m *Machine) EvAdd(event *Event, states S, args A) Result {
	if m.disposed.Load() || m.disposing.Load() ||
		int(m.queueLen.Load()) >= m.QueueLimit {

		return Canceled
	}
	m.queueMutation(MutationAdd, states, args, event)
	m.breakpoint(states, nil)

	return m.processQueue()
}

// TODO add EvToggle, EvToggle1

// EvAdd1 is like Add1 but passed the source event as the 1st param, which
// results in traceable transitions.
func (m *Machine) EvAdd1(event *Event, states string, args A) Result {
	return m.EvAdd(event, S{states}, args)
}

// EvRemove is like Remove but passed the source event as the 1st param, which
// results in traceable transitions.
func (m *Machine) EvRemove(event *Event, states S, args A) Result {
	if m.disposed.Load() || m.disposing.Load() ||
		int(m.queueLen.Load()) >= m.QueueLimit {

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
	m.queueMutation(MutationRemove, states, args, event)
	m.breakpoint(nil, states)

	return m.processQueue()
}

// EvRemove1 is like Remove1, but passed the source event as the 1st param,
// which results in traceable transitions.
func (m *Machine) EvRemove1(event *Event, states string, args A) Result {
	return m.EvRemove(event, S{states}, args)
}

// EvAddErr is like AddErr, but passed the source event as the 1st param, which
// results in traceable transitions.
func (m *Machine) EvAddErr(event *Event, err error, args A) Result {
	return m.EvAddErrState(event, Exception, err, args)
}

// EvAddErrState is like AddErrState, but passed the source event as the 1st
// param, which results in traceable transitions.
func (m *Machine) EvAddErrState(
	event *Event, state string, err error, args A,
) Result {
	if m.disposed.Load() || m.disposing.Load() ||
		int(m.queueLen.Load()) >= m.QueueLimit {

		return Canceled
	}
	// TODO test Err()
	m.err.Store(&err)

	var trace string
	if m.LogStackTrace {
		trace = captureStackTrace()
	}

	// build args
	// TODO read [event] and fill out relevant fields
	argsT := &AT{
		Err:      err,
		ErrTrace: trace,
	}

	// TODO prepend to the queue? what effects / benefits
	return m.EvAdd(event, S{state, Exception}, PassMerge(args, argsT))
}

// Export exports the machine state as Serialized: ID, machine time, and
// state names.
func (m *Machine) Export() *Serialized {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	if !m.statesVerified.Load() {
		panic("can't export - call VerifyStates first")
	}

	m.log(LogChanges, "[import] exported at %d ticks", m.time(nil))

	return &Serialized{
		ID:         m.id,
		Time:       m.time(nil),
		StateNames: m.stateNames,
	}
}

// Import imports the machine state from Serialized. It's not safe to import
// into a machine which has already produces transitions and/or
// has telemetry connected.
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
	m.id = data.ID
	m.statesVerified.Store(true)

	// TODO optionally run handlers for active states?
	m.log(LogChanges, "[import] imported to %d ticks", t)

	return nil
}

// Index returns the index of a state in the machine's StateNames() list, or -1
// when not found or machine has been disposed.
func (m *Machine) Index(state string) int {
	if m.disposed.Load() {
		return -1
	}
	return slices.Index(m.StateNames(), state)
}

// Resolver return the relation resolver, used to produce target states of
// transitions.
func (m *Machine) Resolver() RelationsResolver {
	return m.resolver
}

// BindTracer binds a Tracer to the machine.
func (m *Machine) BindTracer(tracer Tracer) error {
	m.tracersLock.Lock()
	defer m.tracersLock.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

	m.tracers = append(m.tracers, tracer)
	m.log(LogOps, "[tracers] bind %s", name)

	return nil
}

// DetachTracer tries to remove a tracer from the machine. Returns true if the
// tracer was found and removed.
func (m *Machine) DetachTracer(tracer Tracer) bool {
	// TODO refac: return error
	if m.disposing.Load() {
		return true
	}
	m.tracersLock.Lock()
	defer m.tracersLock.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return false
	}
	name := reflect.TypeOf(tracer).Elem().Name()

	for i, t := range m.tracers {
		if t == tracer {
			// TODO check
			m.tracers = slices.Delete(m.tracers, i, i+1)
			m.log(LogOps, "[tracers] detach %s", name)

			return true
		}
	}

	return false
}

// Tracers return a copy of currenty attached tracers.
func (m *Machine) Tracers() []Tracer {
	m.tracersLock.Lock()
	defer m.tracersLock.Unlock()

	return slices.Clone(m.tracers)
}
