//go:build frogql

package frogql_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/goforj/httpx"
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
	ng, err := amgraph.NewNetworkGraph(ctx, "127.0.0.1:0", g)
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
	httpc := httpx.New()

	post := func(accept, q string) (int, []byte) {
		t.Helper()
		opts := []httpx.Option{httpx.Header("Content-Type", "text/plain")}
		if accept != "" {
			opts = append(opts, httpx.Header("Accept", accept))
		}
		res := httpx.Post[string, []byte](httpc, base+"/query", q, opts...)
		if res.Response == nil || res.Response.Response == nil {
			t.Fatalf("query %q: %v", q, res.Err)
		}
		return res.Response.StatusCode, res.Response.Bytes()
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
	mres := httpx.Get[[]byte](httpc, base+"/machine?id=mach-http")
	if mres.Err != nil {
		t.Fatalf("machine: %v", mres.Err)
	}
	body = mres.Body
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
	mres = httpx.Get[[]byte](httpc, base+"/machine?id=nope")
	var httpErr *httpx.HTTPError
	if !errors.As(mres.Err, &httpErr) ||
		httpErr.StatusCode != http.StatusNotFound {
		t.Fatalf("missing machine: %v", mres.Err)
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
	ng, err := amgraph.NewNetworkGraph(ctx, "127.0.0.1:0", g)
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
	url := "http://" + addr + "/query"
	if reach := httpx.Get[[]byte](nil, url); reach.Response == nil {
		t.Fatalf("server unreachable: %v", reach.Err)
	}

	// closed with the NetworkGraph
	if err := ng.Close(); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	client := httpx.New(httpx.Timeout(time.Second))
	if reach := httpx.Get[[]byte](client, url); reach.Response != nil {
		t.Fatal("server still reachable after Close")
	}
}
