package main

import (
	"context"
	"encoding/gob"
	"log"
	"os"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

var (
	gobFile        = "assets/am-dbg-sim.gob.bz2"
	logLevel       = am.LogOps
	logFile        = "am-dbg-demo.log"
	startupMachine = "sim-p1"
	startupTx      = 25
	initialView    = "matrix"
	playInterval   = 200 * time.Millisecond
)

func main() {
	var err error

	// file logging
	if logFile != "" {
		_ = os.Remove(logFile)
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			panic(err)
		}
		defer file.Close()
		log.SetOutput(file)
	}

	// init the debugger
	// TODO extract to tools/debugger
	dbg := &debugger.Debugger{
		Clients: make(map[string]*debugger.Client),
	}
	gob.Register(debugger.Exportable{})
	gob.Register(am.Relation(0))

	// import data
	dbg.ImportData(gobFile)

	// init machine
	dbg.Mach, err = initMachine(dbg, logLevel)
	if err != nil {
		panic(err)
	}

	// start and wait till the end
	dbg.Mach.Add1(ss.Start, am.A{
		"Client.id":       startupMachine,
		"Client.cursorTx": startupTx,
		"dbgView":         initialView,
	})

	runDemo(dbg)

	dbg.Mach.Remove1(ss.Start, nil)
}

// demo:
// 1 p1, tx: 25, matrix view
// 2 tx: 45
// 3 tree matrix view
// 4 tx: 56
// 5 step timeline
// 6 tx: 58
// 7 tree log view
// 8 reverse steps
// 9 tx: 56
func runDemo(dbg *debugger.Debugger) {
	mach := dbg.Mach
	<-mach.When1(ss.Ready, nil)
	time.Sleep(100 * time.Millisecond)

	// play txes
	goFwd(mach, 20)
	mach.Add1(ss.TreeMatrixView, nil)
	waitForRender()
	goFwd(mach, 11)

	// play steps
	goFwdSteps(mach, 8)
	// highlight Connected
	mach.Add1(ss.StateNameSelected, am.A{"selectedStateName": "Connected"})
	waitForRender()

	goFwdSteps(mach, 15)
	waitForRender()

	// go back
	mach.Add1(ss.TreeLogView, nil)
	goBackSteps(mach, 23)
	waitForRender()

	// end screen
	time.Sleep(2 * time.Second)
}

func goFwd(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		time.Sleep(playInterval)
		mach.Add1(ss.Fwd, nil)
	}
}

func goFwdSteps(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		time.Sleep(playInterval)
		mach.Add1(ss.FwdStep, nil)
	}
}

func goBackSteps(mach *am.Machine, amount int) {
	for i := 0; i < amount; i++ {
		time.Sleep(playInterval)
		mach.Add1(ss.BackStep, nil)
	}
}

func waitForRender() {
	time.Sleep(100 * time.Millisecond)
}

// TODO extract to tools/debugger
func initMachine(
	h *debugger.Debugger, logLevel am.LogLevel) (*am.Machine, error) {
	ctx := context.Background()

	// TODO use NewCommon
	mach := am.New(ctx, ss.States, &am.Opts{
		HandlerTimeout:       time.Hour,
		DontPanicToException: true,
		DontLogID:            true,
	})
	mach.ID = "am-dbg-" + mach.ID

	err := mach.VerifyStates(ss.Names)
	if err != nil {
		return nil, err
	}

	mach.SetTestLogger(log.Printf, logLevel)
	mach.SetLogArgs(am.NewArgsMapper([]string{"Machine.id", "conn_id"}, 20))

	err = mach.BindHandlers(h)
	if err != nil {
		return nil, err
	}

	return mach, nil
}
