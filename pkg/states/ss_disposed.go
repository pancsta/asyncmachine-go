package states

import (
	"fmt"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
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

// DisposedHandlers handle disposal and NEEDS to be initialized manually.
//
//	  h := &Handlers{
//			DisposedHandlers: &ssam.DisposedHandlers{},
//	  }
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

// RegisterDisposalState registers an external / dynamic disposal handler.
// Machine handlers should overload DisposingState and call super instead.
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
		mach.EvAdd1(e, ssD.Disposed, nil)
	}()
}

func (h *DisposedHandlers) DisposedState(e *am.Event) {
	go e.Machine().Dispose()
}
