package cli

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/spf13/cobra"
	"log"
	"os"
	"runtime/debug"
)

const (
	cliParamLogFile          = "log-file"
	cliParamLogLevel         = "log-level"
	cliParamServerAddr       = "listen-on"
	cliParamServerAddrShort  = "l"
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

type Params struct {
	LogLevel        am.LogLevel
	LogFile         string
	Version         bool
	ServerURL       string
	DebugAddr       string
	ImportData      string
	StartupMachine  string
	StartupView     string
	StartupTx       int
	EnableMouse     bool
	CleanOnConnect  bool
	SelectConnected bool
}

type RootFn func(cmd *cobra.Command, args []string, params Params)

func RootCmd(fn RootFn) *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "am-dbg",
		Run: func(cmd *cobra.Command, args []string) {
			fn(cmd, args, ParseParams(cmd, args))
		},
	}

	// TODO --dark-mode auto|on|off
	// TODO --rename-duplicates renames disconnected dups as NAME-TIME
	rootCmd.Flags().String(cliParamLogFile, "am-dbg.log",
		"Log file path")
	rootCmd.Flags().Int(cliParamLogLevel, 0,
		"Log level, 0-5 (silent-everything)")
	rootCmd.Flags().StringP(cliParamServerAddr,
		cliParamServerAddrShort, telemetry.DbgHost,
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

func GetVersion() string {
	build, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}

	ver := build.Main.Version
	if ver == "" {
		return "(devel)"
	}

	return ver
}

func ParseParams(cmd *cobra.Command, _ []string) Params {
	// TODO dont panic

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
	serverURL := cmd.Flag(cliParamServerAddr).Value.String()
	debugURL := cmd.Flag(cliParamAmDbgURL).Value.String()
	importData := cmd.Flag(cliParamImport).Value.String()
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

	return Params{
		LogLevel:        logLevel,
		LogFile:         logFile,
		Version:         version,
		ServerURL:       serverURL,
		DebugAddr:       debugURL,
		ImportData:      importData,
		StartupMachine:  startupMachine,
		StartupView:     startupView,
		StartupTx:       startupTx,
		EnableMouse:     enableMouse,
		CleanOnConnect:  cleanOnConnect,
		SelectConnected: selectConnected,
	}
}

// GetFileLogger returns a file logger, according to params.
func GetFileLogger(params *Params) *log.Logger {
	// TODO slog

	// file logging
	if params.LogFile == "" || params.LogLevel < 1 {
		return nil
	}

	_ = os.Remove(params.LogFile)
	file, err := os.OpenFile(params.LogFile, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		panic(err)
	}

	return log.New(file, "", log.LstdFlags)
}
