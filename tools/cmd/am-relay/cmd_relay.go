package main

import (
	"context"
	"fmt"

	"github.com/alexflint/go-arg"

	"github.com/pancsta/asyncmachine-go/tools/relay"
	"github.com/pancsta/asyncmachine-go/tools/relay/states"
	"github.com/pancsta/asyncmachine-go/tools/relay/types"
)

var ss = states.RelayStates
var args types.Args

func main() {
	p := arg.MustParse(&args)
	ctx := context.Background()
	if p.Subcommand() == nil {
		p.Fail("missing subcommand (" + types.CmdRotateDbg + ")")
	}

	switch {
	case args.RotateDbg != nil:
		if err := rotateDbg(ctx, &args); err != nil {
			_ = p.FailSubcommand(err.Error(), types.CmdRotateDbg)
		}
	}

}

func rotateDbg(ctx context.Context, args *types.Args) error {
	r, err := relay.New(ctx, args, func(msg string, args ...any) {
		fmt.Printf(msg, args...)
	})
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
