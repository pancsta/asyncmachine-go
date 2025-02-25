package main

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
	// amhelp.SetLogLevel(am.LogChanges)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// handle exit TODO not working
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// workers
	for i := range 3 {
		worker, err := newWorker(ctx, i)
		if err != nil {
			log.Print(err)
		}

		time.Sleep(100 * time.Millisecond)
		fmt.Printf("start %d\n", i)
		worker.Log("hello")
	}

	// periodically make some changes
	t := time.NewTicker(3 * time.Second)
	// names := ss.Names()
	for {
		exit := false
		select {
		case <-t.C:
			// randState := names[rand.N(len(names))]
			// worker.Add1(randState, nil)
		case <-sigChan:
			cancel()
			exit = true
		case <-ctx.Done():
			exit = true
		}
		if exit {
			break
		}
	}

	fmt.Println("bye")
}

func newWorker(ctx context.Context, num int) (*am.Machine, error) {
	// init

	handlers := &workerHandlers{}
	id := fmt.Sprintf("worker%d", num)
	worker, err := am.NewCommon(ctx, id, states.ExampleStruct,
		ss.Names(), handlers, nil, nil)
	if err != nil {
		return nil, err
	}
	handlers.Mach = worker

	// telemetry

	amhelp.MachDebugEnv(worker)
	// worker.SetLogArgs(am.NewArgsMapper([]string{"log"}, 0))
	worker.SetLogLevel(am.LogChanges)
	// start a REPL aRPC server, create an addr file
	arpc.MachRepl(worker, "", "tmp", nil)

	return worker, nil
}

type workerHandlers struct {
	*am.ExceptionHandler
	Mach *am.Machine
}

func (h *workerHandlers) FooState(e *am.Event) {
	fmt.Println("FooState")
	h.Mach.Log("FooState")
}

func (h *workerHandlers) BarState(e *am.Event) {
	fmt.Println("BarState")
	h.Mach.Log("BarState")
}

func (h *workerHandlers) BazState(e *am.Event) {
	fmt.Println("BazState")
	h.Mach.Log("BarState")
}
