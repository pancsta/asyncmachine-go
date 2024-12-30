package testing

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/cli"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

var (
	WorkerRpcAddr       = "localhost:53480"
	WorkerTelemetryAddr = "localhost:53470"

	EnvAmDbgWorkerRpcAddr       = "AM_DBG_WORKER_RPC_ADDR"
	EnvAmDbgWorkerTelemetryAddr = "AM_DBG_WORKER_TELEMETRY_ADDR"
)

// NewRpcTest creates a new rpc server and client for testing, which exposes the
// passed worker as a remote one, and binds payloads to the optional consumer.
// TODO sync with rpc/rpc_test
func NewRpcTest(
	t *testing.T, ctx context.Context, worker *am.Machine,
	consumer *am.Machine,
) (*am.Machine, *rpc.Server, *rpc.Client) {
	utils.ConnInit.Lock()
	defer utils.ConnInit.Unlock()

	// read env
	amDbgAddr := os.Getenv(telemetry.EnvAmDbgAddr)
	stdout := os.Getenv(am.EnvAmDebug) == "2"
	logLvl := am.EnvLogLevel("")

	// bind to an open port
	listener := utils.RandListener("localhost")
	addr := listener.Addr().String()

	// worker init
	if worker == nil {
		t.Fatal("worker is nil")
	}

	// Server init
	s, err := rpc.NewServer(ctx, addr, t.Name(), worker, &rpc.ServerOpts{
		Parent: worker,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Listener.Store(&listener)
	amhelpt.MachDebug(t, s.Mach, amDbgAddr, logLvl, stdout)
	// let it settle
	time.Sleep(10 * time.Millisecond)

	// Client init
	c, err := rpc.NewClient(ctx, addr, t.Name(), worker.GetStruct(),
		worker.StateNames(), &rpc.ClientOpts{
			Consumer: consumer,
			Parent:   worker,
		})
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebug(t, c.Mach, amDbgAddr, logLvl, stdout)

	// tear down
	t.Cleanup(func() {
		<-s.Mach.WhenDisposed()
		<-c.Mach.WhenDisposed()
		// cool off am-dbg and free the ports
		if amDbgAddr != "" {
			time.Sleep(100 * time.Millisecond)
		}
	})

	// start with a timeout
	timeout := 3 * time.Second
	if amhelp.IsDebug() {
		timeout = 100 * time.Second
	}

	// server start
	s.Start()
	amhelpt.WaitForErrAll(t, "server RpcReady", ctx, s.Mach, timeout,
		s.Mach.When1(ssrpc.ServerStates.RpcReady, nil))
	amhelpt.WaitForErrAll(t, "client Ready", ctx, s.Mach, timeout,
		c.Mach.When1(ssrpc.ClientStates.Ready, nil))
	amhelpt.WaitForErrAll(t, "server Ready", ctx, s.Mach, timeout,
		s.Mach.When1(ssrpc.ServerStates.Ready, nil))

	return worker, s, c
}

// NewDbgWorker creates a new worker instance of the am-dbg.
func NewDbgWorker(
	realTty bool, opts debugger.Opts,
) (*debugger.Debugger, error) {

	// mock screen
	var screen tcell.Screen
	if !realTty {
		screen = tcell.NewSimulationScreen("utf8")
		screen.SetSize(100, 50)
		screen.Clear()
	}

	// fixtures
	if opts.ImportData == "" {
		dirPrefix := ""
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(wd, "tools/debugger/test") {
			dirPrefix = "../../../"
		}
		opts.ImportData = dirPrefix + "tools/debugger/testdata/am-dbg-sim.gob.br"
	}

	// file logging
	opts.DbgLogLevel = am.EnvLogLevel("")
	if opts.DbgLogLevel > 0 && os.Getenv(am.EnvAmLogFile) != "" {
		opts.DbgLogger = cli.GetLogger(&cli.Params{
			LogLevel: opts.DbgLogLevel,
			LogFile:  opts.ID + ".log",
		})
	}

	// misc opts
	if opts.Screen == nil {
		opts.Screen = screen
	}
	if opts.ID == "" {
		opts.ID = "rem-worker"
	}

	// used for testing live connections (eg TailMode)
	opts.SelectConnected = true

	// create a debugger
	dbg, err := debugger.New(context.TODO(), opts)
	if err != nil {
		return nil, err
	}
	amhelp.MachDebugEnv(dbg.Mach)

	// start at the same place
	res := dbg.Mach.Add1(ssdbg.Start, am.A{
		"Client.id":       "ps-2",
		"Client.cursorTx": 20,
	})
	if res == am.Canceled {
		return nil, errors.New("failed to start am-dbg")
	}
	<-dbg.Mach.When1(ssdbg.Ready, nil)
	<-dbg.Mach.WhenNot1(ssdbg.ScrollToTx, nil)

	dbg.Mach.Log("NewDbgWorker ready")

	return dbg, nil
}

func NewRpcClient(
	t *testing.T, ctx context.Context, addr string, stateStruct am.Struct,
	stateNames am.S, consumer *am.Machine,
) *rpc.Client {

	// Client init
	c, err := rpc.NewClient(ctx, addr, t.Name(), stateStruct, stateNames,
		&rpc.ClientOpts{Consumer: consumer})
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, c.Mach)

	// tear down
	t.Cleanup(func() {
		<-c.Mach.WhenDisposed()
		// cool off am-dbg and free the ports
		if os.Getenv(telemetry.EnvAmDbgAddr) != "" {
			time.Sleep(100 * time.Millisecond)
		}
	})

	// start with a timeout
	timeout := 3 * time.Second
	if os.Getenv("AM_DEBUG") != "" {
		timeout = 100 * time.Second
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// client ready
	c.Start()
	select {
	case <-c.Mach.WhenErr(readyCtx):
		err := c.Mach.Err()
		// timeout
		if readyCtx.Err() != nil {
			err = readyCtx.Err()
		}
		t.Fatal(err)
	case <-c.Mach.When1(ssrpc.ClientStates.Ready, readyCtx):
	}

	return c
}

// RpcShutdown shuts down the passed client and optionally a server.
func RpcShutdown(ctx context.Context, c *rpc.Client, s *rpc.Server) {
	// shut down
	c.Mach.Remove1(ssrpc.ClientStates.Start, nil)
	if s != nil {
		s.Mach.Remove1(ssrpc.ServerStates.Start, nil)
	}
	<-c.Mach.When1(ssrpc.ClientStates.Disconnected, ctx)
	// server disconnects synchronously
}

// RpcGet retrieves a value from the RPC server.
func RpcGet[G any](
	t *testing.T, c *rpc.Client, name server.GetField, defVal G,
) G {
	// TODO rewrite to SendPayload-WorkerPayload state flow
	// resp, err := c.Get(context.TODO(), name.Encode())
	// assert.NoError(t, err)
	//
	// // default values
	// if resp.Value == nil {
	// 	t.Fatal("nil response")
	// 	return defVal
	// }
	//
	// return resp.Value.(G)

	panic("not implemented")
}
