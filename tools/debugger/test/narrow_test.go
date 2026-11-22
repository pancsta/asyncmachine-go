//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/require"

	amtest "github.com/pancsta/asyncmachine-go/internal/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/debugger"
	"github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

// TestNarrowClientListHidden checks that the client list, hidden by the narrow
// layout, is not repainted over the content by targeted repaints.
func TestNarrowClientListHidden(t *testing.T) {
	screen := tcell.NewSimulationScreen("utf8")
	require.NoError(t, screen.Init())
	screen.SetSize(140, 45)

	d, err := amtest.NewDbgWorker(false, types.Params{
		Screen:          screen,
		Id:              "narrow-client-list",
		OutputDir:       t.TempDir(),
		Version:         "test",
		ViewTheme:       "dark",
		EnableClipboard: false,
		Filters:         &types.Filters{LogLevel: am.LogChanges},
	})
	require.NoError(t, err)
	t.Cleanup(d.Mach.Dispose)

	require.Eventually(t, func() bool {
		return strings.Contains(narrowScreenText(screen), "Machines")
	}, 5*time.Second, 100*time.Millisecond, "client list not rendered")

	x, y, ok := narrowFindText(screen, "narrow")
	require.True(t, ok, "narrow toolbar item not found")

	// hide the client list
	narrowClick(t, d, x+1, y)
	narrowWait(t, d, ss.NarrowLayout)

	// wait for the narrow layout redraw to drop the list
	require.Eventually(t, func() bool {
		return !strings.Contains(narrowScreenText(screen), "Machines")
	}, 3*time.Second, 100*time.Millisecond,
		"client list still rendered after narrow layout")

	// background list updates must not draw the hidden list over the content
	require.Never(t, func() bool {
		return strings.Contains(narrowScreenText(screen), "Machines")
	}, 4*time.Second, 250*time.Millisecond,
		"client list rendered over the content in narrow layout")

	// restore it
	narrowClick(t, d, x+1, y)
	narrowWait(t, d, ss.ClientListVisible)

	require.Eventually(t, func() bool {
		return strings.Contains(narrowScreenText(screen), "Machines")
	}, 3*time.Second, 100*time.Millisecond,
		"client list not restored after leaving narrow layout")
}

func narrowWait(t *testing.T, d *debugger.Debugger, state string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-d.Mach.When1(state, ctx):
	case <-ctx.Done():
		t.Fatalf("state %s not active, got %s", state,
			d.Mach.Switch(states.DebuggerGroups.Focused))
	}
}

func narrowClick(t *testing.T, d *debugger.Debugger, x, y int) {
	t.Helper()

	d.App.QueueEvent(tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone))
	time.Sleep(30 * time.Millisecond)
	d.App.QueueEvent(tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone))
}

func narrowScreenText(s tcell.SimulationScreen) string {
	cells, w, h := s.GetContents()
	var b strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			if len(c.Runes) == 0 {
				b.WriteByte(' ')
				continue
			}
			b.WriteRune(c.Runes[0])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func narrowFindText(s tcell.SimulationScreen, needle string) (int, int, bool) {
	cells, w, h := s.GetContents()
	n := []rune(needle)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			match := true
			for i, r := range n {
				if x+i >= w {
					match = false
					break
				}
				c := cells[y*w+x+i]
				if len(c.Runes) == 0 || c.Runes[0] != r {
					match = false
					break
				}
			}
			if match {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}
