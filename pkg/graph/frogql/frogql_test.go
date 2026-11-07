//go:build frogql

package frogql_test

import (
	"strings"
	"testing"

	"github.com/pancsta/asyncmachine-go/pkg/graph/frogql"
)

func TestSmoke(t *testing.T) {
	conn, err := frogql.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	exec := func(q string) any {
		res, err := conn.Exec(q)
		if err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
		return res
	}

	// insert nodes
	res := exec("INSERT (a:Person {name: 'Alice'})")
	if counters, ok := res.(map[string]any); !ok ||
		counters["nodes_inserted"] != float64(1) {
		t.Fatalf("unexpected insert result: %#v", res)
	}
	exec("INSERT (b:Person {name: 'Bob'})")

	// edge insert
	res = exec("MATCH (a:Person {name: 'Alice'}), (b:Person {name: 'Bob'})" +
		" INSERT (a)-[:KNOWS {since: 2020}]->(b)")
	if counters, ok := res.(map[string]any); !ok ||
		counters["edges_inserted"] != float64(1) {
		t.Fatalf("unexpected edge insert result: %#v", res)
	}

	// query
	res = exec("MATCH (p:Person)-[:KNOWS]->(q:Person) RETURN p.name AS a," +
		" q.name AS b")
	rows, ok := res.([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("unexpected rows: %#v", res)
	}
	if row := rows[0].(map[string]any); row["a"] != "Alice" ||
		row["b"] != "Bob" {
		t.Fatalf("unexpected row: %#v", row)
	}

	// counts
	if conn.NodeCount() != 2 || conn.EdgeCount() != 1 {
		t.Fatalf("unexpected counts: %d %d", conn.NodeCount(),
			conn.EdgeCount())
	}

	// SET, RETURN the new value
	exec("MATCH (p:Person {name: 'Alice'}) SET p.tier = 'gold'")
	res = exec("MATCH (p:Person) RETURN p.tier AS tier")
	rows = res.([]any)
	tiers := map[any]int{}
	for _, row := range rows {
		tiers[row.(map[string]any)["tier"]]++
	}
	if tiers["gold"] != 1 {
		t.Fatalf("SET failed: %#v", rows)
	}

	// REMOVE
	exec("MATCH (p:Person {name: 'Alice'}) REMOVE p.tier")
	res = exec("MATCH (p:Person) RETURN p.tier AS tier")
	rows = res.([]any)
	for _, row := range rows {
		if row.(map[string]any)["tier"] == "gold" {
			t.Fatalf("REMOVE failed: %#v", rows)
		}
	}

	// to_json snapshot
	snap, err := conn.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snap, "KNOWS") || !strings.Contains(snap, "Alice") {
		t.Fatalf("unexpected snapshot: %s", snap)
	}
}

func TestListProps(t *testing.T) {
	conn, err := frogql.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// list literal, incl empty
	_, err = conn.Exec("INSERT (a:Machine {id: 'm1', tags: ['foo','bar']})")
	if err != nil {
		t.Fatalf("list insert: %v", err)
	}
	_, err = conn.Exec("INSERT (a:Machine {id: 'm2', tags: []})")
	if err != nil {
		t.Fatalf("empty list insert: %v", err)
	}

	res, err := conn.Exec("MATCH (m:Machine {id: 'm1'}) RETURN m.tags AS t")
	if err != nil {
		t.Fatal(err)
	}
	rows := res.([]any)
	tags, ok := rows[0].(map[string]any)["t"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "foo" {
		t.Fatalf("unexpected tags: %#v", rows)
	}

	// SET a list prop
	_, err = conn.Exec("MATCH (m:Machine {id: 'm1'})" +
		" SET m.tags = ['baz']")
	if err != nil {
		t.Fatalf("list set: %v", err)
	}
	res, _ = conn.Exec("MATCH (m:Machine {id: 'm1'}) RETURN m.tags AS t")
	tags = res.([]any)[0].(map[string]any)["t"].([]any)
	if len(tags) != 1 || tags[0] != "baz" {
		t.Fatalf("unexpected set tags: %#v", tags)
	}
}

func TestErrors(t *testing.T) {
	conn, err := frogql.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	_, err = conn.Exec("INSERT (a:Person {name: 'Alice'})")
	if err != nil {
		t.Fatal(err)
	}

	// invalid GQL returns an error
	if _, err = conn.Exec("NONSENSE ("); err == nil {
		t.Fatal("expected an error")
	}

	// data survives a failed statement
	if conn.NodeCount() != 1 {
		t.Fatalf("data lost after a failed statement: %d", conn.NodeCount())
	}

	// DETACH DELETE a non-matching node is a no-op
	_, err = conn.Exec("MATCH (n:Person {name: 'Nobody'}) DETACH DELETE n")
	if err != nil {
		t.Fatalf("no-op delete: %v", err)
	}
	if conn.NodeCount() != 1 {
		t.Fatalf("unexpected count after no-op delete: %d", conn.NodeCount())
	}

	// delete for real
	_, err = conn.Exec("MATCH (n:Person {name: 'Alice'}) DETACH DELETE n")
	if err != nil {
		t.Fatal(err)
	}
	if conn.NodeCount() != 0 {
		t.Fatalf("unexpected count after delete: %d", conn.NodeCount())
	}
}
