package main

import (
	"context"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/cli"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/spf13/cobra"
)

var (
	dataFile       = "assets/am-dbg-sim.gob.br"
	logLevel       = am.LogOps
	logFile        = "am-dbg-teaser.log"
	filterLogLevel = am.LogOps
	startupMachine = "sim-p1"
	startupTx      = 25
	initialView    = "matrix"
	playInterval   = 200 * time.Millisecond
	debugAddr      = ""
	// debugAddr = "localhost:9913"
)

func main() {
	rootCmd := cli.RootCmd(cliRun)
	err := rootCmd.Execute()
	if err != nil {
		panic(err)
	}
}

func cliRun(_ *cobra.Command, _ []string, p cli.Params) {

	// ctx
	ctx := context.Background()

	// overwrite params
	p.LogFile = logFile
	p.LogLevel = logLevel
	p.ImportData = dataFile
	p.DebugAddr = debugAddr

	// init the debugger
	dbg, err := debugger.New(ctx, debugger.Opts{
		Filters: &debugger.OptsFilters{
			LogLevel: filterLogLevel,
		},
		ImportData:  p.ImportData,
		DBGLogLevel: p.LogLevel,
		DBGLogger:   cli.GetFileLogger(&p),
		ServerAddr:  p.ServerURL,
		EnableMouse: p.EnableMouse,
		Version:     cli.GetVersion(),
	})
	if err != nil {
		panic(err)
	}

	// rpc client
	if p.DebugAddr != "" {
		err := telemetry.TransitionsToDBG(dbg.Mach, p.DebugAddr)
		// TODO retries
		if err != nil {
			panic(err)
		}
	}

	dbg.Start(startupMachine, startupTx, initialView)
	go runTeaser(dbg)

	select {
	case <-dbg.Mach.WhenDisposed():
	case <-dbg.Mach.WhenNot1(ss.Start, nil):
	}

	dbg.Dispose()
}

// demo:
// - p1, tx: 25, matrix view
// - tx: 45
// - tree matrix view
// - tx: 58
// - step to 59
// - tx: 59
// - highlight Connected
// - tx: 60
// - tree log view
// - step reverse to 59
// - tx: 59
// - log filter to changes
// - step reverse to 59
// - tx: 58
func runTeaser(dbg *debugger.Debugger) {
	mach := dbg.Mach
	// ctx := mach.Ctx
	<-mach.When1(ss.Ready, nil)
	time.Sleep(100 * time.Millisecond)

	// play
	goFwd(mach, 20)

	// goFwd(mach, 1)
	mach.Add1(ss.TreeMatrixView, nil)
	goFwd(mach, 13)

	// play steps
	goFwdSteps(mach, 8)

	// highlight Connected
	mach.Add1(ss.StateNameSelected, am.A{"selectedStateName": "Connected"})
	goFwdSteps(mach, 16) // 15 + empty step

	// go back with LogOps
	mach.Add1(ss.TreeLogView, nil)
	goBackSteps(mach, 16) // 15 + empty step

	// go back with LogChanges
	goBackSteps(mach, 9) // 8 + empty step

	// go back with LogChanges
	dbg.SetFilterLogLevel(am.LogChanges)
	goBackSteps(mach, 12)

	// end screen
	time.Sleep(2 * time.Second)
	mach.Dispose()
}

func goFwd(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		<-time.After(playInterval)
		mach.Add1(ss.Fwd, nil)
	}
	waitForRender()
}

func goFwdSteps(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		<-time.After(playInterval)
		mach.Add1(ss.FwdStep, nil)
	}
	waitForRender()
}

func goBackSteps(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		<-time.After(playInterval)
		mach.Add1(ss.BackStep, nil)
	}
	waitForRender()
}

func waitForRender() {
	time.Sleep(100 * time.Millisecond)
}
