// TODO ExceptionState: separate error screen with stack trace

package debugger

import (
	"time"

	"github.com/pancsta/cview"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/am-dbg/states"
)

func (d *Debugger) InitState(_ *am.Event) {
	d.app = cview.NewApplication()
	d.initUIComponents()
	d.initLayout()
	d.bindKeyboard()
	if d.EnableMouse {
		d.app.EnableMouse(true)
	}

	// redraw on auto states
	go func() {
		// bind to transitions
		txEndCh := d.Mach.On([]string{am.EventTransitionEnd}, nil)
		for event := range txEndCh {
			// TODO typesafe Args
			tx := event.Args["transition"].(*am.Transition)
			if tx.IsAuto() && tx.Accepted {
				d.updateTxBars()
				d.draw()
			}
		}
	}()

	// draw in a goroutine
	go func() {
		d.app.SetRoot(d.layoutRoot, true)
		d.app.SetFocus(d.tree)
		err := d.app.Run()
		if err != nil {
			d.Mach.AddErr(err)
		}
		d.Mach.Remove(am.S{"Init"}, nil)
	}()
}

func (d *Debugger) StateNameSelectedState(e *am.Event) {
	d.selectedState = e.Args["selectedStateName"].(string)
	d.updateTree()
	d.updateKeyBars()
}

func (d *Debugger) StateNameSelectedStateNameSelected(e *am.Event) {
	d.StateNameSelectedState(e)
}

func (d *Debugger) StateNameSelectedEnd(_ *am.Event) {
	d.selectedState = ""
	d.updateTree()
	d.updateKeyBars()
}

func (d *Debugger) LiveViewState(_ *am.Event) {
	if d.cursorTx == 0 {
		return
	}
	d.cursorTx = len(d.msgTxs)
	d.FullRedraw()
}

func (d *Debugger) PlayingState(_ *am.Event) {
	// TODO play by steps
	if d.playTimer == nil {
		d.playTimer = time.NewTicker(playInterval)
	} else {
		d.playTimer.Reset(time.Second)
	}
	ctx := d.Mach.GetStateCtx(ss.Playing)
	go func() {
		d.Mach.Add(am.S{ss.Fwd}, nil)
		for range d.playTimer.C {
			if ctx.Err() != nil {
				break
			}
			d.Mach.Add(am.S{ss.Fwd}, nil)
		}
	}()
}

func (d *Debugger) PlayingEnd(_ *am.Event) {
	d.playTimer.Stop()
}

func (d *Debugger) FwdEnter(_ *am.Event) bool {
	return d.cursorTx < len(d.msgTxs)
}

func (d *Debugger) FwdState(_ *am.Event) {
	defer d.Mach.Remove(am.S{ss.Fwd}, nil)
	d.cursorTx++
	d.cursorStep = 0
	if d.Mach.Is(am.S{ss.Playing}) && d.cursorTx == len(d.msgTxs) {
		d.Mach.Remove(am.S{ss.Playing}, nil)
	}
	d.FullRedraw()
}

func (d *Debugger) RewindEnter(_ *am.Event) bool {
	return d.cursorTx > 0
}

func (d *Debugger) RewindState(_ *am.Event) {
	defer d.Mach.Remove(am.S{ss.Rewind}, nil)
	d.cursorTx--
	d.cursorStep = 0
	d.FullRedraw()
}

func (d *Debugger) FwdStepEnter(_ *am.Event) bool {
	nextTx := d.NextTx()
	if nextTx == nil {
		return false
	}
	return d.cursorStep < len(nextTx.Steps)+1
}

func (d *Debugger) FwdStepState(_ *am.Event) {
	defer d.Mach.Remove(am.S{ss.FwdStep}, nil)
	// next tx
	nextTx := d.NextTx()
	// scroll to the next tx
	if d.cursorStep == len(nextTx.Steps) {
		d.Mach.Add(am.S{ss.Fwd}, nil)
		return
	}
	d.cursorStep++
	d.FullRedraw()
}

func (d *Debugger) RewindStepEnter(_ *am.Event) bool {
	return d.cursorStep > 0 || d.cursorTx > 0
}

func (d *Debugger) RewindStepState(_ *am.Event) {
	defer d.Mach.Remove(am.S{ss.RewindStep}, nil)
	// wrap if theres a prev tx
	if d.cursorStep <= 0 {
		d.cursorTx--
		d.updateLog()
		nextTx := d.NextTx()
		d.cursorStep = len(nextTx.Steps)
	} else {
		d.cursorStep--
	}
	d.FullRedraw()
}

func (d *Debugger) HelpScreenState() {
	if d.helpScreen == nil {
		d.helpScreen = d.initHelpScreen()
	}
	d.treeRoot.ClearChildren()
	d.log.Clear()
	d.msgStruct = nil
	d.msgTxs = []*telemetry.MsgTx{}
	d.Mach.Add(am.S{ss.LiveView}, nil)
}

func (d *Debugger) ClientConnectedState(_ *am.Event) {
	d.Clear()
	for _, box := range d.focusable {
		box.SetBorderColorFocused(colorActive)
	}
	d.draw()
}

func (d *Debugger) ClientConnectedEnd(_ *am.Event) {
	for _, box := range d.focusable {
		box.SetBorderColorFocused(cview.ColorUnset)
	}
	d.Mach.Remove(am.S{ss.LiveView}, nil)
	d.draw()
}

func (d *Debugger) ClientMsgState(e *am.Event) {
	// initial structure data
	_, structOk := e.Args["msg_struct"]
	if structOk {
		msgStruct := e.Args["msg_struct"].(*telemetry.MsgStruct)
		d.handleMsgStruct(msgStruct)
		return
	}
	// transition data
	msgTx := e.Args["msg_tx"].(*telemetry.MsgTx)
	// TODO scalable storage
	d.msgTxs = append(d.msgTxs, msgTx)
	// parse the msg
	d.parseClientMsg(msgTx)
	err := d.appendLog(msgTx)
	if err != nil {
		d.Mach.AddErr(err)
		return
	}
	if d.Mach.Is(am.S{ss.LiveView}) {
		// force the latest tx
		// TODO debounce?
		d.cursorTx = len(d.msgTxs)
		d.cursorStep = 0
		d.updateTxBars()
		d.updateTree()
		d.updateLog()
	}
	d.updateTimelines()
	d.draw()
}
