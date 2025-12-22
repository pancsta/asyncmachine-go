package rpc

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

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
	indexStateCtx   am.IndexStateCtx
	indexWhen       am.IndexWhen
	indexWhenTime   am.IndexWhenTime
	// TODO indexWhenArgs
	// indexWhenArgs am.IndexWhenArgs
	whenDisposed   chan struct{}
	tracers        []am.Tracer
	tracersMx      sync.RWMutex
	handlers       []*handler
	handlersMx     sync.Mutex
	parentId       string
	tags           []string
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
}

var ssNS = states.NetSourceStates

var _ am.Api = &NetworkMachine{}

// NewNetworkMachine creates a new instance of a NetworkMachine.
func NewNetworkMachine(
	ctx context.Context, id string, conn NetMachConn, schema am.Schema,
	stateNames am.S, parent *am.Machine, tags []string, filterMutations bool,
) (*NetworkMachine, *NetMachInternal, error) {
	// validate
	if ctx == nil {
		return nil, nil, errors.New("ctx cannot be nil")
	}
	if schema != nil && len(schema) != len(stateNames) {
		return nil, nil, errors.New(
			"schema and stateNames must have the same length")
	}
	if parent == nil {
		return nil, nil, errors.New("parent cannot be nil")
	}
	if tags == nil {
		tags = []string{"rpc-worker", "src-id:"}
	}

	netMach := &NetworkMachine{
		LogStackTrace: true,

		conn:            conn,
		id:              id,
		ctx:             parent.Ctx(),
		schema:          schema,
		stateNames:      stateNames,
		indexWhen:       am.IndexWhen{},
		indexStateCtx:   am.IndexStateCtx{},
		indexWhenTime:   am.IndexWhenTime{},
		whenDisposed:    make(chan struct{}),
		machTime:        make(am.Time, len(stateNames)),
		machClock:       am.Clock{},
		queueTick:       1,
		parentId:        parent.Id(),
		tags:            tags,
		filterMutations: filterMutations,
	}
	netMach.logId.Store(true)

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

// Add is [am.Api.Add].
func (m *NetworkMachine) Add(states am.S, args am.A) am.Result {
	if m.conn == nil {
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
	if !m.conn.Call(m.Ctx(), ServerAdd, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// Add1 is [am.Api.Add1].
func (m *NetworkMachine) Add1(state string, args am.A) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	return m.Add(am.S{state}, args)
}

// AddNS is a NoSync method - an efficient way for adding states, as it
// doesn't wait for, nor transfers a response. Because of which it doesn't
// update the clock. Use Sync() to update the clock after a batch of AddNS
// calls.
func (m *NetworkMachine) AddNS(states am.S, args am.A) am.Result {
	if m.conn == nil {
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
	if !m.conn.Notify(m.Ctx(), ServerAddNS, rpcArgs) {
		return am.Canceled
	}

	return am.Executed
}

// Add1NS is a single state version of AddNS.
func (m *NetworkMachine) Add1NS(state string, args am.A) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	return m.AddNS(am.S{state}, args)
}

// Remove is [am.Api.Remove].
func (m *NetworkMachine) Remove(states am.S, args am.A) am.Result {
	if m.conn == nil {
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
	if !m.conn.Call(m.Ctx(), ServerRemove, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// Remove1 is [am.Api.Remove1].
func (m *NetworkMachine) Remove1(state string, args am.A) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	return m.Remove(am.S{state}, args)
}

// Set is [am.Api.Set].
func (m *NetworkMachine) Set(states am.S, args am.A) am.Result {
	if m.conn == nil {
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
	if !m.conn.Call(m.Ctx(), ServerSet, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// AddErr is [am.Api.AddErr].
func (m *NetworkMachine) AddErr(err error, args am.A) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	return m.AddErrState(am.StateException, err, args)
}

// AddErrState is [am.Api.AddErrState].
func (m *NetworkMachine) AddErrState(
	state string, err error, args am.A,
) am.Result {
	if m.conn == nil || m.disposed.Load() {
		return am.Canceled
	}

	// keep the last err locally
	m.err.Store(&err)

	// log a stack trace to the local log
	if m.LogStackTrace {
		trace := utils.CaptureStackTrace()
		m.log(am.LogChanges, fmt.Sprintf("ERROR: %s\nTrace:\n%s", err, trace))
	}

	// build args
	argsT := &am.AT{Err: err}

	// mark errors added locally with ErrOnClient
	errStates := am.S{ssNS.ErrOnClient, state, am.StateException}
	return m.Add(errStates, am.PassMerge(args, argsT))
}

// EvAdd is [am.Api.EvAdd].
func (m *NetworkMachine) EvAdd(
	event *am.Event, states am.S, args am.A,
) am.Result {
	if m.conn == nil {
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
	if !m.conn.Call(m.Ctx(), ServerAdd, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// EvAdd1 is [am.Api.EvAdd1].
func (m *NetworkMachine) EvAdd1(
	event *am.Event, state string, args am.A,
) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	return m.EvAdd(event, am.S{state}, args)
}

// TODO EvAddNS

// EvRemove1 is [am.Api.EvRemove1].
func (m *NetworkMachine) EvRemove1(
	event *am.Event, state string, args am.A,
) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	return m.EvRemove(event, am.S{state}, args)
}

// EvRemove is [am.Api.EvRemove].
func (m *NetworkMachine) EvRemove(
	event *am.Event, states am.S, args am.A,
) am.Result {
	if m.conn == nil {
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
	if !m.conn.Call(m.Ctx(), ServerRemove, rpcArgs, resp) {
		return am.Canceled
	}

	return resp.Result
}

// EvAddErr is [am.Api.EvAddErr].
func (m *NetworkMachine) EvAddErr(
	event *am.Event, err error, args am.A,
) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	return m.EvAddErrState(event, am.StateException, err, args)
}

// EvAddErrState is [am.Api.EvAddErrState].
func (m *NetworkMachine) EvAddErrState(
	event *am.Event, state string, err error, args am.A,
) am.Result {
	if m.conn == nil {
		return am.Canceled
	}

	// keep the last err locally
	m.err.Store(&err)

	// log a stack trace to the local log
	if m.LogStackTrace {
		trace := utils.CaptureStackTrace()
		m.log(am.LogChanges, fmt.Sprintf("ERROR: %s\nTrace:\n%s", err, trace))
	}

	// build args
	argsT := &am.AT{Err: err}

	// mark errors added locally with ErrOnClient
	errStates := am.S{ssNS.ErrOnClient, state, am.StateException}
	return m.EvAdd(event, errStates, am.PassMerge(args, argsT))
}

// Toggle is [am.Api.Toggle].
func (m *NetworkMachine) Toggle(states am.S, args am.A) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	if m.Is(states) {
		return m.Remove(states, args)
	} else {
		return m.Add(states, args)
	}
}

// Toggle1 is [am.Api.Toggle1].
func (m *NetworkMachine) Toggle1(state string, args am.A) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	if m.Is1(state) {
		return m.Remove1(state, args)
	} else {
		return m.Add1(state, args)
	}
}

// EvToggle is [am.Api.EvToggle].
func (m *NetworkMachine) EvToggle(
	e *am.Event, states am.S, args am.A,
) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	if m.Is(states) {
		return m.EvRemove(e, states, args)
	} else {
		return m.EvAdd(e, states, args)
	}
}

// EvToggle1 is [am.Api.EvToggle1].
func (m *NetworkMachine) EvToggle1(
	e *am.Event, state string, args am.A,
) am.Result {
	if m.conn == nil {
		return am.Canceled
	}
	if m.Is1(state) {
		return m.EvRemove1(e, state, args)
	}
	return m.EvAdd1(e, state, args)
}

// ///// Checking (local)

// Is is [am.Api.Is].
func (m *NetworkMachine) Is(states am.S) bool {
	return m.is(states)
}

// Is1 is [am.Api.Is1].
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

// IsErr is [am.Api.IsErr].
func (m *NetworkMachine) IsErr() bool {
	return m.Is1(am.StateException)
}

// Not is [am.Api.Not].
func (m *NetworkMachine) Not(states am.S) bool {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	return m.not(states)
}

func (m *NetworkMachine) not(states am.S) bool {
	return utils.SlicesNone(m.MustParseStates(states), m.ActiveStates(nil))
}

// Not1 is [am.Api.No1].
func (m *NetworkMachine) Not1(state string) bool {
	return m.Not(am.S{state})
}

// Any is [am.Api.Any].
func (m *NetworkMachine) Any(states ...am.S) bool {
	for _, s := range states {
		if m.Is(s) {
			return true
		}
	}
	return false
}

// Any1 is [am.Api.Any1].
func (m *NetworkMachine) Any1(states ...string) bool {
	for _, s := range states {
		if m.Is1(s) {
			return true
		}
	}
	return false
}

// Has is [am.Api.Has].
func (m *NetworkMachine) Has(states am.S) bool {
	return utils.SlicesEvery(m.StateNames(), states)
}

// Has1 is [am.Api.Has1].
func (m *NetworkMachine) Has1(state string) bool {
	return m.Has(am.S{state})
}

// IsClock is [am.Api.IsClock].
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

// WasClock is [am.Api.WasClock].
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

// IsTime is [am.Api.IsTime].
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

// WasTime is [am.Api.WasTime].
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

// Switch is [am.Api.Switch].
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

// WhenErr is [am.Api.WhenErr].
func (m *NetworkMachine) WhenErr(disposeCtx context.Context) <-chan struct{} {
	return m.When([]string{am.StateException}, disposeCtx)
}

// When is [am.Api.When].
func (m *NetworkMachine) When(
	states am.S, ctx context.Context,
) <-chan struct{} {
	if m.disposed.Load() {
		return newClosedChan()
	}

	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	return m.subs.When(m.MustParseStates(states), ctx)
}

// When1 is an alias to When() for a single state.
// See When.
func (m *NetworkMachine) When1(
	state string, ctx context.Context,
) <-chan struct{} {
	return m.When(am.S{state}, ctx)
}

// WhenNot returns a channel that will be closed when all the passed states
// become inactive or the machine gets disposed.
//
// ctx: optional context that will close the channel early.
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
func (m *NetworkMachine) WhenNot1(
	state string, ctx context.Context,
) <-chan struct{} {
	return m.WhenNot(am.S{state}, ctx)
}

// WhenTime returns a channel that will be closed when all the passed states
// have passed the specified time. The time is a logical clock of the state.
// Machine time can be sourced from [Machine.Time](), or [Machine.Clock]().
//
// ctx: optional context that will close the channel early.
func (m *NetworkMachine) WhenTime(
	states am.S, times am.Time, ctx context.Context,
) <-chan struct{} {
	if m.disposed.Load() {
		return newClosedChan()
	}

	// close early on invalid
	if len(states) != len(times) {
		err := fmt.Errorf(
			"whenTime: states and times must have the same length (%s)",
			utils.J(states))
		m.AddErr(err, nil)

		return newClosedChan()
	}

	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	return m.subs.WhenTime(states, times, ctx)
}

// WhenTime1 waits till ticks for a single state equal the given value (or
// more).
//
// ctx: optional context that will close the channel early.
func (m *NetworkMachine) WhenTime1(
	state string, ticks uint64, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(am.S{state}, am.Time{ticks}, ctx)
}

// WhenTicks waits N ticks of a single state (relative to now). Uses WhenTime
// underneath.
//
// ctx: optional context that will close the channel early.moon
func (m *NetworkMachine) WhenTicks(
	state string, ticks int, ctx context.Context,
) <-chan struct{} {
	return m.WhenTime(am.S{state}, am.Time{uint64(ticks) + m.Tick(state)}, ctx)
}

// WhenQuery returns a channel that will be closed when the passed [clockCheck]
// function returns true. [clockCheck] should be a pure function and
// non-blocking.`
//
// ctx: optional context that will close the channel early.
func (m *NetworkMachine) WhenQuery(
	clockCheck func(clock am.Clock) bool, ctx context.Context,
) <-chan struct{} {
	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	return m.subs.WhenQuery(clockCheck, ctx)
}

func (m *NetworkMachine) WhenQueue(tick am.Result) <-chan struct{} {
	// locks
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	// finish early
	if m.queueTick >= uint64(tick) {
		return newClosedChan()
	}

	return m.subs.WhenQueue(tick)
}

// ///// Waiting (remote)

// WhenArgs returns a channel that will be closed when the passed state
// becomes active with all the passed args. Args are compared using the native
// '=='. It's meant to be used with async Multi states, to filter out
// a specific completion.
//
// ctx: optional context that will close the channel when done.
func (m *NetworkMachine) WhenArgs(
	state string, args am.A, ctx context.Context,
) <-chan struct{} {
	// TODO subscribe on the source via a uint8 token
	return newClosedChan()
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

// StateNames returns a copy of all the state names.
func (m *NetworkMachine) StateNames() am.S {
	m.schemaMx.Lock()
	defer m.schemaMx.Unlock()

	return slices.Clone(m.stateNames)
}

func (m *NetworkMachine) StateNamesMatch(re *regexp.Regexp) am.S {
	ret := am.S{}
	for _, name := range m.StateNames() {
		if re.MatchString(name) {
			ret = append(ret, name)
		}
	}

	return ret
}

// ActiveStates returns a copy of the currently active states.
func (m *NetworkMachine) ActiveStates(states am.S) am.S {
	active := *m.activeStates.Load()
	if states == nil {
		return slices.Clone(active)
	}

	ret := make(am.S, 0, len(states))
	for _, state := range active {
		if slices.Contains(active, state) {
			ret = append(ret, state)
		}
	}

	return ret
}

// Tick returns the current tick for a given state.
func (m *NetworkMachine) Tick(state string) uint64 {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	return m.tick(state)
}

func (m *NetworkMachine) tick(state string) uint64 {
	// TODO validate
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
// Context cancels when the state has been de-activated, or right away,
// if it isn't currently active.
//
// State contexts are used to check state expirations and should be checked
// often inside goroutines.
// TODO log reader
func (m *NetworkMachine) NewStateCtx(state string) context.Context {
	// TODO reuse existing ctxs
	m.clockMx.Lock()
	defer m.clockMx.Unlock()

	if _, ok := m.indexStateCtx[state]; ok {
		return m.indexStateCtx[state].Ctx
	}

	v := am.CtxValue{
		Id:    m.id,
		State: state,
		Tick:  m.machClock[state],
	}
	stateCtx, cancel := context.WithCancel(context.WithValue(m.Ctx(),
		am.CtxKey, v))

	// cancel early
	if !m.is(am.S{state}) {
		// TODO decision msg
		cancel()
		return stateCtx
	}

	binding := &am.CtxBinding{
		Ctx:    stateCtx,
		Cancel: cancel,
	}

	// add an index
	m.indexStateCtx[state] = binding
	m.log(am.LogOps, "[ctx:new] %s", state)

	return stateCtx
}

// ///// MISC

// Log logs is a local logger.
func (m *NetworkMachine) Log(msg string, args ...any) {
	m.log(am.LogExternal, msg, args...)
}

func (m *NetworkMachine) SemLogger() am.SemLogger {
	return m.semLogger
}

// StatesVerified returns true if the state names have been ordered
// using VerifyStates.
func (m *NetworkMachine) StatesVerified() bool {
	return true
}

// Ctx return worker's root context.
func (m *NetworkMachine) Ctx() context.Context {
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

// ParentId returns the id of the parent machine (if any).
func (m *NetworkMachine) ParentId() string {
	return m.parentId
}

// Tags returns machine's tags, a list of unstructured strings without spaces.
func (m *NetworkMachine) Tags() []string {
	return m.tags
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
		ret += fmt.Sprintf("%s:%d", state, m.machTime[idx])
	}

	return ret + ")"
}

// StringAll returns a one line representation of all the states, with their
// clock values. Inactive states are in square brackets.
// Eg: (Foo:1 Bar:3)[Baz:2]
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
			ret += fmt.Sprintf("%s:%d", state, m.machTime[idx])
			continue
		}

		if ret2 != "[" {
			ret2 += " "
		}
		ret2 += fmt.Sprintf("%s:%d", state, m.machTime[idx])
	}

	return ret + ") " + ret2 + "]"
}

// Inspect returns a multi-line string representation of the machine (states,
// relations, clock).
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
		ret += fmt.Sprintf("%s %s\n"+
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

	out := fmt.Sprintf(msg, args...)
	logger := m.semLogger.Logger()
	if logger != nil {
		logger(level, msg, args...)
	} else {
		fmt.Println(out)
	}

	m.logEntriesLock.Lock()
	defer m.logEntriesLock.Unlock()

	m.logEntries = append(m.logEntries, &am.LogEntry{
		Level: level,
		Text:  out,
	})
}

// MustParseStates parses the states and returns them as a list.
// Panics when a state is not defined. It's an usafe equivalent of VerifyStates.
func (m *NetworkMachine) MustParseStates(states am.S) am.S {
	m.schemaMx.Lock()
	defer m.schemaMx.Unlock()

	// check if all states are defined in m.Schema
	for _, s := range states {
		if !slices.Contains(m.stateNames, s) {
			panic(fmt.Sprintf("state %s is not defined for %s (via %s)", s,
				m.remoteId, m.id))
		}
	}

	return utils.SlicesUniq(states)
}

// Index1 returns the index of a state in the machine's StateNames() list.
func (m *NetworkMachine) Index1(state string) int {
	return slices.Index(m.StateNames(), state)
}

func (m *NetworkMachine) Index(states am.S) []int {
	ret := make([]int, len(states))
	for i, state := range states {
		ret[i] = m.Index1(state)
	}

	return ret
}

// Dispose disposes the machine and all its emitters. You can wait for the
// completion of the disposal with `<-mach.WhenDisposed`.
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
// disposed. Requires bound handlers. Use Machine.Disposed in case no handlers
// have been bound.
func (m *NetworkMachine) WhenDisposed() <-chan struct{} {
	return m.whenDisposed
}

// Export exports the machine state: id, time and state names.
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
	}, am.CloneSchema(m.schema), nil
}

// Schema returns a copy of machine's state structure.
func (m *NetworkMachine) Schema() am.Schema {
	return m.schema
}

// BindHandlers is [am.Api.BindHandlers].
//
// NetworkMachine supports only pipe handlers (final ones, without negotiation).
func (m *NetworkMachine) BindHandlers(handlers any) error {
	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(handlers).Elem().Name()
	m.handlersMx.Lock()
	defer m.handlersMx.Unlock()

	h := newHandler(handlers, name, &v)

	// TODO race
	// old := m.getHandlers(false)
	// m.setHandlers(false, append(old, h))

	m.handlers = append(m.handlers, h)
	if name != "" {
		m.log(am.LogOps, "[handlers] bind %s", name)
	} else {
		// index for anon handlers
		// TODO race
		m.log(am.LogOps, "[handlers] bind %d", len(m.handlers))
	}

	return nil
}

// HasHandlers is [am.Api.HasHandlers].
func (m *NetworkMachine) HasHandlers() bool {
	// TODO lock
	// w.handlersLock.Lock()
	// defer w.handlersLock.Unlock()

	return len(m.handlers) > 0
}

// DetachHandlers is [am.Api.DetachHandlers].
func (m *NetworkMachine) DetachHandlers(handlers any) error {
	old := m.handlers

	for _, h := range old {
		if h.h == handlers {
			m.handlers = utils.SlicesWithout(old, h)
			// TODO
			// h.dispose()

			return nil
		}
	}

	return errors.New("handlers not bound")
}

// BindTracer is [am.Machine.BindTracer].
//
// NetworkMachine tracers cannot mutate synchronously, as network machines
// don't have a queue and WILL deadlock when nested.
func (m *NetworkMachine) BindTracer(tracer am.Tracer) error {
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

	m.tracers = append(m.tracers, tracer)
	m.log(am.LogOps, "[tracers] bind %s", name)

	return nil
}

// DetachTracer is [am.Api.DetachTracer].
func (m *NetworkMachine) DetachTracer(tracer am.Tracer) error {
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
			m.log(am.LogOps, "[tracers] detach %s", name)

			return nil
		}
	}

	return errors.New("tracer not bound")
}

// Tracers is [am.Api.Tracers].
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

// handle runs a single handler method (currently only pipes).
func (m *NetworkMachine) handle(h *handler, i int, state, suffix string) {
	h.mx.Lock()
	methodName := state + suffix
	e := am.NewEvent(nil, m)
	e.Name = methodName
	e.MachineId = m.remoteId

	// TODO descriptive name
	handlerName := strconv.Itoa(i) + ":" + h.name

	if m.semLogger.Level() >= am.LogEverything {
		emitterId := utils.TruncateStr(handlerName, 15)
		emitterId = utils.PadString(strings.ReplaceAll(
			emitterId, " ", "_"), 15, "_")
		m.log(am.LogEverything, "[handle:%-15s] %s", emitterId, methodName)
	}

	// cache
	_, ok := h.missingCache[methodName]
	if ok {
		h.mx.Unlock()

		return
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
			return
		}
		h.methodCache[methodName] = method
	}

	// call the handler (pipes dont block)
	m.log(am.LogOps, "[handler:%d] %s", i, methodName)

	// tracers
	// m.tracersMx.RLock()
	tx := m.t.Load()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// call TODO should go-fork to avoid nested deadlocks?
	_ = method.Call([]reflect.Value{reflect.ValueOf(e)})

	// tracers
	// m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].HandlerEnd(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// locks
	h.mx.Unlock()
}

func (m *NetworkMachine) processSubscriptions(
	activated, deactivated am.S, clockBefore am.Clock,
) {
	// lock
	m.clockMx.RLock()

	// collect
	toCancel := m.subs.ProcessStateCtx(deactivated)
	toClose := slices.Concat(
		m.subs.ProcessWhen(activated, deactivated),
		m.subs.ProcessWhenTime(clockBefore),
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

// AddBreakpoint1 is [am.Api.AddBreakpoint1].
func (m *NetworkMachine) AddBreakpoint1(
	added string, removed string, strict bool,
) {
	// TODO
}

// AddBreakpoint is [am.Api.AddBreakpoint].
func (m *NetworkMachine) AddBreakpoint(
	added am.S, removed am.S, strict bool,
) {
	// TODO
}

// Groups is [am.Api.Groups].
func (m *NetworkMachine) Groups() (map[string][]int, []string) {
	// TODO maybe sync along with schema?
	return nil, nil
}

// CanAdd is [am.Api.CanAdd].
func (m *NetworkMachine) CanAdd(states am.S, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

// CanAdd1 is [am.Api.CanAdd1].
func (m *NetworkMachine) CanAdd1(state string, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

// CanRemove is [am.Api.CanRemove].
func (m *NetworkMachine) CanRemove(states am.S, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

// CanRemove1 is [am.Api.CanRemove1].
func (m *NetworkMachine) CanRemove1(state string, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

// Transition is [am.Machine.Transition].
func (m *NetworkMachine) Transition() *am.Transition {
	return m.t.Load()
}

// QueueLen is [am.Api.QueueLen].
func (m *NetworkMachine) QueueLen() uint16 {
	// TODO implement outbound throttling
	return 0
}

func (m *NetworkMachine) queueFlush() {
	m.subs.QueueFlush()
}

// QueueTick is [am.Api.QueueTick].
func (m *NetworkMachine) QueueTick() uint64 {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	return m.queueTick
}

// MachineTick is [am.Api.MachineTick].
func (m *NetworkMachine) MachineTick() uint32 {
	m.clockMx.RLock()
	defer m.clockMx.RUnlock()

	return m.machTick
}

// ParseStates is [am.Api.ParseStates].
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

// OnDispose is [am.Api.OnDispose].
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
