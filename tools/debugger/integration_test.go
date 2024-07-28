package debugger

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/stretchr/testify/assert"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/pkg/x/helpers"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func init() {
	// DEBUG
	// os.Setenv("AM_DBG_URL", "localhost:9913")
	// os.Setenv("AM_LOG", "2")
}

// NewTest creates a new debugger for testing.
func NewTest(t *testing.T) *Debugger {
	// mock screen
	sscreen := tcell.NewSimulationScreen("utf8")
	sscreen.SetSize(100, 50)
	sscreen.Clear()

	// fixtures
	dirPrefix := ""
	wd, err := os.Getwd()
	assert.NoError(t, err)
	if !strings.Contains(wd, "tools/debugger") {
		err = os.Chdir("../../")
		assert.NoError(t, err)
		dirPrefix = "tools/debugger/"
	}

	// create a debugger
	dbg, err := New(context.TODO(), Opts{
		ImportData:  dirPrefix + "testdata/am-dbg-sim.gob.br",
		DBGLogLevel: helpers.EnvLogLevel("AM_LOG"),
		Screen:      sscreen,
		ID:          t.Name(),
	})
	if err != nil {
		assert.NoError(t, err)
	}

	// debug tests with am-dbg via AM_DBG_URL (NOT parallel)
	debugURL := os.Getenv("AM_DBG_URL")
	if debugURL != "" {
		err = telemetry.TransitionsToDBG(dbg.Mach, debugURL)
		assert.NoError(t, err)
	}

	// start at the same place
	res := dbg.Mach.Add1(ss.Start, am.A{
		"Client.id":       "ps-2",
		"Client.cursorTx": 20,
	})
	assert.NotEqual(t, res, am.Canceled)
	<-dbg.Mach.When1(ss.Ready, nil)
	<-dbg.Mach.WhenNot1(ss.ScrollToTx, nil)

	dbg.Mach.Log("NewTest.Ready")

	return dbg
}

func (d *Debugger) AfterMulti(state string) {
	// TODO support when active
	when := d.Mach.WhenTicks(state, 2, nil)
	d.Mach.Add1(ss.UserFwdStep, nil)
	<-when
}

// ///// ///// /////
// ///// TESTS
// ///// ///// /////

func TestUserFwd(t *testing.T) {
	t.Parallel()
	d := NewTest(t)

	cursor := d.C.CursorTx
	when := d.Mach.WhenTicks(ss.UserFwd, 1, nil)
	res := d.Mach.Add1(ss.UserFwd, nil)

	<-when
	assert.NotEqual(t, res, am.Canceled)
	assert.Equal(t, cursor+1, d.C.CursorTx)

	d.Dispose()
}

func TestUserFwd100(t *testing.T) {
	t.Parallel()
	d := NewTest(t)
	d.ShowClientTx("sim", 20)

	cursor := d.C.CursorTx
	ticks := d.Mach.Clock(ss.UserFwd)

	// add ss.UserFwd 100 times in a series
	for i := 0; i < 100; i++ {
		res := d.Mach.Add1(ss.UserFwd, nil)
		<-d.Mach.WhenTime(am.S{ss.UserFwd}, am.T{ticks + 2 + uint64(i)*2}, nil)
		if res == am.Canceled {
			t.Fatal("Canceled")
		}
	}

	assert.Equal(t, cursor+100, d.C.CursorTx)

	d.Dispose()
}

func TestUserBack(t *testing.T) {
	t.Parallel()
	d := NewTest(t)

	cursor := d.C.CursorTx
	when := d.Mach.WhenTicks(ss.UserBack, 1, nil)
	res := d.Mach.Add1(ss.UserBack, nil)

	<-when
	assert.NotEqual(t, res, am.Canceled)
	assert.Equal(t, cursor-1, d.C.CursorTx)

	d.Dispose()
}

func TestStepsResetAfterStateJump(t *testing.T) {
	t.Parallel()
	d := NewTest(t)

	d.Mach.Add1(ss.StateNameSelected, am.A{
		"selectedStateName": "PublishMessage",
	})
	d.AfterMulti(ss.UserFwdStep)
	d.AfterMulti(ss.UserFwdStep)

	whenScroll := d.Mach.WhenTicks(ss.ScrollToTx, 2, nil)
	d.ScrollToStateTx(d.C.selectedState, true)
	<-whenScroll

	assert.Equal(t, 0, d.C.CursorStep, "Steps timeline should reset")

	d.Dispose()
}
