package debugger

import (
	"log"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"

	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

const (
	// TODO light mode
	colorActive     = tcell.ColorOlive
	colorActive2    = tcell.ColorGreenYellow
	colorInactive   = tcell.ColorLimeGreen
	colorHighlight  = tcell.ColorDarkSlateGray
	colorHighlight2 = tcell.ColorDimGray
	colorHighlight3 = tcell.Color233
	colorErr        = tcell.ColorRed
	// TODO customize
	playInterval = 500 * time.Millisecond
	// TODO add param --max-clients
	maxClients            = 5000
	timeFormat            = "15:04:05.000000000"
	fastJumpAmount        = 50
	arrowThrottleMs       = 200
	logUpdateDebounce     = 300 * time.Millisecond
	sidebarUpdateDebounce = time.Second
	searchAsTypeWindow    = 1500 * time.Millisecond
	heartbeatInterval     = 1 * time.Minute
	// heartbeatInterval = 5 * time.Second
	// msgMaxAge             = 0

	// maxMemMb = 100
	maxMemMb        = 50
	msgMaxThreshold = 300
	// max txs queued for scrolling the timelines
	scrollTxThrottle = 3
)

var colorDefault = cview.Styles.PrimaryTextColor

const (
	// row 1
	toolFilterCanceledTx ToolName = "skip-canceled"
	toolFilterQueuedTx   ToolName = "skip-queued"
	toolFilterAutoTx     ToolName = "skip-auto"
	toolFilterEmptyTx    ToolName = "skip-empty"
	toolFilterHealth     ToolName = "skip-health"
	toolFilterOutGroup   ToolName = "skip-outgroup"
	toolFilterChecks     ToolName = "skip-checks"
	ToolLogTimestamps    ToolName = "hide-timestamps"
	ToolFilterTraces     ToolName = "hide-traces"
	toolLog              ToolName = "log"
	toolDiagrams         ToolName = "diagrams"
	toolTimelines        ToolName = "timelines"
	// toolLog0              ToolName = "log-0"
	// toolLog1              ToolName = "log-1"
	// toolLog2              ToolName = "log-2"
	// toolLog3              ToolName = "log-3"
	// toolLog4              ToolName = "log-4"
	toolReader ToolName = "reader"
	toolRain   ToolName = "rain"

	// row 2

	toolHelp     ToolName = "help"
	toolPlay     ToolName = "play"
	toolTail     ToolName = "tail"
	toolPrev     ToolName = "prev"
	toolNext     ToolName = "next"
	toolJumpNext ToolName = "jump-next"
	toolJumpPrev ToolName = "jump-prev"
	toolFirst    ToolName = "first"
	toolLast     ToolName = "last"
	toolExpand   ToolName = "expand"
	toolMatrix   ToolName = "matrix"
	toolExport   ToolName = "export"
	toolNextStep ToolName = "next-step"
	toolPrevStep ToolName = "prev-step"
)

type Opts struct {
	MachUrl         string
	SelectConnected bool
	CleanOnConnect  bool
	EnableMouse     bool
	ShowReader      bool
	// MachAddress to listen on
	AddrRpc  string
	AddrHttp string
	// Log level of the debugger's machine
	DbgLogLevel am.LogLevel
	// Go race detector is enabled
	DbgRace   bool
	DbgLogger *log.Logger
	// Filters for the transitions and logging
	Filters *OptsFilters
	// Timelines is the number of timelines to show (0-2).
	Timelines int
	// File path to import (brotli)
	ImportData string
	// File to dump client list into.
	OutputClients bool
	// Root dir for output files
	OutputDir string
	// OutputDiagrams is the details level of the current machine's diagram (0-3).
	// 0 - off, 3 - most detailed
	OutputDiagrams int
	// Screen overload for tests & ssh
	Screen tcell.Screen
	// Debugger's ID
	Id string
	// version of this instance
	Version         string
	MaxMemMb        int
	Log2Ttl         time.Duration
	ViewNarrow      bool
	ViewRain        bool
	TailMode        bool
	OutputTx        bool
	EnableClipboard bool
}

type OptsFilters struct {
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

func (f *OptsFilters) Equal(filters *OptsFilters) bool {
	if filters == nil {
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

type ToolName string

type Client struct {
	*server.Client

	CursorTx1           int
	ReaderCollapsed     bool
	CursorStep1         int
	SelectedState       string
	SelectedReaderEntry *types.LogReaderEntryPtr
	// extracted log entries per tx ID
	// TODO GC when all entries are closedAt and the first client's tx is later
	//  than the latest closedAt; whole tx needs to be disposed at the same time
	LogReader map[string][]*types.LogReaderEntry

	logRenderedCursor1    int
	logRenderedLevel      am.LogLevel
	logRenderedFilters    *OptsFilters
	logRenderedTimestamps bool
	logReaderMx           sync.Mutex
}

func newClient(id, connId, schemaHash string, data *server.Exportable) *Client {
	c := &Client{
		Client: &server.Client{
			Id:         id,
			ConnId:     connId,
			SchemaHash: schemaHash,
			Exportable: data,
		},
		LogReader: make(map[string][]*types.LogReaderEntry),
	}
	c.ParseSchema()

	return c
}

func (c *Client) AddReaderEntry(txId string, entry *types.LogReaderEntry) int {
	c.logReaderMx.Lock()
	defer c.logReaderMx.Unlock()

	c.LogReader[txId] = append(c.LogReader[txId], entry)

	return len(c.LogReader[txId]) - 1
}

func (c *Client) GetReaderEntry(txId string, idx int) *types.LogReaderEntry {
	c.logReaderMx.Lock()
	defer c.logReaderMx.Unlock()

	ptrTx, ok := c.LogReader[txId]
	if !ok || idx >= len(ptrTx) {
		return nil
	}

	return ptrTx[idx]
}
