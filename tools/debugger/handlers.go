// TODO ExceptionState: separate error screen with stack trace

package debugger

import (
	"context"
	"slices"
	"time"

	"github.com/pancsta/cview"
	"golang.org/x/exp/maps"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

func (d *Debugger) StartEnd(_ *am.Event) {
	d.app.Stop()
}

func (d *Debugger) StartState(e *am.Event) {
	clientID, _ := e.Args["Client.id"].(string)
	cursorTx, _ := e.Args["Client.cursorTx"].(int)
	view, _ := e.Args["dbgView"].(string)

	d.app = cview.NewApplication()
	d.P = message.NewPrinter(language.English)
	d.bindKeyboard()
	d.initUIComponents()
	d.initLayout()
	if d.EnableMouse {
		d.app.EnableMouse(true)
	}

	stateCtx := d.Mach.NewStateCtx(ss.Start)

	// redraw on auto states
	go func() {
		// bind to transitions
		txEndCh := d.Mach.OnEvent([]string{am.EventTransitionEnd}, nil)
		for event := range txEndCh {

			if stateCtx.Err() != nil {
				return // expired
			}

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
		if stateCtx.Err() != nil {
			return // expired
		}

		d.app.SetRoot(d.layoutRoot, true)
		d.app.SetFocus(d.sidebar)
		err := d.app.Run()
		if err != nil {
			d.Mach.AddErr(err)
		}

		d.Mach.Remove1(ss.Start, nil)
	}()

	// post-start ops
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}

		// initial view from CLI
		switch view {
		case "tree-matrix":
			d.Mach.Add1(ss.TreeMatrixView, nil)
		case "matrix":
			d.Mach.Add1(ss.MatrixView, nil)
		}

		// boot imported data
		if len(d.Clients) <= 0 {
			d.Mach.Add1(ss.Ready, nil)
			return
		}

		d.buildSidebar(-1)
		if clientID == "" {
			ids := maps.Keys(d.Clients)
			clientID = ids[0]
		}
		d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": clientID})
		<-d.Mach.When1(ss.ClientSelected, nil)

		if stateCtx.Err() != nil {
			return // expired
		}

		if cursorTx != 0 {
			d.Mach.Add1(ss.ScrollToTx, am.A{"Client.cursorTx": cursorTx})
		}

		d.Mach.Add1(ss.Ready, nil)
	}()
}

func (d *Debugger) StateNameSelectedState(e *am.Event) {
	d.C.selectedState = e.Args["selectedStateName"].(string)
	switch d.Mach.Switch(ss.GroupViews...) {

	case ss.TreeLogView:
		d.updateTree()

	case ss.TreeMatrixView:
		d.updateTree()
		d.updateMatrix()

	case ss.MatrixView:
		d.updateMatrix()
	}

	d.updateKeyBars()
}

// StateNameSelectedStateNameSelected handles cursor moving from a state name to
// another state name case.
func (d *Debugger) StateNameSelectedStateNameSelected(e *am.Event) {
	d.StateNameSelectedState(e)
}

func (d *Debugger) StateNameSelectedEnd(_ *am.Event) {
	d.C.selectedState = ""
	d.updateTree()
	d.updateKeyBars()
}

func (d *Debugger) LiveViewEnter(_ *am.Event) bool {
	return d.C != nil
}

func (d *Debugger) LiveViewState(_ *am.Event) {
	d.C.CursorTx = len(d.C.MsgTxs)
	d.C.CursorStep = 0
	d.updateTxBars()
	d.updateTimelines()
	d.draw()
}

func (d *Debugger) PlayingState(_ *am.Event) {
	if d.playTimer == nil {
		d.playTimer = time.NewTicker(playInterval)
	} else {
		// TODO dont reset if resuming after switching clients
		d.playTimer.Reset(playInterval)
	}

	// initial play step
	if d.Mach.Is1(ss.TimelineStepsFocused) {
		d.Mach.Add1(ss.FwdStep, nil)
	} else {
		d.Mach.Add1(ss.Fwd, nil)
	}

	ctx := d.Mach.NewStateCtx(ss.Playing)
	go func() {
		for ctx.Err() == nil {
			select {
			case <-ctx.Done(): // expired

			case <-d.playTimer.C:

				if d.Mach.Is1(ss.TimelineStepsFocused) {
					d.Mach.Add1(ss.FwdStep, nil)
				} else {
					d.Mach.Add1(ss.Fwd, nil)
				}
			}
		}
	}()
}

func (d *Debugger) PlayingEnd(_ *am.Event) {
	d.playTimer.Stop()
}

func (d *Debugger) PausedState(_ *am.Event) {
	// TODO stop scrolling the log when coming from TailMode (confirm)
	d.updateTxBars()
	d.draw()
}

func (d *Debugger) TailModeState(_ *am.Event) {
	// needed bc tail mode if carried over via SelectingClient
	d.updateTxBars()
	d.draw()
}

///// FWD / BACK

func (d *Debugger) UserFwdState(e *am.Event) {
	d.Mach.Remove1(ss.UserFwd, nil)
}

func (d *Debugger) FwdEnter(e *am.Event) bool {
	amount, _ := e.Args["amount"].(int)
	amount = max(amount, 1)
	return d.C.CursorTx+amount <= len(d.C.MsgTxs)
}

func (d *Debugger) FwdState(e *am.Event) {
	d.Mach.Remove1(ss.Fwd, nil)

	amount, _ := e.Args["amount"].(int)
	amount = max(amount, 1)

	d.C.CursorTx += amount
	d.C.CursorStep = 0
	if d.Mach.Is1(ss.Playing) && d.C.CursorTx == len(d.C.MsgTxs) {
		d.Mach.Remove1(ss.Playing, nil)
	}

	d.RedrawFull(false)
}

func (d *Debugger) UserBackState(e *am.Event) {
	d.Mach.Remove1(ss.UserBack, nil)
}

func (d *Debugger) BackEnter(e *am.Event) bool {
	amount, _ := e.Args["amount"].(int)
	amount = max(amount, 1)
	return d.C.CursorTx-amount >= 0
}

func (d *Debugger) BackState(e *am.Event) {
	d.Mach.Remove1(ss.Back, nil)

	amount, _ := e.Args["amount"].(int)
	amount = max(amount, 1)

	d.C.CursorTx -= amount
	d.C.CursorStep = 0

	d.RedrawFull(false)
}

///// STEP BACK / FWD

func (d *Debugger) UserFwdStepState(e *am.Event) {
	d.Mach.Remove1(ss.UserFwdStep, nil)
}

func (d *Debugger) FwdStepEnter(_ *am.Event) bool {
	nextTx := d.NextTx()
	if nextTx == nil {
		return false
	}
	return d.C.CursorStep < len(nextTx.Steps)+1
}

func (d *Debugger) FwdStepState(_ *am.Event) {
	d.Mach.Remove1(ss.FwdStep, nil)

	// next tx
	nextTx := d.NextTx()
	// scroll to the next tx
	if d.C.CursorStep == len(nextTx.Steps) {
		d.Mach.Add1(ss.Fwd, nil)
		return
	}
	d.C.CursorStep++
	d.RedrawFull(false)
}

func (d *Debugger) UserBackStepState(e *am.Event) {
	d.Mach.Remove1(ss.UserBackStep, nil)
}

func (d *Debugger) BackStepEnter(_ *am.Event) bool {
	return d.C.CursorStep > 0 || d.C.CursorTx > 0
}

func (d *Debugger) BackStepState(_ *am.Event) {
	d.Mach.Remove1(ss.BackStep, nil)

	// wrap if there's a prev tx
	if d.C.CursorStep <= 0 {
		d.C.CursorTx--
		d.updateLog(false)
		nextTx := d.NextTx()
		d.C.CursorStep = len(nextTx.Steps)
	} else {
		d.C.CursorStep--
	}

	d.RedrawFull(false)
}

func (d *Debugger) TimelineStepsFocusedState(_ *am.Event) {
	d.RedrawFull(false)
}

func (d *Debugger) TimelineStepsFocusedEnd(_ *am.Event) {
	d.RedrawFull(false)
}

///// CONNECTION

func (d *Debugger) ConnectEventEnter(e *am.Event) bool {
	_, ok1 := e.Args["msg_struct"].(*telemetry.DbgMsgStruct)
	_, ok2 := e.Args["conn_id"].(string)
	if !ok1 || !ok2 {
		d.Mach.Log("Error: msg_struct malformed\n")
		return false
	}
	return true
}

func (d *Debugger) ConnectEventState(e *am.Event) {
	// initial structure data
	msg := e.Args["msg_struct"].(*telemetry.DbgMsgStruct)
	connID := e.Args["conn_id"].(string)
	c := d.Clients[msg.ID]

	// cleanup removes all previous clients, if all are disconnected
	cleanup := false
	if d.CleanOnConnect {
		// remove old clients
		cleanup = d.doCleanOnConnect()
	}

	if c == nil {
		// new client
		c = &Client{
			id:        msg.ID,
			connID:    connID,
			connected: true,
			Exportable: Exportable{
				MsgStruct: msg,
			},
		}

		d.Clients[msg.ID] = c
		if !cleanup {
			d.buildSidebar(-1)
		}

	} else {
		// update the existing client
		c = &Client{
			id:        msg.ID,
			connID:    connID,
			connected: true,
			Exportable: Exportable{
				MsgStruct: msg,
			},
		}
		d.Clients[msg.ID] = c

		// currently selected - refresh the UI
		if !cleanup {
			if d.C != nil && d.C.id == msg.ID {
				d.C = c
				d.log.Clear()
				d.updateTimelines()
				d.updateTxBars()
				// update the tree in case the struct changed
				d.buildStatesTree()
				d.updateBorderColor()
			}
			d.updateSidebar(true)
		}
	}

	// rebuild the UI in case of a cleanup
	if cleanup {
		// select the new (and only) client
		d.C = c
		d.log.Clear()
		d.updateTimelines()
		d.updateTxBars()
		d.updateBorderColor()
		d.buildSidebar(0)
		// initial build of the states tree
		d.buildStatesTree()
		d.updateViews(false)
	}

	// remove the last active client if over the limit
	// TODO prioritize disconns
	if len(d.Clients) > maxClients {
		var (
			lastActiveTime time.Time
			lastActiveID   string
		)
		// TODO get time from msgs
		for id, c := range d.Clients {
			if c.lastActive.After(lastActiveTime) || lastActiveID == "" {
				lastActiveTime = c.lastActive
				lastActiveID = id
			}
		}
		d.Mach.Add1(ss.RemoveClient, am.A{"Client.id": lastActiveID})
	}

	// if only 1 client connected, select it
	// if the only client in total, select it
	if len(d.Clients) == 1 || (d.SelectConnected && d.ConnectedClients() == 1) {
		d.Mach.Add1(ss.SelectingClient, am.A{
			"Client.id": msg.ID,
			// mark the origin
			"from_connected": true,
		})
	}

	// first client, tail mode
	if len(d.Clients) == 1 {
		d.Mach.Add1(ss.TailMode, nil)
	}

	d.draw()
}

func (d *Debugger) DisconnectEventEnter(e *am.Event) bool {
	_, ok := e.Args["conn_id"].(string)
	if !ok {
		d.Mach.Log("Error: DisconnectEvent malformed\n")
		return false
	}

	return true
}

func (d *Debugger) DisconnectEventState(e *am.Event) {
	d.Mach.Remove1(ss.DisconnectEvent, nil)

	connID := e.Args["conn_id"].(string)
	for _, c := range d.Clients {
		if c.connID != "" && c.connID == connID {
			// mark as disconnected
			c.connected = false
			break
		}
	}

	d.updateBorderColor()
	d.updateSidebar(true)
	d.draw()
}

///// CLIENTS

func (d *Debugger) ClientMsgEnter(e *am.Event) bool {
	_, ok := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)
	if !ok {
		d.Mach.Log("Error: msg_tx malformed\n")
		return false
	}

	return true
}

func (d *Debugger) ClientMsgState(e *am.Event) {
	msgs := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)

	// add a timestamp
	updateLive := false
	updateFirstTx := false
	for _, msg := range msgs {

		c := d.Clients[msg.MachineID]
		if _, ok := d.Clients[msg.MachineID]; !ok {
			d.Mach.Log("Error: client not found: %s\n", msg.MachineID)
			continue
		}

		if c.MsgStruct == nil {
			d.Mach.Log("Error: struct missing for %s, ignoring tx\n", msg.MachineID)
			continue
		}

		// TODO scalable storage
		index := len(c.MsgTxs)
		c.MsgTxs = append(c.MsgTxs, msg)
		// parse the msg
		d.parseClientMsg(c, len(c.MsgTxs)-1)

		// update the UI
		// TODO debounce UI updates

		if c == d.C {
			err := d.appendLog(index)
			if err != nil {
				d.Mach.Log("Error: log append %s\n", err)
				// d.Mach.AddErr(err)
				return
			}
			if d.Mach.Is1(ss.TailMode) {
				updateLive = true
				c.CursorTx = len(c.MsgTxs)
				c.CursorStep = 0
			}
			// update Tx info on the first Tx
			if len(c.MsgTxs) == 1 {
				updateFirstTx = true
			}
		}
	}

	d.updateSidebar(false)
	// UI updates for the selected client
	if updateLive {
		// force the latest tx
		d.updateViews(false)
	}

	// update Tx info on the first Tx
	if updateLive || updateFirstTx {
		d.updateTxBars()
	}

	// timelines always change
	d.updateTimelines()

	d.draw()
}

func (d *Debugger) RemoveClientEnter(e *am.Event) bool {
	cid, ok := e.Args["Client.id"].(string)
	return ok && cid != ""
}

func (d *Debugger) RemoveClientState(e *am.Event) {
	d.Mach.Remove1(ss.RemoveClient, nil)

	cid := e.Args["Client.id"].(string)
	c := d.Clients[cid]
	if c == nil {
		d.Mach.Log("Error: cant remove client %s: not found", cid)
		return
	}

	// clean up
	delete(d.Clients, cid)
	c.MsgStruct = nil
	c.MsgTxs = nil
	c.logMsgs = nil
	c.msgTxsParsed = nil
	c.CursorTx = 0
	c.CursorStep = 0

	// if currently selected, switch to the first one
	if c == d.C {
		for id := range d.Clients {
			d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": id})
			break
		}
		// if last client, unselect
		if len(d.Clients) == 0 {
			d.Mach.Remove1(ss.ClientSelected, nil)
		}
		d.buildSidebar(d.sidebar.GetCurrentItemIndex() - 1)
	} else {
		d.buildSidebar(-1)
	}
	d.draw()
}

// TODO SelectingClientSelectingClient

func (d *Debugger) SelectingClientEnter(e *am.Event) bool {
	cid, ok1 := e.Args["Client.id"].(string)
	// same client
	if d.C != nil && cid == d.C.id {
		return false
	}
	// does the client exist?
	_, ok2 := d.Clients[cid]

	return len(d.Clients) > 0 && ok1 && ok2
}

func (d *Debugger) SelectingClientState(e *am.Event) {
	clientID := e.Args["Client.id"].(string)
	fromConnected, _ := e.Args["from_connected"].(bool)
	fromPlaying := slices.Contains(e.Transition().StatesBefore, ss.Playing)

	if d.Clients[clientID] == nil {
		// TODO handle err, remove state
		d.Mach.Log("Error: client not found: %s\n", clientID)

		return
	}

	ctx := d.Mach.NewStateCtx(ss.SelectingClient)
	// select the new default client
	d.C = d.Clients[clientID]
	// re-feed the whole log and pass the context to allow cancellation
	logRebuildEnd := len(d.C.logMsgs)
	d.logRebuildEnd = logRebuildEnd
	// remain in TailMode after the selection
	wasTailMode := slices.Contains(e.Transition().StatesBefore, ss.TailMode)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// scroll to the same place as the prev client
		match := false
		if !wasTailMode {
			for i, tx := range d.C.MsgTxs {
				if tx.Time.After(d.prevClientTxTime) {

					// pick the closer one
					if i > 0 && tx.Time.Sub(d.prevClientTxTime) >
						d.prevClientTxTime.Sub(*d.C.MsgTxs[i-1].Time) {
						d.C.CursorTx = i
					} else {
						d.C.CursorTx = i + 1
					}
					match = true

					break
				}
			}
		}

		// or scroll to the last one
		if !match {
			d.C.CursorTx = len(d.C.MsgTxs)
		}
		d.C.CursorStep = 0
		d.updateTimelines()
		d.updateTxBars()
		d.updateSidebar(true)

		// scroll into view
		selOffset := d.getSidebarCurrClientIdx()
		offset, _ := d.sidebar.GetOffset()
		_, _, _, lines := d.sidebar.GetInnerRect()
		if selOffset-offset > lines {
			d.sidebar.SetCurrentItem(selOffset)
			d.updateSidebar(false)
		}

		// initial build of the states tree
		d.buildStatesTree()
		if d.Mach.Is1(ss.TreeLogView) || d.Mach.Is1(ss.TreeMatrixView) {
			d.updateTree()
		}

		// rebuild the whole log, keep an eye on the ctx
		// TODO cache in single []byte
		for i := 0; i < logRebuildEnd && ctx.Err() == nil; i++ {
			err := d.appendLog(i)
			if err != nil {
				d.Mach.Log("Error: log rebuild %s\n", err)
			}
		}
		if ctx.Err() != nil {
			return // expired
		}
		d.draw()

		target := am.S{ss.ClientSelected}
		if wasTailMode {
			target = append(target, ss.TailMode)
		}

		d.Mach.Add(target, am.A{
			"ctx":            ctx,
			"from_connected": fromConnected,
			"from_playing":   fromPlaying,
		})
	}()
}

func (d *Debugger) ClientSelectedState(e *am.Event) {
	ctx := e.Args["ctx"].(context.Context)
	fromConnected, _ := e.Args["from_connected"].(bool)
	fromPlaying, _ := e.Args["from_playing"].(bool)
	if ctx.Err() != nil {
		d.Mach.Log("Error: context expired\n")
		return // expired
	}

	// catch up with new log msgs
	for i := d.logRebuildEnd; i < len(d.C.logMsgs); i++ {
		err := d.appendLog(i)
		if err != nil {
			d.Mach.Log("Error: log rebuild %s\n", err)
		}
	}

	// update views
	if d.Mach.Is1(ss.TreeLogView) {
		d.updateLog(false)
	}
	if d.Mach.Is1(ss.MatrixView) || d.Mach.Is1(ss.TreeMatrixView) {
		d.updateMatrix()
	}

	// first client connected, set tail mode
	if fromConnected && len(d.Clients) == 1 {
		d.Mach.Add1(ss.TailMode, nil)
	} else if fromPlaying {
		d.Mach.Add1(ss.Playing, nil)
	}

	d.updateBorderColor()

	d.draw()
}

func (d *Debugger) ClientSelectedEnd(e *am.Event) {
	// memorize the current tx time
	if d.C != nil {
		if d.C.CursorTx > 0 && d.C.CursorTx <= len(d.C.MsgTxs) {
			d.prevClientTxTime = *d.C.MsgTxs[d.C.CursorTx-1].Time
		}
	}

	// clean up, except when switching to SelectingClient
	if !e.Mutation().StateWasCalled(ss.SelectingClient) {
		d.C = nil
	}

	d.log.Clear()
	d.treeRoot.ClearChildren()
	d.RedrawFull(true)
}

func (d *Debugger) HelpDialogState(e *am.Event) {
	// TODO use Visibility instead of SendToFront
	d.layoutRoot.SendToFront("main")
	d.layoutRoot.SendToFront("help")
}

func (d *Debugger) HelpDialogEnd(e *am.Event) {
	diff := am.DiffStates(ss.GroupDialog, e.Transition().TargetStates)
	if len(diff) == len(ss.GroupDialog) {
		// all dialogs closed, show main
		d.layoutRoot.SendToFront("main")
	}
}

func (d *Debugger) ExportDialogState(e *am.Event) {
	// TODO use Visibility instead of SendToFront
	d.layoutRoot.SendToFront("main")
	d.layoutRoot.SendToFront("export")
}

func (d *Debugger) ExportDialogEnd(e *am.Event) {
	diff := am.DiffStates(ss.GroupDialog, e.Transition().TargetStates)
	if len(diff) == len(ss.GroupDialog) {
		// all dialogs closed, show main
		d.layoutRoot.SendToFront("main")
	}
}

func (d *Debugger) MatrixViewState(_ *am.Event) {
	d.drawViews()
}

func (d *Debugger) MatrixViewEnd(_ *am.Event) {
	d.drawViews()
}

func (d *Debugger) TreeMatrixViewState(_ *am.Event) {
	d.drawViews()
}

func (d *Debugger) TreeMatrixViewEnd(_ *am.Event) {
	d.drawViews()
}

func (d *Debugger) ScrollToTxEnter(e *am.Event) bool {
	cursor, ok := e.Args["Client.cursorTx"].(int)
	c := d.C
	return ok && c != nil && len(c.MsgTxs) > cursor+1
}

func (d *Debugger) ScrollToTxState(e *am.Event) {
	d.Mach.Remove1(ss.ScrollToTx, nil)

	cursor := e.Args["Client.cursorTx"].(int)
	d.C.CursorTx = cursor
	d.RedrawFull(false)
}
