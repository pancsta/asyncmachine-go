package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"code.rocketnine.space/tslocum/cbind"
	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/samber/lo"
)

const (
	// TODO light mode
	colorActive    = tcell.ColorOlive
	colorInactive  = tcell.ColorLimeGreen
	colorHighlight = tcell.ColorDarkSlateGray
	playInterval   = 500 * time.Millisecond
)

func main() {
	// debug
	runtime.SetMutexProfileFraction(1)

	// log
	logFile := os.Getenv("AM_DEBUG_LOG")
	if logFile == "" {
		logFile = "log.txt"
	}
	_ = os.Remove(logFile)
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	log.SetOutput(file)

	// machine
	ctx := context.Background()
	mach := am.New(ctx, ss.States, &am.Opts{
		HandlerTimeout:       time.Hour,
		DontPanicToException: true,
	})
	err = mach.VerifyStates(ss.Names)
	if err != nil {
		panic(err)
	}
	// mach.SetLogLevel(am.LogChanges)
	mach.SetLogLevel(am.LogOps)
	// mach.SetLogLevel(am.LogEverything)
	mach.LogID = false
	mach.SetLogger(func(level am.LogLevel, msg string, args ...any) {
		txt := fmt.Sprintf(msg, args...)
		log.Print(txt)
	})
	h := &machineHandlers{mach: mach}
	err = mach.BindHandlers(h)
	if err != nil {
		panic(err)
	}
	// redraw on auto states
	go func() {
		// bind to transitions
		txEndCh := mach.On([]string{am.EventTransitionEnd}, nil)
		for event := range txEndCh {
			tx := event.Args["transition"].(*am.Transition)
			if tx.IsAuto() && tx.Accepted {
				// handle Paused
				h.updateTxBars()
				h.draw()
			}
		}
	}()

	// rpc client
	if url := os.Getenv("AM_DEBUG_URL"); url != "" {
		err := telemetry.MonitorTransitions(mach, url)
		// TODO retries
		if err != nil {
			panic(err)
		}
	}

	// rpc server
	go startRCP(&RPCServer{
		mach: mach,
		url:  os.Getenv("AM_DEBUG_SERVER_URL"),
	})

	mach.Add(am.S{"Init"}, nil)
	<-mach.WhenNot(am.S{"Init"}, nil)
}

type machineHandlers struct {
	am.ExceptionHandler
	mach          *am.Machine
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
	currTxBarRight *cview.Frame
	currTxBarLeft  *cview.Frame
	nextTxBarLeft  *cview.Frame
	nextTxBarRight *cview.Frame
	msgTxsParsed   []MsgTxParsed
	// TODO legend
	helpScreen    *cview.Panels
	keystrokesBar *cview.TextView
}

type MsgTxParsed struct {
	Time          time.Time
	StatesAdded   am.S
	StatesRemoved am.S
	StatesTouched am.S
}

type nodeRef struct {
	stateName   string
	isRef       bool
	isRel       bool
	rel         am.Relation
	parentState string
}

// handlers

// TODO ExceptionState: separate error screen with stack trace

func (h *machineHandlers) InitState(_ *am.Event) {
	h.app = cview.NewApplication()
	h.InitUIComponents()

	// layout
	currTxBar := cview.NewGrid()
	currTxBar.AddItem(h.currTxBarLeft, 0, 0, 1, 1, 0, 0, false)
	currTxBar.AddItem(h.currTxBarRight, 0, 1, 1, 1, 0, 0, false)

	nextTxBar := cview.NewGrid()
	nextTxBar.AddItem(h.nextTxBarLeft, 0, 0, 1, 1, 0, 0, false)
	nextTxBar.AddItem(h.nextTxBarRight, 0, 1, 1, 1, 0, 0, false)

	mainGrid := cview.NewGrid()
	mainGrid.SetRows(-1, 2, 3, 2, 3, 2)
	mainGrid.SetColumns(-1, -1, -1)
	mainGrid.AddItem(h.tree, 0, 0, 1, 1, 0, 0, false)
	mainGrid.AddItem(h.log, 0, 1, 1, 2, 0, 0, false)
	mainGrid.AddItem(currTxBar, 1, 0, 1, 3, 0, 0, false)
	mainGrid.AddItem(h.timelineTxs, 2, 0, 1, 3, 0, 0, false)
	mainGrid.AddItem(nextTxBar, 3, 0, 1, 3, 0, 0, false)
	mainGrid.AddItem(h.timelineSteps, 4, 0, 1, 3, 0, 0, false)
	mainGrid.AddItem(h.keystrokesBar, 5, 0, 1, 3, 0, 0, false)

	h.handleKeyboard()

	// draw in a goroutine
	go func() {
		h.app.SetRoot(mainGrid, true)
		h.app.SetFocus(h.tree)
		err := h.app.Run()
		if err != nil {
			h.mach.AddErr(err)
		}
		h.mach.Remove(am.S{"Init"}, nil)
	}()
}

func (h *machineHandlers) InitUIComponents() {
	// tree view
	h.tree = h.initMachineTree()
	h.tree.SetTitle(" Machine ")
	h.tree.SetBorder(true)
	// log view
	h.log = cview.NewTextView()
	h.log.SetBorder(true)
	h.log.SetRegions(true)
	// h.log.SetChangedFunc(func() {
	// 	h.app.Draw()
	// })
	h.log.SetTextAlign(cview.AlignLeft)
	h.log.SetDynamicColors(true)
	h.log.SetTitle(" Log ")
	// TODO step info bar: type, from, to, data
	h.currTxBarLeft = cview.NewFrame(cview.NewBox())
	h.currTxBarLeft.SetBorders(0, 0, 0, 0, 0, 0)
	h.currTxBarRight = cview.NewFrame(cview.NewBox())
	h.currTxBarRight.SetBorders(0, 0, 0, 0, 0, 0)
	h.nextTxBarLeft = cview.NewFrame(cview.NewBox())
	h.nextTxBarLeft.SetBorders(0, 0, 0, 0, 0, 0)
	h.nextTxBarRight = cview.NewFrame(cview.NewBox())
	h.nextTxBarRight.SetBorders(0, 0, 0, 0, 0, 0)
	// timeline tx
	h.timelineTxs = cview.NewProgressBar()
	h.timelineTxs.SetBorder(true)
	// timeline steps
	h.timelineSteps = cview.NewProgressBar()
	h.timelineSteps.SetBorder(true)
	// keystrokes bar
	h.keystrokesBar = cview.NewTextView()
	h.keystrokesBar.SetDynamicColors(true)

	// update models
	h.updateTimelines()
	h.updateTxBars()
	h.updateKeyBars()
	// collect all focusable components
	h.focusable = []*cview.Box{
		h.log.Box, h.tree.Box, h.timelineTxs.Box,
		h.timelineSteps.Box,
	}
}

// TODO bind End/Home to timelines
func (h *machineHandlers) handleKeyboard() {
	// focus manager
	focusManager := cview.NewFocusManager(h.app.SetFocus)
	focusManager.SetWrapAround(true)
	focusManager.Add(h.tree, h.log, h.timelineTxs, h.timelineSteps)
	h.app.SetAfterFocusFunc(func(p cview.Primitive) {
		switch p {
		case h.tree:
			h.mach.Add(am.S{ss.TreeFocused}, nil)
		case h.log:
			h.mach.Add(am.S{ss.LogFocused}, nil)
		case h.timelineTxs:
			h.mach.Add(am.S{ss.TimelineTxsFocused}, nil)
		case h.timelineSteps:
			h.mach.Add(am.S{ss.TimelineStepsFocused}, nil)
		}
		// update the log highlight on focus change
		h.updateLog()
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
			h.mach.AddErr(err)
		}
	}
	for _, key := range cview.Keys.MoveNextField {
		err := inputHandler.Set(key, wrap(focusManager.FocusNext))
		if err != nil {
			h.mach.AddErr(err)
		}
	}

	// space
	err := inputHandler.Set("space", func(ev *tcell.EventKey) *tcell.EventKey {
		if h.mach.Is(am.S{ss.Playing}) {
			h.mach.Add(am.S{ss.Paused}, nil)
		} else {
			h.mach.Add(am.S{ss.Playing}, nil)
		}
		return nil
	})
	if err != nil {
		h.mach.AddErr(err)
	}

	// left
	err = inputHandler.Set("Left", func(ev *tcell.EventKey) *tcell.EventKey {
		h.mach.Remove(am.S{ss.Playing}, nil)
		if h.mach.Is(am.S{ss.TimelineStepsFocused}) {
			h.mach.Add(am.S{ss.RewindStep}, nil)
		} else {
			h.mach.Add(am.S{ss.Rewind}, nil)
		}
		// TODO fast jump scroll while holding the key
		return nil
	})
	if err != nil {
		h.mach.AddErr(err)
	}

	// right
	err = inputHandler.Set("Right", func(ev *tcell.EventKey) *tcell.EventKey {
		h.mach.Remove(am.S{ss.Playing}, nil)
		if h.mach.Is(am.S{ss.TimelineStepsFocused}) {
			h.mach.Add(am.S{ss.FwdStep}, nil)
		} else {
			h.mach.Add(am.S{ss.Fwd}, nil)
		}
		// TODO fast jump scroll while holding the key
		return nil
	})
	if err != nil {
		h.mach.AddErr(err)
	}

	// alt+left state jump back
	err = inputHandler.Set("alt+Left", func(ev *tcell.EventKey) *tcell.EventKey {
		if h.mach.Is(am.S{ss.StateNameSelected}) {
			h.mach.Remove(am.S{ss.Playing}, nil)
			h.ScrollToStateTx(h.selectedState, false)
		}
		return nil
	})
	if err != nil {
		h.mach.AddErr(err)
	}

	// alt+right state jump fwd
	err = inputHandler.Set("alt+Right", func(ev *tcell.EventKey) *tcell.EventKey {
		if h.mach.Is(am.S{ss.StateNameSelected}) {
			h.mach.Remove(am.S{ss.Playing}, nil)
			h.ScrollToStateTx(h.selectedState, true)
		}
		return nil
	})
	if err != nil {
		h.mach.AddErr(err)
	}

	// expand / collapse alt+e
	err = inputHandler.Set("alt+e", func(_ *tcell.EventKey) *tcell.EventKey {
		expanded := false
		children := h.tree.GetRoot().GetChildren()
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
		return nil
	})
	if err != nil {
		h.mach.AddErr(err)
	}

	// alt+l live view
	err = inputHandler.Set("alt+l", func(_ *tcell.EventKey) *tcell.EventKey {
		h.mach.Add(am.S{ss.LiveView}, nil)
		return nil
	})
	if err != nil {
		h.mach.AddErr(err)
	}

	h.app.SetInputCapture(inputHandler.Capture)
}

func (h *machineHandlers) StateNameSelectedState(e *am.Event) {
	h.selectedState = e.Args["selectedStateName"].(string)
	h.updateTree()
	h.updateKeyBars()
}

func (h *machineHandlers) StateNameSelectedStateNameSelected(e *am.Event) {
	h.StateNameSelectedState(e)
}

func (h *machineHandlers) StateNameSelectedEnd(_ *am.Event) {
	h.selectedState = ""
	h.updateTree()
	h.updateKeyBars()
}

func (h *machineHandlers) LiveViewState(_ *am.Event) {
	if h.cursorTx == 0 {
		return
	}
	h.cursorTx = len(h.msgTxs)
	h.fullRedraw()
}

func (h *machineHandlers) fullRedraw() {
	h.updateTree()
	h.updateLog()
	h.updateTimelines()
	h.updateTxBars()
	h.updateKeyBars()
	h.draw()
}

func (h *machineHandlers) PlayingState(_ *am.Event) {
	// TODO play by steps
	if h.playTimer == nil {
		h.playTimer = time.NewTicker(playInterval)
	} else {
		h.playTimer.Reset(time.Second)
	}
	ctx := h.mach.GetStateCtx(ss.Playing)
	go func() {
		h.mach.Add(am.S{ss.Fwd}, nil)
		for range h.playTimer.C {
			if ctx.Err() != nil {
				break
			}
			h.mach.Add(am.S{ss.Fwd}, nil)
		}
	}()
}

func (h *machineHandlers) PlayingEnd(_ *am.Event) {
	h.playTimer.Stop()
}

func (h *machineHandlers) FwdEnter(_ *am.Event) bool {
	return h.cursorTx < len(h.msgTxs)
}

func (h *machineHandlers) FwdState(_ *am.Event) {
	defer h.mach.Remove(am.S{ss.Fwd}, nil)
	h.cursorTx++
	h.cursorStep = 0
	if h.mach.Is(am.S{ss.Playing}) && h.cursorTx == len(h.msgTxs) {
		h.mach.Remove(am.S{ss.Playing}, nil)
	}
	h.fullRedraw()
}

func (h *machineHandlers) RewindEnter(_ *am.Event) bool {
	return h.cursorTx > 0
}

func (h *machineHandlers) RewindState(_ *am.Event) {
	defer h.mach.Remove(am.S{ss.Rewind}, nil)
	h.cursorTx--
	h.cursorStep = 0
	h.fullRedraw()
}

func (h *machineHandlers) FwdStepEnter(_ *am.Event) bool {
	nextTx := h.nextTx()
	if nextTx == nil {
		return false
	}
	return h.cursorStep < len(nextTx.Steps)+1
}

func (h *machineHandlers) FwdStepState(_ *am.Event) {
	defer h.mach.Remove(am.S{ss.FwdStep}, nil)
	// next tx
	nextTx := h.nextTx()
	// scroll to the next tx
	if h.cursorStep == len(nextTx.Steps) {
		h.mach.Add(am.S{ss.Fwd}, nil)
		return
	}
	h.cursorStep++
	h.fullRedraw()
}

func (h *machineHandlers) RewindStepEnter(_ *am.Event) bool {
	return h.cursorStep > 0 || h.cursorTx > 0
}

func (h *machineHandlers) RewindStepState(_ *am.Event) {
	defer h.mach.Remove(am.S{ss.RewindStep}, nil)
	// wrap if theres a prev tx
	if h.cursorStep <= 0 {
		h.cursorTx--
		h.updateLog()
		nextTx := h.nextTx()
		h.cursorStep = len(nextTx.Steps)
	} else {
		h.cursorStep--
	}
	h.fullRedraw()
}

func (h *machineHandlers) Clear() {
	h.treeRoot.ClearChildren()
	h.log.Clear()
	h.msgStruct = nil
	h.msgTxs = []*telemetry.MsgTx{}
	h.mach.Add(am.S{ss.LiveView}, nil)
}

func (h *machineHandlers) HelpScreenState() {
	if h.helpScreen == nil {
		h.helpScreen = h.initHelpScreen()
	}
	h.treeRoot.ClearChildren()
	h.log.Clear()
	h.msgStruct = nil
	h.msgTxs = []*telemetry.MsgTx{}
	h.mach.Add(am.S{ss.LiveView}, nil)
}

func (h *machineHandlers) ClientConnectedState(_ *am.Event) {
	h.Clear()
	for _, box := range h.focusable {
		box.SetBorderColorFocused(colorActive)
	}
	h.draw()
}

func (h *machineHandlers) ClientConnectedEnd(_ *am.Event) {
	for _, box := range h.focusable {
		box.SetBorderColorFocused(cview.ColorUnset)
	}
	h.draw()
}

func (h *machineHandlers) nextTx() *telemetry.MsgTx {
	onLastTx := h.cursorTx >= len(h.msgTxs)
	if onLastTx {
		return nil
	}
	return h.msgTxs[h.cursorTx]
}

func (h *machineHandlers) currentTx() *telemetry.MsgTx {
	if h.cursorTx == 0 {
		return nil
	}
	return h.msgTxs[h.cursorTx-1]
}

func (h *machineHandlers) ClientMsgState(e *am.Event) {
	// initial structure data
	_, structOk := e.Args["msg_struct"]
	if structOk {
		msgStruct := e.Args["msg_struct"].(*telemetry.MsgStruct)
		h.handleMsgStruct(msgStruct)
		return
	}
	// transition data
	msgTx := e.Args["msg_tx"].(*telemetry.MsgTx)
	// TODO scalable storage
	h.msgTxs = append(h.msgTxs, msgTx)
	// parse the msg
	h.parseClientMsg(msgTx)
	err := h.appendLog(msgTx)
	if err != nil {
		h.mach.AddErr(err)
		return
	}
	if h.mach.Is(am.S{ss.LiveView}) {
		// force the latest tx
		// TODO debounce?
		h.cursorTx = len(h.msgTxs)
		h.cursorStep = 0
		h.updateTxBars()
		h.updateTree()
		h.updateLog()
	}
	h.updateTimelines()
	h.draw()
}

func (h *machineHandlers) parseClientMsg(msgTx *telemetry.MsgTx) {
	msgTxParsed := MsgTxParsed{
		Time: time.Now(),
	}
	// added / removed
	if len(h.msgTxs) > 1 {
		prev := h.msgTxs[len(h.msgTxs)-2]
		for i, name := range h.msgStruct.StatesIndex {
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
	h.msgTxsParsed = append(h.msgTxsParsed, msgTxParsed)
}

func (h *machineHandlers) appendLog(msgTx *telemetry.MsgTx) error {
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
	_, err := h.log.Write([]byte(logStr))
	if err != nil {
		return err
	}
	return nil
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

func (h *machineHandlers) handleMsgStruct(msg *telemetry.MsgStruct) {
	h.treeRoot.SetText(msg.ID)
	h.msgStruct = msg
	h.treeRoot.ClearChildren()
	for _, name := range msg.StatesIndex {
		h.addState(name)
	}
}

func (h *machineHandlers) draw() {
	// TODO debounce every 16msec
	h.app.QueueUpdateDraw(func() {})
}

// methods

func (h *machineHandlers) updateTxBars() {
	h.currTxBarLeft.Clear()
	h.currTxBarRight.Clear()
	h.nextTxBarLeft.Clear()
	h.nextTxBarRight.Clear()
	tx := h.currentTx()
	if tx != nil {
		var title string
		switch h.mach.Switch(ss.GroupPlaying...) {
		case ss.Playing:
			title = "Playing"
		case ss.Paused:
			title = "Paused"
		case ss.LiveView:
			title += "Live"
			if h.mach.Not(am.S{ss.ClientConnected}) {
				title += " (disconnected)"
			}
		}
		left, right := getTxInfo(tx, &h.msgTxsParsed[h.cursorTx-1], title)
		h.currTxBarLeft.AddText(left, true, cview.AlignLeft,
			cview.Styles.PrimaryTextColor)
		h.currTxBarRight.AddText(right, true, cview.AlignRight,
			cview.Styles.PrimaryTextColor)
	}

	nextTx := h.nextTx()
	if nextTx != nil {
		title := "Next"
		left, right := getTxInfo(nextTx, &h.msgTxsParsed[h.cursorTx], title)
		h.nextTxBarLeft.AddText(left, true, cview.AlignLeft,
			cview.Styles.PrimaryTextColor)
		h.nextTxBarRight.AddText(right, true, cview.AlignRight,
			cview.Styles.PrimaryTextColor)
	}
}

func getTxInfo(
	tx *telemetry.MsgTx, parsed *MsgTxParsed, title string,
) (string, string) {
	// left side
	left := title
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

func (h *machineHandlers) updateLog() {
	if h.msgStruct != nil {
		lvl := h.msgStruct.LogLevel
		h.log.SetTitle(" Log:" + lvl.String() + " ")
	}
	// highlight the next tx if scrolling by steps
	bySteps := h.mach.Is(am.S{ss.TimelineStepsFocused})
	tx := h.currentTx()
	if bySteps {
		tx = h.nextTx()
	}
	if tx == nil {
		h.log.Highlight("")
		if bySteps {
			h.log.ScrollToEnd()
		} else {
			h.log.ScrollToBeginning()
		}
		return
	}
	h.log.Highlight(tx.ID)
	h.log.ScrollToHighlight()
}

func (h *machineHandlers) updateTimelines() {
	txCount := len(h.msgTxs)
	nextTx := h.nextTx()
	h.timelineSteps.SetTitleColor(cview.Styles.PrimaryTextColor)
	h.timelineSteps.SetBorderColor(cview.Styles.PrimaryTextColor)
	h.timelineSteps.SetFilledColor(cview.Styles.PrimaryTextColor)
	if nextTx != nil &&
		// mark last step of a cancelled tx in red
		h.cursorStep == len(nextTx.Steps) && !nextTx.Accepted {
		h.timelineSteps.SetFilledColor(tcell.ColorRed)
	}
	if h.cursorTx == txCount {
		h.timelineSteps.SetTitleColor(tcell.ColorGrey)
		h.timelineSteps.SetBorderColor(tcell.ColorGrey)
	}
	stepsCount := 0
	onLastTx := h.cursorTx >= txCount
	if !onLastTx {
		stepsCount = len(h.msgTxs[h.cursorTx].Steps)
	}

	// progressbar cant be max==0
	h.timelineTxs.SetMax(max(txCount, 1))
	// progress <= max
	h.timelineTxs.SetProgress(h.cursorTx)
	h.timelineTxs.SetTitle(fmt.Sprintf(
		" Transition %d / %d ", h.cursorTx, txCount))

	// progressbar cant be max==0
	h.timelineSteps.SetMax(max(stepsCount, 1))
	// progress <= max
	h.timelineSteps.SetProgress(h.cursorStep)
	h.timelineSteps.SetTitle(fmt.Sprintf(
		" Next mutation step %d / %d ", h.cursorStep, stepsCount))
}

// tree

func (h *machineHandlers) updateTree() {
	var msg telemetry.Msg
	queue := ""
	if h.cursorTx == 0 {
		msg = h.msgStruct
	} else {
		tx := h.msgTxs[h.cursorTx-1]
		msg = tx
		queue = "(" + strconv.Itoa(tx.Queue) + ") "
	}
	h.tree.SetTitle(" Machine " + queue)
	var steps []*am.TransitionStep
	if h.cursorStep > 0 {
		steps = h.nextTx().Steps
	}
	h.tree.GetRoot().Walk(func(node, parent *cview.TreeNode) bool {
		// skip the root
		if parent == nil {
			return true
		}
		refSrc := node.GetReference()
		if refSrc == nil {
			return true
		}
		node.SetBold(false)
		node.SetUnderline(false)
		ref := refSrc.(nodeRef)
		if ref.isRel {
			node.SetText(capitalizeFirst(ref.rel.String()))
			return true
		}
		// inherit
		if parent == h.tree.GetRoot() || !parent.GetHighlighted() {
			node.SetHighlighted(false)
		}
		stateName := ref.stateName
		color := colorInactive
		if msg.Is(h.msgStruct.StatesIndex, am.S{stateName}) {
			color = colorActive
		}
		// reset to defaults
		if stateName != h.selectedState {
			if !ref.isRef {
				// un-highlight all descendants
				for _, child := range node.GetChildren() {
					child.SetHighlighted(false)
					for _, child2 := range child.GetChildren() {
						child2.SetHighlighted(false)
					}
				}
				tick := strconv.FormatUint(msg.Clock(h.msgStruct.StatesIndex,
					stateName), 10)
				node.SetColor(color)
				node.SetText(stateName + " (" + tick + ")")
			} else {
				// reset to defaults
				node.SetText(stateName)
			}
			return true
		}
		// reference
		if node != h.tree.GetCurrentNode() {
			node.SetHighlighted(true)
			log.Println("highlight", stateName)
		}
		if ref.isRef {
			return true
		}
		// top-level state
		tick := strconv.FormatUint(msg.Clock(h.msgStruct.StatesIndex,
			stateName), 10)
		node.SetColor(color)
		node.SetText(stateName + " (" + tick + ")")
		if node == h.tree.GetCurrentNode() {
			return true
		}
		// highlight all descendants
		for _, child := range node.GetChildren() {
			child.SetHighlighted(true)
			for _, child2 := range child.GetChildren() {
				child2.SetHighlighted(true)
			}
		}
		return true
	})

	// TODO extract
	// decorate steps
	h.tree.GetRoot().Walk(func(node, parent *cview.TreeNode) bool {
		// skip the root
		if parent == nil {
			return true
		}
		refSrc := node.GetReference()
		if refSrc == nil {
			return true
		}
		ref := refSrc.(nodeRef)
		if ref.stateName != "" {
			// STATE NAME NODES
			stateName := ref.stateName
			for i := range steps {
				if h.cursorStep == i {
					break
				}
				step := steps[i]
				switch step.Type {
				case am.TransitionStepTypeNoSet:
					if step.ToState == stateName && !ref.isRef {
						node.SetBold(true)
					}
				case am.TransitionStepTypeRemove:
					if step.ToState == stateName && !ref.isRef {
						node.SetText("-" + node.GetText())
						node.SetBold(true)
					}
				case am.TransitionStepTypeRelation:
					if step.FromState == stateName && !ref.isRef {
						node.SetBold(true)
					} else if step.ToState == stateName && !ref.isRef {
						node.SetText(node.GetText() + " <")
						node.SetBold(true)
					} else if ref.isRef && step.ToState == stateName &&
						ref.parentState == step.FromState {
						node.SetText(node.GetText() + " >")
						node.SetBold(true)
					}
				case am.TransitionStepTypeTransition:
					if ref.isRef {
						continue
					}
					// states handler executed
					if step.FromState == stateName || step.ToState == stateName {
						node.SetText("*" + node.GetText())
						node.SetBold(true)
					}
				case am.TransitionStepTypeSet:
					if step.ToState == stateName && !ref.isRef {
						node.SetText("+" + node.GetText())
						node.SetBold(true)
					}
				case am.TransitionStepTypeRequested:
					if step.ToState == stateName && !ref.isRef {
						node.SetUnderline(true)
						node.SetBold(true)
					}
				case am.TransitionStepTypeCancel:
					if step.ToState == stateName && !ref.isRef {
						node.SetText("!" + node.GetText())
						node.SetBold(true)
					}
				}
			}
		} else if ref.isRel {
			// RELATION NODES
			for i := range steps {
				if h.cursorStep == i {
					break
				}
				step := steps[i]
				if step.Type != am.TransitionStepTypeRelation {
					continue
				}
				if step.Data == ref.rel && ref.parentState == step.FromState {
					node.SetBold(true)
				}
			}
		}
		return true
	})
}

func (h *machineHandlers) initMachineTree() *cview.TreeView {
	h.treeRoot = cview.NewTreeNode("")
	h.treeRoot.SetColor(tcell.ColorRed)
	tree := cview.NewTreeView()
	tree.SetRoot(h.treeRoot)
	tree.SetCurrentNode(h.treeRoot)
	tree.SetHighlightColor(colorHighlight)
	tree.SetChangedFunc(func(node *cview.TreeNode) {
		reference := node.GetReference()
		if reference == nil || reference.(nodeRef).stateName == "" {
			h.mach.Remove(am.S{ss.StateNameSelected}, nil)
			return
		}
		ref := reference.(nodeRef)
		h.mach.Add(am.S{ss.StateNameSelected},
			am.A{"selectedStateName": ref.stateName})
	})
	tree.SetSelectedFunc(func(node *cview.TreeNode) {
		node.SetExpanded(!node.IsExpanded())
	})
	return tree
}

func (h *machineHandlers) addState(name string) {
	state := h.msgStruct.States[name]
	stateNode := cview.NewTreeNode(name + " (0)")
	stateNode.SetReference(name)
	stateNode.SetSelectable(true)
	stateNode.SetReference(nodeRef{stateName: name})
	h.treeRoot.AddChild(stateNode)
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

func (h *machineHandlers) initHelpScreen() *cview.Panels {
	background := cview.NewTextView()

	modal := func(p cview.Primitive, width, height int) cview.Primitive {
		grid := cview.NewGrid()
		grid.SetColumns(0, width, 0)
		grid.SetRows(0, height, 0)
		grid.AddItem(p, 1, 1, 1, 1, 0, 0, true)
		return grid
	}

	box := cview.NewBox()
	box.SetBorder(true)
	box.SetTitle("Centered Box")

	panels := cview.NewPanels()
	panels.AddPanel("background", background, true, true)
	panels.AddPanel("modal", modal(box, 40, 10), true, true)

	return panels
}

// ScrollToStateTx scrolls to the next transition involving the state being
// activated or deactivated. If fwd is true, it scrolls forward, otherwise
// backwards.
func (h *machineHandlers) ScrollToStateTx(state string, fwd bool) {
	step := -1
	if fwd {
		step = 1
	}
	for i := h.cursorTx + step; i > 0 && i < len(h.msgTxs)+1; i = i + step {
		parsed := h.msgTxsParsed[i-1]
		if !lo.Contains(parsed.StatesAdded, state) &&
			!lo.Contains(parsed.StatesRemoved, state) {
			continue
		}
		// scroll to this tx
		h.cursorTx = i
		h.fullRedraw()
		break
	}
}

func (h *machineHandlers) updateKeyBars() {
	// TODO light mode
	stateJump := "[grey]Alt+Left/Right: state jump[yellow]"
	if h.mach.Is(am.S{ss.StateNameSelected}) {
		stateJump = "Alt+Left/Right: state jump"
	}
	keys := "[yellow]Space: play/pause | Left/Right: rewind/fwd | Alt+e: " +
		"expand/collapse | Tab: focus | Alt+l: Live view | " + stateJump
	// TODO legend
	// * handler, > rel source, < rel target, + add, - remove, bold touched,
	//   underline requested, ! cancelled
	h.keystrokesBar.SetText(keys)
}

func addRelation(
	stateNode *cview.TreeNode, name string, rel am.Relation, relations []string,
) {
	if len(relations) <= 0 {
		return
	}
	relNode := cview.NewTreeNode(capitalizeFirst(rel.String()))
	relNode.SetSelectable(true)
	relNode.SetReference(nodeRef{
		isRel:       true,
		rel:         rel,
		parentState: name,
	})
	for _, relState := range relations {
		stateNode := cview.NewTreeNode(relState)
		stateNode.SetReference(nodeRef{
			isRef:       true,
			stateName:   relState,
			parentState: name,
		})
		relNode.AddChild(stateNode)
	}
	stateNode.AddChild(relNode)
}

func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}
