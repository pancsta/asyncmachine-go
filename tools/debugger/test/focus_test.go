//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/require"

	amutils "github.com/pancsta/asyncmachine-go/internal/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

// newFocusWorker starts a local headless debugger worker with the passed
// params. It uses no imported data, so the layout and focus order are
// deterministic.
func newFocusWorker(t *testing.T, p types.Params) *debugger.Debugger {
	t.Helper()

	screen := tcell.NewSimulationScreen("utf8")
	require.NoError(t, screen.Init())
	screen.SetSize(100, 50)

	p.Screen = screen
	p.Id = "focus-" + amutils.RandId(0)
	p.OutputDir = t.TempDir()
	p.Version = "test"
	p.ViewTheme = "dark"
	p.EnableClipboard = false
	if p.Filters == nil {
		p.Filters = &types.Filters{LogLevel: am.LogChanges}
	}

	d, err := debugger.New(context.Background(), p)
	require.NoError(t, err)
	t.Cleanup(d.Mach.Dispose)

	d.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-d.Mach.When1(ss.Ready, ctx):
	case <-ctx.Done():
		t.Fatal("debugger not ready")
	}

	// wait for the Ready state handler (and its focus changes) to finish
	require.True(t, d.Mach.Eval("focusTestSync", func() {}, nil),
		"machine eval timeout")

	return d
}

// setFocus activates the passed focus state and syncs the cview focus.
func setFocus(t *testing.T, d *debugger.Debugger, state string) {
	t.Helper()

	// the startup transition may still apply its default focus, so retry
	// until the requested state sticks
	deadline := time.Now().Add(2 * time.Second)
	for {
		d.Mach.Add1(state, nil)
		d.Mach.Add1(ss.UpdateFocus, nil)

		if d.Mach.Is1(state) {
			return
		}
		if time.Now().After(deadline) {
			require.FailNowf(t, "focus state not active",
				"want %s, got %s", state,
				d.Mach.Switch(states.DebuggerGroups.Focused))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// assertFocusSeq walks the focus order with Tab and asserts each step.
func assertFocusSeq(
	t *testing.T, d *debugger.Debugger, start string, want []string,
) {
	t.Helper()
	setFocus(t, d, start)

	for _, wantState := range want {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		when := d.Mach.When1(wantState, ctx)

		res := d.Mach.Add1(ss.FocusNext, nil)
		if res == am.Canceled {
			cancel()
			t.Fatalf("FocusNext canceled, want %s", wantState)
		}

		select {
		case <-when:
			cancel()
		case <-ctx.Done():
			cancel()
			t.Fatalf("want %s, got %s", wantState,
				d.Mach.Switch(states.DebuggerGroups.Focused))
		}
	}
}

func TestFocusOrderWide(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewReader:    true,
		ViewTimelines: types.ParamsViewTimelinesTwo,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})

	assertFocusSeq(t, d, ss.ClientListFocused, []string{
		ss.TreeGroupsFocused,
		ss.TreeFocused,
		ss.LogFocused,
		ss.LogReaderFocused,
		ss.TimelineTxsFocused,
		ss.TimelineMarkersFocused,
		ss.Toolbar1Focused,
		ss.Toolbar2Focused,
		ss.Toolbar3Focused,
		ss.Toolbar4Focused,
		ss.AddressFocused,
	})
}

func TestFocusOrderNarrow(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewReader:    true,
		ViewNarrow:    true,
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})

	assertFocusSeq(t, d, ss.TreeGroupsFocused, []string{
		ss.TreeFocused,
		ss.LogFocused,
		ss.LogReaderFocused,
		ss.TimelineTxsFocused,
		ss.Toolbar1Focused,
		ss.Toolbar2Focused,
		ss.Toolbar3Focused,
		ss.Toolbar4Focused,
		ss.AddressFocused,
	})
}

func TestFocusOrderNoLog(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewReader:    true,
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogNothing},
	})

	assertFocusSeq(t, d, ss.ClientListFocused, []string{
		ss.TreeGroupsFocused,
		ss.TreeFocused,
		ss.LogReaderFocused,
		ss.TimelineTxsFocused,
		ss.Toolbar1Focused,
		ss.Toolbar2Focused,
		ss.Toolbar3Focused,
		ss.Toolbar4Focused,
		ss.AddressFocused,
	})
}

func TestFocusOrderNoReader(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewReader:    false,
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})

	assertFocusSeq(t, d, ss.ClientListFocused, []string{
		ss.TreeGroupsFocused,
		ss.TreeFocused,
		ss.LogFocused,
		ss.TimelineTxsFocused,
		ss.Toolbar1Focused,
		ss.Toolbar2Focused,
		ss.Toolbar3Focused,
		ss.Toolbar4Focused,
		ss.AddressFocused,
	})
}

func TestFocusOrderNoTimelines(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewReader:    true,
		ViewTimelines: types.ParamsViewTimelinesNone,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})

	assertFocusSeq(t, d, ss.ClientListFocused, []string{
		ss.TreeGroupsFocused,
		ss.TreeFocused,
		ss.LogFocused,
		ss.LogReaderFocused,
		ss.Toolbar1Focused,
		ss.Toolbar2Focused,
		ss.Toolbar3Focused,
		ss.Toolbar4Focused,
		ss.AddressFocused,
	})
}

func TestFocusOrderMatrix(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		StartupView:   "matrix",
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogNothing},
	})

	assertFocusSeq(t, d, ss.ClientListFocused, []string{
		ss.MatrixFocused,
		ss.TimelineTxsFocused,
		ss.Toolbar1Focused,
		ss.Toolbar2Focused,
		ss.Toolbar3Focused,
		ss.Toolbar4Focused,
		ss.AddressFocused,
	})
}

func TestFocusOrderTreeMatrix(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		StartupView:   "tree-matrix",
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})

	assertFocusSeq(t, d, ss.ClientListFocused, []string{
		ss.TreeGroupsFocused,
		ss.TreeFocused,
		ss.MatrixFocused,
		ss.TimelineTxsFocused,
		ss.Toolbar1Focused,
		ss.Toolbar2Focused,
		ss.Toolbar3Focused,
		ss.Toolbar4Focused,
		ss.AddressFocused,
	})
}

func TestFocusLogHidden(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewReader:    true,
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})
	setFocus(t, d, ss.LogFocused)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	when := d.Mach.When1(ss.LogReaderFocused, ctx)

	// hide the log pane; focus should move to the reader
	d.SetFilterLogLevel(am.LogNothing)

	select {
	case <-when:
	case <-ctx.Done():
		t.Fatalf("want %s, got %s", ss.LogReaderFocused,
			d.Mach.Switch(states.DebuggerGroups.Focused))
	}
}

func TestFocusNarrowEntry(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})
	setFocus(t, d, ss.ClientListFocused)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	when := d.Mach.When1(ss.TreeGroupsFocused, ctx)

	// hide the client list; focus should move to the first content panel
	d.Mach.Add1(ss.NarrowLayout, nil)

	select {
	case <-when:
	case <-ctx.Done():
		t.Fatalf("want %s, got %s", ss.TreeGroupsFocused,
			d.Mach.Switch(states.DebuggerGroups.Focused))
	}
}

func TestFocusTimelineHidden(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})
	setFocus(t, d, ss.TimelineTxsFocused)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	when := d.Mach.When1(ss.Toolbar1Focused, ctx)

	// hiding the last timeline should move focus to the first toolbar
	d.Mach.Add1(ss.TimelineTxHidden, nil)

	select {
	case <-when:
	case <-ctx.Done():
		t.Fatalf("want %s, got %s", ss.Toolbar1Focused,
			d.Mach.Switch(states.DebuggerGroups.Focused))
	}
}

func TestFocusViewSwitch(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})
	setFocus(t, d, ss.TreeFocused)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	when := d.Mach.When1(ss.MatrixFocused, ctx)

	// switching to the matrix view should focus the matrix
	d.Mach.Add1(ss.MatrixView, nil)

	select {
	case <-when:
	case <-ctx.Done():
		t.Fatalf("want %s, got %s", ss.MatrixFocused,
			d.Mach.Switch(states.DebuggerGroups.Focused))
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	whenTree := d.Mach.When1(ss.TreeFocused, ctx2)

	// switching back should focus the tree
	d.Mach.Add1(ss.TreeLogView, nil)

	select {
	case <-whenTree:
	case <-ctx2.Done():
		t.Fatalf("want %s, got %s", ss.TreeFocused,
			d.Mach.Switch(states.DebuggerGroups.Focused))
	}
}

func TestFocusReaderClosed(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewReader:    true,
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})
	setFocus(t, d, ss.LogReaderFocused)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	when := d.Mach.When1(ss.LogFocused, ctx)

	// closing the reader should focus the log
	d.Mach.Remove1(ss.LogReaderEnabled, nil)

	select {
	case <-when:
	case <-ctx.Done():
		t.Fatalf("want %s, got %s", ss.LogFocused,
			d.Mach.Switch(states.DebuggerGroups.Focused))
	}
}

// screenText returns the current TUI screen buffer.
func screenText(d *debugger.Debugger) string {
	var text string
	if !d.Mach.Eval("screenText", func() {
		text = d.App.ScreenText()
	}, nil) {
		return ""
	}

	return text
}

// clientListFocused checks if the client list border is rendered as focused.
func clientListFocused(d *debugger.Debugger) bool {
	for _, line := range strings.Split(screenText(d), "\n") {
		if strings.Contains(line, "Machines") {
			return strings.HasPrefix(line, "╔")
		}
	}

	return false
}

func TestFocusRedraw(t *testing.T) {
	d := newFocusWorker(t, types.Params{
		ViewTimelines: types.ParamsViewTimelinesOne,
		Filters:       &types.Filters{LogLevel: am.LogChanges},
	})
	setFocus(t, d, ss.ClientListFocused)

	require.Eventually(t, func() bool {
		return clientListFocused(d)
	}, 2*time.Second, 10*time.Millisecond, "client list focus not rendered")

	// let the initial client list debounce redraw settle (1s)
	time.Sleep(1100 * time.Millisecond)

	// Tab to the tree groups; the old client list highlight must be cleared
	// without any other event
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	when := d.Mach.When1(ss.TreeGroupsFocused, ctx)
	d.App.QueueEvent(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
	select {
	case <-when:
	case <-ctx.Done():
		t.Fatal("tab did not move focus")
	}

	require.Eventually(t, func() bool {
		return !clientListFocused(d)
	}, 2*time.Second, 10*time.Millisecond, "stale client list highlight")

	// Shift+Tab back; the client list highlight must return
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	whenBack := d.Mach.When1(ss.ClientListFocused, ctx2)
	d.App.QueueEvent(tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone))
	select {
	case <-whenBack:
	case <-ctx2.Done():
		t.Fatal("shift+tab did not move focus")
	}

	require.Eventually(t, func() bool {
		return clientListFocused(d)
	}, 2*time.Second, 10*time.Millisecond, "client list focus not redrawn")
}
