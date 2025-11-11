package frostdb

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestFrostdbTrack(t *testing.T) {
	if amhelp.IsTestRunner() {
		t.Skip("no experimental in test runner")
	}

	// Create memory storage
	ctx := context.Background()
	mem, err := NewMemory(ctx)
	require.NoError(t, err)

	// Create a machine with ss states
	mach := am.New(ctx, BasicSchema, &am.Opts{Id: "MyMach1"})
	onErr := func(err error) {
		t.Error(err)
	}

	// TODO NewCommon, DebugMachEnv
	tr, err := mem.Track(onErr, mach, nil, nil, 10^6)
	require.NoError(t, err)

	rounds := 500
	start := time.Now()

	for i := range rounds {
		if i%100 == 0 {
			t.Logf("i: %d", i)
		}
		mach.Toggle1(ssB.Start, nil)
	}
	t.Logf("elapsed1: %s", time.Since(start))

	// wait for the Tracer to finish
	require.Eventually(t, func() bool {
		return tr.Active.Load() == 0
	}, time.Second*10, time.Millisecond*100, "too slow")

	t.Logf("elapsed2: %s", time.Since(start))

	err = tr.db.Snapshot(ctx)
	require.NoError(t, err)
	err = tr.db.Close()
	require.NoError(t, err)

	// // Create a new query engine to retrieve data and print the results
	// engine := query.NewEngine(memory.DefaultAllocator,
	// 	database.TableProvider())
	// _ = engine.ScanTable("simple_table").
	// 	Project(logicalplan.DynCol("names")).
	// 	Filter(
	// 		logicalplan.Col("names.first_name").Eq(
	// 			logicalplan.Literal("Frederic")),
	// 	).Execute(context.Background(),
	// 		func(_ context.Context, r arrow.RecordBatch) error {
	// 	fmt.Println(r)
	// 	return nil
	// })
}
