// TODO ExceptionState: separate error screen with stack trace

package debugger

import (
	"context"
	"fmt"
	"math"
	"os"
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

func (d *Debugger) StartState(e *am.Event) {
	clientID, _ := e.Args["Client.id"].(string)
	cursorTx, _ := e.Args["Client.cursorTx"].(int)
	view, _ := e.Args["dbgView"].(string)

	d.App = cview.NewApplication()
	if d.Opts.Screen != nil {
		d.App.SetScreen(d.Opts.Screen)
	}
	d.P = message.NewPrinter(language.English)
	d.bindKeyboard()
	d.initUIComponents()
	d.initLayout()
	if d.Opts.EnableMouse {
		d.App.EnableMouse(true)
	}

	stateCtx := d.Mach.NewStateCtx(ss.Start)

	// draw in a goroutine
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}
		d.Mach.PanicToErr(nil)

		d.App.SetRoot(d.LayoutRoot, true)
		d.App.SetFocus(d.clientList)
		err := d.App.Run()
		if err != nil {
			d.Mach.AddErr(err, nil)
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

		d.buildClientList(-1)
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

func (d *Debugger) StartEnd(_ *am.Event) {
	d.App.Stop()
}

func (d *Debugger) ReadyState(_ *am.Event) {
	d.healthcheck = time.NewTicker(healthcheckInterval)

	// unblock
	go func() {
		for {
			select {
			case <-d.healthcheck.C:
				d.Mach.Add1(ss.Healthcheck, nil)

			case <-d.Mach.Ctx().Done():
				d.healthcheck.Stop()
			}
		}
	}()
}

func (d *Debugger) ReadyEnd(_ *am.Event) {
	d.healthcheck.Stop()
}

func (d *Debugger) HealthcheckState(_ *am.Event) {
	d.Mach.Remove1(ss.Healthcheck, nil)
	d.checkGcMsgs()
}

// AnyState is a global final handler
func (d *Debugger) AnyState(e *am.Event) {
	tx := e.Transition()

	// redraw on auto states
	// TODO this should be done better
	if tx.IsAuto() && tx.Accepted {
		d.updateTxBars()
		d.draw()
	}
}

func (d *Debugger) StateNameSelectedState(e *am.Event) {
	// TODO guard
	d.C.SelectedState = e.Args["state"].(string)
	d.lastSelectedState = d.C.SelectedState

	switch d.Mach.Switch(ss.GroupViews) {

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
	d.C.SelectedState = ""
	d.updateTree()
	d.updateKeyBars()
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
	d.SetCursor(d.filterTxCursor(d.C, len(d.C.MsgTxs), false))
	d.updateMatrixRain()
	d.updateClientList(true)
	// needed bc tail mode if carried over via SelectingClient
	d.RedrawFull(true)
}

func (d *Debugger) TailModeEnd(_ *am.Event) {
	d.updateMatrixRain()
	d.RedrawFull(true)
}

// ///// FWD / BACK

func (d *Debugger) UserFwdState(_ *am.Event) {
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

	c := d.C
	d.SetCursor(d.filterTxCursor(c, c.CursorTx+amount, true))
	c.CursorStep = 0
	d.handleTStepsScrolled()
	if d.Mach.Is1(ss.Playing) && c.CursorTx == len(c.MsgTxs) {
		d.Mach.Remove1(ss.Playing, nil)
	}

	d.memorizeTxTime(c)
	// sidebar for errs
	d.updateClientList(true)

	d.RedrawFull(false)
}

func (d *Debugger) UserBackState(_ *am.Event) {
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

	c := d.C
	d.SetCursor(d.filterTxCursor(d.C, c.CursorTx-amount, false))
	c.CursorStep = 0
	d.handleTStepsScrolled()

	d.memorizeTxTime(c)
	// sidebar for errs
	d.updateClientList(true)

	d.RedrawFull(false)
}

// ///// STEP BACK / FWD

func (d *Debugger) UserFwdStepState(_ *am.Event) {
	d.Mach.Remove1(ss.UserFwdStep, nil)
}

func (d *Debugger) FwdStepEnter(_ *am.Event) bool {
	nextTx := d.nextTx()
	if nextTx == nil {
		return false
	}
	return d.C.CursorStep < len(nextTx.Steps)+1
}

func (d *Debugger) FwdStepState(_ *am.Event) {
	d.Mach.Remove1(ss.FwdStep, nil)

	// next tx
	nextTx := d.nextTx()
	// scroll to the next tx
	if d.C.CursorStep == len(nextTx.Steps) {
		d.Mach.Add1(ss.Fwd, nil)
		return
	}
	d.C.CursorStep++

	d.handleTStepsScrolled()
	d.RedrawFull(false)
}

func (d *Debugger) UserBackStepState(_ *am.Event) {
	d.Mach.Remove1(ss.UserBackStep, nil)
}

func (d *Debugger) BackStepEnter(_ *am.Event) bool {
	return d.C.CursorStep > 0 || d.C.CursorTx > 0
}

func (d *Debugger) BackStepState(_ *am.Event) {
	d.Mach.Remove1(ss.BackStep, nil)

	// wrap if there's a prev tx
	if d.C.CursorStep <= 0 {
		d.SetCursor(d.filterTxCursor(d.C, d.C.CursorTx-1, false))
		d.updateLog(false)
		nextTx := d.nextTx()
		d.C.CursorStep = len(nextTx.Steps)

	} else {
		d.C.CursorStep--
	}

	d.updateClientList(false)
	d.handleTStepsScrolled()
	d.RedrawFull(false)
}

func (d *Debugger) handleTStepsScrolled() {
	// TODO merge with a CursorStep setter
	tStepsScrolled := d.C.CursorStep != 0

	if tStepsScrolled {
		d.Mach.Add1(ss.TimelineStepsScrolled, nil)
	} else {
		d.Mach.Remove1(ss.TimelineStepsScrolled, nil)
	}
}

func (d *Debugger) TimelineStepsFocusedState(_ *am.Event) {
	// keep in sync with initLayout()
	// TODO support no reader
	d.treeLogGrid.UpdateItem(d.tree, 0, 0, 1, 3, 0, 0, false)

	if d.Mach.Is1(ss.LogReaderVisible) {
		d.treeLogGrid.UpdateItem(d.log, 0, 3, 1, 2, 0, 0, false)
		d.treeLogGrid.UpdateItem(d.logReader, 0, 5, 1, 1, 0, 0, false)
	} else {
		d.treeLogGrid.UpdateItem(d.log, 0, 3, 1, 3, 0, 0, false)
	}

	d.treeMatrixGrid.UpdateItem(d.tree, 0, 0, 1, 3, 0, 0, false)
	d.treeMatrixGrid.UpdateItem(d.matrix, 0, 3, 1, 3, 0, 0, false)

	d.RedrawFull(false)
}

func (d *Debugger) TimelineStepsFocusedEnd(_ *am.Event) {
	// keep in sync with initLayout()
	// TODO support no reader
	d.treeLogGrid.UpdateItem(d.tree, 0, 0, 1, 2, 0, 0, false)

	if d.Mach.Is1(ss.LogReaderVisible) {
		d.treeLogGrid.UpdateItem(d.log, 0, 2, 1, 2, 0, 0, false)
		d.treeLogGrid.UpdateItem(d.logReader, 0, 4, 1, 2, 0, 0, false)
	} else {
		d.treeLogGrid.UpdateItem(d.log, 0, 2, 1, 4, 0, 0, false)
	}

	d.treeMatrixGrid.UpdateItem(d.tree, 0, 0, 1, 2, 0, 0, false)
	d.treeMatrixGrid.UpdateItem(d.matrix, 0, 2, 1, 4, 0, 0, false)

	d.RedrawFull(false)
}

func (d *Debugger) FiltersFocusedEnter(e *am.Event) bool {
	f, ok1 := e.Args["filter"]
	_, ok2 := f.(FilterName)

	// both or none
	return ok1 && ok2 || (!ok1 && !ok2)
}

func (d *Debugger) FiltersFocusedState(e *am.Event) {
	if filter, ok := e.Args["filter"].(FilterName); ok {
		d.focusedFilter = filter
	}
	d.filtersBar.SetBackgroundColor(cview.Styles.MoreContrastBackgroundColor)
	d.updateFiltersBar()
}

func (d *Debugger) FiltersFocusedEnd(_ *am.Event) {
	d.filtersBar.SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.updateFiltersBar()
}

// ///// CONNECTION

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
	d.Mach.Remove1(ss.ConnectEvent, nil)

	// initial structure data
	msg := e.Args["msg_struct"].(*telemetry.DbgMsgStruct)
	connID := e.Args["conn_id"].(string)
	var c *Client

	// cleanup removes all previous clients, if all are disconnected
	cleanup := false
	if d.Opts.CleanOnConnect {
		// remove old clients
		cleanup = d.doCleanOnConnect()
	}

	// always create a new client on struct msgs
	// TODO rename and keep the old client
	c = &Client{
		id:     msg.ID,
		connID: connID,
		Exportable: Exportable{
			MsgStruct: msg,
		},
	}
	c.connected.Store(true)

	d.Clients[msg.ID] = c
	if !cleanup {
		d.buildClientList(-1)
	}

	// rebuild the UI in case of a cleanup or connect under the same ID
	if cleanup || (d.C != nil && d.C.id == msg.ID) {
		// select the new (and only) client
		d.C = c
		d.log.Clear()
		d.updateTimelines()
		d.updateTxBars()
		d.updateBorderColor()
		d.buildClientList(0)
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
			active := c.lastActive()
			if active.After(lastActiveTime) || lastActiveID == "" {
				lastActiveTime = active
				lastActiveID = id
			}
		}
		d.Mach.Add1(ss.RemoveClient, am.A{"Client.id": lastActiveID})
	}

	// if only 1 client connected, select it
	// if the only client in total, select it
	if len(d.Clients) == 1 || (d.Opts.SelectConnected &&
		d.ConnectedClients() == 1) {

		d.Mach.Add1(ss.SelectingClient, am.A{
			"Client.id": msg.ID,
			// mark the origin
			"from_connected": true,
		})

		// re-select the state
		if d.lastSelectedState != "" {
			d.Mach.Add1(ss.StateNameSelected, am.A{"state": d.lastSelectedState})
			// TODO Keep in StateNameSelected behind a flag
			d.selectTreeState(d.lastSelectedState)
		}
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
			c.connected.Store(false)
			break
		}
	}

	d.updateBorderColor()
	d.updateClientList(true)
	d.draw()
}

// ///// CLIENTS

func (d *Debugger) ClientMsgEnter(e *am.Event) bool {
	_, ok := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)
	if !ok {
		d.Mach.Log("Error: msg_tx malformed\n")
		return false
	}

	return true
}

func (d *Debugger) ClientMsgState(e *am.Event) {
	d.Mach.Remove1(ss.ClientMsg, nil)

	// TODO make it async via a dedicated goroutine, pushing results to
	//  async multi state ClientMsgDone

	msgs := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)
	mach := d.Mach

	updateTailMode := false
	updateFirstTx := false
	for _, msg := range msgs {

		// TODO check tokens
		machId := msg.MachineID
		c := d.Clients[machId]
		if _, ok := d.Clients[machId]; !ok {
			d.Mach.Log("Error: client not found: %s\n", machId)
			continue
		}

		if c.MsgStruct == nil {
			d.Mach.Log("Error: struct missing for %s, ignoring tx\n", machId)
			continue
		}

		// TODO scalable storage (support filtering)
		idx := len(c.MsgTxs)
		c.MsgTxs = append(c.MsgTxs, msg)
		// parse the msg
		d.parseMsg(c, idx)
		filterOK := d.filterTx(c, idx, mach.Is1(ss.FilterCanceledTx),
			mach.Is1(ss.FilterAutoTx), mach.Is1(ss.FilterEmptyTx))
		if filterOK {
			c.msgTxsFiltered = append(c.msgTxsFiltered, idx)
		}

		// update the UI
		// TODO debounce UI updates

		if c == d.C {
			err := d.appendLogEntry(idx)
			if err != nil {
				d.Mach.Log("Error: log append %s\n", err)
				// d.Mach.AddErr(err, nil)
				return
			}
			if d.Mach.Is1(ss.TailMode) {
				updateTailMode = true
				c.CursorStep = 0
			}

			// update Tx info on the first Tx
			if len(c.MsgTxs) == 1 {
				updateFirstTx = true
			}
		}
	}

	// UI updates for the selected client
	if updateTailMode {
		// force the latest tx
		d.SetCursor(d.filterTxCursor(d.C, len(d.C.MsgTxs), false))
		// sidebar for errs
		d.updateViews(false)
	}

	// update Tx info on the first Tx
	if updateTailMode || updateFirstTx {
		d.updateTxBars()
	}

	// timelines always change
	d.updateClientList(false)
	d.updateTimelines()

	d.draw()
}

func (d *Debugger) SetCursor(cursor int) {
	// TODO validate
	d.C.CursorTx = cursor
	if cursor == 0 {
		d.lastScrolledTxTime = time.Time{}
	} else {
		d.lastScrolledTxTime = *d.currentTx().Time
	}
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
	// c.MsgStruct = nil
	// c.MsgTxs = nil
	// c.logMsgs = nil
	// c.msgTxsParsed = nil
	// d.SetCursor(0)
	// c.CursorStep = 0

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
		d.buildClientList(d.clientList.GetCurrentItemIndex() - 1)
	} else {
		d.buildClientList(-1)
	}

	d.draw()
}

// TODO SelectingClientSelectingClient (for?)

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
	fromPlaying := slices.Contains(e.Transition().StatesBefore(), ss.Playing)

	if d.Clients[clientID] == nil {
		// TODO handle err, remove state
		d.Mach.Log("Error: client not found: %s\n", clientID)

		return
	}

	ctx := d.Mach.NewStateCtx(ss.SelectingClient)
	// select the new default client
	d.C = d.Clients[clientID]
	// re-feed the whole log and pass the context to allow cancelation
	logRebuildEnd := len(d.C.logMsgs)
	d.logRebuildEnd = logRebuildEnd
	// remain in TailMode after the selection
	wasTailMode := slices.Contains(e.Transition().StatesBefore(), ss.TailMode)

	// TODO dont fork once progressive log rendering is in place
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// start with prepping the data
		d.filterClientTxs()

		// scroll to the same place as the prev client
		match := false
		if !wasTailMode {
			match = d.scrollToTime(d.lastScrolledTxTime, true)
		}

		// or scroll to the last one
		if !match {
			d.SetCursor(d.filterTxCursor(d.C, len(d.C.MsgTxs), false))
		}
		d.C.CursorStep = 0
		d.updateTimelines()
		d.updateTxBars()
		d.updateClientList(true)

		// scroll into view
		selOffset := d.getSidebarCurrClientIdx()
		offset, _ := d.clientList.GetOffset()
		_, _, _, lines := d.clientList.GetInnerRect()
		if selOffset-offset > lines {
			d.clientList.SetCurrentItem(selOffset)
			d.updateClientList(false)
		}

		// initial build of the states tree
		d.buildStatesTree()
		if d.Mach.Is1(ss.TreeLogView) || d.Mach.Is1(ss.TreeMatrixView) {
			d.updateTree()
		}

		// rebuild the whole log, keep an eye on the ctx
		err := d.rebuildLog(ctx, logRebuildEnd)
		if err != nil {
			d.Mach.AddErr(err, nil)
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
		err := d.appendLogEntry(i)
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

	// re-select the state
	if d.lastSelectedState != "" {
		d.Mach.Add1(ss.StateNameSelected, am.A{"state": d.lastSelectedState})
		// TODO Keep in StateNameSelected behind a flag
		d.selectTreeState(d.lastSelectedState)
	}

	go d.focusManager.Focus(d.clientList)
	d.updateBorderColor()
	d.draw()
}

func (d *Debugger) ClientSelectedEnd(e *am.Event) {
	// clean up, except when switching to SelectingClient
	if !e.Mutation().StateWasCalled(ss.SelectingClient) {
		d.C = nil
	}

	d.log.Clear()
	d.treeRoot.ClearChildren()
	d.RedrawFull(true)
}

func (d *Debugger) HelpDialogState(_ *am.Event) {
	// TODO use Visibility instead of SendToFront
	d.LayoutRoot.SendToFront("main")
	d.LayoutRoot.SendToFront("help")
}

func (d *Debugger) HelpDialogEnd(e *am.Event) {
	diff := am.DiffStates(ss.GroupDialog, e.Transition().TargetStates())
	if len(diff) == len(ss.GroupDialog) {
		// all dialogs closed, show main
		d.LayoutRoot.SendToFront("main")
	}
}

func (d *Debugger) ExportDialogState(_ *am.Event) {
	// TODO use Visibility instead of SendToFront
	d.LayoutRoot.SendToFront("main")
	d.LayoutRoot.SendToFront("export")
}

func (d *Debugger) ExportDialogEnd(e *am.Event) {
	diff := am.DiffStates(ss.GroupDialog, e.Transition().TargetStates())
	if len(diff) == len(ss.GroupDialog) {
		// all dialogs closed, show main
		d.LayoutRoot.SendToFront("main")
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

func (d *Debugger) MatrixRainState(_ *am.Event) {
	d.drawViews()
}

func (d *Debugger) ScrollToTxEnter(e *am.Event) bool {
	cursor, ok := e.Args["Client.cursorTx"].(int)
	c := d.C
	return ok && c != nil && len(c.MsgTxs) > cursor+1
}

// ScrollToTxState scrolls to a specific transition.
func (d *Debugger) ScrollToTxState(e *am.Event) {
	d.Mach.Remove1(ss.ScrollToTx, nil)

	cursor := e.Args["Client.cursorTx"].(int)
	d.SetCursor(d.filterTxCursor(d.C, cursor, true))
	// reset the step timeline
	d.C.CursorStep = 0
	d.updateClientList(false)
	d.RedrawFull(false)
}

func (d *Debugger) ToggleFilterState(_ *am.Event) {
	// TODO split the state into an async one
	filterTxs := false

	switch d.focusedFilter {
	// TODO move logic after toggle to handlers

	case filterCanceledTx:
		d.Mach.Toggle1(ss.FilterCanceledTx)
		filterTxs = true

	case filterAutoTx:
		d.Mach.Toggle1(ss.FilterAutoTx)
		filterTxs = true

	case filterEmptyTx:
		d.Mach.Toggle1(ss.FilterEmptyTx)
		filterTxs = true

	case FilterSummaries:
		d.Mach.Toggle1(ss.FilterSummaries)

	case filterLog0:
		d.Opts.Filters.LogLevel = am.LogNothing
	case filterLog1:
		d.Opts.Filters.LogLevel = am.LogChanges
	case filterLog2:
		d.Opts.Filters.LogLevel = am.LogOps
	case filterLog3:
		d.Opts.Filters.LogLevel = am.LogDecisions
	case filterLog4:
		d.Opts.Filters.LogLevel = am.LogEverything
	}

	stateCtx := d.Mach.NewStateCtx(ss.ToggleFilter)

	// process the filter change
	go d.ProcessFilterChange(stateCtx, filterTxs)
}

func (d *Debugger) SwitchingClientTxState(e *am.Event) {
	clientID, _ := e.Args["Client.id"].(string)
	cursorTx, _ := e.Args["Client.cursorTx"].(int)
	ctx := d.Mach.NewStateCtx(ss.SwitchingClientTx)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		if d.C != nil && d.C.id != clientID {
			when := d.Mach.WhenTicks(ss.ClientSelected, 2, ctx)
			d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": clientID})
			<-when
			if ctx.Err() != nil {
				return // expired
			}
		}

		when := d.Mach.WhenTicks(ss.ScrollToTx, 2, ctx)
		d.Mach.Add1(ss.ScrollToTx, am.A{"Client.cursorTx": cursorTx})
		<-when
		if ctx.Err() != nil {
			return // expired
		}

		d.Mach.Add1(ss.SwitchedClientTx, nil)
	}()
}

func (d *Debugger) SwitchedClientTxState(_ *am.Event) {
	d.Mach.Remove1(ss.SwitchedClientTx, nil)
}

// ScrollToMutTxState scrolls to a transition which mutated the passed state,
// If fwd is true, it scrolls forward, otherwise backwards.
func (d *Debugger) ScrollToMutTxState(e *am.Event) {
	d.Mach.Remove1(ss.ScrollToMutTx, nil)

	// TODO validate in Enter and add a sentinel error ErrInvalidArgs
	state, _ := e.Args["state"].(string)
	fwd, _ := e.Args["fwd"].(bool)

	c := d.C
	if c == nil {
		return
	}
	step := -1
	if fwd {
		step = 1
	}

	for i := c.CursorTx + step; i > 0 && i < len(c.MsgTxs)+1; i = i + step {

		msgIdx := i - 1
		parsed := c.msgTxsParsed[msgIdx]
		tx := c.MsgTxs[msgIdx]

		// check mutations and canceled
		if !slices.Contains(c.indexesToStates(parsed.StatesAdded), state) &&
			!slices.Contains(c.indexesToStates(parsed.StatesRemoved), state) &&
			!slices.Contains(tx.CalledStateNames(c.MsgStruct.StatesIndex), state) {

			continue
		}

		// skip filtered out
		if d.isFiltered() && !slices.Contains(c.msgTxsFiltered, msgIdx) {
			continue
		}

		// scroll to this tx
		d.Mach.Add1(ss.ScrollToTx, am.A{"Client.cursorTx": i})
		break
	}

	d.memorizeTxTime(c)
}

// ExceptionState creates a log file with the error and stack trace, after
// calling the super exception handler.
func (d *Debugger) ExceptionState(e *am.Event) {
	d.ExceptionHandler.ExceptionState(e)

	trace := ""
	if errTrace, ok := e.Args["err.trace"].(string); ok {
		trace = errTrace
	}

	d.updateBorderColor()

	// create a log file
	s := fmt.Sprintf("%s\n%s\n\n%s", time.Now(), d.Mach.Err(), trace)
	err := os.WriteFile("am-dbg-err.log", []byte(s), 0o644)
	if err != nil {
		d.Mach.Log("Error: %s\n", err)
	}
}

// TODO Enter with ParseArgs

func (d *Debugger) GcMsgsState(e *am.Event) {
	// TODO log reader entries
	d.Mach.Remove1(ss.GcMsgs, nil)
	// TODO ParseArgs
	clients := e.Args["[]*Clients"].([]*Client)
	msgs := e.Args["msgCount"].(int)
	ctx := d.Mach.NewStateCtx(ss.GcMsgs)

	// get oldest clients
	slices.SortFunc(clients, func(c1, c2 *Client) int {
		if len(c1.MsgTxs) == 0 || len(c2.MsgTxs) == 0 {
			return 0
		}
		if (*c1.MsgTxs[0].Time).After(*c2.MsgTxs[0].Time) {
			return 1
		} else {
			return -1
		}
	})

	round := 0
	for msgs > d.Opts.MsgMaxAmount {
		if round > 100 {
			d.Mach.Log("Too many GC rounds")

			break
		}
		round++

		// delete a half per client
		for _, c := range clients {
			if ctx.Err() != nil {
				d.Mach.Log("Context expired")

				break
			}
			idx := len(c.MsgTxs) / 2
			c.MsgTxs = c.MsgTxs[idx:]
			c.msgTxsParsed = c.msgTxsParsed[idx:]
			c.logMsgs = c.logMsgs[idx:]

			// adjust the current client
			if d.C == c {
				err := d.rebuildLog(ctx, len(c.MsgTxs)-1)
				if err != nil {
					d.Mach.AddErr(err, nil)
				}
				c.CursorTx = int(math.Max(0, float64(c.CursorTx-idx)))
				// re-filter
				if d.isFiltered() {
					d.filterClientTxs()
				}
			}

			// delete small clients
			if len(c.MsgTxs) < msgMaxThreshold {
				d.Mach.Add1(ss.RemoveClient, am.A{"Client.id": c.id})
				msgs -= len(c.MsgTxs)
			} else {
				msgs -= idx
				c.mTimeSum = 0
				for _, m := range c.msgTxsParsed {
					c.mTimeSum += m.TimeSum
				}
			}
		}
	}

	d.RedrawFull(false)
}

func (d *Debugger) LogReaderVisibleState(e *am.Event) {
	if d.Mach.Not1(ss.TimelineStepsFocused) {
		d.treeLogGrid.UpdateItem(d.log, 0, 2, 1, 2, 0, 0, false)
		d.treeLogGrid.AddItem(d.logReader, 0, 4, 1, 2, 0, 0, false)
	} else {
		d.treeLogGrid.UpdateItem(d.log, 0, 3, 1, 2, 0, 0, false)
		d.treeLogGrid.AddItem(d.logReader, 0, 5, 1, 1, 0, 0, false)
	}
	d.updateFocusable()
}

func (d *Debugger) LogReaderVisibleEnd(e *am.Event) {
	if d.Mach.Not1(ss.TimelineStepsFocused) {
		d.treeLogGrid.UpdateItem(d.log, 0, 2, 1, 4, 0, 0, false)
	} else {
		d.treeLogGrid.UpdateItem(d.log, 0, 3, 1, 3, 0, 0, false)
	}
	d.treeLogGrid.RemoveItem(d.logReader)
	d.updateFocusable()
}
