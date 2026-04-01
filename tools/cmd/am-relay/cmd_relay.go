package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexflint/go-arg"
	"github.com/pancsta/asyncmachine-go/internal/utils"

	"github.com/pancsta/asyncmachine-go/tools/relay"
	"github.com/pancsta/asyncmachine-go/tools/relay/states"
	"github.com/pancsta/asyncmachine-go/tools/relay/types"
)

var ss = states.RelayStates
var args types.Args

func main() {
	p := arg.MustParse(&args)
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if args.Version {
		fmt.Println(utils.GetVersion())
		os.Exit(0)
	}

	if p.Subcommand() == nil {
		p.Fail("missing subcommand (" + types.CmdRotateDbg + ")")
	}

	switch {
	case args.RotateDbg != nil:
		if err := run(ctx, args); err != nil {
			_ = p.FailSubcommand(err.Error(), types.CmdRotateDbg)
		}
	case args.Wasm != nil:
		if err := run(ctx, args); err != nil {
			_ = p.FailSubcommand(err.Error(), types.CmdRotateDbg)
		}
	}
}

func run(ctx context.Context, args types.Args) error {
	r, err := relay.New(ctx, args)
	if err != nil {
		return err
	}
	r.Mach.Add1(ss.Start, nil)
	<-r.Mach.WhenNot1(ss.Start, nil)
	if r.Mach.Is1(ss.Exception) {
		fmt.Printf("last err: %v\n", r.Mach.Err())
	}
	fmt.Println("exiting am-relay, bye")

	return nil
}
