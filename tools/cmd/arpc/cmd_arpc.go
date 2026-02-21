package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/repl"
	"github.com/pancsta/asyncmachine-go/tools/repl/states"
)

var ss = states.ReplStates

// var randomId = false

func init() {
	// amhelp.EnableDebugging(false)
	// amhelp.EnableDebugging(true)
}

type S = am.S
type T = am.Time

func main() {
	ctx := context.Background()

	var cliArgs []string
	var connArgs []string

	// all OS args
	osArgs := os.Args[1:]
	for i, v := range osArgs {
		if v == "--" && len(os.Args) > i+1 {
			// args for REPL to execute as CLI
			cliArgs = os.Args[i+2:]
			// args to config REPL
			connArgs = os.Args[1 : i+1]
			break
		}
	}

	// repl
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	id := "repl-" + amhelp.Hash(wd, 6)
	r, err := repl.New(ctx, id)
	if err != nil {
		panic(err)
	}
	rootCmd := repl.NewRootCommand(r, cliArgs, osArgs)
	if len(connArgs) > 0 {
		rootCmd.SetArgs(connArgs)
	}
	r.Cmd = rootCmd

	// cobra
	err = rootCmd.Execute()
	if err != nil {
		if amhelp.IsTelemetry() {
			time.Sleep(time.Second)
		}
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}

	// TODO NotifyContext
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// waits
	select {

	// signal
	case <-sigCh:
		r.Mach.Add1(ss.Disposing, nil)
	// CLI
	case <-r.Mach.WhenNot1(ss.ReplMode, nil):
	// exit
	case <-r.Mach.When1(ss.Disposed, nil):
	}

	// fmt.Println("bye")
	r.Mach.Dispose()

	if amhelp.IsTelemetry() {
		time.Sleep(time.Second)
	}

	os.Exit(0)
}
