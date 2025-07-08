package debugger

import (
	"fmt"
	"log"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
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
	healthcheckInterval   = 1 * time.Minute
	// msgMaxAge             = 0

	// maxMemMb = 100
	maxMemMb        = 50
	msgMaxThreshold = 300
)

var colorDefault = cview.Styles.PrimaryTextColor

const (
	// row 1
	toolFilterCanceledTx  ToolName = "skip-canceled"
	toolFilterAutoTx      ToolName = "skip-auto"
	toolFilterEmptyTx     ToolName = "skip-empty"
	toolFilterHealthcheck ToolName = "skip-healthcheck"
	// TODO rename to timestamps
	ToolFilterSummaries ToolName = "hide-summaries"
	ToolFilterTraces    ToolName = "hide-traces"
	toolLog             ToolName = "log"
	toolDiagrams        ToolName = "diagrams"
	toolTimelines       ToolName = "timelines"
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

type MsgTxParsed struct {
	StatesAdded   []int
	StatesRemoved []int
	StatesTouched []int
	// TimeSum is machine time after this transition.
	TimeSum       uint64
	ReaderEntries []*logReaderEntryPtr
	// Transitionss which reported this one as their source
	Forks       []MachAddress
	ForksLabels []string
}

type Opts struct {
	MachUrl         string
	SelectConnected bool
	CleanOnConnect  bool
	EnableMouse     bool
	ShowReader      bool
	// MachAddress to listen on
	ServerAddr string
	// Log level of the debugger's machine
	DbgLogLevel am.LogLevel
	DbgLogger   *log.Logger
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
	ID string
	// version of this instance
	Version    string
	MaxMemMb   int
	Log2Ttl    time.Duration
	ViewNarrow bool
	ViewRain   bool
	TailMode   bool
	OutputTx   bool
}

type OptsFilters struct {
	SkipCanceledTx  bool
	SkipAutoTx      bool
	SkipEmptyTx     bool
	SkipHealthcheck bool
	LogLevel        am.LogLevel
}

type ToolName string

type Exportable struct {
	// TODO refac MsgStruct
	MsgStruct *telemetry.DbgMsgStruct
	MsgTxs    []*telemetry.DbgMsgTx
}

type MachAddress struct {
	MachId    string
	TxId      string
	MachTime  uint64
	HumanTime time.Time
	// TODO support step
	Step int
}

func (ma *MachAddress) Clone() *MachAddress {
	return &MachAddress{
		MachId:   ma.MachId,
		TxId:     ma.TxId,
		Step:     ma.Step,
		MachTime: ma.MachTime,
	}
}

func (a *MachAddress) String() string {
	if a.TxId != "" {
		u := fmt.Sprintf("mach://%s/%s", a.MachId, a.TxId)
		if a.Step != 0 {
			u += fmt.Sprintf("/%d", a.Step)
		}
		if a.MachTime != 0 {
			u += fmt.Sprintf("/t%d", a.MachTime)
		}

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

type Client struct {
	// bits which get saved into the go file
	Exportable
	// current transition, 1-based, mirrors the slider (eg 1 means tx.ID == 0)
	// TODO atomic
	CursorTx1 int
	// current step, 1-based, mirrors the slider
	// TODO atomic
	CursorStep1         int
	SelectedState       string
	SelectedReaderEntry *logReaderEntryPtr
	ReaderCollapsed     bool

	txCache    map[string]int
	txCacheMx  sync.Mutex
	id         string
	connId     string
	schemaHash string
	connected  atomic.Bool
	// processed
	msgTxsParsed []*MsgTxParsed
	// processed list of filtered tx indexes
	MsgTxsFiltered []int
	// cache of processed log entries
	logMsgs [][]*am.LogEntry
	// extracted log entries per tx ID
	// TODO GC when all entries are closedAt and the first client's tx is later
	//  than the latest closedAt; whole tx needs to be disposed at the same time
	logReader   map[string][]*logReaderEntry
	logReaderMx sync.Mutex
	// indexes of txs with errors, desc order for bisects
	// TOOD refresh on GC
	errors   []int
	mTimeSum uint64
}

func (c *Client) lastActive() time.Time {
	if len(c.MsgTxs) == 0 {
		return time.Time{}
	}

	return *c.MsgTxs[len(c.MsgTxs)-1].Time
}

func (c *Client) hadErrSinceTx(tx, distance int) bool {
	if slices.Contains(c.errors, tx) {
		return true
	}

	// see TestHadErrSince
	index := sort.Search(len(c.errors), func(i int) bool {
		return c.errors[i] < tx
	})

	if index >= len(c.errors) {
		return false
	}

	return tx-c.errors[index] < distance
}

func (c *Client) lastTxTill(hTime time.Time) int {
	l := len(c.MsgTxs)
	if l == 0 {
		return -1
	}

	i := sort.Search(l, func(i int) bool {
		t := c.MsgTxs[i].Time
		return t.After(hTime) || t.Equal(hTime)
	})

	if i == l {
		i = l - 1
	}
	tx := c.MsgTxs[i]

	// pick the closer one
	if i > 0 && tx.Time.Sub(hTime) > hTime.Sub(*c.MsgTxs[i-1].Time) {
		return i
	} else {
		return i
	}
}

func (c *Client) addReaderEntry(txId string, entry *logReaderEntry) int {
	c.logReaderMx.Lock()
	defer c.logReaderMx.Unlock()

	c.logReader[txId] = append(c.logReader[txId], entry)

	return len(c.logReader[txId]) - 1
}

func (c *Client) getReaderEntry(txId string, idx int) *logReaderEntry {
	c.logReaderMx.Lock()
	defer c.logReaderMx.Unlock()

	ptrTx, ok := c.logReader[txId]
	if !ok || idx >= len(ptrTx) {
		return nil
	}

	return ptrTx[idx]
}

func (c *Client) statesToIndexes(states am.S) []int {
	return amhelp.StatesToIndexes(c.MsgStruct.StatesIndex, states)
}

func (c *Client) indexesToStates(indexes []int) am.S {
	return amhelp.IndexesToStates(c.MsgStruct.StatesIndex, indexes)
}

// txIndex returns the index of transition ID [id] or -1 if not found.
func (c *Client) txIndex(id string) int {
	c.txCacheMx.Lock()
	defer c.txCacheMx.Unlock()

	if c.txCache == nil {
		c.txCache = make(map[string]int)
	}
	if idx, ok := c.txCache[id]; ok {
		return idx
	}

	for i, tx := range c.MsgTxs {
		if tx.ID == id {
			c.txCache[id] = i
			return i
		}
	}

	return -1
}

func (c *Client) tx(idx int) *telemetry.DbgMsgTx {
	if idx < 0 || idx >= len(c.MsgTxs) {
		return nil
	}

	return c.MsgTxs[idx]
}

func (c *Client) txParsed(idx int) *MsgTxParsed {
	if idx < 0 || idx >= len(c.msgTxsParsed) {
		return nil
	}

	return c.msgTxsParsed[idx]
}

func (c *Client) txByMachTime(sum uint64) int {
	idx, ok := slices.BinarySearchFunc(c.msgTxsParsed,
		&MsgTxParsed{TimeSum: sum}, func(i, j *MsgTxParsed) int {
			if i.TimeSum < j.TimeSum {
				return -1
			} else if i.TimeSum > j.TimeSum {
				return 1
			}

			return 0
		})

	if !ok {
		return 0
	}

	return idx
}

func (c *Client) filterIndexByCursor1(cursor1 int) int {
	if cursor1 == 0 {
		return 0
	}
	return slices.Index(c.MsgTxsFiltered, cursor1-1)
}

// TODO
// func NewClient()
