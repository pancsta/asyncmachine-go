package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	amss "github.com/pancsta/asyncmachine-go/pkg/states"
)

func TestTrack(t *testing.T) {
	// configs
	rounds := 50
	onErr := func(err error) {
		t.Error(err)
	}
	cfg := amhist.Config{
		TrackedStates: am.S{ss.Start},
	}

	// init machine and history
	ctx := context.Background()
	mach := am.New(ctx, amss.BasicSchema, &am.Opts{Id: "MyMach1"})
	mem, err := amhist.NewMemory(ctx, nil, mach, cfg, onErr)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, mem.Dispose())
	}()

	// common test
	AssertBasics(t, mem, rounds)
}

func TestTrackMany(t *testing.T) {
	// configs
	onErr := func(err error) {
		t.Error(err)
	}
	cfg := amhist.Config{
		TrackedStates: am.S{ss.Start},
		MaxRecords:    10e6,
	}

	// init machine and history
	ctx := context.Background()
	mach := am.New(ctx, amss.BasicSchema, &am.Opts{Id: "MyMach1"})
	mem, err := amhist.NewMemory(ctx, nil, mach, cfg, onErr)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, mem.Dispose())
	}()

	// common test
	rounds := 50_000
	AssertBasics(t, mem, rounds)
}
