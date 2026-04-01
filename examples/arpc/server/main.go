package main

import (
	"context"
	"fmt"
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
	// amhelp.SetEnvLogLevel(am.LogOps)
	// os.Setenv(arpc.EnvAmRpcLogServer, "1")
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// worker
	netSourceMach, err := newNetSource(ctx)
	if err != nil {
		panic(err)
	}

	// arpc
	server, err := newServer(ctx, addr, netSourceMach)
	if err != nil {
		panic(err)
	}

	// server start
	server.Start(nil)
	err = amhelp.WaitForAll(ctx, 3*time.Second,
		server.Mach.When1(ssrpc.ServerStates.RpcReady, ctx))
	if err != nil {
		panic(err)
	}
	fmt.Printf("Started aRPC server on %s\n", server.Addr)

	// wait for a client
	select {
	case <-server.Mach.When1(ssrpc.ServerStates.Ready, ctx):
		fmt.Printf("Client connected\n")
	case <-ctx.Done():
	}

	// periodically send data to the client
	t := time.NewTicker(3 * time.Second)
	for {
		exit := false
		select {
		case <-t.C:
			err := server.SendPayload(ctx, nil, &arpc.MsgSrvPayload{
				Name:   "mypayload",
				Source: "netSrc1",
				Data:   "Hello aRPC",
			})
			if err != nil {
				panic(err)
			}
		case <-ctx.Done():
			exit = true
		}
		if exit {
			break
		}
	}

	fmt.Println("bye")
}

func newNetSource(ctx context.Context) (*am.Machine, error) {

	// init
	netSource, err := am.NewCommon(ctx, "netSrc1", states.ExampleSchema, ss.Names(), &workerHandlers{}, nil, nil)
	if err != nil {
		return nil, err
	}
	amhelp.MachDebugEnv(netSource)

	return netSource, nil
}

func newServer(ctx context.Context, addr string, netSource *am.Machine) (*arpc.Server, error) {

	// init
	s, err := arpc.NewServer(ctx, addr, netSource.Id(), netSource, nil)
	if err != nil {
		panic(err)
	}
	amhelp.MachDebugEnv(s.Mach)

	// start
	s.Start(nil)
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
	fmt.Println("FooState")
}

func (h *workerHandlers) BarState(e *am.Event) {
	fmt.Println("BarState")
}

func (h *workerHandlers) BazState(e *am.Event) {
	fmt.Println("BazState")
}
