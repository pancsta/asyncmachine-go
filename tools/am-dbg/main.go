package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/am-dbg/debugger"
	ss "github.com/pancsta/asyncmachine-go/tools/am-dbg/states"
)

const (
	cliParamLogFile     string = "log-file"
	cliParamLogLevel    string = "log-level"
	cliParamLogID       string = "log-machine-id"
	cliParamServerURL   string = "server-url"
	cliParamAmDbgURL    string = "am-dbg-url"
	cliParamEnableMouse string = "enable-mouse"
	cliParamVersion     string = "version"
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
	rootCmd.Flags().String(cliParamLogFile, "am-dbg.log",
		"Log file path")
	rootCmd.Flags().Int(cliParamLogLevel, 0,
		"Log level, 0-5 (silent-everything)")
	rootCmd.Flags().Bool(cliParamLogID, true,
		"Include machine ID in log messages")
	rootCmd.Flags().String(cliParamServerURL, telemetry.RpcHost,
		"Host and port for the server to listen on")
	rootCmd.Flags().String(cliParamAmDbgURL, "",
		"Debug this instance of am-dbg with another one")
	rootCmd.Flags().Bool(cliParamEnableMouse, false,
		"Enable mouse support")
	rootCmd.Flags().Bool(cliParamVersion, false,
		"Print version and exit")
	return rootCmd
}

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
	enableMouse, err := cmd.Flags().GetBool(cliParamEnableMouse)
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
	if logLevel > 0 && logFile != "" {
		_ = os.Remove(logFile)
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			panic(err)
		}
		defer file.Close()
		log.SetOutput(file)
	}

	mach, err := initMachine(logLevel, enableMouse)
	if err != nil {
		panic(err)
	}

	// rpc client
	if debugURL != "" {
		err := telemetry.MonitorTransitions(mach, debugURL)
		// TODO retries
		if err != nil {
			panic(err.Error())
		}
	}

	// rpc server
	go debugger.StartRCP(&debugger.RPCServer{
		Mach: mach,
		URL:  serverURL,
	})

	// start and wait till the end
	mach.Add(am.S{"Init"}, nil)
	<-mach.WhenNot(am.S{"Init"}, nil)
}

func initMachine(logLevel am.LogLevel, enableMouse bool) (*am.Machine, error) {
	// machine
	ctx := context.Background()
	mach := am.New(ctx, ss.States, &am.Opts{
		HandlerTimeout:       time.Hour,
		DontPanicToException: true,
	})
	err := mach.VerifyStates(ss.Names)
	if err != nil {
		return nil, err
	}

	mach.SetLogLevel(logLevel)
	mach.LogID = false
	mach.SetLogger(func(level am.LogLevel, msg string, args ...any) {
		txt := fmt.Sprintf(msg, args...)
		log.Print(txt)
	})

	// init handlers
	h := &debugger.Debugger{
		Mach:        mach,
		EnableMouse: enableMouse,
	}
	err = mach.BindHandlers(h)
	if err != nil {
		return nil, err
	}

	return mach, nil
}
