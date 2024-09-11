// AM_DBG_WORKER_ADDR
// AM_DBG_ADDR
package main

import (
	"context"
	"flag"
	"os"
	"time"

	amtest "github.com/pancsta/asyncmachine-go/internal/testing"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssSrv "github.com/pancsta/asyncmachine-go/pkg/rpc/states/server"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ssDbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

	// test worker has its own flags
	serverAddr := flag.String("server-addr", "",
		"Addr of the debugger server (opt)")
	workerAddr := flag.String("worker-addr", amtest.WorkerRpcAddr,
		"Addr of the rpc worker")
	flag.Parse()

	// worker init
	os.Setenv("AM_LOG_FILE", "am-dbg-worker-remote.log")
	dbg, err := amtest.NewTestWorker(true, debugger.Opts{
		ServerAddr: *serverAddr,
	})
	if err != nil {
		panic(err)
	}

	// server init
	s, err := arpc.NewServer(ctx, *workerAddr, "worker", dbg.Mach,
		debugger.RpcGetter(dbg))
	if err != nil {
		panic(err)
	}
	utils.MachDebug(s.Mach, amDbgAddr, logLvl, false)

	// tear down
	defer func() {
		if amDbgAddr != "" {
			// cool off am-dbg and free the ports
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// am-dbg server (used for testing live connections)
	if *serverAddr != "" {
		go server.StartRpc(dbg.Mach, *serverAddr, nil)
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
	case <-s.Mach.When1(ssSrv.RpcReady, readyCtx):
	}

	// wait till the end
	select {
	case <-dbg.Mach.WhenDisposed():
		// user exit
	case <-dbg.Mach.WhenNot1(ssDbg.Start, nil):
		dbg.Dispose()
	}
}
