// Package debugger provides a TUI debugger with multi-client support. Runnable
// command can be found in tools/cmd/am-dbg.
package debugger

// TODO
//  - ProcessFilterChange state
//  - DoUpdateLog state
//  - refac WalkUnsage to Walk via delayed writes, to fix races
//  - use the `hMethod` convention and impl Eval2Getter

import (
	"bufio"
	"context"
	_ "embed"
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

	"github.com/PuerkitoBio/goquery"
	"github.com/andybalholm/brotli"
	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"
	"github.com/zyedidia/clipper"
	"golang.org/x/text/message"

	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	amvis "github.com/pancsta/asyncmachine-go/tools/visualizer"
)

type S = am.S

type cache struct {
	diagramId  string
	diagramLvl int
	diagramDom *goquery.Document
}

type Debugger struct {
	*am.ExceptionHandler
	Mach *am.Machine

	Clients map[string]*Client
	// TODO make threadsafe
	Opts       Opts
	LayoutRoot *cview.Panels
	// selected client
	// TODO atomic, drop eval
	C   *Client
	App *cview.Application
	// printer for numbers TODO global
	P *message.Printer
	// TODO GC removed machines
	History       []*types.MachAddress
	HistoryCursor int

	// UI is currently being drawn
	drawing            atomic.Bool
	cache              cache
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
	// repaintScheduled controls the UI paint debounce
	repaintScheduled atomic.Bool
	// repaintPending indicates a skipped repaint
	repaintPending atomic.Bool
	genGraphsLast  time.Time
	graph          *amgraph.Graph
	// update client list scheduled
	updateCLScheduled atomic.Bool
	buildCLScheduled  atomic.Bool
	lastKeystroke     tcell.Key
	lastKeystrokeTime time.Time
	matrix            *cview.Table
	focusManager      *cview.FocusManager
	exportDialog      *cview.Modal
	contentPanels     *cview.Panels
	toolbars          [3]*cview.Table
	schemaLogGrid     *cview.Grid
	treeMatrixGrid    *cview.Grid
	lastSelectedState string
	// TODO should be after a redraw, not before
	// redrawCallback is auto-disposed in draw()
	redrawCallback  func()
	heartbeatT      *time.Ticker
	logReader       *cview.TreeView
	helpDialogRight *cview.TextView
	addressBar      *cview.Table
	tagsBar         *cview.TextView
	clip            clipper.Clipboard
	// toolbarItems is a list of row of toolbars items
	toolbarItems     [][]toolbarItem
	clientListFile   *os.File
	txListFile       *os.File
	msgsDelayed      []*telemetry.DbgMsgTx
	msgsDelayedConns []string
	currTxBar        *cview.Flex
	nextTxBar        *cview.Flex
	mainGridCols     []int
	readerExpanded   map[string]bool
	treeGroups       *cview.DropDown
	treeLayout       *cview.Flex
	// list of states to show, bypassing other ones from the schema
	schemaTreeStates  am.S
	lastSelectedGroup string
	// number of appended log msgs without a rebuild
	logAppends        int
	logRenderedClient string
	lastResize        uint64
	logLastResize     uint64
}

// New creates a new debugger instance and optionally import a data file.
func New(ctx context.Context, opts Opts) (*Debugger, error) {
	var err error
	// init the debugger
	d := &Debugger{
		Clients:        make(map[string]*Client),
		readerExpanded: make(map[string]bool),
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

	gob.Register(server.Exportable{})
	gob.Register(am.Relation(0))

	id := utils.RandId(0)
	if opts.Id != "" {
		id = opts.Id
	}
	mach, err := am.NewCommon(ctx, "d-"+id, ss.States, ss.Names, d, nil, &am.Opts{
		DontLogId: true,
		Tags:      []string{"am-dbg"},
	})
	if err != nil {
		return nil, err
	}
	d.Mach = mach
	// mach.AddBreakpoint1(ss.BuildingLog, "", false)
	// mach.AddBreakpoint1(ss.BuildingLog, "", true)
	// TODO update to schemas
	mach.SetGroupsString(map[string]am.S{
		"Dialog":           ss.GroupDialog,
		"Playing":          ss.GroupPlaying,
		"Focused":          ss.GroupFocused,
		"Views":            ss.GroupViews,
		"SwitchedClientTx": ss.GroupSwitchedClientTx,
		"Filters":          ss.GroupFilters,
		"Debug":            ss.GroupDebug,
	}, []string{
		"Dialog",
		"Playing",
		"Focused",
		"Views",
		"SwitchedClientTx",
		"Filters",
		"Debug",
	})

	if d.Opts.Version == "" {
		d.Opts.Version = "(devel)"
	}

	// logging
	semLog := mach.SemLogger()
	if opts.DbgLogger != nil {
		semLog.SetSimple(opts.DbgLogger.Printf, opts.DbgLogLevel)
	} else {
		semLog.SetSimple(log.Printf, opts.DbgLogLevel)
	}
	semLog.SetArgsMapper(am.NewArgsMapper([]string{
		// TODO extract
		"Client.id", "conn_id", "cursorTx1", "amount", "Client.id",
		"state", "fwd", "filter", "ToolName", "uri", "addr", "url",
		"err", "_am_err",
	}, 20))

	d.graph, err = amgraph.New(d.Mach)
	if err != nil {
		mach.AddErr(fmt.Errorf("graph init: %w", err), nil)
	}

	// import data TODO state
	if opts.ImportData != "" {
		fmt.Printf("Importing data from %s\nPlease wait...\n", opts.ImportData)
		start := time.Now()
		mach.Log("Importing data from %s", opts.ImportData)
		d.hImportData(opts.ImportData)
		mach.Log("Imported data in %s", time.Since(start))
	}

	// clipboard
	if d.Opts.EnableClipboard {
		clip, err := clipper.GetClipboard(clipper.Clipboards...)
		if err != nil {
			mach.AddErr(fmt.Errorf("clipboard init: %w", err), nil)
		}
		d.clip = clip
	}

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

	// tx file
	if d.Opts.OutputTx {
		p := path.Join(d.Opts.OutputDir, "am-dbg-tx.md")
		txListFile, err := os.Create(p)
		if err != nil {
			mach.AddErr(fmt.Errorf("tx list file open: %w", err), nil)
		}
		d.txListFile = txListFile
	}

	return d, nil
}

// ///// ///// /////

// ///// PUB

// ///// ///// /////

// hGetMachAddress returns the address of the currently visible view (mach, tx).
func (d *Debugger) hGetMachAddress() *types.MachAddress {
	c := d.C
	if c == nil {
		return nil
	}
	a := &types.MachAddress{
		MachId:   c.Id,
		MachTime: c.MTimeSum,
	}
	if c.CursorTx1 > 0 {
		a.TxId = c.MsgTxs[c.CursorTx1-1].ID
	}
	if c.CursorStep1 > 0 {
		a.Step = c.CursorStep1
	}

	return a
}

// hGoToMachAddress tries to render a view of the provided address (mach, tx).
// Blocks. TODO state: GoToMachAddr, MachAddr
func (d *Debugger) hGoToMachAddress(
	addr *types.MachAddress, skipHistory bool,
) bool {
	// TODO should be an async state

	if addr.MachId == "" {
		return false
	}

	var wait <-chan struct{}
	mach := d.Mach

	// select the target mach, if not selected
	if d.C == nil || d.C.Id != addr.MachId {
		// TODO ctx
		// TODO extract as amhelp.WhenNextActive
		if mach.Is1(ss.ClientSelected) {
			wait = mach.WhenTicks(ss.ClientSelected, 2, nil)
		} else {
			wait = mach.When1(ss.ClientSelected, nil)
		}
		res := mach.Add1(ss.SelectingClient, am.A{"Client.id": addr.MachId})
		if res == am.Canceled {
			return false
		}
	} else {
		// TODO ctx
		wait = mach.When1(ss.ClientSelected, nil)
	}

	// TODO DONT BLOCK
	<-wait
	if addr.TxId != "" {
		// TODO typed args
		args := am.A{"Client.txId": addr.TxId}
		if addr.Step != 0 {
			args["cursorStep1"] = addr.Step
			mach.Add1(ss.ScrollToStep, args)
		}
		mach.Add1(ss.ScrollToTx, args)
	} else if addr.MachTime != 0 {
		tx := d.C.Tx(d.C.TxByMachTime(addr.MachTime))
		mach.Add1(ss.ScrollToTx, am.A{"Client.txId": tx.ID})
	} else if !addr.HumanTime.IsZero() {
		tx := d.C.Tx(d.C.LastTxTill(addr.HumanTime))
		mach.Add1(ss.ScrollToTx, am.A{"Client.txId": tx.ID})
	}
	if !skipHistory {
		d.hPrependHistory(addr)
		d.hUpdateAddressBar()
		// TODO only if main panel visible
		d.draw(d.addressBar)
	}

	return true
}

// func (d *Debugger) hSetCursor1(
//   cursor int, cursorStep int, skipHistory bool,
// ) {
// 	if d.C.CursorTx1 == cursor {
// 		return
// 	}
//
// 	// TODO validate
// 	d.C.CursorTx1 = cursor
//
// 	if d.HistoryCursor == 0 && !skipHistory {
// 		// add current mach if needed
// 		if len(d.History) > 0 && d.History[0].MachId != d.C.id {
// 			d.hPrependHistory(d.hGetMachAddress())
// 		}
// 		// keeping the current tx as history head
// 		if tx := d.C.tx(d.C.CursorTx1 - 1); tx != nil {
// 			// dup the current machine if tx differs
// 			if len(d.History) > 1 && d.History[1].MachId == d.C.id &&
// 				d.History[1].TxId != tx.ID {
//
// 				d.hPrependHistory(d.History[0].Clone())
// 			}
// 			if len(d.History) > 0 {
// 				d.History[0].TxId = tx.ID
// 			}
// 		}
// 	}
//
// 	// debug
// 	// d.Opts.DbgLogger.Printf("HistoryCursor: %d\n", d.HistoryCursor)
// 	// d.Opts.DbgLogger.Printf("History: %v\n", d.History)
//
// 	if cursor == 0 {
// 		d.lastScrolledTxTime = time.Time{}
// 	} else {
// 		tx := d.hCurrentTx()
// 		d.lastScrolledTxTime = *tx.Time
//
// 		// tx file
// 		if d.Opts.OutputTx {
// 			index := d.C.MsgStruct.StatesIndex
// 			_, _ = d.txListFile.WriteAt([]byte(tx.TxString(index)), 0)
// 		}
// 	}
//
// 	// reset the step timeline
// 	// TODO validate
// 	d.C.CursorStep1 = cursorStep
// 	d.Mach.Remove1(ss.TimelineStepsScrolled, nil)
// }

func (d *Debugger) hRemoveHistory(clientId string) {
	hist := make([]*types.MachAddress, 0)
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

func (d *Debugger) hPrependHistory(addr *types.MachAddress) {
	d.History = slices.Concat([]*types.MachAddress{addr}, d.History)
	d.hTrimHistory()
}

// hTrimHistory will trim the head to the current position, making it the newest
// entry
func (d *Debugger) hTrimHistory() {
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

func (d *Debugger) hGetClient(machId string) *Client {
	if d.Clients == nil {
		return nil
	}
	c, ok := d.Clients[machId]
	if !ok {
		return nil
	}

	return c
}

func (d *Debugger) hGetClientTx(
	machId, txId string,
) (*Client, *telemetry.DbgMsgTx) {
	c := d.hGetClient(machId)
	if c == nil {
		return nil, nil
	}
	idx := c.TxIndex(txId)
	if idx < 0 {
		return nil, nil
	}
	tx := c.Tx(idx)
	if tx == nil {
		return nil, nil
	}

	return c, tx
}

// Client returns the current Client. Thread safe via Eval().
func (d *Debugger) Client() *Client {
	var c *Client

	// SelectingClient locks d.C TODO amhelp.WaitForAll
	<-d.Mach.WhenNot1(ss.SelectingClient, nil)

	// TODO eval to getter
	d.Mach.Eval("Client", func() {
		c = d.C
	}, nil)

	// TODO confirm c != nil, return err
	return c
}

// NextTx returns the next transition. Thread safe via Eval().
func (d *Debugger) NextTx() *telemetry.DbgMsgTx {
	var tx *telemetry.DbgMsgTx

	// SelectingClient locks d.C
	<-d.Mach.WhenNot1(ss.SelectingClient, nil)

	// TODO eval to getter
	d.Mach.Eval("NextTx", func() {
		tx = d.hNextTx()
	}, nil)

	// TODO confirm tx != nil, return err
	return tx
}

func (d *Debugger) hNextTx() *telemetry.DbgMsgTx {
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

	// TODO eval to getter
	d.Mach.Eval("CurrentTx", func() {
		tx = d.hCurrentTx()
	}, nil)

	// TODO confirm tx != nil, return err
	return tx
}

func (d *Debugger) hCurrentTx() *telemetry.DbgMsgTx {
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

	// TODO eval to getter
	d.Mach.Eval("PrevTx", func() {
		tx = d.hPrevTx()
	}, nil)

	// TODO confirm tx != nil, return err
	return tx
}

func (d *Debugger) hPrevTx() *telemetry.DbgMsgTx {
	c := d.C
	if c == nil {
		return nil
	}
	if c.CursorTx1 < 2 {
		return nil
	}
	return c.MsgTxs[c.CursorTx1-2]
}

func (d *Debugger) hConnectedClients() int {
	// if only 1 client connected, select it (if SelectConnected == true)
	var conns int
	for _, c := range d.Clients {
		if c.Connected.Load() {
			conns++
		}
	}

	return conns
}

func (d *Debugger) Dispose() {
	// TODO switch to Disposed mixin
	d.Mach.Dispose()
	<-d.Mach.WhenDisposed()

	// logger
	logger := d.Opts.DbgLogger
	if logger != nil {
		// check if the logger is writing to a file
		if file, ok := logger.Writer().(*os.File); ok {
			file.Close()
		}
	}
}

// TODO config param to New(
func (d *Debugger) Start(
	clientID string, txNum int, uiView string, group string,
) {
	d.Mach.Add1(ss.Start, am.A{
		"Client.id": clientID,
		"cursorTx1": txNum,
		// TODO rename to uiView
		"dbgView": uiView,
		"group":   group,
	})
}

// TODO state: SetOptsState
func (d *Debugger) SetFilterLogLevel(lvl am.LogLevel) {
	d.Mach.Eval("SetFilterLogLevel", func() {
		d.Opts.Filters.LogLevel = lvl

		// process the toolbarItem change
		d.Mach.Add1(ss.ToolToggled, nil)
		d.hUpdateSchemaLogGrid()
		d.hRedrawFull(false)
	}, nil)
}

// TODO state: ImportingData, DataImported
func (d *Debugger) hImportData(filename string) {
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
	var res []*server.Exportable
	err = decoder.Decode(&res)
	if err != nil {
		d.Mach.AddErr(fmt.Errorf("import failed %w", err), nil)
		return
	}

	// parse the data
	for _, data := range res {
		id := data.MsgStruct.ID
		hash := amhelp.SchemaHash((*data).MsgStruct.States)
		d.Clients[id] = newClient(id, id, hash, data)
		if d.graph != nil {
			err := d.graph.AddClient(data.MsgStruct)
			if err != nil {
				d.Mach.AddErr(fmt.Errorf("import failed %w", err), nil)
				return
			}
		}
		d.Mach.Add1(ss.InitClient, am.A{"id": id})
		for i := range data.MsgTxs {
			d.hParseMsg(d.Clients[id], i)
		}
	}

	// GC
	runtime.GC()
}

// ///// ///// /////

// ///// PRIV

// ///// ///// /////

func (d *Debugger) hUpdateToolbar() {
	f := fmt.Sprintf

	// tx filters
	for i, row := range d.toolbarItems {
		focused := d.Mach.Is1(ss.Toolbar1Focused)
		switch i {
		case 1:
			focused = d.Mach.Is1(ss.Toolbar2Focused)
		case 2:
			focused = d.Mach.Is1(ss.Toolbar3Focused)
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
			if sel != -1 && d.toolbarItems[i][sel].id == ToolName(item.id) &&
				focused {

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

func (d *Debugger) hUpdateAddressBar() {
	machId := ""
	machConn := false
	txId := ""
	stepId := ""
	if d.C != nil {
		machId = d.C.Id
		if d.C.CursorTx1 > 0 {
			// TODO conflict with GC?
			txId = d.C.MsgTxs[d.C.CursorTx1-1].ID
		}
		if d.C.CursorStep1 > 0 {
			// TODO conflict with GC?
			stepId = strconv.Itoa(d.C.CursorStep1)
		}
		machConn = d.C.Connected.Load()
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
		s := ""
		if stepId != "" {
			s = "/" + stepId
		}
		addrCell.SetText(machColor + "mach://[-][::u]" + machId +
			"[::-][grey]/" + txId + s)
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
		parentTags := d.hGetParentTags(d.C, nil)
		if len(parentTags) > 0 {
			if tags != "" {
				tags += " ... "
			}
			tags += "[::b]#[::-]" + strings.Join(parentTags, " [::b]#[::-]")
		}
	}
	d.tagsBar.SetText(tags)
}

// hUpdateViews updates the contents of the currentl visible view.
func (d *Debugger) hUpdateViews(immediate bool) {
	switch d.Mach.Switch(ss.GroupViews) {

	case ss.MatrixView:
		d.hUpdateMatrix()
		d.contentPanels.HidePanel("tree-log")
		d.contentPanels.HidePanel("tree-matrix")

		d.contentPanels.ShowPanel("matrix")

	case ss.TreeMatrixView:
		d.hUpdateMatrix()
		d.hUpdateSchemaTree()
		d.contentPanels.HidePanel("matrix")
		d.contentPanels.HidePanel("tree-log")

		d.contentPanels.ShowPanel("tree-matrix")

	case ss.TreeLogView:
		fallthrough
	default:
		d.hUpdateSchemaTree()
		if immediate {
			d.Mach.Add1(ss.UpdateLogScheduled, nil)
		} else {
			d.Mach.Add1(ss.UpdateLogScheduled, nil)
		}
		d.contentPanels.HidePanel("matrix")
		d.contentPanels.HidePanel("tree-matrix")

		d.contentPanels.ShowPanel("tree-log")
	}
}

// TODO remove?
// hMemorizeTxTime will memorize the current tx time
// func (d *Debugger) hMemorizeTxTime(c *Client) {
// 	if c.CursorTx1 > 0 && c.CursorTx1 <= len(c.MsgTxs) {
// 		d.lastScrolledTxTime = *c.MsgTxs[c.CursorTx1-1].Time
// 	}
// }

func (d *Debugger) hParseMsg(c *Client, idx int) {
	// TODO handle panics from wrongly indexed msgs
	// defer d.Mach.PanicToErr(nil)

	// TODO verify connId
	msgTx := c.MsgTxs[idx]

	var sum uint64
	for _, v := range msgTx.Clocks {
		sum += v
	}
	index := c.MsgStruct.StatesIndex
	prevTx := &telemetry.DbgMsgTx{}
	prevTxParsed := &types.MsgTxParsed{}
	if len(c.MsgTxs) > 1 && idx > 0 {
		prevTx = c.MsgTxs[idx-1]
		prevTxParsed = c.MsgTxsParsed[idx-1]
	}

	// cast to Transition
	fakeTx := &am.Transition{
		TimeBefore: prevTx.Clocks,
		TimeAfter:  msgTx.Clocks,
		Steps:      msgTx.Steps,
	}

	// err if TimeAfter < TimeBefore, fake the rest
	after := fakeTx.TimeAfter.Sum(nil)
	before := fakeTx.TimeBefore.Sum(nil)
	if after < before {
		d.Mach.AddErr(fmt.Errorf("time after < time before"), nil)
		c.MTimeSum = sum
		c.MsgTxsParsed = append(c.MsgTxsParsed, &types.MsgTxParsed{TimeSum: sum})
		c.LogMsgs = append(c.LogMsgs, make([]*am.LogEntry, 0))

		return
	}

	added, removed, touched := amhelp.GetTransitionStates(fakeTx, index)
	msgTxParsed := &types.MsgTxParsed{
		TimeSum: sum,
		// TODO use in tx info bars
		TimeDiff:      sum - prevTxParsed.TimeSum,
		StatesAdded:   c.StatesToIndexes(added),
		StatesRemoved: c.StatesToIndexes(removed),
		StatesTouched: c.StatesToIndexes(touched),
	}

	// optimize space
	if len(msgTx.CalledStates) > 0 {
		msgTx.CalledStatesIdxs = amhelp.StatesToIndexes(index,
			msgTx.CalledStates)
		msgTx.MachineID = ""
		msgTx.CalledStates = nil
	}
	// TODO refac when dbg@v2 lands
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
		if strings.HasPrefix(name, am.PrefixErr) && msgTx.Is1(index, name) {
			isErr = true
			break
		}
	}
	if isErr || msgTx.Is1(index, am.StateException) {
		// prepend to errors
		c.Errors = append([]int{idx}, c.Errors...)
	}

	// store the parsed msg
	c.MsgTxsParsed = append(c.MsgTxsParsed, msgTxParsed)
	c.MTimeSum = sum

	// logs and graph
	d.hParseMsgLog(c, msgTx, idx)
	if d.graph != nil {
		d.graph.ParseMsg(c.Id, msgTx)
	}

	// rebuild the log to trim the head (unless importing)
	if d.Mach.Is1(ss.Start) {
		d.Mach.Add1(ss.BuildingLog, nil)
	}

	// TODO DEBUG
	msgTx.CalledStates = amhelp.IndexesToStates(index, msgTx.CalledStatesIdxs)
}

// hIsTxSkipped checks if the tx at the given index is skipped by toolbarItems
// idx is 0-based
func (d *Debugger) hIsTxSkipped(c *Client, idx int) bool {
	if !d.filtersActive() {
		return false
	}
	return slices.Index(c.MsgTxsFiltered, idx) == -1
}

// hFilterTxCursor1 fixes the current cursor according to toolbarItems
// by skipping filtered out txs. If none found, returns the current cursor.
func (d *Debugger) hFilterTxCursor1(c *Client, newCursor1 int, back bool) int {
	if !d.filtersActive() {
		return newCursor1
	}

	// skip filtered out txs
	for {
		if newCursor1 < 1 {
			return 0
		} else if newCursor1 > len(c.MsgTxs) {
			// not found
			if !d.hIsTxSkipped(c, c.CursorTx1-1) {
				return c.CursorTx1
			} else {
				return 0
			}
		}

		if d.hIsTxSkipped(c, newCursor1-1) {
			if back {
				newCursor1--
			} else {
				newCursor1++
			}
		} else {
			break
		}
	}

	return newCursor1
}

// TODO highlight selected state names, extract common logic
func (d *Debugger) hUpdateTxBars() {
	d.currTxBarLeft.Clear()
	d.currTxBarRight.Clear()
	d.nextTxBarLeft.Clear()
	d.nextTxBarRight.Clear()

	if d.Mach.Not(am.S{ss.SelectingClient, ss.ClientSelected}) {
		d.currTxBarLeft.SetText("Listening for connections on " + d.Opts.AddrRpc)
		return
	}

	c := d.C
	tx := d.hCurrentTx()
	if tx == nil {
		// c is nil when switching clients
		if c == nil || len(c.MsgTxs) == 0 {
			d.currTxBarLeft.SetText("No transitions yet...")
		} else {
			d.currTxBarLeft.SetText("Initial machine schema")
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

		left, right := d.hGetTxInfo(c.CursorTx1, tx, c.MsgTxsParsed[c.CursorTx1-1],
			title)
		d.currTxBarLeft.SetText(left)
		d.currTxBarRight.SetText(right)
	}

	nextTx := d.hNextTx()
	if nextTx != nil && c != nil {
		title := "Next   "
		left, right := d.hGetTxInfo(c.CursorTx1+1, nextTx,
			c.MsgTxsParsed[c.CursorTx1], title)
		d.nextTxBarLeft.SetText(left)
		d.nextTxBarRight.SetText(right)
	}
}

func (d *Debugger) hUpdateTimelines() {
	// check for a ready client
	c := d.C
	if c == nil {
		return
	}

	txCount := len(c.MsgTxs)
	nextTx := d.hNextTx()
	d.timelineSteps.SetTitleColor(cview.Styles.PrimaryTextColor)
	d.timelineSteps.SetBorderColor(cview.Styles.PrimaryTextColor)
	d.timelineSteps.SetFilledColor(cview.Styles.PrimaryTextColor)

	// grey rejected bars
	if nextTx != nil && !nextTx.Accepted {
		d.timelineSteps.SetFilledColor(tcell.ColorGray)
	}

	// mark the last step of a canceled tx in red
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
	if d.filtersActive() {
		pos := slices.Index(c.MsgTxsFiltered, c.CursorTx1-1) + 1
		if c.CursorTx1 == 0 {
			pos = 0
		}
		title = d.P.Sprintf(" Transition %d / %d [%s]%d / %d[-] ",
			pos, len(c.MsgTxsFiltered), colorHighlight2, c.CursorTx1, txCount)
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

func (d *Debugger) hUpdateBorderColor() {
	color := colorInactive
	if d.Mach.IsErr() {
		color = tcell.ColorRed
	}
	for _, box := range d.focusable {
		box.SetBorderColorFocused(color)
	}
}

// TODO state: ExportingData, DataExported
// TODO remove log.
func (d *Debugger) hExportData(filename string, snapshot bool) {
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
	now := *d.C.Tx(max(0, d.C.CursorTx1)).Time
	data := make([]*server.Exportable, len(d.Clients))
	i := 0
	for _, c := range d.Clients {
		data[i] = c.Exportable
		// snapshot limits to a single tx
		if snapshot {
			data[i].MsgTxs = []*telemetry.DbgMsgTx{c.Tx(c.LastTxTill(now))}
		}
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

func (d *Debugger) hGetTxInfo(txIndex1 int,
	tx *telemetry.DbgMsgTx, parsed *types.MsgTxParsed, title string,
) (string, string) {
	left := title
	right := " "
	if tx == nil {
		return left, right
	}

	// left side
	var prev *telemetry.DbgMsgTx
	if txIndex1 > 1 {
		prev = d.C.MsgTxs[txIndex1-2]
	}
	// TODO limit state names to a group when [2]group
	calledStates := tx.CalledStateNames(d.C.MsgStruct.StatesIndex)

	left += d.P.Sprintf(" | tx: %d", txIndex1)
	if parsed.TimeDiff == 0 {
		left += " | Time: [gray] 0[-]"
	} else {
		left += d.P.Sprintf(" | Time: +%d", parsed.TimeDiff)
	}
	left += " |"

	multi := ""
	if len(calledStates) == 1 && d.C.MsgStruct.States[calledStates[0]].Multi {
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

	if tx.IsAuto {
		right += "auto | "
	}
	if tx.IsCheck {
		right += "check | "
	}
	if tx.IsQueued {
		right += "[grey]queued[-] | "
	} else if !tx.Accepted {
		right += "[grey]canceled[-] | "
	}

	// format time
	tStamp := tx.Time.Format(timeFormat)
	if prev != nil {
		prevTStamp := prev.Time.Format(timeFormat)
		if idx := findFirstDiff(prevTStamp, tStamp); idx != -1 {
			tStamp = tStamp[:idx] + "[white]" + tStamp[idx:idx+1] + "[grey]" +
				tStamp[idx+1:]
		}
	}

	right += fmt.Sprintf("add: %d | rm: %d | touch: %3s | [grey]%s",
		len(parsed.StatesAdded), len(parsed.StatesRemoved),
		strconv.Itoa(len(parsed.StatesTouched)), tStamp,
	)

	return left, right
}

func (d *Debugger) hCleanOnConnect() bool {
	if len(d.Clients) == 0 {
		return false
	}
	var disconns []*Client
	for _, c := range d.Clients {
		if !c.Connected.Load() {
			disconns = append(disconns, c)
		}
	}

	// if all disconnected, clean up
	if len(disconns) == len(d.Clients) {
		for _, c := range d.Clients {
			// TODO cant be scheduled, as the client can connect in the meantime
			// d.Add1(ss.RemoveClient, am.A{"Client.id": c.id})
			delete(d.Clients, c.Id)
			d.hRemoveHistory(c.Id)
		}
		if d.graph != nil {
			d.graph.Clear()
		}

		return true
	}

	return false
}

func (d *Debugger) hUpdateMatrix() {
	if !d.Mach.Any1(ss.MatrixView, ss.TreeMatrixView) {
		return
	}

	if d.Mach.Is1(ss.MatrixRain) {
		d.hUpdateMatrixRain()
	} else {
		d.hUpdateMatrixRelations()
	}
}

func (d *Debugger) hUpdateMatrixRelations() {
	// TODO optimize: re-use existing cells or gen txt
	// TODO switch to rel matrix from helpers
	d.matrix.Clear()
	d.matrix.SetTitle(" Matrix ")

	c := d.C
	if c == nil || d.C.CursorTx1 == 0 {
		return
	}

	index := c.MsgStruct.StatesIndex
	if c.SelectedGroup != "" {
		index = c.MsgSchemaParsed.Groups[c.SelectedGroup]
	}
	var tx *telemetry.DbgMsgTx
	var prevTx *telemetry.DbgMsgTx
	if c.CursorStep1 == 0 {
		tx = d.hCurrentTx()
		prevTx = d.hPrevTx()
	} else {
		tx = d.hNextTx()
		prevTx = d.hCurrentTx()
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
		t := strconv.Itoa(int(c.MsgTxsParsed[c.CursorTx1-1].TimeSum))
		title += "Time:" + t + " "
	}
	d.matrix.SetTitle(title)
}

func (d *Debugger) hUpdateMatrixRain() {
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

	currTxRow := -1
	d.matrix.SetSelectionChangedFunc(func(row, column int) {
		d.Mach.Add1(ss.MatrixRainSelected, am.A{
			// TODO typed args
			"row":       row,
			"column":    column,
			"currTxRow": currTxRow,
		})
	})
	d.matrix.SetSelectable(true, true)

	index := c.MsgStruct.StatesIndex
	if g := c.SelectedGroup; g != "" {
		index = c.MsgSchemaParsed.Groups[g]
	}
	tx := d.hCurrentTx()
	prevTx := d.hPrevTx()
	_, _, _, height := d.matrix.GetInnerRect()
	height -= 1

	// collect tx to show, starting from the end (timeline 1-based index)
	// TODO renders rows 1 too many
	toShow := []int{}
	ahead := height / 2
	if d.Mach.Is1(ss.TailMode) {
		ahead = 0
	}

	// TODO collect rows-amount before and after (always) and display, then fill
	//  the missing rows from previously collected

	cur := c.FilterIndexByCursor1(c.CursorTx1)
	var curLast int

	// ahead
	aheadOk := func(i int, max int) bool {
		return i < len(c.MsgTxsFiltered) && len(toShow) <= max
	}
	for i := cur; aheadOk(i, ahead); i++ {
		toShow = append(toShow, c.MsgTxsFiltered[i])
		curLast = i
	}

	// behind
	behindOk := func(i int) bool {
		return i >= 0 && i < len(c.MsgTxsFiltered) && len(toShow) <= height
	}
	for i := cur - 1; behindOk(i); i-- {
		toShow = slices.Concat([]int{c.MsgTxsFiltered[i]}, toShow)
	}

	// ahead again
	for i := curLast + 1; aheadOk(i, height); i++ {
		toShow = append(toShow, c.MsgTxsFiltered[i])
	}

	for rowIdx, txIdx1 := range toShow {

		row := ""
		txIdx1 = txIdx1 + 1
		if txIdx1 == c.CursorTx1 {
			// TODO keep idx using cell.SetReference(...) for the 1st cell in each row
			currTxRow = rowIdx
		}
		tx := c.MsgTxs[txIdx1-1]
		txParsed := c.MsgTxsParsed[txIdx1-1]
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
			d.matrix.SetCellSimple(rowIdx, ii, v)
			cell := d.matrix.GetCell(rowIdx, ii)
			cell.SetSelectable(true)

			// gray out some
			if !tx.Accepted || v == "." || v == "|" || v == "c" || v == "*" {
				cell.SetTextColor(colorHighlight)
			}

			// mark called states
			if slices.Contains(calledStates, name) {
				cell.SetAttributes(tcell.AttrUnderline)
			}

			if txIdx1 == c.CursorTx1 {
				// current tx
				cell.SetBackgroundColor(colorHighlight3)
			} else if d.C.SelectedState == name {
				// mark selected state
				cell.SetBackgroundColor(colorHighlight3)
			}

			if (sIsErr || name == am.StateException) && tx.Is1(index, name) {
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
		if txIdx1 > 1 {
			prevTStamp := c.MsgTxs[txIdx1-2].Time.Format(timeFormat)
			if idx := findFirstDiff(prevTStamp, tStamp); idx != -1 {
				tStampFmt = tStamp[:idx] + "[white]" + tStamp[idx:idx+1] + "[gray]" +
					tStamp[idx+1:]
			}
		}

		// tail cell
		d.matrix.SetCellSimple(rowIdx, len(index), fmt.Sprintf(
			"  [gray]%d | %s[-]", txIdx1, tStampFmt))
		tailCell := d.matrix.GetCell(rowIdx, len(index))

		// current tx
		if txIdx1 == c.CursorTx1 {
			tailCell.SetBackgroundColor(colorHighlight3)
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
		t := strconv.Itoa(int(c.MsgTxsParsed[c.CursorTx1-1].TimeSum))
		title += "Time:" + t + " "
	}
	d.matrix.SetTitle(title)

	if d.Mach.Is1(ss.TailMode) {
		// TODO restore column scroll
		d.matrix.ScrollToEnd()
	}
}

func (d *Debugger) hUpdateStatusBar() {
	c := d.C
	if c == nil {
		d.statusBar.SetText("")
		return
	}

	txt := ""
	if c.CursorStep1 > 0 {
		tx := c.MsgTxs[c.CursorTx1]
		step := tx.Steps[c.CursorStep1-1]
		txt = step.StringFromIndex(c.MsgStruct.StatesIndex)
	}

	// markdown to cview
	i := 0
	for strings.Contains(txt, "**") {
		rep := "[::b]"
		if i%2 == 1 {
			rep = "[::-]"
		}
		i++
		txt = strings.Replace(txt, "**", rep, 1)
	}
	d.statusBar.SetText(txt)
}

func (d *Debugger) hGetSidebarCurrClientIdx() int {
	if d.C == nil {
		return -1
	}

	i := 0
	for _, item := range d.clientList.GetItems() {
		ref := item.GetReference().(*sidebarRef)
		if ref.name == d.C.Id {
			return i
		}
		i++
	}

	return -1
}

// hFilterClientTxs filters client's txs according the selected
// toolbarItems. Called by toolbarItem states, not directly.
func (d *Debugger) hFilterClientTxs() {
	if d.C == nil || !d.filtersActive() {
		return
	}

	d.C.MsgTxsFiltered = nil
	for i := range d.C.MsgTxs {
		match := d.hFilterTx(d.C, i, d.filtersFromStates())
		if match {
			d.C.MsgTxsFiltered = append(d.C.MsgTxsFiltered, i)
		}
	}
}

func (d *Debugger) filtersFromStates() *OptsFilters {
	is := d.Mach.Is1

	return &OptsFilters{
		SkipCanceledTx:     is(ss.FilterCanceledTx),
		SkipAutoTx:         is(ss.FilterAutoTx),
		SkipAutoCanceledTx: is(ss.FilterAutoCanceledTx),
		SkipEmptyTx:        is(ss.FilterEmptyTx),
		SkipHealthTx:       is(ss.FilterHealth),
		SkipQueuedTx:       is(ss.FilterQueuedTx),
		SkipOutGroup:       is(ss.FilterOutGroup),
		SkipChecks:         is(ss.FilterChecks),
	}
}

// filtersActive checks if any filters are active.
func (d *Debugger) filtersActive() bool {
	return d.Mach.Any1(ss.GroupFilters...)
}

// hFilterTx returns true when a TX passes selected toolbarItems.
func (d *Debugger) hFilterTx(c *Client, idx int, filters *OptsFilters) bool {
	tx := c.MsgTxs[idx]
	parsed := c.MsgTxsParsed[idx]
	called := tx.CalledStateNames(c.MsgStruct.StatesIndex)
	group := c.SelectedGroup
	f := filters

	// basic filters
	if f.SkipAutoTx && tx.IsAuto {
		return false
	} else if f.SkipAutoCanceledTx && tx.IsAuto && !tx.Accepted {
		return false
	}
	if f.SkipCanceledTx && !tx.Accepted {
		return false
	}
	if f.SkipQueuedTx && tx.IsQueued {
		return false
	}
	if f.SkipChecks && tx.IsCheck {
		return false
	}

	// filter out txs without called from the group (if any)
	if f.SkipOutGroup && group != "" {
		groupStates := c.MsgSchemaParsed.Groups[group]
		if len(am.SameStates(called, groupStates)) == 0 {
			return false
		}
	}

	// skip empty (except queued and canceled)
	if f.SkipEmptyTx && parsed.TimeDiff == 0 && !tx.IsQueued && tx.Accepted {
		return false
	}

	// healthcheck
	if f.SkipHealthTx {
		health := S{ssam.BasicStates.Healthcheck, ssam.BasicStates.Heartbeat}
		if len(called) == 1 && slices.Contains(health, called[0]) {
			return false
		}
	}

	return true
}

func (d *Debugger) hScrollToTime(
	e *am.Event, hTime time.Time, filter bool,
) bool {
	if d.C == nil {
		return false
	}
	latestTx := d.C.LastTxTill(hTime)
	if latestTx == -1 {
		return false
	}

	if filter {
		latestTx = d.hFilterTxCursor1(d.C, latestTx, true)
	}
	d.hSetCursor1(e, am.A{
		"cursor1":    latestTx,
		"filterBack": true,
	})

	return true
}

func (d *Debugger) hGetParentTags(c *Client, tags []string) []string {
	parent, ok := d.Clients[c.MsgStruct.Parent]
	if !ok {
		return tags
	}
	tags = slices.Concat(tags, parent.MsgStruct.Tags)

	return d.hGetParentTags(parent, tags)
}

func (d *Debugger) initGraphGen(
	snapshot *amgraph.Graph, id string, detailsLvl, numClients int, outDir,
	svgName string, statesAllowlist S,
) []*amvis.Renderer {
	var vizs []*amvis.Renderer

	// render single (current one)
	vis := amvis.NewRenderer(snapshot, d.Mach.Log)
	amvis.PresetSingle(vis)
	// TODO enum
	switch detailsLvl {
	default:
		return vizs

		// single (simple)
	case 1:
		vis.RenderNestSubmachines = false
		vis.RenderStart = false
		vis.RenderInherited = false
		vis.RenderPipes = false
		vis.RenderHalfPipes = false
		vis.RenderHalfConns = false
		vis.RenderHalfHierarchy = false
		vis.RenderParentRel = false

		// single (detailed)
	case 2:
		vis.RenderNestSubmachines = false
		vis.RenderStart = true
		vis.RenderInherited = true
		vis.RenderPipes = false
		vis.RenderHalfPipes = false
		vis.RenderHalfConns = false
		vis.RenderHalfHierarchy = false

		// single (external)
	case 3:
		vis.RenderNestSubmachines = true
		vis.RenderStart = true
		vis.RenderInherited = true
		vis.RenderPipes = true
	}

	d.Mach.Log("rendering graphs lvl %d", detailsLvl)

	// machine diagram
	vis.RenderMachs = []string{id}
	vis.OutputFilename = path.Join(outDir, svgName)
	if len(statesAllowlist) > 0 {
		vis.RenderAllowlist = statesAllowlist
	}

	vizs = append(vizs, vis)

	// TODO render by prefixes
	// } else {
	// 	for _, p := range strings.Split(d.Opts.Graph, ",") {
	//
	// 		// vis
	// 		vis := amvis.NewRenderer(d.Mach, shot)
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

	// TODO render a mutation

	// map
	// TODO skip if there was no change in schemas
	//  hash schemas and schema's hashes, then compare

	// map renderer, check cache
	mapPath := fmt.Sprintf("am-vis-map-%d", numClients)
	if _, err := os.Stat(mapPath); err != nil {
		vis = amvis.NewRenderer(snapshot, d.Mach.Log)
		amvis.PresetMap(vis)
		vis.OutputFilename = path.Join(outDir, mapPath)
	}

	return append(vizs, vis)
}

func (d *Debugger) hSyncOptsTimelines() {
	switch d.Opts.Timelines {
	case 0:
		d.Mach.Add(S{ss.TimelineTxHidden, ss.TimelineStepsHidden}, nil)
	case 1:
		d.Mach.Add1(ss.TimelineStepsHidden, nil)
		d.Mach.Remove1(ss.TimelineTxHidden, nil)
	case 2:
		d.Mach.Remove(S{ss.TimelineStepsHidden, ss.TimelineTxHidden}, nil)
	}
}

func (d *Debugger) diagramsRender(
	e *am.Event, shot *amgraph.Graph, id string, details, clients int,
	outDir string, svgName string, states S,
) {
	ctx := d.Mach.NewStateCtx(ss.DiagramsRendering)
	if ctx.Err() != nil {
		return // expired
	}
	// mark rendering as in-progress
	d.Mach.EvRemove1(e, ss.DiagramsScheduled, nil)

	// create the visualizer
	vizs := d.initGraphGen(shot, id, details, clients, outDir, svgName, states)
	// TODO config
	pool := amhelp.Pool(2)

	for _, vis := range vizs {
		pool.Go(func() error {
			return vis.GenDiagrams(ctx)
		})
	}

	err := pool.Wait()
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}

	// link
	d.diagramLink(outDir, svgName)
	if ctx.Err() != nil {
		return // expired
	}

	// symlink am-vis-map.svg -> am-vis-map-CLIENTS.svg
	source := path.Join(outDir, "am-vis-map.svg")
	target := fmt.Sprintf("am-vis-map-%d.svg", clients)
	_ = os.Remove(source)
	err = os.Symlink(target, source)
	if ctx.Err() != nil {
		return // expired
	}
	d.Mach.EvAddErr(e, err, nil)

	// next
	d.Mach.EvAdd1(e, ss.DiagramsReady, nil)
}

func (d *Debugger) diagramLink(outDir string, svgName string) {
	// symlink am-vis.svg -> ID-LVL-HASH.svg
	source := path.Join(outDir, "am-vis.svg")
	target := fmt.Sprintf("%s.svg", svgName)
	_ = os.Remove(source)
	err := os.Symlink(target, source)
	d.Mach.AddErr(err, nil)
}

func (d *Debugger) diagramsMemCache(
	e *am.Event, id string, cache *goquery.Document, tx *telemetry.DbgMsgTx,
	outDir, svgName string,
) {
	svgPath := path.Join(outDir, svgName+".svg")
	ctx := d.Mach.NewStateCtx(ss.DiagramsRendering)
	if ctx.Err() != nil {
		return // expired
	}
	// mark rendering as in-progress
	d.Mach.EvRemove1(e, ss.DiagramsScheduled, nil)

	// update cache DOM
	states := d.C.MsgStruct.StatesIndex
	sel := amvis.Fragment{
		MachId: id,
		States: states,
		Active: tx.ActiveStates(states),
	}
	err := amvis.UpdateCache(ctx, svgPath, cache, &sel)
	if ctx.Err() != nil {
		return // expired
	}
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}

	// link
	d.diagramLink(outDir, svgName)
	if ctx.Err() != nil {
		return // expired
	}

	// next
	d.Mach.EvAdd1(e, ss.DiagramsReady, nil)
}

func (d *Debugger) diagramsFileCache(
	e *am.Event, id string, tx *telemetry.DbgMsgTx, lvl int, outDir,
	svgName string,
) {
	defer d.Mach.PanicToErrState(ss.ErrDiagrams, nil)

	svgPath := path.Join(outDir, svgName+".svg")
	ctx := d.Mach.NewStateCtx(ss.DiagramsRendering)
	if ctx.Err() != nil {
		return // expired
	}
	// mark rendering as in-progress
	d.Mach.EvRemove1(e, ss.DiagramsScheduled, nil)

	// read cache from a file
	file, err := os.Open(svgPath)
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}
	cache, err := goquery.NewDocumentFromReader(file)
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}

	// update cache DOM
	// TODO support groups
	states := d.C.MsgStruct.StatesIndex
	sel := amvis.Fragment{
		MachId: id,
		States: states,
	}
	if tx != nil {
		sel.Active = tx.ActiveStates(states)
	}

	err = amvis.UpdateCache(ctx, svgPath, cache, &sel)
	if ctx.Err() != nil {
		return // expired
	}
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}

	// link
	d.diagramLink(outDir, svgName)
	if ctx.Err() != nil {
		return // expired
	}

	// next
	d.Mach.EvAdd1(e, ss.DiagramsReady, am.A{
		// TODO typed args
		"Diagram.cache": cache,
		"Diagram.id":    id,
		"Diagram.lvl":   lvl,
	})
}

func (d *Debugger) getFocusColor() tcell.Color {
	color := cview.Styles.MoreContrastBackgroundColor
	if d.Mach.IsErr() {
		color = tcell.ColorRed
	}

	return color
}
