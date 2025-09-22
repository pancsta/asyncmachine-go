package main

import (
	"context"
	"time"

	"github.com/joho/godotenv"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func init() {
	// load .env
	_ = godotenv.Load()

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetEnvLogLevel(am.LogOps)
}

func main() {
	// init the state machine
	mach := am.New(nil, am.Schema{
		"ProcessingFile": { // async
			Remove: am.S{"FileProcessed"},
		},
		"FileProcessed": { // async
			Remove: am.S{"ProcessingFile"},
		},
		"InProgress": { // sync
			Auto:    true,
			Require: am.S{"ProcessingFile"},
		},
	}, &am.Opts{LogLevel: am.LogOps, Id: "raw-strings"})
	amhelp.MachDebugEnv(mach)
	mach.BindHandlers(&Handlers{
		Filename: "README.md",
	})
	// change the state
	mach.Add1("ProcessingFile", nil)

	// wait for completed
	select {
	case <-time.After(5 * time.Second):
		println("timeout")
	case <-mach.WhenErr(nil):
		println("err:", mach.Err())
	case <-mach.When1("FileProcessed", nil):
		println("done")
	}
}

type Handlers struct {
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
	mach := e.Machine()
	// tick-based context
	stateCtx := mach.NewStateCtx("ProcessingFile")
	go func() {
		// block in the background, locks needed
		if stateCtx.Err() != nil {
			return // expired
		}
		// blocking call
		err := processFile(h.Filename, stateCtx)
		if err != nil {
			mach.AddErr(err, nil)
			return
		}
		// re-check the tick ctx after a blocking call
		if stateCtx.Err() != nil {
			return // expired
		}
		// move to the next state in the flow
		mach.Add1("FileProcessed", am.A{"beaver": "1"})
	}()
}

func processFile(name string, ctx context.Context) error {
	time.Sleep(1 * time.Second)
	return nil
}
