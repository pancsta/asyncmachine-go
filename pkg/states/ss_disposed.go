package states

import (
	"fmt"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// DisposedStatesDef contains all the states of the Disposed state machine.
// One a machine implements this state mixing, it HAS TO be disposed using the
// Disposing state (instead of [am.Machine.Dispose]).
//
// Required states:
// - Start
type DisposedStatesDef struct {
	*am.StatesBase

	// RegisterDisposal registers a disposal handler passed under the
	// DisposedArgHandler key. Requires [DisposedHandlers] to be bound prior to
	// the registration. Handlers registered via RegisterDisposal can block.
	RegisterDisposal string
	// Disposing starts the machine disposal - first state-based and then calls
	// [am.Machine.Dispose].
	Disposing string
	// Disposed indicates that the machine has disposed allocated resources
	// and is ready to be garbage collected by calling [am.Machine.Dispose].
	Disposed string
}

// DisposedGroupsDef contains all the state groups Disposed state machine.
type DisposedGroupsDef struct {
	Disposed S
}

// DisposedSchema represents all relations and properties of DisposedStates.
var DisposedSchema = am.Schema{
	ssD.RegisterDisposal: {Multi: true},
	ssD.Disposing:        {Remove: SAdd(sgD.Disposed, S{ssB.Start})},
	ssD.Disposed:         {Remove: SAdd(sgD.Disposed, S{ssB.Start})},
}

// EXPORTS AND GROUPS

var (
	ssD = am.NewStates(DisposedStatesDef{})
	sgD = am.NewStateGroups(DisposedGroupsDef{
		Disposed: S{ssD.RegisterDisposal, ssD.Disposing, ssD.Disposed},
	})

	// DisposedStates contains all the states for the Disposed machine.
	DisposedStates = ssD
	// DisposedGroups contains all the state groups for the Disposed machine.
	DisposedGroups = sgD
)

// handlers

// DisposedArgHandler is the key for the disposal handler passed to the
// RegisterDisposal state. It needs to contain the EXPLICIT type of
// am.HandlerDispose, eg
//
//	var dispose am.HandlerDispose = func(id string, ctx *am.StateCtx) {
//		// ...
//	}
var DisposedArgHandler = "DisposedArgHandler"

type DisposedHandlers struct {
	// DisposedHandlers is a list of handler for pkg/states.DisposedStates
	DisposedHandlers []am.HandlerDispose
}

func (h *DisposedHandlers) RegisterDisposalEnter(e *am.Event) bool {
	fn, ok := e.Args[DisposedArgHandler].(am.HandlerDispose)
	ret := ok && fn != nil
	// avoid errs on check mutations
	if !ret && !e.IsCheck {
		err := fmt.Errorf("%w: DisposedArgHandler invalid", am.ErrInvalidArgs)
		e.Machine().AddErr(err, nil)
	}

	return ret
}

func (h *DisposedHandlers) RegisterDisposalState(e *am.Event) {
	// TODO ability to deregister a disposal handler (by ref)
	// TODO typed args?
	fn := e.Args[DisposedArgHandler].(am.HandlerDispose)
	h.DisposedHandlers = append(h.DisposedHandlers, fn)
}

func (h *DisposedHandlers) DisposingState(e *am.Event) {
	mach := e.Machine()
	ctx := mach.NewStateCtx(ssD.Disposing)

	// unblock
	go func() {
		for _, fn := range h.DisposedHandlers {
			if ctx.Err() != nil {
				return // expired
			}
			fn(mach.Id(), ctx)
		}

		// TODO retries?
		mach.Add1(ssD.Disposed, nil)
	}()
}

func (h *DisposedHandlers) DisposedState(e *am.Event) {
	go e.Machine().Dispose()
}
