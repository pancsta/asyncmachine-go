package types

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/coder/websocket"
	"github.com/orsinium-labs/enum"
	"github.com/pancsta/cview"
	"github.com/pancsta/tcell-v2"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

var ss = ssdbg.DebuggerStates

// ///// ///// /////

// ///// PARAMS

// ///// ///// /////

// Params are CLI params for am-dbg, also used as internal state via Debugger.Params.
//
//nolint:lll
type Params struct {

	// line args

	MachUrl string `arg:"positional" help:"Machine URL to connect to"`

	// main params

	ListenAddr     string   `arg:"-l,--listen-addr" default:"localhost:6831" help:"Host and port for the debugger to listen on"`
	OutputDir      string   `arg:"-d,--dir" default:"." help:"Output directory for generated files"`
	CleanOnConnect bool     `arg:"--clean-on-connect" default:"true" help:"Clean up disconnected clients on the 1st connection"`
	ImportData     string   `arg:"-i,--import-data" help:"Import an exported gob.br file"`
	FwdData        []string `arg:"-f,--fwd-data,separate" help:"Forward incoming data to other instances (repeatable)"`

	// select

	SelectConnected bool   `arg:"-c,--select-connected" help:"Select the newly connected machine, if no other is connected"`
	SelectGroup     string `arg:"--select-group" help:"Default group to select"`

	// minor params

	EnableClipboard      bool        `arg:"--enable-clipboard" default:"true" help:"Enable clipboard support"`
	EnableMouse          bool        `arg:"--enable-mouse" default:"true" help:"Enable mouse support"`
	FilterAutoTx         bool        `arg:"--filter-auto" help:"Filter automatic transitions"`
	FilterAutoCanceledTx bool        `arg:"--filter-auto-canceled" help:"Filter automatic canceled transitions"`
	FilterCanceledTx     bool        `arg:"--filter-canceled" help:"Filter canceled transitions"`
	FilterChecks         bool        `arg:"--filter-checks" help:"Filter check (read-only) transitions"`
	FilterDisconn        bool        `arg:"--filter-disconn" help:"Filter disconnected clients"`
	FilterEmptyTx        bool        `arg:"--filter-empty" help:"Filter empty transitions"`
	FilterGroup          bool        `arg:"--filter-group" default:"true" help:"Filter transitions by a selected group"`
	FilterHealthTx       bool        `arg:"--filter-health" help:"Filter health-check transitions"`
	FilterLogLevel       am.LogLevel `arg:"--filter-log-level" default:"2" help:"Filter transitions up to this log level, 0-5 (silent-everything)"`
	FilterQueuedTx       bool        `arg:"--filter-queued" help:"Filter queued transitions"`

	OutputClients   bool                 `arg:"--output-clients" help:"Write a detailed client list into am-dbg-clients.txt inside --dir"`
	OutputDiagrams  ParamsOutputDiagrams `arg:"--output-diagrams" help:"Level of details for machine graph diagrams (svg, d2, mermaid) in --dir (0 off, 1-3 on) (EXPERIMENTAL)"`
	OutputDiagGroup ParamsOutDiagGroup   `arg:"--output-diag-group" help:"Only show states from the selected group (valid: hide, skip)" default:"hide"`
	OutputDiagTx    ParamsOutDiagTx      `arg:"--output-diag-tx" help:"Dim states and rels unrelated to a transition (valid: called, changed, touched, relations)" default:"relations"`
	OutputLog       bool                 `arg:"--output-log" help:"Write the current log buffer to log.txt inside --dir"`
	OutputTx        bool                 `arg:"--output-tx" default:"true" help:"Write the current transition with steps into tx.md / d2 / mermaid / txt inside --dir (EXPERIMENTAL)"`

	// ui

	UiSsh bool `arg:"--ui-ssh" help:"Enable SSH headless mode on port --listen-addr +2 (EXPERIMENTAL)"`
	UiWeb bool `arg:"--ui-web" default:"true" help:"Start a web server for --dir and diagrams on --listen-addr +1 (EXPERIMENTAL)"`

	// view

	PrintVersion  bool                `arg:"--version" help:"Print version and exit"`
	StartupView   string              `arg:"-v,--view" default:"tree-log" help:"Initial view (tree-log, tree-matrix, matrix)"`
	ViewLogWrap   bool                `arg:"--view-log-wrap" help:"Wrap log lines"`
	ViewNarrow    bool                `arg:"--view-narrow" help:"Force a narrow view, independently of the viewport size"`
	ViewRain      bool                `arg:"--view-rain" help:"Show the rain view"`
	ViewReader    bool                `arg:"-r,--view-reader" help:"Show the log reader view"`
	TailMode      bool                `arg:"--view-tail" default:"true" help:"Start from the latest tx"`
	ViewTheme     string              `arg:"--view-theme" default:"dark" help:"Color theme (dark, light)"`
	ViewTimelines ParamsViewTimelines `arg:"--view-timelines" default:"2" help:"Number of timelines to show (0-2)"`

	// optimization

	LogOpsTtl time.Duration `arg:"--log-ops-ttl" default:"24h" help:"Max time to live for logs level LogOps"`
	MaxMemMb  int           `arg:"--max-mem" default:"1000" help:"Max memory usage (in MB) to flush old transitions"`

	// self-dbg

	DebugAddr    string `arg:"--dbg-am-dbg-addr" help:"Debug this instance of am-dbg with another one"`
	RaceDetector bool   `arg:"--dbg-go-race" help:"Go race detector is enabled"`
	// Id is the ID of this asyncmachine (for debugging).
	Id string `arg:"--dbg-id" default:"am-dbg" help:"ID of this instance"`
	// LogLevel is the log level of this instance (for debugging).
	LogLevel am.LogLevel `arg:"--dbg-log-level" default:"0" help:"Log level produced by this instance, 0-5 (silent-everything)"`
	DbgOtel  bool        `arg:"--dbg-otel" help:"Enable OpenTelemetry tracing for this instance"`
	ProfSrv  string      `arg:"--dbg-prof-srv" help:"Start pprof server"`

	// internal params

	// AddrHttp is the computed HTTP server address (ListenAddr port+1).
	AddrHttp string `arg:"-"`
	// AddrRpc is the RPC listen address (same as ListenAddr).
	AddrRpc string `arg:"-"`
	// AddrSsh is the computed SSH server address (ListenAddr port+2).
	AddrSsh string `arg:"-"`
	// DbgLogger is the file logger for this instance.
	DbgLogger *log.Logger `arg:"-"`
	// Filters is the active set of transition filters.
	Filters *Filters `arg:"-"`
	// Print is the output function for status messages
	Print   func(txt string, args ...any) `arg:"-"`
	ProfCpu bool                          `arg:"-"`
	ProfMem bool                          `arg:"-"`
	// Screen is an optional tcell screen override (tests, SSH).
	Screen tcell.Screen `arg:"-"`
	// Version is the computed build version string.
	Version string `arg:"-"`
}

// -----

// ParamsOutputDiagrams is an enum for --output-diagrams
type ParamsOutputDiagrams enum.Member[int]

// 0, 1, 2, 3
var (
	ParamsOutputDiagramsNone  = ParamsOutputDiagrams{0}
	ParamsOutputDiagramsOne   = ParamsOutputDiagrams{1}
	ParamsOutputDiagramsTwo   = ParamsOutputDiagrams{2}
	ParamsOutputDiagramsThree = ParamsOutputDiagrams{3}

	ParamsOutputDiagramsEnum = enum.New(
		ParamsOutputDiagramsNone,
		ParamsOutputDiagramsOne,
		ParamsOutputDiagramsTwo,
		ParamsOutputDiagramsThree,
	)
)

func (p *ParamsOutputDiagrams) UnmarshalText(b []byte) error {
	value, err := strconv.Atoi(string(b))
	if err != nil {
		return fmt.Errorf("invalid value: %s", b)
	}
	res := ParamsOutputDiagramsEnum.Parse(value)
	if res == nil {
		return fmt.Errorf("invalid value: %s", b)
	}
	*p = *res
	return nil
}

// -----

// ParamsOutDiagTx is an enum for --output-diagrams-group
type ParamsOutDiagTx enum.Member[string]

// "", "called", "changed", "touched"
var (
	ParamsOutDiagTxNone      = ParamsOutDiagTx{""}
	ParamsOutDiagTxCalled    = ParamsOutDiagTx{"called"}
	ParamsOutDiagTxMutated   = ParamsOutDiagTx{"mutated"}
	ParamsOutDiagTxTouched   = ParamsOutDiagTx{"touched"}
	ParamsOutDiagTxRelations = ParamsOutDiagTx{"relations"}

	ParamsOutDiagTxEnum = enum.New(ParamsOutDiagTxNone, ParamsOutDiagTxCalled,
		ParamsOutDiagTxMutated, ParamsOutDiagTxTouched, ParamsOutDiagTxRelations)
)

func (p *ParamsOutDiagTx) UnmarshalText(b []byte) error {
	res := ParamsOutDiagTxEnum.Parse(string(b))
	if res == nil {
		return fmt.Errorf("invalid value: %s", b)
	}
	*p = *res
	return nil
}

// -----

// ParamsOutDiagGroup is an enum for --output-diagrams-group
type ParamsOutDiagGroup enum.Member[string]

// "", "hide", "skip"
var (
	ParamsOutDiagGroupNone = ParamsOutDiagGroup{""}
	ParamsOutDiagGroupHide = ParamsOutDiagGroup{"hide"}
	ParamsOutDiagGroupSkip = ParamsOutDiagGroup{"skip"}

	ParamsOutDiagGroupEnum = enum.New(ParamsOutDiagGroupNone,
		ParamsOutDiagGroupHide, ParamsOutDiagGroupSkip)
)

func (p *ParamsOutDiagGroup) UnmarshalText(b []byte) error {
	res := ParamsOutDiagGroupEnum.Parse(string(b))
	if res == nil {
		return fmt.Errorf("invalid value: %s", b)
	}
	*p = *res
	return nil
}

// -----

// ParamsViewTimelines is an enum for --view-timelines
type ParamsViewTimelines enum.Member[int]

// 0, 1, 2
var (
	ParamsViewTimelinesNone = ParamsViewTimelines{0}
	ParamsViewTimelinesOne  = ParamsViewTimelines{1}
	ParamsViewTimelinesTwo  = ParamsViewTimelines{2}

	ParamsViewTimelinesEnum = enum.New(
		ParamsViewTimelinesNone,
		ParamsViewTimelinesOne,
		ParamsViewTimelinesTwo,
	)
)

func (p *ParamsViewTimelines) UnmarshalText(b []byte) error {
	value, err := strconv.Atoi(string(b))
	if err != nil {
		return fmt.Errorf("invalid value: %s", b)
	}
	res := ParamsViewTimelinesEnum.Parse(value)
	if res == nil {
		return fmt.Errorf("invalid value: %s", b)
	}
	*p = *res
	return nil
}

// ///// ///// /////

// ///// FILTERS

// ///// ///// /////

// Filters controls which transitions and log entries are shown.
type Filters struct {
	SkipCanceledTx     bool
	SkipAutoTx         bool
	SkipAutoCanceledTx bool
	SkipEmptyTx        bool
	SkipHealthTx       bool
	SkipQueuedTx       bool
	SkipOutGroup       bool
	SkipChecks         bool
	LogLevel           am.LogLevel
}

func (f *Filters) Equal(filters *Filters) bool {
	if filters == nil || f == nil {
		return false
	}

	return f.SkipCanceledTx == filters.SkipCanceledTx &&
		f.SkipAutoTx == filters.SkipAutoTx &&
		f.SkipAutoCanceledTx == filters.SkipAutoCanceledTx &&
		f.SkipEmptyTx == filters.SkipEmptyTx &&
		f.SkipHealthTx == filters.SkipHealthTx &&
		f.SkipQueuedTx == filters.SkipQueuedTx &&
		f.SkipOutGroup == filters.SkipOutGroup &&
		f.SkipChecks == filters.SkipChecks &&
		f.LogLevel == filters.LogLevel
}

// ///// ///// /////

// ///// PROFILING

// ///// ///// /////

// GetLogger returns a file logger, according to params.
func GetLogger(params *Params, dir string) *log.Logger {
	// TODO slog

	if params.LogLevel <= 0 {
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

type MachAddress struct {
	MachId    string
	TxId      string
	MachTime  uint64
	HumanTime time.Time
	Step      int

	// TODO
	QueueTick uint64

	// TODO support as GET param

	Group    string
	State    string
	Relation string
	RelState string
}

type MachTime struct {
	Id   string
	Time uint64
}

func (a *MachAddress) Clone() *MachAddress {
	return &MachAddress{
		MachId:   a.MachId,
		TxId:     a.TxId,
		Step:     a.Step,
		MachTime: a.MachTime,
	}
}

func (a *MachAddress) String() string {
	if a == nil {
		return ""
	}

	if a.TxId != "" {
		u := fmt.Sprintf("mach://%s/%s", a.MachId, a.TxId)
		if a.Step != 0 {
			u += fmt.Sprintf("/%d", a.Step)
		}
		if a.MachTime != 0 {
			u += fmt.Sprintf("/t%d", a.MachTime)
		}
		// TODO queue tick as q123

		return u
	}
	if a.MachTime != 0 {
		return fmt.Sprintf("mach://%s/t%d", a.MachId, a.MachTime)
	}
	if !a.HumanTime.IsZero() {
		return fmt.Sprintf("mach://%s/%s", a.MachId, a.HumanTime)
	}

	return fmt.Sprintf("mach://%s", a.MachId)
}

func ParseMachUrl(u string) (*MachAddress, error) {
	// TODO merge parsing with addr bar

	up, err := url.Parse(u)
	if err != nil {
		return nil, err
	} else if up.Host == "" {
		return nil, fmt.Errorf("host missing in: %s", u)
	}

	addr := &MachAddress{
		MachId: up.Host,
	}
	p := strings.Split(up.Path, "/")
	if len(p) > 1 {
		addr.TxId = p[1]
	}
	if len(p) > 2 {
		if s, err := strconv.Atoi(p[2]); err == nil {
			addr.Step = s
		}
	}

	return addr, nil
}

type MsgTxParsed struct {
	StatesAdded   []int
	StatesRemoved []int
	StatesTouched []int
	// TimeSum is machine time after this transition.
	TimeSum       uint64
	TimeDiff      uint64
	ReaderEntries []*LogReaderEntryPtr
	// Transitions which reported this one as their source
	Forks       []MachAddress
	ForksLabels []string
	// QueueTick when this tx should be executed
	ResultTick uint64
}

type MsgSchemaParsed struct {
	Groups      map[string]am.S
	GroupsOrder []string
}

type LogReaderEntry struct {
	Kind LogReaderKind
	// states are empty for logReaderWhenQueue
	States []int
	// CreatedAt is machine time when this entry was created
	CreatedAt uint64
	// ClosedAt is human time when this entry was closed, so it can be disposed.
	ClosedAt time.Time

	// per-type fields

	// Pipe is for logReaderPipeIn, logReaderPipeOut
	Pipe am.MutationType
	// Mach is for logReaderPipeIn, logReaderPipeOut, logReaderMention
	Mach string
	// Ticks is for logReaderWhenTime only
	Ticks am.Time
	// Args is for logReaderWhenArgs only
	Args string
	// QueueTick is for logReaderWhenQueue only
	QueueTick int
}

type LogReaderEntryPtr struct {
	TxId     string
	EntryIdx int
}

type LogReaderKind int

const (
	LogReaderCtx LogReaderKind = iota + 1
	LogReaderWhen
	LogReaderWhenNot
	LogReaderWhenTime
	LogReaderWhenArgs
	LogReaderWhenQueue
	LogReaderPipeIn
	LogReaderPipeOut
	// TODO mentions of machine IDs
	// logReaderMention
)

// ///// ///// /////

// ///// TOOLS

// ///// ///// /////

// ToolName is a debugger's toolbar button.
type ToolName enum.Member[string]

var (
	ToolFilterCanceledTx = ToolName{"skip-canceled"}
	ToolFilterQueuedTx   = ToolName{"skip-queued"}
	ToolFilterAutoTx     = ToolName{"skip-auto"}
	ToolFilterEmptyTx    = ToolName{"skip-empty"}
	ToolFilterHealth     = ToolName{"skip-health"}
	ToolFilterOutGroup   = ToolName{"skip-outgroup"}
	ToolFilterChecks     = ToolName{"skip-checks"}
	ToolFilterDisconn    = ToolName{"skip-disconn"}
	ToolLogTimestamps    = ToolName{"hide-timestamps"}
	ToolFilterTraces     = ToolName{"hide-traces"}
	ToolNarrowLayout     = ToolName{"narrow-layout"}
	ToolLog              = ToolName{"log"}
	ToolDiagrams         = ToolName{"diagrams"}
	ToolDiagramsTx       = ToolName{"diag-tx"}
	ToolDiagramsGroup    = ToolName{"diag-group"}
	ToolTimelines        = ToolName{"timelines"}
	ToolReader           = ToolName{"reader"}
	ToolRain             = ToolName{"rain"}
	ToolLogWrap          = ToolName{"log-wrap"}
	ToolWeb              = ToolName{"web"}
	ToolHelp             = ToolName{"help"}
	ToolPlay             = ToolName{"play"}
	ToolTail             = ToolName{"tail"}
	ToolPrev             = ToolName{"prev"}
	ToolNext             = ToolName{"next"}
	ToolJumpNext         = ToolName{"jump-next"}
	ToolJumpPrev         = ToolName{"jump-prev"}
	ToolFirst            = ToolName{"first"}
	ToolLast             = ToolName{"last"}
	ToolExpand           = ToolName{"expand"}
	ToolMatrix           = ToolName{"matrix"}
	ToolExport           = ToolName{"export"}
	ToolNextStep         = ToolName{"next-step"}
	ToolPrevStep         = ToolName{"prev-step"}

	ToolNames = enum.New(
		ToolFilterCanceledTx,
		ToolFilterQueuedTx,
		ToolFilterAutoTx,
		ToolFilterEmptyTx,
		ToolFilterHealth,
		ToolFilterOutGroup,
		ToolFilterChecks,
		ToolFilterDisconn,
		ToolLogTimestamps,
		ToolFilterTraces,
		ToolNarrowLayout,
		ToolLog,
		ToolDiagrams,
		ToolDiagramsTx,
		ToolDiagramsGroup,
		ToolTimelines,
		ToolReader,
		ToolRain,
		ToolLogWrap,
		ToolWeb,
		ToolHelp,
		ToolPlay,
		ToolTail,
		ToolPrev,
		ToolNext,
		ToolJumpNext,
		ToolJumpPrev,
		ToolFirst,
		ToolLast,
		ToolExpand,
		ToolMatrix,
		ToolExport,
		ToolNextStep,
		ToolPrevStep,
	)
)

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "dbg"

// A is a struct for debugger arguments. It's a typesafe alternative to [am.A].
type A struct {

	// client identification

	ClientId string `log:"client_id"`
	TxId     string `log:"tx_id"`
	ConnId   string `log:"conn_id"`
	ConnIds  []string

	// cursor positioning (1-based)

	Cursor1     int `log:"cursor1"`
	CursorStep1 int `log:"cursor_step1"`
	// TODO merge with Cursor1
	CursorTx1 int `log:"cursor_tx1"`

	// navigation

	Amount int  `log:"amount" json:",string"`
	Fwd    bool `log:"fwd" json:",string"`

	// state / group selection

	State string `log:"state"`
	Group string

	// tool toggling

	ToolName ToolName `log:"tool_name"`

	// client init

	Id string `log:"id"`

	// log building

	LogRebuildEnd int `json:",string"`
	LogBuffer     string
	LogLevel      am.LogLevel `json:",string"`

	// cursor history / filtering

	SkipHistory bool
	TrimHistory bool
	FilterBack  bool

	// filter / update flags

	FilterTxs       bool
	BuildClientList bool
	Immediate       bool
	FromConnected   bool
	FromPlaying     bool
	MouseFocus      bool

	// display / text

	Text string `log:"text"`
	Uri  string `log:"uri"`
	Addr string `log:"addr"`

	// matrix rain

	Row       int `json:",string"`
	Column    int `json:",string"`
	CurrTxRow int `json:",string"`

	// non-rpc / complex types

	// FocusPrimitive is the UI element receiving focus.
	FocusPrimitive cview.Primitive
	// DiagramCache is parsed HTML for diagram generation.
	DiagramCache *goquery.Document
	// DiagramName is the name of the diagram being generated.
	DiagramName string
	// HttpRequest is for web server route handlers.
	HttpRequest *http.Request
	// HttpResponseWriter is for web server route handlers.
	HttpResponseWriter http.ResponseWriter
	// DoneChan signals completion of a web handler.
	DoneChan chan struct{}
	// WebSocketConn is for WebSocket handlers.
	WebSocketConn *websocket.Conn
	// MsgStruct is the schema message from am-dbg protocol.
	MsgStruct *dbg.DbgMsgStruct
	// MsgsTx is a batch of transaction messages.
	MsgsTx []*dbg.DbgMsgTx
	// Ctx is a context passed into handlers.
	Ctx context.Context
}

// ARpc is a subset of A, that can be passed over RPC.
type ARpc struct {

	// client identification

	ClientId string `log:"client_id"`
	TxId     string `log:"tx_id"`
	ConnId   string `log:"conn_id"`
	ConnIds  []string

	// cursor positioning (1-based)

	Cursor1     int `log:"cursor1"`
	CursorStep1 int `log:"cursor_step1"`
	// TODO merge with Cursor1
	CursorTx1 int `log:"cursor_tx1"`

	// navigation

	Amount int  `log:"amount" json:",string"`
	Fwd    bool `log:"fwd" json:",string"`

	// state / group selection

	State string `log:"state"`
	Group string

	// tool toggling

	ToolName ToolName `log:"tool_name"`

	// client init

	Id string `log:"id"`

	// log building

	LogRebuildEnd int `json:",string"`
	LogBuffer     string
	LogLevel      am.LogLevel `json:",string"`

	// cursor history / filtering

	SkipHistory bool
	TrimHistory bool
	FilterBack  bool

	// filter / update flags

	FilterTxs       bool
	BuildClientList bool
	Immediate       bool
	FromConnected   bool
	FromPlaying     bool
	MouseFocus      bool

	// display / text

	Text string `log:"text"`
	Uri  string `log:"uri"`
	Addr string `log:"addr"`

	// matrix rain

	Row       int `json:",string"`
	Column    int `json:",string"`
	CurrTxRow int `json:",string"`
}

var StateCalls = []am.CallSignature{
	{States: am.S{ss.StateNameSelected}, Needed: []string{"State"}},
	{States: am.S{ss.Redraw}, Args: []string{"Immediate"}},
	{States: am.S{ss.Fwd}, Args: []string{"Amount"}},
	{States: am.S{ss.Back}, Args: []string{"Amount"}},
	{States: am.S{ss.ConnectEvent}, Needed: []string{"MsgStruct", "ConnId"}},
	{States: am.S{ss.DisconnectEvent}, Needed: []string{"ConnId"}},
	{
		States: am.S{ss.ClientMsg},
		Needed: []string{"MsgsTx", "ConnIds"},
	},
	{
		States: am.S{ss.RemoveClient},
		Needed: []string{"ClientId"},
	},
	{
		States: am.S{ss.SetGroup},
		Needed: []string{"Group"},
	},
	{
		States: am.S{ss.SelectingClient},
		Needed: []string{"ClientId"},
		Args:   []string{"Group", "FromConnected"},
	},
	{
		States: am.S{ss.ClientSelected},
		Args:   []string{"Ctx", "FromConnected", "FromPlaying"},
	},
	{
		States: am.S{ss.ScrollToTx},
		Args:   []string{"CursorTx1", "TxId", "CursorStep1", "TrimHistory"},
	},
	{
		States: am.S{ss.ScrollToStep},
		Needed: []string{"CursorStep1"},
	},
	{
		States: am.S{ss.ToggleTool},
		Needed: []string{"ToolName"},
		Values: map[string][]string{
			"ToolName": ToolNames.Values(),
		},
	},
	{
		States: am.S{ss.ToolToggled},
		Args:   []string{"FilterTxs", "BuildClientList"},
	},
	{
		States: am.S{ss.SwitchingClientTx},
		Needed: []string{"ClientId", "CursorTx1"},
	},
	{
		States: am.S{ss.ScrollToMutTx},
		Needed: []string{"State"},
		Args:   []string{"Fwd"},
	},
	{
		States: am.S{ss.AfterFocus},
		Needed: []string{"FocusPrimitive"},
		Args:   []string{"MouseFocus"},
	},
	{
		States: am.S{ss.Resized},
		Args:   []string{"LogRebuildEnd"},
	},
	{
		States: am.S{ss.WebReq},
		Needed: []string{"HttpRequest", "HttpResponseWriter", "DoneChan"},
		Args:   []string{"Uri", "Addr"},
	},
	{
		States: am.S{ss.WebSocketDiag},
		Needed: []string{"WebSocketConn", "HttpRequest", "HttpResponseWriter",
			"DoneChan"},
		Args: []string{"Addr"},
	},
	{
		States: am.S{ss.MatrixRainSelected},
		Needed: []string{"Row", "Column", "CurrTxRow"},
	},
}

// ParseArgs extracts A from [am.Event.Args][APrefix].
func ParseArgs(args am.A) *A {
	if r, ok := args[APrefix].(*ARpc); ok {
		return amhelp.ArgsToArgs(r, &A{})
	} else if r, ok := args[APrefix].(ARpc); ok {
		return amhelp.ArgsToArgs(&r, &A{})
	}
	if a, _ := args[APrefix].(*A); a != nil {
		return a
	}
	return &A{}
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	return am.A{APrefix: args}
}

// LogArgs is an args logger for A.
func LogArgs(args am.A) map[string]string {
	a := ParseArgs(args)
	if a == nil {
		return nil
	}
	return amhelp.ArgsToLogMap(a, 0)
}

// ParseRpc parses [am.A] to *ARpc namespaced in [am.A]. Useful for REPLs.
func ParseRpc(args am.A) am.A {
	ret := am.A{APrefix: &ARpc{}}
	jsonArgs, err := json.Marshal(args)
	if err == nil {
		_ = json.Unmarshal(jsonArgs, ret[APrefix])
	}

	return ret
}
