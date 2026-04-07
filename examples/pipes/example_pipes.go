package main

import (
	"context"
	"fmt"
	"time"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

func init() {
	// manual logging
	// amhelp.SetEnvLogLevel(am.LogOps)
	// os.Setenv(amhelp.EnvAmLogPrint, "2")

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetEnvLogLevel(am.LogOps)
}

func main() {
	ctx := context.Background()

	// init state machines
	mach1 := am.New(ctx, am.Schema{
		"Ready":       {},
		"Foo":         {},
		"Bar":         {},
		"Custom":      {},
		"Healthcheck": {Multi: true},
	}, &am.Opts{LogLevel: am.LogOps, Id: "source"})
	mach2 := am.New(ctx, am.Schema{
		"Ready":       {},
		"Foo":         {},
		"Bar":         {},
		"Custom":      {},
		"Healthcheck": {Multi: true},
	}, &am.Opts{LogLevel: am.LogOps, Id: "destination"})
	amhelp.MachDebugEnv(mach1)
	amhelp.MachDebugEnv(mach2)

	// pipe conventional states
	err := ampipe.BindReady(mach1, mach2, "", "")
	if err != nil {
		panic(err)
	}
	err = ampipe.BindErr(mach1, mach2, "")
	if err != nil {
		panic(err)
	}

	// bind single
	err = ampipe.Bind(mach1, mach2, "Custom", "", "")
	if err != nil {
		panic(err)
	}

	// bind many
	err = ampipe.BindMany(mach1, mach2, am.S{"Foo", "Bar"}, nil)
	if err != nil {
		panic(err)
	}

	mach1.Add1("Ready", nil)
	mach1.Add(am.S{"Foo", "Bar"}, nil)

	// pipes are async, wait needed
	err = amhelp.WaitForAll(ctx, time.Second, mach2.When(am.S{"Foo", "Bar", "Ready"}, nil))
	if err != nil {
		panic(err)
	}

	fmt.Println("done")
}
