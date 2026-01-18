package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/joho/godotenv"
	"github.com/teivah/onecontext"

	"github.com/pancsta/asyncmachine-go/examples/cli_daemon/states"
	"github.com/pancsta/asyncmachine-go/examples/cli_daemon/types"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

var ss = states.DaemonStates

type Args struct {
	Addr  string `default:"localhost:8090"`
	Debug bool   `default:"true" help:"Enable debugging for asyncmachine"`
}

type daemon struct {
	Mach *am.Machine
	Args *Args
	S    *arpc.Server
}

var args Args

func main() {
	p := arg.MustParse(&args)
	ctx := context.Background()
	d, err := newDaemon(ctx, &args)
	if err != nil {
		p.Fail(err.Error())
	}

	<-d.Mach.WhenQueue(d.Mach.Add1(ss.Start, nil))
	<-d.Mach.WhenNot1(ss.Start, nil)
}

// newDaemon creates a new CLI with a state machine.
func newDaemon(ctx context.Context, args *Args) (*daemon, error) {
	if args.Debug {
		fmt.Printf("debugging enabled\n")
		// load .env
		_ = godotenv.Load()
	}

	// init daemon
	d := &daemon{
		Args: args,
	}
	mach, err := am.NewCommon(ctx, "daemon", states.DaemonSchema, ss.Names(),
		d, nil, &am.Opts{
			DontPanicToException: args.Debug,
			LogLevel:             am.LogChanges,
			LogArgs:              types.LogArgs,
		})
	if err != nil {
		return nil, err
	}
	if args.Debug {
		amhelp.MachDebugEnv(mach)
	}
	d.Mach = mach

	// init aRPC server
	s, err := arpc.NewServer(ctx, args.Addr, d.Mach.Id(), d.Mach, nil)
	if err != nil {
		return nil, err
	}
	if args.Debug {
		amhelp.MachDebugEnv(s.Mach)
	}
	d.S = s
	s.Start()
	if err != nil {
		return nil, err
	}
	// TODO print when ready
	fmt.Printf("listening on %s\n", args.Addr)

	// REPL on port+1
	host, port, _ := net.SplitHostPort(args.Addr)
	portNum, _ := strconv.Atoi(port)
	addrRepl := host + ":" + strconv.Itoa(portNum+1)
	err = arpc.MachRepl(mach, addrRepl, &arpc.ReplOpts{
		AddrDir:    ".",
		ArgsPrefix: types.APrefix,
		Args:       &types.ARpc{},
	})
	if err != nil {
		return nil, err
	}
	// TODO print when ready
	fmt.Printf("REPL listening on %s\n", addrRepl)

	return d, nil
}

func (d *daemon) OpFoo1State(e *am.Event) {
	d.hOp(e)
}
func (d *daemon) OpBar2State(e *am.Event) {
	d.hOp(e)
}

// hOp is a sub-handler for common operation logic.
func (d *daemon) hOp(e *am.Event) {
	ctxDaemon := d.Mach.NewStateCtx(ss.Start)
	ctxRpc := d.S.Mach.NewStateCtx(ssrpc.ServerStates.ClientConnected)
	ctx, _ := onecontext.Merge(ctxDaemon, ctxRpc)
	duration := types.ParseArgs(e.Args).Duration
	called := e.Transition().CalledStates()

	fmt.Printf("starting op %s for %s\n", called, duration)
	go func() {
		defer d.Mach.Remove(called, nil)
		select {
		case <-ctx.Done():
			fmt.Printf("ctx done\n")
			return
		case <-time.After(duration):
		}

		fmt.Printf("op done\n")
		// TODO
		d.Mach.AddErr(d.S.SendPayload(ctx, e, &arpc.MsgSrvPayload{
			Name: "opdone",
			Data: fmt.Sprintf("done for %s", called),
		}), nil)
	}()
}
