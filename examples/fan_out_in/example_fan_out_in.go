package main

import (
	"context"
	"time"

	"github.com/joho/godotenv"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func init() {
	// load .env
	_ = godotenv.Load()

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	amhelp.EnableDebugging(false)
	amhelp.SetLogLevel(am.LogChanges)
}

func main() {
	ctx := context.Background()

	states := am.Struct{
		// task start state
		"Task": {Require: am.S{"Start"}},
		// task done state
		"TaskDone": {},

		// rest

		"Start": {},
		"Ready": {
			Auto:    true,
			Require: am.S{"TaskDone"},
		},
		"Healthcheck": {Multi: true},
	}
	names := am.S{"Start", "Task", "TaskDone", "Ready", "Healthcheck", am.Exception}

	// init state machine
	mach, err := am.NewCommon(ctx, "fan", states, names, &am.ExceptionHandler{}, nil, &am.Opts{
		LogLevel: am.LogOps,
	})
	if err != nil {
		panic(err)
	}
	amhelp.MachDebugEnv(mach)

	// define a task func
	fn := func(num int, state, stateDone string) {
		ctx := mach.NewStateCtx(state)
		go func() {
			if ctx.Err() != nil {
				return // expired
			}
			amhelp.Wait(ctx, time.Second)
			if ctx.Err() != nil {
				return // expired
			}
			mach.Add1(stateDone, nil)
		}()
	}

	// create task states
	// 10 tasks, 3 running concurrently
	_, err = amhelp.FanOutIn(mach, "Task", 15, 3, fn)
	if err != nil {
		panic(err)
	}

	// start and wait
	mach.Add(am.S{"Start", "Task"}, nil)
	<-mach.When1("Ready", nil)

	// end
	println("done")

	// debug
	time.Sleep(time.Second)
}
