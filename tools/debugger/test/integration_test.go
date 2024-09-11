package test_local

import (
	"context"
	"encoding/gob"
	"os"
	"testing"

	ssTest "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	"github.com/soheilhy/cmux"
	"github.com/stretchr/testify/assert"

	amtest "github.com/pancsta/asyncmachine-go/internal/testing"
	amh "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// worker is a local worker with imported data, which listens for new
// telemetry connections
var worker *debugger.Debugger

// make sure these ports aren't used in other tests
var workerAddr = "localhost:" + utils.RandPort(52001, 53000)

func init() {
	var err error
	gob.Register(server.GetField(0))

	// DEBUG
	if os.Getenv("AM_TEST_DEBUG") != "" {
		enableTestDebug()
		// enable file logging
		os.Setenv("AM_LOG_FILE", "1")
	}

	// worker
	worker, err = amtest.NewTestWorker(false, debugger.Opts{
		ID: "loc-worker"})
	if err != nil {
		panic(err)
	}

	// init am-dbg telemetry server
	muxCh := make(chan cmux.CMux, 1)
	defer close(muxCh)
	go server.StartRpc(worker.Mach, workerAddr, muxCh)
	// wait for mux
	<-muxCh
}

func TestUserFwd(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mach := worker.Mach

	// fixtures
	cursorTx := 20
	amh.Add1AsyncBlock(ctx, mach, ss.SwitchedClientTx, ss.SwitchingClientTx,
		am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

	// test
	res := amh.Add1Block(ctx, mach, ss.UserFwd, nil)

	// assert
	assert.NotEqual(t, res, am.Canceled)
	assert.Equal(t, cursorTx+1, worker.C.CursorTx)
}

func TestUserFwd100(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mach := worker.Mach

	// fixtures
	cursorTx := 20
	amh.Add1AsyncBlock(ctx, mach, ss.SwitchedClientTx, ss.SwitchingClientTx,
		am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

	// test
	// add ss.UserFwd 100 times in a series
	for i := 0; i < 100; i++ {
		// wait for Fwd to de-activate each time (Fwd is implied by UserFwd)
		when := mach.WhenTicks(ss.Fwd, 2, ctx)
		res := mach.Add1(ss.UserFwd, nil)
		if res == am.Canceled {
			t.Fatal(res)
		}
		<-when
	}

	// assert
	assert.Equal(t, cursorTx+100, worker.C.CursorTx)
}

func TestTailMode(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// create a listener for ConnectEvent
	whenConn := worker.Mach.WhenTicks(ss.ConnectEvent, 1, ctx)

	// fixture machine
	mach := utils.NewRels(t, nil)
	mach.SetLoggerEmpty(am.LogOps)
	err := telemetry.TransitionsToDbg(mach, workerAddr)
	if err != nil {
		t.Fatal(err)
	}

	// wait for the fixture machine to connect
	<-whenConn
	whenDelivered := worker.Mach.WhenTicks(ss.ClientMsg, 2, ctx)

	// generate fixture events
	// mach.SetLoggerSimple(t.Logf, am.LogOps)
	mach.Add1(ssTest.C, nil)
	mach.Add1(ssTest.D, nil)
	mach.Add1(ssTest.A, nil)

	// wait for the msg
	for {
		<-whenDelivered
		// because of receive batching, sometimes txs come in 1, sometimes in 2
		// msgs
		c := len(worker.C.MsgTxs)
		if c >= 3 {
			break
		}

		// wait more
		t.Logf("waiting for more txs (%d)", c)
		whenDelivered = worker.Mach.WhenTicks(ss.ClientMsg, 2, ctx)
	}

	// go back 2 txs
	worker.Mach.Add1(ss.UserBack, nil)
	worker.Mach.Add1(ss.UserBack, nil)
	// switch to tail mode
	worker.Mach.Add1(ss.TailMode, nil)

	// assert.NotEqual(t, res, am.Canceled)
	assert.Equal(t, 4, len(worker.C.MsgTxs), "tx count")
	assert.Equal(t, 4, worker.C.CursorTx, "cursorTx")
	// TODO assert tree clocks
	// TODO assert log highlight
}

func TestUserBack(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mach := worker.Mach

	// fixtures
	cursorTx := 20
	amh.Add1AsyncBlock(ctx, mach, ss.SwitchedClientTx, ss.SwitchingClientTx,
		am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

	// test
	res := amh.Add1Block(ctx, mach, ss.UserBack, nil)

	// assert
	assert.NotEqual(t, res, am.Canceled)
	assert.Equal(t, cursorTx-1, worker.C.CursorTx)
}

func TestStepsResetAfterStateJump(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mach := worker.Mach

	// fixtures
	state := "PublishMessage"
	cursorTx := 20
	amh.Add1AsyncBlock(ctx, mach, ss.SwitchedClientTx, ss.SwitchingClientTx,
		am.A{"Client.id": "ps-2", "Client.cursorTx": cursorTx})

	// test
	amh.Add1Block(ctx, mach, ss.StateNameSelected, am.A{"state": state})
	amh.Add1Block(ctx, mach, ss.UserFwdStep, nil)
	amh.Add1Block(ctx, mach, ss.UserFwdStep, nil)

	// trigger a state jump and wait for the next scroll
	amh.Add1AsyncBlock(ctx, mach, ss.ScrollToTx, ss.ScrollToMutTx, am.A{
		"state": state,
		"fwd":   true,
	})

	// assert
	assert.Equal(t, 0, worker.C.CursorStep, "Steps timeline should reset")
	// TODO assert not playing
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// enableTestDebug sets the env vars for debugging local workers, should be run
// in init().
func enableTestDebug() {
	os.Setenv("AM_DEBUG", "1")
	os.Setenv("AM_DBG_ADDR", "localhost:9913")
	os.Setenv("AM_LOG", "2")
	os.Setenv("AM_DETECT_EVAL", "1")
}
