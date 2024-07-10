// Package debugger provides a TUI debugger with multi-client support. Runnable
// command can be found in tools/cmd/am-dbg.
package debugger

import (
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dsnet/compress/bzip2"
	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"
	"github.com/samber/lo"
	"golang.org/x/text/message"

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
	// TODO customize
	playInterval = 500 * time.Millisecond
	// TODO add param --max-clients
	maxClients            = 150
	timeFormat            = "15:04:05.000000000"
	fastJumpAmount        = 50
	arrowThrottleMs       = 200
	logUpdateDebounce     = 300 * time.Millisecond
	sidebarUpdateDebounce = time.Second
	searchAsTypeWindow    = 1500 * time.Millisecond
)

// TODO NewDebugger

type Debugger struct {
	am.ExceptionHandler
	Mach            *am.Machine
	Clients         map[string]*Client
	EnableMouse     bool
	CleanOnConnect  bool
	SelectConnected bool

	// current client
	C             *Client
	app           *cview.Application
	tree          *cview.TreeView
	treeRoot      *cview.TreeNode
	log           *cview.TextView
	timelineTxs   *cview.ProgressBar
	timelineSteps *cview.ProgressBar
	// TODO take from views
	focusable              []*cview.Box
	playTimer              *time.Ticker
	currTxBarRight         *cview.TextView
	currTxBarLeft          *cview.TextView
	nextTxBarLeft          *cview.TextView
	nextTxBarRight         *cview.TextView
	helpDialog             *cview.Flex
	keyBar                 *cview.TextView
	layoutRoot             *cview.Panels
	sidebar                *cview.List
	mainGrid               *cview.Grid
	URL                    string
	logRebuildEnd          int
	prevClientTxTime       time.Time
	repaintScheduled       bool
	updateSidebarScheduled bool
	lastKey                tcell.Key
	lastKeyTime            time.Time
	P                      *message.Printer
	updateLogScheduled     bool
	matrix                 *cview.Table
	focusManager           *cview.FocusManager
	exportDialog           *cview.Modal
	contentPanels          *cview.Panels
	filtersBar             *cview.TextView
	focusedFilter          string
	LogLevel               am.LogLevel
}

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
	CursorStep int

	id     string
	connID string
	// TODO extract from msgs
	lastActive    time.Time
	connected     bool
	selectedState string
	// processed
	msgTxsParsed []MsgTxParsed
	// processed list of filtered tx indexes
	msgTxsFiltered []int
	// processed
	logMsgs [][]*am.LogEntry
}

type MsgTxParsed struct {
	StatesAdded   am.S
	StatesRemoved am.S
	StatesTouched am.S
}

// ScrollToStateTx scrolls to the next transition involving the state being
// activated or deactivated. If fwd is true, it scrolls forward, otherwise
// backwards.
func (d *Debugger) ScrollToStateTx(state string, fwd bool) {
	c := d.C
	if c == nil {
		return
	}
	step := -1
	if fwd {
		step = 1
	}

	for i := c.CursorTx + step; i > 0 && i < len(c.MsgTxs)+1; i = i + step {

		parsed := c.msgTxsParsed[i-1]
		if !slices.Contains(parsed.StatesAdded, state) &&
			!slices.Contains(parsed.StatesRemoved, state) {
			continue
		}

		// scroll to this tx
		d.Mach.Add1(ss.ScrollToTx, am.A{"Client.cursorTx": i})
		d.Mach.Remove1(ss.TailMode, nil)
		break
	}
}

func (d *Debugger) NextTx() *telemetry.DbgMsgTx {
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

func (d *Debugger) CurrentTx() *telemetry.DbgMsgTx {
	c := d.C
	if c == nil {
		return nil
	}
	if c.CursorTx == 0 || len(c.MsgTxs) < c.CursorTx {
		return nil
	}
	return c.MsgTxs[c.CursorTx-1]
}

func (d *Debugger) PrevTx() *telemetry.DbgMsgTx {
	c := d.C
	if c == nil {
		return nil
	}
	if c.CursorTx < 2 {
		return nil
	}
	return c.MsgTxs[c.CursorTx-2]
}

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
			id:     "log-0",
			label:  "L0",
			active: d.LogLevel == am.LogNothing,
		},
		{
			id:     "log-1",
			label:  "L1",
			active: d.LogLevel == am.LogChanges,
		},
		{
			id:     "log-2",
			label:  "L2",
			active: d.LogLevel == am.LogOps,
		},
		{
			id:     "log-3",
			label:  "L3",
			active: d.LogLevel == am.LogDecisions,
		},
		{
			id:     "log-4",
			label:  "L4",
			active: d.LogLevel == am.LogEverything,
		},
	}

	// tx filters
	for _, item := range filters {
		if item.active {
			text += f(" [::b]%s[::-]", cview.Escape("[X]"))
		} else {
			text += f(" [ ]")
		}

		if d.focusedFilter == item.id && focused {
			text += f("[%s][::b]%s[::-]", colorActive, item.label)
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
	switch d.Mach.Switch(ss.GroupViews...) {

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

	if d.Mach.Is1(ss.StateNameSelected) {
		// state jump
		d.ScrollToStateTx(d.C.selectedState, false)
	} else {
		// fast jump
		d.Mach.Add1(ss.Back, am.A{"amount": min(fastJumpAmount, d.C.CursorTx)})
	}

	return nil
}

func (d *Debugger) jumpFwd(ev *tcell.EventKey) *tcell.EventKey {
	if d.throttleKey(ev, arrowThrottleMs) {
		return nil
	}

	if d.Mach.Is1(ss.StateNameSelected) {
		// state jump
		d.ScrollToStateTx(d.C.selectedState, true)
	} else {
		// fast jump
		d.Mach.Add1(ss.Fwd, am.A{
			"amount": min(fastJumpAmount, len(d.C.MsgTxs)-d.C.CursorTx),
		})
	}

	return nil
}

// TODO verify host token, to distinguish 2 hosts with the same ID
func (d *Debugger) parseMsg(c *Client, idx int) {
	msgTxParsed := MsgTxParsed{}
	msgTx := c.MsgTxs[idx]
	var entries []*am.LogEntry

	// added / removed
	if len(c.MsgTxs) > 1 && idx > 0 {
		prevTx := c.MsgTxs[idx-1]
		index := c.MsgStruct.StatesIndex

		for i, name := range index {
			if prevTx.Is1(index, name) && !msgTx.Is1(index, name) {
				msgTxParsed.StatesRemoved = append(msgTxParsed.StatesRemoved, name)
			} else if !prevTx.Is1(index, name) && msgTx.Is1(index, name) {
				msgTxParsed.StatesAdded = append(msgTxParsed.StatesAdded, name)
			} else if prevTx.Clocks[i] != msgTx.Clocks[i] {
				// treat multi states as added
				msgTxParsed.StatesAdded = append(msgTxParsed.StatesAdded, name)
			}
		}
	}

	// touched
	touched := am.S{}
	for _, step := range msgTx.Steps {
		if step.FromState != "" {
			touched = append(touched, step.FromState)
		}
		if step.ToState != "" {
			touched = append(touched, step.ToState)
		}
	}
	msgTxParsed.StatesTouched = lo.Uniq(touched)

	// store the parsed msg
	c.msgTxsParsed = append(c.msgTxsParsed, msgTxParsed)
	// TODO take from msgs
	c.lastActive = time.Now()

	d.parseMsgLog(c, msgTx, entries)
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
		d.currTxBarLeft.SetText("Listening for connections on " + d.URL)
		return
	}

	c := d.C
	tx := d.CurrentTx()
	if tx == nil {
		// c is nil when switching clients
		if c == nil || len(c.MsgTxs) == 0 {
			d.currTxBarLeft.SetText("No transitions yet...")
		} else {
			d.currTxBarLeft.SetText("Initial structure")
		}
	} else {

		var title string
		switch d.Mach.Switch(ss.GroupPlaying...) {
		case ss.Playing:
			title = formatTxBarTitle("Playing")
		case ss.TailMode:
			title += formatTxBarTitle("Tail") + "   "
		default:
			title = formatTxBarTitle("Paused") + " "
		}

		left, right := d.getTxInfo(c.CursorTx, tx, &c.msgTxsParsed[c.CursorTx-1],
			title)
		d.currTxBarLeft.SetText(left)
		d.currTxBarRight.SetText(right)
	}

	nextTx := d.NextTx()
	if nextTx != nil && c != nil {
		title := "Next   "
		left, right := d.getTxInfo(c.CursorTx+1, nextTx,
			&c.msgTxsParsed[c.CursorTx], title)
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
	nextTx := d.NextTx()
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

	// inactive steps bar when no next tx
	if nextTx == nil {
		d.timelineSteps.SetTitleColor(tcell.ColorGrey)
		d.timelineSteps.SetBorderColor(tcell.ColorGrey)
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

	// progressbar cant be max==0
	d.timelineSteps.SetMax(max(stepsCount, 1))
	// progress <= max
	d.timelineSteps.SetProgress(c.CursorStep)
	d.timelineSteps.SetTitle(fmt.Sprintf(
		" Next mutation step %d / %d ", c.CursorStep, stepsCount))
}

func (d *Debugger) updateSidebar(immediate bool) {
	if immediate {
		d.doUpdateSidebar()
		return
	}

	if d.updateSidebarScheduled {
		// debounce non-forced updates
		return
	}
	d.updateSidebarScheduled = true

	go func() {
		time.Sleep(sidebarUpdateDebounce)
		if !d.updateSidebarScheduled {
			return
		}
		d.doUpdateSidebar()
	}()
}

// TODO sometimes scrolls for no reason
func (d *Debugger) doUpdateSidebar() {
	if d.Mach.Disposed {
		return
	}

	for i, item := range d.sidebar.GetItems() {
		name := item.GetReference().(string)
		c := d.Clients[name]
		label := d.getSidebarLabel(name, c, i)
		item.SetMainText(label)
	}

	d.sidebar.SetTitle(" Machines:" + strconv.Itoa(len(d.Clients)) + " ")
	d.updateSidebarScheduled = false
	d.draw()
}

// buildSidebar builds the sidebar with the list of clients.
// selectedIndex: index of the selected item, -1 for the current one.
func (d *Debugger) buildSidebar(selectedIndex int) {
	selected := ""
	var item *cview.ListItem
	if selectedIndex == -1 {
		item = d.sidebar.GetCurrentItem()
	} else if selectedIndex > -1 {
		item = d.sidebar.GetItem(selectedIndex)
	}
	if item != nil {
		selected = item.GetReference().(string)
	}
	d.sidebar.Clear()
	var list []string
	for _, c := range d.Clients {
		list = append(list, c.id)
	}

	// sort a-z, with value numbers
	humanSort(list)

	for i, name := range list {
		c := d.Clients[name]
		label := d.getSidebarLabel(name, c, i)

		// create list item
		item := cview.NewListItem(label)
		item.SetReference(name)
		d.sidebar.AddItem(item)

		if selected == "" && d.C != nil && d.C.id == name {
			// pre-select the current machine
			d.sidebar.SetCurrentItem(i)
		} else if selected == name {
			// select the previously selected one
			d.sidebar.SetCurrentItem(i)
		}
	}

	d.sidebar.SetTitle(" Machines:" + strconv.Itoa(len(d.Clients)) + " ")
}

func (d *Debugger) getSidebarLabel(name string, c *Client, index int) string {
	label := d.P.Sprintf("%s (%d)", name, len(c.MsgTxs))
	if d.C != nil && name == d.C.id {
		label = "[::bu]" + label
	}

	isSel := d.sidebar.GetCurrentItemIndex() == index
	hasFocus := d.Mach.Is1(ss.SidebarFocused)
	if !c.connected {
		if isSel && !hasFocus {
			label = "[grey]" + label
		} else if !isSel {
			label = "[grey]" + label
		} else {
			label = "[black]" + label
		}
	}

	return label
}

func (d *Debugger) updateBorderColor() {
	color := cview.ColorUnset
	if d.C != nil && d.C.connected {
		color = colorActive
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
	gobPath := path.Join(cwd, filename+".gob.bz2")
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

	// create a new bzip2 writer
	bz2w, err := bzip2.NewWriter(fw, nil)
	if err != nil {
		log.Printf("Error: export failed %s", err)
		return
	}
	defer bz2w.Close()

	// encode
	encoder := gob.NewEncoder(bz2w)
	err = encoder.Encode(data)
	if err != nil {
		log.Printf("Error: export failed %s", err)
	}
}

// TODO async state
func (d *Debugger) ImportData(filename string) {
	fr, err := os.Open(filename)
	if err != nil {
		log.Printf("Error: import failed %s", err)
		return
	}

	// decompress bz2
	bz2r, err := bzip2.NewReader(fr, nil)
	if err != nil {
		log.Printf("Error: import failed %s", err)
		return
	}

	// decode gob
	decoder := gob.NewDecoder(bz2r)
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

	left += d.P.Sprintf(" | tx: %v", txIndex)
	left += d.P.Sprintf(" | T: +%v", tx.TimeSum()-prevT)
	left += " |"

	multi := ""
	if len(tx.CalledStates) == 1 &&
		d.C.MsgStruct.States[tx.CalledStates[0]].Multi {
		multi += " multi"
	}

	if !tx.Accepted {
		left += "[grey]"
	}
	left += fmt.Sprintf(" %s%s: [::b]%s[::-]", tx.Type, multi,
		strings.Join(tx.CalledStates, ", "))

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
		if !c.connected {
			disconns = append(disconns, c)
		}
	}
	// if all disconnected, clean up
	if len(disconns) == len(d.Clients) {
		for _, c := range d.Clients {
			// TODO cant be scheduled, as the client can connect in the meantime
			// d.Add1(ss.RemoveClient, am.A{"Client.id": c.id})
			delete(d.Clients, c.id)
			c.msgTxsParsed = nil
			c.logMsgs = nil
			c.MsgTxs = nil
			c.MsgStruct = nil
		}
		return true
	}
	return false
}

func (d *Debugger) updateMatrix() {
	d.matrix.Clear()
	d.matrix.SetTitle(" Matrix ")

	c := d.C
	if c == nil || d.C.CursorTx == 0 {
		d.matrix.Clear()
		return
	}

	index := c.MsgStruct.StatesIndex
	var tx *telemetry.DbgMsgTx
	var prevTx *telemetry.DbgMsgTx
	if c.CursorStep == 0 {
		tx = d.CurrentTx()
		prevTx = d.PrevTx()
	} else {
		tx = d.NextTx()
		prevTx = d.CurrentTx()
	}
	steps := tx.Steps

	// show the current tx summary on step 0, and partial if cursor > 0
	if c.CursorStep > 0 {
		steps = steps[:c.CursorStep]
	}

	highlightIndex := -1

	// called states
	var called []int
	for i, name := range index {

		v := "0"
		if slices.Contains(tx.CalledStates, name) {
			v = "1"
			called = append(called, i)
		}
		d.matrix.SetCellSimple(0, i, matrixCellVal(v))

		// mark called states
		if slices.Contains(tx.CalledStates, name) {
			d.matrix.GetCell(0, i).SetAttributes(tcell.AttrBold | tcell.AttrUnderline)
		}

		// mark selected state
		if d.C.selectedState == name {
			d.matrix.GetCell(0, i).SetBackgroundColor(colorHighlight)
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
		if d.C.selectedState == name {
			cell.SetBackgroundColor(colorHighlight)
		}
	}
	matrixEmptyRow(d, 3, len(index), highlightIndex)

	// steps
	for iRow, target := range index {
		for iCol, source := range index {
			v := 0

			for _, step := range steps {

				// TODO style just the cells
				if step.FromState == source &&
					((step.ToState == "" && source == target) ||
						step.ToState == target) {
					v += int(step.Type)
				}

				strVal := strconv.Itoa(v)
				strVal = matrixCellVal(strVal)
				d.matrix.SetCellSimple(iRow+4, iCol, strVal)
				cell := d.matrix.GetCell(iRow+4, iCol)

				// mark selected state
				if d.C.selectedState == target || d.C.selectedState == source {
					cell.SetBackgroundColor(colorHighlight)
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

	d.matrix.SetTitle(" Matrix:" + strconv.Itoa(sum) + " ")
}

func (d *Debugger) ConnectedClients() int {
	// if only 1 client connected, select it (if SelectConnected == true)
	var conns int
	for _, c := range d.Clients {
		if c.connected {
			conns++
		}
	}

	return conns
}

func (d *Debugger) getSidebarCurrClientIdx() int {
	if d.C == nil {
		return -1
	}

	i := 0
	for _, item := range d.sidebar.GetItems() {
		if item.GetReference().(string) == d.C.id {
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
		if d.filterTx(i, auto, empty, canceled) {
			d.C.msgTxsFiltered = append(d.C.msgTxsFiltered, i)
		}
	}
}

// isFiltered checks if any filter is active.
func (d *Debugger) isFiltered() bool {
	return d.Mach.Any1(ss.FilterCanceledTx, ss.FilterAutoTx, ss.FilterEmptyTx)
}

// filterTx returns true when a TX passed the passed filters.
func (d *Debugger) filterTx(idx int, auto, empty, canceled bool) bool {
	tx := d.C.MsgTxs[idx]

	if auto && tx.IsAuto {
		return false
	}
	if canceled && !tx.Accepted {
		return false
	}

	// empty
	parsed := d.C.msgTxsParsed[idx]
	txEmpty := true
	if empty && len(parsed.StatesAdded) == 0 &&
		len(parsed.StatesRemoved) == 0 {
		for _, step := range tx.Steps {
			// running a handler is not empty
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

func (d *Debugger) scrollToTime(t time.Time) bool {
	for i, tx := range d.C.MsgTxs {
		if !tx.Time.After(t) {
			continue
		}

		// pick the closer one
		if i > 0 && tx.Time.Sub(t) >
			t.Sub(*d.C.MsgTxs[i-1].Time) {
			d.C.CursorTx = d.filterTxCursor(d.C, i, true)
		} else {
			d.C.CursorTx = d.filterTxCursor(d.C, i+1, true)
		}

		return true
	}

	return false
}
