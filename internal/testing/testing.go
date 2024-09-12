package testing

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/asyncmachine-go/pkg/helpers"
	"github.com/stretchr/testify/assert"

	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssCli "github.com/pancsta/asyncmachine-go/pkg/rpc/states/client"
	ssSrv "github.com/pancsta/asyncmachine-go/pkg/rpc/states/server"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/cli"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ssDbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

var (
	WorkerRpcAddr       = "localhost:53480"
	WorkerTelemetryAddr = "localhost:53470"
)

// NewRpcTest creates a new rpc server and client for testing, which expose the
// passed worker instance, along with its RPC getter.
func NewRpcTest(
	t *testing.T, ctx context.Context, worker *am.Machine,
	getter func(string) any,
) (*am.Machine, *arpc.Server, *arpc.Client) {
	utils.ConnInit.Lock()
	defer utils.ConnInit.Unlock()

	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

	// bind to an open port
	listener := utils.RandListener("localhost")
	addr := listener.Addr().String()

	// worker init
	if worker == nil {
		t.Fatal("worker is nil")
	}

	// Server init
	s, err := arpc.NewServer(ctx, addr, t.Name(), worker, getter)
	if err != nil {
		t.Fatal(err)
	}
	s.Listener = listener
	helpers.MachDebugT(t, s.Mach, amDbgAddr, logLvl, false)
	// let it settle
	time.Sleep(10 * time.Millisecond)

	// Client init
	c, err := arpc.NewClient(ctx, addr, t.Name(), worker.GetStruct(),
		worker.StateNames())
	if err != nil {
		t.Fatal(err)
	}
	helpers.MachDebugT(t, c.Mach, amDbgAddr, logLvl, false)

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
	if os.Getenv("AM_DEBUG") != "" {
		timeout = 100 * time.Second
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// server start
	s.Start()
	select {
	case <-s.Mach.WhenErr(readyCtx):
		err := s.Mach.Err()
		// timeout
		if readyCtx.Err() != nil {
			err = readyCtx.Err()
		}
		t.Fatal(err)
	case <-s.Mach.When1(ssSrv.RpcReady, readyCtx):
	}

	// client start & ready
	c.Start()
	select {
	case <-c.Mach.WhenErr(readyCtx):
		err := c.Mach.Err()
		// timeout
		if readyCtx.Err() != nil {
			err = readyCtx.Err()
		}
		t.Fatal(err)
	case <-c.Mach.When1(ssCli.Ready, readyCtx):
	}

	// server ready
	select {
	case <-s.Mach.WhenErr(readyCtx):
		err := s.Mach.Err()
		// timeout
		if readyCtx.Err() != nil {
			err = readyCtx.Err()
		}
		t.Fatal(err)
	case <-s.Mach.When1(ssSrv.Ready, readyCtx):
	}

	return worker, s, c
}

// NewTestWorker creates a new worker instance of the am-dbg.
func NewTestWorker(
	realTty bool, opts debugger.Opts,
) (*debugger.Debugger, error) {

	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

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
	if opts.DbgLogLevel > 0 && os.Getenv("AM_LOG_FILE") != "" {
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
	helpers.MachDebug(dbg.Mach, amDbgAddr, logLvl, true)

	// start at the same place
	res := dbg.Mach.Add1(ssDbg.Start, am.A{
		"Client.id":       "ps-2",
		"Client.cursorTx": 20,
	})
	if res == am.Canceled {
		return nil, errors.New("failed to start am-dbg")
	}
	<-dbg.Mach.When1(ssDbg.Ready, nil)
	<-dbg.Mach.WhenNot1(ssDbg.ScrollToTx, nil)

	dbg.Mach.Log("NewTestWorker ready")

	return dbg, nil
}

func NewRpcClient(
	t *testing.T, ctx context.Context, addr string, stateStruct am.Struct,
	stateNames am.S,
) *arpc.Client {

	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

	// Client init
	c, err := arpc.NewClient(ctx, addr, t.Name(), stateStruct, stateNames)
	if err != nil {
		t.Fatal(err)
	}
	helpers.MachDebugT(t, c.Mach, amDbgAddr, logLvl, true)

	// tear down
	t.Cleanup(func() {
		<-c.Mach.WhenDisposed()
		// cool off am-dbg and free the ports
		if amDbgAddr != "" {
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
	case <-c.Mach.When1(ssCli.Ready, readyCtx):
	}

	return c
}

// RpcShutdown shuts down the passed client and optionally a server.
func RpcShutdown(ctx context.Context, c *arpc.Client, s *arpc.Server) {
	// shut down
	c.Mach.Remove1(ssCli.Start, nil)
	if s != nil {
		s.Mach.Remove1(ssSrv.Start, nil)
	}
	<-c.Mach.When1(ssCli.Disconnected, ctx)
	// server disconnects synchronously
}

// RpcGet retrieves a value from the RPC server.
func RpcGet[G any](
	t *testing.T, c *arpc.Client, name server.GetField, defVal G,
) G {
	// TODO make it universal (not dbg server only)
	resp, err := c.Get(context.TODO(), name.Encode())
	assert.NoError(t, err)

	// default values
	if resp.Value == nil {
		t.Fatal("nil response")
		return defVal
	}

	return resp.Value.(G)
}
