package main

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/joho/godotenv"
	"github.com/pancsta/cview"

	"github.com/pancsta/asyncmachine-go/examples/tui/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var ss = states.TuiStates

func init() {
	_ = godotenv.Load()
}

func main() {
	ctx := context.Background()

	handlers := &tui{}
	mach, err := am.NewCommon(ctx, "tui", states.TuiSchema, ss.Names(),
		handlers, nil, nil)
	if err != nil {
		panic(err)
	}
	handlers.Mach = mach
	amhelp.MachDebugEnv(mach)
	mach.Add1(ss.Start, nil)
	<-mach.WhenNot1(ss.Start, nil)

	time.Sleep(time.Second)
}

// TUI

type tui struct {
	*am.ExceptionHandler

	Mach *am.Machine
	App  *cview.Application
	// UI is currently being drawn
	drawing atomic.Bool
	// repaintScheduled controls the UI paint debounce
	repaintScheduled atomic.Bool
	// repaintPending indicates a skipped repaint
	repaintPending atomic.Bool

	// go -race flag passed
	DbgRace bool
}

// handlers

func (t *tui) StartState(e *am.Event) {
	// cview TUI app
	t.App = cview.NewApplication()
	t.App.EnableMouse(true)

	// forceful race solving
	t.App.SetBeforeDrawFunc(func(_ tcell.Screen) bool {
		// dont draw while transitioning
		ok := t.Mach.Transition() == nil
		if !ok {
			// reschedule this repaint
			// d.Mach.Log("postpone draw")
			t.repaintPending.Store(true)
			return true
		}

		// mark as in progress
		t.drawing.Store(true)
		return false
	})
	t.App.SetAfterDrawFunc(func(_ tcell.Screen) {
		t.drawing.Store(false)
	})
	t.App.SetAfterResizeFunc(func(width int, height int) {
		go func() {
			time.Sleep(time.Millisecond * 300)
			t.Mach.Add1(ss.Resized, nil)
		}()
	})

	stateCtx := t.Mach.NewStateCtx(ss.Start)

	// draw in a goroutine
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}
		t.Mach.PanicToErr(nil)

		t.App.SetRoot(t.initUi(), true)
		err := t.App.Run()
		if err != nil {
			t.Mach.AddErr(err, nil)
		}

		t.Mach.Dispose()
	}()

	// post-start ops
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}

		// TODO
	}()
}

// AnyEnter prevents most of mutations during a UI redraw (and vice versa)
// forceful race solving
func (t *tui) AnyEnter(e *am.Event) bool {
	// always pass network traffic
	mach := t.Mach
	mut := e.Mutation()
	called := mut.CalledIndex(ss.Names())
	pass := am.S{ss.IncomingData, am.StateException}
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
		ok := !t.drawing.Load()
		if ok {
			// ok
			return true
		}

		// delay, but avoid the race detector which gets stuck here
		if !t.DbgRace {
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
func (t *tui) AnyState(e *am.Event) {
	tx := e.Transition()

	// redraw on auto states
	// TODO this should be done better
	if tx.IsAuto() && tx.IsAccepted.Load() {
		t.repaintPending.Store(false)
		t.draw()
	} else if t.repaintPending.Swap(false) {
		t.draw()
	}
}

// methods

func (t *tui) draw(components ...cview.Primitive) {
	if !t.repaintScheduled.CompareAndSwap(false, true) {
		return
	}

	go func() {
		select {
		case <-t.Mach.Ctx().Done():
			return

		// debounce every 16msec
		case <-time.After(16 * time.Millisecond):
		}

		t.App.QueueUpdateDraw(func() {
			t.repaintScheduled.Store(false)
		}, components...)
	}()
}

func (t *tui) initUi() cview.Primitive {
	newPrimitive := func(text string) cview.Primitive {
		tv := cview.NewTextView()
		tv.SetTextAlign(cview.AlignCenter)
		tv.SetText(text)
		return tv
	}
	menu := newPrimitive("Menu")
	main := newPrimitive("Main content")
	sideBar := newPrimitive("Side Bar")

	grid := cview.NewGrid()
	grid.SetRows(3, 0, 3)
	grid.SetColumns(30, 0, 30)
	grid.SetBorders(true)
	grid.AddItem(newPrimitive("Header"), 0, 0, 1, 3, 0, 0, false)
	grid.AddItem(newPrimitive("Footer"), 2, 0, 1, 3, 0, 0, false)

	// Layout for screens narrower than 100 cells (menu and side bar are hidden).
	grid.AddItem(menu, 0, 0, 0, 0, 0, 0, false)
	grid.AddItem(main, 1, 0, 1, 3, 0, 0, false)
	grid.AddItem(sideBar, 0, 0, 0, 0, 0, 0, false)

	// Layout for screens wider than 100 cells.
	grid.AddItem(menu, 1, 0, 1, 1, 0, 100, false)
	grid.AddItem(main, 1, 1, 1, 1, 0, 100, false)
	grid.AddItem(sideBar, 1, 2, 1, 1, 0, 100, false)

	return grid
}
