// Package pipes provide helpers to pipe states from one machine to another.
package pipes

// TODO implement removal of pipes via:
//  - dispose source handlers when target mach disposes
//  - binding-struct
//  - tagging of handler structs

import (
	"context"
	"fmt"
	"strings"

	"github.com/pancsta/asyncmachine-go/internal/utils"
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
		// TODO add err to source
		panic(am.ErrStateMissing)
	}
	if targetState == "" {
		targetState = sourceState
	}

	// graph info
	source.SemLogger().AddPipeOut(true, sourceState, target.Id())
	target.SemLogger().AddPipeIn(true, targetState, source.Id())

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
			// TODO optimize: fork only for non-local
			go target.EvAdd(e, names, nil)
		} else {
			go target.EvAdd(e, names, e.Args)
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
		// TODO add err to source
		panic(am.ErrStateMissing)
	}
	if targetState == "" {
		targetState = sourceState
	}

	// graph info
	source.SemLogger().AddPipeOut(false, sourceState, target.Id())
	target.SemLogger().AddPipeIn(false, targetState, source.Id())

	// TODO optimize
	source.OnDispose(gcHandler(target))
	target.OnDispose(gcHandler(source))

	return func(e *am.Event) {
		// flat skips unnecessary mutations
		if flat && target.Not1(targetState) {
			return
		} else if flat {
			// TODO optimize: fork only for non-local
			go target.EvRemove1(e, targetState, nil)
		} else {
			go target.EvRemove1(e, targetState, e.Args)
		}
	}
}

// ///// ///// /////

// ///// BINDS

// ///// ///// /////

// BindAny binds a whole machine via the global AnyState handler and Set
// mutation. The target mutates the same number of times as the source.
func BindAny(source, target am.Api) error {

	// validate
	missing := am.StatesDiff(source.StateNames(), target.StateNames())
	if len(missing) > 0 {

		return fmt.Errorf("BindAny: %w in target: %s", am.ErrStateMissing, missing)
	}

	// AnyState
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
	// TODO
	id := "TODO" + utils.RandId(5)
	s := ss.ConnectedStates
	return source.BindHandlerMaps(id, nil, map[string]am.HandlerFinal{
		"DisconnectedState":  Add(source, target, s.Disconnected, disconnected),
		"DisconnectedEnd":    Remove(source, target, s.Disconnected, disconnected),
		"ConnectingState":    Add(source, target, s.Connecting, connecting),
		"ConnectingEnd":      Remove(source, target, s.Connecting, connecting),
		"ConnectedState":     Add(source, target, s.Connected, connected),
		"ConnectedEnd":       Remove(source, target, s.Connected, connected),
		"DisconnectingState": Add(source, target, s.Disconnecting, disconnecting),
		"DisconnectingEnd":   Remove(source, target, s.Disconnecting, disconnecting),
	})
}

// BindErr binds Exception to a custom state using Add. Empty state defaults to
// [am.StateException], and a custom state will also add [am.StateException].
func BindErr(source, target am.Api, targetErr string) error {
	if targetErr == "" {
		targetErr = am.StateException
	}
	// TODO
	id := "TODO" + utils.RandId(5)
	return source.BindHandlerMaps(id, nil, map[string]am.HandlerFinal{
		"ExceptionState": Add(source, target, am.StateException, targetErr),
	})
}

// BindStart binds Start to custom states using Add/Remove. Empty state
// defaults to Start.
func BindStart(
	source, target am.Api, activeState, inactiveState string,
) error {
	// TODO
	id := "TODO" + utils.RandId(5)
	return source.BindHandlerMaps(id, nil, map[string]am.HandlerFinal{
		"StartState": Add(source, target, ss.BasicStates.Start, activeState),
		"StartEnd":   Remove(source, target, ss.BasicStates.Start, inactiveState),
	})
}

// BindReady binds Ready to custom states using Add/. Empty state
// defaults to Ready.
func BindReady(
	source, target am.Api, activeState, inactiveState string,
) error {
	// TODO
	id := "TODO" + utils.RandId(5)
	return source.BindHandlerMaps(id, nil, map[string]am.HandlerFinal{
		"ReadyState": Add(source, target, ss.BasicStates.Ready, activeState),
		"ReadyEnd":   Remove(source, target, ss.BasicStates.Ready, inactiveState),
	})
}

// Bind binds an arbitrary state to custom states using Add and Remove.
// Empty [activeState] and [inactiveState] defaults to [source]. Each binding
// creates a separate handler struct, unlike in BindMany.
func Bind(
	source, target am.Api, state string, activeState, inactiveState string,
) error {
	// TODO use BindHandlerMaps

	if activeState == "" {
		activeState = state
	}
	if inactiveState == "" {
		inactiveState = activeState
	}

	// TODO assert source has state
	// TODO assert target has activeState and inactiveState

	// TODO
	id := "TODO" + utils.RandId(5)
	return source.BindHandlerMaps(id, nil, map[string]am.HandlerFinal{
		state + am.SuffixState: Add(source, target, state, activeState),
		state + am.SuffixEnd:   Remove(source, target, state, inactiveState),
	})
}

// BindMany binds arbitrary states to a mirrored list of states using Add and
// Remove. Only one handler struct is created for all the bindings.
func BindMany(
	source, target am.Api, states, targetStates am.S,
) error {
	// TODO use BindHandlerMaps

	if targetStates == nil {
		targetStates = states
	}

	if len(states) != len(targetStates) {
		return fmt.Errorf("%w: source and target states len mismatch",
			am.ErrStateMissing)
	}

	// TODO
	id := "TODO" + utils.RandId(5)

	// define handlers for each state
	finals := map[string]am.HandlerFinal{}
	for i, name := range states {
		finals[name+am.SuffixState] = Add(source, target, name, targetStates[i])
		finals[name+am.SuffixEnd] = Remove(source, target, name, targetStates[i])
	}

	return source.BindHandlerMaps(id, nil, finals)
}

// ///// ///// /////

// ///// INTERNAL

// ///// ///// /////

func gcHandler(mach am.Api) am.HandlerDispose {
	return func(id string, ctx context.Context) {
		mach.SemLogger().RemovePipes(id)
	}
}
