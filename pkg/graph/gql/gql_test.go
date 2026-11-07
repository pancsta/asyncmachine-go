package gql_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/vmihailenco/msgpack/v5"

	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	"github.com/pancsta/asyncmachine-go/pkg/graph/gql"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ///// ///// /////

// ///// TESTS

// ///// ///// /////

// TestDbHttpServer wires the native backend into NetGraph's generic DB HTTP
// server (POST /query, GET /machine).
func TestDbHttpServer(t *testing.T) {
	ctx := t.Context()

	srv := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-gql-http"})
	t.Cleanup(srv.Dispose)

	g, err := amgraph.New(srv)
	if err != nil {
		t.Fatal(err)
	}
	ng, err := amgraph.NewNetGraph(ctx, "127.0.0.1:0", g)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ng.Close() })

	// the server requires a DB
	if _, err := ng.StartDbHttpServer("127.0.0.1:0"); err == nil {
		t.Fatal("expected an error without a DB")
	}

	ng.Db = gql.NewDb(g)

	// the graph instance is the store: one machine with clocks and tags
	schemaHash := amhelp.SchemaHash(ssrel.States)
	mach := &amtypes.Machine{
		Serialized: &am.Serialized{
			ID:         "mach-http",
			StateNames: ssrel.Names,
			Time:       make(am.Time, len(ssrel.Names)),
			Tags:       []string{"tag-a"},
		},
		SchemaHash: schemaHash,
		Schema:     ssrel.States,
	}
	mach.Time[1] = 5
	mach.MTimeSum = 5
	if err := g.AddMachine(mach); err != nil {
		t.Fatal(err)
	}

	// the schema is needed by /machine to order the states
	ng.SchemaStore[schemaHash] = amgraph.SchemaInfo{
		Schema:      ssrel.States,
		StatesIndex: ssrel.Names,
	}

	addr, err := ng.StartDbHttpServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr

	post := func(accept, q string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest("POST", base+"/query", strings.NewReader(q))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "text/plain")
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, body
	}

	// query, JSON
	code, body := post("", "MATCH (m:Machine) RETURN m.id AS id")
	if code != http.StatusOK {
		t.Fatalf("query: %d %s", code, body)
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("query json: %v: %s", err, body)
	}
	if len(rows) != 1 || rows[0]["id"] != "mach-http" {
		t.Fatalf("query rows: %#v", rows)
	}

	// query, msgpack
	code, body = post("application/msgpack",
		"MATCH (m:Machine) RETURN m.id AS id")
	if code != http.StatusOK {
		t.Fatalf("query msgpack: %d %s", code, body)
	}
	rows = nil
	if err := msgpack.Unmarshal(body, &rows); err != nil {
		t.Fatalf("query msgpack decode: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "mach-http" {
		t.Fatalf("query msgpack rows: %#v", rows)
	}

	// bad GQL
	code, body = post("", "NONSENSE (")
	if code != http.StatusBadRequest || !strings.Contains(string(body), "error") {
		t.Fatalf("bad query: %d %s", code, body)
	}

	// serialized machine
	resp, err := http.Get(base + "/machine?id=mach-http")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("machine: %d %s", resp.StatusCode, body)
	}

	var ser am.Serialized
	if err := json.Unmarshal(body, &ser); err != nil {
		t.Fatal(err)
	}
	if ser.ID != "mach-http" || len(ser.StateNames) != len(ssrel.Names) {
		t.Fatalf("unexpected machine: %#v", ser)
	}
	if len(ser.Time) != len(ssrel.Names) || ser.Time[1] != 5 {
		t.Fatalf("unexpected clocks: %#v", ser.Time)
	}
	if len(ser.Tags) != 1 || ser.Tags[0] != "tag-a" {
		t.Fatalf("unexpected tags: %#v", ser.Tags)
	}

	// missing machine
	resp, err = http.Get(base + "/machine?id=nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing machine: %d", resp.StatusCode)
	}
}

func TestMachines(t *testing.T) {
	db := newDb(t)

	out := rows(t, db, "MATCH (m:Machine) RETURN m.id AS id ORDER BY id")
	if got := column(t, out, "id"); !reflect.DeepEqual(got,
		[]string{"a", "b", "c"}) {
		t.Fatalf("ids = %v", got)
	}
}

func TestMachineProps(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (m:Machine {id: 'a'})"+
			" RETURN m.queue_tick AS q, m.mach_tick AS mt,"+
			" m.schema_hash AS hash, m.mtime_sum AS sum")
	if len(out) != 1 {
		t.Fatalf("rows = %d", len(out))
	}
	r := out[0]
	if r["q"] != int64(5) || r["mt"] != int64(2) || r["hash"] != "ha" ||
		r["sum"] != int64(3) {
		t.Fatalf("row = %v", r)
	}
}

func TestStates(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (m:Machine)-[:HAS]->(s:State)"+
			" WHERE m.id = 'a'"+
			" RETURN s.name AS name, s.tick AS tick, s.active AS active"+
			" ORDER BY name")
	if got := column(t, out, "name"); !reflect.DeepEqual(got,
		[]string{"A", "B"}) {
		t.Fatalf("names = %v", got)
	}
	if out[0]["tick"] != int64(3) || out[1]["tick"] != int64(0) {
		t.Fatalf("ticks = %v, %v", out[0]["tick"], out[1]["tick"])
	}
	if out[0]["active"] != true || out[1]["active"] != false {
		t.Fatalf("active = %v, %v", out[0]["active"], out[1]["active"])
	}
}

func TestRelations(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (s:State)-[:REQUIRE]->(t:State)"+
			" RETURN s.mach AS mach, s.name AS from, t.name AS to")
	if len(out) != 1 || out[0]["from"] != "A" || out[0]["to"] != "B" ||
		out[0]["mach"] != "a" {
		t.Fatalf("rows = %v", out)
	}
}

func TestTags(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (m:Machine)-[:HAS_TAG]->(t:Tag)"+
			" WHERE m.id = 'a' RETURN t.name AS name, t.value AS value"+
			" ORDER BY name")
	if got := column(t, out, "name"); !reflect.DeepEqual(got,
		[]string{"src-id:b", "t1"}) {
		t.Fatalf("tags = %v", got)
	}
	if got := column(t, out, "value"); !reflect.DeepEqual(got,
		[]string{"b", "t1"}) {
		t.Fatalf("tag values = %v", got)
	}
}

func TestConnAndChild(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (a:Machine)-[:CONNECTED_TO]->(b:Machine)"+
			" RETURN a.id AS a, b.id AS b")
	if len(out) != 1 || out[0]["a"] != "a" || out[0]["b"] != "b" {
		t.Fatalf("conn = %v", out)
	}

	out = rows(t, db,
		"MATCH (c:Machine)-[:CHILD_OF]->(p:Machine)"+
			" RETURN c.id AS c, p.id AS p")
	if len(out) != 1 || out[0]["c"] != "c" || out[0]["p"] != "a" {
		t.Fatalf("child = %v", out)
	}
}

func TestOptionalMatch(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (m:Machine)"+
			" OPTIONAL MATCH (m)-[:HAS_TAG]->(t:Tag)"+
			" RETURN m.id AS id, t.name AS tag ORDER BY id, tag")
	if len(out) != 4 {
		t.Fatalf("rows = %v", out)
	}

	var pairs []string
	for _, r := range out {
		tag, _ := r["tag"].(string)
		pairs = append(pairs, r["id"].(string)+"="+tag)
	}
	want := []string{"a=src-id:b", "a=t1", "b=", "c="}
	if !reflect.DeepEqual(pairs, want) {
		t.Fatalf("pairs = %v", pairs)
	}
}

func TestAggregation(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (m:Machine)-[:HAS]->(s:State)"+
			" RETURN m.id AS id, count(*) AS states ORDER BY id")
	if len(out) != 3 {
		t.Fatalf("rows = %v", out)
	}
	want := map[string]int64{"a": 2, "b": 2, "c": 1}
	for _, r := range out {
		if r["states"] != want[r["id"].(string)] {
			t.Fatalf("row = %v (want %v)", r, want)
		}
	}

	out = rows(t, db,
		"MATCH (s:State) RETURN sum(s.tick) AS sum, min(s.tick) AS min,"+
			" max(s.tick) AS max")
	if out[0]["sum"] != int64(11) || out[0]["min"] != int64(0) ||
		!inEq(out[0]["max"], 7) {
		t.Fatalf("agg = %v", out[0])
	}
}

func TestDistinctLimitSkip(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (m:Machine)-[:HAS]->(s:State)"+
			" RETURN DISTINCT m.id AS id ORDER BY id")
	if got := column(t, out, "id"); !reflect.DeepEqual(got,
		[]string{"a", "b", "c"}) {
		t.Fatalf("distinct = %v", got)
	}

	out = rows(t, db,
		"MATCH (m:Machine) RETURN m.id AS id ORDER BY id SKIP 1 LIMIT 1")
	if len(out) != 1 || out[0]["id"] != "b" {
		t.Fatalf("page = %v", out)
	}
}

func TestWhere(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (m:Machine) WHERE m.id STARTS WITH 'a' RETURN m.id AS id")
	if len(out) != 1 || out[0]["id"] != "a" {
		t.Fatalf("starts with = %v", out)
	}

	out = rows(t, db,
		"MATCH (m:Machine) WHERE m.id IN ['a', 'c'] RETURN m.id AS id ORDER BY id")
	if got := column(t, out, "id"); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("in = %v", got)
	}

	out = rows(t, db,
		"MATCH (m:Machine) WHERE m.parent = '' RETURN m.id AS id ORDER BY id")
	if got := column(t, out, "id"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("empty parent = %v", got)
	}
}

// TestMachineQuery exercises the exact statement the Db HTTP server uses for
// GET /machine.
func TestMachineQuery(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (m:Machine {id: 'a'})"+
			" OPTIONAL MATCH (m)-[:HAS]->(s:State)"+
			" OPTIONAL MATCH (m)-[:HAS_TAG]->(t:Tag)"+
			" RETURN m.schema_hash AS schema_hash,"+
			" m.queue_tick AS queue_tick, m.mach_tick AS mach_tick,"+
			" s.name AS state_name, s.tick AS tick, t.name AS tag")
	// 2 states x 2 tags
	if len(out) != 4 {
		t.Fatalf("rows = %d: %v", len(out), out)
	}

	states := map[string]struct{}{}
	tags := map[string]struct{}{}
	for _, r := range out {
		if r["schema_hash"] != "ha" || r["queue_tick"] != int64(5) ||
			r["mach_tick"] != int64(2) {
			t.Fatalf("props = %v", r)
		}
		states[r["state_name"].(string)] = struct{}{}
		tags[r["tag"].(string)] = struct{}{}
	}
	if len(states) != 2 || len(tags) != 2 {
		t.Fatalf("states = %v, tags = %v", states, tags)
	}
}

func TestUnion(t *testing.T) {
	db := newDb(t)

	out := rows(t, db,
		"MATCH (m:Machine) RETURN m.id AS id"+
			" UNION ALL MATCH (m:Machine) RETURN m.id AS id")
	if len(out) != 6 {
		t.Fatalf("union all rows = %d", len(out))
	}

	out = rows(t, db,
		"MATCH (m:Machine) RETURN m.id AS id"+
			" UNION MATCH (m:Machine) RETURN m.id AS id ORDER BY id")
	if got := column(t, out, "id"); !reflect.DeepEqual(got,
		[]string{"a", "b", "c"}) {
		t.Fatalf("union = %v", got)
	}
}

func TestCounts(t *testing.T) {
	db := newDb(t)

	if n := db.NodeCount(); n != 10 {
		t.Fatalf("NodeCount = %d", n)
	}
	if n := db.EdgeCount(); n != 10 {
		t.Fatalf("EdgeCount = %d", n)
	}
}

func TestExplain(t *testing.T) {
	db := newDb(t)

	out := rows(t, db, "EXPLAIN MATCH (m:Machine) RETURN m.id AS id")
	if len(out) != 1 {
		t.Fatalf("rows = %v", out)
	}
	if _, ok := out[0]["plan"].(string); !ok {
		t.Fatalf("plan = %v", out[0])
	}
}

func TestUnsupported(t *testing.T) {
	db := newDb(t)

	for _, q := range []string{
		"MATCH (a)-[:HAS]->{1,2}(b) RETURN a",
		"MATCH p = (a)-[:HAS]->(b) RETURN p",
	} {
		_, err := db.Exec(q)
		if !errors.Is(err, gql.ErrUnsupported) {
			t.Fatalf("%q: err = %v", q, err)
		}
	}

	if _, err := db.Exec("MATCH ("); err == nil ||
		!errors.Is(err, gql.ErrParse) {
		t.Fatalf("parse err = %v", err)
	}
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// inEq reports whether v equals n (numbers may arrive as int/int64/float64).
func inEq(v any, n int64) bool {
	switch x := v.(type) {
	case int64:
		return x == n
	case int:
		return int64(x) == n
	case float64:
		return x == float64(n)
	}
	return false
}

// fixture builds a small netgraph:
//
//	a: machine, states A,B (A requires B), tags t1 + src-id:b, queue_tick 5
//	b: machine, states B1,B2
//	c: machine (child of a), state C1
func fixture(t *testing.T) *amgraph.Graph {
	t.Helper()

	g, err := amgraph.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	add := func(m *amgraph.Machine) {
		t.Helper()
		if err := g.AddMachine(m); err != nil {
			t.Fatalf("add machine %s: %v", m.ID, err)
		}
	}

	add(&amgraph.Machine{
		Serialized: &am.Serialized{
			ID:          "a",
			StateNames:  []string{"A", "B"},
			Time:        am.Time{3, 0},
			QueueTick:   5,
			MachineTick: 2,
			Tags:        []string{"t1", "src-id:b"},
		},
		SchemaHash: "ha",
		MTimeSum:   3,
		Schema: am.Schema{
			"A": {Require: am.S{"B"}},
			"B": {},
		},
	})
	add(&amgraph.Machine{
		Serialized: &am.Serialized{
			ID:         "b",
			StateNames: []string{"B1", "B2"},
			Time:       am.Time{1, 0},
		},
		SchemaHash: "hb",
		Schema: am.Schema{
			"B1": {},
			"B2": {},
		},
	})
	add(&amgraph.Machine{
		Serialized: &am.Serialized{
			ID:         "c",
			StateNames: []string{"C1"},
			Time:       am.Time{7},
		},
		SchemaHash: "hc",
		Parent:     "a",
		Schema: am.Schema{
			"C1": {},
		},
	})

	return g
}

// newDb returns a DbGraph over the fixture.
func newDb(t *testing.T) amtypes.DbGraph {
	t.Helper()
	return gql.NewDb(fixture(t))
}

// rows executes q and returns the result rows as maps.
func rows(t *testing.T, db amtypes.DbGraph, q string) []map[string]any {
	t.Helper()

	res, err := db.Exec(q)
	if err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
	list, ok := res.([]any)
	if !ok {
		t.Fatalf("exec %q: result type %T", q, res)
	}
	out := make([]map[string]any, len(list))
	for i, r := range list {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("exec %q: row type %T", q, r)
		}
		out[i] = m
	}
	return out
}

// column extracts one column as strings (the fixture values are strings).
func column(t *testing.T, out []map[string]any, name string) []string {
	t.Helper()

	ret := make([]string, len(out))
	for i, r := range out {
		s, _ := r[name].(string)
		ret[i] = s
	}
	return ret
}
