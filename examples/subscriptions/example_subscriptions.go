package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	// amhelp.EnableDebugging(false)
	// amhelp.SetEnvLogLevel(am.LogOps)
}

func main() {
	ctx := context.Background()

	// init state machines
	mach := am.New(ctx, am.Schema{
		"Foo":         {},
		"Bar":         {},
		"Healthcheck": {Multi: true},
	}, &am.Opts{LogLevel: am.LogOps, Id: "source"})
	amhelp.MachDebugEnv(mach)

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		go wg.Done()
		// wait until FileDownloaded becomes active
		<-mach.When1("Foo", nil)
		fmt.Println("1 - Foo activated")
	}()

	wg.Add(1)
	go func() {
		go wg.Done()
		// wait until FileDownloaded becomes inactive
		<-mach.WhenNot1("Bar", nil)
		fmt.Println("2 - Bar deactivated")
	}()

	wg.Add(1)
	go func() {
		go wg.Done()
		// wait for EventConnected to be activated with an arg ID=123
		<-mach.WhenArgs("Bar", am.A{"ID": 123}, nil)
		fmt.Println("3 - Bar activated with ID=123")
	}()

	wg.Add(1)
	go func() {
		go wg.Done()
		// wait for Foo to have a tick >= 1 and Bar tick >= 3
		<-mach.WhenTime(am.S{"Foo", "Bar"}, am.Time{1, 3}, nil)
		fmt.Println("4 - Foo tick >= 1 and Bar tick >= 3")
	}()

	wg.Add(1)
	go func() {
		go wg.Done()
		// wait for DownloadingFile to have a tick increased by 2 since now
		<-mach.WhenTicks("Foo", 2, nil)
		fmt.Println("5 - Foo tick increased by 2 since now")
	}()

	wg.Add(1)
	go func() {
		go wg.Done()
		// wait for an error
		<-mach.WhenErr(ctx)
		fmt.Println("6 - Error")
	}()

	wg.Wait()

	mach.Add1("Foo", nil)
	mach.Add1("Bar", nil)
	mach.Remove1("Bar", nil)
	mach.Remove1("Foo", nil)
	mach.Add1("Foo", nil)
	mach.Add1("Bar", am.A{"ID": 123})
	mach.AddErr(errors.New("err"), nil)

	// wait
	time.Sleep(time.Second)
}
