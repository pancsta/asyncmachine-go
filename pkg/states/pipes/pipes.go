// Package pipe provide helpers to pipe states from one machine to another.
package pipes

// TODO register disposal handlers, detach from source machines

import (
	"context"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

// Add adds a pipe for an Add mutation between source and target machines.
//
// targetState: default to sourceState
func Add(
	source, target *am.Machine, sourceState string, targetState string,
) am.HandlerFinal {
	if sourceState == "" {
		panic(am.ErrStateMissing)
	}
	if targetState == "" {
		targetState = sourceState
	}
	source.LogLvl(am.LogOps, "[pipe-out:add] %s to %s", sourceState, target.Id())
	target.LogLvl(am.LogOps, "[pipe-in:add] %s from %s", targetState, source.Id())

	// TODO optimize
	source.HandleDispose(gcHandler(target))
	target.HandleDispose(gcHandler(source))

	return func(e *am.Event) {
		target.EvAdd1(e, targetState, e.Args)
	}
}

func Remove(
	source, target *am.Machine, sourceState string, targetState string,
) am.HandlerFinal {
	if sourceState == "" {
		panic(am.ErrStateMissing)
	}
	if targetState == "" {
		targetState = sourceState
	}
	source.LogLvl(am.LogOps, "[pipe-out:remove] %s to %s", sourceState,
		target.Id())
	target.LogLvl(am.LogOps, "[pipe-in:remove] %s from %s", targetState,
		source.Id())

	// TODO optimize
	source.HandleDispose(gcHandler(target))
	target.HandleDispose(gcHandler(source))

	return func(e *am.Event) {
		target.EvRemove1(e, targetState, e.Args)
	}
}

// BindConnected binds a [ss.ConnectedSchema] machine to 4 custom states. Each
// one is optional and bound with Add/Remove.
func BindConnected(
	source, target *am.Machine, disconnected, connecting, connected,
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
// [am.Exception].
func BindErr(source, target *am.Machine, targetErr string) error {

	if targetErr == "" {
		targetErr = am.Exception
	}

	h := &struct {
		ExceptionState am.HandlerFinal
	}{
		ExceptionState: Add(source, target, am.Exception, targetErr),
	}

	return source.BindHandlers(h)
}

// BindStart binds Start to custom states using Add/Remove. Empty state
// defaults to Start.
func BindStart(
	source, target *am.Machine, activeState, inactiveState string,
) error {

	if activeState == "" {
		activeState = ss.BasicStates.Start
	}
	if inactiveState == "" {
		inactiveState = ss.BasicStates.Start
	}

	h := &struct {
		StartState am.HandlerFinal
		StartEnd   am.HandlerFinal
	}{
		StartState: Add(source, target, ss.BasicStates.Start, activeState),
		StartEnd:   Remove(source, target, ss.BasicStates.Start, inactiveState),
	}

	return source.BindHandlers(h)
}

// BindReady binds Ready to custom states using Add/Remove. Empty state
// defaults to Ready.
func BindReady(
	source, target *am.Machine, activeState, inactiveState string,
) error {

	if activeState == "" {
		activeState = ss.BasicStates.Ready
	}
	if inactiveState == "" {
		inactiveState = ss.BasicStates.Ready
	}

	h := &struct {
		ReadyState am.HandlerFinal
		ReadyEnd   am.HandlerFinal
	}{
		ReadyState: Add(source, target, ss.BasicStates.Ready, activeState),
		ReadyEnd:   Remove(source, target, ss.BasicStates.Ready, inactiveState),
	}

	return source.BindHandlers(h)
}

// // Bind binds an arbitrary state to custom states using Add and Remove.
// // Empty [activeState] and [inactiveState] defaults to [source].
// // TODO
// func Bind(
// 	state string, source, target *am.Machine, activeState, inactiveState string
// ) error {
//
// 	if state == "" {
// 		return am.ErrStateMissing
// 	}
// 	if activeState == "" {
// 		activeState = state
// 	}
// 	if inactiveState == "" {
// 		inactiveState = state
// 	}
//
// 	// TODO dynamic struct init, see pkg/rpc
// 	h := &struct {
// 		StartState am.HandlerFinal
// 		StartEnd   am.HandlerFinal
// 	}{
// 		StartState: Add(source, target, ss.BasicStates.Start, activeState),
// 		StartEnd:   Remove(source, target, ss.BasicStates.Start, inactiveState),
// 	}
//
// 	return source.BindHandlers(h)
// }

func gcHandler(mach *am.Machine) am.HandlerDispose {
	return func(id string, ctx context.Context) {
		mach.LogLvl(am.LogOps, "[pipe:gc] %s", id)
	}
}
