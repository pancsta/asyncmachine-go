//go:build frogql && integration

package test

import (
	"testing"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	"github.com/pancsta/asyncmachine-go/pkg/graph/frogql"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
)

func init() {
	registerBenchBackend("frogql",
		func(_ testing.TB, _ *amgraph.Graph) (amtypes.DbGraph, error) {
			return frogql.NewDbGraph()
		})
}

// TestNetGraphFrogqlDb mirrors the shared machine graph into a frogql DB and
// asserts it over HTTP+JSON.
func TestNetGraphFrogqlDb(t *testing.T) {
	f := NewMachineGraph(t, t.Context(), DefaultGraphSpec)

	// sync the sink graph without the DB first, the mirroring slows the
	// receiver down
	f.AddState(ssrel.B)
	f.WaitSink()

	db, err := frogql.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	RunDbMirrorSuite(t, f, db)
}
