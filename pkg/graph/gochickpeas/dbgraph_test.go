package gochickpeas_test

import (
	"slices"
	"testing"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/pkg/graph/gochickpeas"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func newMach(id, parent string, tags []string) *amtypes.Machine {
	return &amtypes.Machine{
		Serialized: &am.Serialized{
			ID:         id,
			StateNames: ssrel.Names,
			Time:       make(am.Time, len(ssrel.Names)),
			QueueTick:  1,
			Tags:       tags,
		},
		SchemaHash: "hash-1",
		Schema:     ssrel.States,
		Parent:     parent,
	}
}

func TestDbGraph(t *testing.T) {
	db, err := gochickpeas.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	exec := func(q string, want int) []map[string]any {
		t.Helper()
		res, err := db.Exec(q)
		if err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
		rows, ok := res.([]any)
		if !ok {
			t.Fatalf("exec %q: unexpected result %#v", q, res)
		}
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			m, ok := row.(map[string]any)
			if !ok {
				t.Fatalf("exec %q: unexpected row %#v", q, row)
			}
			out = append(out, m)
		}
		if want >= 0 && len(out) != want {
			t.Fatalf("exec %q: got %d rows, want %d: %#v", q, len(out), want,
				out)
		}
		return out
	}

	states := len(ssrel.Names)
	// A requires C; B adds C; C after D; D adds C and B
	relations := 5

	// machine + states + tags(as nodes) + relations
	child := newMach("mach-child", "mach-parent", []string{"src-id:mach-src"})
	if err := db.UpsertMachine(child); err != nil {
		t.Fatal(err)
	}
	if got, want := db.NodeCount(), 1+states+1; got != want {
		t.Fatalf("node count: %d, want %d", got, want)
	}
	if got, want := db.EdgeCount(), states+1+relations; got != want {
		t.Fatalf("edge count: %d, want %d", got, want)
	}

	// tag nodes
	rows := exec("MATCH (m:Machine {id: 'mach-child'})-[:HAS_TAG]->(t:Tag)"+
		" RETURN t.name AS name", 1)
	if rows[0]["name"] != "src-id:mach-src" {
		t.Fatalf("unexpected tags: %#v", rows)
	}

	// AFTER relation edge
	exec("MATCH (a:State {mach: 'mach-child', name: 'C'})-[:AFTER]->"+
		"(b:State {mach: 'mach-child', name: 'D'}) RETURN b.name AS name", 1)

	// parent and src arrive later, cross-machine edges resolve retroactively
	if err := db.UpsertMachine(newMach("mach-parent", "", nil)); err != nil {
		t.Fatal(err)
	}
	rows = exec("MATCH (a:Machine)-[:CHILD_OF]->"+
		"(b:Machine {id: 'mach-parent'}) RETURN a.id AS id", 1)
	if rows[0]["id"] != "mach-child" {
		t.Fatalf("CHILD_OF edge missing: %#v", rows)
	}

	if err := db.UpsertMachine(newMach("mach-src", "", nil)); err != nil {
		t.Fatal(err)
	}
	rows = exec("MATCH (a:Machine)-[:CONNECTED_TO]->"+
		"(b:Machine {id: 'mach-src'}) RETURN a.id AS id", 1)
	if rows[0]["id"] != "mach-child" {
		t.Fatalf("CONNECTED_TO edge missing: %#v", rows)
	}

	// sync clocks and tags
	child.Time[1] = 7
	child.MTimeSum = 7
	child.Tags = []string{"src-id:mach-src", "foo"}
	if err := db.SyncMachine(child); err != nil {
		t.Fatal(err)
	}
	rows = exec("MATCH (s:State {mach: 'mach-child', name: 'A'})"+
		" RETURN s.tick AS tick, s.active AS active", 1)
	if rows[0]["tick"] != int64(7) || rows[0]["active"] != true {
		t.Fatalf("clock sync failed: %#v", rows)
	}
	rows = exec("MATCH (s:State {mach: 'mach-child', name: 'B'})"+
		" RETURN s.active AS active", 1)
	if rows[0]["active"] != false {
		t.Fatalf("inactive state failed: %#v", rows)
	}
	rows = exec("MATCH (m:Machine {id: 'mach-child'})-[:HAS_TAG]->(t:Tag)"+
		" RETURN t.name AS name, t.value AS value", 2)
	names := []string{rows[0]["name"].(string), rows[1]["name"].(string)}
	slices.Sort(names)
	if !slices.Equal(names, []string{"foo", "src-id:mach-src"}) {
		t.Fatalf("tag sync failed: %#v", names)
	}
	values := map[string]any{}
	for _, row := range rows {
		values[row["name"].(string)] = row["value"]
	}
	if values["src-id:mach-src"] != "mach-src" || values["foo"] != "foo" {
		t.Fatalf("tag values failed: %#v", values)
	}

	// idempotent upsert
	nodes, edges := db.NodeCount(), db.EdgeCount()
	if err := db.UpsertMachine(child); err != nil {
		t.Fatal(err)
	}
	if db.NodeCount() != nodes || db.EdgeCount() != edges {
		t.Fatalf("not idempotent: %d/%d != %d/%d", db.NodeCount(),
			db.EdgeCount(), nodes, edges)
	}

	// GQL is read-only
	if _, err := db.Exec("INSERT (a:Person {name: 'Alice'})"); err == nil {
		t.Fatal("expected an error for a write statement")
	}
}
