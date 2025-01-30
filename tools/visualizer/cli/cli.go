package cli

import (
	"context"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"

	"github.com/spf13/cobra"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

const (
	pLogFile         = "log-file"
	pLogLevel        = "log-level"
	pVersion         = "version"
	pVersionShort    = "v"
	pServerAddr      = "addr"
	pServerAddrShort = "l"

	pEnableMouse      = "enable-mouse"
	pCleanOnConnect   = "clean-on-connect"
	pImport           = "import-data"
	pImportShort      = "i"
	pSelectConn       = "select-connected"
	pSelectConnShort  = "c"
	pStartupMach      = "select-machine"
	pStartupMachShort = "m"
	pStartupTx        = "select-transition"
	pStartupTxShort   = "t"

	// TODO AM_DBG_PROF
	pProfSrv      = "prof-srv"
	pMaxMem       = "max-mem"
	pReaderShort  = "r"
	pFwdData      = "fwd-data"
	pFwdDataShort = "f"
)

type Params struct {
	LogLevel   am.LogLevel
	LogFile    string
	Version    bool
	ServerAddr string
	DebugAddr  string
	ImportData string

	// later
	StartupMachine  string
	StartupView     string
	StartupTx       int
	EnableMouse     bool
	CleanOnConnect  bool
	SelectConnected bool
	ProfSrv         string

	MaxMemMb int
	FwdData  []string
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
	// TODO bind param here, remove ParseParams, support StringArrayP
	// p := &Params{}

	f := rootCmd.Flags()
	f.String(pLogFile, "", "Log file path")
	f.Int(pLogLevel, 0, "Log level, 0-5 (silent-everything)")
	f.StringP(pServerAddr, pServerAddrShort, telemetry.DbgAddr,
		"Host and port for the debugger to listen on")
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
	f.StringP(pImport, pImportShort, "",
		"Import an exported gob.bt file")
	f.StringP(pFwdData, pFwdDataShort, "",
		"Fordward incoming data to other instances (eg addr1,addr2)")

	// profile & mem
	f.String(pProfSrv, "", "Start pprof server")
	f.Int(pMaxMem, 100, "Max memory usage (in MB) to flush old transitions")

	f.BoolP(pVersion, pVersionShort, false, "Print version and exit")
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
	importData := cmd.Flag(pImport).Value.String()
	fwdDataRaw := cmd.Flag(pFwdData).Value.String()
	profSrv := cmd.Flag(pProfSrv).Value.String()
	startupMachine := cmd.Flag(pStartupMach).Value.String()
	startupTx, err := cmd.Flags().GetInt(pStartupTx)
	if err != nil {
		panic(err)
	}
	maxMem, err := cmd.Flags().GetInt(pMaxMem)
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

	// TODO suppose multiple instances
	var fwdData []string
	if fwdDataRaw != "" {
		fwdData = strings.Split(fwdDataRaw, ",")
	}

	return Params{
		Version:         version,
		LogLevel:        logLevel,
		LogFile:         logFile,
		ServerAddr:      serverAddr,
		ImportData:      importData,
		StartupMachine:  startupMachine,
		StartupTx:       startupTx,
		EnableMouse:     enableMouse,
		CleanOnConnect:  cleanOnConnect,
		SelectConnected: selectConnected,
		FwdData:         fwdData,

		// profiling
		ProfSrv:  profSrv,
		MaxMemMb: maxMem,
	}
}

// GetLogger returns a file logger, according to params.
func GetLogger(params *Params) *log.Logger {
	// TODO slog

	if params.LogLevel < am.LogNothing {
		return log.Default()
	}

	// file logging
	if params.LogFile == "" {
		return log.New(io.Discard, "", log.LstdFlags)
	}
	_ = os.Remove(params.LogFile)
	file, err := os.OpenFile(params.LogFile, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		panic(err)
	}

	return log.New(file, "", log.LstdFlags)
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
