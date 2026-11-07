//go:build frogql

package frogql_test

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/vmihailenco/msgpack/v5"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	"github.com/pancsta/asyncmachine-go/pkg/graph/frogql"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func TestDbHttpServer(t *testing.T) {
	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-http"})
	t.Cleanup(server.Dispose)
	g, err := amgraph.New(server)
	if err != nil {
		t.Fatal(err)
	}
	db, err := frogql.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}
	ng, err := amgraph.NewNetGraph(ctx, "127.0.0.1:0", g)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = ng.Close()
		_ = db.Close()
	})

	// the server requires a DB
	if _, err := ng.StartDbHttpServer("127.0.0.1:0"); err == nil {
		t.Fatal("expected an error without a DB")
	}

	ng.Db = db

	// mirror a machine with clocks and tags
	schemaHash := amhelp.SchemaHash(ssrel.States)
	mach := newDbMach("mach-http", "", []string{"tag-a"})
	mach.SchemaHash = schemaHash
	mach.Time[1] = 5
	mach.MTimeSum = 5
	if err := db.UpsertMachine(mach); err != nil {
		t.Fatal(err)
	}

	// the schema is needed to order the states
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
		req, err := http.NewRequest("POST", base+"/query",
			strings.NewReader(q))
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

	// serialized machine, lowercase keys
	resp, err := http.Get(base + "/machine?id=mach-http")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("machine: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"state_names"`) {
		t.Fatalf("not lowercase: %s", body)
	}
	var ser am.Serialized
	if err := json.Unmarshal(body, &ser); err != nil {
		t.Fatal(err)
	}
	if ser.ID != "mach-http" || !slices.Equal(ser.StateNames, ssrel.Names) {
		t.Fatalf("unexpected machine: %#v", ser)
	}
	if len(ser.Time) != len(ssrel.Names) || ser.Time[1] != 5 {
		t.Fatalf("unexpected clocks: %#v", ser.Time)
	}
	if !slices.Equal(ser.Tags, []string{"tag-a"}) {
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

	// websocket, text
	conn, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText,
		[]byte("MATCH (m:Machine) RETURN m.id AS id")); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows = nil
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("ws json: %v: %s", err, data)
	}
	if len(rows) != 1 || rows[0]["id"] != "mach-http" {
		t.Fatalf("ws rows: %#v", rows)
	}

	// websocket, msgpack
	q, err := msgpack.Marshal("MATCH (m:Machine) RETURN m.id AS id")
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, q); err != nil {
		t.Fatal(err)
	}
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows = nil
	if err := msgpack.Unmarshal(data, &rows); err != nil {
		t.Fatalf("ws msgpack: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "mach-http" {
		t.Fatalf("ws msgpack rows: %#v", rows)
	}

	// websocket, serialized machine (JSON)
	req, err := json.Marshal(map[string]string{"machine": "mach-http"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, req); err != nil {
		t.Fatal(err)
	}
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var serWs am.Serialized
	if err := json.Unmarshal(data, &serWs); err != nil {
		t.Fatalf("ws machine json: %v: %s", err, data)
	}
	if serWs.ID != "mach-http" || !slices.Equal(serWs.StateNames, ssrel.Names) {
		t.Fatalf("ws machine: %#v", serWs)
	}
	if len(serWs.Time) != len(ssrel.Names) || serWs.Time[1] != 5 {
		t.Fatalf("ws machine clocks: %#v", serWs.Time)
	}

	// websocket, serialized machine (msgpack)
	req, err = msgpack.Marshal(map[string]string{"machine": "mach-http"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, req); err != nil {
		t.Fatal(err)
	}
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	serWs = am.Serialized{}
	if err := msgpack.Unmarshal(data, &serWs); err != nil {
		t.Fatalf("ws machine msgpack: %v", err)
	}
	if serWs.ID != "mach-http" || !slices.Equal(serWs.StateNames, ssrel.Names) {
		t.Fatalf("ws machine msgpack: %#v", serWs)
	}

	// websocket, unknown machine
	req, err = json.Marshal(map[string]string{"machine": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, req); err != nil {
		t.Fatal(err)
	}
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wsErr map[string]string
	if err := json.Unmarshal(data, &wsErr); err != nil {
		t.Fatalf("ws error: %v: %s", err, data)
	}
	if wsErr["error"] != "machine not found" {
		t.Fatalf("ws error: %#v", wsErr)
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestDbHttpServerClose(t *testing.T) {
	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-http-close"})
	t.Cleanup(server.Dispose)
	g, err := amgraph.New(server)
	if err != nil {
		t.Fatal(err)
	}
	db, err := frogql.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}
	ng, err := amgraph.NewNetGraph(ctx, "127.0.0.1:0", g)
	if err != nil {
		t.Fatal(err)
	}
	ng.Db = db

	addr, err := ng.StartDbHttpServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	// already started
	if _, err := ng.StartDbHttpServer("127.0.0.1:0"); err == nil {
		t.Fatal("expected an error when already started")
	}

	// reachable (empty query -> 400, but the connection works)
	resp, err := http.Get("http://" + addr + "/query")
	if err != nil {
		t.Fatalf("server unreachable: %v", err)
	}
	resp.Body.Close()

	// closed with the NetGraph
	if err := ng.Close(); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	client := &http.Client{Timeout: time.Second}
	if resp, err := client.Get("http://" + addr + "/query"); err == nil {
		resp.Body.Close()
		t.Fatal("server still reachable after Close")
	}
}
