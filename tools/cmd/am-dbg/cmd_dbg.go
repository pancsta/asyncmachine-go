// am-dbg is a lightweight, multi-client debugger for asyncmachine-go.
package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

func main() {
	rootCmd := types.RootCmd(cliRun)
	err := rootCmd.Execute()
	if err != nil {
		panic(err)
	}
}

// TODO error msgs
func cliRun(c *cobra.Command, _ []string, p types.Params) {
	ctx := context.Background()

	// print the version
	ver := utils.GetVersion()
	if p.Version {
		println(ver)
		os.Exit(0)
	}

	// logger and profiler
	logger := types.GetLogger(&p, p.OutputDir)
	types.StartCpuProfileSrv(ctx, logger, &p)
	stopProfile := types.StartCpuProfile(logger, &p)
	if stopProfile != nil {
		defer stopProfile()
	}

	httpAddr := ""
	if p.ListenAddr != "-1" {
		addr := strings.Split(p.ListenAddr, ":")
		httpPort, _ := strconv.Atoi(addr[1])
		httpPort += 1
		httpAddr = addr[0] + ":" + strconv.Itoa(httpPort)
	}

	// init the debugger
	dbg, err := debugger.New(ctx, debugger.Opts{
		Id:             p.Id,
		DbgLogLevel:    p.LogLevel,
		DbgLogger:      logger,
		ImportData:     p.ImportData,
		OutputClients:  p.OutputClients,
		OutputDiagrams: p.OutputDiagrams,
		Timelines:      p.Timelines,
		// ...:           p.FilterLogLevel,
		OutputDir:       p.OutputDir,
		AddrRpc:         p.ListenAddr,
		AddrHttp:        httpAddr,
		EnableMouse:     p.EnableMouse,
		EnableClipboard: p.EnableClipboard,
		MachUrl:         p.MachUrl,
		SelectConnected: p.SelectConnected,
		ShowReader:      p.Reader,
		CleanOnConnect:  p.CleanOnConnect,
		MaxMemMb:        p.MaxMemMb,
		Log2Ttl:         p.LogOpsTtl,
		ViewNarrow:      p.ViewNarrow,
		ViewRain:        p.ViewRain,
		TailMode:        p.TailMode && p.StartupTx == 0,
		Version:         ver,
		Filters: &debugger.OptsFilters{
			SkipOutGroup: p.FilterGroup,
			LogLevel:     p.FilterLogLevel,
		},
	})
	if err != nil {
		panic(err)
	}

	// rpc client
	if p.DebugAddr != "" {
		amhelp.MachDebug(dbg.Mach, p.DebugAddr, p.LogLevel, false,
			amhelp.SemConfig(true))

		// TODO --otel flag
		// os.Setenv(telemetry.EnvService, "dbg")
		// os.Setenv(telemetry.EnvOtelTrace, "1")
		// os.Setenv(telemetry.EnvOtelTraceTxs, "1")
		// err = telemetry.MachBindOtelEnv(dbg.Mach)
		// if err != nil {
		// 	panic(err)
		// }
	}

	// rpc server
	if p.ListenAddr != "-1" {
		go server.StartRpc(dbg.Mach, p.ListenAddr, nil, p.FwdData,
			p.UiDiagrams)
	}

	// start and wait till the end
	dbg.Start(p.StartupMachine, p.StartupTx, p.StartupView, p.StartupGroup)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-dbg.Mach.WhenDisposed():
	case <-sigChan:
	case <-dbg.Mach.WhenNot1(ss.Start, nil):
	}

	// show footer stats
	printStats(dbg)

	dbg.Dispose()

	// pprof memory profile
	types.HandleProfMem(logger, &p)
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
