package test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	amss "github.com/pancsta/asyncmachine-go/pkg/states"
)

var ss = amss.BasicStates

func AssertBasics(t *testing.T, mem amhist.MemoryApi, rounds int) {

	ctx := context.Background()
	mach := mem.Machine()
	start := time.Now()

	// validate
	require.True(t, mach.Has(ss.Names()), "Machine has to implement BasicStates")
	require.True(t, mem.IsTracked1(ss.Start), "Start should be tracked")
	require.False(t, mem.IsTracked1(ss.Ready), "Ready should not be tracked")

	t.Logf("rounds: %d", rounds)

	// mutate
	for range rounds {
		mach.Toggle1(ss.Start, nil)
	}

	t.Logf("mach: %s", time.Since(start))

	require.NoError(t, mem.Sync())

	t.Logf("db: %s", time.Since(start))

	// check conditions
	// now := time.Now().UTC()
	// require.True(t, mem.ActivatedBetween(ctx, ss.Start, start.UTC(), now),
	// 	"Start was activated")
	// require.False(t, mem.ActivatedBetween(ctx, ss.Ready, start.UTC(), now),
	// 	"Ready isn't tracked")

	// machine record
	machRec := mem.MachineRecord()
	require.NotNil(t, machRec, "Machine record is not nil")

	// many rows, no condition
	latest, err := mem.FindLatest(ctx, false, 25, amhist.Query{})
	// sum := []int{}
	// for _, r := range latest {
	// 	sum = append(sum, int(r.Time.MTimeSum))
	// }
	// print(sum)
	require.NoError(t, err)
	require.Len(t, latest, 25, "25 rows returned")
	require.Equal(t, int(machRec.NextId)-24, int(latest[23].Time.MTimeSum),
		"time sum matches")
	require.Equal(t, int(machRec.NextId)-25, int(latest[24].Time.MTimeSum),
		"time sum matches")

	t.Logf("query: %s", time.Since(start))
}

func AssertGc(t *testing.T, mem amhist.MemoryApi, rounds int) {
	ctx := context.Background()

	// GC
	all, err := mem.FindLatest(ctx, false, mem.Config().MaxRecords*2,
		amhist.Query{})
	require.NoError(t, err)
	require.LessOrEqual(t, len(all), mem.Config().MaxRecords,
		"max records respected")
}

// TODO test resume
// TODO test schema
// TODO test parallel tracking
// TODO test rpc tracking in /pkg/rpc
// TODO test double tracking
// TODO test config
// TODO test restore
// TODO test transition
// TODO test transition queries
