package cli

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
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
	pDbgLogLevel      = "dbg-log-level"
	pDbgAmDbgAddr     = "dbg-am-dbg-addr"
	pGoRace           = "dbg-go-race"
	pId               = "dbg-id"
	pServerAddr       = "listen-on"
	pServerAddrShort  = "l"
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
	pStartupGroup     = "select-group"
	pStartupTxShort   = "t"
	pTail             = "tail"
	// TODO AM_DBG_PROF
	pProfSrv      = "dbg-prof-srv"
	pMaxMem       = "max-mem"
	pLogOpsTtl    = "log-ops-ttl"
	pReaderShort  = "r"
	pFwdData      = "fwd-data"
	pFwdDataShort = "f"
	// TODO --filters + loglevel, eg canceled,L1,empty
	// TODO --view-rain

	// MISC

	pDir            = "dir"
	pOutputDirShort = "d"
	pOutputClients  = "output-clients"
	pOutputTx       = "output-tx"
	pOutputDiagrams = "output-diagrams"
	pUiDiagrams     = "ui-diagrams"

	// UI

	pView       = "view"
	pViewShort  = "v"
	pViewNarrow = "view-narrow"
	pViewReader = "view-reader"
	pTimelines  = "view-timelines"
	pRain       = "view-rain"

	// filters

	pFilterGroup    = "filter-group"
	pFilterLogLevel = "filter-log-level"
	// TODO filter-auto=exec=all
	// TODO filter-checked
	// TODO rest of filters
)

type Params struct {
	// LogLevel is the log level of this instance (for debugging).
	LogLevel am.LogLevel
	// Id is the ID of this asyncmachine (for debugging).
	Id              string
	Version         bool
	ListenAddr      string
	DebugAddr       string
	RaceDetector    bool
	ImportData      string
	EnableMouse     bool
	CleanOnConnect  bool
	SelectConnected bool
	ProfMem         bool
	ProfCpu         bool
	ProfSrv         string
	MaxMemMb        int
	LogOpsTtl       time.Duration
	Reader          bool
	FwdData         []string

	StartupMachine string
	StartupView    string
	StartupTx      int
	StartupGroup   string

	ViewNarrow bool
	ViewRain   bool

	FilterGroup    bool
	FilterLogLevel am.LogLevel

	OutputDir      string
	OutputDiagrams int
	UiDiagrams     bool
	OutputClients  bool
	Timelines      int
	Rain           bool
	TailMode       bool
	OutputTx       bool
	MachUrl        string
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
	// TODO understand string names like Changes, Ops
	f.Int(pDbgLogLevel, int(am.LogNothing),
		"Log level produced by this instance, 0-5 (silent-everything)")
	f.StringP(pServerAddr, pServerAddrShort, telemetry.DbgAddr,
		"Host and port for the debugger to listen on")
	f.String(pId, "am-dbg", "ID of this instance")
	f.String(pDbgAmDbgAddr, "", "Debug this instance of am-dbg with another one")
	f.Bool(pGoRace, false, "Go race detector is enabled")
	f.StringP(pView, pViewShort, "tree-log",
		"Initial view (tree-log, tree-matrix, matrix)")
	f.StringP(pStartupMach,
		pStartupMachShort, "",
		"Select a machine by (partial) ID on startup (requires --"+pImport+")")

	// TODO parse copy-paste commas, eg 1,001
	f.IntP(pStartupTx, pStartupTxShort, 0,
		"Select a transaction by _number_ on startup (requires --"+
			pStartupMach+")")
	f.String(pStartupGroup, "", "Startup group")
	f.Bool(pEnableMouse, true,
		"Enable mouse support (experimental)")
	f.Bool(pCleanOnConnect, false,
		"Clean up disconnected clients on the 1st connection")
	f.BoolP(pSelectConn, pSelectConnShort, false,
		"Select the newly connected machine, if no other is connected")
	f.Bool(pViewNarrow, false,
		"Force a narrow view, independently of the viewport size")
	f.StringP(pImport, pImportShort, "",
		"Import an exported gob.br file")
	f.BoolP(pViewReader, pReaderShort, false, "Enable Log Reader")
	f.StringP(pFwdData, pFwdDataShort, "",
		"Forward incoming data to other instances (eg addr1,addr2)")

	// FILTERS

	f.Bool(pFilterGroup, true, "Filter transitions by a selected group")
	f.Int(pFilterLogLevel, int(am.LogChanges), "Filter transitions to this log "+
		"level produced by this instance, 0-5 (silent-everything)")

	// MISC

	f.String(pProfSrv, "", "Start pprof server")
	f.Int(pMaxMem, 1000, "Max memory usage (in MB) to flush old transitions")
	f.String(pLogOpsTtl, "24h", "Max time to live for logs level LogOps")
	f.Bool(pVersion, false, "Print version and exit")

	// OUTPUT

	f.StringP(pDir, pOutputDirShort, ".",
		"Output directory for generated files")
	f.Bool(pOutputClients, false,
		"Write a detailed client list into am-dbg-clients.txt inside --dir")
	f.Bool(pOutputTx, false,
		"Write the current transition with steps into am-dbg-tx.md inside --dir")
	f.Int(pOutputDiagrams, 0,
		"Level of details for diagrams (svg, d2, mermaid) in "+
			"--dir (0 off, 1-3 on). EXPERIMENTAL")
	f.Bool(pUiDiagrams, true,
		"Start a web diagrams viewer on a +1 port (EXPERIMENTAL)")
	f.Int(pTimelines, 2, "Number of timelines to show (0-2)")
	f.Bool(pRain, false, "Show the rain view")
	f.Bool(pTail, true, "Start from the lastest tx")
}

func ParseParams(cmd *cobra.Command, args []string) Params {
	// TODO dont panic
	url := ""
	if len(args) > 0 {
		url = args[0]
	}

	// params
	version, err := cmd.Flags().GetBool(pVersion)
	if err != nil {
		panic(err)
	}

	outputDir := cmd.Flag(pDir).Value.String()
	id := cmd.Flag(pId).Value.String()

	dbgLogLevelInt, err := cmd.Flags().GetInt(pDbgLogLevel)
	if err != nil {
		panic(err)
	}
	dbgLogLevel := am.LogLevel(min(5, dbgLogLevelInt))

	filterLogLevelInt, err := cmd.Flags().GetInt(pFilterLogLevel)
	if err != nil {
		panic(err)
	}
	filterLogLevel := am.LogLevel(min(5, filterLogLevelInt))

	serverAddr := cmd.Flag(pServerAddr).Value.String()
	debugAddr := cmd.Flag(pDbgAmDbgAddr).Value.String()
	importData := cmd.Flag(pImport).Value.String()
	startupGroup := cmd.Flag(pStartupGroup).Value.String()

	race, err := cmd.Flags().GetBool(pGoRace)
	if err != nil {
		panic(err)
	}

	// diagrams
	outputDiagrams, err := cmd.Flags().GetInt(pOutputDiagrams)
	if err != nil {
		panic(err)
	}
	outputDiagrams = min(3, outputDiagrams)
	uiDiagrams, err := cmd.Flags().GetBool(pUiDiagrams)
	if err != nil {
		panic(err)
	}

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
	outputTx, err := cmd.Flags().GetBool(pOutputTx)
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
	log2Ttl, err := time.ParseDuration(cmd.Flag(pLogOpsTtl).Value.String())
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

	reader, err := cmd.Flags().GetBool(pViewReader)
	if err != nil {
		panic(err)
	}

	filterGroup, err := cmd.Flags().GetBool(pFilterGroup)
	if err != nil {
		panic(err)
	}

	// TODO suppose multiple instances
	var fwdData []string
	if fwdDataRaw != "" {
		fwdData = strings.Split(fwdDataRaw, ",")
	}

	return Params{
		MachUrl:    url,
		ListenAddr: serverAddr,
		FwdData:    fwdData,

		// config

		StartupMachine:  startupMachine,
		StartupTx:       startupTx,
		CleanOnConnect:  cleanOnConnect,
		SelectConnected: selectConnected,
		TailMode:        tail,

		// UI

		EnableMouse:  enableMouse,
		Reader:       reader,
		StartupView:  startupView,
		StartupGroup: startupGroup,
		ViewNarrow:   viewNarrow,
		ViewRain:     rain,

		// filters

		FilterGroup:    filterGroup,
		FilterLogLevel: filterLogLevel,

		// output

		OutputClients:  outputClients,
		OutputTx:       outputTx,
		OutputDir:      outputDir,
		OutputDiagrams: outputDiagrams,
		UiDiagrams:     uiDiagrams,
		Timelines:      timelines,

		// misc

		MaxMemMb:  maxMem,
		LogOpsTtl: log2Ttl,
		Version:   version,

		// dbg

		DebugAddr:    debugAddr,
		RaceDetector: race,
		ImportData:   importData,
		LogLevel:     dbgLogLevel,
		Id:           id,
		ProfSrv:      profSrv,
	}
}

// GetLogger returns a file logger, according to params.
func GetLogger(params *Params, dir string) *log.Logger {
	// TODO slog

	if params.LogLevel <= am.LogNothing {
		return log.Default()
	}
	name := filepath.Join(dir, "am-dbg.log")

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
