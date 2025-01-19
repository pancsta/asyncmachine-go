// TODO ExceptionState: separate error screen with stack trace

package debugger

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/pancsta/cview"
	"golang.org/x/exp/maps"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func (d *Debugger) StartState(e *am.Event) {
	clientId, _ := e.Args["Client.id"].(string)
	cursorTx, _ := e.Args["Client.cursorTx"].(int)
	view, _ := e.Args["dbgView"].(string)

	d.App = cview.NewApplication()
	if d.Opts.Screen != nil {
		d.App.SetScreen(d.Opts.Screen)
	}
	d.P = message.NewPrinter(language.English)
	d.bindKeyboard()
	d.initUiComponents()
	d.initLayout()
	if d.Opts.EnableMouse {
		d.App.EnableMouse(true)
	}
	if d.Opts.ShowReader {
		d.Mach.Add1(ss.LogReaderEnabled, nil)
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
		ids := maps.Keys(d.Clients)
		if clientId == "" {
			clientId = ids[0]
		} else {
			// partial match available client IDs
			for _, id := range ids {
				if strings.Contains(id, clientId) {
					clientId = id
					break
				}
			}
		}
		d.prependHistory(&MachAddress{MachId: clientId})
		d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": clientId})
		// TODO dimeout
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
	d.Mach.Add1(ss.GcMsgs, nil)
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

	d.updateStatusBars()
}

// StateNameSelectedStateNameSelected handles cursor moving from a state name to
// another state name case.
func (d *Debugger) StateNameSelectedStateNameSelected(e *am.Event) {
	d.StateNameSelectedState(e)
}

func (d *Debugger) StateNameSelectedEnd(_ *am.Event) {
	if d.C != nil {
		d.C.SelectedState = ""
	}
	d.updateTree()
	d.updateStatusBars()
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
	d.updateToolbar()

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
	d.updateToolbar()
}

func (d *Debugger) PausedState(_ *am.Event) {
	// TODO stop scrolling the log when coming from TailMode (confirm)
	d.updateTxBars()
	d.draw()
}

func (d *Debugger) TailModeState(_ *am.Event) {
	d.SetCursor1(d.filterTxCursor(d.C, len(d.C.MsgTxs), false), false)
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
	return d.C.CursorTx1+amount <= len(d.C.MsgTxs)
}

func (d *Debugger) FwdState(e *am.Event) {
	d.Mach.Remove1(ss.Fwd, nil)

	amount, _ := e.Args["amount"].(int)
	amount = max(amount, 1)

	c := d.C
	d.SetCursor1(d.filterTxCursor(c, c.CursorTx1+amount, true), false)
	d.trimHistory()
	d.handleTStepsScrolled()
	if d.Mach.Is1(ss.Playing) && c.CursorTx1 == len(c.MsgTxs) {
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
	return d.C.CursorTx1-amount >= 0
}

func (d *Debugger) BackState(e *am.Event) {
	d.Mach.Remove1(ss.Back, nil)

	amount, _ := e.Args["amount"].(int)
	amount = max(amount, 1)

	c := d.C
	d.SetCursor1(d.filterTxCursor(d.C, c.CursorTx1-amount, false), false)
	d.trimHistory()
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
	return d.C.CursorStep > 0 || d.C.CursorTx1 > 0
}

func (d *Debugger) BackStepState(_ *am.Event) {
	d.Mach.Remove1(ss.BackStep, nil)

	// wrap if there's a prev tx
	if d.C.CursorStep <= 0 {
		d.SetCursor1(d.filterTxCursor(d.C, d.C.CursorTx1-1, false), false)
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

func (d *Debugger) TimelineStepsScrolledState(_ *am.Event) {
	d.expandStructPane()
}

func (d *Debugger) TimelineStepsScrolledEnd(_ *am.Event) {
	d.shinkStructPane()
}

func (d *Debugger) TimelineStepsFocusedState(_ *am.Event) {
	d.expandStructPane()
}

func (d *Debugger) TimelineStepsFocusedEnd(_ *am.Event) {
	d.shinkStructPane()
}

func (d *Debugger) Toolbar1FocusedState(e *am.Event) {
	d.toolbars[0].SetBackgroundColor(cview.Styles.MoreContrastBackgroundColor)
	d.updateToolbar()
}

func (d *Debugger) Toolbar1FocusedEnd(_ *am.Event) {
	d.toolbars[0].SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.updateToolbar()
}

func (d *Debugger) Toolbar2FocusedState(e *am.Event) {
	d.toolbars[1].SetBackgroundColor(cview.Styles.MoreContrastBackgroundColor)
	d.updateToolbar()
}

func (d *Debugger) Toolbar2FocusedEnd(_ *am.Event) {
	d.toolbars[1].SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.updateToolbar()
}

func (d *Debugger) AddressFocusedState(e *am.Event) {
	d.addressBar.SetBackgroundColor(cview.Styles.MoreContrastBackgroundColor)
	d.updateToolbar()
}

func (d *Debugger) AddressFocusedEnd(_ *am.Event) {
	d.addressBar.SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.updateToolbar()
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

	if _, exists := d.Clients[msg.ID]; exists {
		d.Mach.Log("client %s already exists", msg.ID)
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
		d.prependHistory(&MachAddress{MachId: msg.ID})

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
	d.updateAddressBar()
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
	connIds := e.Args["conn_ids"].([]string)
	mach := d.Mach

	updateTailMode := false
	updateFirstTx := false
	selectedUpdated := false
	for i, msg := range msgs {

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

		// verify it's from the same client
		if c.connID != connIds[i] {
			d.Mach.Log("Error: conn_id mismatch for %s, ignoring tx\n", machId)
			continue
		}

		// TODO scalable storage (support filtering)
		idx := len(c.MsgTxs)
		c.MsgTxs = append(c.MsgTxs, msg)
		// parse the msg
		d.parseMsg(c, idx)
		filterOK := d.filterTx(c, idx, mach.Is1(ss.FilterCanceledTx),
			mach.Is1(ss.FilterAutoTx), mach.Is1(ss.FilterEmptyTx),
			mach.Is1(ss.FilterHealthcheck))
		if filterOK {
			c.msgTxsFiltered = append(c.msgTxsFiltered, idx)
		}

		// update the UI
		// TODO debounce UI updates

		if c == d.C {
			selectedUpdated = true
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
		d.SetCursor1(d.filterTxCursor(d.C, len(d.C.MsgTxs), false), false)
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
	d.updateMatrix()

	if selectedUpdated {
		d.draw()
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
	d.removeHistory(c.id)

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
		d.buildClientList(-1)
	} else {
		d.buildClientList(d.clientList.GetCurrentItemIndex() - 1)
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
	// TODO support tx ID
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
			d.SetCursor1(d.filterTxCursor(d.C, len(d.C.MsgTxs), false), true)
		}
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

	d.updateBorderColor()
	d.updateAddressBar()
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
	// re-render for mem stats
	d.updateHelpDialog()
	d.updateToolbar()
	// TODO use Visibility instead of SendToFront
	d.LayoutRoot.SendToFront("main")
	d.LayoutRoot.SendToFront("help")
}

func (d *Debugger) HelpDialogEnd(e *am.Event) {
	diff := am.DiffStates(ss.GroupDialog, e.Transition().TargetStates())
	if len(diff) == len(ss.GroupDialog) {
		// all dialogs closed, show main
		d.LayoutRoot.SendToFront("main")
		d.updateFocusable()
		d.updateToolbar()
	}
}

func (d *Debugger) ExportDialogState(_ *am.Event) {
	d.updateToolbar()
	// TODO use Visibility instead of SendToFront
	d.LayoutRoot.SendToFront("main")
	d.LayoutRoot.SendToFront("export")
}

func (d *Debugger) ExportDialogEnd(e *am.Event) {
	diff := am.DiffStates(ss.GroupDialog, e.Transition().TargetStates())
	if len(diff) == len(ss.GroupDialog) {
		// all dialogs closed, show main
		d.LayoutRoot.SendToFront("main")
		d.updateFocusable()
		d.updateToolbar()
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
	cursor, ok1 := e.Args["Client.cursorTx"].(int)
	id, ok2 := e.Args["Client.txId"].(string)
	c := d.C

	return c != nil && (ok2 && c.txIndex(id) > -1 ||
		ok1 && len(c.MsgTxs) > cursor+1)
}

// ScrollToTxState scrolls to a specific transition (cursor position 1-based).
func (d *Debugger) ScrollToTxState(e *am.Event) {
	d.Mach.Remove1(ss.ScrollToTx, nil)

	cursor, _ := e.Args["Client.cursorTx"].(int)
	trim, _ := e.Args["trimHistory"].(bool)
	id, ok2 := e.Args["Client.txId"].(string)
	if ok2 {
		// TODO lowers the index each time
		cursor = d.C.txIndex(id) + 1
	}
	d.SetCursor1(d.filterTxCursor(d.C, cursor, true), false)
	if trim {
		d.trimHistory()
	}
	d.updateClientList(false)
	d.RedrawFull(false)
}

func (d *Debugger) ScrollToStepEnter(e *am.Event) bool {
	cursor, _ := e.Args["Client.cursorStep"].(int)
	c := d.C
	return c != nil && cursor > 0 && d.nextTx() != nil
}

// ScrollToTxState scrolls to a specific transition (cursor position 1-based).
func (d *Debugger) ScrollToStepState(e *am.Event) {
	d.Mach.Remove1(ss.ScrollToStep, nil)

	cursor := e.Args["Client.cursorStep"].(int)
	nextTx := d.nextTx()

	if cursor > len(nextTx.Steps) {
		cursor = len(nextTx.Steps)
	}
	d.C.CursorStep = cursor

	d.handleTStepsScrolled()
	d.RedrawFull(false)
}

func (d *Debugger) ToggleToolEnter(e *am.Event) bool {
	_, ok := e.Args["ToolName"].(ToolName)
	return ok
}

func (d *Debugger) ToggleToolState(e *am.Event) {
	// TODO split the state into an async one
	// TODO refac to FilterToggledState
	tool := e.Args["ToolName"].(ToolName)

	// tool is a filter and needs re-filter txs
	filterTxs := false

	switch tool {
	// TODO move logic after toggle to handlers

	case toolFilterCanceledTx:
		d.Mach.Toggle1(ss.FilterCanceledTx, nil)
		filterTxs = true

	case toolFilterAutoTx:
		d.Mach.Toggle1(ss.FilterAutoTx, nil)
		filterTxs = true

	case toolFilterEmptyTx:
		d.Mach.Toggle1(ss.FilterEmptyTx, nil)
		filterTxs = true

	case toolFilterHealthcheck:
		d.Mach.Toggle1(ss.FilterHealthcheck, nil)
		filterTxs = true

	case ToolFilterSummaries:
		d.Mach.Toggle1(ss.FilterSummaries, nil)

	case toolLog0:
		d.Opts.Filters.LogLevel = am.LogNothing
	case toolLog1:
		d.Opts.Filters.LogLevel = am.LogChanges
	case toolLog2:
		d.Opts.Filters.LogLevel = am.LogOps
	case toolLog3:
		d.Opts.Filters.LogLevel = am.LogDecisions
	case toolLog4:
		d.Opts.Filters.LogLevel = am.LogEverything

	case toolReader:
		d.Mach.Toggle1(ss.LogReaderEnabled, nil)

	case toolRain:
		d.toolRain()

	case toolHelp:
		d.Mach.Toggle1(ss.HelpDialog, nil)

	case toolPlay:
		if d.Mach.Is1(ss.Paused) {
			d.Mach.Add1(ss.Playing, nil)
		} else {
			d.Mach.Add1(ss.Paused, nil)
		}

	case toolTail:
		d.Mach.Toggle1(ss.TailMode, nil)

	case toolPrev:
		d.Mach.Add1(ss.UserBack, nil)

	case toolNext:
		d.Mach.Add1(ss.UserFwd, nil)

	case toolJumpPrev:
		// TODO state
		go d.jumpBack(nil)

	case toolJumpNext:
		// TODO state
		go d.jumpFwd(nil)

	case toolFirst:
		d.toolFirstTx()

	case toolLast:
		d.toolLastTx()

	case toolExpand:
		d.toolExpand()

	case toolMatrix:
		d.toolMatrix()

	case toolExport:
		d.Mach.Toggle1(ss.ExportDialog, nil)
	}

	stateCtx := d.Mach.NewStateCtx(ss.ToggleTool)

	// process the toolbarItem change
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
		d.Mach.Add1(ss.ScrollToTx, am.A{
			"Client.cursorTx": cursorTx,
			"trimHistory":     true,
		})
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

	for i := c.CursorTx1 + step; i > 0 && i < len(c.MsgTxs)+1; i = i + step {

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
		d.Mach.Add1(ss.ScrollToTx, am.A{
			"Client.cursorTx": i,
			"trimHistory":     true,
		})
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

func (d *Debugger) GcMsgsEnter(e *am.Event) bool {
	runtime.GC()
	return AllocMem() > uint64(d.Opts.MaxMemMb)*1024*1024
}

func (d *Debugger) GcMsgsState(e *am.Event) {
	// TODO GC log reader entries
	// TODO GC tx steps before GCing transitions
	ctx := d.Mach.NewStateCtx(ss.GcMsgs)

	// unblock
	go func() {
		defer d.Mach.Remove1(ss.GcMsgs, nil)

		// get oldest clients
		clients := maps.Values(d.Clients)
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

		mem1 := AllocMem()
		d.Mach.Log(d.P.Sprintf("GC mem: %d bytes\n", mem1))

		// check TTL of client log msgs >lvl 2
		// TODO remember the tip of cleaning (date) and binary find it, then
		//  continue
		for _, c := range clients {
			for i, logMsg := range c.logMsgs {
				htime := c.MsgTxs[i].Time
				if htime.Add(d.Opts.Log2Ttl).After(time.Now()) {
					continue
				}

				// TODO optimize
				var repl []*am.LogEntry
				for _, log := range logMsg {
					if log == nil {
						continue
					}
					if log.Level == am.LogChanges {
						repl = append(repl, log)
					}
				}

				// override
				c.logMsgs[i] = repl
			}
		}

		runtime.GC()
		mem2 := AllocMem()
		if mem1 > mem2 {
			d.Mach.Log(d.P.Sprintf("GC logs shaved %d bytes\n", mem1-mem2))
		}

		round := 0
		for AllocMem() > uint64(d.Opts.MaxMemMb)*1024*1024 {
			if ctx.Err() != nil {
				d.Mach.Log("GC: context expired")
				break
			}
			if round > 100 {
				d.Mach.AddErr(errors.New("Too many GC rounds"), nil)
				break
			}
			d.Mach.Log("GC tx round %d", round)
			round++

			// delete a half per client
			for _, c := range clients {
				if ctx.Err() != nil {
					d.Mach.Log("GC: context expired")
					break
				}
				idx := len(c.MsgTxs) / 2
				c.MsgTxs = c.MsgTxs[idx:]
				c.msgTxsParsed = c.msgTxsParsed[idx:]
				c.logMsgs = c.logMsgs[idx:]

				// empty cache
				c.txCache = nil
				// TODO GC c.logReader
				// TODO refresh c.errors (extract from parseMsg)

				// adjust the current client
				if d.C == c {
					err := d.rebuildLog(ctx, len(c.MsgTxs)-1)
					if err != nil {
						d.Mach.AddErr(err, nil)
					}
					c.CursorTx1 = int(math.Max(0, float64(c.CursorTx1-idx)))
					// re-filter
					if d.isFiltered() {
						d.filterClientTxs()
					}
				}

				// delete small clients
				if len(c.MsgTxs) < msgMaxThreshold {
					d.Mach.Add1(ss.RemoveClient, am.A{"Client.id": c.id})
				} else {
					c.mTimeSum = 0
					for _, m := range c.msgTxsParsed {
						c.mTimeSum += m.TimeSum
					}
				}
			}

			runtime.GC()
		}
		mem3 := AllocMem()
		if mem1 > mem3 {
			d.Mach.Log(d.P.Sprintf("GC in total shaved %d bytes", mem1-mem3))
		}

		d.RedrawFull(false)
	}()
}

func (d *Debugger) LogReaderVisibleState(e *am.Event) {
	if d.Mach.Not1(ss.TimelineStepsScrolled) {
		d.treeLogGrid.UpdateItem(d.log, 0, 2, 1, 2, 0, 0, false)
		d.treeLogGrid.AddItem(d.logReader, 0, 4, 1, 2, 0, 0, false)
	} else {
		d.treeLogGrid.UpdateItem(d.log, 0, 3, 1, 2, 0, 0, false)
		d.treeLogGrid.AddItem(d.logReader, 0, 5, 1, 1, 0, 0, false)
	}
	d.updateFocusable()
	d.updateBorderColor()
}

func (d *Debugger) LogReaderVisibleEnd(e *am.Event) {
	if d.Mach.Not1(ss.TimelineStepsScrolled) {
		d.treeLogGrid.UpdateItem(d.log, 0, 2, 1, 4, 0, 0, false)
	} else {
		d.treeLogGrid.UpdateItem(d.log, 0, 3, 1, 3, 0, 0, false)
	}
	d.treeLogGrid.RemoveItem(d.logReader)
	d.updateFocusable()
}
