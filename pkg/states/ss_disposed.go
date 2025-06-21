package states

import (
	"fmt"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// DisposedStatesDef contains all the states of the Disposed state machine.
// Required states:
// - Start
type DisposedStatesDef struct {
	*am.StatesBase

	// RegisterDisposal registers a disposal handler passed under the
	// DisposedArgHandler key.
	RegisterDisposal string
	// Disposing indicates that the machine is during the disposal process.
	Disposing string
	// Disposed indicates that the machine has disposed allocated resoruces
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
	ssD.Disposing:        {Remove: sgD.Disposed},
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
// RegisterDisposal state. It need to contain the EXPLICIT type of
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
	if ret == false && !e.IsCheck {
		err := fmt.Errorf("%w: DisposedArgHandler invalid", am.ErrInvalidArgs)
		e.Machine().AddErr(err, nil)
	}

	return ret
}

func (h *DisposedHandlers) RegisterDisposalState(e *am.Event) {
	// TODO ability to deregister a disposal handler (by ref)
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

		mach.Add1(ssD.Disposed, nil)
	}()
}
