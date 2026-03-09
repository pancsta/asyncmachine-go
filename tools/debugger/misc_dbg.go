package debugger

import (
	"errors"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/pancsta/cview"
	"github.com/pancsta/tcell-v2"
	"github.com/pancsta/tcell-v2/terminfo"

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
	toolFilterDisconn    ToolName = "skip-disconn"
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
	toolWeb    ToolName = "web"

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
	AddrSsh  string
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
	// Dump client list into a txt file.
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
	UiSsh           bool
	UiWeb           bool
	Print           func(txt string, args ...any)
	OutputLog       bool
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

	CursorTx1       int
	ReaderCollapsed bool
	CursorStep1     int
	SelectedState   string
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

// ///// ///// /////

// ///// SSH

// ///// ///// /////

func NewSessionScreen(s ssh.Session) (tcell.Screen, error) {
	pi, ch, ok := s.Pty()
	if !ok {
		return nil, errors.New("no pty requested")
	}
	ti, err := terminfo.LookupTerminfo(pi.Term)
	if err != nil {
		return nil, err
	}
	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(&tty{
		Session: s,
		size:    pi.Window,
		ch:      ch,
	}, ti)
	if err != nil {
		return nil, err
	}
	return screen, nil
}

type tty struct {
	ssh.Session
	size     ssh.Window
	ch       <-chan ssh.Window
	resizecb func()
	mu       sync.Mutex
}

func (t *tty) Start() error {
	go func() {
		for win := range t.ch {
			t.mu.Lock()
			t.size = win
			t.notifyResize()
			t.mu.Unlock()
		}
	}()
	return nil
}

func (t *tty) Stop() error {
	return nil
}

func (t *tty) Drain() error {
	return nil
}

func (t *tty) WindowSize() (window tcell.WindowSize, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return tcell.WindowSize{
		Width:  t.size.Width,
		Height: t.size.Height,
	}, nil
}

func (t *tty) NotifyResize(cb func()) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.resizecb = cb
}

func (t *tty) notifyResize() {
	if t.resizecb != nil {
		t.resizecb()
	}
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

// openURL opens the specified URL in the default browser of the user.
// https://gist.github.com/sevkin/9798d67b2cb9d07cb05f89f14ba682f8
func openURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd.exe"
		args = []string{
			"/c", "rundll32", "url.dll,FileProtocolHandler",
			strings.ReplaceAll(url, "&", "^&"),
		}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		if isWSL() {
			cmd = "cmd.exe"
			args = []string{"start", url}
		} else {
			cmd = "xdg-open"
			args = []string{url}
		}
	}

	e := exec.Command(cmd, args...)
	err := e.Start()
	if err != nil {
		return err
	}
	err = e.Wait()
	if err != nil {
		return err
	}

	return nil
}

// isWSL checks if the Go program is running inside Windows Subsystem for Linux
func isWSL() bool {
	releaseData, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(releaseData)), "microsoft")
}
