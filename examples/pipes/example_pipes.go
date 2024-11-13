package main

import (
	"context"
	"time"

	"github.com/joho/godotenv"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

func init() {
	// load .env
	_ = godotenv.Load()

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetLogLevel(am.LogChanges)
}

func main() {
	ctx := context.Background()

	// init state machines
	mach1 := am.New(ctx, am.Struct{
		"Ready":       {},
		"Foo":         {},
		"Bar":         {},
		"Custom":      {},
		"Healthcheck": {Multi: true},
	}, &am.Opts{LogLevel: am.LogOps, ID: "source"})
	mach2 := am.New(ctx, am.Struct{
		"Ready":       {},
		"Custom":      {},
		"Healthcheck": {Multi: true},
	}, &am.Opts{LogLevel: am.LogOps, ID: "destination"})
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

	// pipe custom states (anon handlers)
	pipeCustom := &struct {
		CustomState am.HandlerFinal
		CustomEnd   am.HandlerFinal
	}{
		CustomState: ampipe.Add(mach1, mach2, "Custom", ""),
		CustomEnd:   ampipe.Remove(mach1, mach2, "Custom", ""),
	}
	err = mach1.BindHandlers(pipeCustom)
	if err != nil {
		panic(err)
	}

	// debug
	time.Sleep(time.Second)
}
