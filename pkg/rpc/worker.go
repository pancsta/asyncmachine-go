package rpc

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	amh "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/rpcnames"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/types"
)

// Worker is a subset of `pkg/machine#Machine` for RPC. Lacks the queue and
// other local methods. Most methods are clock-based, thus executed locally.
type Worker struct {
	ID  string
	Ctx context.Context
	// TODO remote push
	Disposed atomic.Bool

	c                *Client
	err              error
	states           am.Struct
	clockTime        am.Time
	stateNames       am.S
	activeStatesLock sync.RWMutex
	indexStateCtx    am.IndexStateCtx
	indexWhen        am.IndexWhen
	indexWhenTime    am.IndexWhenTime
	// TODO indexWhenArgs
	// indexWhenArgs am.IndexWhenArgs
	whenDisposed chan struct{}
}

// Worker implements MachineApi
var _ types.MachineApi = &Worker{}

// ///// RPC methods

// Sync requests fresh clock values from the remote machine. Useful to call
// after a batch of no-sync methods, eg AddNS.
func (w *Worker) Sync() am.Time {
	w.c.Mach.Log("Sync")

	// call rpc
	resp := &RespSync{}
	if !w.c.call(w.c.Mach.Ctx, rpcnames.Sync.Encode(), w.TimeSum(nil), resp) {
		return nil
	}

	// validate
	if len(resp.Time) > 0 && len(resp.Time) != len(w.stateNames) {
		errResponseStr(w.c.Mach, "wrong clock len")
		return nil
	}

	// process if time is returned
	if len(resp.Time) > 0 {
		w.c.updateClock(nil, resp.Time)
	}

	return w.clockTime
}

// ///// Mutations (remote)

// Add activates a list of states in the machine, returning the result of the
// transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (w *Worker) Add(states am.S, args am.A) am.Result {
	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{States: amh.StatesToIndexes(w, states), Args: args}
	if !w.c.call(w.c.Mach.Ctx, rpcnames.Add.Encode(), rpcArgs, resp) {
		return am.ResultNoOp
	}

	// validate
	if resp.Result == 0 {
		errResponseStr(w.c.Mach, "no Result")
		return am.ResultNoOp
	}

	// process
	w.c.updateClock(resp.Clock, nil)

	return resp.Result
}

// Add1 is a shorthand method to add a single state with the passed args.
func (w *Worker) Add1(state string, args am.A) am.Result {
	return w.Add(am.S{state}, args)
}

// AddNS is a NoSync method - an efficient way for adding states, as it
// doesn't wait for, nor transfers a response. Because of which it doesn't
// update the clock. Use Sync() to update the clock after a batch of AddNS
// calls.
func (w *Worker) AddNS(states am.S, args am.A) am.Result {
	w.c.log("AddNS")

	// call rpc
	rpcArgs := &ArgsMut{States: amh.StatesToIndexes(w, states), Args: args}
	if !w.c.notify(w.c.Mach.Ctx, rpcnames.AddNS.Encode(), rpcArgs) {
		return am.ResultNoOp
	}

	return am.Executed
}

// Add1NS is a single state version of AddNS.
func (w *Worker) Add1NS(state string, args am.A) am.Result {
	return w.AddNS(am.S{state}, args)
}

// Remove de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (w *Worker) Remove(states am.S, args am.A) am.Result {
	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{States: amh.StatesToIndexes(w, states), Args: args}
	if !w.c.call(w.c.Mach.Ctx, rpcnames.Remove.Encode(), rpcArgs, resp) {
		return am.ResultNoOp
	}

	// validate
	if resp.Result == 0 {
		errResponseStr(w.c.Mach, "no Result")
		return am.ResultNoOp
	}

	// process
	w.c.updateClock(resp.Clock, nil)

	return resp.Result
}

// Remove1 is a shorthand method to remove a single state with the passed args.
// See Remove().
func (w *Worker) Remove1(state string, args am.A) am.Result {
	return w.Remove(am.S{state}, args)
}

// Set de-activates a list of states in the machine, returning the result of
// the transition (Executed, Queued, Canceled).
// Like every mutation method, it will resolve relations and trigger handlers.
func (w *Worker) Set(states am.S, args am.A) am.Result {
	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsMut{States: amh.StatesToIndexes(w, states), Args: args}
	if !w.c.call(w.c.Mach.Ctx, rpcnames.Set.Encode(), rpcArgs, resp) {
		return am.ResultNoOp
	}

	// validate
	if resp.Result == 0 {
		errResponseStr(w.c.Mach, "no Result")
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
	return w.AddErrState(am.Exception, err, args)
}

// AddErrState adds a dedicated error state, along with the build in Exception
// state.
// Like every mutation method, it will resolve relations and trigger handlers.
// AddErrState produces a stack trace of the error, if LogStackTrace is enabled.
func (w *Worker) AddErrState(state string, err error, args am.A) am.Result {
	if w.Disposed.Load() {
		return am.Canceled
	}
	// TODO remove once remote errors are implemented
	w.err = err

	// TODO stack traces
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
	return w.Add(am.S{states.ErrOnClient, state, am.Exception}, args)
}

// ///// Checking (local)

// Is checks if all the passed states are currently active.
//
//	machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
//	machine.Add(S{"Foo"})
//	machine.Is(S{"Foo"}) // true
//	machine.Is(S{"Foo", "Bar"}) // false
func (w *Worker) Is(states am.S) bool {
	w.activeStatesLock.Lock()
	defer w.activeStatesLock.Unlock()

	return w.is(states)
}

// Is1 is a shorthand method to check if a single state is currently active.
// See Is().
func (w *Worker) Is1(state string) bool {
	return w.Is(am.S{state})
}

func (w *Worker) is(states am.S) bool {
	w.c.clockMx.Lock()
	defer w.c.clockMx.Unlock()

	for _, state := range states {
		idx := slices.Index(w.stateNames, state)
		if !am.IsActiveTick(w.clockTime[idx]) {
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
//	machine.Not(S{"A", "C"})
//	// -> false
//
//	// not(C) and not(D)
//	machine.Not(S{"C", "D"})
//	// -> true
func (w *Worker) Not(states am.S) bool {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	return slicesNone(w.MustParseStates(states), w.activeStates())
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
	return slicesEvery(w.StateNames(), states)
}

// Has1 is a shorthand for Has. It returns true if the passed state is
// registered in the machine.
func (w *Worker) Has1(state string) bool {
	return w.Has(am.S{state})
}

// IsClock checks if the machine has changed since the passed
// clock. Returns true if at least one state has changed.
func (w *Worker) IsClock(clock am.Clock) bool {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	for state, tick := range clock {
		if w.clockTime[w.Index(state)] != tick {
			return false
		}
	}

	return true
}

// IsTime checks if the machine has changed since the passed
// time (list of ticks). Returns true if at least one state has changed. The
// states param is optional and can be used to check only a subset of states.
func (w *Worker) IsTime(t am.Time, states am.S) bool {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	if states == nil {
		states = w.stateNames
	}

	for i, tick := range t {
		if w.clockTime[w.Index(states[i])] != tick {
			return false
		}
	}

	return true
}

// Switch returns the first state from the passed list that is currently active,
// making it useful for switch statements.
//
//	switch mach.Switch(ss.GroupPlaying...) {
//	case "Playing":
//	case "Paused":
//	case "Stopped":
//	}
func (w *Worker) Switch(states ...string) string {
	activeStates := w.ActiveStates()

	for _, state := range states {
		if slices.Contains(activeStates, state) {
			return state
		}
	}

	return ""
}

// ///// Waiting (local)

// When returns a channel that will be closed when all the passed states
// become active or the machine gets disposed.
//
// ctx: optional context that will close the channel when done. Useful when
// listening on 2 When() channels within the same `select` to GC the 2nd one.
// TODO re-use channels with the same state set and context
func (w *Worker) When(states am.S, ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	if w.Disposed.Load() {
		close(ch)
		return ch
	}

	w.activeStatesLock.Lock()
	defer w.activeStatesLock.Unlock()

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

	// dispose with context
	disposeWithCtx(w, ctx, ch, states, binding, &w.activeStatesLock, w.indexWhen)

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
// ctx: optional context that will close the channel when done. Useful when
// listening on 2 WhenNot() channels within the same `select` to GC the 2nd one.
func (w *Worker) WhenNot(states am.S, ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	if w.Disposed.Load() {
		close(ch)
		return ch
	}

	w.activeStatesLock.Lock()
	defer w.activeStatesLock.Unlock()

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

	// dispose with context
	disposeWithCtx(w, ctx, ch, states, binding, &w.activeStatesLock, w.indexWhen)

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
// Machine time can be sourced from the Time() method, or Clock() for a specific
// state.
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

	w.activeStatesLock.Lock()
	defer w.activeStatesLock.Unlock()

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

	// dispose with context
	disposeWithCtx(w, ctx, ch, states, binding, &w.activeStatesLock,
		w.indexWhenTime)

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

// WhenTicks waits N ticks of a single state (relative to now). Uses WhenTime
// underneath.
func (w *Worker) WhenTicks(
	state string, ticks int, ctx context.Context,
) <-chan struct{} {
	return w.WhenTime(am.S{state}, am.Time{uint64(ticks) + w.Tick(state)}, ctx)
}

// WhenTicksEq waits till ticks for a single state equal the given absolute
// value (or more). Uses WhenTime underneath.
func (w *Worker) WhenTicksEq(
	state string, ticks uint64, ctx context.Context,
) <-chan struct{} {
	return w.WhenTime(am.S{state}, am.Time{ticks}, ctx)
}

// WhenErr returns a channel that will be closed when the machine is in the
// Exception state.
//
// ctx: optional context defaults to the machine's context.
func (w *Worker) WhenErr(ctx context.Context) <-chan struct{} {
	return w.When([]string{am.Exception}, ctx)
}

// ///// Waiting (remote)

// WhenArgs returns a channel that will be closed when the passed state
// becomes active with all the passed args. Args are compared using the native
// '=='. It's meant to be used with async Multi states, to filter out
// a specific completion.
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
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	return w.activeStates()
}

func (w *Worker) activeStates() am.S {
	ret := am.S{}
	for _, state := range w.stateNames {
		if am.IsActiveTick(w.tick(state)) {
			ret = append(ret, state)
		}
	}

	return ret
}

func (w *Worker) activeStatesUnlocked() am.S {
	ret := am.S{}
	for _, state := range w.stateNames {
		if am.IsActiveTick(w.tickUnlocked(state)) {
			ret = append(ret, state)
		}
	}

	return ret
}

// Tick return the current tick for a given state.
func (w *Worker) Tick(state string) uint64 {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	return w.tick(state)
}

func (w *Worker) tick(state string) uint64 {
	w.c.clockMx.Lock()
	defer w.c.clockMx.Unlock()

	return w.tickUnlocked(state)
}

func (w *Worker) tickUnlocked(state string) uint64 {
	idx := slices.Index(w.stateNames, state)

	return w.clockTime[idx]
}

// Clock returns current machine's clock, a state-keyed map of ticks. If states
// are passed, only the ticks of the passed states are returned.
func (w *Worker) Clock(states am.S) am.Clock {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	return w.clock(states)
}

func (w *Worker) clock(states am.S) am.Clock {
	if states == nil {
		states = w.stateNames
	}

	ret := am.Clock{}
	for _, state := range states {
		idx := slices.Index(w.stateNames, state)
		ret[state] = w.clockTime[idx]
	}

	return ret
}

// Time returns machine's time, a list of ticks per state. Returned value
// includes the specified states, or all the states if nil.
func (w *Worker) Time(states am.S) am.Time {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	return w.time(states)
}

func (w *Worker) time(states am.S) am.Time {
	if states == nil {
		states = w.stateNames
	}

	ret := am.Time{}
	for _, state := range states {
		idx := slices.Index(w.stateNames, state)
		ret = append(ret, w.clockTime[idx])
	}

	return ret
}

// TimeSum returns the sum of machine's time (ticks per state).
// Returned value includes the specified states, or all the states if nil.
// It's a very inaccurate, yet simple way to measure the machine's
// time.
// TODO handle overflow
func (w *Worker) TimeSum(states am.S) uint64 {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	if states == nil {
		states = w.stateNames
	}

	var sum uint64
	for _, state := range states {
		idx := slices.Index(w.stateNames, state)
		sum += w.clockTime[idx]
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
// TODO reuse existing ctxs
func (w *Worker) NewStateCtx(state string) context.Context {
	w.activeStatesLock.Lock()
	defer w.activeStatesLock.Unlock()

	stateCtx, cancel := context.WithCancel(w.c.Mach.Ctx)

	// close early
	if !w.is(am.S{state}) {
		cancel()
		return stateCtx
	}

	// add an index
	if _, ok := w.indexStateCtx[state]; !ok {
		w.indexStateCtx[state] = []context.CancelFunc{cancel}
	} else {
		w.indexStateCtx[state] = append(w.indexStateCtx[state], cancel)
	}

	return stateCtx
}

// ///// MISC

// Log logs an [extern] message unless LogNothing is set (default).
// Optionally redirects to a custom logger from SetLogger.
func (w *Worker) Log(msg string, args ...any) {
	// call rpc
	resp := &RespResult{}
	rpcArgs := &ArgsLog{Msg: msg, Args: args}
	if !w.c.call(w.c.Mach.Ctx, rpcnames.Log.Encode(), rpcArgs, resp) {
		return
	}
	// TODO local log?
}

// String returns a one line representation of the currently active states,
// with their clock values. Inactive states are omitted.
// Eg: (Foo:1 Bar:3)
func (w *Worker) String() string {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	active := w.activeStates()
	ret := "("
	for _, state := range w.stateNames {
		if !slices.Contains(active, state) {
			continue
		}

		if ret != "(" {
			ret += " "
		}
		idx := slices.Index(w.stateNames, state)
		ret += fmt.Sprintf("%s:%d", state, w.clockTime[idx])
	}

	return ret + ")"
}

// StringAll returns a one line representation of all the states, with their
// clock values. Inactive states are in square brackets.
// Eg: (Foo:1 Bar:3)[Baz:2]
func (w *Worker) StringAll() string {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	activeStates := w.activeStates()
	ret := "("
	ret2 := "["
	for _, state := range w.stateNames {
		idx := slices.Index(w.stateNames, state)

		if slices.Contains(activeStates, state) {
			if ret != "(" {
				ret += " "
			}
			ret += fmt.Sprintf("%s:%d", state, w.clockTime[idx])
			continue
		}

		if ret2 != "[" {
			ret2 += " "
		}
		ret2 += fmt.Sprintf("%s:%d", state, w.clockTime[idx])
	}

	return ret + ")" + ret2 + "]"
}

// Inspect returns a multi-line string representation of the machine (states,
// relations, clock).
// states: param for ordered or partial results.
func (w *Worker) Inspect(states am.S) string {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	if states == nil {
		states = w.stateNames
	}

	activeStates := w.activeStates()
	ret := ""
	for _, name := range states {

		state := w.states[name]
		active := "false"
		if slices.Contains(activeStates, name) {
			active = "true"
		}

		idx := slices.Index(w.stateNames, name)
		ret += name + ":\n"
		ret += fmt.Sprintf("  State:   %s %d\n", active, w.clockTime[idx])
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

// log forwards a log msg to the Clients machine, respecting its log level.
func (w *Worker) log(level am.LogLevel, msg string, args ...any) {
	if w.c.Mach.GetLogLevel() < level {
		return
	}

	// TODO get log level from the remote worker
	msg = "[worker] " + msg
	msg = strings.ReplaceAll(msg, "] [", ":")
	// TODO replace {} with [] once #101 is fixed
	msg = strings.ReplaceAll(strings.ReplaceAll(msg, "[", "{"), "]", "}")
	w.c.Mach.Log(msg, args...)
}

// MustParseStates parses the states and returns them as a list.
// Panics when a state is not defined. It's an usafe equivalent of VerifyStates.
func (w *Worker) MustParseStates(states am.S) am.S {
	// check if all states are defined in m.Struct
	for _, s := range states {
		if _, ok := w.states[s]; !ok {
			panic(fmt.Sprintf("state %s is not defined", s))
		}
	}

	return slicesUniq(states)
}

func (w *Worker) processStateCtxBindings(statesBefore am.S) {
	active := w.ActiveStates()

	w.activeStatesLock.RLock()
	deactivated := am.DiffStates(statesBefore, active)

	var toCancel []context.CancelFunc
	for _, s := range deactivated {

		toCancel = append(toCancel, w.indexStateCtx[s]...)
		delete(w.indexStateCtx, s)
	}

	w.activeStatesLock.RUnlock()

	// cancel all the state contexts outside the critical zone
	for _, cancel := range toCancel {
		cancel()
	}
}

func (w *Worker) processWhenBindings(statesBefore am.S) {
	active := w.ActiveStates()

	w.activeStatesLock.Lock()

	// calculate activated and deactivated states
	activated := am.DiffStates(active, statesBefore)
	deactivated := am.DiffStates(statesBefore, active)

	// merge all states
	all := am.S{}
	all = append(all, activated...)
	all = append(all, deactivated...)

	var toClose []chan struct{}
	for _, s := range all {
		for k, binding := range w.indexWhen[s] {

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

				if state == s {
					w.indexWhen[s] = append(w.indexWhen[s][:k], w.indexWhen[s][k+1:]...)
					continue
				}

				w.indexWhen[state] = slices.Delete(w.indexWhen[state], k, k+1)
			}

			w.log(am.LogDecisions, "[when] match for (%s)", j(names))
			// close outside the critical zone
			toClose = append(toClose, binding.Ch)
		}
	}
	w.activeStatesLock.Unlock()

	// notify outside the critical zone
	for ch := range toClose {
		closeSafe(toClose[ch])
	}
}

func (w *Worker) processWhenTimeBindings(timeBefore am.Time) {
	w.activeStatesLock.Lock()
	indexWhenTime := w.indexWhenTime
	var toClose []chan struct{}

	// collect all the ticked states
	all := am.S{}
	for idx, t := range timeBefore {
		// if changed, collect to check
		if w.clockTime[idx] != t {
			all = append(all, w.stateNames[idx])
		}
	}

	// check all the bindings for all the ticked states
	for _, s := range all {
		for k, binding := range indexWhenTime[s] {

			// check if the requested time has passed
			if !binding.Completed[s] &&
				w.clockTime[w.Index(s)] >= binding.Times[binding.Index[s]] {
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
				if state == s {
					indexWhenTime[s] = append(indexWhenTime[s][:k],
						indexWhenTime[s][k+1:]...)
					continue
				}

				indexWhenTime[state] = slices.Delete(indexWhenTime[state], k, k+1)
			}

			w.log(am.LogDecisions, "[when:time] match for (%s)", j(names))
			// close outside the critical zone
			toClose = append(toClose, binding.Ch)
		}
	}
	w.activeStatesLock.Unlock()

	// notify outside the critical zone
	for ch := range toClose {
		closeSafe(toClose[ch])
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
	closeSafe(w.whenDisposed)
}

// WhenDisposed returns a channel that will be closed when the machine is
// disposed. Requires bound handlers. Use Machine.Disposed in case no handlers
// have been bound.
func (w *Worker) WhenDisposed() <-chan struct{} {
	return w.whenDisposed
}

// Export exports the machine state: ID, time and state names.
func (w *Worker) Export() *am.Serialized {
	w.activeStatesLock.RLock()
	defer w.activeStatesLock.RUnlock()

	w.log(am.LogChanges, "[import] exported at %d ticks", w.time(nil))

	return &am.Serialized{
		ID:         w.ID,
		Time:       w.time(nil),
		StateNames: w.stateNames,
	}
}

func (w *Worker) GetStruct() am.Struct {
	return w.states
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// j joins state names into a single string
func j(states []string) string {
	return strings.Join(states, " ")
}

// disposeWithCtx handles early binding disposal caused by a canceled context.
// It's used by most of "when" methods.
func disposeWithCtx[T comparable](
	mach *Worker, ctx context.Context, ch chan struct{}, states am.S, binding T,
	lock *sync.RWMutex, index map[string][]T,
) {
	if ctx == nil {
		return
	}
	go func() {
		select {
		case <-ch:
			return
		case <-mach.Ctx.Done():
			return
		case <-ctx.Done():
		}
		// GC only if needed
		if mach.Disposed.Load() {
			return
		}

		// TODO track
		closeSafe(ch)

		lock.Lock()
		defer lock.Unlock()

		for _, s := range states {
			if _, ok := index[s]; ok {
				if len(index[s]) == 1 {
					delete(index, s)
				} else {
					index[s] = slicesWithout(index[s], binding)
				}
			}
		}
	}()
}

func closeSafe[T any](ch chan T) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func slicesWithout[S ~[]E, E comparable](coll S, el E) S {
	idx := slices.Index(coll, el)
	ret := slices.Clone(coll)
	if idx == -1 {
		return ret
	}
	return slices.Delete(ret, idx, idx+1)
}

// slicesNone returns true if none of the elements of coll2 are in coll1.
func slicesNone[S1 ~[]E, S2 ~[]E, E comparable](col1 S1, col2 S2) bool {
	for _, el := range col2 {
		if slices.Contains(col1, el) {
			return false
		}
	}
	return true
}

// slicesEvery returns true if all elements of coll2 are in coll1.
func slicesEvery[S1 ~[]E, S2 ~[]E, E comparable](col1 S1, col2 S2) bool {
	for _, el := range col2 {
		if !slices.Contains(col1, el) {
			return false
		}
	}
	return true
}

func slicesUniq[S ~[]E, E comparable](coll S) S {
	var ret S
	for _, el := range coll {
		if !slices.Contains(ret, el) {
			ret = append(ret, el)
		}
	}
	return ret
}
