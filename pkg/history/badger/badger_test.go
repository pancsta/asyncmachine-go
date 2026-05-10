package badger

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	testhist "github.com/pancsta/asyncmachine-go/pkg/history/test"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

var ss = ssam.BasicStates

func TestBadgerRead(t *testing.T) {
	if amhelp.IsTestRunner() {
		t.Skip("skipping debug test")
	}

	// init memory
	db, err := NewDb("")
	require.NoError(t, err)
	defer db.Close()

	// list machines
	machines, err := ListMachines(db)
	require.NoError(t, err)
	for _, m := range machines {
		t.Logf("machine: %s nextId: %d", m.MachId, m.NextId)
	}
}

func TestBadgerTrack(t *testing.T) {
	os.RemoveAll("amhist.badger")
	debug := false
	// debug := true

	// configs
	rounds := 50
	onErr := func(err error) {
		t.Error(err)
	}
	cfg := Config{
		QueueBatch: 10,
		BaseConfig: amhist.BaseConfig{
			Log:           debug,
			TrackedStates: am.S{ss.Start},
			MaxRecords:    30,
		},
		EncJson: debug,
	}

	// init basic machine
	ctx := context.Background()
	mach := am.New(ctx, ssam.BasicSchema, &am.Opts{Id: "MyMach1"})

	// init memory
	db, err := NewDb("")
	require.NoError(t, err)
	defer db.Close()

	mem, err := NewMemory(ctx, db, mach, cfg, onErr)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, mem.Dispose())
	}()

	// common test
	testhist.AssertBasics(t, mem, rounds)

	// GC test
	mem.checkGc()
	testhist.AssertGc(t, mem, rounds)
}

func TestBadgerTrackMany(t *testing.T) {
	os.RemoveAll("amhist.badger")
	debug := false
	// debug := true

	// configs
	rounds := 50000
	onErr := func(err error) {
		t.Error(err)
	}
	cfg := Config{
		BaseConfig: amhist.BaseConfig{
			Log:           debug,
			TrackedStates: am.S{ss.Start},
			MaxRecords:    10e6,
		},
		EncJson: debug,
	}

	// init basic machine
	ctx := context.Background()
	mach := am.New(ctx, ssam.BasicSchema, &am.Opts{Id: "MyMach1"})

	// init memory
	db, err := NewDb("")
	require.NoError(t, err)
	defer db.Close()

	mem, err := NewMemory(ctx, db, mach, cfg, onErr)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, mem.Dispose())
	}()

	// common test
	testhist.AssertBasics(t, mem, rounds)
}
