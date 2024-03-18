// Package debugger provides a TUI debugger with multi-client support. Runnable
// command can be found in tools/cmd/am-dbg.
package debugger

import (
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"code.rocketnine.space/tslocum/cbind"
	"github.com/dsnet/compress/bzip2"
	"github.com/gdamore/tcell/v2"
	"github.com/lithammer/dedent"
	"github.com/pancsta/cview"
	"github.com/samber/lo"
	"golang.org/x/exp/maps"
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
	keystrokesBar          *cview.TextView
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
	// processed
	logMsgs []string
}

type MsgTxParsed struct {
	StatesAdded   am.S
	StatesRemoved am.S
	StatesTouched am.S
}

type nodeRef struct {
	// TODO type
	// type nodeType
	// node is a state (reference or top level)
	stateName string
	// node is a state reference, not a top level state
	// eg Bar in case of: Foo -> Remove -> Bar
	// TODO name collision with nodeRef
	isRef bool
	// node is a relation (Remove, Add, Require, After)
	isRel bool
	// relation type (if isRel)
	rel am.Relation
	// top level state name (for both rels and refs)
	parentState string
	// node touched by a transition step
	touched bool
	// expanded by the user
	expanded bool
	// node is a state property (Auto, Multi)
	isProp    bool
	propLabel string
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

// RedrawFull updates all components of the debugger UI, except the sidebar.
func (d *Debugger) RedrawFull(immediate bool) {
	d.updateViews(immediate)
	d.updateTimelines()
	d.updateTxBars()
	d.updateKeyBars()
	d.updateBorderColor()
	d.draw()
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

func (d *Debugger) initUIComponents() {
	d.helpDialog = d.initHelpDialog()
	d.exportDialog = d.initExportDialog()

	// tree view
	d.tree = d.initMachineTree()
	d.tree.SetTitle(" Structure ")
	d.tree.SetBorder(true)

	// sidebar
	d.sidebar = cview.NewList()
	d.sidebar.SetTitle(" Machines ")
	d.sidebar.SetBorder(true)
	d.sidebar.ShowSecondaryText(false)
	d.sidebar.SetSelectedFocusOnly(true)
	d.sidebar.SetMainTextColor(colorActive)
	d.sidebar.SetSelectedTextColor(tcell.ColorWhite)
	d.sidebar.SetSelectedBackgroundColor(colorHighlight2)
	d.sidebar.SetHighlightFullLine(true)
	d.sidebar.SetSelectedFunc(func(i int, listItem *cview.ListItem) {
		cid := listItem.GetReference().(string)
		d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": cid})
	})
	d.sidebar.SetSelectedAlwaysVisible(true)

	// log view
	d.log = cview.NewTextView()
	d.log.SetBorder(true)
	d.log.SetRegions(true)
	d.log.SetTextAlign(cview.AlignLeft)
	d.log.SetWrap(true)
	d.log.SetDynamicColors(true)
	d.log.SetTitle(" Log ")
	d.log.SetHighlightForegroundColor(tcell.ColorWhite)
	d.log.SetHighlightBackgroundColor(colorHighlight2)

	// matrix
	d.matrix = cview.NewTable()
	d.matrix.SetBorder(true)
	d.matrix.SetTitle(" Matrix ")
	// TODO draw scrollbar at the right edge
	d.matrix.SetScrollBarVisibility(cview.ScrollBarNever)
	d.matrix.SetPadding(0, 0, 1, 0)

	// current tx bar
	d.currTxBarLeft = cview.NewTextView()
	d.currTxBarLeft.SetDynamicColors(true)
	d.currTxBarRight = cview.NewTextView()
	d.currTxBarRight.SetTextAlign(cview.AlignRight)

	// next tx bar
	d.nextTxBarLeft = cview.NewTextView()
	d.nextTxBarLeft.SetDynamicColors(true)
	d.nextTxBarRight = cview.NewTextView()
	d.nextTxBarRight.SetTextAlign(cview.AlignRight)

	// TODO step info bar: type, from, to, data
	// timeline tx
	d.timelineTxs = cview.NewProgressBar()
	d.timelineTxs.SetBorder(true)

	// timeline steps
	d.timelineSteps = cview.NewProgressBar()
	d.timelineSteps.SetBorder(true)

	// keystrokes bar
	d.keystrokesBar = cview.NewTextView()
	d.keystrokesBar.SetTextAlign(cview.AlignCenter)
	d.keystrokesBar.SetDynamicColors(true)

	// update models
	d.updateTimelines()
	d.updateTxBars()
	d.updateKeyBars()
	d.updateFocusable()
}

func (d *Debugger) initLayout() {
	// TODO flexbox
	currTxBar := cview.NewGrid()
	currTxBar.AddItem(d.currTxBarLeft, 0, 0, 1, 1, 0, 0, false)
	currTxBar.AddItem(d.currTxBarRight, 0, 1, 1, 1, 0, 0, false)

	// TODO flexbox
	nextTxBar := cview.NewGrid()
	nextTxBar.AddItem(d.nextTxBarLeft, 0, 0, 1, 1, 0, 0, false)
	nextTxBar.AddItem(d.nextTxBarRight, 0, 1, 1, 1, 0, 0, false)

	// content grid
	treeLogGrid := cview.NewGrid()
	treeLogGrid.SetRows(-1)
	treeLogGrid.SetColumns( /*tree*/ -1 /*log*/, -1, -1)
	treeLogGrid.AddItem(d.tree, 0, 0, 1, 1, 0, 0, false)
	treeLogGrid.AddItem(d.log, 0, 1, 1, 2, 0, 0, false)

	treeMatrixGrid := cview.NewGrid()
	treeMatrixGrid.SetRows(-1)
	treeMatrixGrid.SetColumns( /*tree*/ -1 /*log*/, -1, -1)
	treeMatrixGrid.AddItem(d.tree, 0, 0, 1, 1, 0, 0, false)
	treeMatrixGrid.AddItem(d.matrix, 0, 1, 1, 2, 0, 0, false)

	// content panels
	d.contentPanels = cview.NewPanels()
	d.contentPanels.AddPanel("tree-log", treeLogGrid, true, true)
	d.contentPanels.AddPanel("tree-matrix", treeMatrixGrid, true, false)
	d.contentPanels.AddPanel("matrix", d.matrix, true, false)
	d.contentPanels.SetBackgroundColor(colorHighlight)

	// main grid
	mainGrid := cview.NewGrid()
	mainGrid.SetRows(-1, 2, 3, 2, 3, 2)
	cols := []int{ /*sidebar*/ -1 /*content*/, -1, -1, -1, -1, -1, -1}
	mainGrid.SetColumns(cols...)
	// row 1 left
	mainGrid.AddItem(d.sidebar, 0, 0, 1, 1, 0, 0, false)
	// row 1 mid, right
	mainGrid.AddItem(d.contentPanels, 0, 1, 1, 6, 0, 0, false)
	// row 2...5
	mainGrid.AddItem(currTxBar, 1, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.timelineTxs, 2, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(nextTxBar, 3, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.timelineSteps, 4, 0, 1, len(cols), 0, 0, false)
	mainGrid.AddItem(d.keystrokesBar, 5, 0, 1, len(cols), 0, 0, false)

	panels := cview.NewPanels()
	panels.AddPanel("export", d.exportDialog, false, true)
	panels.AddPanel("help", d.helpDialog, true, true)
	panels.AddPanel("main", mainGrid, true, true)

	d.mainGrid = mainGrid
	d.layoutRoot = panels
}

type Focusable struct {
	cview.Primitive
	*cview.Box
}

// TODO feat: support dialogs
// TODO refac: generate d.focusable list via GetFocusable
// TODO fix: preserve currently focused element
func (d *Debugger) updateFocusable() {
	if d.focusManager == nil {
		d.Mach.Log("Error: focus manager not initialized")
		return
	}

	var prims []cview.Primitive
	switch d.Mach.Switch(ss.GroupViews...) {

	case ss.MatrixView:
		d.focusable = []*cview.Box{
			d.sidebar.Box, d.matrix.Box, d.timelineTxs.Box, d.timelineSteps.Box,
		}
		prims = []cview.Primitive{
			d.sidebar, d.matrix, d.timelineTxs,
			d.timelineSteps,
		}

	case ss.TreeMatrixView:
		d.focusable = []*cview.Box{
			d.sidebar.Box, d.tree.Box, d.matrix.Box, d.timelineTxs.Box,
			d.timelineSteps.Box,
		}
		prims = []cview.Primitive{
			d.sidebar, d.tree, d.matrix, d.timelineTxs,
			d.timelineSteps,
		}

	case ss.TreeLogView:
		fallthrough
	default:
		d.focusable = []*cview.Box{
			d.sidebar.Box, d.tree.Box, d.log.Box, d.timelineTxs.Box,
			d.timelineSteps.Box,
		}
		prims = []cview.Primitive{
			d.sidebar, d.tree, d.log, d.timelineTxs,
			d.timelineSteps,
		}

	}

	d.focusManager.Reset()
	d.focusManager.Add(prims...)

	switch d.Mach.Switch(ss.GroupFocused...) {
	case ss.SidebarFocused:
		d.focusManager.Focus(d.sidebar)
	case ss.TreeFocused:
		if d.Mach.Any1(ss.TreeMatrixView, ss.TreeLogView) {
			d.focusManager.Focus(d.tree)
		} else {
			d.focusManager.Focus(d.sidebar)
		}
	case ss.LogFocused:
		if d.Mach.Is1(ss.TreeLogView) {
			d.focusManager.Focus(d.tree)
		} else {
			d.focusManager.Focus(d.sidebar)
		}
		d.focusManager.Focus(d.log)
	case ss.MatrixFocused:
		if d.Mach.Any1(ss.TreeMatrixView, ss.MatrixView) {
			d.focusManager.Focus(d.matrix)
		} else {
			d.focusManager.Focus(d.sidebar)
		}
	case ss.TimelineTxsFocused:
		d.focusManager.Focus(d.timelineTxs)
	case ss.TimelineStepsFocused:
		d.focusManager.Focus(d.timelineSteps)
	default:
		d.focusManager.Focus(d.sidebar)
	}
}

// TODO tab navigation
// TODO delegated flow machine?
func (d *Debugger) initExportDialog() *cview.Modal {
	exportDialog := cview.NewModal()
	form := exportDialog.GetForm()
	form.AddInputField("Filename", "am-dbg-dump", 20, nil, nil)

	exportDialog.SetText("Export all clients data (esc quits)")
	exportDialog.AddButtons([]string{"Save"})
	// TODO support cancel
	// exportDialog.AddButtons([]string{"Save", "Cancel"})
	exportDialog.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		field, ok := form.GetFormItemByLabel("Filename").(*cview.InputField)
		if !ok {
			d.Mach.Log("Error: export dialog field not found")
			return
		}
		filename := field.GetText()

		if buttonLabel == "Save" && filename != "" {
			form.GetButton(0).SetLabel("Saving...")
			form.Draw(d.app.GetScreen())
			d.exportData(filename)
			d.Mach.Remove1(ss.ExportDialog, nil)
			form.GetButton(0).SetLabel("Save")
		} else if buttonLabel == "Cancel" {
			d.Mach.Remove1(ss.ExportDialog, nil)
		}
	})

	return exportDialog
}

// TODO better approach to modals
// TODO modal titles
// TODO state color meanings
// TODO page up/down on tx timeline
func (d *Debugger) initHelpDialog() *cview.Flex {
	left := cview.NewTextView()
	left.SetBackgroundColor(colorHighlight)
	left.SetTitle(" Legend ")
	left.SetDynamicColors(true)
	left.SetPadding(1, 1, 1, 1)
	left.SetText(dedent.Dedent(strings.Trim(`
		[::b]### [::u]tree legend[::-]
	
		[::b]*[::-]            handler ran
		[::b]+[::-]            to be added
		[::b]-[::-]            to be removed
		[::b]bold[::-]         touched state
		[::b]underline[::-]    called state
		[::b]![::-]            state canceled
	
		[::b]### [::u]matrix legend[::-]
	
		[::b]underline[::-]  called state
	
		[::b]1st row[::-]    called states
		           col == state index
	
		[::b]2nd row[::-]    state tick changes
		           col == state index
	
		[::b]>=3 row[::-]    state relations
		           cartesian product
		           col == source state index
		           row == target state index
	
	`, "\n ")))

	right := cview.NewTextView()
	right.SetBackgroundColor(colorHighlight)
	right.SetTitle(" Keystrokes ")
	right.SetDynamicColors(true)
	right.SetPadding(1, 1, 1, 1)
	right.SetText(dedent.Dedent(strings.Trim(`
		[::b]### [::u]keystrokes[::-]
	
		[::b]tab[::-]                change focus
		[::b]shift+tab[::-]          change focus
		[::b]space[::-]              play/pause
		[::b]left/right[::-]         back/fwd
		[::b]alt+left/right[::-]     fast jump
		[::b]alt+h/l[::-]            fast jump
		[::b]alt+h/l[::-]            state jump (if selected)
		[::b]up/down[::-]            scroll / navigate
		[::b]j/k[::-]                scroll / navigate
		[::b]alt+j/k[::-]            page up/down
		[::b]alt+e[::-]              expand/collapse tree
		[::b]enter[::-]              expand/collapse node
		[::b]alt+v[::-]              tail mode
		[::b]alt+m[::-]              matrix view
		[::b]home/end[::-]           struct / last tx
		[::b]alt+s[::-]              export data
		[::b]backspace[::-]          remove machine
		[::b]ctrl+q[::-]             quit
		[::b]?[::-]                  show help
	`, "\n ")))

	grid := cview.NewGrid()
	grid.SetTitle(" AsyncMachine Debugger ")
	grid.SetColumns(0, 0)
	grid.SetRows(0)
	grid.AddItem(left, 0, 0, 1, 1, 0, 0, false)
	grid.AddItem(right, 0, 1, 1, 1, 0, 0, false)

	box1 := cview.NewBox()
	box1.SetBackgroundTransparent(true)
	box2 := cview.NewBox()
	box2.SetBackgroundTransparent(true)
	box3 := cview.NewBox()
	box3.SetBackgroundTransparent(true)
	box4 := cview.NewBox()
	box4.SetBackgroundTransparent(true)

	flexHor := cview.NewFlex()
	flexHor.AddItem(box1, 0, 1, false)
	flexHor.AddItem(grid, 0, 2, false)
	flexHor.AddItem(box2, 0, 1, false)

	flexVer := cview.NewFlex()
	flexVer.SetDirection(cview.FlexRow)
	flexVer.AddItem(box3, 0, 1, false)
	flexVer.AddItem(flexHor, 0, 2, false)
	flexVer.AddItem(box4, 0, 1, false)

	return flexVer
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

func (d *Debugger) bindKeyboard() {
	// focus manager
	d.focusManager = cview.NewFocusManager(d.app.SetFocus)
	d.focusManager.SetWrapAround(true)
	inputHandler := cbind.NewConfiguration()
	d.app.SetAfterFocusFunc(d.afterFocus())

	focusChange := func(f func()) func(ev *tcell.EventKey) *tcell.EventKey {
		return func(ev *tcell.EventKey) *tcell.EventKey {
			// keep Tab inside dialogs
			if d.Mach.Any1(ss.GroupDialog...) {
				return ev
			}

			// fwd to FocusManager
			f()
			return nil
		}
	}

	// tab
	for _, key := range cview.Keys.MovePreviousField {
		err := inputHandler.Set(key, focusChange(d.focusManager.FocusPrevious))
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}

	// shift+tab
	for _, key := range cview.Keys.MoveNextField {
		err := inputHandler.Set(key, focusChange(d.focusManager.FocusNext))
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}

	// custom keys
	for key, fn := range d.getKeystrokes() {
		err := inputHandler.Set(key, fn)
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}

	d.searchTreeSidebar(inputHandler)
	d.app.SetInputCapture(inputHandler.Capture)
}

// afterFocus forwards focus events to machine states
func (d *Debugger) afterFocus() func(p cview.Primitive) {
	return func(p cview.Primitive) {
		switch p {

		case d.tree:
			fallthrough
		case d.tree.Box:
			d.Mach.Add1(ss.TreeFocused, nil)

		case d.log:
			fallthrough
		case d.log.Box:
			d.Mach.Add1(ss.LogFocused, nil)

		case d.timelineTxs:
			fallthrough
		case d.timelineTxs.Box:
			d.Mach.Add1(ss.TimelineTxsFocused, nil)

		case d.timelineSteps:
			fallthrough
		case d.timelineSteps.Box:
			d.Mach.Add1(ss.TimelineStepsFocused, nil)

		case d.sidebar:
			fallthrough
		case d.sidebar.Box:
			d.Mach.Add1(ss.SidebarFocused, nil)

		case d.matrix:
			fallthrough
		case d.matrix.Box:
			d.Mach.Add1(ss.MatrixFocused, nil)

		case d.helpDialog:
			fallthrough
		case d.helpDialog.Box:
			fallthrough
		case d.exportDialog:
			fallthrough
		case d.exportDialog.Box:
			d.Mach.Add1(ss.DialogFocused, nil)
		}

		// update the log highlight on focus change
		if d.Mach.Is1(ss.TreeLogView) {
			d.updateLog(true)
		}

		d.updateSidebar(true)
	}
}

// searchTreeSidebar searches for a-z, -, _ in the tree and sidebar, with a
// searchAsTypeWindow buffer.
func (d *Debugger) searchTreeSidebar(inputHandler *cbind.Configuration) {
	var (
		bufferStart time.Time
		buffer      string
		keys        = []string{"-", "_"}
	)

	for i := 0; i < 26; i++ {
		keys = append(keys,
			fmt.Sprintf("%c", 'a'+i))
	}

	for _, key := range keys {
		key := key
		err := inputHandler.Set(key, func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not(am.S{ss.SidebarFocused, ss.TreeFocused}) {
				return ev
			}

			// buffer
			if bufferStart.Add(searchAsTypeWindow).After(time.Now()) {
				buffer += key
			} else {
				bufferStart = time.Now()
				buffer = key
			}

			// find the first client starting with the key

			// sidebar
			if d.Mach.Is1(ss.SidebarFocused) {
				for i, item := range d.sidebar.GetItems() {
					text := normalizeText(item.GetMainText())
					if strings.HasPrefix(text, buffer) {
						d.sidebar.SetCurrentItem(i)
						d.updateSidebar(true)

						d.draw()
						break
					}
				}
			} else if d.Mach.Is1(ss.TreeFocused) {

				// tree
				found := false
				d.treeRoot.WalkUnsafe(
					func(node, parent *cview.TreeNode, depth int) bool {
						if found {
							return false
						}

						text := normalizeText(node.GetText())

						if parent != nil && parent.IsExpanded() &&
							strings.HasPrefix(text, buffer) {
							d.Mach.Remove1(ss.StateNameSelected, nil)
							d.tree.SetCurrentNode(node)
							d.updateTree()
							d.draw()
							found = true

							return false
						}

						return true
					})
			}

			return nil
		})
		if err != nil {
			log.Printf("Error: binding keys %s", err)
		}
	}
}

// regexp removing [foo]
var re = regexp.MustCompile(`\[(.*?)\]`)

func normalizeText(text string) string {
	return strings.ToLower(re.ReplaceAllString(text, ""))
}

func (d *Debugger) getKeystrokes() map[string]func(
	ev *tcell.EventKey) *tcell.EventKey {
	// TODO add state deps to the keystrokes structure
	// TODO use tcell.KeyNames instead of strings as keys
	// TODO rate limit
	return map[string]func(ev *tcell.EventKey) *tcell.EventKey{
		// play/pause
		"space": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}

			if d.Mach.Is1(ss.Paused) {
				d.Mach.Add1(ss.Playing, nil)
			} else {
				d.Mach.Add1(ss.Paused, nil)
			}

			return nil
		},

		// prev tx
		"left": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}
			if d.throttleKey(ev, arrowThrottleMs) {
				// TODO fast jump scroll while holding the key
				return nil
			}

			// scroll matrix
			if d.Mach.Is1(ss.MatrixFocused) {
				return ev
			}

			// scroll timelines
			if d.Mach.Is1(ss.TimelineStepsFocused) {
				d.Mach.Add1(ss.UserBackStep, nil)
			} else {
				d.Mach.Add1(ss.UserBack, nil)
			}

			return nil
		},

		// next tx
		"right": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}
			if d.throttleKey(ev, arrowThrottleMs) {
				// TODO fast jump scroll while holding the key
				return nil
			}

			// scroll matrix
			if d.Mach.Is1(ss.MatrixFocused) {
				return ev
			}

			// scroll timelines
			if d.Mach.Is1(ss.TimelineStepsFocused) {
				// TODO try mach.IsScheduled(ss.UserFwdStep, am.MutationTypeAdd)
				d.Mach.Add1(ss.UserFwdStep, nil)
			} else {
				d.Mach.Add1(ss.UserFwd, nil)
			}

			return nil
		},

		// state jumps
		"alt+h":     d.jumpBack,
		"alt+l":     d.jumpFwd,
		"alt+Left":  d.jumpBack,
		"alt+Right": d.jumpFwd,

		// page up / down
		"alt+j": func(ev *tcell.EventKey) *tcell.EventKey {
			return tcell.NewEventKey(tcell.KeyPgDn, ' ', tcell.ModNone)
		},
		"alt+k": func(ev *tcell.EventKey) *tcell.EventKey {
			return tcell.NewEventKey(tcell.KeyPgUp, ' ', tcell.ModNone)
		},

		// expand / collapse the tree root
		"alt+e": func(ev *tcell.EventKey) *tcell.EventKey {
			expanded := false
			children := d.tree.GetRoot().GetChildren()

			for _, child := range children {
				if child.IsExpanded() {
					expanded = true
					break
				}
				child.Collapse()
			}

			for _, child := range children {
				if expanded {
					child.Collapse()
					child.GetReference().(*nodeRef).expanded = false
				} else {
					child.Expand()
					child.GetReference().(*nodeRef).expanded = true
				}
			}

			return nil
		},

		// tail mode
		"alt+v": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Add1(ss.TailMode, nil)

			return nil
		},

		// matrix view
		"alt+m": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is1(ss.TreeLogView) {
				d.Mach.Add1(ss.MatrixView, nil)
			} else if d.Mach.Is1(ss.MatrixView) {
				d.Mach.Add1(ss.TreeMatrixView, nil)
			} else {
				d.Mach.Add1(ss.TreeLogView, nil)
			}

			return nil
		},

		// scroll to the first tx
		"home": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}
			d.C.CursorTx = 0
			d.RedrawFull(true)

			return nil
		},

		// scroll to the last tx
		"end": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ClientSelected) {
				return nil
			}
			d.C.CursorTx = len(d.C.MsgTxs)
			d.RedrawFull(true)

			return nil
		},

		// quit the app
		"ctrl+q": func(ev *tcell.EventKey) *tcell.EventKey {
			d.Mach.Remove1(ss.Start, nil)

			return nil
		},

		// help modal
		"?": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.HelpDialog) {
				d.Mach.Add1(ss.HelpDialog, nil)
			} else {
				d.Mach.Remove(ss.GroupDialog, nil)
			}

			return ev
		},

		// export modal
		"alt+s": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.ExportDialog) {
				d.Mach.Add1(ss.ExportDialog, nil)
			} else {
				d.Mach.Remove(ss.GroupDialog, nil)
			}

			return ev
		},

		// exit modals
		"esc": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Any1(ss.GroupDialog...) {
				d.Mach.Remove(ss.GroupDialog, nil)
				return nil
			}

			return ev
		},

		// remove client (sidebar)
		"backspace": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Not1(ss.SidebarFocused) {
				return ev
			}

			sel := d.sidebar.GetCurrentItem()
			if sel == nil || d.Mach.Not1(ss.SidebarFocused) {
				return nil
			}

			cid := sel.GetReference().(string)
			d.Mach.Add1(ss.RemoveClient, am.A{"Client.id": cid})

			return nil
		},

		// scroll to LogScrolled
		// scroll sidebar
		"down": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is1(ss.SidebarFocused) {
				// TODO state?
				go func() {
					d.updateSidebar(true)
					d.draw()
				}()
			} else if d.Mach.Is1(ss.LogFocused) {
				d.Mach.Add1(ss.LogUserScrolled, nil)
			}

			return ev
		},
		"up": func(ev *tcell.EventKey) *tcell.EventKey {
			if d.Mach.Is1(ss.SidebarFocused) {
				// TODO state?
				go func() {
					d.updateSidebar(true)
					d.draw()
				}()
			} else if d.Mach.Is1(ss.LogFocused) {
				d.Mach.Add1(ss.LogUserScrolled, nil)
			}

			return ev
		},
	}
}

// TODO optimize usage places
func (d *Debugger) throttleKey(ev *tcell.EventKey, ms int) bool {
	// throttle
	sameKey := d.lastKey == ev.Key()
	elapsed := time.Since(d.lastKeyTime)
	if sameKey && elapsed < time.Duration(ms)*time.Millisecond {
		return true
	}

	d.lastKey = ev.Key()
	d.lastKeyTime = time.Now()

	return false
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
func (d *Debugger) parseClientMsg(c *Client, idx int) {
	msgTxParsed := MsgTxParsed{}
	msgTx := c.MsgTxs[idx]

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

	// pre-log entries
	logStr := ""
	for _, entry := range msgTx.PreLogEntries {
		logStr += fmtLogEntry(entry, c.MsgStruct.States)
	}

	// tx log entries
	if len(msgTx.LogEntries) > 0 {
		f := ""
		for _, entry := range msgTx.LogEntries {
			f += fmtLogEntry(entry, c.MsgStruct.States)
		}
		// create a highlight region
		logStr += `["` + msgTx.ID + `"]` + f + `[""]`
	}

	// TODO highlight the selected state

	// store the parsed log
	c.logMsgs = append(c.logMsgs, logStr)
}

func (d *Debugger) appendLog(index int) error {
	c := d.C
	logStr := c.logMsgs[index]
	if logStr == "" {
		return nil
	}
	if index > 0 {
		msgTime := c.MsgTxs[index].Time
		prevMsgTime := c.MsgTxs[index-1].Time
		if prevMsgTime.Second() != msgTime.Second() {
			// grouping labels (per second)
			// TODO [::-]
			logStr = `[grey]` + msgTime.Format(timeFormat) + "[white]\n" + logStr
		}
	}
	_, err := d.log.Write([]byte(logStr))
	if err != nil {
		return err
	}

	// prevent scrolling when not in tail mode
	if d.Mach.Not(am.S{ss.TailMode, ss.LogUserScrolled}) {
		d.log.ScrollToHighlight()
	} else if d.Mach.Not1(ss.TailMode) {
		d.log.ScrollToEnd()
	}

	return nil
}

func (d *Debugger) draw() {
	if d.repaintScheduled {
		return
	}
	d.repaintScheduled = true

	go func() {
		// debounce every 16msec
		time.Sleep(16 * time.Millisecond)
		// TODO re-draw only changed components
		d.app.QueueUpdateDraw(func() {})
		d.repaintScheduled = false
	}()
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
	if nextTx != nil {
		title := "Next   "
		left, right := d.getTxInfo(c.CursorTx+1, nextTx,
			&c.msgTxsParsed[c.CursorTx], title)
		d.nextTxBarLeft.SetText(left)
		d.nextTxBarRight.SetText(right)
	}
}

func (d *Debugger) updateLog(immediate bool) {
	if immediate {
		d.doUpdateLog()
		return
	}

	if d.updateLogScheduled {
		return
	}
	d.updateLogScheduled = true

	go func() {
		time.Sleep(logUpdateDebounce)
		d.doUpdateLog()
		d.draw()
		d.updateLogScheduled = false
	}()
}

func (d *Debugger) doUpdateLog() {
	// check for a ready client
	c := d.C
	if c == nil {
		return
	}

	if c.MsgStruct != nil {
		lvl := c.MsgStruct.LogLevel
		d.log.SetTitle(" Log:" + lvl.String() + " ")
	}

	// highlight the next tx if scrolling by steps
	bySteps := d.Mach.Is1(ss.TimelineStepsFocused)
	tx := d.CurrentTx()
	if bySteps {
		tx = d.NextTx()
	}
	if tx == nil {
		d.log.Highlight("")
		if bySteps {
			d.log.ScrollToEnd()
		} else {
			d.log.ScrollToBeginning()
		}

		return
	}

	// highlight this tx or the prev if empty
	if len(tx.LogEntries) == 0 && d.PrevTx() != nil {
		last := d.PrevTx()
		for i := d.C.CursorTx - 1; i >= 0; i-- {
			if len(last.LogEntries) > 0 {
				tx = last
				break
			}
			last = d.C.MsgTxs[i-1]
		}

		d.log.Highlight(last.ID)
	} else {
		d.log.Highlight(tx.ID)
	}

	// scroll, but only if not manually scrolled
	if d.Mach.Not1(ss.LogUserScrolled) {
		d.log.ScrollToHighlight()
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
	d.timelineTxs.SetTitle(d.P.Sprintf(
		" Transition %d / %d ", c.CursorTx, txCount))

	// progressbar cant be max==0
	d.timelineSteps.SetMax(max(stepsCount, 1))
	// progress <= max
	d.timelineSteps.SetProgress(c.CursorStep)
	d.timelineSteps.SetTitle(fmt.Sprintf(
		" Next mutation step %d / %d ", c.CursorStep, stepsCount))
}

func (d *Debugger) updateKeyBars() {
	// TODO light mode
	keys := "[yellow]space: play/pause | left/right: back/fwd | " +
		"alt+left/right/h/l: fast/state jump | home/end: start/end | " +
		"alt+e/enter: expand/collapse | tab: focus | alt+v: tail mode | " +
		"alt+m: matrix view | alt+s: export | ?: help"
	d.keystrokesBar.SetText(keys)
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
			d.parseClientMsg(d.Clients[id], i)
		}
	}
}

///// ///// ///// ///// /////
///// UTILS /////
///// ///// ///// ///// /////

func fmtLogEntry(entry string, machStruct am.Struct) string {
	if entry == "" {
		return entry
	}

	prefixEnd := "[][white]"

	// color the first brackets per each line
	ret := ""
	for _, s := range strings.Split(entry, "\n") {
		s = strings.Replace(strings.Replace(s,
			"]", prefixEnd, 1),
			"[", "[yellow][", 1)
		ret += s + "\n"
	}

	// highlight state names (in the msg body)
	// TODO removes the last letter
	idx := strings.Index(ret, prefixEnd)
	prefix := ret[0 : idx+len(prefixEnd)]

	// style state names, start from the longest ones
	// TODO compile as regexp and limit to words only
	toReplace := maps.Keys(machStruct)
	slices.Sort(toReplace)
	slices.Reverse(toReplace)
	for _, name := range toReplace {
		body := ret[idx+len(prefixEnd):]
		body = strings.ReplaceAll(body, " "+name, " [::b]"+name+"[::-]")
		body = strings.ReplaceAll(body, "+"+name, "+[::b]"+name+"[::-]")
		body = strings.ReplaceAll(body, "-"+name, "-[::b]"+name+"[::-]")
		body = strings.ReplaceAll(body, ","+name, ",[::b]"+name+"[::-]")
		ret = prefix + strings.ReplaceAll(body, "("+name, "([::b]"+name+"[::-]")
	}

	return strings.Trim(ret, " \n	") + "\n"
}

func (d *Debugger) getTxInfo(txIndex int,
	tx *telemetry.DbgMsgTx, parsed *MsgTxParsed, title string,
) (string, string) {
	// left side
	left := title
	if tx != nil {

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
	}

	// right side
	// TODO time to execute
	right := " "
	if tx.IsAuto {
		right += "auto | "
	}
	if !tx.Accepted {
		right += "rejected | "
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

// TODO
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

func matrixCellVal(strVal string) string {
	switch len(strVal) {
	case 1:
		strVal = " " + strVal + " "
	case 2:
		strVal = " " + strVal
	}
	return strVal
}

func matrixEmptyRow(d *Debugger, row, colsCount, highlightIndex int) {
	// empty row
	for ii := 0; ii < colsCount; ii++ {
		d.matrix.SetCellSimple(row, ii, "   ")
		if ii == highlightIndex {
			d.matrix.GetCell(row, ii).SetBackgroundColor(colorHighlight)
		}
	}
}

func (d *Debugger) drawViews() {
	d.updateViews(true)
	d.updateFocusable()
	d.draw()
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

func formatTxBarTitle(title string) string {
	return "[::u]" + title + "[::-]"
}

var humanSortRE = regexp.MustCompile(`[0-9]+`)

func humanSort(strs []string) {
	sort.Slice(strs, func(i, j int) bool {
		// skip overlapping parts
		maxChars := min(len(strs[i]), len(strs[j]))
		firstDiff := 0
		for k := 0; k < maxChars; k++ {
			if strs[i][k] != strs[j][k] {
				break
			}
			firstDiff++
		}

		// if no numbers - compare as strings
		posI := humanSortRE.FindStringIndex(strs[i][firstDiff:])
		posJ := humanSortRE.FindStringIndex(strs[j][firstDiff:])
		if len(posI) <= 0 || len(posJ) <= 0 || posI[0] != posJ[0] {
			return strs[i] < strs[j]
		}

		// if contains numbers - sort by numbers
		numsI := humanSortRE.FindAllString(strs[i][firstDiff:], -1)
		numsJ := humanSortRE.FindAllString(strs[j][firstDiff:], -1)
		numI, _ := strconv.Atoi(numsI[0])
		numJ, _ := strconv.Atoi(numsJ[0])

		if numI != numJ {
			// If the numbers are different, order by the numbers
			return numI < numJ
		}

		// If the numbers are the same, order lexicographically
		return strs[i] < strs[j]
	})
}
