// Package pipes provide helpers to pipe states from one machine to another.
package pipes

// TODO register disposal handlers, detach from source machines
// TODO implement removal of pipes via:
//  - binding-struct
//  - tagging of handler structs

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

// Add adds a pipe for an Add mutation between [source] and [target] machines,
// for a single target state.
//
// targetState: defaults to sourceState
func Add(
	source, target am.Api, sourceState string, targetState string,
) am.HandlerFinal {

	return add(false, source, target, sourceState, targetState)
}

// AddFlat is like [Add], but skips unnecessary mutations.
func AddFlat(
	source, target am.Api, sourceState string, targetState string,
) am.HandlerFinal {

	return add(true, source, target, sourceState, targetState)
}

func add(
	flat bool, source, target am.Api, sourceState string, targetState string,
) am.HandlerFinal {

	if sourceState == "" {
		// TODO log, dont panic?
		panic(am.ErrStateMissing)
	}
	if targetState == "" {
		targetState = sourceState
	}

	// graph info
	semLog := source.SemLogger()
	semLog.AddPipeOut(true, sourceState, target.Id())
	semLog.AddPipeIn(true, targetState, source.Id())

	// TODO optimize
	source.OnDispose(gcHandler(target))
	target.OnDispose(gcHandler(source))
	names := am.S{targetState}
	// include Exception when adding errors
	if strings.HasPrefix(targetState, am.PrefixErr) {
		names = am.S{am.StateException, targetState}
	}

	return func(e *am.Event) {
		// flat skips unnecessary mutations
		if flat && target.Is(names) {

			return
		} else if flat {
			target.Add(names, nil)
		} else {
			target.EvAdd(e, names, e.Args)
		}
	}
}

// Remove adds a pipe for a Remove mutation between source and target
// machines, for a single target state.
//
// targetState: defaults to sourceState
func Remove(
	source, target am.Api, sourceState string, targetState string,
) am.HandlerFinal {

	return remove(false, source, target, sourceState, targetState)
}

func RemoveFlat(
	source, target am.Api, sourceState string, targetState string,
) am.HandlerFinal {

	return remove(true, source, target, sourceState, targetState)
}

func remove(
	flat bool, source, target am.Api, sourceState string, targetState string,
) am.HandlerFinal {

	if sourceState == "" {
		panic(am.ErrStateMissing)
	}
	if targetState == "" {
		targetState = sourceState
	}

	// graph info
	semLog := source.SemLogger()
	semLog.AddPipeOut(false, sourceState, target.Id())
	semLog.AddPipeIn(false, targetState, source.Id())

	// TODO optimize
	source.OnDispose(gcHandler(target))
	target.OnDispose(gcHandler(source))

	return func(e *am.Event) {
		// flat skips unnecessary mutations
		if flat && target.Not1(targetState) {
			return
		} else if flat {
			target.Remove1(targetState, nil)
		} else {
			target.EvRemove1(e, targetState, e.Args)
		}
	}
}

// ///// ///// /////

// ///// BINDS

// ///// ///// /////

// BindAny binds a whole machine via the global AnyState handler and Set
// mutation. The target mutates the same number of times as the source.
func BindAny(source, target am.Api) error {

	// TODO assert target has all the source states

	fn := func(e *am.Event) {
		tx := e.Transition()

		// set if not set
		states := tx.TargetStates()
		if target.Is(states) {

			return
		}
		target.Set(states, e.Args)
	}
	h := &struct {
		AnyState am.HandlerFinal
	}{
		AnyState: fn,
	}

	// graph info TODO? optimize in bulk
	semLog := source.SemLogger()
	for _, sourceState := range source.StateNames() {
		semLog.AddPipeOut(true, sourceState, target.Id())
		semLog.AddPipeIn(true, sourceState, source.Id())
	}

	// TODO optimize
	source.OnDispose(gcHandler(target))
	target.OnDispose(gcHandler(source))

	return source.BindHandlers(h)
}

// BindConnected binds a [ss.ConnectedSchema] machine to 4 custom states. Each
// one is optional and bound with Add/Remove.
func BindConnected(
	source, target am.Api, disconnected, connecting, connected,
	disconnecting string,
) error {

	h := &struct {
		DisconnectedState am.HandlerFinal
		DisconnectedEnd   am.HandlerFinal

		ConnectingState am.HandlerFinal
		ConnectingEnd   am.HandlerFinal

		ConnectedState am.HandlerFinal
		ConnectedEnd   am.HandlerFinal

		DisconnectingState am.HandlerFinal
		DisconnectingEnd   am.HandlerFinal
	}{}

	s := ss.ConnectedStates
	if disconnected != "" {
		h.DisconnectedState = Add(source, target, s.Disconnected, disconnected)
		h.DisconnectedEnd = Remove(source, target, s.Disconnected, disconnected)
	}
	if connecting != "" {
		h.ConnectingState = Add(source, target, s.Connecting, connecting)
		h.ConnectingEnd = Remove(source, target, s.Connecting, connecting)
	}
	if connected != "" {
		h.ConnectedState = Add(source, target, s.Connected, connected)
		h.ConnectedEnd = Remove(source, target, s.Connected, connected)
	}
	if disconnecting != "" {
		h.DisconnectingState = Add(source, target, s.Disconnecting, disconnecting)
		h.DisconnectingEnd = Remove(source, target, s.Disconnecting, disconnecting)
	}

	return source.BindHandlers(h)
}

// BindErr binds Exception to a custom state using Add. Empty state defaults to
// [am.StateException], and a custom state will also add [am.StateException].
func BindErr(source, target am.Api, targetErr string) error {
	if targetErr == "" {
		targetErr = am.StateException
	}

	h := &struct {
		ExceptionState am.HandlerFinal
	}{
		ExceptionState: Add(source, target, am.StateException, targetErr),
	}

	return source.BindHandlers(h)
}

// BindStart binds Start to custom states using Add/Remove. Empty state
// defaults to Start.
func BindStart(
	source, target am.Api, activeState, inactiveState string,
) error {
	h := &struct {
		StartState am.HandlerFinal
		StartEnd   am.HandlerFinal
	}{
		StartState: Add(source, target, ss.BasicStates.Start, activeState),
		StartEnd:   Remove(source, target, ss.BasicStates.Start, inactiveState),
	}

	return source.BindHandlers(h)
}

// BindReady binds Ready to custom states using Add/. Empty state
// defaults to Ready.
func BindReady(
	source, target am.Api, activeState, inactiveState string,
) error {
	h := &struct {
		ReadyState am.HandlerFinal
		ReadyEnd   am.HandlerFinal
	}{
		ReadyState: Add(source, target, ss.BasicStates.Ready, activeState),
		ReadyEnd:   Remove(source, target, ss.BasicStates.Ready, inactiveState),
	}

	return source.BindHandlers(h)
}

// Bind binds an arbitrary state to custom states using Add and Remove.
// Empty [activeState] and [inactiveState] defaults to [source]. Each binding
// creates a separate handler struct, unlike in BindMany.
func Bind(
	source, target am.Api, state string, activeState, inactiveState string,
) error {

	if activeState == "" {
		activeState = state
	}
	if inactiveState == "" {
		inactiveState = activeState
	}

	// TODO assert source has state
	// TODO assert target has activeState and inactiveState

	// define handlers for each mutation
	var fields []reflect.StructField
	add := Add(source, target, state, activeState)
	remove := Remove(source, target, state, inactiveState)
	fields = append(fields, reflect.StructField{
		Name: state + am.SuffixState,
		Type: reflect.TypeOf(add),
	})
	fields = append(fields, reflect.StructField{
		Name: state + am.SuffixEnd,
		Type: reflect.TypeOf(remove),
	})

	// define a struct with handlers
	structType := reflect.StructOf(fields)
	val := reflect.New(structType).Elem()

	// set handlers
	val.Field(0).Set(reflect.ValueOf(add))
	val.Field(1).Set(reflect.ValueOf(remove))

	// bind handlers
	handlers := val.Addr().Interface()

	return source.BindHandlers(handlers)
}

// TODO godoc
func BindMany(
	source, target am.Api, states, targetStates am.S,
) error {

	if len(states) != len(targetStates) {
		return fmt.Errorf("%w: source and target states len mismatch",
			am.ErrStateMissing)
	}

	var fields []reflect.StructField
	var fns []am.HandlerFinal

	// define handlers for each state
	for i, name := range states {
		add := Add(source, target, name, targetStates[i])
		remove := Remove(source, target, name, targetStates[i])
		fields = append(fields, reflect.StructField{
			Name: name + am.SuffixState,
			Type: reflect.TypeOf(add),
		})
		fields = append(fields, reflect.StructField{
			Name: name + am.SuffixEnd,
			Type: reflect.TypeOf(remove),
		})
		fns = append(fns, add, remove)
	}

	// define a struct with handlers
	structType := reflect.StructOf(fields)
	val := reflect.New(structType).Elem()

	// set handlers
	for i, fn := range fns {
		val.Field(i).Set(reflect.ValueOf(fn))
	}

	// bind handlers
	handlers := val.Addr().Interface()

	return source.BindHandlers(handlers)
}

// ///// ///// /////

// ///// INTERNAL

// ///// ///// /////

func gcHandler(mach am.Api) am.HandlerDispose {
	return func(id string, ctx context.Context) {
		mach.SemLogger().RemovePipes(id)
	}
}
