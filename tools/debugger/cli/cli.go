package cli

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"

	"github.com/spf13/cobra"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

const (
	cliParamLogFile          = "log-file"
	cliParamLogLevel         = "log-level"
	cliParamServerAddr       = "listen-on"
	cliParamServerAddrShort  = "l"
	cliParamAmDbgAddr        = "am-dbg-addr"
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
	cliParamProfMem          = "prof-mem"
	cliParamProfCpu          = "prof-cpu"
	cliParamProfSrv          = "prof-srv"
)

type Params struct {
	LogLevel        am.LogLevel
	LogFile         string
	Version         bool
	ServerAddr      string
	DebugAddr       string
	ImportData      string
	StartupMachine  string
	StartupView     string
	StartupTx       int
	EnableMouse     bool
	CleanOnConnect  bool
	SelectConnected bool
	ProfMem         bool
	ProfCpu         bool
	ProfSrv         bool
}

type RootFn func(cmd *cobra.Command, args []string, params Params)

func RootCmd(fn RootFn) *cobra.Command {
	// TODO --dark-mode auto|on|off
	// TODO --rename-duplicates renames disconnected dups as NAME-TIME
	rootCmd := &cobra.Command{
		Use: "am-dbg",
		Run: func(cmd *cobra.Command, args []string) {
			fn(cmd, args, ParseParams(cmd, args))
		},
	}

	AddFlags(rootCmd)

	return rootCmd
}

func AddFlags(rootCmd *cobra.Command) {
	f := rootCmd.Flags()
	f.String(cliParamLogFile, "am-dbg.log",
		"Log file path")
	f.Int(cliParamLogLevel, 0,
		"Log level, 0-5 (silent-everything)")
	f.StringP(cliParamServerAddr,
		cliParamServerAddrShort, telemetry.DbgAddr,
		"Host and port for the debugger to listen on")
	f.String(cliParamAmDbgAddr, "",
		"Debug this instance of am-dbg with another one")
	f.StringP(cliParamView, cliParamViewShort, "tree-log",
		"Initial view (tree-log, tree-matrix, matrix)")
	f.StringP(cliParamStartupMach,
		cliParamStartupMachShort, "",
		"Select a machine by ID on startup (requires --"+cliParamImport+")")

	// TODO parse copy-paste commas, eg 1,001
	f.IntP(cliParamStartupTx, cliParamStartupTxShort, 0,
		"Select a transaction by _number_ on startup (requires --"+
			cliParamStartupMach+")")
	f.Bool(cliParamEnableMouse, false,
		"Enable mouse support (experimental)")
	f.Bool(cliParamCleanOnConnect, false,
		"Clean up disconnected clients on the 1st connection")
	f.BoolP(cliParamSelectConn, cliParamSelectConnShort, false,
		"Select the newly connected machine, if no other is connected")
	f.StringP(cliParamImport, cliParamImportShort, "",
		"ImportFile an exported gob.bt file")
	f.Bool(cliParamVersion, false,
		"Print version and exit")

	// profile
	f.Bool(cliParamProfMem, false, "Profile memory usage")
	f.Bool(cliParamProfCpu, false, "Profile CPU usage")
	f.Bool(cliParamProfSrv, false, "Start pprof server on :6060")
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
	serverAddr := cmd.Flag(cliParamServerAddr).Value.String()
	debugAddr := cmd.Flag(cliParamAmDbgAddr).Value.String()
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

	profMem, err := cmd.Flags().GetBool(cliParamProfMem)
	if err != nil {
		panic(err)
	}

	profCpu, err := cmd.Flags().GetBool(cliParamProfCpu)
	if err != nil {
		panic(err)
	}

	profSrv, err := cmd.Flags().GetBool(cliParamProfSrv)
	if err != nil {
		panic(err)
	}

	return Params{
		LogLevel:        logLevel,
		LogFile:         logFile,
		Version:         version,
		ServerAddr:      serverAddr,
		DebugAddr:       debugAddr,
		ImportData:      importData,
		StartupMachine:  startupMachine,
		StartupView:     startupView,
		StartupTx:       startupTx,
		EnableMouse:     enableMouse,
		CleanOnConnect:  cleanOnConnect,
		SelectConnected: selectConnected,

		// profiling
		ProfMem: profMem,
		ProfCpu: profCpu,
		ProfSrv: profSrv,
	}
}

// GetLogger returns a file logger, according to params.
func GetLogger(params *Params) *log.Logger {
	// TODO slog

	// file logging
	if params.LogFile == "" || params.LogLevel < 1 {
		return log.Default()
	}

	_ = os.Remove(params.LogFile)
	file, err := os.OpenFile(params.LogFile, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		panic(err)
	}

	return log.New(file, "", log.LstdFlags)
}

func HandleProfMem(logger *log.Logger, p *Params) {
	if !p.ProfMem {
		return
	}

	f, err := os.Create("mem.prof")
	if err != nil {
		logger.Fatal("could not create memory profile: ", err)
	}
	defer f.Close() // error handling omitted for example

	runtime.GC() // get up-to-date statistics
	if err := pprof.WriteHeapProfile(f); err != nil {
		logger.Fatal("could not write memory profile: ", err)
	}
}

func StartCpuProfile(logger *log.Logger, p *Params) func() {
	if !p.ProfCpu {
		return nil
	}

	f, err := os.Create("cpu.prof")
	if err != nil {
		logger.Fatal("could not create CPU profile: ", err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		logger.Fatal("could not start CPU profile: ", err)
	}
	return func() {
		pprof.StopCPUProfile()
		f.Close()
	}
}

func StartCpuProfileSrv(ctx context.Context, logger *log.Logger, p *Params) {
	if !p.ProfSrv {
		return
	}
	go func() {
		logger.Println("Starting pprof server on :6060")
		// TODO support ctx cancel
		if err := http.ListenAndServe(":6060", nil); err != nil {
			logger.Fatalf("could not start pprof server: %v", err)
		}
	}()
}
