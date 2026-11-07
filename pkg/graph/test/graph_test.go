//go:build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/vmihailenco/msgpack/v5"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	"github.com/pancsta/asyncmachine-go/pkg/graph/gochickpeas"
	"github.com/pancsta/asyncmachine-go/pkg/graph/gql"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/visualizer"
)

const (
	// benchMachines is the number of fully synced machines per benchmark.
	benchMachines = 80
	// benchQueryList is the hot query used by the DbGraph tests.
	benchQueryList = "MATCH (m:Machine) RETURN m.id AS id"
)

// benchEnv is a fully synced graph mirror served over HTTP.
type benchEnv struct {
	db   amtypes.DbGraph
	addr string
	base string
}

// dbBackends is filled by backend registration files. It holds every backend
// for the read-only shared suites (auth).
var dbBackends = map[string]dbBackendFactory{}

// benchBackends holds writable backends only: the benchmark setup seeds the
// DB via [amtypes.DbGraph.UpsertMachine], which graph-bound backends ignore.
var benchBackends = map[string]dbBackendFactory{}

func init() {
	registerBenchBackend("gql",
		func(_ testing.TB, g *amgraph.Graph) (amtypes.DbGraph, error) {
			return gql.NewDb(g), nil
		})
	registerBenchBackend("gochickpeas",
		func(_ testing.TB, _ *amgraph.Graph) (amtypes.DbGraph, error) {
			return gochickpeas.NewDbGraph()
		})
}

// ///// ///// /////

// ///// TESTS

// ///// ///// /////

// TestDbServerAuth runs the Db HTTP server auth checks against every
// registered backend.
func TestDbServerAuth(t *testing.T) {
	for _, name := range slices.Sorted(maps.Keys(dbBackends)) {
		factory := dbBackends[name]
		t.Run(name, func(t *testing.T) {
			runDbServerAuth(t, factory)
		})
	}
}

// TestNetGraphDAGVisual renders the shared machine graph as a D2 SVG.
// TODO similar test for frogql, behind "frogql integration" tags, query the graph over JSON to assert according to the spec
func TestNetGraphDiagrams(t *testing.T) {
	f := NewMachineGraph(t, t.Context(), DefaultGraphSpec)
	f.AddState(ssrel.B)
	f.WaitSink()

	// graph := f.Sink.Graph
	// graph = graph

	// write to CWD
	out := "netgraph"
	r := visualizer.NewRenderer(f.Sink.Graph, t.Logf)
	visualizer.PresetMap(r)
	r.OutputFilename = out
	r.OutputMermaid = false
	r.OutputD2 = true
	r.OutputD2Svg = true

	if err := r.GenDiagrams(t.Context()); err != nil {
		t.Fatal(err)
	}

	svg, err := os.ReadFile(out + ".svg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), "<svg") {
		t.Fatal("generated file is not an SVG")
	}

	// output inspect markdown to a file
	inspect, err := f.Sink.Graph.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	md := amgraph.Markdown(inspect)
	if err := os.WriteFile(out+".md", []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestNetGraphGochickpeasDb mirrors the shared machine graph into a
// gochickpeas DB and asserts it over HTTP+JSON.
func TestNetGraphGochickpeasDb(t *testing.T) {
	f := NewMachineGraph(t, t.Context(), DefaultGraphSpec)

	// sync the sink graph without the DB first, the mirroring slows the
	// receiver down
	f.AddState(ssrel.B)
	f.WaitSink()

	db, err := gochickpeas.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	RunDbMirrorSuite(t, f, db)
}

// TestNetGraphDAG builds the shared machine graph over UDP.
func TestNetGraph(t *testing.T) {
	ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	f := NewMachineGraph(t, ctx, DefaultGraphSpec)
	f.AddState(ssrel.B)

	f.WaitParents()
	f.WaitSink()
	f.AssertSinkGraph()

	source := f.Machines[0]
	target := f.Machines[1].Id()
	tags := []string{ssrpc.TagKeySrcId + ":" + target}
	source.SetTags(tags)
	f.WaitTags(source.Id(), tags)

	conn, err := f.Sink.Graph.Connection(source.Id(), target)
	if err != nil || !conn.Edge.MachConnectedTo {
		t.Fatalf("tag RPC edge missing: %v", err)
	}

	tagID := source.Id() + "#" + tags[0]
	tagVertex, err := f.Sink.Graph.G.Vertex(tagID)
	if err != nil || tagVertex.TagName != tags[0] {
		t.Fatalf("tag vertex missing: %v", err)
	}
	conn, err = f.Sink.Graph.Connection(source.Id(), tagID)
	if err != nil || !conn.Edge.MachHasTag {
		t.Fatalf("machine-tag edge missing: %v", err)
	}
}

// TestNetGraphDAGLive keeps the same graph alive while mutating every machine
// once per second. With AM_DBG_ADDR set, machines are connected via amhelpt.
func TestNetGraphLive(t *testing.T) {
	t.Skip("LIVE TEST")

	if testing.Short() {
		t.Skip("live test")
	}

	ctx, stop := signal.NotifyContext(t.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	f := NewMachineGraph(t, ctx, DefaultGraphSpec)
	for _, mach := range f.Machines {
		amhelpt.MachDebugEnv(t, mach)
	}

	liveFor := 3 * time.Second
	if raw := os.Getenv("AM_NETGRAPH_LIVE_DURATION"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			liveFor = d
		}
	}
	t.Logf("live for %s", liveFor)

	f.AddState(ssrel.B)

	states := ssrel.Names
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(liveFor)
	defer deadline.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-deadline.C:
			break loop
		case <-ticker.C:
			for _, mach := range f.Machines {
				if rand.IntN(2) == 0 {
					mach.Remove1(states[rand.IntN(len(states))], nil)
					continue
				}
				mach.Add1(states[rand.IntN(len(states))], nil)
			}
		}
	}

	f.WaitClients(f.Sink.Graph, f.Machines)
}

// ///// ///// /////

// ///// BENCHES

// ///// ///// /////

// BenchmarkMachineListExec queries all machines directly from the DB.
func BenchmarkMachineListExec(b *testing.B) {
	runBackends(b, func(b *testing.B, env *benchEnv) {
		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			if _, err := env.db.Exec(benchQueryList); err != nil {
				b.Fatal(err)
			}
		}
		reportTps(b, start)
	})
}

// BenchmarkMachineListExecParallel queries all machines directly from the DB
// in parallel. Backends without a global execution mutex (gql) scale with the
// goroutines, the mirrored ones expose their per-DB lock.
func BenchmarkMachineListExecParallel(b *testing.B) {
	runBackends(b, func(b *testing.B, env *benchEnv) {
		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := env.db.Exec(benchQueryList); err != nil {
					b.Error(err)
					return
				}
			}
		})
		reportTps(b, start)
	})
}

// BenchmarkMachineListHttp queries all machines over HTTP+JSON.
func BenchmarkMachineListHttp(b *testing.B) {
	runBackends(b, func(b *testing.B, env *benchEnv) {
		client := &http.Client{}

		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			resp, err := client.Post(env.base+"/query", "text/plain",
				strings.NewReader(benchQueryList))
			if err != nil {
				b.Fatal(err)
			}
			var rows []map[string]any
			err = json.NewDecoder(resp.Body).Decode(&rows)
			_ = resp.Body.Close()
			if err != nil {
				b.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK || len(rows) != benchMachines {
				b.Fatalf("unexpected result: %d %d", resp.StatusCode,
					len(rows))
			}
		}
		reportTps(b, start)
	})
}

// BenchmarkMachineListHttpParallel queries all machines over HTTP+JSON from
// multiple goroutines, exposing the DB mutex contention.
func BenchmarkMachineListHttpParallel(b *testing.B) {
	runBackends(b, func(b *testing.B, env *benchEnv) {
		client := &http.Client{}

		b.ResetTimer()
		b.ReportAllocs()
		start := time.Now()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := client.Post(env.base+"/query", "text/plain",
					strings.NewReader(benchQueryList))
				if err != nil {
					b.Error(err)
					return
				}
				var rows []map[string]any
				err = json.NewDecoder(resp.Body).Decode(&rows)
				_ = resp.Body.Close()
				if err != nil {
					b.Error(err)
					return
				}
				if resp.StatusCode != http.StatusOK ||
					len(rows) != benchMachines {
					b.Errorf("unexpected result: %d %d", resp.StatusCode,
						len(rows))
					return
				}
			}
		})
		reportTps(b, start)
	})
}

// BenchmarkMachineSerializedHttp fetches one machine as am.Serialized over
// HTTP+JSON, incl its states and tags.
func BenchmarkMachineSerializedHttp(b *testing.B) {
	runBackends(b, func(b *testing.B, env *benchEnv) {
		client := &http.Client{}
		url := env.base + "/machine?id=bench-mach-40"

		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			resp, err := client.Get(url)
			if err != nil {
				b.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				b.Fatal(err)
			}
			var ser am.Serialized
			if err := json.Unmarshal(body, &ser); err != nil {
				b.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK ||
				ser.ID != "bench-mach-40" ||
				!slices.Equal(ser.StateNames, ssrel.Names) {
				b.Fatalf("unexpected machine: %d %s", resp.StatusCode, body)
			}
		}
		reportTps(b, start)
	})
}

// BenchmarkWsQueryJson queries all machines over WebSocket, JSON frames.
func BenchmarkWsQueryJson(b *testing.B) {
	runBackends(b, func(b *testing.B, env *benchEnv) {
		ctx := b.Context()

		conn, _, err := websocket.Dial(ctx, "ws://"+env.addr+"/ws", nil)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		})

		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			if err := conn.Write(ctx, websocket.MessageText,
				[]byte(benchQueryList)); err != nil {
				b.Fatal(err)
			}
			_, data, err := conn.Read(ctx)
			if err != nil {
				b.Fatal(err)
			}
			var rows []map[string]any
			if err := json.Unmarshal(data, &rows); err != nil {
				b.Fatal(err)
			}
			if len(rows) != benchMachines {
				b.Fatalf("unexpected rows: %d", len(rows))
			}
		}
		reportTps(b, start)
	})
}

// BenchmarkWsQueryMsgpack queries all machines over WebSocket, msgpack
// binary frames.
func BenchmarkWsQueryMsgpack(b *testing.B) {
	runBackends(b, func(b *testing.B, env *benchEnv) {
		ctx := b.Context()

		q, err := msgpack.Marshal(benchQueryList)
		if err != nil {
			b.Fatal(err)
		}
		conn, _, err := websocket.Dial(ctx, "ws://"+env.addr+"/ws", nil)
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		})

		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			if err := conn.Write(ctx, websocket.MessageBinary, q); err != nil {
				b.Fatal(err)
			}
			_, data, err := conn.Read(ctx)
			if err != nil {
				b.Fatal(err)
			}
			var rows []map[string]any
			if err := msgpack.Unmarshal(data, &rows); err != nil {
				b.Fatal(err)
			}
			if len(rows) != benchMachines {
				b.Fatalf("unexpected rows: %d", len(rows))
			}
		}
		reportTps(b, start)
	})
}

// BenchmarkWsQueryMsgpackParallel queries all machines over WebSocket, msgpack
// binary frames, one connection per goroutine.
func BenchmarkWsQueryMsgpackParallel(b *testing.B) {
	runBackends(b, func(b *testing.B, env *benchEnv) {
		ctx := b.Context()

		q, err := msgpack.Marshal(benchQueryList)
		if err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()

		b.ReportAllocs()
		start := time.Now()
		b.RunParallel(func(pb *testing.PB) {
			conn, _, err := websocket.Dial(ctx, "ws://"+env.addr+"/ws", nil)
			if err != nil {
				b.Error(err)
				return
			}
			defer func() {
				_ = conn.Close(websocket.StatusNormalClosure, "")
			}()

			for pb.Next() {
				if err := conn.Write(ctx, websocket.MessageBinary, q); err != nil {
					b.Error(err)
					return
				}
				_, data, err := conn.Read(ctx)
				if err != nil {
					b.Error(err)
					return
				}
				var rows []map[string]any
				if err := msgpack.Unmarshal(data, &rows); err != nil {
					b.Error(err)
					return
				}
				if len(rows) != benchMachines {
					b.Errorf("unexpected rows: %d", len(rows))
					return
				}
			}
		})
		reportTps(b, start)
	})
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// newBenchEnv mirrors benchMachines machines, fully synced, and starts the
// Db HTTP server.
func newBenchEnv(b *testing.B, factory dbBackendFactory) *benchEnv {
	b.Helper()

	ctx := b.Context()
	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-bench"})
	b.Cleanup(server.Dispose)

	g, err := amgraph.New(server)
	if err != nil {
		b.Fatal(err)
	}
	ng, err := amgraph.NewNetGraph(ctx, "127.0.0.1:0", g)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = ng.Close() })

	db, err := factory(b, g)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	ng.Db = db

	// the schema is needed by GET /machine
	schemaHash := amhelp.SchemaHash(ssrel.States)
	ng.SchemaStore[schemaHash] = amgraph.SchemaInfo{
		Schema:      ssrel.States,
		StatesIndex: ssrel.Names,
	}

	// fully sync all machines before the timer starts
	ids := make([]string, benchMachines)
	for i := range ids {
		ids[i] = fmt.Sprintf("bench-mach-%d", i)
	}
	for i, id := range ids {
		var tags []string
		if i%3 == 0 {
			tags = []string{"src-id:" + ids[(i+7)%len(ids)]}
		}
		m := benchNewMach(id, tags)
		m.SchemaHash = schemaHash

		// seed both sides: mirroring backends take UpsertMachine, graph-bound
		// ones (gql) read the graph directly
		if err := g.AddMachine(m); err != nil {
			b.Fatal(err)
		}
		if err := db.UpsertMachine(m); err != nil {
			b.Fatal(err)
		}
	}

	addr, err := ng.StartDbHttpServer("127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}

	return &benchEnv{db: db, addr: addr, base: "http://" + addr}
}

// benchNewMach returns a machine mirroring the fixture shape.
func benchNewMach(id string, tags []string) *amtypes.Machine {
	return &amtypes.Machine{
		Serialized: &am.Serialized{
			ID:         id,
			StateNames: ssrel.Names,
			Time:       make(am.Time, len(ssrel.Names)),
			QueueTick:  1,
			Tags:       tags,
		},
		SchemaHash: "bench-hash",
		Schema:     ssrel.States,
	}
}

// reportTps reports queries per second for the elapsed benchmark time.
func reportTps(b *testing.B, start time.Time) {
	elapsed := time.Since(start)
	b.StopTimer()
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "tps")
}

// runBackends runs a benchmark body for every registered backend.
func runBackends(
	b *testing.B, body func(b *testing.B, env *benchEnv),
) {
	names := slices.Sorted(maps.Keys(benchBackends))
	for _, name := range names {
		factory := benchBackends[name]
		b.Run(name, func(b *testing.B) {
			body(b, newBenchEnv(b, factory))
		})
	}
}

// dbBackendFactory builds a DbGraph for the given graph. Remote backends
// ignore g, the native one reads it directly.
type dbBackendFactory func(
	t testing.TB, g *amgraph.Graph,
) (amtypes.DbGraph, error)

// registerBenchBackend registers a writable DbGraph backend for the shared
// benchmarks.
func registerBenchBackend(name string, factory dbBackendFactory) {
	dbBackends[name] = factory
	benchBackends[name] = factory
}

// runDbServerAuth asserts X-API-Key/Sec-WebSocket-Protocol auth on HTTP and
// WebSocket, mirroring tools/relay.
func runDbServerAuth(t *testing.T, factory dbBackendFactory) {
	t.Helper()

	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-auth"})
	t.Cleanup(server.Dispose)
	g, err := amgraph.New(server)
	if err != nil {
		t.Fatal(err)
	}
	ng, err := amgraph.NewNetGraph(ctx, "127.0.0.1:0", g)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ng.Close() })

	db, err := factory(t, g)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ng.Db = db

	// one machine, so /machine is authorized-and-present
	mach := am.New(ctx, ssrel.States, &am.Opts{Id: "mach-auth"})
	if err := mach.VerifyStates(ssrel.Names); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mach.Dispose)
	if _, err := ng.TraceMach(mach); err != nil {
		t.Fatal(err)
	}

	addr, err := ng.StartDbHttpServer("127.0.0.1:0",
		amgraph.DbServerOpts{ApiKeys: []string{"secret-1", "secret-2"}})
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr

	query := func(key string) int {
		t.Helper()
		req, err := http.NewRequest("POST", base+"/query",
			strings.NewReader("MATCH (m:Machine) RETURN m.id AS id"))
		if err != nil {
			t.Fatal(err)
		}
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()

		return resp.StatusCode
	}

	// HTTP auth
	if code := query(""); code != http.StatusUnauthorized {
		t.Fatalf("missing key: %d", code)
	}
	if code := query("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", code)
	}
	if code := query("secret-1"); code != http.StatusOK {
		t.Fatalf("valid key: %d", code)
	}

	// /machine auth
	resp, err := http.Get(base + "/machine?id=mach-auth")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("machine without key: %d", resp.StatusCode)
	}
	req, err := http.NewRequest("GET", base+"/machine?id=mach-auth", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", "secret-2")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("machine with key: %d", resp.StatusCode)
	}

	// WS without a key
	_, resp, err = websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
	if err == nil {
		t.Fatal("expected a WS handshake error without a key")
	}
	if resp != nil {
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("ws no key: %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// WS with the X-API-Key header
	conn, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws",
		&websocket.DialOptions{
			HTTPHeader: http.Header{"X-API-Key": {"secret-2"}},
		})
	if err != nil {
		t.Fatal(err)
	}
	wsAuthQuery(t, ctx, conn)
	_ = conn.Close(websocket.StatusNormalClosure, "")

	// WS with the key in Sec-WebSocket-Protocol (browser/WASM style)
	conn, _, err = websocket.Dial(ctx, "ws://"+addr+"/ws",
		&websocket.DialOptions{
			Subprotocols: []string{"X-API-Key", "secret-1"},
		})
	if err != nil {
		t.Fatal(err)
	}
	wsAuthQuery(t, ctx, conn)
	_ = conn.Close(websocket.StatusNormalClosure, "")

	// close explicitly, the cleanup context is already cancelled (saves ~3s)
	if err := ng.Close(); err != nil {
		t.Fatal(err)
	}
}

// wsAuthQuery runs one GQL frame over an authorized WS connection.
func wsAuthQuery(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()

	if err := conn.Write(ctx, websocket.MessageText,
		[]byte("MATCH (m:Machine) RETURN m.id AS id")); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("ws auth json: %v: %s", err, data)
	}
	if len(rows) != 1 || rows[0]["id"] != "mach-auth" {
		t.Fatalf("ws auth rows: %#v", rows)
	}
}
