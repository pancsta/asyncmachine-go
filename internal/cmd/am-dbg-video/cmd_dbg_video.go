package main

import (
	"context"
	"math"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	"github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/cli"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/spf13/cobra"
)

var (
	dataFile       = "assets/asyncmachine-go/am-dbg-exports/pubsub-sim.gob.br"
	logLevel       = am.LogOps
	filterLogLevel = am.LogChanges
	startupMachine = "sim-p1"
	startupTx      = 27
	initialView    = "matrix"
	playInterval   = 200 * time.Millisecond
	debugAddr      = ""
	// debugAddr  = "localhost:6831"
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
		am.StateException,
	}
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
	p.LogLevel = logLevel
	p.ImportData = dataFile
	p.DebugAddr = debugAddr

	// init the debugger
	dbg, err := debugger.New(ctx, debugger.Opts{
		Filters: &debugger.OptsFilters{
			LogLevel: filterLogLevel,
		},
		ImportData:      p.ImportData,
		DbgLogLevel:     p.LogLevel,
		DbgLogger:       cli.GetLogger(&p, ""),
		AddrRpc:         p.ListenAddr,
		EnableMouse:     p.EnableMouse,
		EnableClipboard: p.EnableClipboard,
		Version:         utils.GetVersion(),
		Id:              "video",
		MaxMemMb:        1000,
	})
	if err != nil {
		panic(err)
	}
	helpers.MachDebug(dbg.Mach, debugAddr, logLevel, false,
		&am.SemConfig{Full: true})

	dbg.Start(startupMachine, startupTx, initialView, "")
	go render(dbg)

	select {
	case <-dbg.Mach.WhenDisposed():
	case <-dbg.Mach.WhenNot1(ss.Start, nil):
	}

	dbg.Dispose()
}

func render(dbg *debugger.Debugger) {
	// SkipStart() // TODO

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
	mach.Add1(ss.TimelineStepsFocused, nil)
	tx := dbg.NextTx()
	goFwdSteps(mach, len(tx.Steps)+1)

	// play steps with moving selection
	tx = dbg.NextTx()
	ii := -1
	for i := 0; i < len(tx.Steps); i++ {
		ii = (ii + 1) % len(stateNames)
		mach.Add1(ss.StateNameSelected, am.A{"state": stateNames[ii]})

		goFwdSteps(mach, 1)
	}

	// keep the highlight 1 frame longer
	ii++

	// play steps back with moving selection
	for i := 0; i < len(tx.Steps); i++ {
		ii = (ii - 1) % len(stateNames)
		ii = int(math.Max(0, float64(ii)))
		if ii > -1 {
			mach.Add1(ss.StateNameSelected, am.A{"state": stateNames[ii]})
		} else {
			mach.Remove1(ss.StateNameSelected, nil)
		}

		goBackSteps(mach, 1)
	}

	mach.Remove1(ss.StateNameSelected, nil)
	mach.Add(am.S{ss.TreeMatrixView, ss.MatrixRain}, nil)
	mach.Add1(ss.ClientListFocused, nil)
	goBack(mach, 14)

	// go back with clean UI
	dbg.SetFilterLogLevel(am.LogOps)
	mach.Add1(ss.TreeLogView, nil)
	goBack(mach, 14)

	SkipEnd() // TODO
	mach.Add1(ss.LogTimestamps, nil)
	dbg.SetFilterLogLevel(am.LogChanges)
	// TODO via state handlers, pass focused filter
	mach.Add1(ss.Toolbar1Focused, am.A{"filter": debugger.ToolLogTimestamps})
	// dbg.HProcessFilterChange(context.TODO(), false)
	goBack(mach, 1)

	// end screen
	time.Sleep(3 * time.Second)
	mach.Dispose()
}

func SkipEnd() {
	playInterval = 200 * time.Millisecond
}

func SkipStart() {
	playInterval = 10 * time.Millisecond
}

func goFwd(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		mach.Add1(ss.Fwd, nil)
		time.Sleep(playInterval)
	}
}

func goBack(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		mach.Add1(ss.Back, nil)
		time.Sleep(playInterval)
	}
}

func goFwdSteps(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		mach.Add1(ss.FwdStep, nil)
		time.Sleep(playInterval)
	}
}

func goBackSteps(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		mach.Add1(ss.BackStep, nil)
		time.Sleep(playInterval)
	}
}
