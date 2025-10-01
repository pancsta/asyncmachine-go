// TODO ExceptionState: separate error screen with stack trace

package debugger

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/coder/websocket"
	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"
	"golang.org/x/exp/maps"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/visualizer"
)

// TODO Enter

func (d *Debugger) StartState(e *am.Event) {
	clientId, _ := e.Args["Client.id"].(string)
	cursorTx1, _ := e.Args["cursorTx1"].(int)
	group, _ := e.Args["group"].(string)
	view, _ := e.Args["dbgView"].(string)

	// cview TUI app
	d.App = cview.NewApplication()
	if d.Opts.Screen != nil {
		d.App.SetScreen(d.Opts.Screen)
	}

	// forceful race solving
	d.App.SetBeforeDrawFunc(func(_ tcell.Screen) bool {
		// dont draw while transitioning
		ok := d.Mach.Transition() == nil
		if !ok {
			// reschedule this repaint
			// d.Mach.Log("postpone draw")
			d.repaintPending.Store(true)
			return true
		}

		// mark as in progress
		d.drawing.Store(true)
		return false
	})
	d.App.SetAfterDrawFunc(func(_ tcell.Screen) {
		d.drawing.Store(false)
	})
	d.App.SetAfterResizeFunc(func(width int, height int) {
		go func() {
			time.Sleep(time.Millisecond * 300)
			d.Mach.Add1(ss.Resized, nil)
		}()
	})

	// init the rest
	d.P = message.NewPrinter(language.English)
	d.hBindKeyboard()
	d.hInitUiComponents()
	d.hInitLayout()
	d.hUpdateFocusable()
	if d.Opts.EnableMouse {
		d.App.EnableMouse(true)
	}
	if d.Opts.ShowReader {
		d.Mach.Add1(ss.LogReaderEnabled, nil)
	}

	// default filters TODO sync filter from CLI
	filters := S{ss.FilterChecks}
	if d.Opts.Filters.SkipOutGroup {
		filters = append(filters, ss.FilterOutGroup)
	}
	d.Mach.Add(filters, nil)

	stateCtx := d.Mach.NewStateCtx(ss.Start)

	// draw in a goroutine
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}
		d.Mach.PanicToErr(nil)

		d.App.SetRoot(d.LayoutRoot, true)
		err := d.App.Run()
		if err != nil {
			d.Mach.AddErr(err, nil)
		}

		d.Dispose()
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

		// go directly to Ready when data empty
		if len(d.Clients) <= 0 {
			d.Mach.Add1(ss.Ready, nil)
			return
		}

		// init imported data
		d.buildClientList(-1)
		ids := maps.Keys(d.Clients)
		if clientId != "" {
			// partial match available client IDs
			for _, id := range ids {
				if strings.Contains(id, clientId) {
					clientId = id
					break
				}
			}
		}
		// default selected ID
		if !slices.Contains(ids, clientId) {
			clientId = ids[0]
		}
		d.hPrependHistory(&MachAddress{MachId: clientId})
		// TODO timeout
		d.Mach.Add1(ss.SelectingClient, am.A{
			"Client.id": clientId,
			"group":     group,
		})
		<-d.Mach.When1(ss.ClientSelected, nil)

		if stateCtx.Err() != nil {
			return // expired
		}

		if cursorTx1 != 0 {
			d.Mach.Add1(ss.ScrollToTx, am.A{"cursorTx1": cursorTx1})
		}

		d.Mach.Add1(ss.Ready, nil)
	}()
}

func (d *Debugger) StartEnd(_ *am.Event) {
	if d.App.GetScreen() != nil {
		d.App.Stop()
	}
}

func (d *Debugger) ReadyState(e *am.Event) {
	d.heartbeatT = time.NewTicker(heartbeatInterval)

	// late options
	// TODO migrate args from Start() method
	if d.Opts.ViewNarrow {
		d.Mach.EvAdd1(e, ss.NarrowLayout, nil)
	}
	d.hSyncOptsTimelines()
	if d.Opts.OutputDiagrams > 0 {
		d.Mach.EvAdd1(e, ss.DiagramsScheduled, nil)
	}
	if d.Opts.ViewRain {
		d.Mach.EvAdd1(e, ss.MatrixRain, nil)
	}
	if d.Opts.TailMode {
		d.Mach.EvAdd1(e, ss.TailMode, nil)
	}

	// TODO extract march url parsing and merge with addr bar
	if u := d.Opts.MachUrl; u != "" {
		up, err := url.Parse(d.Opts.MachUrl)
		if err != nil {
			d.Mach.EvAddErr(e, err, nil)
		} else if up.Host != "" {
			addr := &MachAddress{
				MachId: up.Host,
			}
			p := strings.Split(up.Path, "/")
			if len(p) > 1 {
				addr.TxId = p[1]
			}
			if len(p) > 2 {
				if s, err := strconv.Atoi(p[2]); err == nil {
					addr.Step = s
				}
			}
			go d.hGoToMachAddress(addr, false)
		}
	}

	// unblock
	go func() {
		for {
			select {
			case <-d.heartbeatT.C:
				d.Mach.Add1(ss.Heartbeat, nil)

			case <-d.Mach.Ctx().Done():
				d.heartbeatT.Stop()
			}
		}
	}()
}

func (d *Debugger) ReadyEnd(_ *am.Event) {
	d.heartbeatT.Stop()
}

func (d *Debugger) HeartbeatState(e *am.Event) {
	d.Mach.Remove1(ss.Heartbeat, nil)
	go amhelp.AskEvAdd1(e, d.Mach, ss.GcMsgs, nil)
}

func (d *Debugger) StateNameSelectedEnter(e *am.Event) bool {
	_, ok := e.Args["state"].(string)
	return ok
}

func (d *Debugger) StateNameSelectedState(e *am.Event) {
	d.C.SelectedState = e.Args["state"].(string)
	d.lastSelectedState = d.C.SelectedState

	switch d.Mach.Switch(ss.GroupViews) {

	case ss.TreeLogView:
		d.hUpdateSchemaTree()

	case ss.TreeMatrixView:
		d.hUpdateSchemaTree()
		d.hUpdateMatrix()

	case ss.MatrixView:
		d.hUpdateMatrix()
	}

	d.hUpdateStatusBar()
}

func (d *Debugger) StateNameSelectedEnd(_ *am.Event) {
	if d.C != nil {
		d.C.SelectedState = ""
	}
	d.hUpdateSchemaTree()
	d.hUpdateStatusBar()
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
	d.hUpdateToolbar()

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
	d.hUpdateToolbar()
}

func (d *Debugger) PausedState(_ *am.Event) {
	// TODO stop scrolling the log when coming from TailMode (confirm)
	d.hUpdateTxBars()
	d.draw()
}

func (d *Debugger) TailModeState(e *am.Event) {
	d.hSetCursor1(e, am.A{
		"cursor1":    len(d.C.MsgTxs),
		"filterBack": true,
	})
	d.hUpdateMatrix()
	d.hUpdateClientList()
	d.hUpdateToolbar()
	// needed bc tail mode if carried over via SelectingClient
	d.hRedrawFull(true)
}

func (d *Debugger) TailModeEnd(_ *am.Event) {
	d.hUpdateMatrix()
	d.hUpdateToolbar()
	d.hRedrawFull(true)
}

func (d *Debugger) RedrawState(e *am.Event) {
	d.Mach.Remove1(ss.Redraw, nil)
	immediate, _ := e.Args["immediate"].(bool)
	d.hRedrawFull(immediate)
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
	c := d.C

	amount, _ := e.Args["amount"].(int)
	amount = max(amount, 1)

	d.hSetCursor1(e, am.A{
		"cursor1": c.CursorTx1 + amount,
	})
	if d.Mach.Is1(ss.Playing) && c.CursorTx1 == len(c.MsgTxs) {
		d.Mach.Remove1(ss.Playing, nil)
	}

	// sidebar for errs
	d.hUpdateClientList()
	d.hRedrawFull(false)
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

	d.hSetCursor1(e, am.A{
		"cursor1":    d.C.CursorTx1 - amount,
		"filterBack": true,
	})

	// sidebar for errs
	d.hUpdateClientList()
	d.hRedrawFull(false)
}

// ///// STEP BACK / FWD

func (d *Debugger) UserFwdStepState(_ *am.Event) {
	d.Mach.Remove1(ss.UserFwdStep, nil)
}

func (d *Debugger) FwdStepEnter(_ *am.Event) bool {
	nextTx := d.hNextTx()
	if nextTx == nil {
		return false
	}
	return d.C.CursorStep1 < len(nextTx.Steps)+1
}

func (d *Debugger) FwdStepState(_ *am.Event) {
	d.Mach.Remove1(ss.FwdStep, nil)

	// next tx
	nextTx := d.hNextTx()
	// scroll to the next tx
	if d.C.CursorStep1 == len(nextTx.Steps) {
		d.Mach.Add1(ss.Fwd, nil)
		return
	}
	d.C.CursorStep1++

	d.hHandleTStepsScrolled()
	d.hRedrawFull(false)
}

func (d *Debugger) UserBackStepState(_ *am.Event) {
	d.Mach.Remove1(ss.UserBackStep, nil)
}

func (d *Debugger) BackStepEnter(_ *am.Event) bool {
	return d.C.CursorStep1 > 0 || d.C.CursorTx1 > 0
}

func (d *Debugger) BackStepState(e *am.Event) {
	d.Mach.Remove1(ss.BackStep, nil)

	// wrap if there's a prev tx
	if d.C.CursorStep1 <= 0 {
		d.hSetCursor1(e, am.A{
			"cursor1": d.C.CursorTx1 - 1,
		})

		d.Mach.Add1(ss.UpdateLogScheduled, nil)
		if nextTx := d.hNextTx(); nextTx != nil {
			d.C.CursorStep1 = len(nextTx.Steps)
		}

	} else {
		d.C.CursorStep1--
	}

	d.updateClientList()
	d.hHandleTStepsScrolled()
	d.hRedrawFull(false)
}

// TODO move
func (d *Debugger) hHandleTStepsScrolled() {
	// TODO merge with a CursorStep setter
	tStepsScrolled := d.C.CursorStep1 != 0

	if tStepsScrolled {
		d.Mach.Add1(ss.TimelineStepsScrolled, nil)
	} else {
		d.Mach.Remove1(ss.TimelineStepsScrolled, nil)
	}
}

func (d *Debugger) TimelineStepsScrolledState(_ *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.hRedrawFull(false)
}

func (d *Debugger) TimelineStepsScrolledEnd(_ *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.hRedrawFull(false)
}

func (d *Debugger) TimelineStepsFocusedState(_ *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.hRedrawFull(false)
}

func (d *Debugger) TimelineStepsFocusedEnd(_ *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.hRedrawFull(false)
}

func (d *Debugger) Toolbar1FocusedState(e *am.Event) {
	d.toolbars[0].SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) Toolbar1FocusedEnd(_ *am.Event) {
	d.toolbars[0].SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

func (d *Debugger) Toolbar2FocusedState(e *am.Event) {
	d.toolbars[1].SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) Toolbar2FocusedEnd(_ *am.Event) {
	d.toolbars[1].SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

func (d *Debugger) Toolbar3FocusedState(e *am.Event) {
	d.toolbars[2].SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) Toolbar3FocusedEnd(_ *am.Event) {
	d.toolbars[2].SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

func (d *Debugger) AddressFocusedState(e *am.Event) {
	d.addressBar.SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) AddressFocusedEnd(_ *am.Event) {
	d.addressBar.SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

func (d *Debugger) TreeGroupsFocusedState(e *am.Event) {
	d.treeGroups.SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) TreeGroupsFocusedEnd(_ *am.Event) {
	d.treeGroups.SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

// ///// CONNECTION

func (d *Debugger) ConnectEventEnter(e *am.Event) bool {
	msg, ok1 := e.Args["msg_struct"].(*telemetry.DbgMsgStruct)
	_, ok2 := e.Args["conn_id"].(string)
	if !ok1 || !ok2 || msg.ID == "" {
		d.Mach.Log("Error: msg_struct malformed\n")
		return false
	}

	return true
}

func (d *Debugger) ConnectEventState(e *am.Event) {
	// initial structure data
	msg := e.Args["msg_struct"].(*telemetry.DbgMsgStruct)
	connId := e.Args["conn_id"].(string)
	var c *Client

	// cleanup removes all previous clients if all are disconnected
	cleanup := false
	if d.Opts.CleanOnConnect {
		// remove old clients
		cleanup = d.hCleanOnConnect()
	}

	// update existing client
	if existing, ok := d.Clients[msg.ID]; ok {
		if existing.connId != "" && existing.connId == connId {
			d.Mach.Log("schema changed for %s", msg.ID)
			// TODO use MsgStructPatch
			existing.MsgStruct = msg
			c = existing
			c.parseSchema()

		} else {
			// TODO rename and keep the old client when connId differs
			d.Mach.Log("client %s already exists", msg.ID)
		}
	}

	// create a new client
	if c == nil {
		c = &Client{
			id:         msg.ID,
			connId:     connId,
			schemaHash: amhelp.SchemaHash(msg.States),
			Exportable: Exportable{
				MsgStruct: msg,
			},
			logReader: make(map[string][]*logReaderEntry),
		}
		c.parseSchema()
		c.connected.Store(true)
		d.Clients[msg.ID] = c
	}

	// re-select the last group
	if g := d.lastSelectedGroup; g != "" {
		if _, ok := c.MsgStruct.Groups[g]; ok {
			c.SelectedGroup = g
		}
	}

	if !cleanup {
		d.buildClientList(-1)
	}

	// rebuild the UI in case of a cleanup or connect under the same ID
	if cleanup || (d.C != nil && d.C.id == msg.ID) {
		// select the new (and only) client
		d.C = c
		d.log.Clear()
		d.hUpdateTimelines()
		d.hUpdateTxBars()
		d.hUpdateBorderColor()
		d.buildClientList(0)
		// initial build of the schema tree
		d.hBuildSchemaTree()
		d.hUpdateTreeGroups()
		d.hUpdateViews(false)
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
		d.hConnectedClients() == 1) {

		d.Mach.Add1(ss.SelectingClient, am.A{
			"Client.id": msg.ID,
			// mark the origin
			"from_connected": true,
		})
		d.hPrependHistory(&MachAddress{MachId: msg.ID})

		// re-select the state
		if d.lastSelectedState != "" {
			d.Mach.Add1(ss.StateNameSelected, am.A{"state": d.lastSelectedState})
			// TODO Keep in StateNameSelected behind a flag
			d.hSelectTreeState(d.lastSelectedState)
		}
	}

	// first client, tail mode
	if len(d.Clients) == 1 {
		d.Mach.Add1(ss.TailMode, nil)
	}

	// graph
	if d.graph != nil {
		_ = d.graph.AddClient(msg)
		// TODO errors, check for dups, enable once stable
		// if err != nil {
		// d.Mach.AddErr(err, nil)
		// }
	}
	d.Mach.Add1(ss.InitClient, am.A{"id": msg.ID})

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
	connID := e.Args["conn_id"].(string)
	for _, c := range d.Clients {
		if c.connId != "" && c.connId == connID {
			// mark as disconnected
			c.connected.Store(false)
			d.Mach.Log("client %s disconnected", c.id)
			break
		}
	}

	d.hUpdateBorderColor()
	d.hUpdateAddressBar()
	d.hUpdateClientList()
	d.draw()
}

// ///// CLIENTS

func (d *Debugger) ClientMsgEnter(e *am.Event) bool {
	_, ok1 := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)
	_, ok2 := e.Args["conn_ids"].([]string)
	return ok1 && ok2
}

func (d *Debugger) ClientMsgState(e *am.Event) {
	d.Mach.Remove1(ss.ClientMsg, nil)

	// TODO make it async via a dedicated goroutine, pushing results to
	//  async multi state ClientMsgDone (if possible)

	msgs := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)
	connIds := e.Args["conn_ids"].([]string)
	mach := d.Mach

	// GC in progress - save msgs and parse on next ClientMsgState
	if mach.Is1(ss.GcMsgs) {
		d.msgsDelayed = append(d.msgsDelayed, msgs...)
		d.msgsDelayedConns = append(d.msgsDelayedConns, connIds...)

		return
	}

	// parse pending msgs, if any
	if len(d.msgsDelayed) > 0 {
		msgs = slices.Concat(d.msgsDelayed, msgs)
		connIds = slices.Concat(d.msgsDelayedConns, connIds)
		d.msgsDelayed = nil
		d.msgsDelayedConns = nil
	}

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
		if c.connId != connIds[i] {
			d.Mach.Log("Error: conn_id mismatch for %s, ignoring tx\n", machId)
			continue
		}

		// append and parse the msg
		// TODO scalable storage (support filtering)
		idx := len(c.MsgTxs)
		c.MsgTxs = append(c.MsgTxs, msg)
		d.hParseMsg(c, idx)
		filterOK := d.hFilterTx(c, idx, d.filtersFromStates())
		if filterOK {
			c.MsgTxsFiltered = append(c.MsgTxsFiltered, idx)
		}

		if c == d.C {
			selectedUpdated = true
			err := d.hAppendLogEntry(idx)
			if err != nil {
				d.Mach.Log("Error: log append %s\n", err)
				// d.Mach.AddErr(err, nil)
				return
			}
			if d.Mach.Is1(ss.TailMode) {
				updateTailMode = true
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
		d.hSetCursor1(e, am.A{
			"cursor1":    len(d.C.MsgTxs),
			"filterBack": true,
		})
		// sidebar for errs
		d.hUpdateViews(false)
	}

	// update Tx info on the first Tx
	if updateTailMode || updateFirstTx {
		d.hUpdateTxBars()
	}

	// timelines always change
	d.updateClientList()
	d.hUpdateTimelines()
	d.hUpdateMatrix()
	d.hUpdateAddressBar()

	if selectedUpdated {
		d.draw()
	}
}

func (d *Debugger) RemoveClientEnter(e *am.Event) bool {
	cid, ok := e.Args["Client.id"].(string)
	_, ok2 := d.Clients[cid]

	return ok && cid != "" && ok2
}

func (d *Debugger) RemoveClientState(e *am.Event) {
	d.Mach.Remove1(ss.RemoveClient, nil)
	cid := e.Args["Client.id"].(string)
	c := d.Clients[cid]

	// clean up
	delete(d.Clients, cid)
	d.hRemoveHistory(c.id)

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

func (d *Debugger) SetGroupEnter(e *am.Event) bool {
	group, ok := e.Args["group"].(string)
	if !ok {
		return false
	}

	// extract
	group, _, _ = strings.Cut(group, ":")

	_, ok = d.C.msgSchemaParsed.Groups[group]
	return group != "" && group != d.C.SelectedGroup && ok
}

func (d *Debugger) SetGroupState(e *am.Event) {
	group := e.Args["group"].(string)
	c := d.C

	if group == "all" {
		c.SelectedGroup = ""
	} else {
		c.SelectedGroup = strings.Split(group, ":")[0]
	}
	d.lastSelectedGroup = group
	d.hBuildSchemaTree()
	d.hUpdateSchemaTree()
	go amhelp.AskEvAdd1(e, d.Mach, ss.DiagramsScheduled, nil)
	d.Mach.EvAdd(e, am.S{ss.ToolToggled, ss.BuildingLog}, am.A{
		// TODO typed args
		"filterTxs":     true,
		"logRebuildEnd": len(c.MsgTxs),
	})
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
	group, _ := e.Args["group"].(string)
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
	// remain in TailMode after the selection
	wasTailMode := slices.Contains(e.Transition().StatesBefore(), ss.TailMode)
	d.C.SelectedGroup = group

	// TODO extract SelectingClientFiltered
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// start with prepping the data
		d.hFilterClientTxs()

		// scroll to the same place as the prev client
		// TODO continue in SelectingClientFilteredState
		match := false
		if !wasTailMode {
			match = d.hScrollToTime(e, d.lastScrolledTxTime, true)
		}

		// or scroll to the last one
		if !match {
			d.Mach.Eval("SelectingClientState", func() {
				d.hSetCursor1(e, am.A{
					"cursor1":    len(d.C.MsgTxs),
					"filterBack": true,
				})
			}, ctx)
			if ctx.Err() != nil {
				return // expired
			}

		} else {
			// [hSetCursor1] triggers DiagramsScheduled, so do we
			go amhelp.AskEvAdd1(e, d.Mach, ss.DiagramsScheduled, nil)
		}

		// scroll client list item into view
		selOffset := d.hGetSidebarCurrClientIdx()
		offset, _ := d.clientList.GetOffset()
		_, _, _, lines := d.clientList.GetInnerRect()
		if selOffset-offset > lines {
			d.clientList.SetCurrentItem(selOffset)
			d.updateClientList()
		}

		// rebuild the whole log
		target := am.S{ss.ClientSelected, ss.BuildingLog}
		if wasTailMode {
			target = append(target, ss.TailMode)
		}

		d.Mach.Add(target, am.A{
			"ctx":            ctx,
			"from_connected": fromConnected,
			"from_playing":   fromPlaying,
			"logRebuildEnd":  logRebuildEnd,
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
		err := d.hAppendLogEntry(i)
		if err != nil {
			d.Mach.Log("Error: log rebuild %s\n", err)
		}
	}

	// initial build of the schema tree
	d.hBuildSchemaTree()
	d.hUpdateTreeGroups()
	d.hUpdateTimelines()
	d.hUpdateTxBars()
	d.hUpdateClientList()
	if d.Mach.Is1(ss.TreeLogView) || d.Mach.Is1(ss.TreeMatrixView) {
		d.hUpdateSchemaTree()
	}

	// update views
	if d.Mach.Is1(ss.TreeLogView) {
		d.Mach.Add1(ss.UpdateLogScheduled, nil)
	}
	if d.Mach.Is1(ss.MatrixView) || d.Mach.Is1(ss.TreeMatrixView) {
		d.hUpdateMatrix()
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
		d.hSelectTreeState(d.lastSelectedState)
	}

	d.hUpdateBorderColor()
	d.hUpdateAddressBar()
	d.draw()
}

func (d *Debugger) ClientSelectedEnd(e *am.Event) {
	idx := d.Mach.Index1(ss.SelectingClient)
	// clean up, except when switching to SelectingClient
	if !e.Mutation().IsCalled(idx) {
		d.C = nil
	}

	d.log.Clear()
	d.treeRoot.ClearChildren()
	d.hRedrawFull(true)
}

func (d *Debugger) HelpDialogState(e *am.Event) {
	// re-render for mem stats
	d.hUpdateHelpDialog()
	d.hUpdateToolbar()
	// TODO use Visibility instead of SendToFront
	d.LayoutRoot.SendToFront("main")
	d.LayoutRoot.SendToFront("help")
}

func (d *Debugger) HelpDialogEnd(e *am.Event) {
	tx := e.Transition()
	diff := am.DiffStates(ss.GroupDialog, tx.TargetStates())
	if len(diff) == len(ss.GroupDialog) {
		// all dialogs closed, show main
		d.LayoutRoot.SendToFront("main")
		d.Mach.Add1(ss.UpdateFocus, nil)
		d.hUpdateToolbar()

		// TODO prev focus, read self-log?
		d.focusDefault()
	}
}

func (d *Debugger) ExportDialogState(_ *am.Event) {
	d.hUpdateToolbar()
	// TODO use Visibility instead of SendToFront
	d.LayoutRoot.SendToFront("main")
	d.LayoutRoot.SendToFront("export")
}

func (d *Debugger) ExportDialogEnd(e *am.Event) {
	diff := am.DiffStates(ss.GroupDialog, e.Transition().TargetStates())
	if len(diff) == len(ss.GroupDialog) {
		// all dialogs closed, show main
		d.LayoutRoot.SendToFront("main")
		d.Mach.Add1(ss.UpdateFocus, nil)
		d.hUpdateToolbar()

		// TODO prev focus
		d.focusDefault()
	}
}

func (d *Debugger) MatrixViewState(_ *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

func (d *Debugger) MatrixViewEnd(_ *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

func (d *Debugger) TreeMatrixViewState(_ *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

func (d *Debugger) TreeMatrixViewEnd(_ *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

func (d *Debugger) MatrixRainEnter(e *am.Event) bool {
	states := e.Transition().TimeIndexAfter()

	return states.Any1(ss.TreeMatrixView, ss.MatrixView) &&
		states.Is1(ss.ClientSelected)
}

func (d *Debugger) MatrixRainState(_ *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

func (d *Debugger) ScrollToTxEnter(e *am.Event) bool {
	cursor, ok1 := e.Args["cursorTx1"].(int)
	id, ok2 := e.Args["Client.txId"].(string)
	c := d.C

	return c != nil && (ok2 && c.txIndex(id) > -1 ||
		ok1 && len(c.MsgTxs) > cursor) && cursor >= 0
}

// ScrollToTxState scrolls to a specific transition (cursor position 1-based).
func (d *Debugger) ScrollToTxState(e *am.Event) {
	defer d.Mach.EvRemove1(e, ss.ScrollToTx, nil)

	cursor1, _ := e.Args["cursorTx1"].(int)
	cursorStep1, _ := e.Args["cursorStep1"].(int)
	trim, _ := e.Args["trimHistory"].(bool)
	id, ok2 := e.Args["Client.txId"].(string)
	if ok2 {
		cursor1 = d.C.txIndex(id) + 1
	}

	d.hSetCursor1(e, am.A{
		"cursor1":     cursor1,
		"cursorStep1": cursorStep1,
		"trimHistory": trim,
	})
	d.updateClientList()
	d.hRedrawFull(false)
}

func (d *Debugger) NarrowLayoutExit(e *am.Event) bool {
	// always allow to exit
	if e.Transition().TimeIndexAfter().Not1(ss.Start) {
		return true
	}

	return !d.Opts.ViewNarrow
}

func (d *Debugger) NarrowLayoutState(e *am.Event) {
	d.hUpdateLayout()
	d.buildClientList(-1)
	d.hRedrawFull(false)
}

func (d *Debugger) NarrowLayoutEnd(e *am.Event) {
	d.hUpdateLayout()
	d.hRedrawFull(false)
}

func (d *Debugger) ScrollToStepEnter(e *am.Event) bool {
	cursor, _ := e.Args["cursorStep1"].(int)
	c := d.C
	return c != nil && cursor > 0 && d.hNextTx() != nil
}

// ScrollToStepState scrolls to a specific transition (cursor position 1-based).
func (d *Debugger) ScrollToStepState(e *am.Event) {
	// TODO multi?
	d.Mach.EvRemove1(e, ss.ScrollToStep, nil)

	cStep1 := e.Args["cursorStep1"].(int)
	nextTx := d.hNextTx()

	if cStep1 > len(nextTx.Steps) {
		cStep1 = len(nextTx.Steps)
	}
	d.C.CursorStep1 = cStep1

	d.hHandleTStepsScrolled()
	d.hRedrawFull(false)
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
		d.Mach.EvToggle1(e, ss.FilterCanceledTx, nil)
		filterTxs = true

	case toolFilterQueuedTx:
		d.Mach.EvToggle1(e, ss.FilterQueuedTx, nil)
		filterTxs = true

	case toolFilterAutoTx:
		d.Mach.EvToggle1(e, ss.FilterAutoTx, nil)
		switch d.Mach.Switch(am.S{ss.FilterAutoTx, ss.FilterAutoCanceledTx}) {
		case ss.FilterAutoTx:
			d.Mach.EvAdd1(e, ss.FilterAutoCanceledTx, nil)
		case ss.FilterAutoCanceledTx:
			d.Mach.EvRemove1(e, ss.FilterAutoTx, nil)
		default:
			d.Mach.EvAdd1(e, ss.FilterAutoTx, nil)
		}
		filterTxs = true

	case toolFilterEmptyTx:
		d.Mach.EvToggle1(e, ss.FilterEmptyTx, nil)
		filterTxs = true

	case toolFilterHealth:
		d.Mach.EvToggle1(e, ss.FilterHealth, nil)
		filterTxs = true

	case toolFilterOutGroup:
		d.Mach.EvToggle1(e, ss.FilterOutGroup, nil)
		filterTxs = true

	case toolFilterChecks:
		d.Mach.EvToggle1(e, ss.FilterChecks, nil)
		filterTxs = true

	case ToolLogTimestamps:
		d.Mach.EvToggle1(e, ss.LogTimestamps, nil)
		filterTxs = true

	case ToolFilterTraces:
		d.Mach.EvToggle1(e, ss.FilterTraces, nil)

	case toolLog:
		d.Opts.Filters.LogLevel = (d.Opts.Filters.LogLevel + 1) % 6
		d.hUpdateSchemaLogGrid()

	case toolDiagrams:
		d.Opts.OutputDiagrams = (d.Opts.OutputDiagrams + 1) % 4
		d.Mach.EvAdd1(e, ss.DiagramsScheduled, nil)

	case toolTimelines:
		d.Opts.Timelines = (d.Opts.Timelines + 1) % 3
		d.hSyncOptsTimelines()

	case toolReader:
		if d.Mach.Is1(ss.LogReaderEnabled) &&
			d.Mach.Any1(ss.MatrixView, ss.TreeMatrixView) {

			d.Mach.EvAdd1(e, ss.TreeLogView, nil)
		} else if d.Mach.Not1(ss.LogReaderEnabled) {
			d.Mach.EvAdd1(e, ss.LogReaderEnabled, nil)
		} else {
			d.Mach.EvRemove1(e, ss.LogReaderEnabled, nil)
		}

	case toolRain:
		d.Mach.EvAdd1(e, ss.ToolRain, nil)

	case toolHelp:
		d.Mach.EvToggle1(e, ss.HelpDialog, nil)

	case toolPlay:
		if d.Mach.Is1(ss.Paused) {
			d.Mach.EvAdd1(e, ss.Playing, nil)
		} else {
			d.Mach.EvAdd1(e, ss.Paused, nil)
		}

	case toolTail:
		d.Mach.EvToggle1(e, ss.TailMode, nil)

	case toolPrev:
		d.Mach.EvAdd1(e, ss.UserBack, nil)

	case toolNext:
		d.Mach.EvAdd1(e, ss.UserFwd, nil)

	case toolNextStep:
		d.Mach.EvAdd1(e, ss.UserFwdStep, nil)

	case toolPrevStep:
		d.Mach.EvAdd1(e, ss.UserBackStep, nil)

	case toolJumpPrev:
		// TODO state
		go d.hJumpBackKey(nil)

	case toolJumpNext:
		// TODO state
		go d.hJumpFwdKey(nil)

	case toolFirst:
		d.hToolFirstTx(e)

	case toolLast:
		d.hToolLastTx(e)

	case toolExpand:
		// TODO refresh toolbar on focus changes, reflect expansion state
		d.hToolExpand()

	case toolMatrix:
		d.toolMatrix()

	case toolExport:
		d.Mach.EvToggle1(e, ss.ExportDialog, nil)
	}

	// TODO typed args
	d.Mach.EvAdd1(e, ss.ToolToggled, am.A{"filterTxs": filterTxs})
}

func (d *Debugger) ToolToggledState(e *am.Event) {
	defer d.Mach.Remove1(ss.ToolToggled, nil)
	filterTxs, _ := e.Args["filterTxs"].(bool)

	if filterTxs {
		d.hFilterClientTxs()
	}

	if d.C != nil {

		// TODO scroll the log to prev position

		// stay on the last one
		if d.Mach.Is1(ss.TailMode) {
			d.hSetCursor1(e, am.A{
				"cursor1":    len(d.C.MsgTxs),
				"filterBack": true,
			})
		}

		// rebuild the whole log to reflect the UI changes
		d.Mach.EvAdd1(e, ss.BuildingLog, am.A{
			"logRebuildEnd": len(d.C.MsgTxs),
		})
		// TODO optimization: param to avoid this
		d.Mach.Add1(ss.UpdateLogScheduled, nil)

		if filterTxs {
			d.hSetCursor1(e, am.A{
				"cursor1":    d.C.CursorTx1,
				"filterBack": true,
			})
		}
	}

	d.updateClientList()
	d.hUpdateToolbar()
	d.hUpdateTimelines()
	d.hUpdateMatrix()
	d.hUpdateFocusable()
	d.hUpdateTxBars()
	d.draw()
}

func (d *Debugger) SwitchingClientTxState(e *am.Event) {
	clientID, _ := e.Args["Client.id"].(string)
	cursorTx, _ := e.Args["cursorTx1"].(int)
	ctx := d.Mach.NewStateCtx(ss.SwitchingClientTx)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		if d.C != nil && d.C.id != clientID {
			// TODO async helper
			when := d.Mach.WhenTicks(ss.ClientSelected, 2, ctx)
			d.Mach.Add1(ss.SelectingClient, am.A{"Client.id": clientID})
			<-when
			if ctx.Err() != nil {
				return // expired
			}
		}

		when := d.Mach.WhenTicks(ss.ScrollToTx, 2, ctx)
		d.Mach.Add1(ss.ScrollToTx, am.A{
			"cursorTx1":   cursorTx,
			"trimHistory": true,
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

	// TODO validate in Enter
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
		if d.filtersActive() && !slices.Contains(c.MsgTxsFiltered, msgIdx) {
			continue
		}

		// scroll to this tx
		d.Mach.Add1(ss.ScrollToTx, am.A{
			"cursorTx1":   i,
			"trimHistory": true,
		})
		break
	}
}

func (d *Debugger) ExceptionEnter(e *am.Event) bool {
	// ignore eval timeouts, but log them
	a := am.ParseArgs(e.Args)
	if errors.Is(a.Err, am.ErrEvalTimeout) {
		if !e.IsCheck {
			d.Mach.Log(a.Err.Error())
		}

		return false
	}

	return true
}

// ExceptionState creates a log file with the error and stack trace, after
// calling the super exception handler.
func (d *Debugger) ExceptionState(e *am.Event) {
	d.ExceptionHandler.ExceptionState(e)
	args := am.ParseArgs(e.Args)

	d.hUpdateBorderColor()

	// create / append the err log file
	s := fmt.Sprintf("\n\n%s\n%s\n\n%s", time.Now(), args.Err, args.ErrTrace)
	path := filepath.Join(d.Opts.OutputDir, "am-dbg-err.log")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		d.Mach.Log("Error: %s\n", err)
		return
	}
	_, err = f.Write([]byte(s))
	if err1 := f.Close(); err1 != nil && err == nil {
		d.Mach.Log("Error: %s\n", err1)
		return
	}
}

func (d *Debugger) GcMsgsEnter(e *am.Event) bool {
	return AllocMem() > uint64(d.Opts.MaxMemMb)*1024*1024
}

func (d *Debugger) GcMsgsState(e *am.Event) {
	// TODO GC log reader entries
	// TODO GC tx steps before GCing transitions
	defer d.Mach.Remove1(ss.GcMsgs, nil)
	ctx := d.Mach.NewStateCtx(ss.GcMsgs)

	// unblock

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
				if log.Level <= am.LogChanges {
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
			d.Mach.AddErr(errors.New("too many GC rounds"), nil)
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
			// TODO refresh c.errors (extract from hParseMsg)s

			// adjust the current client
			if d.C == c {

				// rebuild the whole log
				d.Mach.Add1(ss.BuildingLog, am.A{
					"logRebuildEnd": len(c.MsgTxs) - 1,
				})
				c.CursorTx1 = int(math.Max(0, float64(c.CursorTx1-idx)))
				// re-filter
				if d.filtersActive() {
					d.hFilterClientTxs()
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

	d.hRedrawFull(false)
}

func (d *Debugger) LogReaderVisibleState(e *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.Mach.Add1(ss.UpdateFocus, nil)
}

func (d *Debugger) LogReaderVisibleEnd(e *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.Mach.Add1(ss.UpdateFocus, nil)
}

func (d *Debugger) SetCursorEnter(e *am.Event) bool {
	_, ok := e.Args["cursor1"].(int)
	return ok
}

// TODO remove?
func (d *Debugger) SetCursorState(e *am.Event) {
	d.hSetCursor1(e, e.Args)
}

// hSetCursor1 sets both the tx and steps cursors, 1-based.
func (d *Debugger) hSetCursor1(e *am.Event, args am.A) {
	// TODO typed args...
	cursor1 := args["cursor1"].(int)
	cursorStep1, _ := args["cursorStep1"].(int)
	skipHistory, _ := args["skipHistory"].(bool)
	trimHistory, _ := args["trimHistory"].(bool)
	filterBack, _ := args["filterBack"].(bool)

	// TODO optimize for no-change?
	d.C.CursorTx1 = d.hFilterTxCursor1(d.C, cursor1, filterBack)
	// reset the step timeline
	// TODO validate
	d.C.CursorStep1 = cursorStep1

	// TODO dont create 2 entries on the first mach change
	if d.HistoryCursor == 0 && !skipHistory {
		// add current mach if needed
		if len(d.History) > 0 && d.History[0].MachId != d.C.id {
			d.hPrependHistory(d.hGetMachAddress())
		}
		// keeping the current tx as history head
		if tx := d.C.tx(d.C.CursorTx1 - 1); tx != nil {
			// dup the current machine if tx differs
			if len(d.History) > 1 && d.History[1].MachId == d.C.id &&
				d.History[1].TxId != tx.ID {

				d.hPrependHistory(d.History[0].Clone())
			}
			if len(d.History) > 0 {
				d.History[0].TxId = tx.ID
			}
		}
		d.hTrimHistory()

	} else if trimHistory {
		d.hTrimHistory()
	}
	d.hHandleTStepsScrolled()

	// debug
	// d.Opts.DbgLogger.Printf("HistoryCursor: %d\n", d.HistoryCursor)
	// d.Opts.DbgLogger.Printf("History: %v\n", d.History)

	if cursor1 == 0 {
		d.lastScrolledTxTime = time.Time{}
	} else {
		tx := d.hCurrentTx()
		d.lastScrolledTxTime = *tx.Time

		// tx file
		if d.Opts.OutputTx {
			index := d.C.MsgStruct.StatesIndex
			_, _ = d.txListFile.WriteAt([]byte(tx.TxString(index)), 0)
		}
	}

	d.Mach.EvRemove1(e, ss.TimelineStepsScrolled, nil)
	// optional diagrams
	go amhelp.AskEvAdd1(e, d.Mach, ss.DiagramsScheduled, nil)
}

// func (d *Debugger) CursorSetState(e *am.Event) {
// }

func (d *Debugger) DiagramsScheduledEnter(e *am.Event) bool {
	// TODO refuse on too many ErrDiagrams, remove ErrDiagrams in ErrDiagramsState
	return d.C != nil && d.Opts.OutputDiagrams > 0
}

func (d *Debugger) DiagramsScheduledState(e *am.Event) {
	// TODO cancel rendering on:
	//  - client change
	//  - details change
	//  - but not on tx change (wait until completed)
	d.Mach.EvAdd1(e, ss.DiagramsRendering, nil)
}

func (d *Debugger) DiagramsRenderingEnter(e *am.Event) bool {
	return d.Opts.OutputDiagrams > 0 && d.C != nil
}

func (d *Debugger) DiagramsRenderingState(e *am.Event) {
	lvl := d.Opts.OutputDiagrams
	dir := path.Join(d.Opts.OutputDir, "diagrams")
	c := d.C
	tx := d.hCurrentTx()
	svgName := fmt.Sprintf("%s-%d-%s", c.id, lvl, c.schemaHash)

	// state groups
	var states S
	if g := c.SelectedGroup; g != "" {
		states = c.msgSchemaParsed.Groups[g]
		gg := strings.ReplaceAll(strings.ReplaceAll(g, "-", ""), " ", "")
		svgName = fmt.Sprintf("%s-%s-%d-%s",
			c.id, strings.ToLower(gg), lvl, c.schemaHash)
	}
	svgPath := filepath.Join(dir, svgName+".svg")

	// output dir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams,
			fmt.Errorf("create output dir: %w", err), nil)
	}

	// cached?

	// mem cache
	if d.cache.diagramId == c.id && d.cache.diagramLvl == lvl {
		cache := d.cache.diagramDom
		go d.diagramsMemCache(e, c.id, cache, tx, dir, svgName)
		return

		// file cache
	} else if _, err := os.Stat(svgPath); err == nil {
		go d.diagramsFileCache(e, c.id, tx, d.cache.diagramLvl, dir, svgName)
		return
	}

	// no cache - render

	// clone the current graph TODO optimize
	shot, err := d.graph.Clone()
	if err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams, err, nil)
		return
	}

	// unblock
	go d.diagramsRender(e, shot, c.id, lvl, len(d.Clients), dir, svgName, states)
}

func (d *Debugger) DiagramsReadyState(e *am.Event) {
	defer d.Mach.EvRemove1(e, ss.DiagramsReady, nil)

	// update cache
	// TODO typed args
	cache, ok := e.Args["Diagram.cache"].(*goquery.Document)
	if ok {
		d.cache.diagramDom = cache
		d.cache.diagramLvl = e.Args["Diagram.lvl"].(int)
		d.cache.diagramId = e.Args["Diagram.id"].(string)
	}

	// render a fresher one, if scheduled
	d.genGraphsLast = time.Now()
	if d.Mach.Is1(ss.DiagramsScheduled) {
		d.Mach.EvRemove1(e, ss.DiagramsScheduled, nil)
		d.Mach.EvAdd1(e, ss.DiagramsRendering, nil)
	}
}

func (d *Debugger) ClientListVisibleState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.ClientListVisible)
	// TODO via relations
	d.Mach.Add1(ss.UpdateFocus, nil)
	d.buildClientList(-1)
	go func() {
		if !amhelp.Wait(ctx, sidebarUpdateDebounce) {
			return
		}
		d.drawClientList()
	}()
}

func (d *Debugger) ClientListVisibleEnd(e *am.Event) {
	// TODO via relations
	d.Mach.Add1(ss.UpdateFocus, nil)
}

func (d *Debugger) TimelineTxHiddenState(e *am.Event) {
	// handled in TimelineStepsHiddenState
	if e.Machine().Is1(ss.TimelineStepsHidden) {
		return
	}

	d.hUpdateLayout()
}

func (d *Debugger) TimelineTxHiddenEnd(e *am.Event) {
	d.hUpdateLayout()
}

func (d *Debugger) TimelineStepsHiddenEnd(e *am.Event) {
	d.hUpdateLayout()
}

func (d *Debugger) TimelineStepsHiddenState(e *am.Event) {
	d.hUpdateLayout()
}

func (d *Debugger) UpdateFocusState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.UpdateFocus)
	d.hUpdateFocusable()
	d.hUpdateBorderColor()

	var focused cview.Primitive
	var box *cview.Box
	// change focus (or not) when changing view types
	switch d.Mach.Switch(ss.GroupFocused) {
	default:
		focused = d.clientList
		box = d.clientList.Box
	case ss.AddressFocused:
		focused = d.addressBar
		box = d.addressBar.Box
	case ss.TreeFocused:
		focused = d.tree
		box = d.tree.Box
	case ss.LogFocused:
		focused = d.log
		box = d.log.Box
	case ss.LogReaderFocused:
		focused = d.logReader
		box = d.logReader.Box
	case ss.MatrixFocused:
		focused = d.matrix
		box = d.matrix.Box
	case ss.TimelineTxsFocused:
		focused = d.timelineTxs
		box = d.timelineTxs.Box
	case ss.TimelineStepsFocused:
		focused = d.timelineSteps
		box = d.timelineSteps.Box
	case ss.Toolbar1Focused:
		focused = d.toolbars[0]
		box = d.toolbars[0].Box
	case ss.Toolbar2Focused:
		focused = d.toolbars[1]
		box = d.toolbars[1].Box
	case ss.Toolbar3Focused:
		focused = d.toolbars[2]
		box = d.toolbars[2].Box
	case ss.DialogFocused:
		switch {
		case d.Mach.Is1(ss.HelpDialog):
			focused = d.helpDialog
			box = d.helpDialog.Box
		case d.Mach.Is1(ss.ExportDialog):
			focused = d.exportDialog
			box = d.exportDialog.Box
		}
	}

	// move focus if invalid
	if !slices.Contains(d.focusable, box) {
		// TODO take prev el from GroupFocused (ordered visually)
		focused = d.clientList
	}

	// unblock bc of locks
	// TODO race fix locks
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// TODO called on init without focusable elements
		d.focusManager.Focus(focused)
	}()
}

func (d *Debugger) AfterFocusEnter(e *am.Event) bool {
	// TODO typed params
	p, ok := e.Args["cview.Primitive"]
	if !ok {
		return false
	}

	b, _ := d.hBoxFromPrimitive(p)

	// skip when focus impossible
	return slices.Contains(d.focusable, b)
}

func (d *Debugger) AfterFocusState(e *am.Event) {
	// TODO typed params
	focused := e.Args["cview.Primitive"]

	box, state := d.hBoxFromPrimitive(focused)
	// d.Mach.Log("focusing %s", state)
	err := d.focusManager.SetFocusIndex(slices.Index(d.focusable, box))
	if err != nil {
		d.Mach.AddErr(err, nil)
		return
	}
	d.Mach.Add1(state, nil)

	// update the log highlight on focus change
	if d.Mach.Is1(ss.TreeLogView) && d.Mach.Not1(ss.LogReaderFocused) {
		d.Mach.Add1(ss.UpdateLogScheduled, nil)
	}

	d.hUpdateClientList()
	d.hUpdateStatusBar()
}

func (d *Debugger) ToolRainState(e *am.Event) {
	if d.Mach.Is1(ss.MatrixRain) {
		d.Mach.Add1(ss.TreeLogView, nil)
	} else {
		d.Mach.Add(am.S{ss.MatrixRain, ss.TreeMatrixView}, nil)
		// TODO force redraw to get rect size, not ideal
		d.redrawCallback = func() {
			time.Sleep(16 * time.Millisecond)
			d.Mach.Eval("ToolRainState", func() {
				d.hDrawViews()
			}, nil)
		}
	}
}

// AnyEnter prevents most of mutations during a UI redraw (and vice versa)
// forceful race solving
func (d *Debugger) AnyEnter(e *am.Event) bool {
	// always pass network traffic
	mach := d.Mach
	mut := e.Mutation()
	called := mut.CalledIndex(ss.Names)
	pass := S{
		ss.ClientMsg, ss.ConnectEvent, ss.DisconnectEvent,
		am.StateException,
	}
	if called.Any1(pass...) {
		return true
	}

	// dont allow mutations while drawing, pull 10 times
	delay := mach.HandlerTimeout / 10
	tries := 100
	// compensate extended timeouts
	if amhelp.IsDebug() {
		delay = 10 * time.Millisecond
	}
	for e.IsValid() {
		ok := !d.drawing.Load()
		if ok {
			// ok
			return true
		}

		// delay, but avoid the race detector which gets stuck here
		if !d.Opts.DbgRace {
			time.Sleep(delay)
		}

		tries--
		if tries <= 0 {
			// not ok
			break
		}
	}

	// prepend this mutation to the queue and try again TODO loop guard
	// d.Mach.Log("postpone mut")
	go mach.PrependMut(mut)

	return false
}

// AnyState is a global final handler
func (d *Debugger) AnyState(e *am.Event) {
	tx := e.Transition()

	// redraw on auto states
	// TODO this should be done better
	if tx.IsAuto() && tx.IsAccepted.Load() {
		d.repaintPending.Store(false)
		d.hUpdateTxBars()
		d.draw()
	} else if d.repaintPending.Swap(false) {
		d.draw()
	}
}

func (d *Debugger) WebReqState(e *am.Event) {
	// TODO TYPED PARAMS
	r := e.Args["*http.Request"].(*http.Request)
	w := e.Args["http.ResponseWriter"].(http.ResponseWriter)
	done := e.Args["doneChan"].(chan struct{})
	defer close(done)

	u := r.RequestURI
	switch {

	// diagram viewer
	case u == "/":
		fallthrough
	case u == "/diagrams/mach":
		html := string(visualizer.HtmlDiagram)
		html = strings.ReplaceAll(html, "localhost:6831", d.Opts.AddrHttp)
		_, err := w.Write([]byte(html))
		d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)

	// default svg symlink
	case strings.HasPrefix(u, "/diagrams/mach.svg"):
		svgPath := filepath.Join(d.Opts.OutputDir, "diagrams", "am-vis.svg")
		b, err := os.ReadFile(svgPath)
		d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)
		if err != nil {
			return
		}
		_, err = w.Write(b)
		d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)
	}
}

func (d *Debugger) WebSocketState(e *am.Event) {
	// TODO TYPED PARAMS
	ws := e.Args["*websocket.Conn"].(*websocket.Conn)
	r := e.Args["*http.Request"].(*http.Request)
	done := e.Args["doneChan"].(chan struct{})
	clientDone := make(chan struct{})

	// unblock
	go func() {
		for {
			// wait for diagrams start
			select {
			case <-clientDone:
				close(done)
				return
			case <-d.Mach.When1(ss.DiagramsScheduled, nil):
			}

			// wait for diagrams ready
			select {
			case <-clientDone:
				close(done)
				return
			// TODO loop over WSs in DiagramsReady. no goroutine per each
			case <-d.Mach.When1(ss.DiagramsReady, nil):
				// msg
				err := ws.Write(r.Context(), websocket.MessageText,
					[]byte("refresh"))
				d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)
				if err != nil {
					return
				}
			}
		}
	}()

	go func() {
		for {
			// msgType, msg, err := ws.Read(r.Context())
			_, _, err := ws.Read(r.Context())
			if err != nil {
				if websocket.CloseStatus(err) != -1 {
					d.Mach.Log("websocket closed")
				} else {
					err = fmt.Errorf("websocket read: %w", err)
					d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)
				}

				// close up
				close(clientDone)
				return
			}
		}
	}()
}

func (d *Debugger) FilterCanceledTxEnd(e *am.Event) {
	// show empty when showing canceled
	d.Mach.Remove1(ss.FilterEmptyTx, nil)
}

func (d *Debugger) FilterQueuedTxEnd(e *am.Event) {
	// show empty when showing queued
	d.Mach.Remove1(ss.FilterEmptyTx, nil)
}

func (d *Debugger) MatrixRainSelectedState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.MatrixRainSelected)
	row := e.Args["row"].(int)
	column := e.Args["column"].(int)
	currTxRow := e.Args["currTxRow"].(int)
	c := d.C
	index := c.MsgStruct.StatesIndex
	// _, _, _, height := d.matrix.GetInnerRect()

	// select state name
	if column >= 0 && column < len(index) {
		d.Mach.Add1(ss.StateNameSelected, am.A{
			"state": index[column],
		})
	}

	// scroll to another?
	if row == currTxRow || row == -1 {
		return
	}

	diff := row - currTxRow
	idx := c.filterIndexByCursor1(c.CursorTx1) + diff
	if idx == -1 {
		return
	}

	row = c.MsgTxsFiltered[idx] + 1

	// unblock
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// scroll
		if am.Canceled == amhelp.Add1Sync(ctx, d.Mach, ss.ScrollToTx, am.A{
			"cursorTx1":   row,
			"trimHistory": true,
		}) {
			return
		}

		// update layout
		d.Mach.Eval("MatrixRainSelectedState", func() {
			d.hUpdateMatrixRain()
			d.updateClientList()
			d.hRedrawFull(false)
		}, ctx)
	}()
}

func (d *Debugger) ResizedState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.Resized)

	d.lastResize = d.Mach.TimeSum(nil)
	d.hUpdateNarrowLayout()

	// rebuild log
	if d.Mach.Not1(ss.ClientSelected) {
		return
	}

	// TODO loose logRebuildEnd and include as relation
	d.Mach.Add1(ss.BuildingLog, am.A{"logRebuildEnd": len(d.C.MsgTxs)})
	go func() {
		<-d.Mach.When1(ss.LogBuilt, ctx)
		if ctx.Err() != nil {
			return // expired
		}
		// force a redraw TODO bug?
		d.draw()
	}()
}
