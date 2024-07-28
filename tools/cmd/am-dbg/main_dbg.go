package main

import (
	"context"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/cli"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/spf13/cobra"
	"os"
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
	ver := cli.GetVersion()
	if p.Version {
		println(ver)
		os.Exit(0)
	}

	// init the debugger
	dbg, err := debugger.New(ctx, debugger.Opts{
		DBGLogLevel:     p.LogLevel,
		DBGLogger:       cli.GetFileLogger(&p),
		ImportData:      p.ImportData,
		ServerAddr:      p.ServerURL,
		EnableMouse:     p.EnableMouse,
		SelectConnected: p.SelectConnected,
		CleanOnConnect:  p.CleanOnConnect,
		Version:         ver,
	})
	if err != nil {
		panic(err)
	}

	// rpc client
	if p.DebugAddr != "" {
		err := telemetry.TransitionsToDBG(dbg.Mach, p.DebugAddr)
		// TODO retries
		if err != nil {
			panic(err)
		}
	}

	// rpc server
	go server.StartRCP(dbg.Mach, p.ServerURL)

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
	// f, err := os.Create("memprofile")
	// if err != nil {
	// 	log.Fatal("could not create memory profile: ", err)
	// }
	// defer f.Close() // error handling omitted for example
	// runtime.GC()    // get up-to-date statistics
	// if err := pprof.WriteHeapProfile(f); err != nil {
	// 	log.Fatal("could not write memory profile: ", err)
	// }
}

func printStats(dbg *debugger.Debugger) {
	txs := 0
	for _, c := range dbg.Clients {
		txs += len(c.MsgTxs)
	}

	_, _ = dbg.P.Printf("Clients: %d\n", len(dbg.Clients))
	_, _ = dbg.P.Printf("Transitions: %d\n", txs)
}
