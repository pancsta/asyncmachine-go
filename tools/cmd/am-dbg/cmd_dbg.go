// am-dbg is a lightweight, multi-client debugger for asyncmachine-go.
package main

import (
	"context"
	"os"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	"github.com/spf13/cobra"

	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/cli"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func main() {
	rootCmd := cli.RootCmd(cliRun)
	err := rootCmd.Execute()
	if err != nil {
		panic(err)
	}
}

// TODO error msgs
func cliRun(_ *cobra.Command, _ []string, p cli.Params) {
	ctx := context.Background()

	// print the version
	ver := utils.GetVersion()
	if p.Version {
		println(ver)
		os.Exit(0)
	}

	// logger and profiler
	logger := cli.GetLogger(&p)
	cli.StartCpuProfileSrv(ctx, logger, &p)
	stopProfile := cli.StartCpuProfile(logger, &p)
	if stopProfile != nil {
		defer stopProfile()
	}

	// init the debugger
	dbg, err := debugger.New(ctx, debugger.Opts{
		DbgLogLevel:     p.LogLevel,
		DbgLogger:       logger,
		ImportData:      p.ImportData,
		ServerAddr:      p.ServerAddr,
		EnableMouse:     p.EnableMouse,
		SelectConnected: p.SelectConnected,
		ShowReader:      p.Reader,
		CleanOnConnect:  p.CleanOnConnect,
		MaxMemMb:        p.MaxMemMb,
		Log2Ttl:         p.Log2Ttl,
		Version:         ver,
	})
	if err != nil {
		panic(err)
	}

	// rpc client
	if p.DebugAddr != "" {
		err := telemetry.TransitionsToDbg(dbg.Mach, p.DebugAddr)
		// TODO retries
		if err != nil {
			panic(err)
		}
	}

	// rpc server
	if p.ServerAddr != "-1" {
		go server.StartRpc(dbg.Mach, p.ServerAddr, nil, p.FwdData)
	}

	// start and wait till the end
	dbg.Start(p.StartupMachine, p.StartupTx, p.StartupView)

	select {
	case <-dbg.Mach.WhenDisposed():
	case <-dbg.Mach.WhenNot1(ss.Start, nil):
	}

	// show footer stats
	printStats(dbg)

	dbg.Dispose()

	// pprof memory profile
	cli.HandleProfMem(logger, &p)
}

func printStats(dbg *debugger.Debugger) {
	txs := 0
	for _, c := range dbg.Clients {
		txs += len(c.MsgTxs)
	}

	_, _ = dbg.P.Printf("Clients: %d\n", len(dbg.Clients))
	_, _ = dbg.P.Printf("Transitions: %d\n", txs)
	_, _ = dbg.P.Printf("Memory: %dmb\n", debugger.AllocMem()/1024/1024)
}
