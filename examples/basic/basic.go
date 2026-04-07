// ProcessingFile to FileProcessed
// 1 async and 1 sync state
package main

import (
	"context"
	"time"

	"github.com/pancsta/asyncmachine-go/examples/basic/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var ss = states.States

func main() {
	ctx := context.Background()
	// init the state machine
	mach := am.New(nil, states.Schema, nil)
	mach.BindHandlers(&Handlers{
		Filename: "README.md",
		Mach:     mach,
	})
	// change the state
	mach.Add1(ss.ProcessingFile, nil)
	// wait for completed
	err := amhelp.WaitForErrAll(ctx, 5*time.Second, mach,
		mach.When1(ss.FileProcessed, nil))
	if ctx.Err() != nil {
		return // expired
	} else if err != nil {
		println("err: ", err)
	} else {
		println("done")
	}
}

type Handlers struct {
	Mach     *am.Machine
	Filename string
}

// negotiation handler
func (h *Handlers) ProcessingFileEnter(e *am.Event) bool {
	// read-only ops
	// decide if moving fwd is ok
	// no blocking
	// lock-free critical section
	return true
}

// final handler
func (h *Handlers) ProcessingFileState(e *am.Event) {
	// read & write ops
	// no blocking
	// lock-free critical section
	mach := h.Mach
	// clock-based expiration context
	ctx := mach.NewStateCtx(ss.ProcessingFile)
	// unblock
	mach.Fork(ctx, e, func() {
		// blocking call
		err := processFile(ctx, h.Filename)
		// re-check the state ctx after a blocking call
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			mach.AddErr(err, nil)
			return
		}
		// move to the next state in the flow
		mach.Add1(ss.FileProcessed, nil)
	})
}

func processFile(_ context.Context, _ string) error {
	time.Sleep(2 * time.Second)
	return nil
}
