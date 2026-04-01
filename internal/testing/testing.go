package testing

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
	"github.com/pancsta/tcell-v2"
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
	t *testing.T, ctx context.Context, netSrc *am.Machine,
	consumer *am.Machine,
) (*am.Machine, *rpc.Server, *rpc.Client) {
	//

	utils.ConnInit.Lock()
	defer utils.ConnInit.Unlock()

	// read env
	amDbgAddr := os.Getenv(dbg.EnvAmDbgAddr)
	stdout := os.Getenv(am.EnvAmDebug) == "2"
	logLvl := am.EnvLogLevel("")

	// bind to an open port
	listener := utils.RandListener("localhost")
	addr := listener.Addr().String()

	// netSrc init
	if netSrc == nil {
		t.Fatal("worker is nil")
	}

	// Server init
	s, err := rpc.NewServer(ctx, addr, t.Name(), netSrc, &rpc.ServerOpts{
		Parent: netSrc,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Listener.Store(&listener)
	amhelpt.MachDebug(t, s.Mach, amDbgAddr, logLvl, stdout)
	// let it settle
	time.Sleep(10 * time.Millisecond)

	// Client init
	c, err := rpc.NewClient(ctx, addr, t.Name(), netSrc.Schema(), &rpc.ClientOpts{
		Consumer: consumer,
		Parent:   netSrc,
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
	s.Start(nil)
	amhelpt.WaitForErrAll(t, "server RpcReady", ctx, s.Mach, timeout,
		s.Mach.When1(ssrpc.ServerStates.RpcReady, nil))
	amhelpt.WaitForErrAll(t, "client Ready", ctx, s.Mach, timeout,
		c.Mach.When1(ssrpc.ClientStates.Ready, nil))
	amhelpt.WaitForErrAll(t, "server Ready", ctx, s.Mach, timeout,
		s.Mach.When1(ssrpc.ServerStates.Ready, nil))

	return netSrc, s, c
}

// NewDbgWorker creates a new worker instance of the am-dbg.
func NewDbgWorker(
	realTty bool, p types.Params,
) (*debugger.Debugger, error) {

	// mock screen
	if !realTty && p.Screen == nil {
		screen := tcell.NewSimulationScreen("utf8")
		screen.SetSize(100, 50)
		_ = screen.Init()
		screen.Clear()
		p.Screen = screen
	}

	// fixtures
	if p.ImportData == "" {
		dirPrefix := ""
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(wd, "tools/debugger/test") {
			dirPrefix = "../../../"
		}
		p.ImportData = dirPrefix + "tools/debugger/testdata/am-dbg-sim.gob.br"
	}

	// file logging
	p.LogLevel = am.EnvLogLevel("")
	if p.LogLevel > 0 && os.Getenv(amhelp.EnvAmLogFile) != "" {
		p.DbgLogger = types.GetLogger(&p, "")
	}

	// misc opts
	if p.Id == "" {
		p.Id = "rem-worker"
	}

	// used for testing live connections (eg TailMode)
	p.SelectConnected = true
	p.MachUrl = "mach://ps-2/a59d994dd6efb726a1cd3ef1b7f384fd"

	// create a debugger
	d, err := debugger.New(context.TODO(), p)
	if err != nil {
		return nil, err
	}
	_ = amhelp.MachDebugEnv(d.Mach)

	// start at the same place
	res := d.Mach.Add1(ssdbg.Start, nil)
	if res == am.Canceled {
		return nil, errors.New("failed to start am-d")
	}
	<-d.Mach.When1(ssdbg.Ready, nil)
	<-d.Mach.WhenNot1(ssdbg.ScrollToTx, nil)

	d.Mach.Log("NewDbgWorker ready")

	return d, nil
}

func NewRpcClient(
	t *testing.T, ctx context.Context, addr string, netSrcSchema am.Schema,
	consumer *am.Machine,
) *rpc.Client {

	// Client init
	c, err := rpc.NewClient(ctx, addr, t.Name(), netSrcSchema,
		&rpc.ClientOpts{Consumer: consumer})
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, c.Mach)

	// tear down
	t.Cleanup(func() {
		<-c.Mach.WhenDisposed()
		// cool off am-dbg and free the ports
		if os.Getenv(dbg.EnvAmDbgAddr) != "" {
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
	c.Start(nil)
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
