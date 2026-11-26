package ark

import (
	"testing"

	"github.com/mlange-42/ark/ecs"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type worldComp struct {
	Val int
}

// TestNewExistingWorld asserts that NewWithWorld reuses an injected world and
// leaves custom entities intact.
func TestNewExistingWorld(t *testing.T) {
	parent := am.New(t.Context(), nil, &am.Opts{Id: "srv-ark-world"})
	t.Cleanup(parent.Dispose)

	world := ecs.NewWorld()
	customMap := ecs.NewMap1[worldComp](world)
	custom := customMap.NewEntity(&worldComp{Val: 1})

	got, err := NewWithWorld(parent, world)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("NewWithWorld returned a nil graph")
	}

	got.Clear()
	if !world.Alive(custom) || !customMap.HasAll(custom) {
		t.Fatal("custom entity was removed by graph Clear")
	}
}
