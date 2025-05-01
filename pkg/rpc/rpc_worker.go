package rpc

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/rpcnames"
)

// Worker is a subset of `pkg/machine#Machine` for RPC. Lacks the queue and
// other local methods. Most methods are clock-based, thus executed locally.
type Worker struct {
	Disposed atomic.Bool

	// remoteId is the ID of the remote state machine.
	remoteId string
	id       string
	ctx      context.Context
	// RPC client parenting this Worker. If nil, worker is read-only and won't
	// allow for mutations / network calls.
	c             *Client
	err           error
	schema        am.Schema
	clockMx       sync.RWMutex
	machTime      am.Time
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
	logger         atomic.Pointer[am.Logger]
	logEntriesLock sync.Mutex
	logEntries     []*am.LogEntry
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
		parentId:      parent.Id(),
		tags:          tags,
	}
	lvl := am.LogNothing
	w.logLevel.Store(&lvl)
	w.activeState.Store(&am.S{})
	parent.HandleDispose(func(id string, ctx context.Context) {
		w.Dispose()
	})

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
	ok := w.c.callFailsafe(w.c.Mach.Ctx(), rpcnames.Sync.Encode(),
		w.TimeSum(nil), resp)
	if !ok {
		return nil
	}

	// validate
	if len(resp.Time) > 0 && len(resp.Time) != len(w.stateNames) {
		AddErrRpcStr(nil, w.c.Mach, "wrong clock len")
		return nil
	}

	// process if time is returned
	if len(resp.Time) > 0 {
		w.c.updateClock(nil, resp.Time)
	}

	return w.machTime
}

// ///// Mutations (remote)

// Add activates a list of states in the machine, returning the result of the
// transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (w *Worker) Add(states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}

	w.MustParseStates(states)

	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
	}
	if !w.c.callFailsafe(w.Ctx(), rpcnames.Add.Encode(), rpcArgs, resp) {
		return am.ResultNoOp
	}

	// validate
	if resp.Result == 0 {
		AddErrRpcStr(nil, w.c.Mach, "no Result")
		return am.ResultNoOp
	}

	// process
	w.c.updateClock(resp.Clock, nil)

	return resp.Result
}

// Add1 is a shorthand method to add a single state with the passed args.
func (w *Worker) Add1(state string, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}
	return w.Add(am.S{state}, args)
}

// AddNS is a NoSync method - an efficient way for adding states, as it
// doesn't wait for, nor transfers a response. Because of which it doesn't
// update the clock. Use Sync() to update the clock after a batch of AddNS
// calls.
func (w *Worker) AddNS(states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}

	w.c.log("AddNS")
	w.MustParseStates(states)

	// call rpc
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
	}
	if !w.c.notifyFailsafe(w.Ctx(), rpcnames.AddNS.Encode(), rpcArgs) {
		return am.ResultNoOp
	}

	return am.Executed
}

// Add1NS is a single state version of AddNS.
func (w *Worker) Add1NS(state string, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}
	return w.AddNS(am.S{state}, args)
}

// Remove de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (w *Worker) Remove(states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}

	w.MustParseStates(states)

	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
	}
	if !w.c.callFailsafe(w.Ctx(), rpcnames.Remove.Encode(), rpcArgs, resp) {
		return am.ResultNoOp
	}

	// validate
	if resp.Result == 0 {
		AddErrRpcStr(nil, w.c.Mach, "no Result")
		return am.ResultNoOp
	}

	// process
	w.c.updateClock(resp.Clock, nil)

	return resp.Result
}

// Remove1 is a shorthand method to remove a single state with the passed args.
// See Remove().
func (w *Worker) Remove1(state string, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}
	return w.Remove(am.S{state}, args)
}

// Set de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (w *Worker) Set(states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}

	w.MustParseStates(states)

	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
	}
	if !w.c.callFailsafe(w.Ctx(), rpcnames.Set.Encode(), rpcArgs, resp) {
		return am.ResultNoOp
	}

	// validate
	if resp.Result == 0 {
		AddErrRpcStr(nil, w.c.Mach, "no Result")
		return am.ResultNoOp
	}

	// process
	w.c.updateClock(resp.Clock, nil)

	return resp.Result
}

// AddErr is a dedicated method to add the Exception state with the passed
// error and optional arguments.
// Like every mutation method, it will resolve relations and trigger handlers.
// AddErr produces a stack trace of the error, if LogStackTrace is enabled.
func (w *Worker) AddErr(err error, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}
	return w.AddErrState(am.Exception, err, args)
}

// AddErrState adds a dedicated error state, along with the build in Exception
// state.
// Like every mutation method, it will resolve relations and trigger handlers.
// AddErrState produces a stack trace of the error, if LogStackTrace is enabled.
func (w *Worker) AddErrState(state string, err error, args am.A) am.Result {
	if w.c == nil || w.Disposed.Load() {
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
	return w.Add(am.S{ssS.ErrOnClient, state, am.Exception}, args)
}

func (w *Worker) EvAdd(event *am.Event, states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}

	w.MustParseStates(states)

	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{
		States: amhelp.StatesToIndexes(w.StateNames(), states),
		Args:   args,
		Event:  event.Clone(),
	}
	if !w.c.callFailsafe(w.Ctx(), rpcnames.Add.Encode(), rpcArgs, resp) {
		return am.ResultNoOp
	}

	// validate
	if resp.Result == 0 {
		AddErrRpcStr(nil, w.c.Mach, "no Result")
		return am.ResultNoOp
	}

	// process
	w.c.updateClock(resp.Clock, nil)

	return resp.Result
}

// Add1 is a shorthand method to add a single state with the passed args.
func (w *Worker) EvAdd1(event *am.Event, state string, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}
	return w.EvAdd(event, am.S{state}, args)
}

func (w *Worker) EvRemove1(event *am.Event, state string, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}
	return w.EvRemove(event, am.S{state}, args)
}

func (w *Worker) EvRemove(event *am.Event, states am.S, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}
	return am.Canceled // TODO
}

func (w *Worker) EvAddErr(event *am.Event, err error, args am.A) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}
	return am.Canceled // TODO
}

func (w *Worker) EvAddErrState(
	event *am.Event, state string, err error, args am.A,
) am.Result {
	if w.c == nil {
		return am.ResultNoOp
	}
	return am.Canceled // TODO
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
	return w.Is1(am.Exception)
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
		if w.machTime[w.Index(state)] != tick {
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

	if states == nil {
		states = w.stateNames
	}

	for i, tick := range t {
		if w.machTime[w.Index(states[i])] != tick {
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

// When returns a channel that will be closed when all the passed states
// become active or the machine gets disposed.
//
// ctx: optional context that will close the channel when done.
func (w *Worker) When(states am.S, ctx context.Context) <-chan struct{} {
	// TODO mixin from am.Subscription
	// TODO re-use channels with the same state set and context
	ch := make(chan struct{})
	if w.Disposed.Load() {
		close(ch)
		return ch
	}

	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	// if all active, close early
	if w.is(states) {
		close(ch)
		return ch
	}

	setMap := am.StateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = w.is(am.S{s})
		if setMap[s] {
			matched++
		}
	}

	// add the binding to an index of each state
	binding := &am.WhenBinding{
		Ch:       ch,
		Negation: false,
		States:   setMap,
		Total:    len(states),
		Matched:  matched,
	}
	w.log(am.LogOps, "[when:new] %s", utils.J(states))

	// dispose with context
	DisposeWithCtx(w, ctx, ch, states, binding, &w.clockMx, w.indexWhen,
		fmt.Sprintf("[when:match] %s", utils.J(states)))

	// insert the binding
	for _, s := range states {
		if _, ok := w.indexWhen[s]; !ok {
			w.indexWhen[s] = []*am.WhenBinding{binding}
		} else {
			w.indexWhen[s] = append(w.indexWhen[s], binding)
		}
	}

	return ch
}

// When1 is an alias to When() for a single state.
// See When.
func (w *Worker) When1(state string, ctx context.Context) <-chan struct{} {
	return w.When(am.S{state}, ctx)
}

// WhenNot returns a channel that will be closed when all the passed states
// become inactive or the machine gets disposed.
//
// ctx: optional context that will close the channel when done.
func (w *Worker) WhenNot(states am.S, ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	if w.Disposed.Load() {
		close(ch)
		return ch
	}

	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	// if all inactive, close early
	if !w.is(states) {
		close(ch)
		return ch
	}

	setMap := am.StateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = w.is(am.S{s})
		if !setMap[s] {
			matched++
		}
	}

	// add the binding to an index of each state
	binding := &am.WhenBinding{
		Ch:       ch,
		Negation: true,
		States:   setMap,
		Total:    len(states),
		Matched:  matched,
	}
	w.log(am.LogOps, "[whenNot:new] %s", utils.J(states))

	// dispose with context
	DisposeWithCtx(w, ctx, ch, states, binding, &w.clockMx, w.indexWhen,
		fmt.Sprintf("[whenNot:match] %s", utils.J(states)))

	// insert the binding
	for _, s := range states {
		if _, ok := w.indexWhen[s]; !ok {
			w.indexWhen[s] = []*am.WhenBinding{binding}
		} else {
			w.indexWhen[s] = append(w.indexWhen[s], binding)
		}
	}

	return ch
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
// ctx: optional context that will close the channel when done.
func (w *Worker) WhenTime(
	states am.S, times am.Time, ctx context.Context,
) <-chan struct{} {
	ch := make(chan struct{})
	valid := len(states) == len(times)
	if w.Disposed.Load() || !valid {
		if !valid {
			// TODO local log
			w.log(am.LogDecisions, "[when:time] times for all passed states required")
		}
		close(ch)
		return ch
	}

	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	// if all times passed, close early
	passed := true
	for i, s := range states {
		if w.tick(s) < times[i] {
			passed = false
			break
		}
	}
	if passed {
		close(ch)
		return ch
	}

	completed := am.StateIsActive{}
	matched := 0
	index := map[string]int{}
	for i, s := range states {
		completed[s] = w.tick(s) >= times[i]
		if completed[s] {
			matched++
		}
		index[s] = i
	}

	// add the binding to an index of each state
	binding := &am.WhenTimeBinding{
		Ch:        ch,
		Index:     index,
		Completed: completed,
		Total:     len(states),
		Matched:   matched,
		Times:     times,
	}
	w.log(am.LogOps, "[whenTime:new] %s %s", utils.Jw(states, ","), times)

	// dispose with context
	DisposeWithCtx(w, ctx, ch, states, binding, &w.clockMx,
		w.indexWhenTime, fmt.Sprintf("[whenTime:match] %s %s",
			utils.Jw(states, ","), times))

	// insert the binding
	for _, s := range states {
		if _, ok := w.indexWhenTime[s]; !ok {
			w.indexWhenTime[s] = []*am.WhenTimeBinding{binding}
		} else {
			w.indexWhenTime[s] = append(w.indexWhenTime[s], binding)
		}
	}

	return ch
}

// WhenTime1 waits till ticks for a single state equal the given absolute
// value (or more). Uses WhenTime underneath.
//
// ctx: optional context that will close the channel when done.
func (w *Worker) WhenTime1(
	state string, ticks uint64, ctx context.Context,
) <-chan struct{} {
	return w.WhenTime(am.S{state}, am.Time{ticks}, ctx)
}

// WhenTicks waits N ticks of a single state (relative to now). Uses WhenTime
// underneath.
//
// ctx: optional context that will close the channel when done.
func (w *Worker) WhenTicks(
	state string, ticks int, ctx context.Context,
) <-chan struct{} {
	return w.WhenTime(am.S{state}, am.Time{uint64(ticks) + w.Tick(state)}, ctx)
}

// WhenErr returns a channel that will be closed when the machine is in the
// Exception state.
//
// ctx: optional context that will close the channel when done.
func (w *Worker) WhenErr(ctx context.Context) <-chan struct{} {
	return w.When([]string{am.Exception}, ctx)
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
	// TODO implement me
	panic("implement me")
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
	return w.stateNames
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
	idx := slices.Index(w.stateNames, state)

	return w.machTime[idx]
}

// Clock returns current machine's clock, a state-keyed map of ticks. If states
// are passed, only the ticks of the passed states are returned.
func (w *Worker) Clock(states am.S) am.Clock {
	w.clockMx.RLock()
	defer w.clockMx.RUnlock()

	return w.clock(states)
}

func (w *Worker) clock(states am.S) am.Clock {
	if states == nil {
		states = w.stateNames
	}

	ret := am.Clock{}
	for _, state := range states {
		idx := slices.Index(w.stateNames, state)
		ret[state] = w.machTime[idx]
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
	if states == nil {
		states = w.stateNames
	}

	ret := am.Time{}
	for _, state := range states {
		idx := slices.Index(w.stateNames, state)
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

	if states == nil {
		states = w.stateNames
	}

	var sum uint64
	for _, state := range states {
		idx := slices.Index(w.stateNames, state)
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
		Tick:  w.clock(am.S{state})[state],
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
	if !w.c.callFailsafe(w.Ctx(), rpcnames.Log.Encode(), rpcArgs, resp) {
		return
	}
	// TODO local log?
}

// SetLogId enables or disables the logging of the machine's id in log messages.
func (w *Worker) SetLogId(val bool) {}

// GetLogId returns the current state of the log id setting.
func (w *Worker) GetLogId() bool {
	// TODO
	return false
}

// SetLoggerSimple takes log.Printf and sets the log level in one
// call. Useful for testing. Requires LogChanges log level to produce any
// output.
func (w *Worker) SetLoggerSimple(
	logf func(format string, args ...any), level am.LogLevel,
) {
	if logf == nil {
		panic("logf cannot be nil")
	}

	var logger am.Logger = func(_ am.LogLevel, msg string, args ...any) {
		logf(msg, args...)
	}
	w.logger.Store(&logger)
	w.logLevel.Store(&level)
}

// SetLoggerEmpty creates an empty logger that does nothing and sets the log
// level in one call. Useful when combined with am-dbg. Requires LogChanges log
// level to produce any output.
func (w *Worker) SetLoggerEmpty(level am.LogLevel) {
	var logger am.Logger = func(_ am.LogLevel, msg string, args ...any) {
		// no-op
	}
	w.logger.Store(&logger)
	w.logLevel.Store(&level)
}

// SetLogger sets a custom logger function.
func (w *Worker) SetLogger(fn am.Logger) {
	if fn == nil {
		w.logger.Store(nil)

		return
	}
	w.logger.Store(&fn)
}

// GetLogger returns the current custom logger function, or nil.
func (w *Worker) GetLogger() *am.Logger {
	// TODO should return `Logger` not `*Logger`?
	return w.logger.Load()
}

// SetLogLevel sets the log level of the machine.
func (w *Worker) SetLogLevel(level am.LogLevel) {
	w.logLevel.Store(&level)
}

// GetLogLevel returns the log level of the machine.
func (w *Worker) GetLogLevel() am.LogLevel {
	return *w.logLevel.Load()
}

// LogLvl adds an internal log entry from the outside. It should be used only
// by packages extending pkg/machine. Use Log instead.
func (w *Worker) LogLvl(lvl am.LogLevel, msg string, args ...any) {
	if w.Disposed.Load() {
		return
	}

	// single lines only
	msg = strings.ReplaceAll(msg, "\n", " ")

	w.log(lvl, msg, args...)
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

	active := w.ActiveStates()
	ret := "("
	for _, state := range w.stateNames {
		if !slices.Contains(active, state) {
			continue
		}

		if ret != "(" {
			ret += " "
		}
		idx := slices.Index(w.stateNames, state)
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

	activeStates := w.ActiveStates()
	ret := "("
	ret2 := "["
	for _, state := range w.stateNames {
		idx := slices.Index(w.stateNames, state)

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

	if states == nil {
		states = w.stateNames
	}

	activeStates := w.ActiveStates()
	ret := ""
	for _, name := range states {

		state := w.schema[name]
		active := "0"
		if slices.Contains(activeStates, name) {
			active = "1"
		}

		idx := slices.Index(w.stateNames, name)
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
	if w.GetLogLevel() < level {
		return
	}

	out := fmt.Sprintf(msg, args...)
	logger := w.GetLogger()
	if logger != nil {
		(*logger)(level, msg, args...)
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
	// check if all states are defined in m.Struct
	for _, s := range states {
		// TODO lock
		if _, ok := w.schema[s]; !ok {
			panic(fmt.Sprintf("state %s is not defined for %s", s, w.id))
		}
	}

	return utils.SlicesUniq(states)
}

func (w *Worker) processStateCtxBindings(statesBefore am.S) {
	active := w.ActiveStates()

	w.clockMx.RLock()
	deactivated := am.DiffStates(statesBefore, active)

	var toCancel []context.CancelFunc
	for _, s := range deactivated {
		if _, ok := w.indexStateCtx[s]; !ok {
			continue
		}

		toCancel = append(toCancel, w.indexStateCtx[s].Cancel)
		w.log(am.LogOps, "[ctx:match] %s", s)
		delete(w.indexStateCtx, s)
	}

	w.clockMx.RUnlock()

	// cancel all the state contexts outside the critical section
	for _, cancel := range toCancel {
		cancel()
	}
}

func (w *Worker) processWhenBindings(statesBefore am.S) {
	active := w.ActiveStates()

	w.clockMx.Lock()

	// calculate activated and deactivated states
	activated := am.DiffStates(active, statesBefore)
	deactivated := am.DiffStates(statesBefore, active)

	// merge all states
	all := am.S{}
	all = append(all, activated...)
	all = append(all, deactivated...)

	var toClose []chan struct{}
	for _, s := range all {
		for _, binding := range w.indexWhen[s] {

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

				if len(w.indexWhen[state]) == 1 {
					delete(w.indexWhen, state)
					continue
				}

				// delete with a lookup TODO optimize
				w.indexWhen[state] = utils.SlicesWithout(w.indexWhen[state], binding)
			}

			if binding.Negation {
				w.log(am.LogOps, "[whenNot:match] %s", utils.J(names))
			} else {
				w.log(am.LogOps, "[when:match] %s", utils.J(names))
			}
			// close outside the critical section
			toClose = append(toClose, binding.Ch)
		}
	}
	w.clockMx.Unlock()

	// notifyFailsafe outside the critical section
	for ch := range toClose {
		utils.CloseSafe(toClose[ch])
	}
}

func (w *Worker) processWhenTimeBindings(timeBefore am.Time) {
	w.clockMx.Lock()
	indexWhenTime := w.indexWhenTime
	var toClose []chan struct{}

	// collect all the ticked states
	all := am.S{}
	for idx, t := range timeBefore {
		// if changed, collect to check
		if w.machTime[idx] != t {
			all = append(all, w.stateNames[idx])
		}
	}

	// check all the bindings for all the ticked states
	for _, s := range all {
		for _, binding := range indexWhenTime[s] {

			// check if the requested time has passed
			if !binding.Completed[s] &&
				w.machTime[w.Index(s)] >= binding.Times[binding.Index[s]] {
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

				// delete with a lookup TODO optimize
				w.indexWhenTime[state] = utils.SlicesWithout(w.indexWhenTime[state],
					binding)
			}

			w.log(am.LogOps, "[whenTime:match] %s %d", utils.J(names), binding.Times)
			// close outside the critical section
			toClose = append(toClose, binding.Ch)
		}
	}
	w.clockMx.Unlock()

	// notifyFailsafe outside the critical section
	for ch := range toClose {
		utils.CloseSafe(toClose[ch])
	}
}

// Index returns the index of a state in the machine's StateNames() list.
func (w *Worker) Index(state string) int {
	return slices.Index(w.stateNames, state)
}

// Dispose disposes the machine and all its emitters. You can wait for the
// completion of the disposal with `<-mach.WhenDisposed`.
func (w *Worker) Dispose() {
	if !w.Disposed.CompareAndSwap(false, true) {
		return
	}
	w.tracersMx.Lock()
	defer w.tracersMx.Unlock()

	utils.CloseSafe(w.whenDisposed)
	for _, t := range w.tracers {
		t.MachineDispose(w.id)
	}

	// TODO push remotely?
}

// IsDisposed returns true if the machine has been disposed.
func (w *Worker) IsDisposed() bool {
	return w.Disposed.Load()
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
		StateNames: w.stateNames,
	}
}

// Schema returns a copy of machine's state structure.
func (w *Worker) Schema() am.Schema {
	return w.schema
}

// BindHandlers binds a struct of handler methods to machine's states, based on
// the naming convention, eg `FooState(e *Event)`. Negotiation handlers can
// optionall return bool.
//
// RPC worker will bind handelrs locally, not to the remote machine.
func (w *Worker) BindHandlers(handlers any) error {
	names, err := am.ListHandlers(handlers, w.stateNames)
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
func (w *Worker) DetachTracer(tracer am.Tracer) bool {
	w.tracersMx.Lock()
	defer w.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return false
	}
	name := reflect.TypeOf(tracer).Elem().Name()

	for i, t := range w.tracers {
		if t == tracer {
			w.tracers = append(w.tracers[:i], w.tracers[i+1:]...)
			w.log(am.LogOps, "[tracers] detach %s", name)

			return true
		}
	}

	return false
}

// Tracers return a copy of currenty attached tracers.
func (w *Worker) Tracers() []am.Tracer {
	w.clockMx.Lock()
	defer w.clockMx.Unlock()

	return slices.Clone(w.tracers)
}

// UpdateClock is an internal method to update the clock of this Worker. It
// should NOT be called by anything else then a synchronization source (eg RPC
// client, pubsub, etc).
func (w *Worker) UpdateClock(now am.Time, lock bool) {
	// TODO require mutType and called states for PushAllTicks and handlers
	// clockMx already locked by RPC client (but not pkg/pubsub)
	// w.clockMx.Lock()

	// TODO this is terrible
	if lock {
		w.clockMx.Lock()
	}

	before := w.machTime
	// check if changed
	if now.Sum() == before.Sum() {
		w.clockMx.Unlock()
		return
	}
	activeBefore := w.ActiveStates()
	active := am.S{}
	for i, state := range w.stateNames {
		if am.IsActiveTick(now[i]) {
			active = append(active, state)
		}
	}

	tx := &am.Transition{
		Api:      w,
		Accepted: true,

		TimeBefore: before,
		TimeAfter:  now,
		Mutation: &am.Mutation{
			Type:         am.MutationSet,
			CalledStates: active,
			Args:         nil,
			Auto:         false,
		},
		LogEntries: w.logEntries,
	}
	w.logEntries = nil
	// TODO fire handlers if set

	// call tracers
	for _, t := range w.tracers {
		// TODO deadlock on method using w.clockMx
		t.TransitionInit(tx)
	}
	for _, t := range w.tracers {
		// TODO deadlock on method using w.clockMx
		t.TransitionStart(tx)
	}

	// set active states
	w.machTime = now
	w.activeState.Store(&active)

	// clockMx locked by RPC client
	w.clockMx.Unlock()

	for _, t := range w.tracers {
		t.TransitionEnd(tx)
	}

	// process clock-based indexes
	w.processWhenBindings(activeBefore)
	w.processWhenTimeBindings(before)
	w.processStateCtxBindings(activeBefore)
}
