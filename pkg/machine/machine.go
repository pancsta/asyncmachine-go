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
	// StateException. Default: 1s. See also Opts.HandlerTimeout.
	// Using HandlerTimeout can cause race conditions, see Event.IsValid().
	HandlerTimeout time.Duration
	// HandlerDeadline is a grace period after a handler timeout, before the
	// machine moves on.
	HandlerDeadline time.Duration
	// LastHandlerDeadline stores when the last HandlerDeadline was hit.
	LastHandlerDeadline atomic.Pointer[time.Time]
	// HandlerBackoff is the time after a HandlerDeadline, during which the
	// machine will return [Canceled] to any mutation.
	HandlerBackoff time.Duration
	// EvalTimeout is the time the machine will try to execute an eval func.
	// Like any other handler, eval func also has HandlerTimeout. Default: 1s.
	EvalTimeout time.Duration
	// If true, the machine will print all exceptions to stdout. Default: true.
	// Requires an ExceptionHandler binding and Machine.PanicToException set.
	LogStackTrace bool
	// If true, the machine will catch panic and trigger the StateException state.
	// Default: true.
	PanicToException bool
	// DisposeTimeout specifies the duration to wait for the queue to drain during
	// disposal. Default 1s.
	DisposeTimeout time.Duration

	// subs is the subscription manager.
	subs *Subscriptions

	panicCaught atomic.Bool
	// If true, logs will start with the machine's id (5 chars).
	// Default: true.
	logId atomic.Bool
	// statesVerified assures the state names have been ordered using VerifyStates
	statesVerified atomic.Bool
	// Unique ID of this machine. Default: random.
	id string
	// ctx is the context of the machine.
	ctx context.Context
	// parentId is the id of the parent machine (if any).
	parentId string
	// disposing disabled auto schema
	disposing atomic.Bool
	// disposed tells if the machine has been disposed and is no-op.
	disposed atomic.Bool
	// queueRunning indicates the queue is currently being executed.
	queueRunning atomic.Bool
	// tags are short strings describing the machine.
	tags atomic.Pointer[[]string]
	// tracers are optional tracers for telemetry integrations.
	tracers   []Tracer
	tracersMx sync.RWMutex
	// Err is the last error that occurred.
	err atomic.Pointer[error]
	// Currently executing transition (if any).
	t atomic.Pointer[Transition]
	// schema is a map of state names to state definitions.
	// TODO atomic?
	schema      Schema
	groups      map[string][]int
	groupsOrder []string
	schemaLock  sync.RWMutex
	// activeStates is a list of currently active schema.
	activeStates   S
	activeStatesMx sync.RWMutex
	// queue of mutations to be executed.
	queue []*Mutation
	// queueTick is the number of times the queue has processed a mutation. Starts
	// from [1], for easy comparison with [Result].
	queueTick uint64
	// queueTicksPending is the part of the queue with queue ticks assigned.
	queueTicksPending uint64
	queueToken        atomic.Uint64
	queueMx           sync.RWMutex
	queueProcessing   atomic.Bool
	// total len of the queue (ticking and prepended0
	queueLen atomic.Int32
	// length of the ticking mutations
	// Relation resolver, used to produce target schema of a transition.
	// Default: *DefaultRelationsResolver.
	resolver RelationsResolver
	// List of all the registered state names.
	stateNames       S
	stateNamesExport S
	handlersMx       sync.RWMutex
	loopLock         sync.Mutex
	handlers         []*handler
	clock            Clock
	cancel           context.CancelFunc
	logLevel         atomic.Pointer[LogLevel]
	logger           atomic.Pointer[LoggerFn]
	semLogger        SemLogger
	handlerStart     chan *handlerCall
	handlerEnd       chan bool
	handlerPanic     chan recoveryData
	handlerTimer     *time.Timer
	logEntriesLock   sync.Mutex
	logEntries       []*LogEntry
	logArgs          atomic.Pointer[LogArgsMapperFn]
	currentHandler   atomic.Value
	disposeHandlers  []HandlerDispose
	timeLast         atomic.Pointer[Time]
	// Channel closing when the machine finished disposal. Read-only.
	whenDisposed       chan struct{}
	handlerLoopRunning atomic.Bool
	handlerLoopVer     atomic.Int32
	detectEval         bool
	// unlockDisposed means that disposal is in progress and holding the queueMx
	unlockDisposed atomic.Bool
	// breakpoints are a list of breakpoints for debugging. [][added, removed]
	breakpointsMx sync.Mutex
	breakpoints   []*breakpoint
	onError       atomic.Pointer[HandlerError]
}

// NewCommon creates a new Machine instance with all the common options set.
func NewCommon(
	ctx context.Context, id string, stateSchema Schema, stateNames S,
	handlers any, parent Api, opts *Opts,
) (*Machine, error) {
	machOpts := &Opts{Id: id}

	if opts != nil {
		machOpts = opts
		machOpts.Id = id
	}

	if os.Getenv(EnvAmDebug) != "" {
		machOpts = OptsWithDebug(machOpts)
	} else if os.Getenv(EnvAmTestRunner) != "" {
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
func New(ctx context.Context, schema Schema, opts *Opts) *Machine {
	// parse relations
	parsedStates := ParseSchema(schema)

	m := &Machine{
		HandlerTimeout:   100 * time.Millisecond,
		HandlerDeadline:  10 * time.Second,
		HandlerBackoff:   3 * time.Second,
		EvalTimeout:      time.Second,
		LogStackTrace:    true,
		PanicToException: true,
		QueueLimit:       1000,
		DisposeTimeout:   time.Second,

		id:           randId(),
		schema:       parsedStates,
		clock:        Clock{},
		handlers:     []*handler{},
		handlerStart: make(chan *handlerCall),
		handlerEnd:   make(chan bool),
		handlerPanic: make(chan recoveryData),
		handlerTimer: time.NewTimer(24 * time.Hour),
		whenDisposed: make(chan struct{}),
		// queue ticks start from 1 to align with the [Result] enum
		queueTick: 1,
	}

	m.subs = NewSubscriptionManager(m, m.clock, m.is, m.not, m.log)
	m.semLogger = &semLogger{mach: m}
	m.logId.Store(true)
	m.timeLast.Store(&Time{})
	lvl := LogNothing
	m.logLevel.Store(&lvl)

	// parse opts
	// TODO extract
	opts = cloneOptions(opts)
	var parent Api
	if opts != nil {
		if opts.Id != "" {
			m.id = opts.Id
		}
		if opts.HandlerTimeout != 0 {
			m.HandlerTimeout = opts.HandlerTimeout
		}
		if opts.HandlerDeadline != 0 {
			m.HandlerDeadline = opts.HandlerDeadline
		}
		if opts.HandlerBackoff != 0 {
			m.HandlerBackoff = opts.HandlerBackoff
		}
		if opts.DontPanicToException {
			m.PanicToException = false
		}
		if opts.DontLogStackTrace {
			m.LogStackTrace = false
		}
		if opts.DontLogId {
			m.logId.Store(false)
		}
		if opts.Resolver != nil {
			m.resolver = opts.Resolver
		}
		if opts.LogLevel != LogNothing {
			m.semLogger.SetLevel(opts.LogLevel)
		}
		if opts.Tracers != nil {
			m.tracers = opts.Tracers
		}
		if opts.LogArgs != nil {
			m.logArgs.Store(&opts.LogArgs)
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
		m.parentId = opts.ParentId
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
	if _, ok := m.schema[StateException]; !ok {
		m.schema[StateException] = State{
			Multi: true,
		}
	}

	// infer and sort states for defaults
	for name := range m.schema {
		m.stateNames = append(m.stateNames, name)
		slices.Sort(m.stateNames)
		m.clock[name] = 0
	}

	// notify the resolver
	m.resolver.NewSchema(m.schema, m.stateNames)

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
	// TODO var
	if m.Has1(StateStart) {
		m.Remove1(StateStart, nil)
	}

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

	m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].MachineDispose(m.Id())
	}
	m.tracersMx.RUnlock()

	// skip the locks when forcing
	if !force {
		m.activeStatesMx.Lock()
		defer m.activeStatesMx.Unlock()
		m.subs.Mx.Lock()
		defer m.subs.Mx.Unlock()
		m.tracersMx.Lock()
		defer m.tracersMx.Unlock()
		m.handlersMx.Lock()
		defer m.handlersMx.Unlock()
		m.queueMx.Lock()
		defer m.queueMx.Unlock()
	}

	m.log(LogEverything, "[end] doDispose")
	if m.Err() == nil && m.ctx.Err() != nil {
		err := m.ctx.Err()
		m.err.Store(&err)
	}
	m.handlers = nil

	go func() {
		time.Sleep(100 * time.Millisecond)
		m.loopLock.Lock()
		defer m.loopLock.Unlock()

		closeSafe(m.handlerEnd)
		closeSafe(m.handlerPanic)
		closeSafe(m.handlerStart)
	}()

	m.subs.dispose()
	for _, mut := range m.queue {
		if !mut.IsCheck {
			continue
		}
		if done, ok := mut.Args[argCheckDone].(*CheckDone); ok {
			closeSafe(done.Ch)
		}
	}

	if m.handlerTimer != nil {
		m.handlerTimer.Stop()
		// m.handlerTimer = nil
	}

	// release the queue lock
	if m.unlockDisposed.Load() {
		m.unlockDisposed.Store(false)
		m.queueProcessing.Store(false)
	}

	// TODO close all the CheckDone chans in the queue

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

func (m *Machine) getHandlers(locked bool) []*handler {
	if !locked {
		m.handlersMx.Lock()
		defer m.handlersMx.Unlock()
	}

	return slices.Clone(m.handlers)
}

func (m *Machine) setHandlers(locked bool, handlers []*handler) {
	if !locked {
		m.handlersMx.Lock()
		defer m.handlersMx.Unlock()
	}

	m.handlers = handlers
}

// WhenErr returns a channel that will be closed when the machine is in the
// StateException state.
//
// ctx: optional context that will close the channel early.
func (m *Machine) WhenErr(disposeCtx context.Context) <-chan struct{} {
	return m.When([]string{StateException}, disposeCtx)
}

// When returns a channel that will be closed when all the passed states
// become active or the machine gets disposed.
//
// ctx: optional context that will close the channel early.
func (m *Machine) When(states S, ctx context.Context) <-chan struct{} {
	if m.disposed.Load() {
		return newClosedChan()
	}

	// locks
	m.activeStatesMx.Lock()
	defer m.activeStatesMx.Unlock()

	return m.subs.When(m.MustParseStates(states), ctx)
}

// When1 is an alias to When() for a single state.
// See When.
func (m *Machine) When1(state string, ctx context.Context) <-chan struct{} {
	return m.When(S{state}, ctx)
}

// WhenNot returns a channel that will be closed when all the passed states
// become inactive or the machine gets disposed.
//
// ctx: optional context that will close the channel early.
func (m *Machine) WhenNot(states S, ctx context.Context) <-chan struct{} {
	if m.disposed.Load() {
		ch := make(chan struct{})
		close(ch)
		return ch
	}

	// locks
	m.activeStatesMx.Lock()
	defer m.activeStatesMx.Unlock()

	return m.subs.WhenNot(m.MustParseStates(states), ctx)
}

// WhenNot1 is an alias to WhenNot() for a single state.
// See WhenNot.
func (m *Machine) WhenNot1(state string, ctx context.Context) <-chan struct{} {
	return m.WhenNot(S{state}, ctx)
}

// WhenTime returns a channel that will be closed when all the passed states
// have passed the specified time. The time is a logical clock of the state.
// Machine time can be sourced from [Machine.Time](), or [Machine.Clock]().
//
// ctx: optional context that will close the channel early.
func (m *Machine) WhenTime(
	states S, times Time, ctx context.Context,
) <-chan struct{} {
	if m.disposed.Load() {
		return newClosedChan()
	}

	// close early on invalid
	if len(states) != len(times) {
		err := fmt.Errorf(
			"whenTime: states and times must have the same length (%s)", j(states))
		m.AddErr(err, nil)

		return newClosedChan()
	}

	// locks
	m.activeStatesMx.Lock()
	defer m.activeStatesMx.Unlock()

	return m.subs.WhenTime(states, times, ctx)
}

// WhenTime1 waits till ticks for a single state equal the given value (or
// more).
//
// ctx: optional context that will close the channel early.
func (m *Machine) WhenTime1(
	state string, ticks uint64, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(S{state}, Time{ticks}, ctx)
}

// WhenTicks waits N ticks of a single state (relative to now). Uses WhenTime
// underneath.
//
// ctx: optional context that will close the channel early.
func (m *Machine) WhenTicks(
	state string, ticks int, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(S{state}, Time{uint64(ticks) + m.Tick(state)}, ctx)
}

// WhenQueueEnds closes every time the queue ends, or the optional ctx expires.
//
// ctx: optional context that will close the channel early.
func (m *Machine) WhenQueueEnds(ctx context.Context) <-chan struct{} {
	// finish early
	if m.disposed.Load() || !m.queueRunning.Load() {
		return newClosedChan()
	}

	// locks
	m.queueMx.Lock()
	defer m.queueMx.Unlock()

	return m.subs.WhenQueueEnds(ctx, &m.queueMx)
}

// WhenQueue waits until the passed queueTick gets processed.
func (m *Machine) WhenQueue(tick Result) <-chan struct{} {
	if m.disposed.Load() {
		return newClosedChan()
	}

	// locks
	m.queueMx.Lock()
	defer m.queueMx.Unlock()

	// finish early
	if m.queueTick >= uint64(tick) {
		return newClosedChan()
	}

	return m.subs.WhenQueue(tick)
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
	if m.disposed.Load() {
		return newClosedChan()
	}

	// locks
	m.activeStatesMx.Lock()
	defer m.activeStatesMx.Unlock()

	states := m.MustParseStates(S{state})

	return m.subs.WhenArgs(states[0], args, ctx)
}

// WhenDisposed returns a channel that will be closed when the machine is
// disposed. Requires bound handlers. Use Machine.disposed in case no handlers
// have been bound.
func (m *Machine) WhenDisposed() <-chan struct{} {
	return m.whenDisposed
}

// TODO implement +rpc worker
// func (m *Machine) SetArgsComp(comp func(args A, match A) bool) {
// 	return false
// }

// debug
// func (m *Machine) QueueDump() []string {
// 	m.queueLock.Lock()
// 	defer m.queueLock.Unlock()
// 	ret := make([]string, 0)
//
// 	index := m.StateNames()
// 	for _, mut := range m.queue {
// 		if mut.Type == mutationEval {
// 			continue
// 		}
//
// 		ret = append(ret, mut.StringFromIndex(index))
// 	}
//
// 	return ret
// }

// QueueTick is the number of times the queue has processed a mutation. Starts
// from [1], for easy comparison with [Result].
func (m *Machine) QueueTick() uint64 {
	m.queueMx.Lock()
	defer m.queueMx.Unlock()

	return m.queueTick
}

// Time returns machine's time, a list of ticks per state. Returned value
// includes the specified states, or all the states if nil.
func (m *Machine) Time(states S) Time {
	if m.disposed.Load() {
		return nil
	}
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

	return m.time(states)
}

func (m *Machine) time(states S) Time {
	if m.disposed.Load() {
		return nil
	}
	m.schemaLock.RLock()
	defer m.schemaLock.RUnlock()

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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()
	m.schemaLock.RLock()
	defer m.schemaLock.RUnlock()

	if states == nil {
		states = m.stateNames
	}

	ret := uint64(0)
	for _, s := range states {
		ret += m.clock[s]
	}

	return ret
}

// PrependMut prepends the mutation to the front of the queue. This is a special
// cases only method and should be used with caution, as it can create an
// infinite loop. It's useful for postponing mutations inside a negotiation
// handler. The returned Result can't be waited on, as prepended mutations don't
// create a queue tick.
func (m *Machine) PrependMut(mut *Mutation) Result {
	if m.disposed.Load() {
		return Canceled
	}

	isEval := mut.Type == mutationEval
	statesParsed := m.MustParseStates(IndexToStates(m.StateNames(), mut.Called))
	m.log(LogOps, "[prepend:%s] %s%s", mut.Type, j(statesParsed),
		mut.LogArgs(m.SemLogger().ArgsMapper()))

	m.queueMx.Lock()
	if !isEval {
		mut.cacheCalled.Store(&statesParsed)
	}
	m.queue = append([]*Mutation{mut}, m.queue...)
	lenQ := len(m.queue)
	m.queueLen.Store(int32(lenQ))
	if !isEval {
		mut.QueueLen = int32(lenQ)
		mut.QueueToken = m.queueToken.Add(1)
		mut.QueueTickNow = m.queueTick
	}
	m.queueMx.Unlock()

	// tracers
	if !isEval {
		m.tracersMx.RLock()
		for i := 0; !m.disposed.Load() && i < len(m.tracers); i++ {
			m.tracers[i].MutationQueued(m, mut)
		}
		m.tracersMx.RUnlock()
	}

	res := m.processQueue()

	return res
}

// Add activates a list of states in the machine, returning the result of the
// transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (m *Machine) Add(states S, args A) Result {
	if m.disposed.Load() || m.disposing.Load() || m.Backoff() {
		return Canceled
	}

	// let Exception in even with a full queue, but only once
	if int(m.queueLen.Load()) >= m.QueueLimit {
		if !slices.Contains(states, StateException) || m.IsErr() {
			return Canceled
		}
	}

	queueTick := m.queueMutation(MutationAdd, states, args, nil)
	if queueTick == uint64(Executed) {
		return Executed
	}
	m.breakpoint(states, nil)

	res := m.processQueue()
	if res == Queued {
		return Result(queueTick)
	}

	return res
}

// Add1 is a shorthand method to add a single state with the passed args.
func (m *Machine) Add1(state string, args A) Result {
	return m.Add(S{state}, args)
}

// Toggle deactivates a list of states in case all are active, or activates
// them otherwise. Returns the result of the transition (Executed, Queued,
// Canceled).
func (m *Machine) Toggle(states S, args A) Result {
	// TODO add to Api
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
	// TODO add to Api
	if m.disposed.Load() {
		return Canceled
	}
	if m.Is1(state) {
		return m.Remove1(state, args)
	} else {
		return m.Add1(state, args)
	}
}

// EvToggle is a traced version of [Machine.Toggle].
func (m *Machine) EvToggle(e *Event, states S, args A) Result {
	// TODO add to Api
	if m.disposed.Load() {
		return Canceled
	}
	if m.Is(states) {
		return m.EvRemove(e, states, args)
	} else {
		return m.EvAdd(e, states, args)
	}
}

// EvToggle1 is a traced version of [Machine.Toggle1].
func (m *Machine) EvToggle1(e *Event, state string, args A) Result {
	// TODO add to Api
	if m.disposed.Load() {
		return Canceled
	}
	if m.Is1(state) {
		return m.EvRemove1(e, state, args)
	} else {
		return m.EvAdd1(e, state, args)
	}
}

// AddErr is a dedicated method to add the StateException state with the passed
// error and optional arguments.
// Like every mutation method, it will resolve relations and trigger handlers.
// AddErr produces a stack trace of the error, if LogStackTrace is enabled.
func (m *Machine) AddErr(err error, args A) Result {
	return m.AddErrState(StateException, err, args)
}

// AddErrState adds a dedicated error state, along with the build in
// StateException state. Like every mutation method, it will resolve relations
// and trigger handlers. AddErrState produces a stack trace of the error, if
// LogStackTrace is enabled.
func (m *Machine) AddErrState(state string, err error, args A) Result {
	if m.disposed.Load() || m.disposing.Load() || m.Backoff() || err == nil {
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

	// error handler
	onErr := m.onError.Load()
	if onErr != nil {
		(*onErr)(m, err)
	}

	// TODO prepend to the queue? what effects / benefits
	return m.Add(S{state, StateException}, PassMerge(args, argsT))
}

func (m *Machine) CanAdd(states S, args A) Result {
	if m.disposed.Load() || m.disposing.Load() || m.Backoff() {
		return Canceled
	}

	return m.PrependMut(&Mutation{
		Type:    MutationAdd,
		Called:  m.Index(states),
		Args:    args,
		IsCheck: true,
	})
}

func (m *Machine) CanAdd1(state string, args A) Result {
	return m.CanAdd(S{state}, args)
}

func (m *Machine) CanRemove(states S, args A) Result {
	if m.disposed.Load() || m.disposing.Load() || m.Backoff() {
		return Canceled
	}

	return m.PrependMut(&Mutation{
		Type:    MutationRemove,
		Called:  m.Index(states),
		Args:    args,
		IsCheck: true,
	})
}

func (m *Machine) CanRemove1(state string, args A) Result {
	return m.CanRemove(S{state}, nil)
}

// PanicToErr will catch a panic and add the StateException state. Needs to
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

// PanicToErrState will catch a panic and add the StateException state, along
// with the passed state. Needs to be called in a defer statement, just like a
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

// IsErr checks if the machine has the StateException state currently active.
func (m *Machine) IsErr() bool {
	return m.Is(S{StateException})
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
	if m.disposed.Load() || m.disposing.Load() || m.Backoff() {
		return Canceled
	}

	// let Exception in even with a full queue, but only once
	if int(m.queueLen.Load()) >= m.QueueLimit {
		if !slices.Contains(states, StateException) || !m.IsErr() {
			return Canceled
		}
	}

	// return early if none of the states is active
	m.queueMx.RLock()
	lenQueue := len(m.queue)

	// try ignoring this mutation, if none of the states is currently active
	var statesAny []S
	for _, name := range states {
		statesAny = append(statesAny, S{name})
	}

	if lenQueue == 0 && m.Transition() != nil && !m.Any(statesAny...) {
		m.queueMx.RUnlock()
		return Executed
	}

	m.queueMx.RUnlock()
	queueTick := m.queueMutation(MutationRemove, states, args, nil)
	if queueTick == uint64(Executed) {
		return Executed
	}
	m.breakpoint(nil, states)

	res := m.processQueue()
	if res == Queued {
		return Result(queueTick + 1)
	}

	return res
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
	queueTick := m.queueMutation(MutationSet, states, args, nil)
	if queueTick == uint64(Executed) {
		return Executed
	}

	res := m.processQueue()
	if res == Queued {
		return Result(queueTick + 1)
	}

	return res
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

// SetTags updates the machine's tags with the provided slice of strings.
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

	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

	return m.is(states)
}

// Is1 is a shorthand method to check if a single state is currently active.
// See Is().
func (m *Machine) Is1(state string) bool {
	return m.Is(S{state})
}

// is is an unsafe version of Is(), make sure to acquire activeStatesMx.
func (m *Machine) is(states S) bool {
	if m.disposed.Load() {
		return false
	}
	activeStates := m.activeStates
	// TODO optimize, dont copy, use map index

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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

	return m.not(states)
}

func (m *Machine) not(states S) bool {
	return slicesNone(m.MustParseStates(states), m.activeStates)
}

// Not1 is a shorthand method to check if a single state is currently inactive.
// See Not().
func (m *Machine) Not1(state string) bool {
	return m.Not(S{state})
}

// Any a is group call to Is, returns true if any of the params return true
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

// queueMutation queues a mutation to be executed. Returns >= 0 if the mutation
// was queued. 0 mean NoOp.
func (m *Machine) queueMutation(
	mutType MutationType, states S, args A, event *Event,
) uint64 {
	statesParsed := m.MustParseStates(states)
	multi := false
	for _, state := range statesParsed {
		if m.schema[state].Multi {
			multi = true
			break
		}
	}

	// TODO check queuelen > max(int16)

	// Detect duplicates and avoid queueing them, but not for multi states, nor
	// any args.
	if !multi && len(args) == 0 &&
		m.detectQueueDuplicates(mutType, statesParsed, false) {

		m.log(LogOps, "[queue:skipped] Duplicate detected for [%s] %s",
			mutType, j(statesParsed))

		return uint64(Executed)
	}

	// args should always be initialized
	if args == nil {
		args = A{}
	}

	// prep the mutation
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
	mut := &Mutation{
		Type:   mutType,
		Called: m.Index(statesParsed),
		Args:   args,
		Source: source,
	}
	mut.cacheCalled.Store(&statesParsed)

	// work the queue and persist in the mutation
	m.queueMx.Lock()
	m.queue = append(m.queue, mut)
	lenQ := len(m.queue)
	m.queueLen.Store(int32(lenQ))
	m.queueTicksPending += 1
	mut.QueueLen = int32(lenQ)
	mut.QueueTick = m.queueTicksPending + m.queueTick
	mut.QueueTickNow = m.queueTick
	m.queueMx.Unlock()

	// tracers
	m.log(LogOps, "[queue:%s] %s%s", mutType, j(statesParsed),
		mut.LogArgs(m.SemLogger().ArgsMapper()))
	m.tracersMx.RLock()
	for i := 0; !m.disposed.Load() && i < len(m.tracers); i++ {
		m.tracers[i].MutationQueued(m, mut)
	}
	m.tracersMx.RUnlock()

	// breakpoints
	if mut.Type == MutationAdd {
		m.breakpoint(statesParsed, nil)
	} else if mutType == MutationRemove {
		m.breakpoint(nil, statesParsed)
	}

	return mut.QueueTick
}

// Eval executes a function on the machine's queue, allowing to avoid using
// locks for non-handler code. Blocking code should NOT be scheduled here.
// Eval cannot be called within a handler's critical zone, as both are using
// the same serial queue and will deadlock. Eval has a timeout of
// HandlerTimeout/2 and will return false in case it happens. Evals do not
// trigger consensus, thus are much faster than state mutations.
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

	// check every method of every handler against the stack trace
	if m.detectEval {
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
		// TODO optimize: close earlier when [canceled]
		defer close(done)
		if canceled.Load() {
			return
		}
		fn()
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// prepend to the queue the queue, but ignore the result
	mut := &Mutation{
		Type:       mutationEval,
		eval:       wrap,
		evalSource: source,
		ctx:        ctx,
	}
	// TODO handle Canceled?
	_ = m.PrependMut(mut)

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
	m.activeStatesMx.Lock()
	defer m.activeStatesMx.Unlock()

	return m.subs.NewStateCtx(state)
}

// MustBindHandlers is a panicking version of BindHandlers, useful in tests.
func (m *Machine) MustBindHandlers(handlers any) {
	if err := m.BindHandlers(handlers); err != nil {
		panic(err)
	}
}

// BindHandlers binds a struct of handler methods to machine's states, based on
// the naming convention, eg `FooState(e *Event)`. Negotiation handlers can
// optionally return bool.
func (m *Machine) BindHandlers(handlers any) error {
	if m.disposed.Load() {
		return nil
	}
	first := false
	if !m.handlerLoopRunning.Load() {
		first = true
		m.handlerLoopRunning.Store(true)

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
			return fmt.Errorf("listing handlers: %w", err)
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

	// if already in Exception when 1st handler group is bound, re-add the err
	if first && m.IsErr() {
		m.AddErr(m.Err(), nil)
	}

	return nil
}

// DetachHandlers detaches previously bound machine handlers.
func (m *Machine) DetachHandlers(handlers any) error {
	if m.disposing.Load() {
		return nil
	}

	m.handlersMx.Lock()
	defer m.handlersMx.Unlock()

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

// HasHandlers returns true if this machine has bound handlers, and thus an
// allocated goroutine. It also makes it nondeterministic.
func (m *Machine) HasHandlers() bool {
	m.handlersMx.RLock()
	defer m.handlersMx.RUnlock()

	// TODO keep a cache flag?
	return len(m.handlers) > 0
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

// recoverToErr recovers to the StateException state by catching panics.
func (m *Machine) recoverToErr(handler *handler, r recoveryData) {
	if m.disposed.Load() {
		return
	}

	m.panicCaught.Store(true)
	m.currentHandler.Store("")
	t := m.t.Load()
	index := m.StateNames()
	iException := m.Index1(StateException)

	// dont double handle an exception (no nesting)
	mut := t.Mutation
	if mut.IsCalled(iException) {
		return
	}

	m.log(LogOps, "[recover] handling panic...")
	err := fmt.Errorf("%s", r.err)
	m.err.Store(&err)

	// final phase, trouble...
	if t.latestHandlerIsFinal {
		m.recoverFinalPhase()
	}
	m.log(LogOps, "[cancel] (%s) by recover", j(t.TargetStates()))

	// negotiation phase - canceling is enough
	t.IsAccepted.Store(false)
	t.IsCompleted.Store(true)

	// prepend add:Exception to the beginning of the queue
	errMut := &Mutation{
		Type:   MutationAdd,
		Called: m.Index(S{StateException}),
		Args: Pass(&AT{
			Err:      err,
			ErrTrace: r.stack,
			Panic: &ExceptionArgsPanic{
				CalledStates: IndexToStates(index, mut.Called),
				StatesBefore: t.StatesBefore(),
				Transition:   t,
			},
		}),
	}

	// prepend the exception to the queue
	m.PrependMut(errMut)

	// restart the handler loop
	go m.handlerLoop()
}

func (m *Machine) recoverFinalPhase() {
	t := m.t.Load()

	// try to fix active states
	finals := slices.Concat(t.Exits, t.Enters)
	m.activeStatesMx.RLock()
	activeStates := m.activeStates
	m.activeStatesMx.RUnlock()
	found := false

	// walk over enter/exits and remove states after the last step,
	// as their final handlers haven't been executed
	for _, s := range finals {

		if t.latestHandlerToState == s {
			found = true
		}
		if !found {
			continue
		}

		if t.latestHandlerIsEnter {
			activeStates = slicesWithout(activeStates, s)
		} else {
			activeStates = append(activeStates, s)
		}
	}

	m.log(LogOps, "[recover] partial final states as (%s)",
		j(activeStates))
	m.activeStatesMx.Lock()
	defer m.activeStatesMx.Unlock()
	m.setActiveStates(t.CalledStates(), activeStates, t.IsAuto())
}

// MustParseStates parses the states and returns them as a list.
// Panics when a state is not defined.
func (m *Machine) MustParseStates(states S) S {
	// TODO maybe no-on and addErr instead of panics?
	if m.disposed.Load() {
		return nil
	}

	// check if all states are defined in m.Struct
	seen := make(map[string]struct{})
	dups := false
	for i := range states {
		if _, ok := m.schema[states[i]]; !ok {
			panic(fmt.Errorf(
				"%w: %s not defined in schema for %s", ErrStateMissing,
				states[i], m.id))
		}
		if _, ok := seen[states[i]]; !ok {
			seen[states[i]] = struct{}{}
		} else {
			// mark as duplicated
			dups = true
		}
	}

	if dups {
		return slicesUniq(states)
	}
	return states
}

// VerifyStates verifies an array of state names and returns an error in case
// at least one isn't defined. It also retains the order and uses it for
// StateNames. Verification can be checked via Machine.StatesVerified.
func (m *Machine) VerifyStates(states S) error {
	if m.disposed.Load() {
		return nil
	}

	m.schemaLock.RLock()
	defer m.schemaLock.RUnlock()

	return m.verifyStates(states)
}

func (m *Machine) verifyStates(states S) error {
	var errs []error
	var checked []string
	for _, s := range states {

		if _, ok := m.schema[s]; !ok {
			err := fmt.Errorf("state %s not defined in schema for %s", s, m.id)
			errs = append(errs, err)
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
			"error: trying to verify less states than registered: %s", j(missing))
	}

	// memorize the state names order
	m.stateNames = slicesUniq(states)
	m.stateNamesExport = nil
	m.statesVerified.Store(true)

	// tracers
	m.tracersMx.RLock()
	for i := 0; !m.disposed.Load() && i < len(m.tracers); i++ {
		m.tracers[i].VerifyStates(m)
	}
	m.tracersMx.RUnlock()

	return nil
}

// StatesVerified returns true if the state names have been ordered
// using VerifyStates.
func (m *Machine) StatesVerified() bool {
	return m.statesVerified.Load()
}

// setActiveStates sets the new active states incrementing the counters and
// returning the previously active states.
func (m *Machine) setActiveStates(
	calledStates S, targetStates S, isAuto bool,
) S {
	if m.disposed.Load() {
		// no-op
		return S{}
	}

	previous := m.activeStates
	newStates := DiffStates(targetStates, m.activeStates)
	removedStates := DiffStates(m.activeStates, targetStates)
	noChangeStates := DiffStates(targetStates, newStates)
	m.activeStates = slices.Clone(targetStates)

	// Tick all new states by +1 and already active and called multi states by +2
	for _, state := range targetStates {

		data := m.schema[state]
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
	if m.SemLogger().Level() >= LogExternal {
		logMsg := ""
		if len(newStates) > 0 {
			logMsg += " +" + strings.Join(newStates, " +")
		}
		if len(removedStates) > 0 {
			logMsg += " -" + strings.Join(removedStates, " -")
		}
		if len(noChangeStates) > 0 && m.semLogger.Level() > LogDecisions {
			logMsg += " " + j(noChangeStates)
		}

		if len(logMsg) > 0 {
			label := "state"
			if isAuto {
				label = "auto_"
			}

			args := m.t.Load().Mutation.LogArgs(m.SemLogger().ArgsMapper())
			m.log(LogChanges, "["+label+"]"+logMsg+args)
		}
	}

	return previous
}

func (m *Machine) AddBreakpoint1(added string, removed string, strict bool) {
	if added != "" {
		m.AddBreakpoint(S{added}, nil, strict)
	} else if removed != "" {
		m.AddBreakpoint(nil, S{removed}, strict)
	} else {
		m.log(LogOps, "[breakpoint] invalid")
	}
}

// AddBreakpoint adds a breakpoint for an outcome of mutation (added and
// removed states). Once such mutation happens, a log message will be printed
// out. You can set an IDE's breakpoint on this line and see the mutation's sync
// stack trace. When Machine.LogStackTrace is set, the stack trace will be
// printed out as well. Many breakpoints can be added, but none removed.
func (m *Machine) AddBreakpoint(added S, removed S, strict bool) {
	// TODO strict: dont breakpoint added states when already active
	m.breakpointsMx.Lock()
	defer m.breakpointsMx.Unlock()

	m.breakpoints = append(m.breakpoints, &breakpoint{
		Added:   added,
		Removed: removed,
		Strict:  strict,
	})
}

func (m *Machine) breakpoint(added S, removed S) {
	m.breakpointsMx.Lock()
	defer m.breakpointsMx.Unlock()

	found := false
	for _, bp := range m.breakpoints {

		// check if the breakpoint matches
		if len(added) > 0 && !slices.Equal(bp.Added, added) {
			continue
		}
		if len(removed) > 0 && !slices.Equal(bp.Removed, removed) {
			continue
		}

		// strict skips already active / inactive
		if bp.Strict {
			if len(bp.Added) > 0 && m.Is(bp.Added) {
				continue
			}
			if len(bp.Removed) > 0 && m.Not(bp.Removed) {
				continue
			}
		}

		found = true
	}

	if !found {
		return
	}

	// ----- ----- -----
	// SET THE IDE BREAKPOINT BELOW
	// ----- ----- -----

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

		m.queueMx.Lock()
		defer m.queueMx.Unlock()

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
		m.queueMx.Lock()
		lenQ := len(m.queue)
		if lenQ < 1 {
			m.Log("ERROR: missing queue item")
			return Canceled
		}
		mut := m.queue[0]
		m.queue = m.queue[1:]
		m.queueLen.Store(int32(lenQ - 1))
		// queue ticks
		if mut.QueueTick > 0 {
			m.queueTicksPending -= 1
			m.queueTick += 1
		}
		m.queueMx.Unlock()

		// support for context cancelation
		if mut.ctx != nil && mut.ctx.Err() != nil {
			ret = append(ret, Executed)

			continue
		}

		// special case for Eval mutations
		if mut.Type == mutationEval {
			m.Log("eval: " + mut.evalSource)
			mut.eval()

			continue
		}
		t := newTransition(m, mut)

		// execute the transition and set active states
		ret = append(ret, t.emitEvents())
		m.timeLast.Store(&t.TimeAfter)
		// TODO assert QueueTick same as mut queue tick?

		// parse wait chans
		if t.Mutation.IsCheck {
			// TODO test case
			if done, ok := mut.Args[argCheckDone].(*CheckDone); ok {
				done.Canceled = t.IsAccepted.Load()
				closeSafe(done.Ch)
			}
		} else if t.IsAccepted.Load() && !t.Mutation.IsCheck {
			// TODO optimize process only when ticks change (incl queue tick)
			m.processSubscriptions(t)
		}

		t.CleanCache()
	}

	// release the locks
	m.t.Store(nil)
	m.queueProcessing.Store(false)
	m.queueRunning.Store(false)

	// tracers
	m.tracersMx.RLock()
	for i := 0; !m.disposed.Load() && i < len(m.tracers); i++ {
		m.tracers[i].QueueEnd(m)
	}
	m.tracersMx.RUnlock()

	// subscriptions
	m.queueMx.Lock()
	for _, ch := range m.subs.ProcessWhenQueueEnds() {
		closeSafe(ch)
	}
	m.queueMx.Unlock()

	if len(ret) == 0 {
		return Canceled
	}
	return ret[0]
}

func (m *Machine) processSubscriptions(t *Transition) {
	// lock
	m.activeStatesMx.RLock()

	// collect
	toCancel := m.subs.ProcessStateCtx(t.cacheDeactivated)
	toClose := slices.Concat(
		m.subs.ProcessWhen(t.cacheActivated, t.cacheDeactivated),
		m.subs.ProcessWhenTime(t.ClockBefore()),
		m.subs.ProcessWhenQueue(m.queueTick),
	)

	// unlock
	m.activeStatesMx.RUnlock()

	// close outside the critical zone
	for _, cancel := range toCancel {
		cancel()
	}
	for _, ch := range toClose {
		closeSafe(ch)
	}
}

// TODO implement +rpc worker
// func (m *Subscriptions) SetArgsComp(comp func(args A, match A) bool) {
// 	return false
// }

// Ctx return machine's root context.
func (m *Machine) Ctx() context.Context {
	return m.ctx
}

// Log logs an [extern] message unless LogNothing is set.
// Optionally redirects to a custom logger from SemLogger().SetLogger.
func (m *Machine) Log(msg string, args ...any) {
	if m.disposed.Load() {
		return
	}
	prefix := "[exter"

	// single lines only
	msg = strings.ReplaceAll(msg, "\n", " ")

	m.log(LogExternal, prefix+"] "+msg, args...)
}

// log logs a message if the log level is high enough.
// Optionally redirects to a custom logger from SemLogger().SetLogger.
func (m *Machine) log(level LogLevel, msg string, args ...any) {
	if level > m.semLogger.Level() || m.disposed.Load() {
		return
	}

	if m.logId.Load() {
		id := m.id
		if len(id) > 5 {
			id = id[:5]
		}
		msg = "[" + id + "] " + msg
	}

	out := fmt.Sprintf(msg, args...)
	logger := m.semLogger.Logger()
	if logger != nil {
		logger(level, msg, args...)
	} else {
		fmt.Println(out)
	}

	// dont modify completed transitions
	t := m.Transition()
	if t != nil && !t.IsCompleted.Load() {
		// append the log msg to the current transition
		t.InternalLogEntriesLock.Lock()
		defer t.InternalLogEntriesLock.Unlock()
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

// SemLogger returns the semantic logger of the machine
func (m *Machine) SemLogger() SemLogger {
	return m.semLogger
}

// handle triggers methods on handlers structs.
// locked: transition lock currently held
func (m *Machine) handle(
	name string, args A, isFinal, isEnter, isSelf bool,
) (Result, bool) {
	if m.disposing.Load() {
		return Canceled, false
	}

	t := m.t.Load()
	e := &Event{
		Name:         name,
		machine:      m,
		Args:         args,
		TransitionId: t.Id,
		MachineId:    m.Id(),
	}
	targetStates := t.TargetStates()

	t.latestHandlerIsEnter = isEnter
	t.latestHandlerIsFinal = isFinal

	// always init args
	if e.Args == nil {
		e.Args = A{}
	}

	// call the handlers
	res, handlerCalled := m.processHandlers(e)
	if m.panicCaught.Load() {
		res = Canceled
		m.panicCaught.Store(false)
	}

	// negotiation support
	if !isFinal && res == Canceled {
		if m.semLogger.Level() >= LogOps {
			var self string
			if isSelf {
				self = ":self"
			}

			m.log(LogOps, "[cancel%s] (%s) by %s", self,
				j(targetStates), name)
		}

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

		h := handlers[i]
		if h == nil {
			continue
		}
		h.mx.Lock()
		methodName := e.Name
		// TODO descriptive name
		handlerName := strconv.Itoa(i) + ":" + h.name

		if m.semLogger.Level() >= LogEverything {
			emitterID := TruncateStr(handlerName, 15)
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
		m.tracersMx.RLock()
		tx := m.t.Load()
		for i := range m.tracers {
			m.tracers[i].HandlerStart(tx, handlerName, methodName)
		}
		m.tracersMx.RUnlock()
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
			// timeout, fork a new handler loop
			m.log(LogOps, "[cancel] (%s) by timeout", j(tx.TargetStates()))
			m.log(LogDecisions, "[handler:timeout]: %s from %s", methodName, h.name)
			timeout = true

			// wait for the handler to exit within HandlerDeadline
			select {
			case <-m.handlerEnd:
				// accepted timeout (good)
				m.log(LogEverything, "[handler:ack-timeout] %s from %s", e.Name, h.name)

				// TODO optimize re-use a timer like timeout
			case <-time.After(m.HandlerDeadline):
				m.log(LogEverything, "[handler:deadline] %s from %s", e.Name, h.name)
				// deadlined timeout (bad)
				// fork a new handler loop
				go m.handlerLoop()

				// clear the queue
				m.queueMx.Lock()
				m.queue = nil
				m.queueLen.Store(0)
				m.queueTicksPending = 0
				m.queueMx.Unlock()
				// TODO dispose all argCheckDone chans

				// enqueue the relevant err
				err := fmt.Errorf("%w: %s from %s", ErrHandlerTimeout, methodName,
					h.name)
				m.EvAddErr(e, err, Pass(&AT{
					TargetStates: tx.TargetStates(),
					CalledStates: tx.CalledStates(),
					TimeBefore:   tx.TimeBefore,
					TimeAfter:    tx.TimeAfter,
					Event:        e.Export(),
				}))

				// activate Backoff for further mutations
				now := time.Now()
				m.LastHandlerDeadline.Store(&now)
			}

		case r := <-m.handlerPanic:
			// recover partial state
			// TODO pass tx info via &AT{}
			m.recoverToErr(h, r)

		case ret = <-m.handlerEnd:
			m.log(LogEverything, "[handler:end] %s from %s", e.Name, h.name)
			// ok
		}

		m.handlerTimer.Stop()
		m.currentHandler.Store("")

		// tracers
		m.tracersMx.RLock()
		for i := range m.tracers {
			m.tracers[i].HandlerEnd(tx, handlerName, methodName)
		}
		m.tracersMx.RUnlock()

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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()
	for _, ch := range m.subs.ProcessWhenArgs(e) {
		close(ch)
	}

	return Executed, handlerCalled
}

func (m *Machine) handlerLoop() {
	ver := m.handlerLoopVer.Add(1)
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

			// handler signature: FooState(e *am.Event)
			// TODO optimize https://github.com/golang/go/issues/7818
			if call.event.IsValid() {
				callRet := call.fn.Call([]reflect.Value{reflect.ValueOf(call.event)})
				if len(callRet) > 0 {
					ret = callRet[0].Interface().(bool)
				}
			} else {
				m.log(LogDecisions, "[handler:invalid] %s", call.name)
				ret = false
			}

			// exit, a new clone is running
			currVer := m.handlerLoopVer.Load()
			if currVer != ver {
				return
			}

			m.loopLock.Lock()

			// pass the result to handlerLoop
			select {
			case <-m.ctx.Done():
				m.handlerLoopDone()
				m.loopLock.Unlock()
				return

			case m.handlerEnd <- ret:
				m.loopLock.Unlock()
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
	states S, isCheck bool,
) bool {
	if m.disposed.Load() {
		return false
	}
	// check if this mutation is already scheduled
	index := m.IsQueued(mutationType, states, true, true, 0, isCheck, PositionAny)
	if index == -1 {
		return false
	}
	var counterMutType MutationType
	switch mutationType {
	case MutationAdd:
		counterMutType = MutationRemove
	case MutationRemove:
		counterMutType = MutationAdd
	case MutationSet:
		fallthrough
	default:
		// avoid duplicating `set` only if at the end of the queue
		return index > 0 && len(m.queue)-1 > 0
	}

	// Check if a counter-mutation is scheduled and broaden the match
	// - with or without params
	// - state sets same or bigger than `states`
	counterQueued := m.IsQueued(counterMutType, states,
		false, false, index+1, isCheck, PositionAny)

	return counterQueued == -1
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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

	return m.clock[state]
}

// IsQueued checks if a particular mutation has been queued. Returns
// an index of the match or -1 if not found.
//
// mutType: add, remove, set
//
// states: list of states used in the mutation
//
// withoutArgsOnly: matches only mutation without the arguments object
//
// statesStrictEqual: states of the mutation have to be exactly like `states`
// and not a superset.
//
// startIndex: start later than 0
//
// isCheck: the mutation has to be a [Mutation.IsCheck]
//
// position: position in the queue, after applying the [startIndex]
func (m *Machine) IsQueued(mutType MutationType, states S,
	withoutArgsOnly bool, statesStrictEqual bool, startIndex int16, isCheck bool,
	position Position,
) int16 {
	// TODO add QueueQuery with all the params
	// TODO test case

	if m.disposed.Load() {
		return -1
	}
	m.queueMx.RLock()
	defer m.queueMx.RUnlock()

	// start index
	qLen := int16(m.queueLen.Load())
	if qLen == 0 || qLen-startIndex < 1 {
		return -1
	}
	iter := m.queue[startIndex:]

	// position TODO test case
	switch position {
	case PositionLast:
		iter = iter[len(iter)-1:]
	case PositionFirst:
		iter = iter[0:1]
	}

	for i, mut := range iter {
		if mut.IsCheck == isCheck &&
			mut.Type == mutType &&
			((withoutArgsOnly && len(mut.Args) == 0) || !withoutArgsOnly) &&
			// target states have to be at least as long as the checked ones
			// or exactly the same in case of a strict_equal
			((statesStrictEqual &&
				len(mut.Called) == len(states)) ||
				(!statesStrictEqual &&
					len(mut.Called) >= len(states))) &&
			// and all the checked ones have to be included in the target ones
			slicesEvery(mut.Called, m.Index(states)) {

			return int16(i)
		}
	}

	return -1
}

// IsQueuedAbove allows for rate-limiting of mutations for a specific state.
func (m *Machine) IsQueuedAbove(threshold int, mutationType MutationType,
	states S, withoutArgsOnly bool, statesStrictEqual bool, startIndex int,
) bool {
	if m.disposed.Load() {
		return false
	}
	// TODO test
	m.queueMx.RLock()
	defer m.queueMx.RUnlock()

	c := 0

	for i, item := range m.queue {
		if i >= startIndex &&
			item.Type == mutationType &&
			((withoutArgsOnly && len(item.Args) == 0) || !withoutArgsOnly) &&
			// target states have to be at least as long as the checked ones
			// or exactly the same in case of a strict_equal
			((statesStrictEqual &&
				len(item.Called) == len(states)) ||
				(!statesStrictEqual &&
					len(item.Called) >= len(states))) &&
			// and all the checked ones have to be included in the target ones
			slicesEvery(item.Called, m.Index(states)) {

			c++
			if c >= threshold {
				return true
			}
		}
	}

	return false
}

func (m *Machine) QueueLen() int {
	return int(m.queueLen.Load())
}

// WillBe returns true if the passed states are scheduled to be activated.
// Does not cover implied states, only called ones.
// See [Machine.IsQueued] to perform more detailed queries.
//
// position: optional position assertion
func (m *Machine) WillBe(states S, position ...Position) bool {
	// TODO test

	if len(position) == 0 {
		position = []Position{PositionAny}
	}
	idx := m.IsQueued(MutationAdd, states, false, false, 0, false, position[0])

	switch {
	case idx == -1:
		return false
	case idx == 0 && position[0] == PositionFirst:
		return true
	case int32(idx) == m.queueLen.Load()-1 && position[0] == PositionLast:
		return true
	default:
		return true
	}
}

// WillBe1 returns true if the passed state is scheduled to be activated.
// See IsQueued to perform more detailed queries.
func (m *Machine) WillBe1(state string, position ...Position) bool {
	// TODO test
	return m.WillBe(S{state}, position...)
}

// WillBeRemoved returns true if the passed states are scheduled to be
// deactivated. Does not cover implied states, only called ones. See
// [Machine.IsQueued] to perform more detailed queries.
//
// position: optional position assertion
func (m *Machine) WillBeRemoved(states S, position ...Position) bool {
	// TODO test

	if len(position) == 0 {
		position = []Position{PositionAny}
	}
	idx := m.IsQueued(MutationRemove, states, false, false, 0, false, position[0])

	switch {
	case idx == -1:
		return false
	case idx == 0 && position[0] == PositionFirst:
		return true
	case int32(idx) == m.queueLen.Load()-1 && position[0] == PositionLast:
		return true
	default:
		return true
	}
}

// WillBeRemoved1 returns true if the passed state is scheduled to be
// deactivated. See IsQueued to perform more detailed queries.
func (m *Machine) WillBeRemoved1(state string, position ...Position) bool {
	// TODO test
	return m.WillBeRemoved(S{state}, position...)
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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

	for state, tick := range clock {
		if m.clock[state] != tick {
			return false
		}
	}

	return true
}

// WasClock checks if the passed time has happened (or happening right now).
// Returns false if at least one state is too early.
func (m *Machine) WasClock(clock Clock) bool {
	if m.disposed.Load() {
		return false
	}
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

	for state, tick := range clock {
		if m.clock[state] < tick {
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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

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

// WasTime checks if the passed time has happened (or happening right now).
// Returns false if at least one state is too early. The
// states param is optional and can be used to check only a subset of states.
func (m *Machine) WasTime(t Time, states S) bool {
	if m.disposed.Load() {
		return false
	}
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

	if states == nil {
		states = m.stateNames
	}

	for i, tick := range t {
		if m.clock[states[i]] < tick {
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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

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
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

	if states == nil {
		states = m.stateNames
	}

	ret := ""
	for _, name := range states {

		state := m.schema[name]
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
	// TODO replace with ActiveState(states S...) S
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

// CountActive returns the number of active states from a passed list. Useful
// for state groups.
func (m *Machine) CountActive(states S) int {
	// TODO replace with ActiveState(states S...) S
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
// gets closed. Considering it's a low-level feature, it's advised to handle
// disposal via dedicated states.
func (m *Machine) HandleDispose(fn HandlerDispose) {
	m.handlersMx.Lock()
	defer m.handlersMx.Unlock()

	m.disposeHandlers = append(m.disposeHandlers, fn)
}

// ActiveStates returns a copy of the currently active states.
func (m *Machine) ActiveStates() S {
	// TODO merge with Switch, accept optional `states S...` param
	if m.disposed.Load() {
		return S{}
	}
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()

	return slices.Clone(m.activeStates)
}

// StateNames returns a SHARED copy of all the state names.
func (m *Machine) StateNames() S {
	m.schemaLock.RLock()
	defer m.schemaLock.RUnlock()

	if m.stateNamesExport == nil {
		m.stateNamesExport = slices.Clone(m.stateNames)
	}

	return m.stateNamesExport
}

// TODO docs
func (m *Machine) StateNamesMatch(re *regexp.Regexp) S {
	m.schemaLock.RLock()
	defer m.schemaLock.RUnlock()

	ret := S{}
	for _, name := range m.stateNames {
		if re.MatchString(name) {
			ret = append(ret, name)
		}
	}

	return ret
}

// Queue returns a copy of the currently active states.
func (m *Machine) Queue() []*Mutation {
	if m.disposed.Load() {
		return nil
	}
	m.queueMx.RLock()
	defer m.queueMx.RUnlock()

	return slices.Clone(m.queue)
}

// Schema returns a copy of machine's schema.
func (m *Machine) Schema() Schema {
	m.schemaLock.RLock()
	defer m.schemaLock.RUnlock()

	return maps.Clone(m.schema)
}

// SchemaVer return the current version of the schema.
func (m *Machine) SchemaVer() int {
	return len(m.StateNames())
}

// SetSchema sets the machine's schema. It will automatically call
// VerifyStates with the names param and handle EventSchemaChange if successful.
// The new schema has to be longer than the previous one (no relations-only
// changes). The length of the schema is also the version of the schema.
func (m *Machine) SetSchema(newSchema Schema, names S) error {
	m.schemaLock.Lock()
	m.queueMx.RLock()
	defer m.queueMx.RUnlock()

	if len(newSchema) <= len(m.schema) {
		m.schemaLock.Unlock()
		return fmt.Errorf("%w: new schema has to be longer than the old one",
			ErrSchema)
	}

	names = slicesUniq(names)
	if len(newSchema) != len(names) {
		m.schemaLock.Unlock()
		return fmt.Errorf("%w: new schema has to be the same length as"+
			" state names", ErrSchema)
	}

	old := m.schema
	m.schema = ParseSchema(newSchema)

	err := m.verifyStates(names)
	if err != nil {
		return err
	}
	m.schemaLock.Unlock()
	// notify the resolver
	m.resolver.NewSchema(m.schema, m.stateNames)

	// tracers
	m.tracersMx.RLock()
	for i := 0; !m.disposed.Load() && i < len(m.tracers); i++ {
		m.tracers[i].SchemaChange(m, old)
	}
	m.tracersMx.RUnlock()

	return nil
}

// EvAdd is like Add, but passed the source event as the 1st param, which
// results in traceable transitions.
func (m *Machine) EvAdd(event *Event, states S, args A) Result {
	if m.disposed.Load() || m.disposing.Load() || m.Backoff() ||
		int(m.queueLen.Load()) >= m.QueueLimit {

		return Canceled
	}
	queueTick := m.queueMutation(MutationAdd, states, args, event)
	if queueTick == uint64(Executed) {
		return Executed
	}
	m.breakpoint(states, nil)

	res := m.processQueue()
	if res == Queued {
		return Result(queueTick + 1)
	}

	return res
}

// EvAdd1 is like Add1 but passed the source event as the 1st param, which
// results in traceable transitions.
func (m *Machine) EvAdd1(event *Event, state string, args A) Result {
	return m.EvAdd(event, S{state}, args)
}

// EvRemove is like Remove but passed the source event as the 1st param, which
// results in traceable transitions.
func (m *Machine) EvRemove(event *Event, states S, args A) Result {
	if m.disposed.Load() || m.disposing.Load() || m.Backoff() ||
		int(m.queueLen.Load()) >= m.QueueLimit {

		return Canceled
	}

	// return early if none of the states is active
	m.queueMx.RLock()
	lenQueue := len(m.queue)

	// try ignoring this mutation, if none of the states is currently active
	var statesAny []S
	for _, name := range states {
		statesAny = append(statesAny, S{name})
	}

	if lenQueue == 0 && m.Transition() != nil && !m.Any(statesAny...) {
		m.queueMx.RUnlock()
		return Executed
	}

	m.queueMx.RUnlock()
	queueTick := m.queueMutation(MutationRemove, states, args, event)
	if queueTick == uint64(Executed) {
		return Executed
	}
	m.breakpoint(nil, states)

	res := m.processQueue()
	if res == Queued {
		return Result(queueTick + 1)
	}

	return res
}

// EvRemove1 is like Remove1, but passed the source event as the 1st param,
// which results in traceable transitions.
func (m *Machine) EvRemove1(event *Event, state string, args A) Result {
	return m.EvRemove(event, S{state}, args)
}

// EvAddErr is like AddErr, but passed the source event as the 1st param, which
// results in traceable transitions.
func (m *Machine) EvAddErr(event *Event, err error, args A) Result {
	return m.EvAddErrState(event, StateException, err, args)
}

// EvAddErrState is like AddErrState, but passed the source event as the 1st
// param, which results in traceable transitions.
func (m *Machine) EvAddErrState(
	event *Event, state string, err error, args A,
) Result {
	if m.disposed.Load() || m.disposing.Load() || m.Backoff() ||
		int(m.queueLen.Load()) >= m.QueueLimit || err == nil {

		return Canceled
	}
	// TODO test Err()
	m.err.Store(&err)

	var trace string
	if m.LogStackTrace {
		trace = captureStackTrace()
	}

	// error handler
	onErr := m.onError.Load()
	if onErr != nil {
		(*onErr)(m, err)
	}

	// build args
	// TODO read [event] and fill out relevant fields
	argsT := &AT{
		Err:      err,
		ErrTrace: trace,
	}

	// TODO prepend to the queue? test
	return m.EvAdd(event, S{state, StateException}, PassMerge(args, argsT))
}

// Export exports the machine state as Serialized: ID, machine time, and
// state names.
func (m *Machine) Export() *Serialized {
	// TODO return (*Serialized, Schema) to be sure it didnt race
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()
	m.queueMx.Lock()
	defer m.queueMx.Unlock()

	if !m.statesVerified.Load() {
		panic("can't export - call VerifyStates first")
	}

	t := m.time(nil)
	m.log(LogOps, "[import] exported at %d ticks", t.Sum())

	return &Serialized{
		ID:         m.id,
		Time:       t,
		QueueTick:  m.queueTick,
		StateNames: m.stateNames,
	}
}

// Import imports the machine state from Serialized. It's not safe to import
// into a machine which has already produces transitions and/or
// has telemetry connected.
func (m *Machine) Import(data *Serialized) error {
	m.activeStatesMx.RLock()
	defer m.activeStatesMx.RUnlock()
	m.queueMx.Lock()
	defer m.queueMx.Unlock()
	m.schemaLock.Lock()
	defer m.schemaLock.Unlock()

	// verify schema len
	if len(m.schema) != len(data.StateNames) {
		return fmt.Errorf("%w: importing diff state len", ErrSchema)
	}

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
	m.stateNamesExport = nil
	m.id = data.ID
	m.queueTick = data.QueueTick
	m.statesVerified.Store(true)

	// TODO trigger MachineRestoredState
	m.log(LogChanges, "[import] imported to %d ticks", t)

	return nil
}

// Index1 returns the index of a state in the machine's StateNames() list, or -1
// when not found or machine has been disposed.
func (m *Machine) Index1(state string) int {
	if m.disposed.Load() {
		return -1
	}

	return slices.Index(m.StateNames(), state)
}

// Index returns a list of state indexes in the machine's StateNames() list,
// with -1s for missing ones.
func (m *Machine) Index(states S) []int {
	if m.disposed.Load() {
		return []int{}
	}
	index := m.StateNames()
	return StatesToIndex(index, states)
}

// Resolver returns the relation resolver, used to produce target states of
// transitions.
func (m *Machine) Resolver() RelationsResolver {
	return m.resolver
}

// BindTracer binds a Tracer to the machine. Tracers can cause StateException in
// submachines, before any handlers are bound. Use the Err() getter to examine
// such errors.
func (m *Machine) BindTracer(tracer Tracer) error {
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

	m.tracers = append(m.tracers, tracer)
	m.log(LogOps, "[tracers] bind %s", name)

	return nil
}

// DetachTracer tries to remove a tracer from the machine. Returns an error in
// case the tracer wasn't bound.
func (m *Machine) DetachTracer(tracer Tracer) error {
	if m.disposing.Load() {
		return nil
	}
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("DetachTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

	for i, t := range m.tracers {
		if t == tracer {
			// TODO check
			m.tracers = slices.Delete(m.tracers, i, i+1)
			m.log(LogOps, "[tracers] detach %s", name)

			return nil
		}
	}

	return errors.New("tracer not bound")
}

// Tracers return a copy of currenty attached tracers.
func (m *Machine) Tracers() []Tracer {
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	return slices.Clone(m.tracers)
}

// OnError is the most basic error handler, useful for machines without any
// handlers.
func (m *Machine) OnError(fn HandlerError) {
	m.onError.Store(&fn)
}

// Backoff is true in case the machine had a recent HandlerDeadline. During a
// backoff, all mutations will be [Canceled].
func (m *Machine) Backoff() bool {
	last := m.LastHandlerDeadline.Load()
	return last != nil && time.Since(*last) < m.HandlerBackoff
}

// OnChange is the most basic state-change handler, useful for machines without
// any handlers.
func (m *Machine) OnChange(fn HandlerChange) {
	// TODO #262
	panic("OnChange not implemented")
}

func (m *Machine) SetGroups(groups any, optStates States) {
	m.schemaLock.Lock()
	defer m.schemaLock.Unlock()
	list := map[string][]int{}
	order := []string{}
	index := m.stateNames

	// add all the groups
	// TODO recursive for inherited groups
	val := reflect.ValueOf(groups)
	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		kind := field.Type.Kind()
		if kind != reflect.Slice {
			continue
		}
		name := field.Name
		value := val.Field(i).Interface()
		if states, ok := value.(S); ok {
			list[name] = StatesToIndex(index, states)
			order = append(order, name)
		}
	}

	// add all the schemas (nested)

	// state schema structure
	if optStates != nil {
		groups, order2 := optStates.StateGroups()
		for _, name := range order2 {
			list[name] = groups[name]
			order = append(order, name)
		}
	}

	m.groups = list
	m.groupsOrder = order
}

func (m *Machine) SetGroupsString(groups map[string]S, order []string) {
	m.schemaLock.Lock()
	defer m.schemaLock.Unlock()
	list := map[string][]int{}

	for name, states := range groups {
		list[name] = StatesToIndex(m.stateNames, states)
	}

	m.groups = list
	m.groupsOrder = order
}

func (m *Machine) Groups() (map[string][]int, []string) {
	m.schemaLock.Lock()
	defer m.schemaLock.Unlock()

	return m.groups, m.groupsOrder
}
