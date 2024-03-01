package debugger

import (
	"fmt"
	"strings"
	"time"

	"code.rocketnine.space/tslocum/cbind"
	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"
	"github.com/samber/lo"

	"github.com/lithammer/dedent"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/am-dbg/states"
)

const (
	// TODO light mode
	colorActive    = tcell.ColorOlive
	colorInactive  = tcell.ColorLimeGreen
	colorHighlight = tcell.ColorDarkSlateGray
	playInterval   = 500 * time.Millisecond
)

type Debugger struct {
	am.ExceptionHandler
	Mach          *am.Machine
	EnableMouse   bool
	app           *cview.Application
	tree          *cview.TreeView
	treeRoot      *cview.TreeNode
	log           *cview.TextView
	timelineTxs   *cview.ProgressBar
	timelineSteps *cview.ProgressBar
	focusable     []*cview.Box
	selectedState string
	msgStruct     *telemetry.MsgStruct
	msgTxs        []*telemetry.MsgTx
	// current transition, 1-based
	cursorTx int
	// current step, 1-based
	cursorStep     int
	playTimer      *time.Ticker
	currTxBarRight *cview.TextView
	currTxBarLeft  *cview.TextView
	nextTxBarLeft  *cview.TextView
	nextTxBarRight *cview.TextView
	msgTxsParsed   []MsgTxParsed
	helpScreen     *cview.Flex
	keystrokesBar  *cview.TextView
	layoutRoot     *cview.Panels
}

type MsgTxParsed struct {
	Time          time.Time
	StatesAdded   am.S
	StatesRemoved am.S
	StatesTouched am.S
}

type nodeRef struct {
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
}

// ScrollToStateTx scrolls to the next transition involving the state being
// activated or deactivated. If fwd is true, it scrolls forward, otherwise
// backwards.
func (d *Debugger) ScrollToStateTx(state string, fwd bool) {
	step := -1
	if fwd {
		step = 1
	}
	for i := d.cursorTx + step; i > 0 && i < len(d.msgTxs)+1; i = i + step {
		parsed := d.msgTxsParsed[i-1]
		if !lo.Contains(parsed.StatesAdded, state) &&
			!lo.Contains(parsed.StatesRemoved, state) {
			continue
		}
		// scroll to this tx
		d.cursorTx = i
		d.FullRedraw()
		break
	}
}

func (d *Debugger) Clear() {
	d.treeRoot.ClearChildren()
	d.log.Clear()
	d.msgStruct = nil
	d.msgTxs = []*telemetry.MsgTx{}
	d.Mach.Add(am.S{ss.LiveView}, nil)
}

func (d *Debugger) FullRedraw() {
	d.updateTree()
	d.updateLog()
	d.updateTimelines()
	d.updateTxBars()
	d.updateKeyBars()
	d.draw()
}

func (d *Debugger) NextTx() *telemetry.MsgTx {
	onLastTx := d.cursorTx >= len(d.msgTxs)
	if onLastTx {
		return nil
	}
	return d.msgTxs[d.cursorTx]
}

func (d *Debugger) CurrentTx() *telemetry.MsgTx {
	if d.cursorTx == 0 {
		return nil
	}
	return d.msgTxs[d.cursorTx-1]
}

func (d *Debugger) initUIComponents() {
	// tree view
	d.tree = d.initMachineTree()
	d.helpScreen = d.initHelpScreen()
	d.tree.SetTitle(" Machine ")
	d.tree.SetBorder(true)
	// log view
	d.log = cview.NewTextView()
	d.log.SetBorder(true)
	d.log.SetRegions(true)
	d.log.SetTextAlign(cview.AlignLeft)
	d.log.SetDynamicColors(true)
	d.log.SetTitle(" Log ")
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
	// collect all focusable components
	d.focusable = []*cview.Box{
		d.log.Box, d.tree.Box, d.timelineTxs.Box,
		d.timelineSteps.Box,
	}
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

	mainGrid := cview.NewGrid()
	mainGrid.SetRows(-1, 2, 3, 2, 3, 2)
	mainGrid.SetColumns(-1, -1, -1)
	mainGrid.AddItem(d.tree, 0, 0, 1, 1, 0, 0, false)
	mainGrid.AddItem(d.log, 0, 1, 1, 2, 0, 0, false)
	mainGrid.AddItem(currTxBar, 1, 0, 1, 3, 0, 0, false)
	mainGrid.AddItem(d.timelineTxs, 2, 0, 1, 3, 0, 0, false)
	mainGrid.AddItem(nextTxBar, 3, 0, 1, 3, 0, 0, false)
	mainGrid.AddItem(d.timelineSteps, 4, 0, 1, 3, 0, 0, false)
	mainGrid.AddItem(d.keystrokesBar, 5, 0, 1, 3, 0, 0, false)

	panels := cview.NewPanels()
	panels.AddPanel("help", d.helpScreen, true, true)
	panels.AddPanel("main", mainGrid, true, true)

	d.layoutRoot = panels
}

// TODO better approach to modals
// TODO modal titles
func (d *Debugger) initHelpScreen() *cview.Flex {
	left := cview.NewTextView()
	left.SetBackgroundColor(colorHighlight)
	left.SetTitle(" Legend ")
	left.SetDynamicColors(true)
	left.SetPadding(1, 1, 1, 1)
	left.SetText(dedent.Dedent(strings.Trim(`
		[::b]*[::-]         handler
		[::b]>[::-]         rel source
		[::b]<[::-]         rel target
		[::b]+[::-]         add
		[::b]-[::-]         remove
		[::b]bold[::-]      touched
		[::b]underline[::-] requested
		[::b]![::-]         cancelled
	`, "\n ")))

	right := cview.NewTextView()
	right.SetBackgroundColor(colorHighlight)
	right.SetTitle(" Keystrokes ")
	right.SetDynamicColors(true)
	right.SetPadding(1, 1, 1, 1)
	right.SetText(dedent.Dedent(strings.Trim(`
		[::b]Space[::-]          play/pause
		[::b]Left/Right[::-]     rewind/fwd
		[::b]Alt+Left/Right[::-] state jump
		[::b]Alt+e[::-]          expand/collapse
		[::b]Alt+l[::-]          live view
		[::b]Tab[::-]            change focus focus
		[::b]Shift+Tab[::-]      change focus focus
		[::b]?[::-]              show help
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
	flexHor.AddItem(grid, 0, 1, false)
	flexHor.AddItem(box2, 0, 1, false)

	flexVer := cview.NewFlex()
	flexVer.SetDirection(cview.FlexRow)
	flexVer.AddItem(box3, 0, 1, false)
	flexVer.AddItem(flexHor, 0, 1, false)
	flexVer.AddItem(box4, 0, 1, false)

	return flexVer
}

func (d *Debugger) bindKeyboard() {
	// focus manager
	focusManager := cview.NewFocusManager(d.app.SetFocus)
	focusManager.SetWrapAround(true)
	focusManager.Add(d.tree, d.log, d.timelineTxs, d.timelineSteps)
	d.app.SetAfterFocusFunc(func(p cview.Primitive) {
		switch p {
		case d.tree:
			d.Mach.Add(am.S{ss.TreeFocused}, nil)
		case d.log:
			d.Mach.Add(am.S{ss.LogFocused}, nil)
		case d.timelineTxs:
			d.Mach.Add(am.S{ss.TimelineTxsFocused}, nil)
		case d.timelineSteps:
			d.Mach.Add(am.S{ss.TimelineStepsFocused}, nil)
		}
		// update the log highlight on focus change
		d.updateLog()
	})
	inputHandler := cbind.NewConfiguration()
	wrap := func(f func()) func(ev *tcell.EventKey) *tcell.EventKey {
		return func(ev *tcell.EventKey) *tcell.EventKey {
			f()
			return nil
		}
	}
	for _, key := range cview.Keys.MovePreviousField {
		err := inputHandler.Set(key, wrap(focusManager.FocusPrevious))
		if err != nil {
			d.Mach.AddErr(err)
		}
	}
	for _, key := range cview.Keys.MoveNextField {
		err := inputHandler.Set(key, wrap(focusManager.FocusNext))
		if err != nil {
			d.Mach.AddErr(err)
		}
	}
	for key, f := range d.getKeystrokes() {
		err := inputHandler.Set(key, wrap(f))
		if err != nil {
			d.Mach.AddErr(err)
		}
	}

	d.app.SetInputCapture(inputHandler.Capture)
}

func (d *Debugger) getKeystrokes() map[string]func() {
	return map[string]func(){
		// play/pause
		"space": func() {
			if d.Mach.Is(am.S{ss.Playing}) {
				d.Mach.Add(am.S{ss.Paused}, nil)
			} else {
				d.Mach.Add(am.S{ss.Playing}, nil)
			}
		},
		// prev tx
		"Left": func() {
			// TODO fast jump scroll while holding the key
			d.Mach.Remove(am.S{ss.Playing}, nil)
			if d.Mach.Is(am.S{ss.TimelineStepsFocused}) {
				d.Mach.Add(am.S{ss.RewindStep}, nil)
			} else {
				d.Mach.Add(am.S{ss.Rewind}, nil)
			}
		},
		// next tx
		"Right": func() {
			// TODO fast jump scroll while holding the key
			d.Mach.Remove(am.S{ss.Playing}, nil)
			if d.Mach.Is(am.S{ss.TimelineStepsFocused}) {
				d.Mach.Add(am.S{ss.FwdStep}, nil)
			} else {
				d.Mach.Add(am.S{ss.Fwd}, nil)
			}
		},
		// state jump back
		"alt+Left": func() {
			if d.Mach.Is(am.S{ss.StateNameSelected}) {
				d.Mach.Remove(am.S{ss.Playing}, nil)
				d.ScrollToStateTx(d.selectedState, false)
			}
		},
		// state jump fwd
		"alt+Right": func() {
			if d.Mach.Is(am.S{ss.StateNameSelected}) {
				d.Mach.Remove(am.S{ss.Playing}, nil)
				d.ScrollToStateTx(d.selectedState, true)
			}
		},
		// expand / collapse the tree root
		"alt+e": func() {
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
				} else {
					child.Expand()
				}
			}
		},
		// live view
		"alt+l": func() {
			d.Mach.Add(am.S{ss.LiveView}, nil)
		},
		// scroll to the first tx
		"home": func() {
			d.cursorTx = 0
			d.FullRedraw()
		},
		// scroll to the last tx
		"end": func() {
			d.cursorTx = len(d.msgTxs)
			d.FullRedraw()
		},
		// quit the app
		"ctrl+q": func() {
			d.Mach.Remove(am.S{ss.Init}, nil)
		},
		// help modal
		"?": func() {
			name, _ := d.layoutRoot.GetFrontPanel()
			if name == "main" {
				d.layoutRoot.SendToFront("help")
			} else {
				d.layoutRoot.SendToFront("main")
			}
		},
		// help modal exit
		"esc": func() {
			name, _ := d.layoutRoot.GetFrontPanel()
			if name == "help" {
				d.layoutRoot.SendToFront("main")
			}
		},
	}
}

func (d *Debugger) parseClientMsg(msgTx *telemetry.MsgTx) {
	msgTxParsed := MsgTxParsed{
		Time: time.Now(),
	}
	// added / removed
	if len(d.msgTxs) > 1 {
		prev := d.msgTxs[len(d.msgTxs)-2]
		for i, name := range d.msgStruct.StatesIndex {
			if prev.StatesActive[i] && !msgTx.StatesActive[i] {
				msgTxParsed.StatesRemoved = append(msgTxParsed.StatesRemoved, name)
			} else if !prev.StatesActive[i] && msgTx.StatesActive[i] {
				msgTxParsed.StatesAdded = append(msgTxParsed.StatesAdded, name)
			} else if prev.Clocks[i] != msgTx.Clocks[i] {
				// treat multi states as added
				msgTxParsed.StatesAdded = append(msgTxParsed.StatesAdded, name)
			}
		}
	}
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
	d.msgTxsParsed = append(d.msgTxsParsed, msgTxParsed)
}

func (d *Debugger) appendLog(msgTx *telemetry.MsgTx) error {
	logStr := formatLogEntry(strings.Join(msgTx.PreLogEntries, "\n"))
	if len(logStr) > 0 {
		logStr += "\n"
	}
	if len(msgTx.LogEntries) > 0 {
		logStr += `["` + msgTx.ID + `"]` +
			formatLogEntry(strings.Join(msgTx.LogEntries, "\n")) +
			`[""]` + "\n"
	}
	if len(logStr) == 0 {
		return nil
	}
	_, err := d.log.Write([]byte(logStr))
	if err != nil {
		return err
	}
	return nil
}

func (d *Debugger) handleMsgStruct(msg *telemetry.MsgStruct) {
	d.treeRoot.SetText(msg.ID)
	d.msgStruct = msg
	d.treeRoot.ClearChildren()
	for _, name := range msg.StatesIndex {
		d.addState(name)
	}
}

func (d *Debugger) draw() {
	// TODO debounce every 16msec
	d.app.QueueUpdateDraw(func() {})
}

func (d *Debugger) updateTxBars() {
	d.currTxBarLeft.Clear()
	d.currTxBarRight.Clear()
	d.nextTxBarLeft.Clear()
	d.nextTxBarRight.Clear()
	tx := d.CurrentTx()
	if len(d.msgTxs) == 0 {
		d.currTxBarLeft.SetText("Waiting for a client...")
	} else if tx == nil {
		d.currTxBarLeft.SetText(formatTxBarTitle("Paused"))
	} else {
		var title string
		switch d.Mach.Switch(ss.GroupPlaying...) {
		case ss.Playing:
			title = "Playing"
		case ss.Paused:
			title = "Paused"
		case ss.LiveView:
			title += "Live"
			if d.Mach.Not(am.S{ss.ClientConnected}) {
				title += " (disconnected)"
			}
		}
		left, right := getTxInfo(tx, &d.msgTxsParsed[d.cursorTx-1], title)
		d.currTxBarLeft.SetText(left)
		d.currTxBarRight.SetText(right)
	}

	nextTx := d.NextTx()
	if nextTx != nil {
		title := "Next"
		left, right := getTxInfo(nextTx, &d.msgTxsParsed[d.cursorTx], title)
		d.nextTxBarLeft.SetText(left)
		d.nextTxBarRight.SetText(right)
	}
}

func (d *Debugger) updateLog() {
	if d.msgStruct != nil {
		lvl := d.msgStruct.LogLevel
		d.log.SetTitle(" Log:" + lvl.String() + " ")
	}
	// highlight the next tx if scrolling by steps
	bySteps := d.Mach.Is(am.S{ss.TimelineStepsFocused})
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
	d.log.Highlight(tx.ID)
	d.log.ScrollToHighlight()
}

func (d *Debugger) updateTimelines() {
	txCount := len(d.msgTxs)
	nextTx := d.NextTx()
	d.timelineSteps.SetTitleColor(cview.Styles.PrimaryTextColor)
	d.timelineSteps.SetBorderColor(cview.Styles.PrimaryTextColor)
	d.timelineSteps.SetFilledColor(cview.Styles.PrimaryTextColor)
	// mark last step of a cancelled tx in red
	if nextTx != nil && d.cursorStep == len(nextTx.Steps) && !nextTx.Accepted {
		d.timelineSteps.SetFilledColor(tcell.ColorRed)
	}
	// inactive steps bar when no next tx
	if nextTx == nil {
		d.timelineSteps.SetTitleColor(tcell.ColorGrey)
		d.timelineSteps.SetBorderColor(tcell.ColorGrey)
	}
	stepsCount := 0
	onLastTx := d.cursorTx >= txCount
	if !onLastTx {
		stepsCount = len(d.msgTxs[d.cursorTx].Steps)
	}

	// progressbar cant be max==0
	d.timelineTxs.SetMax(max(txCount, 1))
	// progress <= max
	d.timelineTxs.SetProgress(d.cursorTx)
	d.timelineTxs.SetTitle(fmt.Sprintf(
		" Transition %d / %d ", d.cursorTx, txCount))

	// progressbar cant be max==0
	d.timelineSteps.SetMax(max(stepsCount, 1))
	// progress <= max
	d.timelineSteps.SetProgress(d.cursorStep)
	d.timelineSteps.SetTitle(fmt.Sprintf(
		" Next mutation step %d / %d ", d.cursorStep, stepsCount))
}

func (d *Debugger) addState(name string) {
	state := d.msgStruct.States[name]
	stateNode := cview.NewTreeNode(name + " (0)")
	stateNode.SetReference(name)
	stateNode.SetSelectable(true)
	stateNode.SetReference(nodeRef{stateName: name})
	d.treeRoot.AddChild(stateNode)
	stateNode.SetColor(colorInactive)
	// labels
	labels := ""
	if state.Auto {
		labels += "auto"
	}
	if state.Multi {
		if labels != "" {
			labels += " "
		}
		labels += "multi"
	}
	if labels != "" {
		labelNode := cview.NewTreeNode(labels)
		labelNode.SetSelectable(false)
		stateNode.AddChild(labelNode)
	}
	// relations
	addRelation(stateNode, name, am.RelationAdd, state.Add)
	addRelation(stateNode, name, am.RelationRequire, state.Require)
	addRelation(stateNode, name, am.RelationRemove, state.Remove)
	addRelation(stateNode, name, am.RelationAfter, state.After)
}

func (d *Debugger) updateKeyBars() {
	// TODO light mode
	stateJump := "[grey] | Alt+Left/Right: state jump[yellow]"
	if d.Mach.Is(am.S{ss.StateNameSelected}) {
		stateJump = " | Alt+Left/Right: state jump"
	}
	keys := "[yellow]Space: play/pause | Left/Right: rewind/fwd | Alt+e: " +
		"expand/collapse | Tab: focus | Alt+l: live view | ?: help" + stateJump
	d.keystrokesBar.SetText(keys)
}

func formatLogEntry(entry string) string {
	entry = strings.ReplaceAll(strings.ReplaceAll(entry,
		"[", "{{{"),
		"]", "}}}")
	// TODO light mode
	entry = strings.ReplaceAll(strings.ReplaceAll(entry,
		"{{{", "[yellow]["),
		"}}}", "[][white]")
	return entry
}

func getTxInfo(
	tx *telemetry.MsgTx, parsed *MsgTxParsed, title string,
) (string, string) {
	// left side
	left := formatTxBarTitle(title)
	if tx != nil {
		left += " | " + tx.ID
		if tx.IsAuto {
			left += " | auto"
		}
		if tx.Accepted {
			left += " | accepted"
		} else if !tx.Accepted {
			left += " | rejected"
		}
	}
	// right side
	right := fmt.Sprintf("(A: %d | R: %d | T: %d) %s",
		len(parsed.StatesAdded), len(parsed.StatesRemoved),
		len(parsed.StatesTouched), parsed.Time.Format(time.DateTime),
	)
	return left, right
}

func formatTxBarTitle(title string) string {
	return "[::u]" + title + "[::-]"
}
