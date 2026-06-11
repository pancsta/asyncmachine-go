// TODO ExceptionState: separate error screen with stack trace

package debugger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/coder/websocket"
	"github.com/gdamore/tcell/v2"
	"github.com/pancsta/cview"
	"github.com/soheilhy/cmux"
	"golang.org/x/exp/maps"

	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	"github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
	amvis "github.com/pancsta/asyncmachine-go/tools/visualizer"
)

var _ = ss.ErrGraph

func (d *Debugger) ErrGraphEnter(e *am.Event) bool {
	// ignore graph errs
	return false
}

// TODO Enter

var _ = ss.Start

func (d *Debugger) StartState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.Start)
	view := d.params.StartupView

	// cview TUI app
	d.App = cview.NewApplication()
	if d.params.Screen != nil {
		d.App.SetScreen(d.params.Screen)

		// headless mode
	} else if d.params.UiSsh {
		d.Mach.EvAdd1(e, ss.SshServer, nil)
		d.App.SetScreen(tcell.NewSimulationScreen("UTF-8"))
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
		d.Mach.Go(ctx, func() {
			time.Sleep(time.Millisecond * 300)
			d.Mach.Add1(ss.Resized, nil)
		})
	})
	// catch ctrl+c
	// d.App.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
	// 	if event.Key() == tcell.KeyCtrlC {
	// 		_ = d.Mach.EvAdd1(e, ss.Disposing, nil)
	// 		return nil
	// 	}
	//
	// 	return event
	// })

	// init the rest
	d.hBindKeyboard()
	d.hInitUiComponents()
	d.hInitLayout()
	// d.hUpdateFocusableList()
	if d.params.EnableMouse {
		d.App.EnableMouse(true)
	}
	if d.params.ViewReader {
		d.Mach.EvAdd1(e, ss.LogReaderEnabled, nil)
	}

	// default filters
	filters := S{ss.FilterChecks}
	if d.params.Filters.SkipOutGroup {
		filters = append(filters, ss.FilterOutGroup)
	}
	d.Mach.EvAdd(e, filters, nil)

	// draw in a goroutine
	d.Mach.Fork(ctx, e, func() {
		d.App.SetRoot(d.LayoutRoot, true)
		err := d.App.Run()
		if err != nil {
			d.Mach.AddErr(err, nil)
		}

		d.Mach.EvAdd1(e, ss.Disposing, nil)
	})

	// post-start ops
	d.Mach.Go(ctx, func() {
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
		d.Mach.Add1(ss.Ready, nil)
	})

	// servers TODO extract, wait for listening

	if d.ServerMux != nil {
		d.Mach.Go(ctx, func() {
			if err := d.ServerMux.Serve(); err != nil &&
				!errors.Is(err, cmux.ErrListenerClosed) &&
				!errors.Is(err, cmux.ErrServerClosed) {

				d.Mach.EvAddErr(e, err, nil)
			}
		})
	}

	if d.ServerHttp != nil {
		if d.params.UiMcp {
			mcp, err := newMcpServer(d)
			if err != nil {
				d.Mach.Log("Error: %s", err)
				return
			}
			d.ServerHttp.Handler.(*http.ServeMux).Handle("/mcp", mcp.Http)
		}

		d.Mach.Go(ctx, func() {
			if err := d.ServerHttp.ListenAndServe(); err != nil &&
				err != http.ErrServerClosed {

				d.Mach.EvAddErr(e, err, nil)
			}
		})
	}
}

func (d *Debugger) StartEnd(e *am.Event) {
	if d.App.GetScreen() != nil {
		d.App.Stop()
	}
}

var _ = ss.Ready

func (d *Debugger) ReadyState(e *am.Event) {
	d.heartbeatT = time.NewTicker(heartbeatInterval)
	ctx := d.Mach.NewStateCtx(ss.Ready)

	// late options
	// TODO move to hSetParams?
	if d.params.ViewNarrow {
		d.Mach.EvAdd1(e, ss.UserNarrowLayout, nil)
	}
	d.hSyncOptsTimelines()
	if d.params.OutputDiagrams.Value > 0 {
		d.Mach.EvAdd1(e, ss.DiagramsScheduled, nil)
	}
	if d.params.ViewRain {
		d.Mach.EvAdd1(e, ss.MatrixRain, nil)
	}
	if d.params.TailMode {
		d.Mach.EvAdd1(e, ss.TailMode, nil)
	}

	// TODO merge parsing with addr bar
	if addr, err := types.ParseMachUrl(d.params.MachUrl); err == nil {
		go d.GoToMachAddress(addr, false)
	}

	// initial focus TODO def focus
	d.Mach.EvAdd(e, S{ss.AfterFocus, ss.ClientListFocused}, Pass(&A{
		FocusPrimitive: d.clientList,
	}))

	// unblock
	d.Mach.Fork(ctx, e, func() {
		for {
			select {
			case <-d.heartbeatT.C:
				d.Mach.Add1(ss.Heartbeat, nil)

			case <-ctx.Done():
				d.heartbeatT.Stop()
				return
			}
		}
	})

	// select imported client TODO
	if len(d.Clients) > 0 &&
		!d.Mach.Any1(ss.ClientSelected, ss.SelectingClient) &&
		d.params.MachUrl == "" {

		c := d.Clients[maps.Keys(d.Clients)[0]]
		d.Mach.EvAdd1(e, ss.SelectingClient, Pass(&A{
			ClientId: c.Id,
		}))
	}
}

func (d *Debugger) ReadyEnd(e *am.Event) {
	d.heartbeatT.Stop()
}

var _ = ss.Heartbeat

func (d *Debugger) HeartbeatState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.Ready)
	d.Mach.EvRemove1(e, ss.Heartbeat, nil)
	d.Mach.Fork(ctx, e, func() {
		amhelp.AskEvAdd1(e, d.Mach, ss.GcMsgs, nil)
	})
}

var _ = ss.StateNameSelected

func (d *Debugger) StateNameSelectedEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)
	return args.State != ""
}

func (d *Debugger) StateNameSelectedState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.StateNameSelected)
	d.C.SelectedState = am.ParseArgs[A](e.Args).State
	d.lastSelectedState = d.C.SelectedState

	switch d.Mach.Switch(states.DebuggerGroups.Views) {

	case ss.TreeLogView:
		d.hUpdateSchemaTree()

	case ss.TreeMatrixView:
		d.hUpdateSchemaTree()
		d.hUpdateMatrix()

	case ss.MatrixView:
		d.hUpdateMatrix()
	}

	d.hUpdateStatusBar()
	d.Mach.Fork(ctx, e, func() {
		amhelp.AskEvAdd1(e, d.Mach, ss.DiagramsScheduled, nil)
	})
}

func (d *Debugger) StateNameSelectedEnd(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.Start)
	if d.C != nil {
		d.C.SelectedState = ""
	}
	d.hUpdateSchemaTree()
	d.hUpdateStatusBar()
	d.Mach.Fork(ctx, e, func() {
		amhelp.AskEvAdd1(e, d.Mach, ss.DiagramsScheduled, nil)
	})
}

var _ = ss.Playing

func (d *Debugger) PlayingState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.Playing)
	if d.playTimer == nil {
		d.playTimer = time.NewTicker(playInterval)
	} else {
		// TODO dont reset if resuming after switching clients
		d.playTimer.Reset(playInterval)
	}

	// initial play step
	if d.Mach.Is1(ss.TimelineStepsFocused) {
		d.Mach.EvAdd1(e, ss.FwdStep, nil)
	} else {
		d.Mach.EvAdd1(e, ss.Fwd, nil)
	}
	d.hUpdateToolbar()

	d.Mach.Fork(ctx, e, func() {
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
	})
}

func (d *Debugger) PlayingEnd(e *am.Event) {
	d.playTimer.Stop()
	d.hUpdateToolbar()
}

var _ = ss.Paused

func (d *Debugger) PausedState(e *am.Event) {
	// TODO stop scrolling the log when coming from TailMode (confirm)
	d.hUpdateTxBars()
	d.draw()
}

var _ = ss.TailMode

func (d *Debugger) TailModeState(e *am.Event) {
	d.hSetCursor1(e, &A{
		Cursor1:    len(d.C.MsgTxs),
		FilterBack: true,
	})
	d.hUpdateMatrix()
	d.hUpdateClientList()
	d.hUpdateToolbar()
	// needed bc tail mode if carried over via SelectingClient
	d.hRedrawFull(true)
}

func (d *Debugger) TailModeEnd(e *am.Event) {
	d.hUpdateMatrix()
	d.hUpdateToolbar()
	d.hRedrawFull(true)
}

var _ = ss.Redraw

func (d *Debugger) RedrawState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.Redraw, nil)
	immediate := am.ParseArgs[A](e.Args).Immediate
	d.hRedrawFull(immediate)
}

// ///// FWD / BACK

var _ = ss.UserFwd

func (d *Debugger) UserFwdState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.UserFwd, nil)
}

var _ = ss.Fwd

func (d *Debugger) FwdEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)
	amount := max(args.Amount, 1)
	return d.C.CursorTx1+amount <= len(d.C.MsgTxs)
}

func (d *Debugger) FwdState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.Fwd, nil)
	c := d.C

	args := am.ParseArgs[A](e.Args)
	amount := max(args.Amount, 1)

	d.hSetCursor1(e, &A{
		Cursor1: c.CursorTx1 + amount,
	})
	if d.Mach.Is1(ss.Playing) && c.CursorTx1 == len(c.MsgTxs) {
		d.Mach.EvRemove1(e, ss.Playing, nil)
	}

	// sidebar for errs
	d.hUpdateClientList()
	d.hRedrawFull(false)
}

var _ = ss.UserBack

func (d *Debugger) UserBackState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.UserBack, nil)
}

var _ = ss.Back

func (d *Debugger) BackEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)
	amount := max(args.Amount, 1)
	return d.C.CursorTx1-amount >= 0
}

func (d *Debugger) BackState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.Back, nil)

	args := am.ParseArgs[A](e.Args)
	amount := max(args.Amount, 1)

	d.hSetCursor1(e, &A{
		Cursor1:    d.C.CursorTx1 - amount,
		FilterBack: true,
	})

	// sidebar for errs
	d.hUpdateClientList()
	d.hRedrawFull(false)
}

// ///// STEP BACK / FWD

var _ = ss.UserFwdStep

func (d *Debugger) UserFwdStepState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.UserFwdStep, nil)
}

var _ = ss.FwdStep

func (d *Debugger) FwdStepEnter(e *am.Event) bool {
	nextTx := d.hNextTx()
	if nextTx == nil {
		return false
	}
	return d.C.CursorStep1 < len(nextTx.Steps)+1
}

func (d *Debugger) FwdStepState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.FwdStep, nil)

	// next tx
	nextTx := d.hNextTx()
	// scroll to the next tx
	if d.C.CursorStep1 == len(nextTx.Steps) {
		d.Mach.EvAdd1(e, ss.Fwd, nil)
		return
	}
	d.C.CursorStep1++

	d.hHandleTStepsScrolled()
	d.hRedrawFull(false)
}

var _ = ss.UserBackStep

func (d *Debugger) UserBackStepState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.UserBackStep, nil)
}

var _ = ss.BackStep

func (d *Debugger) BackStepEnter(e *am.Event) bool {
	return d.C.CursorStep1 > 0 || d.C.CursorTx1 > 0
}

func (d *Debugger) BackStepState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.BackStep, nil)

	// wrap if there's a prev tx
	if d.C.CursorStep1 <= 0 {
		d.hSetCursor1(e, &A{Cursor1: d.hPrevTxIdx() + 1})

		d.Mach.EvAdd1(e, ss.UpdateLogScheduled, nil)
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

var _ = ss.TimelineStepsScrolled

func (d *Debugger) TimelineStepsScrolledState(e *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.hRedrawFull(false)
}

func (d *Debugger) TimelineStepsScrolledEnd(e *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.hRedrawFull(false)
}

var _ = ss.TimelineStepsFocused

func (d *Debugger) TimelineStepsFocusedState(e *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.hRedrawFull(false)
}

func (d *Debugger) TimelineStepsFocusedEnd(e *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.hRedrawFull(false)
}

var _ = ss.Toolbar1Focused

func (d *Debugger) Toolbar1FocusedState(e *am.Event) {
	d.toolbars[0].SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) Toolbar1FocusedEnd(e *am.Event) {
	d.toolbars[0].SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

var _ = ss.Toolbar2Focused

func (d *Debugger) Toolbar2FocusedState(e *am.Event) {
	d.toolbars[1].SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) Toolbar2FocusedEnd(e *am.Event) {
	d.toolbars[1].SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

var _ = ss.Toolbar3Focused

func (d *Debugger) Toolbar3FocusedState(e *am.Event) {
	d.toolbars[2].SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) Toolbar3FocusedEnd(e *am.Event) {
	d.toolbars[2].SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

var _ = ss.Toolbar4Focused

func (d *Debugger) Toolbar4FocusedState(e *am.Event) {
	d.toolbars[3].SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) Toolbar4FocusedEnd(e *am.Event) {
	d.toolbars[3].SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

var _ = ss.AddressFocused

func (d *Debugger) AddressFocusedState(e *am.Event) {
	d.addressBar.SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) AddressFocusedEnd(e *am.Event) {
	d.addressBar.SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

var _ = ss.TreeGroupsFocused

func (d *Debugger) TreeGroupsFocusedState(e *am.Event) {
	d.treeGroups.SetBackgroundColor(d.getFocusColor())
	d.hUpdateToolbar()
}

func (d *Debugger) TreeGroupsFocusedEnd(e *am.Event) {
	d.treeGroups.SetBackgroundColor(cview.Styles.PrimitiveBackgroundColor)
	d.hUpdateToolbar()
}

// ///// CONNECTION

var _ = ss.ConnectEvent

func (d *Debugger) ConnectEventEnter(e *am.Event) bool {
	args := am.ParseArgs[A](e.Args)
	if args.MsgStruct == nil || args.ConnId == "" || args.MsgStruct.ID == "" {
		d.Mach.Log("Error: msg_struct malformed\n")
		return false
	}

	return true
}

func (d *Debugger) ConnectEventState(e *am.Event) {
	// initial structure data
	args := am.ParseArgs[A](e.Args)
	msg := args.MsgStruct
	connId := args.ConnId
	var c *Client

	// cleanup removes all previous clients if all are disconnected
	cleanup := false
	if d.params.CleanOnConnect {
		// remove old clients
		cleanup = d.hCleanOnConnect()
	}

	// update existing client
	if existing, ok := d.Clients[msg.ID]; ok {
		if existing.ConnId != "" && existing.ConnId == connId {
			d.Mach.Log("schema changed for %s", msg.ID)
			// TODO use MsgStructPatch
			// TODO keep old revisions
			existing.MsgStruct = msg
			c = existing
			c.ParseSchema()

		} else {
			d.Mach.Log("client %s already exists, overriding", msg.ID)
			d.hRemoveClient(existing.Id)
		}
	}

	// create a new client
	if c == nil {
		data := &server.Exportable{
			MsgStruct: msg,
		}
		c = newClient(msg.ID, connId, amhelp.SchemaHash(msg.States), data)
		c.Connected.Store(true)
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
	if cleanup || (d.C != nil && d.C.Id == msg.ID) {
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
			active := c.LastActive()
			if active.After(lastActiveTime) || lastActiveID == "" {
				lastActiveTime = active
				lastActiveID = id
			}
		}
		d.Mach.EvAdd1(e, ss.RemoveClient, Pass(&A{
			ClientId: lastActiveID,
		}))
	}

	// if only 1 client connected, select it
	// if the only client in total, select it
	if len(d.Clients) == 1 || (d.params.SelectConnected &&
		d.hConnectedClients() == 1) {

		d.Mach.EvAdd1(e, ss.SelectingClient, Pass(&A{
			ClientId:      msg.ID,
			FromConnected: true,
		}))
		d.hPrependHistory(&types.MachAddress{MachId: msg.ID})

		// re-select the state
		if d.lastSelectedState != "" {
			d.Mach.EvAdd1(e, ss.StateNameSelected, Pass(&A{
				State: d.lastSelectedState,
			}))
			// TODO Keep in StateNameSelected behind a flag
			d.hSelectTreeState(d.lastSelectedState)
		}
	}

	// first client, tail mode
	if len(d.Clients) == 1 {
		d.Mach.EvAdd1(e, ss.TailMode, nil)
	}

	// graph
	if d.graph != nil {
		_ = d.graph.AddClient(msg)
		// TODO errors, check for dups, enable once stable
		// if err != nil {
		// d.Mach.EvAddErr(e, err, nil)
		// }
	}
	d.Mach.EvAdd1(e, ss.InitClient, Pass(&A{
		Id: msg.ID,
	}))

	d.draw()
}

var _ = ss.DisconnectEvent

func (d *Debugger) DisconnectEventEnter(e *am.Event) bool {
	if am.ParseArgs[A](e.Args).ConnId == "" {
		d.Mach.Log("Error: DisconnectEvent malformed\n")
		return false
	}

	return true
}

func (d *Debugger) DisconnectEventState(e *am.Event) {
	connID := am.ParseArgs[A](e.Args).ConnId
	for _, c := range d.Clients {
		if c.ConnId != "" && c.ConnId == connID {
			// mark as disconnected
			c.Connected.Store(false)
			d.Mach.Log("client %s disconnected", c.Id)

			// remove pipes from other clients TODO optimize by following pipes
			for _, c2 := range d.Clients {
				// skip empty
				if len(c2.MsgTxsParsed) == 0 {
					continue
				}

				// TODO create a fake tx, dont overwrite
				lastTx := c2.MsgTxsParsed[len(c2.MsgTxsParsed)-1]
				lastTx.ReaderEntries = slices.DeleteFunc(lastTx.ReaderEntries,
					func(ptr *types.LogReaderEntryPtr) bool {
						entry := c2.GetReaderEntry(ptr.TxId, ptr.EntryIdx)
						return entry != nil && entry.Mach == c.Id
					})
			}

			break
		}
	}

	d.hUpdateBorderColor()
	d.hUpdateAddressBar()
	if d.Mach.Is1(ss.FilterDisconn) {
		d.buildClientList(-1)
	} else {
		d.hUpdateClientList()
	}
	d.draw()
}

// ///// CLIENTS

var _ = ss.ClientMsg

func (d *Debugger) ClientMsgEnter(e *am.Event) bool {
	a := am.ParseArgs[A](e.Args)
	return a.MsgsTx != nil && a.ConnIds != nil
}

func (d *Debugger) ClientMsgState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.ClientMsg, nil)

	// TODO make it async via a dedicated goroutine, pushing results to
	//  async multi state ClientMsgDone (if possible)

	cArgs := am.ParseArgs[A](e.Args)
	msgs := cArgs.MsgsTx
	connIds := cArgs.ConnIds
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
			d.Mach.Log("Error: schema missing for %s, ignoring tx\n", machId)
			continue
		}

		// verify it's from the same client
		if c.ConnId != connIds[i] {
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
				// d.Mach.EvAddErr(e, err, nil)
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

	// update graph file
	amgraph.AddErrGraph(e, d.Mach,
		d.hUpdateGraphFile(e))

	// UI updates for the selected client
	if updateTailMode {
		// force the latest tx
		d.hSetCursor1(e, &A{
			Cursor1:    len(d.C.MsgTxs),
			FilterBack: true,
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

// TODO move
func (d *Debugger) hUpdateGraphFile(e *am.Event) error {
	if !d.params.OutputGraph || d.graphFileMgml == nil || d.graphFileMd == nil {
		return nil
	}

	_ = d.graphFileMd.Truncate(0)
	_ = d.graphFileMgml.Truncate(0)

	// clone the current graph TODO optimize
	shot, err := d.graph.Clone()
	if err != nil {
		return err
	}
	inspect, err := shot.Inspect()
	if err != nil {
		return err
	}
	_, _ = d.graphFileMd.WriteAt([]byte(amgraph.Markdown(inspect)), 0)
	_, _ = d.graphFileMgml.WriteAt([]byte(amgraph.Markup(inspect)), 0)

	return nil
}

var _ = ss.RemoveClient

func (d *Debugger) RemoveClientEnter(e *am.Event) bool {
	cid := am.ParseArgs[A](e.Args).ClientId
	_, ok2 := d.Clients[cid]

	return cid != "" && ok2
}

func (d *Debugger) RemoveClientState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.RemoveClient, nil)
	cid := am.ParseArgs[A](e.Args).ClientId
	c := d.Clients[cid]

	// clean up
	delete(d.Clients, cid)
	d.hRemoveHistory(c.Id)

	// if currently selected, switch to the first one
	if c == d.C {
		for id := range d.Clients {
			d.Mach.EvAdd1(e, ss.SelectingClient, Pass(&A{
				ClientId: id,
			}))
			break
		}
		// if last client, unselect
		if len(d.Clients) == 0 {
			d.Mach.EvRemove1(e, ss.ClientSelected, nil)
		}
		d.buildClientList(-1)
	} else {
		d.buildClientList(d.clientList.GetCurrentItemIndex() - 1)
	}

	d.draw()
}

var _ = ss.SetGroup

func (d *Debugger) SetGroupEnter(e *am.Event) bool {
	group := am.ParseArgs[A](e.Args).Group
	if group == "" {
		return false
	}

	// extract
	group, _, _ = strings.Cut(group, ":")

	_, ok := d.C.MsgSchemaParsed.Groups[group]
	return group != "" && group != d.C.SelectedGroup && ok
}

func (d *Debugger) SetGroupState(e *am.Event) {
	group := am.ParseArgs[A](e.Args).Group
	c := d.C

	if group == "all" {
		c.SelectedGroup = ""
	} else {
		c.SelectedGroup = strings.Split(group, ":")[0]
	}
	d.lastSelectedGroup = c.SelectedGroup
	d.hBuildSchemaTree()
	d.hUpdateSchemaTree()
	go amhelp.AskEvAdd1(e, d.Mach, ss.DiagramsScheduled, nil)
	d.Mach.EvAdd(e, am.S{ss.ToolToggled, ss.UpdateLogScheduled}, Pass(&A{
		FilterTxs:     true,
		LogRebuildEnd: len(c.MsgTxs),
	}))
}

var _ = ss.SelectingClient

func (d *Debugger) SelectingClientEnter(e *am.Event) bool {
	cid := am.ParseArgs[A](e.Args).ClientId
	// same client
	if d.C != nil && cid == d.C.Id && d.Mach.Is1(ss.ClientSelected) {
		return false
	}
	// does the client exist?
	_, ok2 := d.Clients[cid]

	return len(d.Clients) > 0 && cid != "" && ok2
}

func (d *Debugger) SelectingClientState(e *am.Event) {
	// TODO support tx ID
	sArgs := am.ParseArgs[A](e.Args)
	clientID := sArgs.ClientId
	group := sArgs.Group
	fromConnected := sArgs.FromConnected
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
	logRebuildEnd := len(d.C.LogMsgs)
	// remain in TailMode after the selection
	wasTailMode := slices.Contains(e.Transition().StatesBefore(), ss.TailMode)
	d.C.SelectedGroup = group

	// TODO extract SelectingClientFiltered
	// TODO Remove selecting with a timeout (in case it fails)
	d.Mach.Fork(ctx, e, func() {
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
				d.hSetCursor1(e, &A{
					Cursor1:    len(d.C.MsgTxs),
					FilterBack: true,
				})
			}, ctx)
			if ctx.Err() != nil {
				return // expired
			}

		} else {
			// [hSetCursor1] triggers DiagramsScheduled, so do we
			// TODO optimize: push diagram after the initial rendering
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

		d.Mach.EvAdd(e, target, Pass(&A{
			FromConnected: fromConnected,
			FromPlaying:   fromPlaying,
			LogRebuildEnd: logRebuildEnd,
		}))
	})
}

var _ = ss.ClientSelected

func (d *Debugger) ClientSelectedState(e *am.Event) {
	args := am.ParseArgs[A](e.Args)
	ctx := d.Mach.NewStateCtx(ss.ClientSelected)
	fromConnected := args.FromConnected
	fromPlaying := args.FromPlaying

	if ctx.Err() != nil {
		d.Mach.Log("Error: context expired\n")
		return // expired
	}

	// catch up with new log msgs
	for i := max(0, d.logRebuildEnd-1); i < len(d.C.LogMsgs); i++ {
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
		d.Mach.EvAdd1(e, ss.UpdateLogScheduled, nil)
	}
	if d.Mach.Is1(ss.MatrixView) || d.Mach.Is1(ss.TreeMatrixView) {
		d.hUpdateMatrix()
	}

	// first client connected, set tail mode
	if fromConnected && len(d.Clients) == 1 {
		d.Mach.EvAdd1(e, ss.TailMode, nil)
	} else if fromPlaying {
		d.Mach.EvAdd1(e, ss.Playing, nil)
	}

	// re-select the state
	if d.lastSelectedState != "" {
		d.Mach.EvAdd1(e, ss.StateNameSelected, Pass(&A{
			State: d.lastSelectedState,
		}))
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

var _ = ss.HelpDialog

func (d *Debugger) HelpDialogState(e *am.Event) {
	// re-render for mem stats
	d.hUpdateHelpDialog()
	d.hUpdateFocusableList()
	d.hUpdateToolbar()
	// TODO use Visibility instead of SendToFront
	d.LayoutRoot.SendToFront("main")
	d.LayoutRoot.SendToFront(DialogHelp)
	d.Mach.EvAdd1(e, ss.AfterFocus, Pass(&A{
		FocusPrimitive: d.helpDialogLeft,
	}))
}

func (d *Debugger) HelpDialogEnd(e *am.Event) {
	d.Mach.EvRemove1(e, ss.DialogFocused, nil)
}

var _ = ss.ExportDialog

func (d *Debugger) ExportDialogState(e *am.Event) {
	// TODO use Visibility instead of SendToFront
	d.LayoutRoot.SendToFront("main")
	d.LayoutRoot.SendToFront(DialogExport)
	d.Mach.EvAdd(e, am.S{ss.UpdateFocus, ss.DialogFocused}, nil)
}

func (d *Debugger) ExportDialogEnd(e *am.Event) {
	d.Mach.EvRemove1(e, ss.DialogFocused, nil)
}

var _ = ss.DialogFocused

func (d *Debugger) DialogFocusedEnd(e *am.Event) {
	tx := e.Transition()
	diff := am.StatesDiff(states.DebuggerGroups.Dialog, tx.TargetStates())
	if len(diff) == len(states.DebuggerGroups.Dialog) {
		// all dialogs closed, show main
		d.LayoutRoot.SendToFront("main")
		d.hUpdateToolbar()

		// focus prev one
		_, state := d.hBoxFromPrimitive(d.preModalFocus)
		if state != "" {
			d.Mach.EvAdd(e, am.S{ss.UpdateFocus, state}, nil)
		}

		d.draw()
	}
}

var _ = ss.MatrixView

func (d *Debugger) MatrixViewState(e *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

func (d *Debugger) MatrixViewEnd(e *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

var _ = ss.TreeMatrixView

func (d *Debugger) TreeMatrixViewState(e *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

func (d *Debugger) TreeMatrixViewEnd(e *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

var _ = ss.MatrixRain

func (d *Debugger) MatrixRainEnter(e *am.Event) bool {
	states := e.Transition().TimeIndexAfter()

	return states.Any1(ss.TreeMatrixView, ss.MatrixView) &&
		states.Is1(ss.ClientSelected)
}

func (d *Debugger) MatrixRainState(e *am.Event) {
	d.hDrawViews()
	d.hUpdateToolbar()
}

var _ = ss.ScrollToTx

func (d *Debugger) ScrollToTxEnter(e *am.Event) bool {
	stArgs := am.ParseArgs[A](e.Args)
	cursor, id := stArgs.CursorTx1, stArgs.TxId
	c := d.C

	return c != nil && (id != "" && c.TxIndex(id) > -1 ||
		cursor > 0 && len(c.MsgTxs) >= cursor)
}

// ScrollToTxState scrolls to a specific transition (cursor position 1-based).

func (d *Debugger) ScrollToTxState(e *am.Event) {
	defer d.Mach.EvRemove1(e, ss.ScrollToTx, nil)
	args := am.ParseArgs[A](e.Args)
	cursor1 := args.CursorTx1
	cursorStep1 := args.CursorStep1
	trim := args.TrimHistory

	if args.TxId != "" {
		cursor1 = d.C.TxIndex(args.TxId) + 1
	}

	d.hSetCursor1(e, &A{
		Cursor1:     cursor1,
		CursorStep1: cursorStep1,
		TrimHistory: trim,
	})
	d.updateClientList()
	d.hRedrawFull(false)
}

var _ = ss.NarrowLayout

func (d *Debugger) NarrowLayoutExit(e *am.Event) bool {
	// always allow to exit
	after := e.Transition().TimeIndexAfter()
	if after.Not1(ss.Start) {
		return true
	}

	return after.Not1(ss.UserNarrowLayout)
}

func (d *Debugger) NarrowLayoutState(e *am.Event) {
	d.hUpdateToolbar()
	d.hUpdateLayout()
	d.buildClientList(-1)
	d.hRedrawFull(false)
}

func (d *Debugger) NarrowLayoutEnd(e *am.Event) {
	d.Mach.EvAdd1(e, ss.ClientListVisible, nil)
}

var _ = ss.ScrollToStep

func (d *Debugger) ScrollToStepEnter(e *am.Event) bool {
	cursor := am.ParseArgs[A](e.Args).CursorStep1
	c := d.C
	return c != nil && cursor > 0 && d.hNextTx() != nil
}

// ScrollToStepState scrolls to a specific transition (cursor position 1-based).

func (d *Debugger) ScrollToStepState(e *am.Event) {
	// TODO multi?
	d.Mach.EvRemove1(e, ss.ScrollToStep, nil)

	cStep1 := am.ParseArgs[A](e.Args).CursorStep1
	nextTx := d.hNextTx()

	if cStep1 > len(nextTx.Steps) {
		cStep1 = len(nextTx.Steps)
	}
	d.C.CursorStep1 = cStep1

	d.hHandleTStepsScrolled()
	d.hRedrawFull(false)
}

var _ = ss.ToggleTool

func (d *Debugger) ToggleToolEnter(e *am.Event) bool {
	return am.ParseArgs[A](e.Args).ToolName.Value != ""
}

func (d *Debugger) ToggleToolState(e *am.Event) {
	// TODO split the state into an async one
	// TODO refac to FilterToggledState
	tool := am.ParseArgs[A](e.Args).ToolName

	// tool is a filter and needs re-filter txs
	filterTxs := false
	buildClientList := false

	switch tool {
	// TODO move logic after toggle to handlers

	case types.ToolFilterCanceledTx:
		d.Mach.EvToggle1(e, ss.FilterCanceledTx, nil)
		filterTxs = true

	case types.ToolFilterQueuedTx:
		d.Mach.EvToggle1(e, ss.FilterQueuedTx, nil)
		filterTxs = true

	case types.ToolFilterAutoTx:
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

	case types.ToolFilterEmptyTx:
		d.Mach.EvToggle1(e, ss.FilterEmptyTx, nil)
		filterTxs = true

	case types.ToolFilterHealth:
		d.Mach.EvToggle1(e, ss.FilterHealth, nil)
		filterTxs = true

	case types.ToolFilterOutGroup:
		d.Mach.EvToggle1(e, ss.FilterOutGroup, nil)
		filterTxs = true

	case types.ToolFilterChecks:
		d.Mach.EvToggle1(e, ss.FilterChecks, nil)
		filterTxs = true

	case types.ToolFilterRpcMachs:
		d.Mach.EvToggle1(e, ss.FilterRpcMachs, nil)
		buildClientList = true

	case types.ToolFilterDisconn:
		d.Mach.EvToggle1(e, ss.FilterDisconn, nil)
		buildClientList = true

	case types.ToolNarrowLayout:
		if d.Mach.Is1(ss.UserNarrowLayout) {
			d.Mach.EvRemove(e, S{ss.UserNarrowLayout, ss.NarrowLayout}, nil)
		} else {
			d.Mach.EvAdd1(e, ss.UserNarrowLayout, nil)
		}

	case types.ToolLogTimestamps:
		d.Mach.EvToggle1(e, ss.LogTimestamps, nil)
		filterTxs = true

	case types.ToolFilterTraces:
		d.Mach.EvToggle1(e, ss.FilterTraces, nil)

	case types.ToolLog:
		d.params.Filters.LogLevel = (d.params.Filters.LogLevel + 1) % 6
		d.hUpdateSchemaLogGrid()

	case types.ToolDiagrams:
		switch d.params.OutputDiagrams {
		case types.ParamsOutputDiagramsNone:
			d.params.OutputDiagrams = types.ParamsOutputDiagramsOne
		case types.ParamsOutputDiagramsOne:
			d.params.OutputDiagrams = types.ParamsOutputDiagramsTwo
		case types.ParamsOutputDiagramsTwo:
			d.params.OutputDiagrams = types.ParamsOutputDiagramsThree
		case types.ParamsOutputDiagramsThree:
			d.params.OutputDiagrams = types.ParamsOutputDiagramsNone
		}
		d.Mach.EvAdd1(e, ss.DiagramsScheduled, nil)

	case types.ToolDiagramsSteps:
		d.params.OutputTx = !d.params.OutputTx
		if d.params.OutputTx {
			d.hInitOutputTxFiles()
			if tx := d.hCurrentTx(); tx != nil {
				d.hGenSeqDiagram(am.EvToCtx(d.Mach.Context(), e), tx,
					d.C.MsgTxsParsed[d.C.CursorTx1-1])
			}
		} else {
			d.hCloseOutputTxFiles()
		}

	case types.ToolDiagramsTx:
		switch d.params.OutputDiagTx {
		case types.ParamsOutDiagTxNone:
			d.params.OutputDiagTx = types.ParamsOutDiagTxCalled
		case types.ParamsOutDiagTxCalled:
			d.params.OutputDiagTx = types.ParamsOutDiagTxMutated
		case types.ParamsOutDiagTxMutated:
			d.params.OutputDiagTx = types.ParamsOutDiagTxTouched
		case types.ParamsOutDiagTxTouched:
			d.params.OutputDiagTx = types.ParamsOutDiagTxRelations
		case types.ParamsOutDiagTxRelations:
			d.params.OutputDiagTx = types.ParamsOutDiagTxNone
		}
		d.Mach.EvAdd1(e, ss.DiagramsScheduled, nil)

	case types.ToolDiagramsGroup:
		switch d.params.OutputDiagGroup {
		case types.ParamsOutDiagGroupNone:
			d.params.OutputDiagGroup = types.ParamsOutDiagGroupHide
		case types.ParamsOutDiagGroupHide:
			d.params.OutputDiagGroup = types.ParamsOutDiagGroupSkip
		case types.ParamsOutDiagGroupSkip:
			d.params.OutputDiagGroup = types.ParamsOutDiagGroupNone
		}
		d.Mach.EvAdd1(e, ss.DiagramsScheduled, nil)

	case types.ToolCallLog:
		d.params.OutputCallLog = !d.params.OutputCallLog
		if !d.params.OutputCallLog {
			d.hCloseOutputCallLogFiles()
		} else {
			// process whole call log
		}

	case types.ToolOutputLog:
		d.params.OutputLog = !d.params.OutputLog
		if !d.params.OutputLog {
			d.hCloseOutputLog()
		} else {
			d.hCreateOutputLogFile()
			d.Mach.EvAddErr(e, d.outputLogFile(d.Mach.Context()), nil)
		}

	case types.ToolTimelines:
		switch d.params.ViewTimelines {
		case types.ParamsViewTimelinesNone:
			d.params.ViewTimelines = types.ParamsViewTimelinesOne
		case types.ParamsViewTimelinesOne:
			d.params.ViewTimelines = types.ParamsViewTimelinesTwo
		case types.ParamsViewTimelinesTwo:
			d.params.ViewTimelines = types.ParamsViewTimelinesNone
		}
		d.hSyncOptsTimelines()

	case types.ToolReader:
		if d.Mach.Is1(ss.LogReaderEnabled) &&
			d.Mach.Any1(ss.MatrixView, ss.TreeMatrixView) {

			d.Mach.EvAdd1(e, ss.TreeLogView, nil)
		} else if d.Mach.Not1(ss.LogReaderEnabled) {
			d.Mach.EvAdd1(e, ss.LogReaderEnabled, nil)
		} else {
			d.Mach.EvRemove1(e, ss.LogReaderEnabled, nil)
		}

	case types.ToolRain:
		d.Mach.EvAdd1(e, ss.ToolRain, nil)

	case types.ToolLogWrap:
		d.params.ViewLogWrap = !d.params.ViewLogWrap
		d.log.SetWrap(d.params.ViewLogWrap)

	case types.ToolHelp:
		d.Mach.EvToggle1(e, ss.HelpDialog, nil)

	case types.ToolPlay:
		if d.Mach.Is1(ss.Paused) {
			d.Mach.EvAdd1(e, ss.Playing, nil)
		} else {
			d.Mach.EvAdd1(e, ss.Paused, nil)
		}

	case types.ToolTail:
		d.Mach.EvToggle1(e, ss.TailMode, nil)

	case types.ToolWeb:
		go func() {
			err := openURL("http://" + d.params.AddrHttp)
			d.Mach.EvAddErr(e, err, nil)
		}()

	case types.ToolPrev:
		d.Mach.EvAdd1(e, ss.UserBack, nil)

	case types.ToolNext:
		d.Mach.EvAdd1(e, ss.UserFwd, nil)

	case types.ToolNextStep:
		d.Mach.EvAdd1(e, ss.UserFwdStep, nil)

	case types.ToolPrevStep:
		d.Mach.EvAdd1(e, ss.UserBackStep, nil)

	case types.ToolJumpPrev:
		// TODO state
		go d.hJumpBackKey(nil)

	case types.ToolNextClient:
		d.hSwitchClient(e, 1)

	case types.ToolPrevClient:
		d.hSwitchClient(e, -1)

	case types.ToolJumpNext:
		// TODO state
		go d.hJumpFwdKey(nil)

	case types.ToolFirst:
		d.hToolFirstTx(e)

	case types.ToolLast:
		d.hToolLastTx(e)

	case types.ToolExpand:
		// TODO refresh toolbar on focus changes, reflect expansion state
		d.hToolExpand()

	case types.ToolMatrix:
		d.toolMatrix()

	case types.ToolExport:
		d.Mach.EvAdd(e, am.S{ss.ExportDialog, ss.DialogFocused}, nil)
	}

	d.Mach.EvAdd1(e, ss.ToolToggled, Pass(&A{
		FilterTxs:       filterTxs,
		BuildClientList: buildClientList,
	}))
}

var _ = ss.ToolToggled

func (d *Debugger) ToolToggledState(e *am.Event) {
	defer d.Mach.EvRemove1(e, ss.ToolToggled, nil)
	tArgs := am.ParseArgs[A](e.Args)
	filterTxs := tArgs.FilterTxs
	buildClientList := tArgs.BuildClientList

	if filterTxs {
		d.hFilterClientTxs()
	}

	// TODO skip on dialogs
	if d.C != nil {

		// TODO scroll the log to prev position

		// stay on the last one
		if d.Mach.Is1(ss.TailMode) {
			d.hSetCursor1(e, &A{
				Cursor1:    len(d.C.MsgTxs),
				FilterBack: true,
			})
		}

		// rebuild the whole log to reflect the UI changes
		d.Mach.EvAdd1(e, ss.BuildingLog, Pass(&A{
			LogRebuildEnd: len(d.C.MsgTxs),
		}))
		// TODO optimization: param to avoid this
		d.Mach.EvAdd1(e, ss.UpdateLogScheduled, nil)

		if filterTxs {
			d.hSetCursor1(e, &A{
				Cursor1:    d.C.CursorTx1,
				FilterBack: true,
			})
		}
	}

	if buildClientList {
		// TODO immediate via i
		d.buildClientList(-1)
	} else {
		d.updateClientList()
	}
	d.hUpdateToolbar()
	d.hUpdateTimelines()
	d.hUpdateMatrix()
	d.hUpdateTxBars()
	d.draw()
}

var _ = ss.SwitchingClientTx

func (d *Debugger) SwitchingClientTxState(e *am.Event) {
	mach := d.Mach
	args := am.ParseArgs[A](e.Args)
	clientID := args.ClientId
	cursorTx := args.CursorTx1
	ctx := d.Mach.NewStateCtx(ss.SwitchingClientTx)

	d.Mach.Fork(ctx, e, func() {
		if d.C != nil && d.C.Id != clientID {
			amhelp.EvAdd1Async(ctx, e, mach, ss.ClientSelected,
				ss.SelectingClient, Pass(&A{
					ClientId: clientID,
				}))
			if ctx.Err() != nil {
				return // expired
			}
		}

		amhelp.EvAdd1Sync(ctx, e, mach, ss.ScrollToTx, Pass(&A{
			CursorTx1:   cursorTx,
			TrimHistory: true,
		}))
		if ctx.Err() != nil {
			return // expired
		}

		d.Mach.Add1(ss.SwitchedClientTx, nil)
	})
}

var _ = ss.SwitchedClientTx

func (d *Debugger) SwitchedClientTxState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.SwitchedClientTx, nil)
}

var _ = ss.ScrollToMutTx

func (d *Debugger) ScrollToMutTxState(e *am.Event) {
	d.Mach.EvRemove1(e, ss.ScrollToMutTx, nil)

	// TODO validate in Enter
	smArgs := am.ParseArgs[A](e.Args)
	state := smArgs.State
	fwd := smArgs.Fwd

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
		parsed := c.MsgTxsParsed[msgIdx]
		tx := c.MsgTxs[msgIdx]

		// check mutations and canceled
		if !slices.Contains(c.IndexesToStates(parsed.StatesAdded), state) &&
			!slices.Contains(c.IndexesToStates(parsed.StatesRemoved), state) &&
			!slices.Contains(tx.CalledStateNames(c.MsgStruct.StatesIndex), state) {

			continue
		}

		// skip filtered out
		if d.filtersActive() && !slices.Contains(c.MsgTxsFiltered, msgIdx) {
			continue
		}

		// scroll to this tx
		d.Mach.EvAdd1(e, ss.ScrollToTx, Pass(&A{
			CursorTx1:   i,
			TrimHistory: true,
		}))
		break
	}
}

var _ = ss.Exception

func (d *Debugger) ExceptionEnter(e *am.Event) bool {
	// ignore eval timeouts, but log them
	a := am.ParseArgs[am.AException](e.Args)
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
	args := am.ParseArgs[am.AException](e.Args)

	if d.Mach != nil {
		d.hUpdateBorderColor()
	}

	// create / append the err log file
	s := fmt.Sprintf("\n\n%s\n%s\n\n%s", time.Now(), args.Err, args.ErrTrace)
	path := filepath.Join(d.params.OutputDir, "am-dbg-err.log")
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

var _ = ss.GcMsgs

func (d *Debugger) GcMsgsEnter(e *am.Event) bool {
	return AllocMem() > uint64(d.params.MaxMemMb)*1024*1024
}

func (d *Debugger) GcMsgsState(e *am.Event) {
	// TODO GC log reader entries
	// TODO GC tx steps before GCing transitions
	defer d.Mach.EvRemove1(e, ss.GcMsgs, nil)
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

	runtime.GC()
	mem1 := AllocMem()
	d.Mach.Log(d.P.Sprintf("Alloc mem: %d MBs\n", mem1/1024/1024))

	// check TTL of client log msgs >lvl 2
	// TODO remember the tip of cleaning (date) and binary find it, then
	//  continue
	for _, c := range clients {
		for i, logMsg := range c.LogMsgs {
			htime := c.MsgTxs[i].Time
			if htime.Add(d.params.LogOpsTtl).After(time.Now()) {
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
			c.LogMsgs[i] = repl
		}
	}

	runtime.GC()
	mem2 := AllocMem()
	if mem1 > mem2 {
		d.Mach.Log(d.P.Sprintf("GC logs shaved %d MBs\n", (mem1-mem2)/1024/1024))
	}

	round := 0
	for AllocMem() > uint64(d.params.MaxMemMb)*1024*1024 {
		if ctx.Err() != nil {
			d.Mach.Log("GC: context expired")
			break
		}
		if round > 100 {
			d.Mach.EvAddErr(e, errors.New("too many GC rounds"), nil)
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
			c.MsgTxsParsed = c.MsgTxsParsed[idx:]
			c.LogMsgs = c.LogMsgs[idx:]

			// empty cache
			c.ClearCache()
			// TODO GC c.logReader
			// TODO refresh c.errors (extract from hParseMsg)s

			// adjust the current client
			if d.C == c {

				// rebuild the whole log
				d.Mach.EvAdd1(e, ss.BuildingLog, Pass(&A{
					LogRebuildEnd: len(c.MsgTxs),
				}))
				c.CursorTx1 = int(math.Max(0, float64(c.CursorTx1-idx)))
				// re-filter
				if d.filtersActive() {
					d.hFilterClientTxs()
				}
			}

			// delete small clients
			if len(c.MsgTxs) < msgMaxThreshold {
				d.Mach.EvAdd1(e, ss.RemoveClient, Pass(&A{
					ClientId: c.Id,
				}))
			} else {
				c.MTimeSum = 0
				for _, m := range c.MsgTxsParsed {
					c.MTimeSum += m.TimeSum
				}
			}
		}

		runtime.GC()
	}
	mem3 := AllocMem()
	if mem1 > mem3 {
		d.Mach.Log(d.P.Sprintf("GC in total shaved %d MBs", (mem1-mem3)/1024/1024))
	}

	d.hRedrawFull(false)
}

var _ = ss.LogReaderVisible

func (d *Debugger) LogReaderVisibleState(e *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.Mach.EvAdd1(e, ss.UpdateFocus, nil)
}

func (d *Debugger) LogReaderVisibleEnd(e *am.Event) {
	d.hUpdateSchemaLogGrid()
	d.Mach.EvAdd1(e, ss.UpdateFocus, nil)
}

// TODO remove?
var _ = ss.SetCursor

func (d *Debugger) SetCursorState(e *am.Event) {
	d.hSetCursor1(e, am.ParseArgs[A](e.Args))
}

// func (d *Debugger) CursorSetState(e *am.Event) {
// }

var _ = ss.DiagramsScheduled

func (d *Debugger) DiagramsScheduledEnter(e *am.Event) bool {
	// TODO refuse on too many ErrDiagrams, remove ErrDiagrams in ErrDiagramsState
	return d.C != nil && d.params.OutputDiagrams != types.ParamsOutputDiagramsNone
}

func (d *Debugger) DiagramsScheduledState(e *am.Event) {
	// TODO cancel rendering on:
	//  - client change
	//  - details change
	//  - but not on tx change (wait until completed)
	d.Mach.EvAdd1(e, ss.DiagramsRendering, nil)
}

var _ = ss.DiagramsRendering

func (d *Debugger) DiagramsRenderingEnter(e *am.Event) bool {
	return d.params.OutputDiagrams.Value > 0 && d.C != nil
}

func (d *Debugger) DiagramsRenderingState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.DiagramsRendering)
	lvl := d.params.OutputDiagrams.Value
	diagDir := path.Join(d.params.OutputDir, "diagrams")
	c := d.C
	tx := d.hCurrentTx()
	svgName := fmt.Sprintf("%s-%d-%s", c.Id, lvl, c.SchemaHash)

	// state groups
	var states S
	if g := c.SelectedGroup; g != "" &&
		d.params.OutputDiagGroup == types.ParamsOutDiagGroupSkip {

		states = c.MsgSchemaParsed.Groups[g]
		svgName = fmt.Sprintf("%s-%s-%d-%s",
			c.Id, types.NormalizeGroupName(g), lvl, c.SchemaHash)
	}
	svgPath := filepath.Join(diagDir, svgName+".svg")

	// output dir
	if err := os.MkdirAll(diagDir, 0o755); err != nil {
		d.Mach.EvAddErrState(e, ss.ErrDiagrams,
			fmt.Errorf("create output dir: %w", err), nil)
	}

	// cached?

	// build update fragment
	index := c.MsgStruct.StatesIndex
	diagFilters := &amvis.Filters{
		MachId:   c.Id,
		Index:    index,
		Selected: am.S{d.C.SelectedState},
	}
	if tx != nil {
		diagFilters.Active = tx.ActiveStates(index)

		// dim
		if d.params.OutputDiagTx != types.ParamsOutDiagTxNone {
			parsed := c.MsgTxsParsed[c.CursorTx1-1]
			showIdxs := tx.CalledStatesIdxs
			switch d.params.OutputDiagTx {
			case types.ParamsOutDiagTxMutated:
				showIdxs = slices.Concat(parsed.StatesAdded, parsed.StatesRemoved)
			case types.ParamsOutDiagTxTouched:
				fallthrough
			case types.ParamsOutDiagTxRelations:
				showIdxs = parsed.StatesTouched
			}
			diagFilters.Highlighted = c.IndexesToStates(showIdxs)
		}

		// collect touched rels TODO add to step timeline 1-by-1
		if d.params.OutputDiagTx == types.ParamsOutDiagTxRelations {
			for _, step := range tx.Steps {
				if step.Type != am.StepRelation {
					continue
				}

				diagFilters.HighlightedRels = append(diagFilters.HighlightedRels,
					[3]string{
						step.GetFromState(index),
						step.GetToState(index),
						step.RelType.String(),
					})
			}
		}
	}
	if d.params.OutputDiagGroup == types.ParamsOutDiagGroupHide &&
		c.SelectedGroup != "" {

		diagFilters.Visible = c.MsgSchemaParsed.Groups[c.SelectedGroup]
	}

	// mem cache
	if d.cache.diagramName == svgName {
		cache := d.cache.diagramDom
		d.Mach.Fork(ctx, e, func() {
			d.diagramsMemCache(am.EvToCtx(ctx, e), cache, diagFilters,
				diagDir, svgName)
		})
		return

		// file cache, move to mem cache
	} else if _, err := os.Stat(svgPath); err == nil {
		d.Mach.Fork(ctx, e, func() {
			d.diagramsFileCache(am.EvToCtx(ctx, e), diagFilters, lvl,
				diagDir, svgName)
		})
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
	d.Mach.EvAdd1(e, ss.DiagramsNoCache, nil)
	d.Mach.Fork(ctx, e, func() {
		d.diagramsRender(am.EvToCtx(ctx, e), shot, c.Id, diagFilters, lvl,
			len(d.Clients), diagDir, svgName, states)
	})
}

var _ = ss.DiagramsReady

func (d *Debugger) DiagramsReadyState(e *am.Event) {
	defer d.Mach.EvRemove1(e, ss.DiagramsReady, nil)

	// update cache
	args := am.ParseArgs[A](e.Args)
	if args.DiagramCache != nil {
		d.cache.diagramDom = args.DiagramCache
		d.cache.diagramName = args.DiagramName
	}

	// render a fresher one, if scheduled
	d.genGraphsLast = time.Now()
	if d.Mach.Is1(ss.DiagramsScheduled) {
		d.Mach.EvRemove1(e, ss.DiagramsScheduled, nil)
		d.Mach.EvAdd1(e, ss.DiagramsRendering, nil)
	}
}

var _ = ss.ClientListVisible

func (d *Debugger) ClientListVisibleState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.ClientListVisible)
	// TODO via relations
	d.Mach.EvAdd1(e, ss.UpdateFocus, nil)
	d.buildClientList(-1)
	d.hUpdateLayout()
	d.Mach.Fork(ctx, e, func() {
		if !amhelp.Wait(ctx, sidebarUpdateDebounce) {
			return
		}
		d.drawClientList()
	})
}

func (d *Debugger) ClientListVisibleEnd(e *am.Event) {
	// TODO via relations
	d.Mach.EvAdd1(e, ss.UpdateFocus, nil)
}

var _ = ss.TimelineTxHidden

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

var _ = ss.TimelineStepsHidden

func (d *Debugger) TimelineStepsHiddenEnd(e *am.Event) {
	d.hUpdateLayout()
}

func (d *Debugger) TimelineStepsHiddenState(e *am.Event) {
	d.hUpdateLayout()
}

var _ = ss.UpdateFocus

func (d *Debugger) UpdateFocusState(e *am.Event) {
	old := d.focusablePrims
	d.hUpdateFocusableList()
	d.hUpdateBorderColor()

	var focused cview.Primitive
	// change focus (or not) when changing view types
	switch d.Mach.Switch(states.DebuggerGroups.Focused) {
	case ss.ClientListFocused:
		focused = d.clientList
	case ss.AddressFocused:
		focused = d.addressBar
	case ss.TreeFocused:
		focused = d.tree
	case ss.TreeGroupsFocused:
		focused = d.treeGroups
	case ss.LogFocused:
		focused = d.log
	case ss.LogReaderFocused:
		focused = d.logReader
	case ss.MatrixFocused:
		focused = d.matrix
	case ss.TimelineTxsFocused:
		focused = d.timelineTxs
	case ss.TimelineStepsFocused:
		focused = d.timelineSteps
	case ss.Toolbar1Focused:
		focused = d.toolbars[0]
	case ss.Toolbar2Focused:
		focused = d.toolbars[1]
	case ss.Toolbar3Focused:
		focused = d.toolbars[2]
	case ss.Toolbar4Focused:
		focused = d.toolbars[3]
	case ss.DialogFocused:
		switch {
		case d.Mach.Is1(ss.HelpDialog):
			focused = d.helpDialogLeft
		case d.Mach.Is1(ss.ExportDialog):
			focused = d.exportDialog
		}

	// layout changed, focus the nearest
	default:
		focusIdx := slices.Index(old, d.Focused)
		idx := min(max(focusIdx-1, 0), len(d.focusablePrims)-1)
		var state string
		focused, state = d.hBoxFromPrimitive(d.focusablePrims[idx])
		// fix state
		d.Mach.EvAdd1(e, state, nil)
	}
	d.App.SetFocus(focused)
}

var _ = ss.AfterFocus

func (d *Debugger) AfterFocusEnter(e *am.Event) bool {
	p := am.ParseArgs[A](e.Args).FocusPrimitive
	if p == nil {
		return false
	}

	b, _ := d.hBoxFromPrimitive(p)

	// skip when focus impossible
	return slices.Contains(d.focusable, b)
}

func (d *Debugger) AfterFocusState(e *am.Event) {
	afArgs := am.ParseArgs[A](e.Args)
	focused := afArgs.FocusPrimitive
	mouse := afArgs.MouseFocus
	focused.GetFocusable()

	d.Focused = focused
	if d.Mach.Not1(ss.DialogFocused) {
		d.preModalFocus = focused
	}

	// correct state from mouse focus
	if mouse {
		_, state := d.hBoxFromPrimitive(focused)
		d.Mach.EvAdd1(e, state, nil)
	}

	// update the log highlight on focus change
	if d.Mach.Is1(ss.TreeLogView) && d.Mach.Not1(ss.LogReaderFocused) {
		d.Mach.EvAdd1(e, ss.UpdateLogScheduled, nil)
	}

	d.hUpdateClientList()
	d.hUpdateStatusBar()
	d.hUpdateTimelines()
}

var _ = ss.ToolRain

func (d *Debugger) ToolRainState(e *am.Event) {
	if d.Mach.Is1(ss.MatrixRain) {
		d.Mach.EvAdd1(e, ss.TreeLogView, nil)
	} else {
		d.Mach.EvAdd(e, am.S{ss.MatrixRain, ss.TreeMatrixView}, nil)
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
var _ = am.StateAny

func (d *Debugger) AnyEnter(e *am.Event) bool {
	// always pass network traffic
	mach := d.Mach
	mut := e.Mutation()
	called := mut.CalledIndex(ss.Names())
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
		if !d.params.RaceDetector {
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
	go mach.PrependMut(mut.Clone())

	return false
}

// AnyState is a global final handler
var _ = am.StateAny

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

var _ = ss.WebReq

func (d *Debugger) WebReqState(e *am.Event) {
	wrArgs := am.ParseArgs[A](e.Args)
	r := wrArgs.HttpRequest
	w := wrArgs.HttpResponseWriter
	done := wrArgs.DoneChan
	defer close(done)

	uri := r.RequestURI
	switch {

	// diagram viewer
	case uri == "/":
		fallthrough
	case uri == "/diagrams/mach":
		html := string(amvis.HtmlDiagram)
		html = strings.ReplaceAll(html, "localhost:6831",
			d.listenHost+":"+d.httpPort)
		_, err := w.Write([]byte(html))
		d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)

	// default svg symlink
	case strings.HasPrefix(uri, "/diagrams/mach.svg"):
		svgPath := filepath.Join(d.params.OutputDir, "diagrams", "am-vis.svg")
		b, err := os.ReadFile(svgPath)
		d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)
		if err != nil {
			return
		}

		// send
		w.Header().Set("Content-Type", "image/svg+xml")
		_, err = w.Write(b)
		d.Mach.EvAddErrState(e, ss.ErrWeb, err, nil)
	}
}

type WsDiagMsg struct {
	Type  string
	Addr  *types.MachAddress
	Group string
}

var _ = ss.WebSocketDiag

func (d *Debugger) WebSocketDiagState(e *am.Event) {
	mach := d.Mach
	ctx := mach.NewStateCtx(ss.WebSocketDiag)
	wsdArgs := am.ParseArgs[A](e.Args)
	ws := wsdArgs.WebSocketConn
	r := wsdArgs.HttpRequest
	done := wsdArgs.DoneChan
	clientDone := make(chan struct{})

	mach.EvAdd1(e, ss.DiagramsScheduled, nil)

	// unblock
	mach.Fork(ctx, e, func() {
		defer close(done)
		var ctxReq context.Context
		var cancelReq context.CancelFunc
		for {
			// release prev run's wait chans
			if cancelReq != nil {
				cancelReq()
			}

			ctxReq, cancelReq = context.WithCancel(ctx)
			defer cancelReq()

			// wait for diagrams start
			select {
			case <-clientDone:
				return
			case <-mach.When1(ss.DiagramsScheduled, ctxReq):
			}

			// show progress
			select {

			case <-clientDone:
				return

			case <-mach.When1(ss.DiagramsNoCache, ctxReq):
				// msg
				msg, err := json.Marshal(WsDiagMsg{
					Type: "loading",
				})
				if err != nil {
					mach.Log(err.Error())
					continue
				}

				// send
				err = ws.Write(r.Context(), websocket.MessageText, msg)
				if err != nil {
					mach.EvAddErrState(e, ss.ErrWeb, err, nil)
					return
				}

			case <-mach.When1(ss.DiagramsReady, ctxReq):
				// ok
			}

			// wait for diagrams ready
			select {

			case <-clientDone:
				return

			// TODO loop over WSs in DiagramsReady. no goroutine per each
			case <-mach.When1(ss.DiagramsReady, ctxReq):
				// msg
				msg := WsDiagMsg{
					Type: "refresh",
					Addr: d.MachAddr(),
				}
				mach.Eval("WebSocketDiagState", func() {
					if d.params.OutputDiagGroup == types.ParamsOutDiagGroupSkip {
						msg.Group = d.C.SelectedGroup
					}
				}, r.Context())
				msgB, err := json.Marshal(msg)
				if err != nil {
					mach.Log(err.Error())
					continue
				}

				// send
				err = ws.Write(r.Context(), websocket.MessageText, msgB)
				if err != nil {
					mach.EvAddErrState(e, ss.ErrWeb, err, nil)
					return
				}
			}
		}
	})

	mach.Fork(ctx, e, func() {
		for {
			// msgType, msg, err := ws.Read(r.Context())
			_, _, err := ws.Read(r.Context())
			if err != nil {
				mach.Log("websocket closed")
				if websocket.CloseStatus(err) == -1 {
					err = fmt.Errorf("websocket read: %w", err)
					mach.EvAddErrState(e, ss.ErrWeb, err, nil)
				}

				// close up
				close(clientDone)
				return
			}
		}
	})
}

var _ = ss.FilterCanceledTx

func (d *Debugger) FilterCanceledTxEnd(e *am.Event) {
	// show empty when showing canceled
	d.Mach.EvRemove1(e, ss.FilterEmptyTx, nil)
}

var _ = ss.FilterQueuedTx

func (d *Debugger) FilterQueuedTxEnd(e *am.Event) {
	// show empty when showing queued
	d.Mach.EvRemove1(e, ss.FilterEmptyTx, nil)
}

var _ = ss.MatrixRainSelected

func (d *Debugger) MatrixRainSelectedState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.MatrixRainSelected)
	mrArgs := am.ParseArgs[A](e.Args)
	row := mrArgs.Row
	column := mrArgs.Column
	currTxRow := mrArgs.CurrTxRow
	c := d.C
	index := c.MsgStruct.StatesIndex
	// _, _, _, height := d.matrix.GetInnerRect()

	// select state name
	if column >= 0 && column < len(index) {
		d.Mach.EvAdd1(e, ss.StateNameSelected, Pass(&A{
			State: index[column],
		}))
	}

	// scroll to another?
	if row == currTxRow || row == -1 {
		return
	}

	diff := row - currTxRow
	idx := c.FilterIndexByCursor1(c.CursorTx1) + diff
	if idx == -1 {
		return
	}

	row = c.MsgTxsFiltered[idx] + 1

	// unblock
	d.Mach.Fork(ctx, e, func() {
		// scroll
		ok := amhelp.Add1Sync(ctx, d.Mach, ss.ScrollToTx, Pass(&A{
			CursorTx1:   row,
			TrimHistory: true,
		}))
		if ctx.Err() != nil {
			return // expired
		}
		if ok {
			return
		}

		// update layout
		d.Mach.Eval("MatrixRainSelectedState", func() {
			d.hUpdateMatrixRain()
			d.updateClientList()
			d.hRedrawFull(false)
		}, ctx)
	})
}

var _ = ss.Resized

func (d *Debugger) ResizedState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.Resized)

	d.lastResize = d.Mach.Time(nil).Sum(nil)
	d.hUpdateNarrowLayout()

	// rebuild log
	if d.Mach.Not1(ss.ClientSelected) {
		return
	}

	// TODO loose logRebuildEnd and include as relation
	d.Mach.EvAdd1(e, ss.BuildingLog, Pass(&A{
		LogRebuildEnd: len(d.C.MsgTxs),
	}))
	d.Mach.Fork(ctx, e, func() {
		<-d.Mach.When1(ss.LogBuilt, ctx)
		if ctx.Err() != nil {
			return // expired
		}
		// force a redraw TODO bug?
		d.draw()
	})
}

var _ = ss.SshServer

func (d *Debugger) SshServerEnter(e *am.Event) bool {
	return d.params.UiSsh && d.params.AddrSsh != ""
}

func (d *Debugger) SshServerState(e *am.Event) {
	ctx := d.Mach.NewStateCtx(ss.SshServer)
	// TODO SshClientState
	busy := atomic.Bool{}
	handler := func(sess ssh.Session) {
		d.Mach.Log("new SSH session " + sess.RemoteAddr().String())
		if busy.Load() {
			_, _ = sess.Write([]byte("am-dbg server busy...\n"))
			_ = sess.Close()
			return
		}
		// TODO prevent double conns via SshServerConnectedState

		_, _, isPty := sess.Pty()
		if !isPty {
			return
		}
		screen, err := NewSessionScreen(sess)
		if err != nil {
			d.Mach.EvAddErr(e, err, nil)
			return
		}
		d.App.SetScreen(screen)
		busy.Store(true)

		// TODO https://github.com/gliderlabs/ssh/issues/226
		sigCh := make(chan ssh.Signal, 1)
		sess.Signals(sigCh)
		defer close(sigCh)

		// wait till end
		select {
		case <-d.Mach.WhenTicks(ss.SshDisconn, 1, nil):
		case <-sigCh: // TODO
		case <-ctx.Done():
		}

		// restore sim screen
		busy.Store(false)
		d.App.SetScreen(tcell.NewSimulationScreen("UTF-8"))
	}

	d.Mach.Fork(ctx, e, func() {
		if ctx.Err() != nil {
			return // expired
		}
		optSrv := func(s *ssh.Server) error {
			d.sshSrv = s
			return nil
		}

		// show banner TODO optional
		_, port, _ := net.SplitHostPort(d.params.AddrSsh)
		p := d.params.Print
		p("SSH: listening on %s\n", d.params.AddrSsh)
		p("\n")
		p("Connect via:\n")
		p("$ ssh %s -p %s -o UserKnownHostsFile=/dev/null "+
			"-o StrictHostKeyChecking=no\n", d.listenHost, port)
		d.Mach.EvAddErr(
			e, ssh.ListenAndServe(d.params.AddrSsh, handler, optSrv), nil,
		)
	})
}

func (d *Debugger) SshServerEnd(e *am.Event) {
	d.sshSrv.Close()
}
