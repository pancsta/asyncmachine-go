package gorm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

const benchTicksRecords = 20000

var benchTicksStates = am.S{"A", "B", "C", "D"}

// BenchmarkFindLatestTicks compares the query cost of the old full tick layout
// with the sparse tick layout, using the same result builder.
func BenchmarkFindLatestTicks(b *testing.B) {
	full, fullRows := seedTicksBench(b, false)
	sparse, sparseRows := seedTicksBench(b, true)

	b.Logf("rows: full=%d sparse=%d", fullRows, sparseRows)

	b.Run("full/active", func(b *testing.B) {
		stateID := full.cacheDbIdxs["A"]
		matcher := func(_ *am.TimeIndex, q *gorm.DB) *gorm.DB {
			return q.Joins(utilsJoin("A")).
				Where("A.state_id = ? AND A.active = ?", stateID, true).
				Order("A.time_id DESC")
		}
		runMatch(b, full, matcher)
	})

	b.Run("full/activated", func(b *testing.B) {
		stateID := full.cacheDbIdxs["A"]
		matcher := func(_ *am.TimeIndex, q *gorm.DB) *gorm.DB {
			return q.Joins(utilsJoin("A")).
				Where("A.state_id = ? AND A.activated = ?", stateID, true).
				Order("A.time_id DESC")
		}
		runMatch(b, full, matcher)
	})

	b.Run("sparse/active", func(b *testing.B) {
		runFind(b, sparse, amhist.Query{Active: am.S{"A"}})
	})

	b.Run("sparse/inactive", func(b *testing.B) {
		runFind(b, sparse, amhist.Query{Inactive: am.S{"A"}})
	})

	b.Run("sparse/activated", func(b *testing.B) {
		runFind(b, sparse, amhist.Query{Activated: am.S{"A"}})
	})

	b.Run("sparse/mixed", func(b *testing.B) {
		runFind(b, sparse, amhist.Query{
			Active:    am.S{"A"},
			Activated: am.S{"B"},
		})
	})
}

// seedTicksBench creates a DB with synthetic records, either with all state
// ticks per record (old) or only changed ones (new).
func seedTicksBench(b *testing.B, sparse bool) (*Memory, int64) {
	b.Helper()
	ctx := context.Background()

	name := filepath.Join(b.TempDir(), "ticks")
	db, sqlDb, err := NewDb(name, false)
	if err != nil {
		b.Fatal(err)
	}
	sqlDb.SetMaxOpenConns(1)
	b.Cleanup(func() { _ = sqlDb.Close() })

	mach := am.New(ctx, am.Schema{"A": {}, "B": {}, "C": {}, "D": {}},
		&am.Opts{Id: "bench-" + name})
	b.Cleanup(mach.Dispose)
	mem, err := NewMemory(ctx, db, mach, Config{
		BaseConfig: amhist.BaseConfig{TrackedStates: benchTicksStates},
	}, func(err error) { b.Error(err) })
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = mem.Dispose() })

	machID := mem.machRec.ID
	stateIDs := make([]uint, len(benchTicksStates))
	for i, state := range benchTicksStates {
		stateIDs[i] = mem.cacheDbIdxs[state]
	}

	clock := make(am.Time, len(benchTicksStates))
	changed := make([]bool, len(benchTicksStates))
	now := time.Now().UTC()
	times := make([]Time, 0, benchTicksRecords)
	ticks := make([]Tick, 0, benchTicksRecords*len(benchTicksStates))

	for i := 0; i < benchTicksRecords; i++ {
		for j := range changed {
			changed[j] = false
		}
		if i > 0 {
			idx := i % len(benchTicksStates)
			clock[idx]++
			changed[idx] = true
		}

		mTime, err := json.Marshal(clock)
		if err != nil {
			b.Fatal(err)
		}
		times = append(times, Time{
			ID:               uint64(i + 1),
			MachineID:        machID,
			MTimeSum:         clock.Sum(nil),
			HTime:            now,
			MTimeTracked:     datatypes.JSON(mTime),
			MTimeTrackedDiff: datatypes.JSON("[]"),
			MachTick:         uint32(i),
		})

		for s := range stateIDs {
			if sparse && i > 0 && !changed[s] {
				continue
			}
			active := clock[s]%2 == 1
			tick := Tick{
				TimeID:    uint64(i + 1),
				MachineID: machID,
				StateID:   stateIDs[s],
				Tick:      clock[s],
				Active:    active,
			}
			if changed[s] && active {
				tick.Activated = true
			}
			if changed[s] && !active {
				tick.Deactivated = true
			}
			ticks = append(ticks, tick)
		}
	}

	if err := db.CreateInBatches(&times, 1000).Error; err != nil {
		b.Fatal(err)
	}
	if err := db.Omit("State").CreateInBatches(&ticks, 1000).Error; err != nil {
		b.Fatal(err)
	}
	if err := mem.Sync(ctx); err != nil {
		b.Fatal(err)
	}

	var rows int64
	if err := db.Model(&Tick{}).Count(&rows).Error; err != nil {
		b.Fatal(err)
	}
	// include the WAL in the on-disk size
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		b.Fatal(err)
	}
	if fi, err := os.Stat(name + ".sqlite"); err == nil {
		b.Logf("%s: tick rows=%d db=%dKB", map[bool]string{
			true: "sparse", false: "full",
		}[sparse], rows, fi.Size()/1024)
	}

	return mem, rows
}

// utilsJoin mimics [Memory.JoinState] without a state ID condition.
func utilsJoin(name string) string {
	return "\n\t\tJOIN ticks " + name +
		"\n\t\t\tON " + name + ".time_id = times.id" +
		"\n\t\t\tAND " + name + ".machine_id = times.machine_id\n\t"
}

func runMatch(b *testing.B, mem *Memory, matcher MatcherFn) {
	b.Helper()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := mem.Match(ctx, 100, matcher); err != nil {
			b.Fatal(err)
		}
	}
}

func runFind(b *testing.B, mem *Memory, q amhist.Query) {
	b.Helper()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := mem.FindLatest(ctx, false, 100, q); err != nil {
			b.Fatal(err)
		}
	}
}
