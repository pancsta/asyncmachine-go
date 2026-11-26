package rpc

import (
	"context"
	"errors"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/pancsta/asyncmachine-go/internal/nofmt"
	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

type ClockUpdateFunc func(now am.Time, qTick uint64, machTick uint32)

// NetMachConn is a mutation interface for NetworkMachine instances.
// It's meant to be (optionally) injected by whatever creates network machines,
// so they can communicate with the server (or another source).
type NetMachConn interface {
	// TODO take event for tracing errors
	Call(ctx context.Context, method ServerMethod, args any, resp any) bool
	Notify(ctx context.Context, method ServerMethod, args any) bool
}

// NetMachInternal are internal methods of a NetworkMachine instance returned
// by the constructor.
type NetMachInternal struct {
	nm *NetworkMachine
}

func (i *NetMachInternal) UpdateClock(
	now am.Time, qTick uint64, machTick uint32,
) {
	i.nm.updateClock(now, qTick, machTick)
}

func (i *NetMachInternal) Lock() {
	i.nm.clockMx.Lock()
}

func (i *NetMachInternal) Unlock() {
	i.nm.clockMx.Unlock()
}

// NetworkMachine is a subset of `pkg/machine#Machine` for RPC. Lacks the queue
// and other local methods. Most methods are clock-based, thus executed locally.
// NetworkMachine implements [am.Api].
//
// # Example
//
//	c.NetMach.Add1("Foo", nil)
//	<-c.NetMach.When1("Bar", nil)
type NetworkMachine struct {
	// If true, the machine will print all exceptions to stdout. Default: true.
	// Requires an ExceptionHandler binding and Machine.PanicToException set.
	LogStackTrace bool

	// internal

	// RPC client parenting this NetworkMachine. If nil, the machine is read-only
	// and won't allow for mutations / network calls.
	conn NetMachConn
	// remoteId is the ID of the remote state machine.
	remoteId string

	// net machine internal

	errInternal chan error
	// embed and reuse subscriptions
	subs     *am.Subscriptions
	id       string
	ctx      context.Context
	disposed atomic.Bool
	// Err is the last error that occurred on this netowrk instance.
	err    atomic.Pointer[error]
	schema am.Schema
	// external lock
	clockMx         sync.RWMutex
	schemaMx        sync.RWMutex
	machTime        am.Time
	machClock       am.Clock
	queueTick       uint64
	stateNames      am.S
	activeStates    atomic.Pointer[am.S]
	activeStatesDbg am.S
	// TODO indexWhenArgs
	// indexWhenArgs am.IndexWhenArgs
	whenDisposed   chan struct{}
	tracers        []am.Tracer
	tracersMx      sync.RWMutex
	handlers       []*handler
	handlersMx     sync.Mutex
	parentId       string
	tags           atomic.Pointer[[]string]
	logLevel       atomic.Pointer[am.LogLevel]
	logger         atomic.Pointer[am.LoggerFn]
	logEntriesLock sync.Mutex
	logEntries     []*am.LogEntry
	// If true, logs will start with the machine's id (5 chars).
	// Default: true.
	logId     atomic.Bool
	semLogger *semLogger
	// execQueue executed handlers and tracers
	machTick        uint32
	t               atomic.Pointer[am.Transition]
	disposeHandlers []am.HandlerDispose
	filterMutations bool
	nextHandlerNum  int
}

var ssNS = states.StateSourceStates

var _ am.Api = &NetworkMachine{}

// NewNetworkMachine creates a new instance of a NetworkMachine.
func NewNetworkMachine(
	ctx context.Context, id string, conn NetMachConn, schema am.Schema,
	stateNames am.S, parent am.Api, tags []string, filterMutations bool,
) (*NetworkMachine, *NetMachInternal, error) {
	//

	// validate
	if ctx == nil {
		return nil, nil, errors.New("ctx cannot be nil")
	}
	if schema != nil && len(schema) != len(stateNames) {
		return nil, nil, errors.New(
			"schema and stateNames must have the same length",
		)
	}
	if parent == nil {
		return nil, nil, errors.New("parent cannot be nil")
	}
	if tags == nil {
		tags = []string{states.TagRpcNetmach, states.TagKeySrcId}
	}

	netMach := &NetworkMachine{
		LogStackTrace: true,

		errInternal:     make(chan error, 0),
		conn:            conn,
		id:              id,
		ctx:             parent.Context(),
		schema:          schema,
		stateNames:      stateNames,
		whenDisposed:    make(chan struct{}),
		machTime:        make(am.Time, len(stateNames)),
		machClock:       am.Clock{},
		queueTick:       1,
		parentId:        parent.Id(),
		filterMutations: filterMutations,
	}
	netMach.tags.Store(&tags)
	netMach.logId.Store(true)
	close(netMach.errInternal)

	// init clock
	for _, state := range stateNames {
		netMach.machClock[state] = 0
	}
	netMach.subs = am.NewSubscriptionManager(netMach, netMach.machClock,
		netMach.is, netMach.not, netMach.log)
	netMach.semLogger = &semLogger{mach: netMach}
	lvl := am.LogNothing
	netMach.logLevel.Store(&lvl)
	netMach.activeStates.Store(&am.S{})
	parent.OnDispose(func(id string, ctx context.Context) {
		netMach.Dispose()
	})

	// ret a priv func to update the clock of this instance
	return netMach, &NetMachInternal{netMach}, nil
}

// ///// RPC methods

// ///// Mutations (remote)

// Add activates a list of states in the machine, returning the result of the
// transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
//
// # Example
//
//	mach.Add(am.S{"Foo", "Bar"}, nil)
//	// (Foo:1 Bar:1)
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) Add(states am.S, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}

	m.MustParseStates(states)

	// reject early
	if m.filterMutations && !m.mutAccepted(am.MutationAdd, states) {
		return am.Canceled
	}

	// call rpc
	resp := &MsgSrvMutation{}
	rpcArgs := &MsgCliMutation{
		States: amhelp.StatesToIndexes(m.StateNames(), states),
		Args:   args,
	}
	if !m.conn.Call(m.Context(), ServerAdd, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// Add1 is a shorthand method to add a single state with the passed args.
// See Add().
//
// # Example
//
//	mach.Add1("Foo", nil)
//	// (Foo:1)
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) Add1(state string, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	return m.Add(am.S{state}, args)
}

// AddNS is a NoSync method - an efficient way for adding states, as it
// doesn't wait for, nor transfers a response. Because of which it doesn't
// update the clock. Use Sync() to update the clock after a batch of AddNS
// calls.
func (m *NetworkMachine) AddNS(states am.S, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}

	m.MustParseStates(states)

	// reject early
	if m.filterMutations && !m.mutAccepted(am.MutationAdd, states) {
		return am.Canceled
	}

	// call rpc
	rpcArgs := &MsgCliMutation{
		States: amhelp.StatesToIndexes(m.StateNames(), states),
		Args:   args,
	}
	if !m.conn.Notify(m.Context(), ServerAddNS, rpcArgs) {
		return am.Canceled
	}

	return am.Executed
}

// Add1NS is a single state version of AddNS.
func (m *NetworkMachine) Add1NS(state string, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	return m.AddNS(am.S{state}, args)
}

// Remove deactivates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
//
// # Example
//
//	mach.Remove(am.S{"Foo"}, nil)
//	// ()
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) Remove(states am.S, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}

	m.MustParseStates(states)

	// reject early
	if m.filterMutations && !m.mutAccepted(am.MutationRemove, states) {
		return am.Canceled
	}

	// call rpc
	resp := &MsgSrvMutation{}
	rpcArgs := &MsgCliMutation{
		States: amhelp.StatesToIndexes(m.StateNames(), states),
		Args:   args,
	}
	if !m.conn.Call(m.Context(), ServerRemove, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// Remove1 is [am.Machine.Remove] for a single state.
//
// # Example
//
//	mach.Remove1("Foo", nil)
//	// ()
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) Remove1(state string, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	return m.Remove(am.S{state}, args)
}

// Set deactivates a list of states in the machine, returning the result of
// the transition (Executed, Canceled, Queued).
// Like every mutation method, it will resolve relations and trigger handlers.
//
// # Example
//
//	mach.Set(am.S{"Foo"}, nil)
//	// (Foo:1)
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) Set(states am.S, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}

	m.MustParseStates(states)

	// reject early
	if m.filterMutations && !m.mutAccepted(am.MutationSet, states) {
		return am.Canceled
	}

	// call rpc
	resp := &MsgSrvMutation{}
	rpcArgs := &MsgCliMutation{
		States: amhelp.StatesToIndexes(m.StateNames(), states),
		Args:   args,
	}
	if !m.conn.Call(m.Context(), ServerSet, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// AddErr is a dedicated method to add the StateException state with the passed
// error and optional arguments.
// Like every mutation method, it will resolve relations and trigger handlers.
// AddErr produces a stack trace of the error, if LogStackTrace is enabled.
func (m *NetworkMachine) AddErr(err error, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	return m.AddErrState(am.StateException, err, args)
}

// AddErrState adds a dedicated error state, along with the build in
// StateException state. Like every mutation method, it will resolve relations
// and trigger handlers. AddErrState produces a stack trace of the error, if
// LogStackTrace is enabled.
func (m *NetworkMachine) AddErrState(
	state string, err error, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() ||
		err == context.Canceled {

		return am.Canceled
	}

	// keep the last err locally
	m.err.Store(&err)

	// log a stack trace to the local log
	if m.LogStackTrace {
		trace := utils.CaptureStackTrace()
		m.log(am.LogChanges, nofmt.Sprintf("ERROR: %s\nTrace:\n%s", err, trace))
	}

	// build args
	args2 := am.Pass(&am.AException{
		Err: err,
	})

	errStates := am.S{state, am.StateException}
	// mark errors added locally with ErrOnClient
	if m.Has1(ssNS.ErrOnClient) {
		errStates = append(errStates, ssNS.ErrOnClient)
	}

	return m.Add(errStates, am.PassMerge(args, args2))
}

// EvAdd is like Add, but passed the source event as the 1st param, which
// results in traceable transitions.
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) EvAdd(
	event *am.Event, states am.S, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}

	m.MustParseStates(states)

	// TODO mutation filtering (req schema)

	// call rpc
	resp := &MsgSrvMutation{}
	rpcArgs := &MsgCliMutation{
		States: amhelp.StatesToIndexes(m.StateNames(), states),
		Args:   args,
		Event:  event.Export(),
	}
	if !m.conn.Call(m.Context(), ServerAdd, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// EvAdd1 is like Add1 but passed the source event as the 1st param, which
// results in traceable transitions.
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) EvAdd1(
	event *am.Event, state string, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	return m.EvAdd(event, am.S{state}, args)
}

// TODO EvAddNS

// EvRemove1 is like Remove1, but passed the source event as the 1st param,
// which results in traceable transitions.
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) EvRemove1(
	event *am.Event, state string, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	return m.EvRemove(event, am.S{state}, args)
}

// EvRemove is like Remove but passed the source event as the 1st param, which
// results in traceable transitions.
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) EvRemove(
	event *am.Event, states am.S, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}

	m.MustParseStates(states)

	// TODO mutation filtering (req schema)

	// call rpc
	resp := &MsgSrvMutation{}
	rpcArgs := &MsgCliMutation{
		States: amhelp.StatesToIndexes(m.StateNames(), states),
		Args:   args,
		Event:  event.Export(),
	}
	if !m.conn.Call(m.Context(), ServerRemove, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// EvAddErr is like AddErr, but passed the source event as the 1st param, which
// results in traceable transitions.
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) EvAddErr(
	event *am.Event, err error, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	return m.EvAddErrState(event, am.StateException, err, args)
}

// EvAddErrState is like AddErrState, but passed the source event as the 1st
// param, which results in traceable transitions.
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) EvAddErrState(
	event *am.Event, state string, err error, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() ||
		err == context.Canceled {

		return am.Canceled
	}

	// keep the last err locally
	m.err.Store(&err)

	// log a stack trace to the local log
	if m.LogStackTrace {
		trace := utils.CaptureStackTrace()
		m.log(am.LogChanges, nofmt.Sprintf("ERROR: %s\nTrace:\n%s", err, trace))
	}

	// build args
	args2 := am.Pass(&am.AException{
		Err: err,
	})

	errStates := am.S{state, am.StateException}
	// mark errors added locally with ErrOnClient
	if m.Has1(ssNS.ErrOnClient) {
		errStates = append(errStates, ssNS.ErrOnClient)
	}

	return m.EvAdd(event, errStates, am.PassMerge(args, args2))
}

// Toggle deactivates a list of states in case all are active, or activates
// them otherwise. Returns the result of the transition (Executed, Queued,
// Canceled).
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) Toggle(states am.S, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	if m.Is(states) {
		return m.Remove(states, args)
	} else {
		return m.Add(states, args)
	}
}

// Toggle1 activates or deactivates a single state, depending on its current
// state. Returns the result of the transition (Executed, Queued, Canceled).
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) Toggle1(state string, args am.A) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	if m.Is1(state) {
		return m.Remove1(state, args)
	} else {
		return m.Add1(state, args)
	}
}

// EvToggle is a traced version of [am.Machine.Toggle].
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) EvToggle(
	e *am.Event, states am.S, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	if m.Is(states) {
		return m.EvRemove(e, states, args)
	} else {
		return m.EvAdd(e, states, args)
	}
}

// EvToggle1 is a traced version of [am.Machine.Toggle1].
//
// # Blocking
//
// This method is blocking and waits for the remote result.
func (m *NetworkMachine) EvToggle1(
	e *am.Event, state string, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}
	if m.Is1(state) {
		return m.EvRemove1(e, state, args)
	}
	return m.EvAdd1(e, state, args)
}

// ///// Checking (local)

// Is checks if all the passed states are currently active.
//
//	machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
//	machine.Add(am.S{"Foo"}, nil)
//	machine.Is(am.S{"Foo"}) // true
//	machine.Is(am.S{"Foo", "Bar"}) // false
func (m *NetworkMachine) Is(states am.S) bool {
	return m.is(states)
}

// Is1 is [am.Machine.Is] for a single state.
func (m *NetworkMachine) Is1(state string) bool {
	return m.Is(am.S{state})
}

func (m *NetworkMachine) is(states am.S) bool {
	activeStates := m.ActiveStates(nil)
	for _, state := range m.MustParseStates(states) {
		if !slices.Contains(activeStates, state) {
			return false
		}
	}

	return true
}

// IsErr checks if the machine has the StateException state currently active.
func (m *NetworkMachine) IsErr() bool {
	return m.Is1(am.StateException)
}

// Not checks if **none** of the passed states are currently active.
//
//	machine.StringAll()
//	// -> ()[A:0 B:0 C:0 D:0]
//	machine.Add(am.S{"A", "B"}, nil)
//
//	// not(A) and not(C)
//	machine.Not(am.S{"A", "C"})
//	// -> false
//
//	// not(C) and not(D)
//	machine.Not(am.S{"C", "D"})
//	// -> true
func (m *NetworkMachine) Not(states am.S) bool {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	return m.not(states)
}

func (m *NetworkMachine) not(states am.S) bool {
	return utils.SlicesNone(m.MustParseStates(states), m.ActiveStates(nil))
}

// Not1 is [am.Machine.Not] for a single state.
func (m *NetworkMachine) Not1(state string) bool {
	return m.Not(am.S{state})
}

// Any is a group call to Is, returns true if any of the params return true
// from Is.
//
//	machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
//	machine.Add(am.S{"Foo"}, nil)
//	// is(Foo, Bar) or is(Bar)
//	machine.Any(am.S{"Foo", "Bar"}, am.S{"Bar"}) // false
//	// is(Foo) or is(Bar)
//	machine.Any(am.S{"Foo"}, am.S{"Bar"}) // true
func (m *NetworkMachine) Any(states ...am.S) bool {
	for _, s := range states {
		if m.Is(s) {
			return true
		}
	}
	return false
}

// Any1 is group call to [am.Machine.Is1], returns true if any of the params
// return true from [am.Machine.Is1].
func (m *NetworkMachine) Any1(states ...string) bool {
	for _, s := range states {
		if m.Is1(s) {
			return true
		}
	}
	return false
}

// Has return true is passed states are registered in the machine. Useful for
// checking if a machine implements a specific state set.
func (m *NetworkMachine) Has(states am.S) bool {
	return utils.SlicesEvery(m.StateNames(), states)
}

// Has1 is shorthand for Has. It returns true if the passed state is
// registered in the machine.
func (m *NetworkMachine) Has1(state string) bool {
	return m.Has(am.S{state})
}

// IsClock checks if the machine has changed since the passed
// clock. Returns true if at least one state has changed.
func (m *NetworkMachine) IsClock(clock am.Clock) bool {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	for state, tick := range clock {
		if m.machTime[m.Index1(state)] != tick {
			return false
		}
	}

	return true
}

// WasClock checks if the passed time has happened (or happening right now).
// Returns false if at least one state is too early.
func (m *NetworkMachine) WasClock(clock am.Clock) bool {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	for state, tick := range clock {
		if m.machTime[m.Index1(state)] < tick {
			return false
		}
	}

	return true
}

// IsTime checks if the machine has changed since the passed
// time (list of ticks). Returns true if at least one state has changed. The
// states param is optional and can be used to check only a subset of states.
func (m *NetworkMachine) IsTime(t am.Time, states am.S) bool {
	m.MustParseStates(states)
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	index := m.StateNames()
	if states == nil {
		states = index
	}

	for i, tick := range t {
		if m.machTime[slices.Index(index, states[i])] != tick {
			return false
		}
	}

	return true
}

// WasTime checks if the passed time has happened (or happening right now).
// Returns false if at least one state is too early. The
// states param is optional and can be used to check only a subset of states.
func (m *NetworkMachine) WasTime(t am.Time, states am.S) bool {
	m.MustParseStates(states)
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	index := m.StateNames()
	if states == nil {
		states = index
	}

	for i, tick := range t {
		if m.machTime[slices.Index(index, states[i])] < tick {
			return false
		}
	}

	return true
}

// Switch returns the first state from the passed list that is currently active,
// making it handy for switch statements. Useful for state groups.
//
//	switch mach.Switch(ss.GroupPlaying) {
//	case "Playing":
//	case "Paused":
//	case "Stopped":
//	}
func (m *NetworkMachine) Switch(groups ...am.S) string {
	activeStates := m.ActiveStates(nil)
	for _, states := range groups {
		for _, state := range states {
			if slices.Contains(activeStates, state) {
				return state
			}
		}
	}

	return ""
}

// ///// Waiting (local)

// WhenErr returns a channel that will be closed when the machine is in the
// StateException state.
//
// ctx: optional context that will close the channel early.
//
// # Example
//
//	<-mach.WhenErr(nil)
func (m *NetworkMachine) WhenErr(disposeCtx context.Context) <-chan struct{} {
	return m.When([]string{am.StateException}, disposeCtx)
}

// When returns a channel that will be closed when all the passed states
// become active or the machine gets disposed.
//
// ctx: optional context that will close the channel early.
//
// # Example
//
//	<-mach.When(am.S{"Foo", "Bar"}, nil)
func (m *NetworkMachine) When(
	states am.S, ctx context.Context,
) <-chan struct{} {
	if m.disposed.Load() {
		return m.subs.Closed
	}

	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	return m.subs.When(m.MustParseStates(states), ctx)
}

// When1 is an alias to When() for a single state.
// See When.
//
// # Example
//
//	<-mach.When1("Foo", nil)
func (m *NetworkMachine) When1(
	state string, ctx context.Context,
) <-chan struct{} {
	return m.When(am.S{state}, ctx)
}

// WhenNot returns a channel that will be closed when all the passed states
// become inactive or the machine gets disposed.
//
// ctx: optional context that will close the channel early.
//
// # Example
//
//	<-mach.WhenNot(am.S{"Foo"}, nil)
func (m *NetworkMachine) WhenNot(
	states am.S, ctx context.Context,
) <-chan struct{} {
	if m.disposed.Load() {
		ch := make(chan struct{})
		close(ch)
		return ch
	}

	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	return m.subs.WhenNot(m.MustParseStates(states), ctx)
}

// WhenNot1 is an alias to WhenNot() for a single state.
// See WhenNot.
//
// # Example
//
//	<-mach.WhenNot1("Foo", nil)
func (m *NetworkMachine) WhenNot1(
	state string, ctx context.Context,
) <-chan struct{} {
	return m.WhenNot(am.S{state}, ctx)
}

// WhenTime returns a channel that will be closed when all the passed states
// have passed the specified time. The time is a logical clock of the state.
// Machine time can be sourced from [am.Machine.Time](), or
// [am.Machine.Clock]().
//
// ctx: optional context that will close the channel early.
//
// # Example
//
//	<-mach.WhenTime(am.S{"Foo", "Bar"}, am.Time{6, 10}, nil)
func (m *NetworkMachine) WhenTime(
	states am.S, times am.Time, ctx context.Context,
) <-chan struct{} {
	if m.disposed.Load() {
		return m.subs.Closed
	}

	// close early on invalid
	if len(states) != len(times) {
		err := nofmt.Errorf(
			"whenTime: states and times must have the same length (%s)",
			utils.J(states),
		)
		m.AddErr(err, nil)

		return m.subs.Closed
	}

	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	return m.subs.WhenTime(states, times, ctx)
}

// WhenTime1 waits till ticks for a single state equal the given value (or
// higher).
//
// ctx: optional context that will close the channel early.
//
// # Example
//
//	<-mach.WhenTime1("Foo", 6, nil)
func (m *NetworkMachine) WhenTime1(
	state string, ticks uint64, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(am.S{state}, am.Time{ticks}, ctx)
}

// WhenTicks waits N ticks of a single state (relative to now). Uses WhenTime
// underneath.
//
// ctx: optional context that will close the channel early.
//
// # Example
//
//	<-mach.WhenTicks("Foo", 2, nil)
func (m *NetworkMachine) WhenTicks(
	state string, ticks int, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(am.S{state}, am.Time{uint64(ticks) + m.Tick(state)}, ctx)
}

// WhenNextActive waits until the state becomes active, excluding the current
// activity.
//
// ctx: optional context that will close the channel early.
//
// # Example
//
//	<-mach.WhenNextActive("Foo", nil)
func (m *NetworkMachine) WhenNextActive(
	state string, ctx context.Context,
) <-chan struct{} {
	// TODO add to API
	return m.WhenTicks(state, am.NextActiveIn(m.Tick(state)), ctx)
}

// WhenQuery returns a channel that will be closed when the passed [clockCheck]
// function returns true. [clockCheck] should be a pure function and
// non-blocking.
//
// ctx: optional context that will close the channel early.
//
// # Example
//
//	<-mach.WhenQuery(func(c am.Clock) bool {
//		// Foo activated >5 times and Bar activated twice as much
//		return c["Foo"] >= 10 && c["Bar"] >= 2*c["Foo"]
//	}, nil)
func (m *NetworkMachine) WhenQuery(
	clockCheck func(clock am.Clock) bool, ctx context.Context,
) <-chan struct{} {
	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	return m.subs.WhenQuery(clockCheck, ctx)
}

// WhenQueue waits until the passed queueTick gets processed.
//
// # Example
//
//	<-mach.WhenQueue(mach.Add1("Foo", nil))
func (m *NetworkMachine) WhenQueue(tick am.Result) <-chan struct{} {
	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	// finish early
	if m.queueTick >= uint64(tick) {
		return m.subs.Closed
	}

	return m.subs.WhenQueue(tick)
}

// WhenTimeSum waits till machine time equals the given value (or higher).
//
// ctx: optional context that will close the channel early.
//
// # Example
//
//	<-mach.WhenTimeSum(16, nil)
func (m *NetworkMachine) WhenTimeSum(
	mtime uint64, ctx context.Context,
) <-chan struct{} {
	//

	if m.disposed.Load() {
		return m.subs.Closed
	}

	// close early on too late
	if mtime <= m.Time(nil).Sum(nil) {
		return m.subs.Closed
	}

	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	return m.subs.WhenTimeSum(mtime, ctx)
}

// ///// Waiting (remote)

// WhenArgs returns a channel that will be closed when the passed state
// becomes active with all the passed args. ArgsBase are compared using
// the native
// '=='. It's meant to be used with async Multi states, to filter out
// a specific call.
//
// ctx: optional context that will close the channel when handler loop ends.
//
// # Example
//
//	<-mach.WhenArgs("Foo", am.A{"ID": 123}, nil)
func (m *NetworkMachine) WhenArgs(
	state string, args am.A, ctx context.Context,
) <-chan struct{} {
	// TODO subscribe on the source via a uint8 token
	return m.subs.Closed
}

// ///// Getters (remote)

// Err returns the last error.
func (m *NetworkMachine) Err() error {
	err := m.err.Load()
	if err == nil {
		return nil
	}
	return *err
}

// ///// Getters (local)

// StateNames returns a SHARED copy of all the state names.
func (m *NetworkMachine) StateNames() am.S {
	m.schemaMx.Lock()
	defer m.schemaMx.Unlock()

	return slices.Clone(m.stateNames)
}

// ActiveStates returns a copy of the currently active states when states is
// nil, optionally limiting the results to a subset of states.
func (m *NetworkMachine) ActiveStates(states am.S) am.S {
	active := *m.activeStates.Load()
	if states == nil {
		return slices.Clone(active)
	}

	ret := make(am.S, 0, len(states))
	for _, state := range states {
		if slices.Contains(active, state) {
			ret = append(ret, state)
		}
	}

	return ret
}

// Tick return the current tick for a given state.
func (m *NetworkMachine) Tick(state string) uint64 {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	return m.tick(state)
}

func (m *NetworkMachine) tick(state string) uint64 {
	m.MustParseStates(am.S{state})
	return m.machClock[state]
}

// Clock returns current machine's clock, a state-keyed map of ticks. If states
// are passed, only the ticks of the passed states are returned.
func (m *NetworkMachine) Clock(states am.S) am.Clock {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()
	index := m.StateNames()

	if states == nil {
		states = index
	}

	ret := am.Clock{}
	for _, state := range states {
		ret[state] = m.machClock[state]
	}

	return ret
}

// Time returns machine's time, a list of ticks per state. Returned value
// includes the specified states, or all the states if nil.
func (m *NetworkMachine) Time(states am.S) am.Time {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	return m.time(states)
}

func (m *NetworkMachine) time(states am.S) am.Time {
	index := m.StateNames()
	if states == nil {
		states = index
	}

	ret := am.Time{}
	for _, state := range states {
		idx := slices.Index(index, state)
		ret = append(ret, m.machTime[idx])
	}

	return ret
}

// NewStateCtx returns a new sub-context, bound to the current clock's tick of
// the passed state.
//
// Context cancels when the state has been deactivated, or right away,
// if it isn't currently active.
//
// State contexts are used to check state expirations and should be checked
// often inside goroutines.
//
// [e]: optional [am.Event] to bind to this context.
func (m *NetworkMachine) NewStateCtx(
	state string, event ...*am.Event,
) context.Context {
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	var mTime am.Time
	if os.Getenv(am.EnvAmDebug) != "" {
		mTime = m.time(nil)
	}

	if e := am.OptEv(event); e != nil {
		return am.EvToCtx(m.subs.NewStateCtx(state, mTime), e)
	}

	return m.subs.NewStateCtx(state, mTime)
}

// ///// MISC

// Log logs an [extern] message unless LogNothing is set.
// Optionally redirects to a custom logger from [SemLogger.SetLogger].
func (m *NetworkMachine) Log(msg string, args ...any) {
	m.log(am.LogExternal, msg, args...)
}

// SemLogger returns the semantic logger of the machine
func (m *NetworkMachine) SemLogger() am.SemLogger {
	return m.semLogger
}

// StatesVerified returns true if the state names have been ordered
// using VerifyStates.
func (m *NetworkMachine) StatesVerified() bool {
	return true
}

// Context returns the machine's context. This context lives longer than
// the parent context and allows for graceful shutdown. This is the context
// which should be used to create submachines. See also
// [am.Machine.DisposeTimeout].
func (m *NetworkMachine) Context() context.Context {
	return m.ctx
}

// ContextParent returns the original context passed to the constructor.
func (m *NetworkMachine) ContextParent() context.Context {
	return m.ctx
}

// Id returns the machine's id.
func (m *NetworkMachine) Id() string {
	return m.id
}

// RemoteId returns the ID of the remote state machine.
func (m *NetworkMachine) RemoteId() string {
	return m.remoteId
}

// ParentId returns the ID of the parent machine (if any).
func (m *NetworkMachine) ParentId() string {
	return m.parentId
}

// Tags returns machine's tags, a list of unstructured strings without spaces.
func (m *NetworkMachine) Tags() []string {
	tags := m.tags.Load()
	if tags == nil {
		return nil
	}
	return slices.Clone(*tags)
}

// SetTags updates the machine's tags with the provided slice of strings.
func (m *NetworkMachine) SetTags(tags []string) {
	tags = slices.Clone(tags)
	m.tags.Store(&tags)
	m.semLogger.SetTags()
}

// String returns a one line representation of the currently active states,
// with their clock values. Inactive states are omitted.
// Eg: (Foo:1 Bar:3)
func (m *NetworkMachine) String() string {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	index := m.StateNames()
	active := m.ActiveStates(nil)
	ret := "("
	for _, state := range index {
		if !slices.Contains(active, state) {
			continue
		}

		if ret != "(" {
			ret += " "
		}
		idx := slices.Index(index, state)
		ret += nofmt.Sprintf("%s:%d", state, m.machTime[idx])
	}

	return ret + ")"
}

// StringAll returns a one line representation of all the states, with their
// clock values. Inactive states are in square brackets.
//
//	(Foo:1 Bar:3) [Baz:2]
func (m *NetworkMachine) StringAll() string {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	index := m.StateNames()
	activeStates := m.ActiveStates(nil)
	ret := "("
	ret2 := "["
	for _, state := range index {
		idx := slices.Index(index, state)

		if slices.Contains(activeStates, state) {
			if ret != "(" {
				ret += " "
			}
			ret += nofmt.Sprintf("%s:%d", state, m.machTime[idx])
			continue
		}

		if ret2 != "[" {
			ret2 += " "
		}
		ret2 += nofmt.Sprintf("%s:%d", state, m.machTime[idx])
	}

	return ret + ") " + ret2 + "]"
}

// Inspect returns a multi-line string representation of the machine (states,
// relations, clocks).
// states: param for ordered or partial results.
func (m *NetworkMachine) Inspect(states am.S) string {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	index := m.StateNames()
	if states == nil {
		states = index
	}

	activeStates := m.ActiveStates(nil)
	ret := ""
	for _, name := range states {

		state := m.schema[name]
		active := "0"
		if slices.Contains(activeStates, name) {
			active = "1"
		}

		idx := slices.Index(index, name)
		ret += nofmt.Sprintf("%s %s\n"+
			"    |Tick     %d\n", active, name, m.machTime[idx])
		if state.Auto {
			ret += "    |Auto     true\n"
		}
		if state.Multi {
			ret += "    |Multi    true\n"
		}
		if state.Add != nil {
			ret += "    |Add      " + utils.J(state.Add) + "\n"
		}
		if state.Require != nil {
			ret += "    |Require  " + utils.J(state.Require) + "\n"
		}
		if state.Remove != nil {
			ret += "    |Remove   " + utils.J(state.Remove) + "\n"
		}
		if state.After != nil {
			ret += "    |After    " + utils.J(state.After) + "\n"
		}
	}

	return ret
}

func (m *NetworkMachine) log(level am.LogLevel, msg string, args ...any) {
	if m.semLogger.Level() < level {
		return
	}

	prefix := ""
	if m.logId.Load() {
		id := m.id
		if len(id) > 5 {
			id = id[:5]
		}
		prefix = "[" + id + "] "
		msg = prefix + msg
	}

	out := nofmt.Sprintf(msg, args...)
	logger := m.semLogger.Logger()
	if logger != nil {
		logger(level, msg, args...)
	} else {
		nofmt.Println(out)
	}

	m.logEntriesLock.Lock()
	defer m.logEntriesLock.Unlock()

	m.logEntries = append(m.logEntries, &am.LogEntry{
		Level: level,
		Text:  out,
	})
}

// MustParseStates parses a list of states, panicking on
// states which are not defined for the machine, and removing duplicates.
func (m *NetworkMachine) MustParseStates(states am.S) am.S {
	m.schemaMx.Lock()
	defer m.schemaMx.Unlock()

	// check if all states are defined in m.Schema
	for _, s := range states {
		if !slices.Contains(m.stateNames, s) {
			panic(nofmt.Sprintf("state %s is not defined for %s (via %s)", s,
				m.remoteId, m.id))
		}
	}

	return utils.SlicesUniq(states)
}

// Index1 returns the index of a state in the machine's StateNames() list, or -1
// when not found or machine has been disposed.
func (m *NetworkMachine) Index1(state string) int {
	return slices.Index(m.StateNames(), state)
}

// Index returns a list of state indexes in the machine's StateNames() list,
// with -1s for missing ones.
func (m *NetworkMachine) Index(states am.S) []int {
	ret := make([]int, len(states))
	for i, state := range states {
		ret[i] = m.Index1(state)
	}

	return ret
}

// Dispose disposes the machine and all its emitters. You can wait for the
// completion of the disposal with `<-mach.WhenDisposed()`. It's advised to use
// Dispose() func from pkg/helpers instead, which handles Disposed state mixing.
func (m *NetworkMachine) Dispose() {
	if !m.disposed.CompareAndSwap(false, true) {
		return
	}

	// tracers
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()
	for _, t := range m.tracers {
		t.MachineDispose(m.id)
	}

	// run doDispose handlers
	// TODO timeouts?
	for _, fn := range m.disposeHandlers {
		fn(m.id, m.ctx)
	}

	utils.CloseSafe(m.whenDisposed)

	// TODO push remotely?
}

// IsDisposed returns true if the machine has been disposed.
func (m *NetworkMachine) IsDisposed() bool {
	return m.disposed.Load()
}

// WhenDisposed returns a channel that will be closed when the machine is
// disposed.
func (m *NetworkMachine) WhenDisposed() <-chan struct{} {
	return m.whenDisposed
}

// Export exports the machine state as Serialized: ID, machine time, and
// state names.
func (m *NetworkMachine) Export() (*am.Serialized, am.Schema, error) {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()
	m.schemaMx.RLock()
	defer m.schemaMx.RUnlock()

	m.log(am.LogChanges, "[import] exported at %d ticks", m.time(nil))

	return &am.Serialized{
		ID:          m.id,
		Time:        m.time(nil),
		StateNames:  m.StateNames(),
		MachineTick: m.machTick,
		QueueTick:   m.queueTick,
	}, m.schema.Clone(), nil
}

// Schema returns a copy of machine's schema.
func (m *NetworkMachine) Schema() am.Schema {
	return m.schema
}

// HandlersBindMaps is like [am.Machine.HandlersBind] but accepts maps of
// handlers to bypass reflecting structs. It may lead to higher performance.
// Returns a binding ID.
func (m *NetworkMachine) HandlersBindMaps(
	negotiations map[string]am.HandlerNegotiation,
	finals map[string]am.HandlerFinal, opts ...am.BindOpts,
) (string, error) {
	m.handlersMx.Lock()
	defer m.handlersMx.Unlock()

	o := optBindOpts(opts)
	// TODO name from stack trace
	name := "anon" + amhelp.RandId(4)
	if o.Id != "" {
		name = idRe.ReplaceAllString(o.Id, "")
	}

	h := m.newHandlerMap(negotiations, finals)
	h.id = name
	h.num = m.nextHandlerNum
	h.opts = &o
	m.nextHandlerNum++
	m.handlers = append(m.handlers, h)
	m.log(am.LogOps, "[handlers] bind %d:%s", h.num, name)

	return name, nil
}

// newHandlerMap creates a map-based [handler] binding.
func (m *NetworkMachine) newHandlerMap(
	negotiations map[string]am.HandlerNegotiation,
	finals map[string]am.HandlerFinal,
) *handler {
	return &handler{
		isMap:        true,
		finals:       finals,
		negotiations: negotiations,
	}
}

// handleMap runs a map-based pipe handler. Must be called with h.mx locked.
func (m *NetworkMachine) handleMap(
	h *handler, state, suffix string, e *am.Event, handlerName string,
) {
	methodName := state + suffix

	var fn am.HandlerFinal
	var neg am.HandlerNegotiation
	if suffix == am.SuffixState || suffix == am.SuffixEnd {
		fn = h.finals[methodName]
	} else {
		neg = h.negotiations[methodName]
	}
	if fn == nil && neg == nil {
		return
	}

	m.log(am.LogOps, "[handler:%d] %s", h.num, methodName)

	tx := m.t.Load()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(tx, handlerName, methodName)
	}
	if fn != nil {
		fn(e)
	} else {
		neg(e)
	}
	for i := range m.tracers {
		m.tracers[i].HandlerEnd(tx, handlerName, methodName)
	}
}

// TODO move
// optBindOpts will return the first [BindOpts] from a list.
func optBindOpts(args []am.BindOpts) am.BindOpts {
	if len(args) > 0 {
		return args[0]
	}
	return am.BindOpts{}
}

// TODO move
var idRe = regexp.MustCompile(`[^a-zA-Z0-9-_]+`)

// Handlers returns the IDs of bound handlers.
func (m *NetworkMachine) Handlers() []string {
	// TODO lock, support id
	// w.handlersLock.Lock()
	// defer w.handlersLock.Unlock()

	ret := make([]string, 0, len(m.handlers))
	for _, h := range m.handlers {
		ret = append(ret, h.id)
	}

	return ret
}

// HandlersDetach detaches previously bound machine handlers.
func (m *NetworkMachine) HandlersDetach(bindingId string) error {
	old := m.handlers

	for _, h := range old {
		if h.id == bindingId {
			m.handlers = utils.SlicesWithout(old, h)
			// TODO
			// h.dispose()

			return nil
		}
	}

	return errors.New("handlers not bound")
}

// BindHandlers is deprecated, use [am.Api.HandlersBind].
func (m *NetworkMachine) BindHandlers(handlers any,
	opts ...am.BindOpts,
) (string, error) {
	return m.HandlersBind(handlers, opts...)
}

// DetachHandlers is deprecated, use [am.Api.HandlersDetach].
func (m *NetworkMachine) DetachHandlers(bindingId string) error {
	return m.DetachHandlers(bindingId)
}

// TracerBind binds a Tracer to the machine. Tracers can cause StateException in
// submachines, before any handlers are bound. Use the Err() getter to examine
// such errors.
//
// # NetworkMachine
//
// NetworkMachine tracers cannot mutate synchronously, as network machines
// don't have a queue and WILL deadlock when nested.
func (m *NetworkMachine) TracerBind(tracer am.Tracer) (string, error) {
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return "", errors.New("BindTracer expects a pointer to a struct")
	}

	m.tracers = append(m.tracers, tracer)
	m.log(am.LogOps, "[tracers] bind %s", tracer.TracerId())

	return tracer.TracerId(), nil
}

// TracerDetach tries to remove a tracer from the machine. Returns an error in
// case the tracer wasn't bound.
func (m *NetworkMachine) TracerDetach(id string) error {
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	for i, t := range m.tracers {
		if t.TracerId() == id {
			// TODO check
			m.tracers = slices.Delete(m.tracers, i, i+1)
			m.log(am.LogOps, "[tracers] detach %s", id)

			return nil
		}
	}

	return errors.New("tracer not bound")
}

// BindTracer is deprecated, use [NetworkMachine.TracerBind].
func (m *NetworkMachine) BindTracer(tracer am.Tracer) (string, error) {
	return m.TracerBind(tracer)
}

// DetachTracer is deprecated, use [NetworkMachine.TracerDetach].
func (m *NetworkMachine) DetachTracer(id string) error {
	return m.TracerDetach(id)
}

// Tracers return a copy of currently attached tracers.
func (m *NetworkMachine) Tracers() []am.Tracer {
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	return slices.Clone(m.tracers)
}

// updateClock updates the clock of this NetworkMachine and requires a locked
// clockMx, which is then unlocked by this method.
func (m *NetworkMachine) updateClock(
	now am.Time, qTick uint64, machTick uint32,
) {
	// TODO require mutType and called states for SyncAllMutations and handlers

	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	timeBefore := m.machTime
	clockBefore := maps.Clone(m.machClock)

	activeBefore := m.ActiveStates(nil)
	activeNow := am.S{}
	index := m.StateNames()
	for i, state := range index {
		if am.IsActiveTick(now[i]) {
			activeNow = append(activeNow, state)
		}
	}
	activeNow = m.activateRequired(activeNow)
	activated := am.StatesDiff(activeNow, activeBefore)
	deactivated := am.StatesDiff(activeBefore, activeNow)

	tx := &am.Transition{
		MachApi: m,
		Id:      utils.RandId(8),

		TimeBefore: timeBefore,
		TimeAfter:  now,
		Mutation: &am.Mutation{
			// TODO use add and remove when all ticks passed
			Type:   am.MutationSet,
			Called: m.Index(activeNow),
			Args:   nil,
			IsAuto: false,
		},
		LogEntries:    m.logEntries,
		TargetIndexes: m.Index(activeNow),
	}
	tx.IsCompleted.Store(true)
	tx.IsSettled.Store(true)
	// TODO may not be true for qTicks-only updates
	tx.IsAccepted.Store(true)
	m.t.Store(tx)
	m.logEntries = nil

	// call tracers
	for _, t := range m.tracers {
		t.TransitionInit(tx)
	}
	for _, t := range m.tracers {
		t.TransitionStart(tx)
	}

	// set active states
	m.machTime = now
	for idx, tick := range m.machTime {
		m.machClock[index[idx]] = tick
	}
	m.machTick = machTick
	// the local queue ticks later than the new one, all queue subs are invalid
	if m.queueTick > qTick {
		m.log(am.LogOps, "[queueTick] flushing (%d to %s)", m.queueTick, qTick)
		m.queueFlush()
	}
	m.queueTick = qTick
	m.activeStates.Store(&activeNow)
	m.activeStatesDbg = activeNow

	// handlers
	m.processHandlers(activated, deactivated)

	// unlock for tracers (always locked by the caller)
	m.clockMx.Unlock()

	for _, t := range m.tracers {
		// TODO dbg tracing doesnt show up in the UI?
		t.TransitionEnd(tx)
	}

	// subscriptions
	m.processSubscriptions(activated, deactivated, clockBefore)
	m.t.Store(nil)
}

// activateRequired will fake required states (when not all synced and
// schema present)
func (m *NetworkMachine) activateRequired(active am.S) am.S {
	// skip for schema-less netmachs
	if m.schema == nil {
		return active
	}

	// locks
	m.schemaMx.RLock()
	defer m.schemaMx.RUnlock()

	ret := slices.Clone(active)
	visited := make(map[string]bool)
	var visit func(string)
	visit = func(node string) {
		if !visited[node] {
			visited[node] = true
			for _, reqState := range m.schema[node].Require {
				ret = append(ret, reqState)
				visit(reqState)
			}
		}
	}

	// recurse on all the active states
	for _, state := range active {
		visit(state)
	}

	return utils.SlicesUniq(ret)
}

func (m *NetworkMachine) getHandlers(locked bool) []*handler {
	if !locked {
		m.handlersMx.Lock()
		defer m.handlersMx.Unlock()
	}

	return slices.Clone(m.handlers)
}

func (m *NetworkMachine) processHandlers(activated, deactivated am.S) {
	// no changes
	if len(activated)+len(deactivated) == 0 {
		return
	}

	for i, h := range m.getHandlers(false) {
		if h == nil {
			continue
		}

		// TODO ensure multi states covered
		for _, state := range activated {
			m.handle(h, i, state, am.SuffixState)
		}
		for _, state := range deactivated {
			m.handle(h, i, state, am.SuffixEnd)
		}

		// global handler
		m.handle(h, i, am.StateAny, am.SuffixState)
	}
}

func (m *NetworkMachine) processSubscriptions(
	activated, deactivated am.S, clockBefore am.Clock,
) {
	// lock
	m.clockMx.RLock()

	// collect
	toCancel := m.subs.ProcessStateCtx(activated, deactivated)
	toClose := slices.Concat(
		m.subs.ProcessWhen(activated, deactivated),
		m.subs.ProcessWhenTime(clockBefore),
		m.subs.ProcessWhenTimeSum(),
		m.subs.ProcessWhenQueue(m.queueTick),
		m.subs.ProcessWhenQuery(),
	)

	// unlock
	m.clockMx.RUnlock()

	// close outside the critical zone
	for _, cancel := range toCancel {
		cancel()
	}
	for _, ch := range toClose {
		close(ch)
	}
}

// AddBreakpoint1 is a single state version of [am.Machine.AddBreakpoint].
func (m *NetworkMachine) AddBreakpoint1(
	added string, removed string, strict bool,
) {
	// TODO
}

// AddBreakpoint adds a breakpoint for an outcome of mutation (added and
// removed states) checked by mutation equality. Once such a mutation happens,
// a log message will be printed out. We can set an IDE's breakpoint on this
// line and see the mutation's sync stack trace. If
// [am.Machine.LogStackTrace] is set, the stack trace will be printed out as
// well. Many breakpoints can be added, but none removed.
//
// Breakpoints are useful to find the caller of a mutation, but don't work with
// [am.Machine.Set].
//
// strict: strict skips already active / inactive (for strict of diff equality).
func (m *NetworkMachine) AddBreakpoint(
	added am.S, removed am.S, strict bool,
) {
	// TODO
}

// Groups returns the machine's state groups as indexes, along with the
// group order. See [am.Machine.SetGroups].
func (m *NetworkMachine) Groups() (map[string][]int, []string) {
	// TODO maybe sync along with schema?
	return nil, nil
}

// CanAdd checks if [states] can be added and returns Executed or
// [AT.CheckDone] if a dry run mutation passes. Useful for reducing failed
// negotiations.
func (m *NetworkMachine) CanAdd(states am.S, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

// CanAdd1 is [am.Machine.CanAdd] for a single state.
func (m *NetworkMachine) CanAdd1(state string, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

// CanRemove checks if [states] can be removed and returns Executed or
// [AT.CheckDone] if a dry run mutation passes. Useful for reducing failed
// negotiations.
func (m *NetworkMachine) CanRemove(states am.S, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

// CanRemove1 is [am.Machine.CanRemove] for a single state.
func (m *NetworkMachine) CanRemove1(state string, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

// Transition returns the current transition, if any.
func (m *NetworkMachine) Transition() *am.Transition {
	return m.t.Load()
}

// IsLocal returns true for *am.Machine and false for *arpc.NetworkMachine.
func (m *NetworkMachine) IsLocal() bool {
	return false
}

// ErrInternal returns a channel which receives handler and eval timeout errors,
// so they can be handled accordingly without passing through mutations.
// This method always returns a closed channel for [am.Api.IsLocal] == false.
func (m *NetworkMachine) ErrInternal() <-chan error {
	return m.errInternal
}

// QueueLen returns the number of mutations currently in the queue.
//
// # NetworkMachine
//
// Always returns 0, outbound throttling is not implemented.
func (m *NetworkMachine) QueueLen() uint16 {
	// TODO implement outbound throttling
	return 0
}

func (m *NetworkMachine) queueFlush() {
	m.subs.QueueFlush()
}

// QueueTick is the number of times the queue has processed a mutation. Starts
// from [1], for easy comparison with [Result].
func (m *NetworkMachine) QueueTick() uint64 {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	return m.queueTick
}

// MachineTick is the number of times the machine has been started. Each start
// means an empty queue. Only set by [am.Machine.Import].
func (m *NetworkMachine) MachineTick() uint32 {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	return m.machTick
}

// ActivatedAt returns the machine time of the last activation of the passed
// state (even if the state isnt currently active).
//
// # NetworkMachine
//
// Not implemented yet, panics.
func (m *NetworkMachine) ActivatedAt(state string) uint64 {
	// TOOD copy test
	if m.disposed.Load() {
		return 0
	}
	panic("not implemented yet")
}

// ParseStates parses a list of states, removing unknown ones and duplicates.
// Use [am.Machine.Has] and [am.Machine.Has1] to check if a state is defined
// for the machine.
func (m *NetworkMachine) ParseStates(states am.S) am.S {
	m.schemaMx.Lock()
	defer m.schemaMx.Unlock()

	// check if all states are defined in the schema
	seen := make(map[string]struct{})
	dups := false
	for i := range states {
		if _, ok := m.schema[states[i]]; !ok {
			continue
		}
		if _, ok := seen[states[i]]; !ok {
			seen[states[i]] = struct{}{}
		} else {
			// mark as duplicated
			dups = true
		}
	}

	if dups {
		return utils.SlicesUniq(states)
	}
	return slices.Collect(maps.Keys(seen))
}

// OnDispose adds a function to be called after the machine gets disposed.
// These functions will run synchronously just before WhenDisposed() channel
// gets closed. Considering it's a low-level feature, it's advised to handle
// disposal via dedicated states.
func (m *NetworkMachine) OnDispose(fn am.HandlerDispose) {
	m.handlersMx.Lock()
	defer m.handlersMx.Unlock()

	m.disposeHandlers = append(m.disposeHandlers, fn)
}

func (m *NetworkMachine) mutAccepted(
	mutType am.MutationType, states am.S,
) bool {
	if m.schema == nil {
		return true
	}

	// TODO adapt [am.RelationsResolver] to filter here
	return true
}

// debug
// func (w *NetworkMachine) QueueDump() []string {
// 	return nil
// }
