// am-dbg is a lightweight, multi-client debugger for asyncmachine-go.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
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
func cliRun(_ *cobra.Command, _ []string, p types.Params) {
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

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
	log.SetOutput(logger.Writer())

	httpAddr := ""
	sshAddr := ""
	if p.ListenAddr != "-1" {
		host, port, err := net.SplitHostPort(p.ListenAddr)
		if err != nil {
			panic(err)
		}
		dbgPort, _ := strconv.Atoi(port)
		httpPort := dbgPort + 1
		sshPort := httpPort + 1
		httpAddr = host + ":" + strconv.Itoa(httpPort)
		sshAddr = host + ":" + strconv.Itoa(sshPort)
	}

	if !p.UiSsh {
		sshAddr = ""
	}
	if !p.UiWeb {
		httpAddr = ""
	}

	// init the debugger
	dbg, err := debugger.New(ctx, debugger.Opts{
		Id:            p.Id,
		DbgLogLevel:   p.LogLevel,
		DbgLogger:     logger,
		ImportData:    p.ImportData,
		OutputClients: p.OutputClients,
		// TODO expose levels as states
		OutputDiagrams: p.OutputDiagrams,
		OutputTx:       p.OutputTx,
		OutputLog:      p.OutputLog,
		Timelines:      p.ViewTimelines,
		// ...:           p.FilterLogLevel,
		OutputDir:       p.OutputDir,
		AddrRpc:         p.ListenAddr,
		AddrHttp:        httpAddr,
		AddrSsh:         sshAddr,
		UiSsh:           p.UiSsh && sshAddr != "",
		UiWeb:           p.UiWeb && httpAddr != "",
		EnableMouse:     p.EnableMouse,
		EnableClipboard: p.EnableClipboard,
		MachUrl:         p.MachUrl,
		SelectConnected: p.SelectConnected,
		ShowReader:      p.ViewReader,
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
		_ = amhelp.MachDebug(dbg.Mach, p.DebugAddr, p.LogLevel, false,
			amhelp.SemConfigEnv(true))

	}

	// TODO --otel flag
	// dbg.Mach.SemLogger().EnableSteps(true)
	// os.Setenv(amtele.EnvService, "dbg")
	// os.Setenv(amtele.EnvOtelTrace, "1")
	// os.Setenv(amtele.EnvOtelTraceTxs, "1")
	// os.Setenv(amtele.EnvOtelTraceArgs, "1")
	// os.Setenv(amtele.EnvOtelTraceAllowStates,
	// 	"ClientSelected,SelectingClient,RemoveClient,BuildingLog,LogBuilt")
	// os.Setenv(amtele.EnvOtelTraceAllowStatesRe, "^Diagrams")
	// err = amtele.MachBindOtelEnv(dbg.Mach)
	// if err != nil {
	// 	panic(err)
	// }

	// rpc server
	if p.ListenAddr != "-1" {
		go server.StartRpc(dbg.Mach, p.ListenAddr, nil, p)
	}

	// start and wait till the end
	// TODO move to params
	dbg.Start(p.StartupMachine, p.StartupTx, p.StartupView, p.StartupGroup)

	select {
	case <-dbg.Mach.WhenDisposed():
	case <-dbg.Mach.WhenNot1(ss.Start, nil):
	}

	// show footer stats
	printStats(dbg)

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
