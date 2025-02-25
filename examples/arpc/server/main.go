package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pancsta/asyncmachine-go/examples/arpc/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

const addr = "localhost:8090"

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

	// handle exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// worker
	worker, err := newWorker(ctx)
	if err != nil {
		panic(err)
	}

	// arpc
	server, err := newServer(ctx, addr, worker)
	if err != nil {
		panic(err)
	}

	// server start
	server.Start()
	err = amhelp.WaitForAll(ctx, 3*time.Second,
		server.Mach.When1(ssrpc.ServerStates.RpcReady, ctx))
	if err != nil {
		panic(err)
	}
	fmt.Printf("Started aRPC server on %s\n", server.Addr)

	// wait for a client
	err = amhelp.WaitForAll(ctx, 3*time.Second,
		server.Mach.When1(ssrpc.ServerStates.Ready, ctx))

	// periodically send data to the client
	t := time.NewTicker(3 * time.Second)
	for {
		exit := false
		select {
		case <-t.C:
			worker.Add1(ssrpc.WorkerStates.SendPayload, arpc.Pass(&arpc.A{
				Name: "mypayload",
				Payload: &arpc.ArgsPayload{
					Name:   "mypayload",
					Source: "worker1",
					Data:   "Hello aRPC",
				},
			}))
		case <-ctx.Done():
			exit = true
		}
		if exit {
			break
		}
	}

	fmt.Println("bye")
}

func newWorker(ctx context.Context) (*am.Machine, error) {

	// init
	worker, err := am.NewCommon(ctx, "worker", states.ExampleStruct, ss.Names(), &workerHandlers{}, nil, nil)
	if err != nil {
		return nil, err
	}
	amhelp.MachDebugEnv(worker)

	return worker, nil
}

func newServer(ctx context.Context, addr string, worker *am.Machine) (*arpc.Server, error) {

	// init
	s, err := arpc.NewServer(ctx, addr, worker.Id(), worker, nil)
	if err != nil {
		panic(err)
	}
	amhelp.MachDebugEnv(s.Mach)

	// start
	s.Start()
	err = amhelp.WaitForAll(ctx, 2*time.Second,
		s.Mach.When1(ssrpc.ServerStates.RpcReady, ctx))
	if err != nil {
		return nil, err
	}

	return s, nil
}

type workerHandlers struct {
	*am.ExceptionHandler
}

func (h *workerHandlers) FooState(e *am.Event) {
	fmt.Print("FooState")
}

func (h *workerHandlers) BarState(e *am.Event) {
	fmt.Print("BarState")
}

func (h *workerHandlers) BazState(e *am.Event) {
	fmt.Print("BazState")
}
