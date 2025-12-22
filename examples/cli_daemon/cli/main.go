package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/joho/godotenv"

	"github.com/pancsta/asyncmachine-go/examples/cli_daemon/states"
	"github.com/pancsta/asyncmachine-go/examples/cli_daemon/types"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

var ss = states.DaemonStates

type Args struct {
	Foo1     bool          `arg:"--foo-1" help:"Foo help"`
	Bar2     bool          `arg:"--bar-2" help:"Bar help"`
	Addr     string        `default:"localhost:8090"`
	Duration time.Duration `arg:"-d,--duration" default:"3s" help:"Duration of the op"`
	Debug    bool          `default:"true" help:"Enable debugging for asyncmachine"`
}

var args Args

func main() {
	p := arg.MustParse(&args)
	ctx := context.Background()
	cli, err := newCli(ctx, &args)
	if err != nil {
		p.Fail(err.Error())
	}
	netmach := cli.C.NetMach

	if args.Foo1 {
		netmach.Add1(ss.OpFoo1, types.PassRpc(&types.ARpc{
			Duration: args.Duration,
		}))
	} else if args.Bar2 {
		netmach.Add1(ss.OpBar2, types.PassRpc(&types.ARpc{
			Duration: args.Duration,
		}))
	} else {
		p.Fail("no flag")
	}

	err = amhelp.WaitForAll(ctx, 5*time.Second,
		cli.Mach.When1(ssrpc.ConsumerStates.WorkerPayload, nil))
	if err != nil {
		p.Fail(err.Error())
	}
}

// newCli creates a new CLI with a state machine.
func newCli(ctx context.Context, args *Args) (*cli, error) {
	if args.Debug {
		fmt.Printf("debugging enabled\n")
		// load .env
		_ = godotenv.Load()
	}

	// init cli
	consumer := am.New(ctx, ssrpc.ConsumerSchema, nil)
	err := consumer.BindHandlers(&cli{})
	if err != nil {
		return nil, err
	}

	// init aRPC client
	c, err := newClient(ctx, args.Addr, consumer, states.DaemonSchema)
	if err != nil {
		return nil, err
	}

	// connect
	c.Start()
	err = amhelp.WaitForAll(ctx, 3*time.Second,
		c.Mach.When1(ssrpc.ClientStates.Ready, ctx))
	fmt.Printf("Connected to aRPC %s\n", c.Addr)

	return &cli{
		Mach: consumer,
		C:    c,
		Args: args,
	}, nil
}

func newClient(
	ctx context.Context, addr string, consumer *am.Machine, netSrcSchema am.Schema,
) (*arpc.Client, error) {

	// init
	id := "cli-" + time.Now().Format(time.RFC3339Nano)
	c, err := arpc.NewClient(ctx, addr, id, netSrcSchema, &arpc.ClientOpts{
		Consumer: consumer,
	})
	if err != nil {
		return nil, err
	}
	amhelp.MachDebugEnv(c.Mach)

	return c, nil
}

type cli struct {
	*am.ExceptionHandler

	Mach *am.Machine
	C    *arpc.Client
	Args *Args
}

func (c *cli) WorkerPayloadState(e *am.Event) {
	e.Machine().Remove1(ssrpc.ConsumerStates.WorkerPayload, nil)

	args := arpc.ParseArgs(e.Args)
	println("Payload: " + args.Payload.Data.(string))
}
