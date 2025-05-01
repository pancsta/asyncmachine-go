// Package debugger provides a TUI debugger with multi-client support. Runnable
// command can be found in tools/cmd/am-dbg.
package debugger

import (
	"bufio"
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"
	"github.com/zyedidia/clipper"
	"golang.org/x/text/message"

	amvis "github.com/pancsta/asyncmachine-go/tools/visualizer"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	"github.com/pancsta/asyncmachine-go/pkg/graph"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

type S = am.S

type Debugger struct {
	*am.ExceptionHandler
	Mach *am.Machine

	Clients map[string]*Client
	// TODO make threadsafe
	Opts       Opts
	LayoutRoot *cview.Panels
	Disposed   bool
	// selected client
	// TODO atomic, drop eval
	C   *Client
	App *cview.Application
	// printer for numbers
	P *message.Printer
	// TODO GC removed machines
	History       []*MachAddress
	HistoryCursor int

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
	statusBar          *cview.TextView
	clientList         *cview.List
	mainGrid           *cview.Grid
	logRebuildEnd      int
	lastScrolledTxTime time.Time
	repaintScheduled   atomic.Bool
	genGraphsLast      time.Time
	graph              *graph.Graph
	// update client list scheduled
	updateCLScheduled  atomic.Bool
	buildCLScheduled   atomic.Bool
	lastKeystroke      tcell.Key
	lastKeystrokeTime  time.Time
	updateLogScheduled atomic.Bool
	matrix             *cview.Table
	focusManager       *cview.FocusManager
	exportDialog       *cview.Modal
	contentPanels      *cview.Panels
	toolbars           [2]*cview.Table
	treeLogGrid        *cview.Grid
	treeMatrixGrid     *cview.Grid
	lastSelectedState  string
	// TODO should be after a redraw, not before
	// redrawCallback is auto-disposed in draw()
	redrawCallback  func()
	healthcheck     *time.Ticker
	logReader       *cview.TreeView
	helpDialogRight *cview.TextView
	addressBar      *cview.Table
	tagsBar         *cview.TextView
	clip            clipper.Clipboard
	// toolbarItems is a list of row of toolbars items
	toolbarItems     [][]toolbarItem
	clientListFile   *os.File
	msgsDelayed      []*telemetry.DbgMsgTx
	msgsDelayedConns []string
	currTxBar        *cview.Flex
	nextTxBar        *cview.Flex
	mainGridCols     []int
}

// New creates a new debugger instance and optionally import a data file.
func New(ctx context.Context, opts Opts) (*Debugger, error) {
	var err error
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

	id := utils.RandId(0)
	if opts.ID != "" {
		id = opts.ID
	}
	mach, err := am.NewCommon(ctx, "", ss.States, ss.Names, d, nil, &am.Opts{
		DontLogID:      true,
		HandlerTimeout: 1 * time.Second,
		ID:             "d-" + id,
		Tags:           []string{"am-dbg"},
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

	d.graph, err = graph.New(d.Mach)
	if err != nil {
		mach.AddErr(fmt.Errorf("graph init: %w", err), nil)
	}

	// import data TODO state
	if opts.ImportData != "" {
		start := time.Now()
		mach.Log("Importing data from %s", opts.ImportData)
		d.ImportData(opts.ImportData)
		mach.Log("Imported data in %s", time.Since(start))
	}

	// clipboard
	clip, err := clipper.GetClipboard(clipper.Clipboards...)
	if err != nil {
		mach.AddErr(fmt.Errorf("clipboard init: %w", err), nil)
	}
	d.clip = clip

	// ensure output directory exists
	if d.Opts.OutputDir != "" && d.Opts.OutputDir != "." {
		if err := os.MkdirAll(d.Opts.OutputDir, 0o755); err != nil {
			mach.AddErr(fmt.Errorf("create output dir: %w", err), nil)
		}
	}

	// client list file
	if d.Opts.OutputClients {
		p := path.Join(d.Opts.OutputDir, "am-dbg-clients.txt")
		clientListFile, err := os.Create(p)
		if err != nil {
			mach.AddErr(fmt.Errorf("client list file open: %w", err), nil)
		}
		d.clientListFile = clientListFile
	}

	return d, nil
}

// ///// ///// /////

// ///// PUB

// ///// ///// /////

// GetMachAddress returns the address of the currently visible view (mach, tx).
func (d *Debugger) GetMachAddress() *MachAddress {
	if d.C == nil {
		return nil
	}
	a := &MachAddress{
		MachId: d.C.id,
	}
	if d.C.CursorTx1 > 0 {
		a.TxId = d.C.MsgTxs[d.C.CursorTx1-1].ID
	}
	// TODO mach time

	return a
}

// GoToMachAddress tries to render a view of the provided address (mach, tx).
func (d *Debugger) GoToMachAddress(addr *MachAddress, skipHistory bool) bool {
	// TODO should be ana async state, always change the view via this state

	if addr.MachId == "" {
		return false
	}

	// select mach, if not selected
	if d.C == nil || d.C.id != addr.MachId {
		res := d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": addr.MachId})
		if res == am.Canceled {
			return false
		}
	}

	// TODO ctx
	<-d.Mach.When1(ss.ClientSelected, nil)
	if addr.TxId != "" {
		d.Mach.Add1(ss.ScrollToTx, am.A{"Client.txId": addr.TxId})
	} else if addr.MachTime != 0 {
		tx := d.C.tx(d.C.txByMachTime(addr.MachTime))
		d.Mach.Add1(ss.ScrollToTx, am.A{"Client.txId": tx.ID})
	} else if !addr.HumanTime.IsZero() {
		tx := d.C.tx(d.C.lastTxTill(addr.HumanTime))
		d.Mach.Add1(ss.ScrollToTx, am.A{"Client.txId": tx.ID})
	}
	if !skipHistory {
		d.prependHistory(addr)
		d.updateAddressBar()
		d.draw(d.addressBar)
	}

	return true
}

func (d *Debugger) SetCursor1(cursor int, skipHistory bool) {
	if d.C.CursorTx1 == cursor {
		return
	}

	// TODO validate
	d.C.CursorTx1 = cursor

	if d.HistoryCursor == 0 && !skipHistory {
		// add current mach if needed
		if len(d.History) > 0 && d.History[0].MachId != d.C.id {
			d.prependHistory(d.GetMachAddress())
		}
		// keeping the current tx as history head
		if tx := d.C.tx(d.C.CursorTx1 - 1); tx != nil {
			// dup the current machine if tx differs
			if len(d.History) > 1 && d.History[1].MachId == d.C.id &&
				d.History[1].TxId != tx.ID {

				d.prependHistory(d.History[0].Clone())
			}
			if len(d.History) > 0 {
				d.History[0].TxId = tx.ID
			}
		}
	}

	// debug
	// d.Opts.DbgLogger.Printf("HistoryCursor: %d\n", d.HistoryCursor)
	// d.Opts.DbgLogger.Printf("History: %v\n", d.History)

	if cursor == 0 {
		d.lastScrolledTxTime = time.Time{}
	} else {
		d.lastScrolledTxTime = *d.currentTx().Time
	}

	// reset the step timeline
	d.C.CursorStep1 = 0
	d.Mach.Remove1(ss.TimelineStepsScrolled, nil)

	// re-gen graphs
	d.Mach.Add1(ss.SetCursor, nil)
}

func (d *Debugger) removeHistory(clientId string) {
	hist := make([]*MachAddress, 0)
	for i, item := range d.History {
		if i <= d.HistoryCursor && d.HistoryCursor > 0 {
			d.HistoryCursor--
		}
		if item.MachId == clientId {
			continue
		}

		hist = append(hist, item)
	}

	d.History = hist
}

func (d *Debugger) prependHistory(addr *MachAddress) {
	d.History = slices.Concat([]*MachAddress{addr}, d.History)
	d.trimHistory()
}

// trimHistory will trim the head to the current position, making it the newest
// entry
func (d *Debugger) trimHistory() {
	// remove head
	if d.HistoryCursor > 0 {
		rm := d.HistoryCursor
		if rm >= len(d.History) {
			rm = len(d.History)
		}
		d.History = d.History[rm:]
	}

	// prepend
	d.HistoryCursor = 0
	if len(d.History) > 100 {
		d.History = d.History[100:]
	}

	// debug
	// d.Opts.DbgLogger.Printf("HistoryCursor: %d\n", d.HistoryCursor)
	// d.Opts.DbgLogger.Printf("History: %v\n", d.History)
}

func (d *Debugger) getClient(machId string) *Client {
	if d.Clients == nil {
		return nil
	}
	c, ok := d.Clients[machId]
	if !ok {
		return nil
	}

	return c
}

func (d *Debugger) getClientTx(
	machId, txId string,
) (*Client, *telemetry.DbgMsgTx) {
	c := d.getClient(machId)
	if c == nil {
		return nil, nil
	}
	idx := c.txIndex(txId)
	if idx < 0 {
		return nil, nil
	}
	tx := c.tx(idx)
	if tx == nil {
		return nil, nil
	}

	return c, tx
}

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
	onLastTx := c.CursorTx1 >= len(c.MsgTxs)
	if onLastTx {
		return nil
	}

	return c.MsgTxs[c.CursorTx1]
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

	if c.CursorTx1 == 0 || len(c.MsgTxs) < c.CursorTx1 {
		return nil
	}

	return c.MsgTxs[c.CursorTx1-1]
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
	if c.CursorTx1 < 2 {
		return nil
	}
	return c.MsgTxs[c.CursorTx1-2]
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
	d.statusBar = nil
	d.currTxBarLeft = nil
	d.currTxBarRight = nil
	d.nextTxBarLeft = nil
	d.nextTxBarRight = nil
	d.matrix = nil
	d.focusManager = nil
	d.exportDialog = nil
	d.contentPanels = nil
	for i := range d.toolbars {
		d.toolbars[i] = nil
	}
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
	// TODO thread safe
	d.Opts.Filters.LogLevel = lvl

	// process the toolbarItem change
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
		d.Mach.AddErr(fmt.Errorf("Error: import failed %w", err), nil)
		return
	}

	// parse the data
	for _, data := range res {
		id := data.MsgStruct.ID
		d.Clients[id] = &Client{
			id:         id,
			Exportable: *data,
		}
		if d.graph != nil {
			err := d.graph.AddClient(data.MsgStruct)
			if err != nil {
				d.Mach.AddErr(fmt.Errorf("Error: import failed %w", err), nil)
				return
			}
		}
		d.Mach.Add1(ss.InitClient, am.A{"id": id})
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

func (d *Debugger) updateToolbar() {
	f := fmt.Sprintf

	// tx filters
	for i, row := range d.toolbarItems {
		focused := d.Mach.Is1(ss.Toolbar1Focused)
		if i == 1 {
			focused = d.Mach.Is1(ss.Toolbar2Focused)
		}

		for ii, item := range row {
			text := ""
			_, sel := d.toolbars[i].GetSelection()

			// checked
			esc := cview.Escape
			if item.active != nil && item.active() {
				if item.activeLabel != nil {
					text += f(" [::b]%s[::-]", esc("["+item.activeLabel()+"]"))
				} else {
					text += f(" [::b]%s[::-]", esc("[X]"))
				}

				// button - dedicated icon
			} else if item.active == nil && item.icon != "" {
				text += f(" [gray]%s[-]%s[gray]%s[-]", esc("["), item.icon, esc("]"))

				// unchecked
			} else if item.active == nil {
				text += f(" [grey][ ][-]")

				// button - default icon
			} else {
				text += f(" [ ]")
			}

			// focused
			if d.toolbarItems[i][sel].id == ToolName(item.id) && focused {
				text += "[white]" + item.label
			} else if !focused {
				text += f("[%s]%s", colorHighlight2, item.label)
			} else {
				text += f("%s", item.label)
			}

			cell := d.toolbars[i].GetCell(0, ii)
			cell.SetText(text)
			cell.SetTextColor(tcell.ColorWhite)
			d.toolbars[i].SetCell(0, ii, cell)
		}
	}
}

func (d *Debugger) updateAddressBar() {
	machId := ""
	machConn := false
	txId := ""
	if d.C != nil {
		machId = d.C.id
		if d.C.CursorTx1 > 0 {
			// TODO conflict with GC?
			txId = d.C.MsgTxs[d.C.CursorTx1-1].ID
		}
		machConn = d.C.connected.Load()
	}

	// copy
	copyCell := d.addressBar.GetCell(0, 6)
	copyCell.SetBackgroundColor(tcell.ColorLightGray)
	copyCell.SetTextColor(tcell.ColorBlack)
	if machId == "" {
		copyCell.SetSelectable(false)
		copyCell.SetBackgroundColor(tcell.ColorDefault)
	} else {
		copyCell.SetSelectable(true)
	}
	pasteCell := d.addressBar.GetCell(0, 8)
	pasteCell.SetTextColor(tcell.ColorBlack)
	pasteCell.SetBackgroundColor(tcell.ColorLightGray)

	// history
	fwdCell := d.addressBar.GetCell(0, 2)
	fwdCell.SetBackgroundColor(tcell.ColorLightGray)
	fwdCell.SetTextColor(tcell.ColorBlack)
	if d.HistoryCursor > 0 {
		fwdCell.SetSelectable(true)
	} else {
		fwdCell.SetSelectable(false)
		fwdCell.SetTextColor(tcell.ColorGray)
		fwdCell.SetBackgroundColor(tcell.ColorDefault)
	}
	backCell := d.addressBar.GetCell(0, 0)
	backCell.SetBackgroundColor(tcell.ColorLightGray)
	backCell.SetTextColor(tcell.ColorBlack)
	if d.HistoryCursor < len(d.History)-1 {
		backCell.SetSelectable(true)
	} else {
		backCell.SetSelectable(false)
		backCell.SetTextColor(tcell.ColorGray)
		backCell.SetBackgroundColor(tcell.ColorDefault)
	}

	// detect clipboard
	if d.clip == nil {
		copyCell.SetTextColor(tcell.ColorGrey)
		copyCell.SetSelectable(false)
		copyCell.SetBackgroundColor(tcell.ColorDefault)
		pasteCell.SetTextColor(tcell.ColorGrey)
		pasteCell.SetSelectable(false)
		pasteCell.SetBackgroundColor(tcell.ColorDefault)
	}

	// address
	machColor := "[grey]"
	if machConn {
		machColor = "[" + colorActive.String() + "]"
	}
	addrCell := d.addressBar.GetCell(0, 4)
	if machId != "" && txId != "" {
		addrCell.SetText(machColor + "mach://[-][::u]" + machId +
			"[::-][grey]/" + txId)
	} else if machId != "" {
		addrCell.SetText(machColor + "mach://[-][::u]" + machId)
	} else {
		addrCell.SetText("[grey]mach://[-]")
	}

	// tags
	tags := ""
	if machId != "" {
		if len(d.C.MsgStruct.Tags) > 0 {
			tags += "[::b]#[::-]" + strings.Join(d.C.MsgStruct.Tags, " [::b]#[::-]")
		}
		parentTags := d.GetParentTags(d.C, nil)
		if len(parentTags) > 0 {
			if tags != "" {
				tags += " ... "
			}
			tags += "[::b]#[::-]" + strings.Join(parentTags, " [::b]#[::-]")
		}
	}
	d.tagsBar.SetText(tags)
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

// TODO state
func (d *Debugger) jumpBack(ev *tcell.EventKey) *tcell.EventKey {
	if ev != nil && d.throttleKey(ev, arrowThrottleMs) {
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
			"amount": min(fastJumpAmount, d.C.CursorTx1),
		})
	}

	return nil
}

// TODO state
func (d *Debugger) jumpFwd(ev *tcell.EventKey) *tcell.EventKey {
	if ev != nil && d.throttleKey(ev, arrowThrottleMs) {
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
			"amount": min(fastJumpAmount, len(d.C.MsgTxs)-d.C.CursorTx1),
		})
	}

	return nil
}

// memorizeTxTime will memorize the current tx time
func (d *Debugger) memorizeTxTime(c *Client) {
	if c.CursorTx1 > 0 && c.CursorTx1 <= len(c.MsgTxs) {
		d.lastScrolledTxTime = *c.MsgTxs[c.CursorTx1-1].Time
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

	// optimize space TODO remove with SQL
	if len(msgTx.CalledStates) > 0 {
		msgTx.CalledStatesIdxs = amhelp.StatesToIndexes(index,
			msgTx.CalledStates)
		msgTx.MachineID = ""
		msgTx.CalledStates = nil
	}
	for _, step := range msgTx.Steps {
		if step.FromState != "" || step.ToState != "" {
			step.FromStateIdx = slices.Index(index, step.FromState)
			step.ToStateIdx = slices.Index(index, step.ToState)
			step.FromState = ""
			step.ToState = ""
		}

		// back compat
		if step.Data != nil {
			step.RelType, _ = step.Data.(am.Relation)
		}
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

	// logs and graph
	d.parseMsgLog(c, msgTx, idx)
	if d.graph != nil {
		d.graph.ParseMsg(c.id, msgTx)
	}
}

// isTxSkipped checks if the tx at the given index is skipped by toolbarItems
// idx is 0-based
func (d *Debugger) isTxSkipped(c *Client, idx int) bool {
	if !d.isFiltered() {
		return false
	}
	return slices.Index(c.msgTxsFiltered, idx) == -1
}

// filterTxCursor fixes the current cursor according to toolbarItems
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
			if !d.isTxSkipped(c, c.CursorTx1-1) {
				return c.CursorTx1
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

		left, right := d.getTxInfo(c.CursorTx1, tx, c.msgTxsParsed[c.CursorTx1-1],
			title)
		d.currTxBarLeft.SetText(left)
		d.currTxBarRight.SetText(right)
	}

	nextTx := d.nextTx()
	if nextTx != nil && c != nil {
		title := "Next   "
		left, right := d.getTxInfo(c.CursorTx1+1, nextTx,
			c.msgTxsParsed[c.CursorTx1], title)
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
	if nextTx != nil && c.CursorStep1 == len(nextTx.Steps) && !nextTx.Accepted {
		d.timelineSteps.SetFilledColor(tcell.ColorRed)
	}

	stepsCount := 0
	onLastTx := c.CursorTx1 >= txCount
	if !onLastTx {
		stepsCount = len(c.MsgTxs[c.CursorTx1].Steps)
	}

	// progressbar cant be max==0
	d.timelineTxs.SetMax(max(txCount, 1))
	// progress <= max
	d.timelineTxs.SetProgress(c.CursorTx1)

	// title
	var title string
	if d.isFiltered() {
		pos := slices.Index(c.msgTxsFiltered, c.CursorTx1-1) + 1
		if c.CursorTx1 == 0 {
			pos = 0
		}
		title = d.P.Sprintf(" Transition %d / %d [%s]%d / %d[-] ",
			pos, len(c.msgTxsFiltered), colorHighlight2, c.CursorTx1, txCount)
	} else {
		title = d.P.Sprintf(" Transition %d / %d ", c.CursorTx1, txCount)
	}
	d.timelineTxs.SetTitle(title)
	d.timelineTxs.SetEmptyRune(' ')

	// progressbar cant be max==0
	d.timelineSteps.SetMax(max(stepsCount, 1))
	// progress <= max
	d.timelineSteps.SetProgress(c.CursorStep1)
	d.timelineSteps.SetTitle(fmt.Sprintf(
		" Next mutation step %d / %d ", c.CursorStep1, stepsCount))
	d.timelineSteps.SetEmptyRune(' ')
}

func (d *Debugger) updateBorderColor() {
	color := colorInactive
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

	// create file
	gobPath := path.Join(d.Opts.OutputDir, filename+".gob.br")
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
			d.removeHistory(c.id)
		}
		if d.graph != nil {
			d.graph.Clear()
		}

		return true
	}

	return false
}

func (d *Debugger) updateMatrix() {
	if !d.Mach.Any1(ss.MatrixView, ss.TreeMatrixView) {
		return
	}

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
	if c == nil || d.C.CursorTx1 == 0 {
		return
	}

	index := c.MsgStruct.StatesIndex
	var tx *telemetry.DbgMsgTx
	var prevTx *telemetry.DbgMsgTx
	if c.CursorStep1 == 0 {
		tx = d.currentTx()
		prevTx = d.prevTx()
	} else {
		tx = d.nextTx()
		prevTx = d.currentTx()
	}
	steps := tx.Steps
	calledStates := tx.CalledStateNames(c.MsgStruct.StatesIndex)

	// show the current tx summary on step 0, and partial if cursor > 0
	if c.CursorStep1 > 0 {
		steps = steps[:c.CursorStep1]
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
	if c.CursorTx1 > 0 {
		t := strconv.Itoa(int(c.msgTxsParsed[c.CursorTx1-1].TimeSum))
		title += "Time:" + t + " "
	}
	d.matrix.SetTitle(title)
}

func (d *Debugger) updateMatrixRain() {
	if d.Mach.Not1(ss.MatrixRain) {
		return
	}

	// TODO optimize: re-use existing cells?
	d.matrix.Clear()
	d.matrix.SetTitle(" Rain ")

	c := d.C
	if c == nil {
		return
	}

	// TODO extract, mind currTxRow
	currTxRow := -1
	// TODO keep in eval / state
	d.matrix.SetSelectionChangedFunc(func(row, column int) {
		if d.Mach.Not1(ss.MatrixRain) {
			return
		}

		// 1st select
		if currTxRow == -1 {
			d.Mach.Add1(ss.ScrollToTx, am.A{
				"Client.cursorTx": row,
				"trimHistory":     true,
			})

			// >1st select, if row changed
		} else if row != currTxRow {
			diff := row - currTxRow
			idx := c.filterIndexByCursor1(c.CursorTx1) + diff
			if idx == -1 {
				return
			}

			// scroll
			cur1 := c.msgTxsFiltered[idx] + 1
			d.Mach.Log("diff %s", diff)
			d.Mach.Add1(ss.ScrollToTx, am.A{
				"Client.cursorTx": cur1,
				"trimHistory":     true,
			})
		}

		// select state name
		if column >= 0 && column < len(c.MsgStruct.StatesIndex) {
			d.Mach.Add1(ss.StateNameSelected, am.A{
				"state": c.MsgStruct.StatesIndex[column],
			})
		}
	})
	d.matrix.SetSelectable(true, true)

	index := c.MsgStruct.StatesIndex
	tx := d.currentTx()
	prevTx := d.prevTx()
	_, _, _, rows := d.matrix.GetRect()

	// collect tx to show, starting from the end (timeline 1-based index)
	toShow := []int{}
	ahead := rows / 2
	if d.Mach.Is1(ss.TailMode) {
		ahead = 0
	}

	// TODO collect rows-amount before and after (always) and display, then fill
	//  the missing rows from previously collected

	cur := c.filterIndexByCursor1(c.CursorTx1)
	var curLast int

	// ahead
	aheadOk := func(i int, max int) bool {
		return i < len(c.msgTxsFiltered) && len(toShow) <= max
	}
	for i := cur; aheadOk(i, ahead); i++ {
		toShow = append(toShow, c.msgTxsFiltered[i])
		curLast = i
	}

	// behind
	behindOk := func(i int) bool {
		return i >= 0 && i < len(c.msgTxsFiltered) && len(toShow) <= rows
	}
	for i := cur - 1; behindOk(i); i-- {
		toShow = slices.Concat([]int{c.msgTxsFiltered[i]}, toShow)
	}

	// ahead again
	for i := curLast + 1; aheadOk(i, rows); i++ {
		toShow = append(toShow, c.msgTxsFiltered[i])
	}

	for i, idx := range toShow {

		row := ""
		idx = idx + 1
		if idx == c.CursorTx1 {
			// TODO keep idx using cell.SetReference(...) for the 1st cell in each row
			currTxRow = i
		}
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

			// init table
			d.matrix.SetCellSimple(i, ii, v)
			cell := d.matrix.GetCell(i, ii)
			cell.SetSelectable(true)

			// gray out some
			if !tx.Accepted || v == "." || v == "|" || v == "c" || v == "*" {
				cell.SetTextColor(colorHighlight)
			}

			// mark called states
			if slices.Contains(calledStates, name) {
				cell.SetAttributes(tcell.AttrUnderline)
			}

			if idx == c.CursorTx1 {
				// current tx
				cell.SetBackgroundColor(colorHighlight3)
			} else if d.C.SelectedState == name {
				// mark selected state
				cell.SetBackgroundColor(colorHighlight3)
			}

			if (sIsErr || name == am.Exception) && tx.Is1(index, name) {
				// mark exceptions
				if tx.Accepted {
					cell.SetBackgroundColor(tcell.ColorIndianRed)
				} else {
					cell.SetBackgroundColor(colorHighlight3)
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
		d.matrix.SetCellSimple(i, len(index), fmt.Sprintf(
			"  [gray]%d | %s[-]", idx, tStampFmt))

		// current tx
		if idx == c.CursorTx1 {
			d.matrix.GetCell(i, len(index)).SetBackgroundColor(colorHighlight3)
		}
	}

	diffT := 0
	if c.CursorTx1 > 0 {
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
	if c.CursorTx1 > 0 {
		t := strconv.Itoa(int(c.msgTxsParsed[c.CursorTx1-1].TimeSum))
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

// filterClientTxs toolbarItems client's txs according the selected
// toolbarItems. Called by toolbarItem states, not directly.
func (d *Debugger) filterClientTxs() {
	// TODO remove with SQL
	if d.C == nil || !d.isFiltered() {
		return
	}

	auto := d.Mach.Is1(ss.FilterAutoTx)
	empty := d.Mach.Is1(ss.FilterEmptyTx)
	canceled := d.Mach.Is1(ss.FilterCanceledTx)
	healthcheck := d.Mach.Is1(ss.FilterHealthcheck)

	d.C.msgTxsFiltered = nil
	for i := range d.C.MsgTxs {
		if d.filterTx(d.C, i, auto, empty, canceled, healthcheck) {
			d.C.msgTxsFiltered = append(d.C.msgTxsFiltered, i)
		}
	}
}

// isFiltered checks if any toolbarItem is active.
func (d *Debugger) isFiltered() bool {
	return d.Mach.Any1(ss.GroupFilters...)
}

// filterTx returns true when a TX passed the passed toolbarItems.
func (d *Debugger) filterTx(
	c *Client, idx int, auto, empty, canceled, healthcheck bool,
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

	// healthcheck
	hCheck := ssam.BasicStates.Healthcheck
	hBeat := ssam.BasicStates.Heartbeat
	if healthcheck && len(parsed.StatesAdded) == 1 &&
		len(parsed.StatesRemoved) == 0 {
		if c.indexesToStates(parsed.StatesAdded)[0] == hCheck ||
			c.indexesToStates(parsed.StatesAdded)[0] == hBeat {
			return false
		}
	}
	if healthcheck && len(parsed.StatesRemoved) == 1 &&
		len(parsed.StatesAdded) == 0 {
		if c.indexesToStates(parsed.StatesRemoved)[0] == hCheck ||
			c.indexesToStates(parsed.StatesRemoved)[0] == hBeat {
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
	d.SetCursor1(latestTx, true)

	return true
}

func (d *Debugger) ProcessFilterChange(ctx context.Context, filterTxs bool) {
	// TODO refac to FilterToggledState
	<-d.Mach.WhenQueueEnds(ctx)
	if ctx.Err() != nil {
		d.Mach.Remove1(ss.ToggleTool, nil)
		return // expired
	}

	if filterTxs {
		d.filterClientTxs()
	}

	if d.C != nil {

		// stay on the last one
		if d.Mach.Is1(ss.TailMode) {
			d.SetCursor1(d.filterTxCursor(d.C, len(d.C.MsgTxs), false), false)
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
			d.SetCursor1(d.filterTxCursor(d.C, d.C.CursorTx1, false), false)
		}
	}

	// queue this removal after toolbarItem states, so we can depend on WhenNot
	d.Mach.Remove1(ss.ToggleTool, nil)

	d.updateClientList(false)
	d.updateToolbar()
	d.updateTimelines()
	d.updateMatrix()
	d.updateLog(false)
	d.draw()
}

func (d *Debugger) GetParentTags(c *Client, tags []string) []string {
	parent, ok := d.Clients[c.MsgStruct.Parent]
	if !ok {
		return tags
	}
	tags = slices.Concat(tags, parent.MsgStruct.Tags)

	return d.GetParentTags(parent, tags)
}

func (d *Debugger) initGraphGen(shot *graph.Graph) []*amvis.Visualizer {
	var vizs []*amvis.Visualizer

	// render single (current one)
	vis := amvis.New(d.Mach, shot)
	amvis.PresetSingle(vis)
	// TODO make opts threadsafe
	switch d.Opts.Graph {
	default:
		return vizs

		// single simple
	case 1:
		vis.RenderNestSubmachines = false
		vis.RenderStart = false
		vis.RenderInherited = false
		vis.RenderPipes = false
		vis.RenderHalfPipes = false
		vis.RenderHalfConns = false
		vis.RenderHalfHierarchy = false
		vis.RenderParentRel = false

		// single detailed
	case 2:
		vis.RenderNestSubmachines = false
		vis.RenderStart = true
		vis.RenderInherited = true
		vis.RenderPipes = false
		vis.RenderHalfPipes = false
		vis.RenderHalfConns = false
		vis.RenderHalfHierarchy = false

		// single external
	case 3:
		vis.RenderNestSubmachines = true
		vis.RenderStart = true
		vis.RenderInherited = true
		vis.RenderPipes = true
	}

	d.Mach.Log("rendering graphs lvl %d", d.Opts.Graph)

	// vis
	// mach-id.svg
	machId := d.C.id
	vis.RenderMachs = []string{machId}
	vis.OutputFilename = path.Join(d.Opts.OutputDir, machId)

	vizs = append(vizs, vis)

	// TODO render by prefixes
	// } else {
	// 	for _, p := range strings.Split(d.Opts.Graph, ",") {
	//
	// 		// vis
	// 		vis := amvis.New(d.Mach, shot)
	// 		if p == "1" || p == "true" {
	// 			// render single (current one)
	// 			amvis.PresetSingle(vis)
	// 			vis.RenderMachs = []string{p}
	// 			vis.RenderNestSubmachines = true
	// 			vis.RenderStart = true
	// 		} else {
	// 			// render by prefix
	// 			prefix := regexp.MustCompile("^" + p)
	// 			amvis.PresetNeighbourhood(vis)
	// 			vis.RenderMachsRe = []*regexp.Regexp{prefix}
	// 			vis.RenderDistance = 1
	// 			vis.RenderDepth = 1
	// 		}
	// 		// prefix with am-vis
	// 		vis.OutputFilename = path.Join(d.Opts.OutputDir, "am-vis-"+p)
	//
	// 		vizs = append(vizs, vis)
	// 	}
	// }

	// map
	// TODO skip if there was no change in schemas
	//  hash schemas and schema's hashes, then compare

	vis = amvis.New(d.Mach, shot)
	amvis.PresetMap(vis)
	vis.OutputFilename = path.Join(d.Opts.OutputDir, "am-vis-map")

	return append(vizs, vis)
}

func (d *Debugger) syncOptsTimelines() {
	switch d.Opts.Timelines {
	case 0:
		d.Mach.Add(S{ss.TimelineHidden, ss.TimelineStepsHidden}, nil)
	case 1:
		d.Mach.Add1(ss.TimelineStepsHidden, nil)
		d.Mach.Remove1(ss.TimelineHidden, nil)
	case 2:
		d.Mach.Remove(S{ss.TimelineStepsHidden, ss.TimelineHidden}, nil)
	}
}
