package bbolt

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	testhist "github.com/pancsta/asyncmachine-go/pkg/history/test"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

var ss = ssam.BasicStates

func TestBboltRead(t *testing.T) {
	if amhelp.IsTestRunner() {
		t.Skip("skipping debug test")
	}

	// init memory
	db, err := NewDb("")
	require.NoError(t, err)

	// list
	// require.NoError(t, db.View(func(tx *bbolt.Tx) error {
	// 	b := tx.Bucket([]byte("MyMach1"))
	// 	bTime := b.Bucket([]byte(BuckTimes))
	// 	c := bTime.Cursor()
	// 	for k, v := c.First(); k != nil; k, v = c.Next() {
	// 		t.Logf("key: %s, value: %s", k, v)
	// 	}
	// 	return nil
	// }))

	// single
	require.NoError(t, db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("MyMach1"))
		bTime := b.Bucket([]byte(BuckTimes))
		t.Logf("key: %d, value: %s", 5, bTime.Get(itob(5)))
		return nil
	}))
}

func TestBboltTrack(t *testing.T) {
	os.Remove("amhist.db")
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
	defer func() {
		t.Logf("write time: %v", db.Stats().TxStats.WriteTime)
	}()

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

func TestBboltTrackMany(t *testing.T) {
	os.Remove("amhist.db")
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
	defer func() {
		t.Logf("write time: %v", db.Stats().TxStats.WriteTime)
	}()

	mem, err := NewMemory(ctx, db, mach, cfg, onErr)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, mem.Dispose())
	}()

	// common test
	testhist.AssertBasics(t, mem, rounds)
}
