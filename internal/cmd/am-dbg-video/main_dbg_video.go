package main

import (
	"context"
	"time"

	"github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/cli"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/spf13/cobra"
)

var (
	dataFile       = "assets/am-dbg-sim.gob.br"
	logLevel       = am.LogOps
	logFile        = "am-dbg-video.log"
	filterLogLevel = am.LogOps
	startupMachine = "sim-p1"
	startupTx      = 27
	initialView    = "matrix"
	playInterval   = 200 * time.Millisecond
	debugAddr      = ""
	// TODO get from mach
	stateNames = am.S{
		"Start",
		"IsDHT",
		"Ready",
		"IdentityReady",
		"GenIdentity",
		"BootstrapsConnected",
		"EventHostConnected",
		"Connected",
		"Connecting",
		"Disconnecting",
		"JoiningTopic",
		"TopicJoined",
		"LeavingTopic",
		"TopicLeft",
		"SendingMsgs",
		"MsgsSent",
		"FwdToSim",
		am.Exception,
	}

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
		DbgLogLevel: p.LogLevel,
		DbgLogger:   cli.GetLogger(&p),
		ServerAddr:  p.ServerAddr,
		EnableMouse: p.EnableMouse,
		Version:     cli.GetVersion(),
	})
	if err != nil {
		panic(err)
	}
	helpers.MachDebug(dbg.Mach, debugAddr, logLevel, false)

	dbg.Start(startupMachine, startupTx, initialView)
	go render(dbg)

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
func render(dbg *debugger.Debugger) {
	// playInterval = 1 * time.Millisecond // TODO
	mach := dbg.Mach
	// ctx := mach.Ctx
	<-mach.When1(ss.Ready, nil)
	time.Sleep(100 * time.Millisecond)

	// play
	goFwd(mach, 20)

	// goFwd(mach, 1)
	mach.Add1(ss.TreeMatrixView, nil)
	goFwd(mach, 14)

	// play steps
	tx := dbg.NextTx()
	goFwdSteps(mach, len(tx.Steps)+1)

	// play steps with moving selection
	tx = dbg.NextTx()
	ii := 0
	for i := 0; i < len(tx.Steps)+1; i++ {
		mach.Add1(ss.StateNameSelected, am.A{"state": stateNames[ii]})
		goFwdSteps(mach, 1)
		ii = (ii + 1) % len(stateNames)
	}

	goBackSteps(mach, len(tx.Steps)+1)

	mach.Add1(ss.TreeLogView, nil)
	goBack(mach, 6)
	mach.Remove1(ss.StateNameSelected, nil)
	mach.Add(am.S{ss.TreeMatrixView, ss.MatrixRain}, nil)
	goBack(mach, 4)
	// go back with clean UI
	dbg.SetFilterLogLevel(am.LogChanges)
	mach.Add1(ss.TreeLogView, nil)
	goBack(mach, 7)

	goBack(mach, 1)

	// end screen
	time.Sleep(3 * time.Second)
	mach.Dispose()
}

func goFwd(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		<-time.After(playInterval)
		mach.Add1(ss.Fwd, nil)
	}
}

func goBack(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		<-time.After(playInterval)
		mach.Add1(ss.Back, nil)
	}
}

func goFwdSteps(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		<-time.After(playInterval)
		mach.Add1(ss.FwdStep, nil)
	}
}

func goBackSteps(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		<-time.After(playInterval)
		mach.Add1(ss.BackStep, nil)
	}
}
