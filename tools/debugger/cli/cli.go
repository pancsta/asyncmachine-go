package cli

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"

	"github.com/spf13/cobra"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

const (
	// TODO remove
	pLogFile          = "log-file"
	// TODO remove
	pLogLevel         = "log-level"
	// TODO refac: --addr
	pServerAddr       = "listen-on"
	pServerAddrShort  = "l"
	// TODO remove
	pAmDbgAddr        = "am-dbg-addr"
	pEnableMouse      = "enable-mouse"
	pCleanOnConnect   = "clean-on-connect"
	pVersion          = "version"
	pImport           = "import-data"
	pImportShort      = "i"
	pSelectConn       = "select-connected"
	pSelectConnShort  = "c"
	pStartupMach      = "select-machine"
	pStartupMachShort = "m"
	pStartupTx        = "select-transition"
	pStartupTxShort   = "t"
	pView             = "view"
	pViewShort        = "v"
	// TODO remove
	pProfMem          = "prof-mem"
	// TODO remove
	pProfCpu          = "prof-cpu"
	// TODO AM_DBG_PROF
	pProfSrv          = "prof-srv"
	// TODO --filters
	// TODO --view
	// TODO --rain
	// TODO --reader
	// TODO --gc-log 1h|1d
	// TODO --gc-log-ops 1h|1d
	// TODO --max-mem
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
	f.String(pLogFile, "am-dbg.log",
		"Log file path")
	f.Int(pLogLevel, 0,
		"Log level, 0-5 (silent-everything)")
	f.StringP(pServerAddr,
		pServerAddrShort, telemetry.DbgAddr,
		"Host and port for the debugger to listen on")
	f.String(pAmDbgAddr, "",
		"Debug this instance of am-dbg with another one")
	f.StringP(pView, pViewShort, "tree-log",
		"Initial view (tree-log, tree-matrix, matrix)")
	f.StringP(pStartupMach,
		pStartupMachShort, "",
		"Select a machine by ID on startup (requires --"+pImport+")")

	// TODO parse copy-paste commas, eg 1,001
	f.IntP(pStartupTx, pStartupTxShort, 0,
		"Select a transaction by _number_ on startup (requires --"+
			pStartupMach+")")
	f.Bool(pEnableMouse, false,
		"Enable mouse support (experimental)")
	f.Bool(pCleanOnConnect, false,
		"Clean up disconnected clients on the 1st connection")
	f.BoolP(pSelectConn, pSelectConnShort, false,
		"Select the newly connected machine, if no other is connected")
	f.StringP(pImport, pImportShort, "",
		"ImportFile an exported gob.bt file")
	f.Bool(pVersion, false,
		"Print version and exit")

	// profile
	f.Bool(pProfMem, false, "Profile memory usage")
	f.Bool(pProfCpu, false, "Profile CPU usage")
	f.Bool(pProfSrv, false, "Start pprof server on :6060")
}

func ParseParams(cmd *cobra.Command, _ []string) Params {
	// TODO dont panic

	// params
	version, err := cmd.Flags().GetBool(pVersion)
	if err != nil {
		panic(err)
	}

	logFile := cmd.Flag(pLogFile).Value.String()
	logLevelInt, err := cmd.Flags().GetInt(pLogLevel)
	if err != nil {
		panic(err)
	}

	logLevel := am.LogLevel(logLevelInt)
	serverAddr := cmd.Flag(pServerAddr).Value.String()
	debugAddr := cmd.Flag(pAmDbgAddr).Value.String()
	importData := cmd.Flag(pImport).Value.String()
	startupMachine := cmd.Flag(pStartupMach).Value.String()
	startupView := cmd.Flag(pView).Value.String()
	startupTx, err := cmd.Flags().GetInt(pStartupTx)
	if err != nil {
		panic(err)
	}

	enableMouse, err := cmd.Flags().GetBool(pEnableMouse)
	if err != nil {
		panic(err)
	}

	cleanOnConnect, err := cmd.Flags().GetBool(pCleanOnConnect)
	if err != nil {
		panic(err)
	}

	selectConnected, err := cmd.Flags().GetBool(pSelectConn)
	if err != nil {
		panic(err)
	}

	profMem, err := cmd.Flags().GetBool(pProfMem)
	if err != nil {
		panic(err)
	}

	profCpu, err := cmd.Flags().GetBool(pProfCpu)
	if err != nil {
		panic(err)
	}

	profSrv, err := cmd.Flags().GetBool(pProfSrv)
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
