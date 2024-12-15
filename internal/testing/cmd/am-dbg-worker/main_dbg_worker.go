// AM_DBG_WORKER_ADDR
// AM_DBG_ADDR
package main

import (
	"context"
	"flag"
	"os"
	"time"

	amtest "github.com/pancsta/asyncmachine-go/internal/testing"
	"github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// read env
	amDbgAddr := os.Getenv(telemetry.EnvAmDbgAddr)
	logLvl := am.EnvLogLevel("")

	// test worker has its own flags
	serverAddr := flag.String("server-addr", "",
		"Addr of the debugger server (opt)")
	workerAddr := flag.String("worker-addr", amtest.WorkerRpcAddr,
		"Addr of the rpc worker")
	flag.Parse()

	// worker init
	os.Setenv(am.EnvAmLogFile, "1")
	dbg, err := amtest.NewDbgWorker(true, debugger.Opts{
		ServerAddr: *serverAddr,
	})
	if err != nil {
		panic(err)
	}

	// server init
	s, err := rpc.NewServer(ctx, *workerAddr, "worker", dbg.Mach, nil)
	if err != nil {
		panic(err)
	}
	helpers.MachDebug(s.Mach, amDbgAddr, logLvl, false)

	// tear down
	defer func() {
		if amDbgAddr != "" {
			// cool off am-dbg and free the ports
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// am-dbg server (used for testing live connections)
	if *serverAddr != "" {
		go server.StartRpc(dbg.Mach, *serverAddr, nil, nil)
	}

	// start with a timeout
	readyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// server start
	s.Start()
	select {
	case <-s.Mach.WhenErr(readyCtx):
		err := s.Mach.Err()
		if readyCtx.Err() != nil {
			err = readyCtx.Err()
		}
		panic(err)
	case <-s.Mach.When1(ssrpc.ServerStates.RpcReady, readyCtx):
	}

	// wait till the end
	select {
	case <-dbg.Mach.WhenDisposed():
		// user exit
	case <-dbg.Mach.WhenNot1(ssdbg.Start, nil):
		dbg.Dispose()
	}
}
