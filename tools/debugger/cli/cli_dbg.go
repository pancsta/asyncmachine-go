package cli

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/spf13/cobra"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

// TODO enum
// TODO cobra groups
const (
	// TODO remove
	pLogLevel        = "log-level"
	pServerAddr      = "listen-on"
	pServerAddrShort = "l"
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
	pTail             = "tail"
	// TODO AM_DBG_PROF
	pProfSrv      = "prof-srv"
	pMaxMem       = "max-mem"
	pLog2Ttl      = "log-2-ttl"
	pReaderShort  = "r"
	pFwdData      = "fwd-data"
	pFwdDataShort = "f"
	// TODO --filters + loglevel, eg canceled,L1,empty
	// TODO --view-rain

	// MISC

	pDir            = "dir"
	pOutputDirShort = "d"
	pOutputClients  = "output-clients"

	// UI

	pView       = "view"
	pViewShort  = "v"
	pViewNarrow = "view-narrow"
	pReader     = "view-reader"
	pDiagrams   = "diagrams"
	pTimelines  = "view-timelines"
	pRain       = "view-rain"
)

type Params struct {
	LogLevel        am.LogLevel
	Version         bool
	ListenAddr      string
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
	ProfSrv         string
	MaxMemMb        int
	Log2Ttl         time.Duration
	Reader          bool
	FwdData         []string
	ViewNarrow      bool
	ViewRain        bool

	OutputDir     string
	Graph         int
	OutputClients bool
	Timelines     int
	Rain          bool
	TailMode      bool
}

type RootFn func(cmd *cobra.Command, args []string, params Params)

func RootCmd(fn RootFn) *cobra.Command {
	// TODO --dark-mode auto|on|off
	// TODO --rename-duplicates renames disconnected dups as NAME-TIME
	// TODO group flags in groups
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
	f.Int(pLogLevel, 0, "Log level, 0-5 (silent-everything)")
	f.StringP(pServerAddr, pServerAddrShort, telemetry.DbgAddr,
		"Host and port for the debugger to listen on")
	f.String(pAmDbgAddr, "", "Debug this instance of am-dbg with another one")
	f.StringP(pView, pViewShort, "tree-log",
		"Initial view (tree-log, tree-matrix, matrix)")
	f.StringP(pStartupMach,
		pStartupMachShort, "",
		"Select a machine by (partial) ID on startup (requires --"+pImport+")")

	// TODO parse copy-paste commas, eg 1,001
	f.IntP(pStartupTx, pStartupTxShort, 0,
		"Select a transaction by _number_ on startup (requires --"+
			pStartupMach+")")
	f.Bool(pEnableMouse, true,
		"Enable mouse support (experimental)")
	f.Bool(pCleanOnConnect, false,
		"Clean up disconnected clients on the 1st connection")
	f.BoolP(pSelectConn, pSelectConnShort, false,
		"Select the newly connected machine, if no other is connected")
	f.Bool(pViewNarrow, false,
		"Force a narrow view, independently of the viewport size")
	f.StringP(pImport, pImportShort, "",
		"Import an exported gob.bt file")
	f.BoolP(pReader, pReaderShort, false, "Enable Log Reader")
	f.StringP(pFwdData, pFwdDataShort, "",
		"Forward incoming data to other instances (eg addr1,addr2)")

	// MISC

	f.String(pProfSrv, "", "Start pprof server")
	f.Int(pMaxMem, 100, "Max memory usage (in MB) to flush old transitions")
	f.String(pLog2Ttl, "24h", "Max time to live for logs level 2")
	f.Bool(pVersion, false, "Print version and exit")

	// OUTPUT

	f.StringP(pDir, pOutputDirShort, ".",
		"Output directory for generated files")
	f.Bool(pOutputClients, false,
		"Write a detailed client list into in am-dbg-clients.txt inside --dir")
	f.Int(pDiagrams, 0,
		"Level of details for graphs (svg, d2, mermaid) in --dir (0-3)")
	f.Int(pTimelines, 2, "Number of timelines to show (0-2)")
	f.Bool(pRain, false, "Show the rain view")
	f.Bool(pTail, true, "Start from the last tx")
}

func ParseParams(cmd *cobra.Command, _ []string) Params {
	// TODO dont panic

	// params
	version, err := cmd.Flags().GetBool(pVersion)
	if err != nil {
		panic(err)
	}

	outputDir := cmd.Flag(pDir).Value.String()
	logLevelInt, err := cmd.Flags().GetInt(pLogLevel)
	if err != nil {
		panic(err)
	}
	logLevelInt = min(4, logLevelInt)

	logLevel := am.LogLevel(logLevelInt)
	serverAddr := cmd.Flag(pServerAddr).Value.String()
	debugAddr := cmd.Flag(pAmDbgAddr).Value.String()
	importData := cmd.Flag(pImport).Value.String()

	// graph
	graph, err := cmd.Flags().GetInt(pDiagrams)
	if err != nil {
		panic(err)
	}
	graph = min(3, graph)

	// timelines
	timelines, err := cmd.Flags().GetInt(pTimelines)
	if err != nil {
		panic(err)
	}
	timelines = min(2, timelines)

	outputClients, err := cmd.Flags().GetBool(pOutputClients)
	if err != nil {
		panic(err)
	}
	fwdDataRaw := cmd.Flag(pFwdData).Value.String()
	profSrv := cmd.Flag(pProfSrv).Value.String()
	startupMachine := cmd.Flag(pStartupMach).Value.String()
	startupView := cmd.Flag(pView).Value.String()
	startupTx, err := cmd.Flags().GetInt(pStartupTx)
	if err != nil {
		panic(err)
	}
	maxMem, err := cmd.Flags().GetInt(pMaxMem)
	if err != nil {
		panic(err)
	}
	log2Ttl, err := time.ParseDuration(cmd.Flag(pLog2Ttl).Value.String())
	if err != nil {
		panic(err)
	}

	rain, err := cmd.Flags().GetBool(pRain)
	if err != nil {
		panic(err)
	}
	if rain && startupView == "tree-log" {
		startupView = "tree-matrix"
	}

	tail, err := cmd.Flags().GetBool(pTail)
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

	viewNarrow, err := cmd.Flags().GetBool(pViewNarrow)
	if err != nil {
		panic(err)
	}

	reader, err := cmd.Flags().GetBool(pReader)
	if err != nil {
		panic(err)
	}

	// TODO suppose multiple instances
	var fwdData []string
	if fwdDataRaw != "" {
		fwdData = strings.Split(fwdDataRaw, ",")
	}

	return Params{
		ListenAddr: serverAddr,
		DebugAddr:  debugAddr,
		ImportData: importData,
		FwdData:    fwdData,

		// config

		StartupMachine:  startupMachine,
		StartupTx:       startupTx,
		CleanOnConnect:  cleanOnConnect,
		SelectConnected: selectConnected,
		TailMode:        tail,

		// UI

		EnableMouse: enableMouse,
		Reader:      reader,
		StartupView: startupView,
		ViewNarrow:  viewNarrow,
		ViewRain:    rain,

		// output

		OutputClients: outputClients,
		OutputDir:     outputDir,
		Graph:         graph,
		Timelines:     timelines,

		// misc

		LogLevel: logLevel,
		ProfSrv:  profSrv,
		MaxMemMb: maxMem,
		Log2Ttl:  log2Ttl,
		Version:  version,
	}
}

// GetLogger returns a file logger, according to params.
func GetLogger(params *Params) *log.Logger {
	// TODO slog

	if params.LogLevel < am.LogNothing {
		return log.Default()
	}
	name := "am-dbg.log"

	// file logging
	_ = os.Remove(name)
	file, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY, 0o666)
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
	if p.ProfSrv == "" {
		return
	}
	go func() {
		logger.Println("Starting pprof server on " + p.ProfSrv)
		// TODO support ctx cancel
		if err := http.ListenAndServe(p.ProfSrv, nil); err != nil {
			logger.Fatalf("could not start pprof server: %v", err)
		}
	}()
}
