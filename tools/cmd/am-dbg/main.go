package main

import (
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"

	"github.com/spf13/cobra"
)

const (
	cliParamLogFile          = "log-file"
	cliParamLogLevel         = "log-level"
	cliParamServerURL        = "listen-on"
	cliParamServerURLShort   = "l"
	cliParamAmDbgURL         = "am-dbg-url"
	cliParamEnableMouse      = "enable-mouse"
	cliParamCleanOnConnect   = "clean-on-connect"
	cliParamVersion          = "version"
	cliParamImport           = "import-data"
	cliParamImportShort      = "i"
	cliParamSelectConn       = "select-connected"
	cliParamSelectConnShort  = "c"
	cliParamStartupMach      = "select-machine"
	cliParamStartupMachShort = "m"
	cliParamStartupTx        = "select-transition"
	cliParamStartupTxShort   = "t"
	cliParamView             = "view"
	cliParamViewShort        = "v"
)

func main() {
	rootCmd := getRootCmd()
	err := rootCmd.Execute()
	if err != nil {
		panic(err)
	}
}

func getRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "am-dbg",
		Run: cliRun,
	}

	// TODO --dark-mode auto|on|off
	// TODO --rename-duplicates renames disconnected dups as TIME-NAME
	rootCmd.Flags().String(cliParamLogFile, "am-dbg.log",
		"Log file path")
	rootCmd.Flags().Int(cliParamLogLevel, 0,
		"Log level, 0-5 (silent-everything)")
	rootCmd.Flags().StringP(cliParamServerURL,
		cliParamServerURLShort, telemetry.DbgHost,
		"Host and port for the debugger to listen on")
	rootCmd.Flags().String(cliParamAmDbgURL, "",
		"Debug this instance of am-dbg with another one")
	rootCmd.Flags().StringP(cliParamView, cliParamViewShort, "tree-log",
		"Initial view (tree-log, tree-matrix, matrix)")
	rootCmd.Flags().StringP(cliParamStartupMach,
		cliParamStartupMachShort, "",
		"Select a machine by ID on startup (requires --"+cliParamImport+")")
	// TODO parse copy-paste commas, eg 1,001
	rootCmd.Flags().IntP(cliParamStartupTx, cliParamStartupTxShort, 0,
		"Select a transaction by _number_ on startup (requires --"+
			cliParamStartupMach+")")
	rootCmd.Flags().Bool(cliParamEnableMouse, false,
		"Enable mouse support (experimental)")
	rootCmd.Flags().Bool(cliParamCleanOnConnect, false,
		"Clean up disconnected clients on the 1st connection")
	rootCmd.Flags().BoolP(cliParamSelectConn, cliParamSelectConnShort, false,
		"Select the newly connected machine, if no other is connected")
	rootCmd.Flags().StringP(cliParamImport, cliParamImportShort, "",
		"Import an exported gob.bz2 file")
	rootCmd.Flags().Bool(cliParamVersion, false,
		"Print version and exit")

	return rootCmd
}

// TODO error msgs
func cliRun(cmd *cobra.Command, _ []string) {

	// params
	version, err := cmd.Flags().GetBool(cliParamVersion)
	if err != nil {
		panic(err)
	}

	logFile := cmd.Flag(cliParamLogFile).Value.String()
	logLevelInt, err := cmd.Flags().GetInt(cliParamLogLevel)
	if err != nil {
		panic(err)
	}

	logLevel := am.LogLevel(logLevelInt)
	serverURL := cmd.Flag(cliParamServerURL).Value.String()
	debugURL := cmd.Flag(cliParamAmDbgURL).Value.String()
	importGob := cmd.Flag(cliParamImport).Value.String()
	startupMachine := cmd.Flag(cliParamStartupMach).Value.String()
	startupView := cmd.Flag(cliParamView).Value.String()
	startupTx, err := cmd.Flags().GetInt(cliParamStartupTx)
	if err != nil {
		panic(err)
	}

	enableMouse, err := cmd.Flags().GetBool(cliParamEnableMouse)
	if err != nil {
		panic(err)
	}

	cleanOnConnect, err := cmd.Flags().GetBool(cliParamCleanOnConnect)
	if err != nil {
		panic(err)
	}

	selectConnected, err := cmd.Flags().GetBool(cliParamSelectConn)
	if err != nil {
		panic(err)
	}

	if version {
		build, ok := debug.ReadBuildInfo()
		if !ok {
			panic("No build info available")
		}
		fmt.Println(build.Main.Version)
		os.Exit(0)
	}

	// file logging
	if logFile != "" {
		_ = os.Remove(logFile)
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			panic(err)
		}
		defer file.Close()
		log.SetOutput(file)
	}

	// init the debugger
	// TODO extract to tools/debugger
	dbg := &debugger.Debugger{
		URL:             serverURL,
		EnableMouse:     enableMouse,
		CleanOnConnect:  cleanOnConnect,
		SelectConnected: selectConnected,
		Clients:         make(map[string]*debugger.Client),
		LogLevel:        am.LogChanges,
	}
	gob.Register(debugger.Exportable{})
	gob.Register(am.Relation(0))

	// import data
	if importGob != "" {
		dbg.ImportData(importGob)
	}

	// init machine
	dbg.Mach, err = initMachine(dbg, logLevel)
	if err != nil {
		panic(err)
	}

	// rpc client
	if debugURL != "" {
		err := telemetry.TransitionsToDBG(dbg.Mach, debugURL)
		// TODO retries
		if err != nil {
			panic(err)
		}
	}

	// rpc server
	go server.StartRCP(dbg.Mach, serverURL)

	// start and wait till the end
	dbg.Mach.Add1(ss.Start, am.A{
		"Client.id":       startupMachine,
		"Client.cursorTx": startupTx,
		"dbgView":         startupView,
	})
	<-dbg.Mach.WhenNot1(ss.Start, nil)
	txs := 0
	for _, c := range dbg.Clients {
		txs += len(c.MsgTxs)
	}

	// TODO format numbers
	_, _ = dbg.P.Printf("Clients: %d\n", len(dbg.Clients))
	_, _ = dbg.P.Printf("Transitions: %d\n", txs)
}

// TODO extract to tools/debugger
func initMachine(
	h *debugger.Debugger, logLevel am.LogLevel) (*am.Machine, error) {
	ctx := context.Background()

	// TODO use NewCommon
	mach := am.New(ctx, ss.States, &am.Opts{
		HandlerTimeout:       time.Hour,
		DontPanicToException: true,
		DontLogID:            true,
	})
	mach.ID = "am-dbg-" + mach.ID

	err := mach.VerifyStates(ss.Names)
	if err != nil {
		return nil, err
	}

	mach.SetTestLogger(log.Printf, logLevel)
	mach.SetLogArgs(am.NewArgsMapper([]string{"Machine.id", "conn_id"}, 20))

	err = mach.BindHandlers(h)
	if err != nil {
		return nil, err
	}

	return mach, nil
}
