package main

import (
	"context"
	"fmt"
	"math/rand"
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
	// amhelp.SetEnvLogLevel(am.LogOps)

	// TODO register args
	// gob.Register(&types.ARpc{})
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
	client, err := newClient(ctx, addr, states.ExampleSchema)
	if err != nil {
		panic(err)
	}

	// connect
	client.Start()
	err = amhelp.WaitForAll(ctx, 3*time.Second,
		client.Mach.When1(ssrpc.ClientStates.Ready, ctx))
	fmt.Printf("Connected to aRPC %s\n", client.Addr)

	// randomly mutate the remote worker
	t := time.NewTicker(1 * time.Second)
	for {
		exit := false
		select {
		case <-t.C:
			switch rand.Intn(2) {
			case 0:
				client.NetMach.Add1(ss.Foo, nil)
			case 1:
				client.NetMach.Add1(ss.Bar, nil)
			case 2:
				client.NetMach.Add1(ss.Baz, nil)
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

func newClient(
	ctx context.Context, addr string, netSrcSchema am.Schema,
) (*arpc.Client, error) {

	// consumer
	consumer := am.New(ctx, ssrpc.ConsumerSchema, nil)
	err := consumer.BindHandlers(&clientHandlers{})
	if err != nil {
		return nil, err
	}

	// init
	c, err := arpc.NewClient(ctx, addr, "clientid", netSrcSchema, &arpc.ClientOpts{
		Consumer: consumer,
	})
	if err != nil {
		panic(err)
	}
	amhelp.MachDebugEnv(c.Mach)

	return c, nil
}

type clientHandlers struct {
	*am.ExceptionHandler
}

func (h *clientHandlers) WorkerPayloadState(e *am.Event) {
	e.Machine().Remove1(ssrpc.ConsumerStates.WorkerPayload, nil)

	args := arpc.ParseArgs(e.Args)
	println("Payload: " + args.Payload.Data.(string))
}
