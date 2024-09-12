//go:build test_worker
// +build test_worker

package test_remote

import (
	"context"
	"encoding/gob"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	amtest "github.com/pancsta/asyncmachine-go/internal/testing"
	ssTest "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amh "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

var workerRpcAddr = amtest.WorkerRpcAddr
var workerTelemetryAddr = amtest.WorkerTelemetryAddr

func init() {
	gob.Register(server.GetField(0))

	if addr := os.Getenv("AM_DBG_WORKER_RPC_ADDR"); addr != "" {
		workerRpcAddr = addr
	}
	if addr := os.Getenv("AM_DBG_WORKER_TELEMETRY_ADDR"); addr != "" {
		workerTelemetryAddr = addr
	}

	if os.Getenv("AM_TEST_DEBUG") != "" {
		// DEBUG
		enableTestDebugRemote()
	}
}

func TestUserFwdRemote(t *testing.T) {

	// init rpc
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := amtest.NewRpcClient(t, ctx, workerRpcAddr, ss.States, ss.Names)

	// fixtures
	cursorTx := 20
	amh.Add1AsyncBlock(ctx, c.Worker, ss.SwitchedClientTx, ss.SwitchingClientTx,
		am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

	// test
	res := amh.Add1Block(ctx, c.Worker, ss.UserFwd, nil)

	// assert
	assert.NotEqual(t, res, am.Canceled)
	assert.Equal(t, cursorTx+1, get(t, c, server.GetCursorTx, 0))

	c.Stop(ctx, true)
}

func TestUserFwd100Remote(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := amtest.NewRpcClient(t, ctx, workerRpcAddr, ss.States, ss.Names)
	mach := c.Worker

	// fixtures
	cursorTx := 20
	amh.Add1AsyncBlock(ctx, mach, ss.SwitchedClientTx, ss.SwitchingClientTx,
		am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

	// test
	// add ss.UserFwd 100 times in a series
	for i := 0; i < 100; i++ {
		res := amh.Add1Block(ctx, mach, ss.UserFwd, nil)
		if res == am.Canceled {
			t.Fatal(res)
		}
	}

	// assert
	assert.Equal(t, cursorTx+100, get(t, c, server.GetCursorTx, 0))

	c.Stop(ctx, true)
}

// TestTailModeRemote requires a worker started with --select-connected.
// TODO check select-connected via Opts getter
func TestTailModeRemote(t *testing.T) {

	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

	// init rpc
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := amtest.NewRpcClient(t, ctx, workerRpcAddr, ss.States, ss.Names)

	// create a listener for ConnectEvent
	whenConn := c.Worker.WhenTicks(ss.ConnectEvent, 1, ctx)

	// fixture machine
	mach := utils.NewRels(t, nil)
	amh.MachDebugT(t, mach, amDbgAddr, logLvl, true)

	// connect to the worker as a new telemetry client
	mach.SetLogLevel(am.LogOps)
	err := telemetry.TransitionsToDbg(mach, workerTelemetryAddr)
	if err != nil {
		t.Fatal(err)
	}

	<-whenConn
	whenDelivered := c.Worker.WhenTicks(ss.ClientMsg, 2, nil)
	c.Worker.Add1(ss.TailMode, nil)

	// generate fixture events
	mach.Add1(ssTest.C, nil)
	mach.Add1(ssTest.D, nil)
	mach.Add1(ssTest.D, nil)

	// wait for the msg
	<-whenDelivered

	// go back 2 txs
	c.Worker.Add1(ss.UserBack, nil)
	c.Worker.Add1(ss.UserBack, nil)

	c.Worker.Add1(ss.TailMode, nil)

	// assert.NotEqual(t, res, am.Canceled)
	assert.Equal(t, 4, get(t, c, server.GetMsgCount, 0))
	assert.Equal(t, 4, get(t, c, server.GetCursorTx, 0))
	// TODO assert tree clocks
	// TODO assert log highlight

	c.Stop(ctx, true)
}

func TestUserBackRemote(t *testing.T) {

	// init rpc
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := amtest.NewRpcClient(t, ctx, workerRpcAddr, ss.States, ss.Names)
	mach := c.Worker

	// fixtures
	cursorTx := 20
	amh.Add1AsyncBlock(ctx, mach, ss.SwitchedClientTx, ss.SwitchingClientTx,
		am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

	// test
	res := amh.Add1Block(ctx, mach, ss.UserBack, nil)

	// assert
	assert.NotEqual(t, res, am.Canceled)
	assert.Equal(t, cursorTx-1, get(t, c, server.GetCursorTx, 0))

	c.Stop(ctx, true)
}

func TestStepsResetAfterStateJumpRemote(t *testing.T) {

	// init rpc
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := amtest.NewRpcClient(t, ctx, workerRpcAddr, ss.States, ss.Names)
	mach := c.Worker

	// fixtures
	state := "PublishMessage"
	amh.Add1AsyncBlock(ctx, mach, ss.SwitchedClientTx, ss.SwitchingClientTx,
		am.A{"Client.id": "ps-2", "Client.cursorTx": 20})

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
	assert.Equal(t, 0, get(t, c, server.GetCursorStep, 0),
		"Steps timeline should reset")

	c.Stop(ctx, true)
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func TestUserFwdRemoteLoopback(t *testing.T) {
	t.Skip("Debug only")

	// init debugger
	d, err := amtest.NewTestWorker(false, debugger.Opts{ID: t.Name()})
	assert.NoError(t, err)

	// init rpc (full client-server setup)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, s, c := amtest.NewRpcTest(t, ctx, d.Mach, debugger.RpcGetter(d))

	cursor := get(t, c, server.GetCursorTx, 0)

	when := c.Worker.WhenTicks(ss.UserFwd, 1, nil)
	res := c.Worker.Add1(ss.UserFwd, nil)

	<-when
	assert.NotEqual(t, res, am.Canceled)
	assert.Equal(t, cursor+1, get(t, c, server.GetCursorTx, 0))

	amtest.RpcShutdown(ctx, c, s)
	d.Dispose()
}

// get is a helper for getting a value from the remote worker.
func get[G any](
	t *testing.T, c *arpc.Client, name server.GetField, defVal G,
) G {
	return amtest.RpcGet(t, c, name, defVal)
}

// enableTestDebugRemote sets env vars for debugging of remote workers.
func enableTestDebugRemote() {
	os.Setenv("AM_DEBUG", "1")
	os.Setenv("AM_DBG_ADDR", "localhost:9913")
	os.Setenv("AM_LOG", "2")
	os.Setenv("AM_LOG_FILE", "1")
	os.Setenv("AM_RPC_LOG_CLIENT", "1")
	os.Setenv("AM_RPC_LOG_SERVER", "1")
}
