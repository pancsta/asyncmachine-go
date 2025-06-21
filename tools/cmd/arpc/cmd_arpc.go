package main

import (
	"context"
	"os"
	"os/signal"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/repl"
	"github.com/pancsta/asyncmachine-go/tools/repl/states"
)

var ss = states.ReplStates
var randomId = false

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

	osArgs := os.Args[1:]
	for i, v := range osArgs {
		if v == "--" && len(os.Args) > i+1 {
			cliArgs = os.Args[i+2:]
			connArgs = os.Args[1 : i+1]
			break
		}
	}

	// repl
	id := "repl"
	if randomId {
		id = "repl-" + utils.RandId(4)
	}
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
			os.Exit(1)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

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
