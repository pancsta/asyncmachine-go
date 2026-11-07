//go:build frogql

package frogql_test

import (
	"slices"
	"testing"
	"time"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	"github.com/pancsta/asyncmachine-go/pkg/graph/frogql"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func newDbMach(id, parent string, tags []string) *amgraph.Machine {
	return &amgraph.Machine{
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
	db, err := frogql.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	exec := func(q string, want int) []any {
		t.Helper()
		res, err := db.Exec(q)
		if err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
		rows, ok := res.([]any)
		if !ok {
			t.Fatalf("exec %q: unexpected result %#v", q, res)
		}
		if want >= 0 && len(rows) != want {
			t.Fatalf("exec %q: got %d rows, want %d: %#v", q, len(rows), want,
				rows)
		}
		return rows
	}

	states := len(ssrel.Names)
	// A requires C; B adds C; C after D; D adds C and B
	relations := 5

	// machine + states + tags(as nodes) + relations
	child := newDbMach("mach-child", "mach-parent", []string{"src-id:mach-src"})
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
	if rows[0].(map[string]any)["name"] != "src-id:mach-src" {
		t.Fatalf("unexpected tags: %#v", rows)
	}

	// AFTER relation edge
	exec("MATCH (a:State {mach: 'mach-child', name: 'C'})-[:AFTER]->"+
		"(b:State {mach: 'mach-child', name: 'D'}) RETURN b.name AS name", 1)

	// parent and src arrive later, cross-machine edges resolve retroactively
	parent := newDbMach("mach-parent", "", nil)
	if err := db.UpsertMachine(parent); err != nil {
		t.Fatal(err)
	}
	rows = exec("MATCH (a:Machine)-[:CHILD_OF]->"+
		"(b:Machine {id: 'mach-parent'}) RETURN a.id AS id", 1)
	if rows[0].(map[string]any)["id"] != "mach-child" {
		t.Fatalf("CHILD_OF edge missing: %#v", rows)
	}

	src := newDbMach("mach-src", "", nil)
	if err := db.UpsertMachine(src); err != nil {
		t.Fatal(err)
	}
	rows = exec("MATCH (a:Machine)-[:CONNECTED_TO]->"+
		"(b:Machine {id: 'mach-src'}) RETURN a.id AS id", 1)
	if rows[0].(map[string]any)["id"] != "mach-child" {
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
	if rows[0].(map[string]any)["tick"] != float64(7) ||
		rows[0].(map[string]any)["active"] != true {
		t.Fatalf("clock sync failed: %#v", rows)
	}
	rows = exec("MATCH (s:State {mach: 'mach-child', name: 'B'})"+
		" RETURN s.active AS active", 1)
	if rows[0].(map[string]any)["active"] != false {
		t.Fatalf("inactive state failed: %#v", rows)
	}
	rows = exec("MATCH (m:Machine {id: 'mach-child'})-[:HAS_TAG]->(t:Tag)"+
		" RETURN t.name AS name, t.value AS value", 2)
	names := []string{
		rows[0].(map[string]any)["name"].(string),
		rows[1].(map[string]any)["name"].(string),
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"foo", "src-id:mach-src"}) {
		t.Fatalf("tag sync failed: %#v", names)
	}
	values := map[string]any{}
	for _, row := range rows {
		r := row.(map[string]any)
		values[r["name"].(string)] = r["value"]
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
}

func TestNetGraphDb(t *testing.T) {
	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-db"})
	t.Cleanup(server.Dispose)
	g, err := amgraph.New(server)
	if err != nil {
		t.Fatal(err)
	}
	db, err := frogql.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ng, err := amgraph.NewNetGraph(ctx, "127.0.0.1:0", g)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ng.Close() })
	ng.Db = db

	mach := am.New(ctx, ssrel.States, &am.Opts{Id: "db-1"})
	if err := mach.VerifyStates(ssrel.Names); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mach.Dispose)

	if _, err := ng.TraceMach(mach); err != nil {
		t.Fatal(err)
	}

	// machine + states pushed to the DB
	if got := db.NodeCount(); got != 1+len(ssrel.Names) {
		t.Fatalf("node count: %d", got)
	}
	res, err := db.Exec("MATCH (m:Machine {id: 'db-1'}) RETURN m.id AS id")
	if err != nil {
		t.Fatal(err)
	}
	rows, ok := res.([]any)
	if !ok || len(rows) != 1 || rows[0].(map[string]any)["id"] != "db-1" {
		t.Fatalf("machine missing in the DB: %#v", res)
	}
}

func TestNetGraphDbSync(t *testing.T) {
	ctx := t.Context()

	mach := am.New(ctx, ssrel.States, &am.Opts{Id: "db-sync-1"})
	if err := mach.VerifyStates(ssrel.Names); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mach.Dispose)

	newGraph := func(id string) (*amgraph.Graph, *amgraph.NetGraph, amtypes.DbGraph) {
		server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-" + id})
		g, err := amgraph.New(server)
		if err != nil {
			t.Fatal(err)
		}
		ng, err := amgraph.NewNetGraph(ctx, "127.0.0.1:0", g)
		if err != nil {
			t.Fatal(err)
		}
		db, err := frogql.NewDbGraph()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = ng.Close()
			_ = db.Close()
			server.Dispose()
		})
		return g, ng, db
	}

	_, sender, _ := newGraph("db-sender")
	_, receiver, recvDb := newGraph("db-receiver")
	receiver.Db = recvDb

	schemaHash := amhelp.SchemaHash(mach.Schema())
	receiver.SchemaStore[schemaHash] = amgraph.SchemaInfo{
		Schema:      mach.Schema(),
		StatesIndex: mach.StateNames(),
	}

	if _, err := sender.TraceMach(mach); err != nil {
		t.Fatal(err)
	}
	sender.EmitToAddr(receiver.Addr)

	mach.Add1(ssrel.B, nil)
	<-mach.When1(ssrel.B, ctx)

	wait := func(what string, fn func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			if fn() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// mirror of the machine in the receiver's DB
	wait("machine mirror", func() bool {
		return recvDb.NodeCount() >= 1+len(ssrel.Names)
	})

	// tag changes sync as tag nodes
	tags := []string{"db-tag"}
	mach.SetTags(tags)
	wait("tag nodes", func() bool {
		res, err := recvDb.Exec("MATCH (t:Tag {mach: 'db-sync-1'})" +
			" RETURN t.name AS name")
		if err != nil {
			return false
		}
		rows, ok := res.([]any)
		return ok && len(rows) == 1 &&
			rows[0].(map[string]any)["name"] == "db-tag"
	})
}
