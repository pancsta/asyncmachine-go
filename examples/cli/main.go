package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/joho/godotenv"

	"github.com/pancsta/asyncmachine-go/examples/cli/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var ss = states.CliStates

type Args struct {
	Foo1     bool          `arg:"--foo-1" help:"Foo help"`
	Bar2     bool          `arg:"--bar-2" help:"Bar help"`
	Duration time.Duration `arg:"-d,--duration" default:"3s" help:"Duration of the demo"`
	Debug    bool          `default:"true" help:"Enable debugging for asyncmachine"`
}

type cli struct {
	*am.ExceptionHandler

	Mach *am.Machine
	Args *Args
}

var args Args

func main() {
	p := arg.MustParse(&args)
	ctx := context.Background()
	cli, err := newCli(ctx, &args)
	if err != nil {
		panic(err)
	}

	if args.Foo1 {
		cli.Mach.Add1(ss.Foo1, nil)
	} else if args.Bar2 {
		cli.Mach.Add1(ss.Bar2, nil)
	} else {
		p.Fail("no flag")
	}

	<-cli.Mach.WhenNot1(ss.Start, nil)
}

// newCli creates a new CLI with a state machine.
func newCli(ctx context.Context, args *Args) (*cli, error) {
	if args.Debug {
		fmt.Printf("debugging enabled\n")
		// load .env
		_ = godotenv.Load()
	}

	ret := &cli{
		Args: args,
	}
	mach, err := am.NewCommon(ctx, "cli", states.CliSchema, ss.Names(),
		ret, nil, &am.Opts{
			DontPanicToException: args.Debug,
		})
	if err != nil {
		return nil, err
	}
	if args.Debug {
		amhelp.MachDebugEnv(mach)
	}
	ret.Mach = mach

	return ret, nil
}

func (c *cli) StartState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ss.Start)
	fmt.Printf("starting for %s\n", e.Transition().CalledStates())
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(c.Args.Duration):
		}
		fmt.Printf("start done\n")
		c.Mach.Remove1(ss.Start, nil)
	}()
}
