// am-dbg is a lightweight, multi-client debugger for asyncmachine-go.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexflint/go-arg"
	
	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	"github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

func main() {
	var p types.Params
	parser := arg.MustParse(&p)

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := cliRun(ctx, p); err != nil {
		parser.Fail(err.Error())
	}
}

func cliRun(ctx context.Context, p types.Params) error {
	// print the version
	ver := utils.GetVersion()
	if p.PrintVersion {
		println(ver)
		return nil
	}

	// logger and profiler
	p.DbgLogger = types.GetLogger(&p, p.OutputDir)
	types.StartCpuProfileSrv(ctx, p.DbgLogger, &p)
	stopProfile := types.StartCpuProfile(p.DbgLogger, &p)
	if stopProfile != nil {
		defer stopProfile()
	}
	log.SetOutput(p.DbgLogger.Writer())

	// init the debugger
	dbg, err := debugger.New(ctx, p)
	if err != nil {
		return err
	}

	// rpc client
	if p.DebugAddr != "" {
		_ = amhelp.MachDebug(dbg.Mach, p.DebugAddr, p.LogLevel, false,
			amhelp.SemConfigEnv(true))

	}

	if p.DbgOtel {
		dbg.Mach.SemLogger().EnableSteps(true)
		os.Setenv(amtele.EnvService, "dbg")
		os.Setenv(amtele.EnvOtelTrace, "1")
		os.Setenv(amtele.EnvOtelTraceTxs, "1")
		os.Setenv(amtele.EnvOtelTraceArgs, "1")
		// AM_OTEL_TRACE_ALLOW_STATES=ClientSelected,SelectingClient,RemoveClient,
		// BuildingLog,LogBuilt
		// AM_OTEL_TRACE_ALLOW_STATES_RE=^Diagrams
		// os.Setenv(amtele.EnvOtelTraceAllowStates,
		// 	"ClientSelected,SelectingClient,RemoveClient,BuildingLog,LogBuilt")
		// os.Setenv(amtele.EnvOtelTraceAllowStatesRe, "^Diagrams")
		if err := amtele.MachBindOtelEnv(dbg.Mach); err != nil {
			return err
		}
	}

	// rpc server
	if p.ListenAddr != "-1" {
		go server.StartRpc(dbg.Mach, p.ListenAddr, nil, p)
	}

	// start and wait till the end
	dbg.Start()
	select {
	case <-dbg.Mach.WhenDisposed():
	case <-dbg.Mach.WhenNot1(states.DebuggerStates.Start, nil):
	}

	// show footer stats
	printStats(dbg)

	// pprof memory profile
	types.HandleProfMem(p.DbgLogger, &p)

	return nil
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
