package main

// Steps:
// 1. go run .
// 2. go run github.com/pancsta/asyncmachine-go/tools/cmd/arpc@latest -d tmp

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pancsta/asyncmachine-go/examples/arpc/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

var ss = states.ExampleStates

func init() {
	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(true)
	// amhelp.SetEnvLogLevel(am.LogOps)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// handle exit TODO not working
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// workers
	for i := range 3 {
		worker, err := newStateSource(ctx, i)
		if err != nil {
			log.Print(err)
		}

		time.Sleep(100 * time.Millisecond)
		fmt.Printf("start %d\n", i)
		worker.Log("hello")
	}

	// wait
	<-sigChan

	fmt.Println("bye")
}

func newStateSource(ctx context.Context, num int) (*am.Machine, error) {
	// init
	handlers := &handlers{}
	id := fmt.Sprintf("worker%d", num)
	source, err := am.NewCommon(ctx, id, states.ExampleSchema,
		ss.Names(), handlers, nil, nil)
	if err != nil {
		return nil, err
	}
	handlers.Mach = source

	// telemetry

	amhelp.MachDebugEnv(source)
	// worker.SemLogger().SetArgsMapper(am.NewArgsMapper([]string{"log"}, 0))
	source.SemLogger().SetLevel(am.LogChanges)
	source.SetGroups(states.ExampleGroups, states.ExampleStates)
	// start a REPL aRPC server, create an addr file of a rand addr
	err = arpc.MachRepl(source, "", &arpc.ReplOpts{AddrDir: "tmp"})
	if err != nil {
		return nil, err
	}

	return source, nil
}

type handlers struct {
	*am.ExceptionHandler
	Mach *am.Machine
}

func (h *handlers) FooState(e *am.Event) {
	fmt.Println("FooState")
	h.Mach.Log("FooState")
}

func (h *handlers) BarState(e *am.Event) {
	fmt.Println("BarState")
	h.Mach.Log("BarState")
}

func (h *handlers) BazState(e *am.Event) {
	fmt.Println("BazState")
	h.Mach.Log("BarState")
}
