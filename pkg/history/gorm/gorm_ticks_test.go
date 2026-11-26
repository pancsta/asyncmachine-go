package gorm

import (
	"encoding/json"
	"path/filepath"
	"testing"

	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// TestMemorySparseTicks asserts that only changed state ticks are stored and
// that all state conditions still match records with skipped ticks.
func TestMemorySparseTicks(t *testing.T) {
	ctx := t.Context()
	db, sqlDb, err := NewDb(filepath.Join(t.TempDir(), "ticks"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDb.Close() })
	sqlDb.SetMaxOpenConns(1)

	mach := am.New(ctx, am.Schema{"A": {}, "B": {}}, &am.Opts{Id: "ticks"})
	t.Cleanup(mach.Dispose)
	mem, err := NewMemory(ctx, db, mach, Config{}, func(err error) {
		t.Errorf("history: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mem.Dispose() })

	add := func(state string) {
		if res := mach.Add1(state, nil); res >= am.Queued {
			<-mach.WhenQueue(res)
		}
	}
	remove := func(state string) {
		if res := mach.Remove1(state, nil); res >= am.Queued {
			<-mach.WhenQueue(res)
		}
	}

	add("A")    // 1: baseline, all tracked states
	add("B")    // 2: only B changed
	remove("A") // 3: only A changed

	if err := mem.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	var timesN, ticksN int64
	if err := db.Model(&Time{}).Count(&timesN).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Tick{}).Count(&ticksN).Error; err != nil {
		t.Fatal(err)
	}
	// A is the only active state at the baseline (no inactive baseline rows),
	// then one changed state per transition
	if timesN != 3 || ticksN != 3 {
		t.Fatalf("times=%d ticks=%d, want 3/3", timesN, ticksN)
	}

	find := func(q amhist.Query, want int) {
		t.Helper()
		recs, err := mem.FindLatest(ctx, false, 10, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(recs) != want {
			t.Fatalf("%+v: %d records, want %d", q, len(recs), want)
		}
	}

	// records 1 and 2 have A active, record 3 is the only inactive one
	find(amhist.Query{Active: am.S{"A"}}, 2)
	find(amhist.Query{Inactive: am.S{"A"}}, 1)
	// B is active in records 2 and 3, inactive in record 1
	find(amhist.Query{Active: am.S{"B"}}, 2)
	find(amhist.Query{Inactive: am.S{"B"}}, 1)
	// only changed ticks have rows
	find(amhist.Query{Activated: am.S{"B"}}, 1)
	find(amhist.Query{Deactivated: am.S{"A"}}, 1)
	// mixed clock and tick conditions
	find(amhist.Query{Active: am.S{"A"}, Activated: am.S{"B"}}, 1)
	find(amhist.Query{Start: amhist.ConditionTime{
		MTimeStates: am.S{"A"}, MTime: am.Time{2},
	}}, 1)
	find(amhist.Query{End: amhist.ConditionTime{
		MTimeStates: am.S{"A"}, MTime: am.Time{1},
	}}, 2)

	if got := mem.LatestActivated(ctx, "A", "B"); got != "B" {
		t.Fatalf("latest activated = %q, want B", got)
	}
	if got := mem.LatestDeactivated(ctx, "A", "B"); got != "A" {
		t.Fatalf("latest deactivated = %q, want A", got)
	}
}

// TestTransitionDiffClamp asserts that regressing clocks don't underflow the
// uint64 diffs, which sqlite can't bind.
func TestTransitionDiffClamp(t *testing.T) {
	ctx := t.Context()
	db, sqlDb, err := NewDb(filepath.Join(t.TempDir(), "clamp"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDb.Close() })
	sqlDb.SetMaxOpenConns(1)

	mach := am.New(ctx, am.Schema{"A": {}, "B": {}}, &am.Opts{Id: "clamp"})
	t.Cleanup(mach.Dispose)
	mem, err := NewMemory(ctx, db, mach, Config{}, func(err error) {
		t.Errorf("history: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mem.Dispose() })

	names := len(mach.StateNames())
	newTx := func(before, after am.Time) *am.Transition {
		tx := &am.Transition{
			Id:         "tx",
			TimeBefore: before,
			TimeAfter:  after,
			MachApi:    mach,
			Mutation:   &am.Mutation{Called: []int{1}},
		}
		tx.IsAccepted.Store(true)
		if len(before) != names || len(after) != names {
			t.Fatalf("unexpected clocks: %v %v", before, after)
		}
		return tx
	}

	// normal transition: A activated
	mem.tr.TransitionEnd(newTx(
		am.Time{0, 0, 0},
		am.Time{0, 1, 0},
	))
	// regressing clock: A back to 0
	mem.tr.TransitionEnd(newTx(
		am.Time{0, 1, 0},
		am.Time{0, 0, 0},
	))

	if len(mem.queue.times) != 2 {
		t.Fatalf("queued records = %d, want 2", len(mem.queue.times))
	}
	if rec := mem.queue.times[0]; rec.MTimeDiffSum != 1 ||
		rec.MTimeTrackedDiffSum != 1 {

		t.Fatalf("unexpected first record diffs: %+v", rec)
	}
	rec := mem.queue.times[1]
	if rec.MTimeDiffSum != 0 || rec.MTimeTrackedDiffSum != 0 ||
		rec.MTimeRecordDiffSum != 0 {

		t.Fatalf("unclamped diffs: diff=%d tracked=%d record=%d",
			rec.MTimeDiffSum, rec.MTimeTrackedDiffSum, rec.MTimeRecordDiffSum)
	}
	var diff am.Time
	if err := json.Unmarshal(rec.MTimeTrackedDiff, &diff); err != nil {
		t.Fatal(err)
	}
	for i, tick := range diff {
		if tick != 0 {
			t.Fatalf("unclamped tracked diff at %d: %d", i, tick)
		}
	}
}
