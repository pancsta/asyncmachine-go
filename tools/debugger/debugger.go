// Package debugger provides a TUI debugger with multi-client support. Runnable
// command can be found in tools/cmd/am-dbg.
package debugger

import (
	"bufio"
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"
	"golang.org/x/text/message"

	"github.com/pancsta/asyncmachine-go/internal/utils"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

const (
	// TODO light mode
	colorActive     = tcell.ColorOlive
	colorInactive   = tcell.ColorLimeGreen
	colorHighlight  = tcell.ColorDarkSlateGray
	colorHighlight2 = tcell.ColorDimGray
	colorHighlight3 = tcell.Color233
	colorErr        = tcell.ColorRed
	// TODO customize
	playInterval = 500 * time.Millisecond
	// TODO add param --max-clients
	maxClients            = 1000
	timeFormat            = "15:04:05.000000000"
	fastJumpAmount        = 50
	arrowThrottleMs       = 200
	logUpdateDebounce     = 300 * time.Millisecond
	sidebarUpdateDebounce = time.Second
	searchAsTypeWindow    = 1500 * time.Millisecond
	healthcheckInterval   = 5 * time.Second
	// msgMaxAge             = 0

	// maxMemMb = 100
	maxMemMb = 50
	msgMaxThreshold = 300
)

type Exportable struct {
	MsgStruct *telemetry.DbgMsgStruct
	MsgTxs    []*telemetry.DbgMsgTx
}

type Client struct {
	// bits which get saved into the go file
	Exportable
	// current transition, 1-based, mirrors the slider
	CursorTx int
	// current step, 1-based, mirrors the slider
	CursorStep          int
	SelectedState       string
	SelectedReaderEntry *logReaderEntryPtr
	ReaderCollapsed     bool

	id        string
	connID    string
	connected atomic.Bool
	// processed
	msgTxsParsed []*MsgTxParsed
	// processed list of filtered tx indexes
	msgTxsFiltered []int
	// cache of processed log entries
	logMsgs [][]*am.LogEntry
	// extracted log entries per tx ID
	// TODO GC when all entries are closedAt and the first client's tx is later
	//  than the latest closedAt; whole tx needs to be disposed at the same time
	logReader map[string][]*logReaderEntry
	// indexes of txs with errors, desc order for bisects
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
	if c.logReader == nil {
		c.logReader = make(map[string][]*logReaderEntry)
	}
	c.logReader[txId] = append(c.logReader[txId], entry)

	return len(c.logReader[txId]) - 1
}

func (c *Client) getReaderEntry(txId string, idx int) *logReaderEntry {
	if c.logReader == nil {
		c.logReader = make(map[string][]*logReaderEntry)
	}
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

type MsgTxParsed struct {
	StatesAdded   []int
	StatesRemoved []int
	StatesTouched []int
	// TimeSum is machine time.
	TimeSum       uint64
	ReaderEntries []*logReaderEntryPtr
}

type Opts struct {
	SelectConnected bool
	CleanOnConnect  bool
	EnableMouse     bool
	// Address to listen on
	ServerAddr string
	// Log level of the debugger's machine
	DbgLogLevel am.LogLevel
	DbgLogger   *log.Logger
	// Filters for the transitions and logging
	Filters *OptsFilters
	// File path to import (brotli)
	ImportData string
	// Screen overload for tests & ssh
	Screen tcell.Screen
	// Debugger's ID
	ID string
	// version of this instance
	Version string
	MaxMemMb int
	Log2Ttl  time.Duration
}

type OptsFilters struct {
	SkipCanceledTx bool
	SkipAutoTx     bool
	SkipEmptyTx    bool
	LogLevel       am.LogLevel
}

type FilterName string

const (
	filterCanceledTx FilterName = "skip-canceled"
	filterAutoTx     FilterName = "skip-auto"
	filterEmptyTx    FilterName = "skip-empty"
	FilterSummaries  FilterName = "hide-summaries"
	filterLog0       FilterName = "log-0"
	filterLog1       FilterName = "log-1"
	filterLog2       FilterName = "log-2"
	filterLog3       FilterName = "log-3"
	filterLog4       FilterName = "log-4"
)

type Debugger struct {
	*am.ExceptionHandler
	Mach *am.Machine

	Clients    map[string]*Client
	Opts       Opts
	LayoutRoot *cview.Panels
	Disposed   bool
	// selected client
	// TODO atomic, drop eval
	C   *Client
	App *cview.Application
	// printer for numbers
	P *message.Printer

	tree               *cview.TreeView
	treeRoot           *cview.TreeNode
	log                *cview.TextView
	timelineTxs        *cview.ProgressBar
	timelineSteps      *cview.ProgressBar
	focusable          []*cview.Box
	playTimer          *time.Ticker
	currTxBarRight     *cview.TextView
	currTxBarLeft      *cview.TextView
	nextTxBarLeft      *cview.TextView
	nextTxBarRight     *cview.TextView
	helpDialog         *cview.Flex
	keyBar             *cview.TextView
	clientList         *cview.List
	mainGrid           *cview.Grid
	logRebuildEnd      int
	lastScrolledTxTime time.Time
	repaintScheduled   atomic.Bool
	// update client list scheduled
	updateCLScheduled  atomic.Bool
	lastKeystroke      tcell.Key
	lastKeystrokeTime  time.Time
	updateLogScheduled atomic.Bool
	matrix             *cview.Table
	focusManager       *cview.FocusManager
	exportDialog       *cview.Modal
	contentPanels      *cview.Panels
	filtersBar         *cview.TextView
	focusedFilter      FilterName
	treeLogGrid        *cview.Grid
	treeMatrixGrid     *cview.Grid
	lastSelectedState  string
	// TODO should be a redraw, not before
	redrawCallback func()
	healthcheck    *time.Ticker
	logReader      *cview.TreeView
}

// New creates a new debugger instance and optionally import a data file.
func New(ctx context.Context, opts Opts) (*Debugger, error) {
	// init the debugger
	d := &Debugger{
		Clients: make(map[string]*Client),
	}

	d.Opts = opts

	// default opts
	if d.Opts.Filters == nil {
		d.Opts.Filters = &OptsFilters{
			LogLevel: am.LogChanges,
		}
	}
	if d.Opts.MaxMemMb == 0 {
		d.Opts.MaxMemMb = maxMemMb
	}
	if d.Opts.Log2Ttl == 0 {
		d.Opts.Log2Ttl = time.Hour
	}

	gob.Register(Exportable{})
	gob.Register(am.Relation(0))

	id := utils.RandID(0)
	if opts.ID != "" {
		id = opts.ID
	}
	mach, err := am.NewCommon(ctx, "", ss.States, ss.Names, d, nil, &am.Opts{
		DontLogID:      true,
		HandlerTimeout: 1 * time.Second,
		ID:             "td-" + id,
	})
	if err != nil {
		return nil, err
	}
	d.Mach = mach

	if d.Opts.Version == "" {
		d.Opts.Version = "(devel)"
	}

	// logging
	if opts.DbgLogger != nil {
		mach.SetLoggerSimple(opts.DbgLogger.Printf, opts.DbgLogLevel)
	} else {
		mach.SetLoggerSimple(log.Printf, opts.DbgLogLevel)
	}
	mach.SetLogArgs(am.NewArgsMapper([]string{
		"Machine.id", "conn_id", "Client.cursorTx", "amount", "Client.id",
		"state", "fwd", "filter",
	}, 20))

	// import data
	if opts.ImportData != "" {
		start := time.Now()
		mach.Log("Importing data from %s", opts.ImportData)
		d.ImportData(opts.ImportData)
		mach.Log("Imported data in %s", time.Since(start))
	}

	return d, nil
}

// ///// ///// /////

// ///// PUB

// ///// ///// /////

// Client returns the current Client. Thread safe via Eval().
func (d *Debugger) Client() *Client {
	var c *Client

	// SelectingClient locks d.C TODO timeout?
	<-d.Mach.WhenNot1(ss.SelectingClient, nil)

	d.Mach.Eval("Client", func() {
		c = d.C
	}, nil)

	return c
}

// NextTx returns the next transition. Thread safe via Eval().
func (d *Debugger) NextTx() *telemetry.DbgMsgTx {
	var tx *telemetry.DbgMsgTx

	// SelectingClient locks d.C
	<-d.Mach.WhenNot1(ss.SelectingClient, nil)

	d.Mach.Eval("NextTx", func() {
		tx = d.nextTx()
	}, nil)

	return tx
}

func (d *Debugger) nextTx() *telemetry.DbgMsgTx {
	c := d.C
	if c == nil {
		return nil
	}
	onLastTx := c.CursorTx >= len(c.MsgTxs)
	if onLastTx {
		return nil
	}

	return c.MsgTxs[c.CursorTx]
}

// CurrentTx returns the current transition. Thread safe via Eval().
func (d *Debugger) CurrentTx() *telemetry.DbgMsgTx {
	var tx *telemetry.DbgMsgTx

	// SelectingClient locks d.C
	<-d.Mach.WhenNot1(ss.SelectingClient, nil)

	d.Mach.Eval("CurrentTx", func() {
		tx = d.currentTx()
	}, nil)

	return tx
}

func (d *Debugger) currentTx() *telemetry.DbgMsgTx {
	c := d.C
	if c == nil {
		return nil
	}

	if c.CursorTx == 0 || len(c.MsgTxs) < c.CursorTx {
		return nil
	}

	return c.MsgTxs[c.CursorTx-1]
}

// PrevTx returns the previous transition. Thread safe via Eval().
func (d *Debugger) PrevTx() *telemetry.DbgMsgTx {
	var tx *telemetry.DbgMsgTx

	// SelectingClient locks d.C
	<-d.Mach.WhenNot1(ss.SelectingClient, nil)

	d.Mach.Eval("PrevTx", func() {
		tx = d.prevTx()
	}, nil)

	return tx
}

func (d *Debugger) prevTx() *telemetry.DbgMsgTx {
	c := d.C
	if c == nil {
		return nil
	}
	if c.CursorTx < 2 {
		return nil
	}
	return c.MsgTxs[c.CursorTx-2]
}

func (d *Debugger) ConnectedClients() int {
	// if only 1 client connected, select it (if SelectConnected == true)
	var conns int
	for _, c := range d.Clients {
		if c.connected.Load() {
			conns++
		}
	}

	return conns
}

func (d *Debugger) Dispose() {
	d.Disposed = true

	// machine

	d.Mach.Dispose()
	<-d.Mach.WhenDisposed()

	// data
	d.Clients = nil
	d.C = nil

	// app, give it some time to stop rendering
	time.Sleep(100 * time.Millisecond)
	if d.App.GetScreen() != nil {
		d.App.Stop()
	}
	d.App = nil

	// UI
	d.helpDialog = nil
	d.keyBar = nil
	d.currTxBarLeft = nil
	d.currTxBarRight = nil
	d.nextTxBarLeft = nil
	d.nextTxBarRight = nil
	d.matrix = nil
	d.focusManager = nil
	d.exportDialog = nil
	d.contentPanels = nil
	d.filtersBar = nil
	d.tree = nil
	d.clientList = nil
	d.log = nil

	// logger
	logger := d.Opts.DbgLogger
	if logger != nil {
		// check if the logger is writing to a file
		if file, ok := logger.Writer().(*os.File); ok {
			file.Close()
		}
	}
}

func (d *Debugger) Start(clientID string, txNum int, uiView string) {
	d.Mach.Add1(ss.Start, am.A{
		"Client.id":       clientID,
		"Client.cursorTx": txNum,
		// TODO rename to uiView
		"dbgView": uiView,
	})
}

func (d *Debugger) SetFilterLogLevel(lvl am.LogLevel) {
	d.Opts.Filters.LogLevel = lvl

	// process the filter change
	go d.ProcessFilterChange(context.TODO(), false)
}

func (d *Debugger) ImportData(filename string) {
	// TODO async state
	// TODO show error msg (for dump old formats)

	// support URLs
	var reader *bufio.Reader
	u, err := url.Parse(filename)
	if err == nil && u.Host != "" {

		// download
		resp, err := http.Get(filename)
		if err != nil {
			d.Mach.AddErr(err, nil)
			return
		}
		reader = bufio.NewReader(resp.Body)
	} else {

		// read from fs
		fr, err := os.Open(filename)
		if err != nil {
			d.Mach.AddErr(err, nil)
			return
		}
		defer fr.Close()
		reader = bufio.NewReader(fr)
	}

	// decompress brotli
	brReader := brotli.NewReader(reader)

	// decode gob
	decoder := gob.NewDecoder(brReader)
	var res []*Exportable
	err = decoder.Decode(&res)
	if err != nil {
		log.Printf("Error: import failed %s", err)
		return
	}

	// parse the data
	for _, data := range res {
		id := data.MsgStruct.ID
		d.Clients[id] = &Client{
			id:         id,
			Exportable: *data,
		}
		for i := range data.MsgTxs {
			d.parseMsg(d.Clients[id], i)
		}
	}

	// GC
	runtime.GC()
}

// ///// ///// /////

// ///// PRIV

// ///// ///// /////

func (d *Debugger) updateFiltersBar() {
	focused := d.Mach.Is1(ss.FiltersFocused)
	f := fmt.Sprintf
	text := ""

	// title
	if focused {
		text += "[::bu]F[::-][::b]ilters:[::-]"
	} else {
		text += "[::u]F[::-]ilters:"
	}

	// TODO extract
	filters := []filter{
		{
			id:     "skip-canceled",
			label:  "Skip Canceled",
			active: d.Mach.Is1(ss.FilterCanceledTx),
		},
		{
			id:     "skip-auto",
			label:  "Skip Auto",
			active: d.Mach.Is1(ss.FilterAutoTx),
		},
		{
			id:     "skip-empty",
			label:  "Skip Empty",
			active: d.Mach.Is1(ss.FilterEmptyTx),
		},
		{
			id:     "hide-summaries",
			label:  "Hide Summaries",
			active: d.Mach.Is1(ss.FilterSummaries),
		},
		{
			id:     "log-0",
			label:  "L0",
			active: d.Opts.Filters.LogLevel == am.LogNothing,
		},
		{
			id:     "log-1",
			label:  "L1",
			active: d.Opts.Filters.LogLevel == am.LogChanges,
		},
		{
			id:     "log-2",
			label:  "L2",
			active: d.Opts.Filters.LogLevel == am.LogOps,
		},
		{
			id:     "log-3",
			label:  "L3",
			active: d.Opts.Filters.LogLevel == am.LogDecisions,
		},
		{
			id:     "log-4",
			label:  "L4",
			active: d.Opts.Filters.LogLevel == am.LogEverything,
		},
	}

	// tx filters
	for _, item := range filters {

		// checked
		if item.active {
			text += f(" [::b]%s[::-]", cview.Escape("[X]"))
		} else {
			text += f(" [ ]")
		}

		// focused
		if d.focusedFilter == FilterName(item.id) && focused {
			text += f("[%s][::bu]%s[::-]", colorActive, item.label)
		} else if !focused {
			text += f("[%s]%s", colorHighlight2, item.label)
		} else {
			text += f("%s", item.label)
		}

		text += "[-]"
	}

	// TODO save filters per machine checkbox
	d.filtersBar.SetText(text)
}

func (d *Debugger) updateViews(immediate bool) {
	switch d.Mach.Switch(ss.GroupViews) {

	case ss.MatrixView:
		d.updateMatrix()
		d.contentPanels.HidePanel("tree-log")
		d.contentPanels.HidePanel("tree-matrix")

		d.contentPanels.ShowPanel("matrix")

	case ss.TreeMatrixView:
		d.updateMatrix()
		d.updateTree()
		d.contentPanels.HidePanel("matrix")
		d.contentPanels.HidePanel("tree-log")

		d.contentPanels.ShowPanel("tree-matrix")

	case ss.TreeLogView:
		fallthrough
	default:
		d.updateTree()
		d.updateLog(immediate)
		d.contentPanels.HidePanel("matrix")
		d.contentPanels.HidePanel("tree-matrix")

		d.contentPanels.ShowPanel("tree-log")
	}
}

func (d *Debugger) jumpBack(ev *tcell.EventKey) *tcell.EventKey {
	if d.throttleKey(ev, arrowThrottleMs) {
		return nil
	}

	ctx := context.TODO()
	d.Mach.Remove(am.S{ss.Playing, ss.TailMode}, nil)

	if d.Mach.Is1(ss.StateNameSelected) {
		// state jump
		amhelp.Add1Block(ctx, d.Mach, ss.ScrollToMutTx, am.A{
			"state": d.C.SelectedState,
			"fwd":   false,
		})
		// sidebar for errs
		d.updateClientList(true)
	} else {
		// fast jump
		amhelp.Add1Block(ctx, d.Mach, ss.Back, am.A{
			"amount": min(fastJumpAmount, d.C.CursorTx),
		})
	}

	return nil
}

func (d *Debugger) jumpFwd(ev *tcell.EventKey) *tcell.EventKey {
	if d.throttleKey(ev, arrowThrottleMs) {
		return nil
	}

	ctx := context.TODO()
	d.Mach.Remove(am.S{ss.Playing, ss.TailMode}, nil)

	if d.Mach.Is1(ss.StateNameSelected) {
		// state jump
		amhelp.Add1Block(ctx, d.Mach, ss.ScrollToMutTx, am.A{
			"state": d.C.SelectedState,
			"fwd":   true,
		})
		// sidebar for errs
		d.updateClientList(true)
	} else {
		// fast jump
		amhelp.Add1Block(ctx, d.Mach, ss.Fwd, am.A{
			"amount": min(fastJumpAmount, len(d.C.MsgTxs)-d.C.CursorTx),
		})
	}

	return nil
}

// memorizeTxTime will memorize the current tx time
func (d *Debugger) memorizeTxTime(c *Client) {
	if c.CursorTx > 0 && c.CursorTx <= len(c.MsgTxs) {
		d.lastScrolledTxTime = *c.MsgTxs[c.CursorTx-1].Time
	}
}

func (d *Debugger) parseMsg(c *Client, idx int) {
	// TODO handle panics from wrongly indexed msgs
	// defer d.Mach.PanicToErr(nil)

	// TODO verify hosts by token, to distinguish 2 hosts with the same ID
	msgTx := c.MsgTxs[idx]

	var sum uint64
	for _, v := range msgTx.Clocks {
		sum += v
	}
	index := c.MsgStruct.StatesIndex
	prevTx := &telemetry.DbgMsgTx{}
	if len(c.MsgTxs) > 1 && idx > 0 {
		prevTx = c.MsgTxs[idx-1]
	}

	fakeTx := &am.Transition{
		TimeBefore: prevTx.Clocks,
		TimeAfter:  msgTx.Clocks,
	}
	added, removed, touched := amhelp.GetTransitionStates(fakeTx, index)
	msgTxParsed := &MsgTxParsed{
		TimeSum:       sum,
		StatesAdded:   c.statesToIndexes(added),
		StatesRemoved: c.statesToIndexes(removed),
		StatesTouched: c.statesToIndexes(touched),
	}

	// optimize space
	msgTx.CalledStatesIdxs = amhelp.StatesToIndexes(index,
		msgTx.CalledStates)
	msgTx.MachineID = ""
	msgTx.CalledStates = nil
	for _, step := range msgTx.Steps {
		step.FromStateIdx = slices.Index(index, step.FromState)
		step.ToStateIdx = slices.Index(index, step.ToState)
		step.FromState = ""
		step.ToState = ""
	}

	// errors
	var isErr bool
	for _, name := range index {
		if strings.HasPrefix(name, "Err") && msgTx.Is1(index, name) {
			isErr = true
			break
		}
	}
	if isErr || msgTx.Is1(index, am.Exception) {
		// prepend to errors
		c.errors = append([]int{idx}, c.errors...)
	}

	// store the parsed msg
	c.msgTxsParsed = append(c.msgTxsParsed, msgTxParsed)
	c.mTimeSum = sum

	d.parseMsgLog(c, msgTx, idx)
}

// isTxSkipped checks if the tx at the given index is skipped by filters
// idx is 0-based
func (d *Debugger) isTxSkipped(c *Client, idx int) bool {
	return slices.Index(c.msgTxsFiltered, idx) == -1
}

// filterTxCursor fixes the current cursor according to filters
// by skipping filtered out txs. If none found, returns the current cursor.
func (d *Debugger) filterTxCursor(c *Client, newCursor int, fwd bool) int {
	if !d.isFiltered() {
		return newCursor
	}

	// skip filtered out txs
	for {
		if newCursor < 1 {
			return 0
		} else if newCursor > len(c.MsgTxs) {
			// not found
			if !d.isTxSkipped(c, c.CursorTx-1) {
				return c.CursorTx
			} else {
				return 0
			}
		}

		if d.isTxSkipped(c, newCursor-1) {
			if fwd {
				newCursor++
			} else {
				newCursor--
			}
		} else {
			break
		}
	}

	return newCursor
}

// TODO highlight selected state names, extract common logic
func (d *Debugger) updateTxBars() {
	d.currTxBarLeft.Clear()
	d.currTxBarRight.Clear()
	d.nextTxBarLeft.Clear()
	d.nextTxBarRight.Clear()

	if d.Mach.Not(am.S{ss.SelectingClient, ss.ClientSelected}) {
		d.currTxBarLeft.SetText("Listening for connections on " + d.Opts.ServerAddr)
		return
	}

	c := d.C
	tx := d.currentTx()
	if tx == nil {
		// c is nil when switching clients
		if c == nil || len(c.MsgTxs) == 0 {
			d.currTxBarLeft.SetText("No transitions yet...")
		} else {
			d.currTxBarLeft.SetText("Initial structure")
		}
	} else {

		var title string
		switch d.Mach.Switch(ss.GroupPlaying) {
		case ss.Playing:
			title = formatTxBarTitle("Playing")
		case ss.TailMode:
			title += formatTxBarTitle("Tail") + "   "
		default:
			title = formatTxBarTitle("Paused") + " "
		}

		left, right := d.getTxInfo(c.CursorTx, tx, c.msgTxsParsed[c.CursorTx-1],
			title)
		d.currTxBarLeft.SetText(left)
		d.currTxBarRight.SetText(right)
	}

	nextTx := d.nextTx()
	if nextTx != nil && c != nil {
		title := "Next   "
		left, right := d.getTxInfo(c.CursorTx+1, nextTx,
			c.msgTxsParsed[c.CursorTx], title)
		d.nextTxBarLeft.SetText(left)
		d.nextTxBarRight.SetText(right)
	}
}

func (d *Debugger) updateTimelines() {
	// check for a ready client
	c := d.C
	if c == nil {
		return
	}

	txCount := len(c.MsgTxs)
	nextTx := d.nextTx()
	d.timelineSteps.SetTitleColor(cview.Styles.PrimaryTextColor)
	d.timelineSteps.SetBorderColor(cview.Styles.PrimaryTextColor)
	d.timelineSteps.SetFilledColor(cview.Styles.PrimaryTextColor)

	// grey rejected bars
	if nextTx != nil && !nextTx.Accepted {
		d.timelineSteps.SetFilledColor(tcell.ColorGray)
	}

	// mark last step of a cancelled tx in red
	if nextTx != nil && c.CursorStep == len(nextTx.Steps) && !nextTx.Accepted {
		d.timelineSteps.SetFilledColor(tcell.ColorRed)
	}

	stepsCount := 0
	onLastTx := c.CursorTx >= txCount
	if !onLastTx {
		stepsCount = len(c.MsgTxs[c.CursorTx].Steps)
	}

	// progressbar cant be max==0
	d.timelineTxs.SetMax(max(txCount, 1))
	// progress <= max
	d.timelineTxs.SetProgress(c.CursorTx)

	// title
	var title string
	if d.isFiltered() {
		pos := slices.Index(c.msgTxsFiltered, c.CursorTx-1) + 1
		if c.CursorTx == 0 {
			pos = 0
		}
		title = d.P.Sprintf(" Transition %d / %d [%s]| %d / %d[-] ",
			pos, len(c.msgTxsFiltered), colorHighlight2, c.CursorTx, txCount)
	} else {
		title = d.P.Sprintf(" Transition %d / %d ", c.CursorTx, txCount)
	}
	d.timelineTxs.SetTitle(title)
	d.timelineTxs.SetEmptyRune(' ')

	// progressbar cant be max==0
	d.timelineSteps.SetMax(max(stepsCount, 1))
	// progress <= max
	d.timelineSteps.SetProgress(c.CursorStep)
	d.timelineSteps.SetTitle(fmt.Sprintf(
		" Next mutation step %d / %d ", c.CursorStep, stepsCount))
	d.timelineSteps.SetEmptyRune(' ')
}

func (d *Debugger) updateClientList(immediate bool) {
	if immediate {
		d.doUpdateClientList(true)
		return
	}

	if !d.updateCLScheduled.CompareAndSwap(false, true) {
		// debounce non-forced updates
		return
	}

	go func() {
		time.Sleep(sidebarUpdateDebounce)
		d.doUpdateClientList(false)
	}()
}

func (d *Debugger) doUpdateClientList(immediate bool) {
	if d.Mach.Disposed.Load() {
		return
	}

	update := func() {
		// count
		longestName := 0
		for _, item := range d.clientList.GetItems() {
			ref := item.GetReference().(*sidebarRef)
			l := len(ref.name) + ref.lvl
			if l > longestName {
				longestName = l
			}
		}

		// update
		for i, item := range d.clientList.GetItems() {
			ref := item.GetReference().(*sidebarRef)
			c := d.Clients[ref.name]
			count := int(math.Max(0, float64(longestName+1-len(ref.name)-ref.lvl)))
			spacePre := ""
			spacePost := " "
			if d.clientHasParent(c.id) {
				spacePre = " "
				spacePost = ""
			}
			namePad := strings.Repeat("-", ref.lvl) + spacePre + ref.name +
				strings.Repeat(" ", count) + spacePost
			label := d.getSidebarLabel(namePad, c, i)
			item.SetMainText(label)
		}

		if len(d.Clients) > 0 {
			var totalSum uint64
			for _, c := range d.Clients {
				totalSum += c.mTimeSum
			}

			d.clientList.SetTitle(d.P.Sprintf(
				" Machines:%d T:%v ", len(d.Clients), totalSum))
		} else {
			d.clientList.SetTitle(" Machines ")
		}
	}

	// avoid eval in handlers
	if immediate {
		update()
	} else {
		d.Mach.Eval("doUpdateClientList", update, nil)
	}

	d.updateCLScheduled.Store(false)
	d.draw()
}

// buildClientList builds the clientList with the list of clients.
// selectedIndex: index of the selected item, -1 for the current one.
func (d *Debugger) buildClientList(selectedIndex int) {
	// prev state
	selected := ""
	var item *cview.ListItem
	if selectedIndex == -1 {
		item = d.clientList.GetCurrentItem()
	} else if selectedIndex > -1 {
		item = d.clientList.GetItem(selectedIndex)
	}
	if item != nil {
		selected = item.GetReference().(*sidebarRef).name
	}

	// re-gen all
	d.clientList.Clear()
	var list []string
	for _, c := range d.Clients {
		list = append(list, c.id)
	}

	// sort a-z, with value numbers
	humanSort(list)

	pos := 0
	// TODO REWRITE
	for _, parent := range list {
		if d.clientHasParent(parent) {
			continue
		}

		// create list item
		item := cview.NewListItem(parent)
		item.SetReference(&sidebarRef{name: parent, lvl: 0})
		d.clientList.AddItem(item)

		if selected == "" && d.C != nil && d.C.id == parent {
			// pre-select the current machine
			d.clientList.SetCurrentItem(pos)
		} else if selected == parent {
			// select the previously selected one
			d.clientList.SetCurrentItem(pos)
		}

		pos = d.clientListChild(list, parent, pos, selected, 1)

		pos++
	}

	var totalSum uint64
	for _, c := range d.Clients {
		totalSum += c.mTimeSum
	}

	d.clientList.SetTitle(d.P.Sprintf(
		" Machines:%d T:%v ", len(d.Clients), totalSum))

	// sort TODO blinks anyway
	d.doUpdateClientList(true)
}

type sidebarRef struct {
	name string
	lvl  int
}

func (d *Debugger) clientListChild(
	list []string, parent string, pos int, selected string,
	lvl int,
) int {
	for _, child := range list {
		cc := d.Clients[child]

		if !d.clientHasParent(child) || cc.MsgStruct.Parent != parent {
			continue
		}

		// create list item
		item := cview.NewListItem(child)
		item.SetReference(&sidebarRef{name: child, lvl: lvl})
		d.clientList.AddItem(item)

		if selected == "" && d.C != nil && d.C.id == child {
			// pre-select the current machine
			d.clientList.SetCurrentItem(pos)
		} else if selected == child {
			// select the previously selected one
			d.clientList.SetCurrentItem(pos)
		}

		pos = d.clientListChild(list, child, pos, selected, lvl+1)

		pos++
	}
	return pos
}

func (d *Debugger) clientHasParent(cid string) bool {
	c, ok := d.Clients[cid]
	if !ok || c.MsgStruct.Parent == "" {
		return false
	}
	_, ok = d.Clients[c.MsgStruct.Parent]

	return ok
}

func (d *Debugger) getSidebarLabel(
	name string, c *Client, index int,
) string {
	isHovered := d.clientList.GetCurrentItemIndex() == index
	hasFocus := d.Mach.Is1(ss.SidebarFocused)

	var currCTxIdx int
	var currCTx *telemetry.DbgMsgTx
	// current tx of the selected client
	currSelTx := d.currentTx()
	if currSelTx != nil {
		currTime := d.lastScrolledTxTime
		if currTime.IsZero() {
			currTime = *currSelTx.Time
		}
		currCTxIdx = c.lastTxTill(currTime)
		if currCTxIdx != -1 {
			currCTx = c.MsgTxs[currCTxIdx]
		}
	}

	// Ready, Start states indicators
	state := " "

	if currCTx != nil {
		readyIdx := slices.Index(c.MsgStruct.StatesIndex, "Ready")
		startIdx := slices.Index(c.MsgStruct.StatesIndex, "Start")
		if readyIdx != -1 && am.IsActiveTick(currCTx.Clocks[readyIdx]) {
			state = "R"
		} else if startIdx != -1 && am.IsActiveTick(currCTx.Clocks[startIdx]) {
			state = "S"
		}
	}

	label := d.P.Sprintf("%s %s|%d", name, state, currCTxIdx+1)
	if currCTxIdx+1 < len(c.MsgTxs) {
		label += "+"
	}

	// mark selected
	if d.C != nil && c.id == d.C.id {
		label = "[::bu]" + label
	}

	if c.hadErrSinceTx(currCTxIdx, 100) {
		label = "[red]" + label
	} else if !c.connected.Load() {
		if isHovered && !hasFocus {
			label = "[grey]" + label
		} else if !isHovered {
			label = "[grey]" + label
		} else {
			label = "[black]" + label
		}
	}

	return label
}

func (d *Debugger) updateBorderColor() {
	color := cview.ColorUnset
	if d.C != nil && d.C.connected.Load() {
		color = colorActive
	}

	if d.Mach.IsErr() {
		color = tcell.ColorRed
	}

	for _, box := range d.focusable {
		box.SetBorderColorFocused(color)
	}
}

// TODO should be an async state
func (d *Debugger) exportData(filename string) {
	// validate the input
	if filename == "" {
		log.Printf("Error: export failed no filename")
		return
	}
	if len(d.Clients) == 0 {
		log.Printf("Error: export failed no clients")
		return
	}

	// validate the path
	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("Error: export failed %s", err)
		return
	}
	gobPath := path.Join(cwd, filename+".gob.br")
	fw, err := os.Create(gobPath)
	if err != nil {
		log.Printf("Error: export failed %s", err)
		return
	}
	defer fw.Close()

	// prepare the format
	data := make([]*Exportable, len(d.Clients))
	i := 0
	for _, c := range d.Clients {
		data[i] = &c.Exportable
		i++
	}

	// create a new brotli writer
	brCompress := brotli.NewWriter(fw)
	defer brCompress.Close()

	// encode
	encoder := gob.NewEncoder(brCompress)
	err = encoder.Encode(data)
	if err != nil {
		log.Printf("Error: export failed %s", err)
	}
}

func (d *Debugger) getTxInfo(txIndex int,
	tx *telemetry.DbgMsgTx, parsed *MsgTxParsed, title string,
) (string, string) {
	left := title
	right := " "

	if tx == nil {
		return left, right
	}

	// left side
	prevT := uint64(0)
	if txIndex > 1 {
		prevT = d.C.MsgTxs[txIndex-2].TimeSum()
	}
	calledStates := tx.CalledStateNames(d.C.MsgStruct.StatesIndex)

	left += d.P.Sprintf(" | tx: %d", txIndex)
	left += d.P.Sprintf(" | Time: +%d", tx.TimeSum()-prevT)
	left += " |"

	multi := ""
	if len(calledStates) == 1 &&
		d.C.MsgStruct.States[calledStates[0]].Multi {
		multi += " multi"
	}

	if !tx.Accepted {
		left += "[grey]"
	}
	left += fmt.Sprintf(" %s%s: [::b]%s[::-]", tx.Type, multi,
		strings.Join(calledStates, ", "))

	if !tx.Accepted {
		left += "[-]"
	}

	// right side
	// TODO time to execute
	if tx.IsAuto {
		right += "auto | "
	}
	if !tx.Accepted {
		right += "canceled | "
	}
	right += fmt.Sprintf("add: %d | rm: %d | touch: %d | %s",
		len(parsed.StatesAdded), len(parsed.StatesRemoved),
		len(parsed.StatesTouched), tx.Time.Format(timeFormat),
	)

	return left, right
}

func (d *Debugger) doCleanOnConnect() bool {
	if len(d.Clients) == 0 {
		return false
	}
	var disconns []*Client
	for _, c := range d.Clients {
		if !c.connected.Load() {
			disconns = append(disconns, c)
		}
	}

	// if all disconnected, clean up
	if len(disconns) == len(d.Clients) {
		for _, c := range d.Clients {
			// TODO cant be scheduled, as the client can connect in the meantime
			// d.Add1(ss.RemoveClient, am.A{"Client.id": c.id})
			delete(d.Clients, c.id)
			// c.msgTxsParsed = nil
			// c.logMsgs = nil
			// c.MsgTxs = nil
			// c.MsgStruct = nil
		}
		return true
	}
	return false
}

func (d *Debugger) updateMatrix() {
	if d.Mach.Is1(ss.MatrixRain) {
		d.updateMatrixRain()
	} else {
		d.updateMatrixRelations()
	}
}

func (d *Debugger) updateMatrixRelations() {
	// TODO optimize: re-use existing cells or gen txt
	d.matrix.Clear()
	d.matrix.SetTitle(" Matrix ")

	c := d.C
	if c == nil || d.C.CursorTx == 0 {
		return
	}

	index := c.MsgStruct.StatesIndex
	var tx *telemetry.DbgMsgTx
	var prevTx *telemetry.DbgMsgTx
	if c.CursorStep == 0 {
		tx = d.currentTx()
		prevTx = d.prevTx()
	} else {
		tx = d.nextTx()
		prevTx = d.currentTx()
	}
	steps := tx.Steps
	calledStates := tx.CalledStateNames(c.MsgStruct.StatesIndex)

	// show the current tx summary on step 0, and partial if cursor > 0
	if c.CursorStep > 0 {
		steps = steps[:c.CursorStep]
	}

	highlightIndex := -1

	// TODO use pkg/x/helpers
	// called states
	var called []int
	for i, name := range index {

		v := "0"
		if slices.Contains(calledStates, name) {
			v = "1"
			called = append(called, i)
		}
		d.matrix.SetCellSimple(0, i, matrixCellVal(v))

		// mark called states
		if slices.Contains(calledStates, name) {
			d.matrix.GetCell(0, i).SetAttributes(tcell.AttrBold | tcell.AttrUnderline)
		}

		// mark selected state
		if d.C.SelectedState == name {
			d.matrix.GetCell(0, i).SetBackgroundColor(colorHighlight3)
			highlightIndex = i
		}
	}
	matrixEmptyRow(d, 1, len(index), highlightIndex)

	// ticks
	sum := 0
	for i, name := range index {

		var pTick uint64
		if prevTx != nil {
			pTick = prevTx.Clock(index, name)
		}
		tick := tx.Clock(index, name)

		v := tick - pTick
		sum += int(v)
		d.matrix.SetCellSimple(2, i, matrixCellVal(strconv.Itoa(int(v))))
		cell := d.matrix.GetCell(2, i)

		if v == 0 {
			cell.SetTextColor(tcell.ColorGrey)
		}

		// mark called states
		if slices.Contains(called, i) {
			cell.SetAttributes(
				tcell.AttrBold | tcell.AttrUnderline)
		}

		// mark selected state
		if d.C.SelectedState == name {
			cell.SetBackgroundColor(colorHighlight3)
		}
	}
	matrixEmptyRow(d, 3, len(index), highlightIndex)

	// steps
	for iRow, target := range index {
		for iCol, source := range index {
			v := 0

			for _, step := range steps {

				// TODO style just the cells
				if step.GetFromState(c.MsgStruct.StatesIndex) == source &&
					((step.ToStateIdx == -1 && source == target) ||
						step.GetToState(c.MsgStruct.StatesIndex) == target) {
					v += int(step.Type)
				}

				strVal := strconv.Itoa(v)
				strVal = matrixCellVal(strVal)
				d.matrix.SetCellSimple(iRow+4, iCol, strVal)
				cell := d.matrix.GetCell(iRow+4, iCol)

				// mark selected state
				if d.C.SelectedState == target || d.C.SelectedState == source {
					cell.SetBackgroundColor(colorHighlight3)
				}

				if v == 0 {
					cell.SetTextColor(tcell.ColorGrey)
					continue
				}

				// mark called states
				if slices.Contains(called, iRow) || slices.Contains(called, iCol) {
					cell.SetAttributes(tcell.AttrBold | tcell.AttrUnderline)
				} else {
					cell.SetAttributes(tcell.AttrBold)
				}
			}
		}
	}

	title := " Matrix:" + strconv.Itoa(sum) + " "
	if c.CursorTx > 0 {
		t := strconv.Itoa(int(c.msgTxsParsed[c.CursorTx-1].TimeSum))
		title += "Time:" + t + " "
	}
	d.matrix.SetTitle(title)
}

func (d *Debugger) updateMatrixRain() {
	if d.Mach.Not1(ss.MatrixRain) {
		return
	}

	// TODO optimize: re-use existing cells or gen txt
	d.matrix.Clear()
	d.matrix.SetTitle(" Matrix ")

	c := d.C
	if c == nil {
		return
	}

	index := c.MsgStruct.StatesIndex
	tx := d.currentTx()
	prevTx := d.prevTx()
	_, _, _, rows := d.matrix.GetRect()

	i := 0
	for iShown := 0; iShown < rows; i++ {

		idx := c.CursorTx - i
		if d.Mach.Not1(ss.TailMode) {
			idx += rows / 2
		}
		if idx < 1 || idx > len(c.MsgTxs) {
			break
		}

		// filters
		if d.isFiltered() && d.isTxSkipped(c, idx-1) {
			continue
		}

		row := ""

		tx := c.MsgTxs[idx-1]
		txParsed := c.msgTxsParsed[idx-1]
		calledStates := tx.CalledStateNames(c.MsgStruct.StatesIndex)

		for ii, name := range index {

			v := "."
			sIsErr := strings.HasPrefix(name, "Err")

			if tx.Is1(index, name) {
				v = "1"
				if slices.Contains(txParsed.StatesTouched, ii) {
					v = "2"
				}
			} else if slices.Contains(txParsed.StatesRemoved, ii) {
				v = "|"
			} else if !tx.Accepted && slices.Contains(calledStates, index[ii]) {
				// called but canceled
				v = "c"
			} else if slices.Contains(txParsed.StatesTouched, ii) {
				v = "*"
			}

			row += v

			d.matrix.SetCellSimple(iShown, ii, v)

			// gray out some
			if !tx.Accepted || v == "." || v == "|" || v == "c" || v == "*" {
				d.matrix.GetCell(iShown, ii).SetTextColor(colorHighlight)
			}

			// mark called states
			if slices.Contains(calledStates, name) {
				d.matrix.GetCell(iShown, ii).SetAttributes(tcell.AttrUnderline)
			}

			if idx == c.CursorTx {
				// current tx
				d.matrix.GetCell(iShown, ii).SetBackgroundColor(colorHighlight3)
			} else if d.C.SelectedState == name {
				// mark selected state
				d.matrix.GetCell(iShown, ii).SetBackgroundColor(colorHighlight3)
			}

			if (sIsErr || name == am.Exception) && tx.Is1(index, name) {
				// mark exceptions
				if tx.Accepted {
					d.matrix.GetCell(iShown, ii).SetBackgroundColor(tcell.ColorIndianRed)
				} else {
					d.matrix.GetCell(iShown, ii).SetBackgroundColor(colorHighlight3)
				}
			}
		}

		// timestamp
		tStamp := tx.Time.Format(timeFormat)
		tStampFmt := tStamp
		// highlight first diff number since prev timestamp
		if idx > 1 {
			prevTStamp := c.MsgTxs[idx-2].Time.Format(timeFormat)
			if idx := findFirstDiff(prevTStamp, tStamp); idx != -1 {
				tStampFmt = tStamp[:idx] + "[white]" + tStamp[idx:idx+1] + "[gray]" +
					tStamp[idx+1:]
			}
		}

		// tail cell
		d.matrix.SetCellSimple(iShown, len(index), fmt.Sprintf(
			"  [gray]%d | %s[-]", idx, tStampFmt))

		// current tx
		if idx == c.CursorTx {
			d.matrix.GetCell(iShown, len(index)).SetBackgroundColor(colorHighlight3)
		}

		// inc the shown counter
		iShown++
	}

	diffT := 0
	if c.CursorTx > 0 {
		for _, name := range index {

			var pTick uint64
			if prevTx != nil {
				pTick = prevTx.Clock(index, name)
			}
			tick := tx.Clock(index, name)

			v := tick - pTick
			diffT += int(v)
		}
	}

	title := " Matrix:" + strconv.Itoa(diffT) + " "
	if c.CursorTx > 0 {
		t := strconv.Itoa(int(c.msgTxsParsed[c.CursorTx-1].TimeSum))
		title += "Time:" + t + " "
	}
	d.matrix.SetTitle(title)
	d.matrix.ScrollToBeginning()
}

func (d *Debugger) getSidebarCurrClientIdx() int {
	if d.C == nil {
		return -1
	}

	i := 0
	for _, item := range d.clientList.GetItems() {
		ref := item.GetReference().(*sidebarRef)
		if ref.name == d.C.id {
			return i
		}
		i++
	}

	return -1
}

// filterClientTxs filters client's txs according the selected filters.
// Called by filter states, not directly.
func (d *Debugger) filterClientTxs() {
	if d.C == nil || !d.isFiltered() {
		return
	}

	auto := d.Mach.Is1(ss.FilterAutoTx)
	empty := d.Mach.Is1(ss.FilterEmptyTx)
	canceled := d.Mach.Is1(ss.FilterCanceledTx)

	d.C.msgTxsFiltered = nil
	for i := range d.C.MsgTxs {
		if d.filterTx(d.C, i, auto, empty, canceled) {
			d.C.msgTxsFiltered = append(d.C.msgTxsFiltered, i)
		}
	}
}

// isFiltered checks if any filter is active.
func (d *Debugger) isFiltered() bool {
	return d.Mach.Any1(ss.GroupFilters...)
}

// filterTx returns true when a TX passed the passed filters.
func (d *Debugger) filterTx(
	c *Client, idx int, auto, empty, canceled bool,
) bool {
	tx := c.MsgTxs[idx]

	if auto && tx.IsAuto {
		return false
	}
	if canceled && !tx.Accepted {
		return false
	}

	// empty
	parsed := c.msgTxsParsed[idx]
	txEmpty := true
	if empty && len(parsed.StatesAdded) == 0 &&
		len(parsed.StatesRemoved) == 0 {
		for _, step := range tx.Steps {
			// running a handler is not empty
			// TODO test running a handler is not empty
			if step.Type == am.StepHandler {
				txEmpty = false
			}
		}

		if txEmpty {
			return false
		}
	}

	return true
}

func (d *Debugger) scrollToTime(hT time.Time, filter bool) bool {
	latestTx := d.C.lastTxTill(hT)
	if latestTx == -1 {
		return false
	}

	if filter {
		latestTx = d.filterTxCursor(d.C, latestTx, true)
	}
	d.SetCursor(latestTx)

	return true
}

func (d *Debugger) ProcessFilterChange(ctx context.Context, filterTxs bool) {
	// TODO refac to FilterToggledState
	<-d.Mach.WhenQueueEnds(ctx)
	if ctx.Err() != nil {
		d.Mach.Remove1(ss.ToggleFilter, nil)
		return // expired
	}

	if filterTxs {
		d.filterClientTxs()
	}

	if d.C != nil {

		// stay on the last one
		if d.Mach.Is1(ss.TailMode) {
			d.SetCursor(d.filterTxCursor(d.C, len(d.C.MsgTxs), false))
		}

		// rebuild the whole log to reflect the UI changes
		err := d.rebuildLog(ctx, len(d.C.MsgTxs))
		if err != nil {
			d.Mach.AddErr(err, nil)
		}
		d.updateLog(false)

		if ctx.Err() != nil {
			return // expired
		}

		if filterTxs {
			d.SetCursor(d.filterTxCursor(d.C, d.C.CursorTx, false))
		}
	}

	// queue this removal after filter states, so we can depend on WhenNot
	d.Mach.Remove1(ss.ToggleFilter, nil)

	d.updateClientList(false)
	d.updateFiltersBar()
	d.updateTimelines()
	d.updateMatrixRain()
	d.updateLog(false)
	d.draw()
}

func allocMem() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}
