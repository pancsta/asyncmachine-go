package rpc

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// Worker is a subset of `pkg/machine#Machine` for RPC. Lacks the queue and
// other local methods. Most methods are clock-based, thus executed locally.
type Worker struct {
	// RPC client parenting this Worker. If nil, worker is read-only and won't
	// allow for mutations / network calls.
	c *Client
	// remoteId is the ID of the remote state machine.
	remoteId string

	// machine

	// embed and reuse subscriptions
	subs          *am.Subscriptions
	id            string
	ctx           context.Context
	disposed      atomic.Bool
	err           error
	schema        am.Schema
	clockMx       sync.RWMutex
	schemaMx      sync.RWMutex
	machTime      am.Time
	machClock     am.Clock
	queueTick     uint64
	stateNames    am.S
	activeState   atomic.Pointer[am.S]
	indexStateCtx am.IndexStateCtx
	indexWhen     am.IndexWhen
	indexWhenTime am.IndexWhenTime
	// TODO indexWhenArgs
	// indexWhenArgs am.IndexWhenArgs
	whenDisposed chan struct{}
	// TODO bind
	tracers        []am.Tracer
	tracersMx      sync.Mutex
	handlers       []*remoteHandler
	parentId       string
	tags           []string
	logLevel       atomic.Pointer[am.LogLevel]
	logger         atomic.Pointer[am.LoggerFn]
	logEntriesLock sync.Mutex
	logEntries     []*am.LogEntry
	semLogger      *semLogger
	// execQueue executed handlers and tracers
	// TODO tracers should be synchronous like in pkg/machine
	execQueue errgroup.Group
}

// Worker implements MachineApi
var _ am.Api = &Worker{}

// NewWorker creates a new instance of a Worker.
func NewWorker(
	ctx context.Context, id string, c *Client, schema am.Schema, stateNames am.S,
	parent *am.Machine, tags []string,
) (*Worker, error) {
	// validate
	if ctx == nil {
		return nil, errors.New("ctx cannot be nil")
	}
	if schema == nil {
		return nil, errors.New("schema cannot be nil")
	}
	if len(schema) != len(stateNames) {
		return nil, errors.New("schema and stateNames must have the same length")
	}
	if parent == nil {
		return nil, errors.New("parent cannot be nil")
	}
	if tags == nil {
		tags = []string{"rpc-worker", "src-id:"}
	}

	w := &Worker{
		c:             c,
		id:            id,
		ctx:           parent.Ctx(),
		schema:        schema,
		stateNames:    stateNames,
		indexWhen:     am.IndexWhen{},
		indexStateCtx: am.IndexStateCtx{},
		indexWhenTime: am.IndexWhenTime{},
		whenDisposed:  make(chan struct{}),
		machTime:      make(am.Time, len(stateNames)),
		machClock:     am.Clock{},
		queueTick:     1,
		parentId:      parent.Id(),
		tags:          tags,
	}

	// init clock
	for _, state := range stateNames {
		w.machClock[state] = 0
	}
	w.subs = am.NewSubscriptionManager(w, w.machClock, w.is, w.not, w.log)
	w.semLogger = &semLogger{mach: w}
	lvl := am.LogNothing
	w.logLevel.Store(&lvl)
	w.activeState.Store(&am.S{})
	parent.HandleDispose(func(id string, ctx context.Context) {
		w.Dispose()
	})

	// init queue
	w.execQueue.SetLimit(1)

	return w, nil
}

// ///// RPC methods

// Sync requests fresh clock values from the remote machine. Useful to call
// after a batch of no-sync methods, eg AddNS.
func (w *Worker) Sync() am.Time {
	if w.c == nil {
		return nil
	}

	w.c.Mach.Log("Sync")

	// call rpc
	resp := &RespSync{}
	ok := w.c.callFailsafe(w.c.Mach.Ctx(), ServerSync.Value,
		w.TimeSum(nil), resp)
	if !ok {
		return nil
	}

	// validate
	if len(resp.Time) > 0 && len(resp.Time) != len(w.StateNames()) {
		AddErrRpcStr(nil, w.c.Mach, "wrong clock len")
		return nil
	}

	// process if time is returned
	if len(resp.Time) > 0 {
		w.c.updateClock(nil, resp.Time, resp.QueueTick)
	}

	return w.machTime
}

// ///// Mutations (remote)

// Add activates a list of states in the machine, returning the result of the
// transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (w *Worker) Add(states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}

	w.MustParseStates(states)

	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
	}
	if !w.c.callFailsafe(w.Ctx(), ServerAdd.Value, rpcArgs, resp) {
		return am.Canceled
	}

	// TODO validate resp?

	// process
	w.c.updateClock(resp.Clock, nil, 0)

	return resp.Result
}

// Add1 is a shorthand method to add a single state with the passed args.
func (w *Worker) Add1(state string, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	return w.Add(am.S{state}, args)
}

// AddNS is a NoSync method - an efficient way for adding states, as it
// doesn't wait for, nor transfers a response. Because of which it doesn't
// update the clock. Use Sync() to update the clock after a batch of AddNS
// calls.
func (w *Worker) AddNS(states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}

	w.c.log("AddNS")
	w.MustParseStates(states)

	// call rpc
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
	}
	if !w.c.notifyFailsafe(w.Ctx(), ServerAddNS.Value, rpcArgs) {
		return am.Canceled
	}

	return am.Executed
}

// Add1NS is a single state version of AddNS.
func (w *Worker) Add1NS(state string, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	return w.AddNS(am.S{state}, args)
}

// Remove de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (w *Worker) Remove(states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}

	w.MustParseStates(states)

	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
	}
	if !w.c.callFailsafe(w.Ctx(), ServerRemove.Value, rpcArgs, resp) {
		return am.Canceled
	}

	// TODO validate resp?

	// process
	w.c.updateClock(resp.Clock, nil, 0)

	return resp.Result
}

// Remove1 is a shorthand method to remove a single state with the passed args.
// See Remove().
func (w *Worker) Remove1(state string, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	return w.Remove(am.S{state}, args)
}

// Set de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (w *Worker) Set(states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}

	w.MustParseStates(states)

	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
	}
	if !w.c.callFailsafe(w.Ctx(), ServerSet.Value, rpcArgs, resp) {
		return am.Canceled
	}

	// TODO validate resp?

	// process
	w.c.updateClock(resp.Clock, nil, 0)

	return resp.Result
}

// AddErr is a dedicated method to add the Exception state with the passed
// error and optional arguments.
// Like every mutation method, it will resolve relations and trigger handlers.
// AddErr produces a stack trace of the error, if LogStackTrace is enabled.
func (w *Worker) AddErr(err error, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	return w.AddErrState(am.StateException, err, args)
}

// AddErrState adds a dedicated error state, along with the build in Exception
// state.
// Like every mutation method, it will resolve relations and trigger handlers.
// AddErrState produces a stack trace of the error, if LogStackTrace is enabled.
func (w *Worker) AddErrState(state string, err error, args am.A) am.Result {
	if w.c == nil || w.disposed.Load() {
		return am.Canceled
	}

	w.MustParseStates(am.S{state})
	// TODO remove once remote errors are implemented
	w.err = err

	// TODO stack traces, use am.AT
	// var trace string
	// if m.LogStackTrace {
	// 	trace = captureStackTrace()
	// }

	// build args
	if args == nil {
		args = am.A{}
	} else {
		args = maps.Clone(args)
	}
	args["err"] = err
	// args["err.trace"] = trace

	// mark errors added locally with ErrOnClient
	return w.Add(am.S{ssS.ErrOnClient, state, am.StateException}, args)
}

func (w *Worker) EvAdd(event *am.Event, states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}

	w.MustParseStates(states)

	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
		Event:  event.Export(),
	}
	if !w.c.callFailsafe(w.Ctx(), ServerAdd.Value, rpcArgs, resp) {
		return am.Canceled
	}

	// TODO validate resp?

	// process
	w.c.updateClock(resp.Clock, nil, 0)

	return resp.Result
}

// Add1 is a shorthand method to add a single state with the passed args.
func (w *Worker) EvAdd1(event *am.Event, state string, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	return w.EvAdd(event, am.S{state}, args)
}

func (w *Worker) EvRemove1(event *am.Event, state string, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	return w.EvRemove(event, am.S{state}, args)
}

func (w *Worker) EvRemove(event *am.Event, states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	return am.Canceled // TODO
}

func (w *Worker) EvAddErr(event *am.Event, err error, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	return am.Canceled // TODO
}

func (w *Worker) EvAddErrState(
	event *am.Event, state string, err error, args am.A,
) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	return am.Canceled // TODO
}

// Toggle deactivates a list of states in case all are active, or activates
// them otherwise. Returns the result of the transition (Executed, Queued,
// Canceled).
func (w *Worker) Toggle(states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	if w.Is(states) {
		return w.Remove(states, args)
	} else {
		return w.Add(states, args)
	}
}

// Toggle1 activates or deactivates a single state, depending on its current
// state. Returns the result of the transition (Executed, Queued, Canceled).
func (w *Worker) Toggle1(state string, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	if w.Is1(state) {
		return w.Remove1(state, args)
	} else {
		return w.Add1(state, args)
	}
}

// EvToggle is a traced version of [Machine.Toggle].
func (w *Worker) EvToggle(e *am.Event, states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	if w.Is(states) {
		return w.EvRemove(e, states, args)
	} else {
		return w.EvAdd(e, states, args)
	}
}

// EvToggle1 is a traced version of [Machine.Toggle1].
func (w *Worker) EvToggle1(e *am.Event, state string, args am.A) am.Result {
	if w.c == nil {
		return am.Canceled
	}
	if w.Is1(state) {
		return w.EvRemove1(e, state, args)
	} else {
		return w.EvAdd1(e, state, args)
	}
}

// ///// Checking (local)

// Is checks if all the passed states are currently active.
//
//	machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
//	machine.Add(S{"Foo"})
//	machine.TestIs(S{"Foo"}) // true
//	machine.TestIs(S{"Foo", "Bar"}) // false
func (w *Worker) Is(states am.S) bool {
	return w.is(states)
}

// Is1 is a shorthand method to check if a single state is currently active.
// See Is().
func (w *Worker) Is1(state string) bool {
	return w.Is(am.S{state})
}

func (w *Worker) is(states am.S) bool {
	activeStates := w.ActiveStates()
	for _, state := range w.MustParseStates(states) {
		if !slices.Contains(activeStates, state) {
			return false
		}
	}

	return true
}

// IsErr checks if the machine has the Exception state currently active.
func (w *Worker) IsErr() bool {
	return w.Is1(am.StateException)
}

// Not checks if **none** of the passed states are currently active.
//
//	machine.StringAll()
//	// -> ()[A:0 B:0 C:0 D:0]
//	machine.Add(S{"A", "B"})
//
//	// not(A) and not(C)
//	machine.TestNot(S{"A", "C"})
//	// -> false
//
//	// not(C) and not(D)
//	machine.TestNot(S{"C", "D"})
//	// -> true
func (w *Worker) Not(states am.S) bool {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	return w.not(states)
}

func (w *Worker) not(states am.S) bool {
	return utils.SlicesNone(w.MustParseStates(states), w.ActiveStates())
}

// Not1 is a shorthand method to check if a single state is currently inactive.
// See Not().
func (w *Worker) Not1(state string) bool {
	return w.Not(am.S{state})
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
func (w *Worker) Any(states ...am.S) bool {
	for _, s := range states {
		if w.Is(s) {
			return true
		}
	}
	return false
}

// Any1 is group call to Is1(), returns true if any of the params return true
// from Is1().
func (w *Worker) Any1(states ...string) bool {
	for _, s := range states {
		if w.Is1(s) {
			return true
		}
	}
	return false
}

// Has return true is passed states are registered in the machine.
func (w *Worker) Has(states am.S) bool {
	return utils.SlicesEvery(w.StateNames(), states)
}

// Has1 is a shorthand for Has. It returns true if the passed state is
// registered in the machine.
func (w *Worker) Has1(state string) bool {
	return w.Has(am.S{state})
}

// IsClock checks if the machine has changed since the passed
// clock. Returns true if at least one state has changed.
func (w *Worker) IsClock(clock am.Clock) bool {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	for state, tick := range clock {
		if w.machTime[w.Index1(state)] != tick {
			return false
		}
	}

	return true
}

// WasClock checks if the passed time has happened (or happening right now).
// Returns false if at least one state is too early.
func (w *Worker) WasClock(clock am.Clock) bool {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	for state, tick := range clock {
		if w.machTime[w.Index1(state)] < tick {
			return false
		}
	}

	return true
}

// IsTime checks if the machine has changed since the passed
// time (list of ticks). Returns true if at least one state has changed. The
// states param is optional and can be used to check only a subset of states.
func (w *Worker) IsTime(t am.Time, states am.S) bool {
	w.MustParseStates(states)
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	index := w.StateNames()
	if states == nil {
		states = index
	}

	for i, tick := range t {
		if w.machTime[slices.Index(index, states[i])] != tick {
			return false
		}
	}

	return true
}

// WasTime checks if the passed time has happened (or happening right now).
// Returns false if at least one state is too early. The
// states param is optional and can be used to check only a subset of states.
func (w *Worker) WasTime(t am.Time, states am.S) bool {
	w.MustParseStates(states)
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	index := w.StateNames()
	if states == nil {
		states = index
	}

	for i, tick := range t {
		if w.machTime[slices.Index(index, states[i])] < tick {
			return false
		}
	}

	return true
}

// Switch returns the first state from the passed list that is currently active,
// making it useful for switch statements.
//
//	switch mach.Switch(ss.GroupPlaying) {
//	case "Playing":
//	case "Paused":
//	case "Stopped":
//	}
func (w *Worker) Switch(groups ...am.S) string {
	activeStates := w.ActiveStates()
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
func (w *Worker) WhenErr(disposeCtx context.Context) <-chan struct{} {
	return w.When([]string{am.StateException}, disposeCtx)
}

// When returns a channel that will be closed when all the passed states
// become active or the machine gets disposed.
//
// ctx: optional context that will close the channel early.
func (w *Worker) When(states am.S, ctx context.Context) <-chan struct{} {
	if w.disposed.Load() {
		return newClosedChan()
	}

	// locks
	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	return w.subs.When(w.MustParseStates(states), ctx)
}

// When1 is an alias to When() for a single state.
// See When.
func (w *Worker) When1(state string, ctx context.Context) <-chan struct{} {
	return w.When(am.S{state}, ctx)
}

// WhenNot returns a channel that will be closed when all the passed states
// become inactive or the machine gets disposed.
//
// ctx: optional context that will close the channel early.
func (w *Worker) WhenNot(states am.S, ctx context.Context) <-chan struct{} {
	if w.disposed.Load() {
		ch := make(chan struct{})
		close(ch)
		return ch
	}

	// locks
	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	return w.subs.WhenNot(w.MustParseStates(states), ctx)
}

// WhenNot1 is an alias to WhenNot() for a single state.
// See WhenNot.
func (w *Worker) WhenNot1(state string, ctx context.Context) <-chan struct{} {
	return w.WhenNot(am.S{state}, ctx)
}

// WhenTime returns a channel that will be closed when all the passed states
// have passed the specified time. The time is a logical clock of the state.
// Machine time can be sourced from [Machine.Time](), or [Machine.Clock]().
//
// ctx: optional context that will close the channel early.
func (w *Worker) WhenTime(
	states am.S, times am.Time, ctx context.Context,
) <-chan struct{} {
	if w.disposed.Load() {
		return newClosedChan()
	}

	// close early on invalid
	if len(states) != len(times) {
		err := fmt.Errorf(
			"whenTime: states and times must have the same length (%s)",
			utils.J(states))
		w.AddErr(err, nil)

		return newClosedChan()
	}

	// locks
	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	return w.subs.WhenTime(states, times, ctx)
}

// WhenTime1 waits till ticks for a single state equal the given value (or
// more).
//
// ctx: optional context that will close the channel early.
func (w *Worker) WhenTime1(
	state string, ticks uint64, ctx context.Context,
) <-chan struct{} {
	return w.WhenTime(am.S{state}, am.Time{ticks}, ctx)
}

// WhenTicks waits N ticks of a single state (relative to now). Uses WhenTime
// underneath.
//
// ctx: optional context that will close the channel early.
func (w *Worker) WhenTicks(
	state string, ticks int, ctx context.Context,
) <-chan struct{} {
	return w.WhenTime(am.S{state}, am.Time{uint64(ticks) + w.Tick(state)}, ctx)
}

// WhenQuery returns a channel that will be closed when the passed [clockCheck]
// function returns true. [clockCheck] should be a pure function and
// non-blocking.`
//
// ctx: optional context that will close the channel early.
func (w *Worker) WhenQuery(
	clockCheck func(clock am.Clock) bool, ctx context.Context,
) <-chan struct{} {
	// locks
	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	return w.subs.WhenQuery(clockCheck, ctx)
}

func (w *Worker) WhenQueue(tick am.Result) <-chan struct{} {
	// locks
	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	// finish early
	if w.queueTick >= uint64(tick) {
		return newClosedChan()
	}

	return w.subs.WhenQueue(tick)
}

// ///// Waiting (remote)

// WhenArgs returns a channel that will be closed when the passed state
// becomes active with all the passed args. Args are compared using the native
// '=='. It's meant to be used with async Multi states, to filter out
// a specific completion.
//
// ctx: optional context that will close the channel when done.
func (w *Worker) WhenArgs(
	state string, args am.A, ctx context.Context,
) <-chan struct{} {
	// TODO subscribe on the source via a uint8 token
	return newClosedChan()
}

// ///// Getters (remote)

// Err returns the last error.
func (w *Worker) Err() error {
	// TODO return remote errors
	return w.err
}

// ///// Getters (local)

// StateNames returns a copy of all the state names.
func (w *Worker) StateNames() am.S {
	w.schemaMx.Lock()
	defer w.schemaMx.Unlock()

	return slices.Clone(w.stateNames)
}

func (w *Worker) StateNamesMatch(re *regexp.Regexp) am.S {
	ret := am.S{}
	for _, name := range w.StateNames() {
		if re.MatchString(name) {
			ret = append(ret, name)
		}
	}

	return ret
}

// ActiveStates returns a copy of the currently active states.
func (w *Worker) ActiveStates() am.S {
	return *w.activeState.Load()
}

// Tick return the current tick for a given state.
func (w *Worker) Tick(state string) uint64 {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	return w.tick(state)
}

func (w *Worker) tick(state string) uint64 {
	// TODO validate
	return w.machClock[state]
}

// Clock returns current machine's clock, a state-keyed map of ticks. If states
// are passed, only the ticks of the passed states are returned.
func (w *Worker) Clock(states am.S) am.Clock {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()
	index := w.StateNames()

	if states == nil {
		states = index
	}

	ret := am.Clock{}
	for _, state := range states {
		ret[state] = w.machClock[state]
	}

	return ret
}

// Time returns machine's time, a list of ticks per state. Returned value
// includes the specified states, or all the states if nil.
func (w *Worker) Time(states am.S) am.Time {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	return w.time(states)
}

func (w *Worker) time(states am.S) am.Time {
	index := w.StateNames()
	if states == nil {
		states = index
	}

	ret := am.Time{}
	for _, state := range states {
		idx := slices.Index(index, state)
		ret = append(ret, w.machTime[idx])
	}

	return ret
}

// TimeSum returns the sum of machine's time (ticks per state).
// Returned value includes the specified states, or all the states if nil.
// It's a very inaccurate, yet simple way to measure the machine's
// time.
// TODO handle overflow
func (w *Worker) TimeSum(states am.S) uint64 {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	index := w.StateNames()
	if states == nil {
		states = index
	}

	var sum uint64
	for _, state := range states {
		idx := slices.Index(index, state)
		sum += w.machTime[idx]
	}

	return sum
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
func (w *Worker) NewStateCtx(state string) context.Context {
	// TODO reuse existing ctxs
	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	if _, ok := w.indexStateCtx[state]; ok {
		return w.indexStateCtx[state].Ctx
	}

	v := am.CtxValue{
		Id:    w.id,
		State: state,
		Tick:  w.machClock[state],
	}
	stateCtx, cancel := context.WithCancel(context.WithValue(w.Ctx(),
		am.CtxKey, v))

	// cancel early
	if !w.is(am.S{state}) {
		// TODO decision msg
		cancel()
		return stateCtx
	}

	binding := &am.CtxBinding{
		Ctx:    stateCtx,
		Cancel: cancel,
	}

	// add an index
	w.indexStateCtx[state] = binding
	w.log(am.LogOps, "[ctx:new] %s", state)

	return stateCtx
}

// ///// MISC

// Log logs is a remote logger.
func (w *Worker) Log(msg string, args ...any) {
	if w.c == nil {
		return
	}
	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsLog{Msg: msg, Args: args}
	if !w.c.callFailsafe(w.Ctx(), ServerLog.Value, rpcArgs, resp) {
		return
	}
	// TODO local log?
}

func (w *Worker) SemLogger() am.SemLogger {
	return w.semLogger
}

// StatesVerified returns true if the state names have been ordered
// using VerifyStates.
func (w *Worker) StatesVerified() bool {
	return true
}

// Ctx return worker's root context.
func (w *Worker) Ctx() context.Context {
	return w.ctx
}

// Id returns the machine's id.
func (w *Worker) Id() string {
	return w.id
}

// RemoteId returns the ID of the remote state machine.
func (w *Worker) RemoteId() string {
	return w.remoteId
}

// ParentId returns the id of the parent machine (if any).
func (w *Worker) ParentId() string {
	return w.parentId
}

// Tags returns machine's tags, a list of unstructured strings without spaces.
func (w *Worker) Tags() []string {
	return w.tags
}

// String returns a one line representation of the currently active states,
// with their clock values. Inactive states are omitted.
// Eg: (Foo:1 Bar:3)
func (w *Worker) String() string {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	index := w.StateNames()
	active := w.ActiveStates()
	ret := "("
	for _, state := range index {
		if !slices.Contains(active, state) {
			continue
		}

		if ret != "(" {
			ret += " "
		}
		idx := slices.Index(index, state)
		ret += fmt.Sprintf("%s:%d", state, w.machTime[idx])
	}

	return ret + ")"
}

// StringAll returns a one line representation of all the states, with their
// clock values. Inactive states are in square brackets.
// Eg: (Foo:1 Bar:3)[Baz:2]
func (w *Worker) StringAll() string {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	index := w.StateNames()
	activeStates := w.ActiveStates()
	ret := "("
	ret2 := "["
	for _, state := range index {
		idx := slices.Index(index, state)

		if slices.Contains(activeStates, state) {
			if ret != "(" {
				ret += " "
			}
			ret += fmt.Sprintf("%s:%d", state, w.machTime[idx])
			continue
		}

		if ret2 != "[" {
			ret2 += " "
		}
		ret2 += fmt.Sprintf("%s:%d", state, w.machTime[idx])
	}

	return ret + ") " + ret2 + "]"
}

// Inspect returns a multi-line string representation of the machine (states,
// relations, clock).
// states: param for ordered or partial results.
func (w *Worker) Inspect(states am.S) string {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	index := w.StateNames()
	if states == nil {
		states = index
	}

	activeStates := w.ActiveStates()
	ret := ""
	for _, name := range states {

		state := w.schema[name]
		active := "0"
		if slices.Contains(activeStates, name) {
			active = "1"
		}

		idx := slices.Index(index, name)
		ret += fmt.Sprintf("%s %s\n"+
			"    |Time     %d\n", active, name, w.machTime[idx])
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

func (w *Worker) log(level am.LogLevel, msg string, args ...any) {
	if w.semLogger.Level() < level {
		return
	}

	out := fmt.Sprintf(msg, args...)
	logger := w.semLogger.Logger()
	if logger != nil {
		logger(level, msg, args...)
	} else {
		fmt.Println(out)
	}

	w.logEntriesLock.Lock()
	defer w.logEntriesLock.Unlock()

	w.logEntries = append(w.logEntries, &am.LogEntry{
		Level: level,
		Text:  out,
	})
}

// MustParseStates parses the states and returns them as a list.
// Panics when a state is not defined. It's an usafe equivalent of VerifyStates.
func (w *Worker) MustParseStates(states am.S) am.S {
	w.schemaMx.Lock()
	defer w.schemaMx.Unlock()

	// check if all states are defined in m.Schema
	for _, s := range states {
		if _, ok := w.schema[s]; !ok {
			panic(fmt.Sprintf("state %s is not defined for %s (via %s)", s,
				w.remoteId, w.id))
		}
	}

	return utils.SlicesUniq(states)
}

// Index1 returns the index of a state in the machine's StateNames() list.
func (w *Worker) Index1(state string) int {
	return slices.Index(w.StateNames(), state)
}

func (w *Worker) Index(states am.S) []int {
	ret := make([]int, len(states))
	for i, state := range states {
		ret[i] = w.Index1(state)
	}

	return ret
}

// Dispose disposes the machine and all its emitters. You can wait for the
// completion of the disposal with `<-mach.WhenDisposed`.
func (w *Worker) Dispose() {
	if !w.disposed.CompareAndSwap(false, true) {
		return
	}
	w.tracersMx.Lock()
	defer w.tracersMx.Unlock()

	utils.CloseSafe(w.whenDisposed)
	for _, t := range w.tracers {
		go w.execQueue.Go(func() error {
			t.MachineDispose(w.id)
			return nil
		})
	}

	// TODO push remotely?
}

// IsDisposed returns true if the machine has been disposed.
func (w *Worker) IsDisposed() bool {
	return w.disposed.Load()
}

// WhenDisposed returns a channel that will be closed when the machine is
// disposed. Requires bound handlers. Use Machine.Disposed in case no handlers
// have been bound.
func (w *Worker) WhenDisposed() <-chan struct{} {
	return w.whenDisposed
}

// Export exports the machine state: id, time and state names.
func (w *Worker) Export() *am.Serialized {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	w.log(am.LogChanges, "[import] exported at %d ticks", w.time(nil))

	return &am.Serialized{
		ID:         w.id,
		Time:       w.time(nil),
		StateNames: w.StateNames(),
	}
}

// Schema returns a copy of machine's state structure.
func (w *Worker) Schema() am.Schema {
	return w.schema
}

// BindHandlers binds a struct of handler methods to machine's states, based on
// the naming convention, eg `FooState(e *Event)`. Negotiation handlers can
// optionally return bool.
//
// RPC worker will bind handlers locally, not to the remote machine.
// RPC worker handlers are still TODO.
func (w *Worker) BindHandlers(handlers any) error {
	names, err := am.ListHandlers(handlers, w.StateNames())
	if err != nil {
		return err
	}

	// TODO start a handler loop
	// TODO cal RemotePushAllTicks to tell the remote worker to send all the
	//  clock changes
	//  trigger a similar handler logic as a local machine, besides negotiation
	//  support relation based negotiation, to locally reject mutations
	// call RemotePushAllTicks

	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(handlers).Elem().Name()
	// TODO lock
	w.handlers = append(w.handlers, newRemoteHandler(handlers, names))
	w.log(am.LogOps, "[handlers] bind %s", name)

	return nil
}

// HasHandlers returns true if this machine has bound handlers, and thus an
// allocated goroutine. It also makes it nondeterministic.
func (w *Worker) HasHandlers() bool {
	// TODO lock
	// w.handlersLock.Lock()
	// defer w.handlersLock.Unlock()

	return len(w.handlers) > 0
}

// DetachHandlers detaches previously bound machine handlers.
func (w *Worker) DetachHandlers(handlers any) error {
	old := w.handlers

	for _, h := range old {
		if h.h == handlers {
			w.handlers = utils.SlicesWithout(old, h)
			// TODO
			// h.dispose()

			return nil
		}
	}

	return errors.New("handlers not bound")
}

// BindTracer binds a Tracer to the machine.
func (w *Worker) BindTracer(tracer am.Tracer) error {
	w.tracersMx.Lock()
	defer w.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

	w.tracers = append(w.tracers, tracer)
	w.log(am.LogOps, "[tracers] bind %s", name)

	return nil
}

// DetachTracer tries to remove a tracer from the machine. Returns true if the
// tracer was found and removed.
func (w *Worker) DetachTracer(tracer am.Tracer) error {
	w.tracersMx.Lock()
	defer w.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("DetachTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

	for i, t := range w.tracers {
		if t == tracer {
			// TODO check
			w.tracers = slices.Delete(w.tracers, i, i+1)
			w.log(am.LogOps, "[tracers] detach %s", name)

			return nil
		}
	}

	return errors.New("tracer not bound")
}

// Tracers return a copy of currenty attached tracers.
func (w *Worker) Tracers() []am.Tracer {
	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	return slices.Clone(w.tracers)
}

// InternalUpdateClock is an internal method to update the clock of this Worker.
// It should NOT be called by anything else then a synchronization source (eg
// RPC client, pubsub, etc).
func (w *Worker) InternalUpdateClock(now am.Time, qTick uint64, lock bool) {
	// TODO require mutType and called states for PushAllTicks and handlers
	// TODO pass tx ID
	// clockMx already locked by RPC client (but not pkg/pubsub)
	// w.clockMx.Lock()

	// TODO this is terrible
	if lock {
		w.clockMx.Lock()
	}

	w.tracersMx.Lock()
	defer w.tracersMx.Unlock()

	timeBefore := w.machTime
	clockBefore := maps.Clone(w.machClock)

	activeBefore := w.ActiveStates()
	active := am.S{}
	index := w.StateNames()
	for i, state := range index {
		if am.IsActiveTick(now[i]) {
			active = append(active, state)
		}
	}
	activated := am.DiffStates(active, activeBefore)
	deactivated := am.DiffStates(activeBefore, active)

	tx := &am.Transition{
		Api: w,

		TimeBefore: timeBefore,
		TimeAfter:  now,
		Mutation: &am.Mutation{
			// TODO use add and remove when all ticks passed
			Type:   am.MutationSet,
			Called: w.Index(active),
			Args:   nil,
			Auto:   false,
		},
		LogEntries: w.logEntries,
	}
	// TODO may not be true for qticks only updates
	tx.IsAccepted.Store(true)
	w.logEntries = nil
	// TODO fire handlers if set

	// call tracers
	for _, t := range w.tracers {
		go w.execQueue.Go(func() error {
			t.TransitionInit(tx)
			return nil
		})
	}
	for _, t := range w.tracers {
		go w.execQueue.Go(func() error {
			t.TransitionStart(tx)
			return nil
		})
	}

	// set active states
	w.machTime = now
	for idx, tick := range w.machTime {
		w.machClock[index[idx]] = tick
	}
	w.queueTick = qTick
	w.activeState.Store(&active)

	// clockMx locked by RPC [Client]
	w.clockMx.Unlock()

	for _, t := range w.tracers {
		go w.execQueue.Go(func() error {
			t.TransitionEnd(tx)
			return nil
		})
	}

	// subscriptions
	w.processSubscriptions(activated, deactivated, clockBefore)
}

func (w *Worker) processSubscriptions(
	activated, deactivated am.S, clockBefore am.Clock,
) {
	// lock
	w.clockMx.RLock()

	// collect
	toCancel := w.subs.ProcessStateCtx(deactivated)
	toClose := slices.Concat(
		w.subs.ProcessWhen(activated, deactivated),
		w.subs.ProcessWhenTime(clockBefore),
		w.subs.ProcessWhenQueue(w.queueTick),
	)

	// unlock
	w.clockMx.RUnlock()

	// close outside the critical zone
	for _, cancel := range toCancel {
		cancel()
	}
	for _, ch := range toClose {
		close(ch)
	}
}

func (w *Worker) AddBreakpoint1(added string, removed string, strict bool) {
	// TODO
}

func (w *Worker) AddBreakpoint(added am.S, removed am.S, strict bool) {
	// TODO
}

func (w *Worker) Groups() (map[string][]int, []string) {
	// TODO maybe sync along with schema?
	return nil, nil
}

func (w *Worker) CanAdd(states am.S, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

func (w *Worker) CanAdd1(state string, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

func (w *Worker) CanRemove(states am.S, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

func (w *Worker) CanRemove1(state string, args am.A) am.Result {
	// TODO check relations
	return am.Executed
}

// CountActive returns the number of active states from a passed list. Useful
// for state groups.
func (w *Worker) CountActive(states am.S) int {
	activeStates := w.ActiveStates()
	c := 0
	for _, state := range states {
		if slices.Contains(activeStates, state) {
			c++
		}
	}

	return c
}

func (w *Worker) QueueTick() uint64 {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	return w.queueTick
}

// debug
// func (w *Worker) QueueDump() []string {
// 	return nil
// }
