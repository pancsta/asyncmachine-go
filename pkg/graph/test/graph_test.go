//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"github.com/goforj/httpx"
	"github.com/vmihailenco/msgpack/v5"

	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	"github.com/pancsta/asyncmachine-go/pkg/graph/gochickpeas"
	"github.com/pancsta/asyncmachine-go/pkg/graph/gql"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/types"
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

// TestNetworkGraphDAGVisual renders the shared machine graph as a D2 SVG.
// TODO similar test for frogql, behind "frogql integration" tags, query the graph over JSON to assert according to the spec
func TestNetworkGraphDiagrams(t *testing.T) {
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

// TestNetworkGraphGochickpeasDb mirrors the shared machine graph into a
// gochickpeas DB and asserts it over HTTP+JSON.
func TestNetworkGraphGochickpeasDb(t *testing.T) {
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

// TestNetworkGraphRing connects 4 independent graphs into a ring of their sink
// nodes and asserts that every sink mirrors all 4 clusters without duplicates.
func TestNetworkGraphRing(t *testing.T) {
	ctx := t.Context()
	const clusters = 4

	var nets [clusters]*MachineGraph
	var bases [clusters]string
	var allIDs []string
	seen := make(map[string]struct{})

	for i := range clusters {
		db, err := gochickpeas.NewDbGraph()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })

		// prefix the node IDs, so each cluster has unique machines
		spec := strings.ReplaceAll(DefaultGraphSpec, "node",
			fmt.Sprintf("c%dnode", i))
		nets[i] = NewMachineGraph(t, ctx, spec, GraphOpts{
			SinkDb:       db,
			Quiet:        true,
			NoRandomTags: true,
		})
		for _, mach := range nets[i].Machines {
			id := mach.Id()
			if _, ok := seen[id]; ok {
				t.Fatalf("duplicate machine ID %s", id)
			}
			seen[id] = struct{}{}
			allIDs = append(allIDs, id)
		}

		addr, err := nets[i].Sink.Net.StartDbHttpServer("127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		bases[i] = "http://" + addr
	}

	// quiet the ring: it intentionally produces resend requests
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	for i := range clusters {
		for _, n := range nets[i].Nodes {
			n.Net.Logger = quiet
		}
	}

	// ring of sinks, each emitting to the next one
	for i := range clusters {
		_, cancel := nets[i].Sink.Net.EmitToAddr(
			nets[(i+1)%clusters].Sink.Net.Addr)
		t.Cleanup(cancel)
	}

	// one change per machine, so all of them get emitted
	for i := range clusters {
		nets[i].AddState(ssrel.B)
	}

	// wait until every sink mirrors every cluster
	want := len(allIDs)
	idsBySink := make([]map[string]int, clusters)
	for i := range clusters {
		idsBySink[i] = waitRingMachines(t, bases[i], want)
	}

	for i := range clusters {
		ids := idsBySink[i]
		if len(ids) != want {
			t.Fatalf("sink %d: %d unique machines, want %d", i, len(ids), want)
		}
		for id, n := range ids {
			if n != 1 {
				t.Fatalf("sink %d: %d copies of %s", i, n, id)
			}
		}
		for _, id := range allIDs {
			if _, ok := ids[id]; !ok {
				t.Fatalf("sink %d: missing machine %s", i, id)
			}
		}
		// all 4 clusters are present
		for c := range clusters {
			prefix := fmt.Sprintf("c%dnode", c)
			wantC := len(nets[c].Machines)
			got := 0
			for id := range ids {
				if strings.HasPrefix(id, prefix) {
					got++
				}
			}
			if got != wantC {
				t.Fatalf("sink %d: cluster %d has %d/%d machines",
					i, c, got, wantC)
			}
		}
		if got := nets[i].Sink.Graph.MachineCount(); got != want {
			t.Fatalf("sink %d: graph has %d machines, want %d", i, got, want)
		}
	}

	// no duplicate state vertices or HAS edges per machine
	states := len(ssrel.Names)
	for i := range clusters {
		rows, err := queryGql(bases[i],
			"MATCH (s:State) RETURN s.mach AS mach, s.name AS name")
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != want*states {
			t.Fatalf("sink %d: %d state vertices, want %d",
				i, len(rows), want*states)
		}
		pairs := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			mach, _ := row["mach"].(string)
			name, _ := row["name"].(string)
			pairs[mach+"\x00"+name] = struct{}{}
		}
		if len(pairs) != len(rows) {
			t.Fatalf("sink %d: duplicate state vertices", i)
		}

		for c := range clusters {
			id := nets[c].Machines[0].Id()
			rows, err := queryGql(bases[i], fmt.Sprintf(
				"MATCH (m:Machine {id: '%s'})-[:HAS]->(s:State) "+
					"RETURN s.name AS name", id))
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != states {
				t.Fatalf("sink %d: %s has %d HAS edges, want %d",
					i, id, len(rows), states)
			}
		}
	}
}

// waitRingMachines waits until a GQL DB mirrors want machines.
func waitRingMachines(t *testing.T, base string, want int) map[string]int {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	var err error
	for {
		var rows []map[string]any
		rows, err = queryGql(base, "MATCH (m:Machine) RETURN m.id AS id")
		if err == nil {
			ids := make(map[string]int, len(rows))
			for _, row := range rows {
				id, _ := row["id"].(string)
				ids[id]++
			}
			if len(ids) >= want {
				return ids
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d machines in GQL: %v", want, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestNetworkGraphSim builds the full tree, discovers node addresses from #addr
// tags over real TCP RPC conns, and measures how long a state change on the
// deepest node takes to show up in a GQL query against the sink's DB.
func TestNetworkGraphSim(t *testing.T) {
	ctx := t.Context()

	// mirror the sink into a GQL DB from the start
	db, err := gochickpeas.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	f := NewMachineGraph(t, ctx, DefaultGraphSpec, GraphOpts{SinkDb: db})
	addr, err := f.Sink.Net.StartDbHttpServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr

	f.AddState(ssrel.B)
	f.WaitParents()
	f.WaitSink()

	// discovery from #addr tags, resulting in real TCP RPC conns
	f.StartDiscovery()
	f.WaitDiscovery(15 * time.Second)

	// the deepest node, 3 UDP hops from the sink
	deep := f.ByID["node-4-4"].Machines[0]
	deepID := deep.Id()
	if tagAddr := amhelp.TagValue(deep.Tags(), ssrpc.TagKeyAddr); tagAddr == "" {
		t.Fatalf("%s has no %s tag", deepID, ssrpc.TagKeyAddr)
	}

	// wait until the sink's GQL DB sees the deep node in the given state
	waitActive := func(active bool) time.Duration {
		t.Helper()
		start := time.Now()
		deadline := time.Now().Add(10 * time.Second)
		for {
			rows, err := queryGql(base, fmt.Sprintf(
				"MATCH (m:Machine {id: '%s'})-[:HAS]->(s:State) "+
					"WHERE s.name = '%s' AND s.active = %t "+
					"RETURN m.id AS id", deepID, ssrel.B, active))
			if err == nil && len(rows) > 0 {
				return time.Since(start)
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s active=%t in GQL: %v",
					deepID, active, err)
			}
			time.Sleep(time.Millisecond)
		}
	}

	// initial sync
	waitActive(true)

	// measure deep changes until they are discoverable by GQL
	var samples []time.Duration
	for i := range 5 {
		start := time.Now()
		deep.Remove1(ssrel.B, nil)
		waitActive(false)
		removed := time.Since(start)

		start = time.Now()
		deep.Add1(ssrel.B, nil)
		waitActive(true)
		added := time.Since(start)

		samples = append(samples, removed, added)
		t.Logf("round %d: remove %s, add %s", i+1, removed, added)
	}
	slices.Sort(samples)
	t.Logf("deep state change -> GQL: min=%s p50=%s max=%s (n=%d)",
		samples[0], samples[len(samples)/2], samples[len(samples)-1],
		len(samples))

	// the discovered TCP conn is mirrored as a CONNECTED_TO edge
	var clientID string
	f.discoveryMx.Lock()
	for _, cl := range f.Clients {
		if cl.NetMach != nil && cl.NetMach.RemoteId() == deepID &&
			cl.Mach.ParentId() == f.Sink.Server.Id() {
			clientID = cl.NetMach.Id()
			break
		}
	}
	f.discoveryMx.Unlock()
	if clientID == "" {
		t.Fatalf("sink has no RPC client for %s", deepID)
	}

	deadline := time.Now().Add(5 * time.Second)
	var rows []map[string]any
	for {
		rows, err = queryGql(base, fmt.Sprintf(
			"MATCH (m:Machine)-[:CONNECTED_TO]->(src:Machine) "+
				"WHERE m.id = '%s' RETURN src.id AS src", clientID))
		if err == nil && len(rows) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: no CONNECTED_TO edge to %s: %v", clientID, deepID, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := rows[0]["src"]; got != deepID {
		t.Fatalf("%s: CONNECTED_TO %v, want %s", clientID, got, deepID)
	}
}

// TestNetworkGraphDAG builds the shared machine graph over UDP.
func TestNetworkGraph(t *testing.T) {
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
	tagVertex, err := f.Sink.Graph.Vertex(tagID)
	if err != nil || tagVertex.TagName != tags[0] {
		t.Fatalf("tag vertex missing: %v", err)
	}
	conn, err = f.Sink.Graph.Connection(source.Id(), tagID)
	if err != nil || !conn.Edge.MachHasTag {
		t.Fatalf("machine-tag edge missing: %v", err)
	}
}

// TestNetworkGraphDAGLive keeps the same graph alive while mutating every machine
// once per second. With AM_DBG_ADDR set, machines are connected via amhelpt.
func TestNetworkGraphLive(t *testing.T) {
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
		client := httpx.New()

		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			res := httpx.Post[string, []map[string]any](client,
				env.base+"/query", benchQueryList,
				httpx.Header("Content-Type", "text/plain"))
			if res.Err != nil {
				b.Fatal(res.Err)
			}
			if len(res.Body) != benchMachines {
				b.Fatalf("unexpected result: %d", len(res.Body))
			}
		}
		reportTps(b, start)
	})
}

// BenchmarkMachineListHttpParallel queries all machines over HTTP+JSON from
// multiple goroutines, exposing the DB mutex contention.
func BenchmarkMachineListHttpParallel(b *testing.B) {
	runBackends(b, func(b *testing.B, env *benchEnv) {
		client := httpx.New()

		b.ResetTimer()
		b.ReportAllocs()
		start := time.Now()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				res := httpx.Post[string, []map[string]any](client,
					env.base+"/query", benchQueryList,
					httpx.Header("Content-Type", "text/plain"))
				if res.Err != nil {
					b.Error(res.Err)
					return
				}
				if len(res.Body) != benchMachines {
					b.Errorf("unexpected result: %d", len(res.Body))
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
		client := httpx.New()
		url := env.base + "/machine?id=bench-mach-40"

		b.ReportAllocs()
		b.ResetTimer()
		start := time.Now()
		for i := 0; i < b.N; i++ {
			res := httpx.Get[am.Serialized](client, url)
			if res.Err != nil {
				b.Fatal(res.Err)
			}
			ser := res.Body
			if ser.ID != "bench-mach-40" ||
				!slices.Equal(ser.StateNames, ssrel.Names) {
				b.Fatalf("unexpected machine: %#v", ser)
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
	ng, err := amgraph.NewNetworkGraph(ctx, "127.0.0.1:0", g)
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

// runBackends runs a benchmark body for every registered backend, optionally
// filtered by AM_BENCH_BACKEND.
func runBackends(
	b *testing.B, body func(b *testing.B, env *benchEnv),
) {
	names := slices.Sorted(maps.Keys(benchBackends))
	if filter := os.Getenv("AM_BENCH_BACKEND"); filter != "" {
		names = slices.DeleteFunc(names, func(name string) bool {
			return name != filter
		})
	}
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
	ng, err := amgraph.NewNetworkGraph(ctx, "127.0.0.1:0", g)
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
	httpc := httpx.New()

	status := func(key string) int {
		t.Helper()
		opts := []httpx.Option{httpx.Header("Content-Type", "text/plain")}
		if key != "" {
			opts = append(opts, httpx.Header("X-API-Key", key))
		}
		res := httpx.Post[string, []byte](httpc, base+"/query",
			"MATCH (m:Machine) RETURN m.id AS id", opts...)
		if res.Err == nil {
			return http.StatusOK
		}
		var httpErr *httpx.HTTPError
		if errors.As(res.Err, &httpErr) {
			return httpErr.StatusCode
		}
		t.Fatalf("query %q: %v", key, res.Err)
		return 0
	}

	// HTTP auth
	if code := status(""); code != http.StatusUnauthorized {
		t.Fatalf("missing key: %d", code)
	}
	if code := status("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", code)
	}
	if code := status("secret-1"); code != http.StatusOK {
		t.Fatalf("valid key: %d", code)
	}

	// /machine auth
	res := httpx.Get[[]byte](httpc, base+"/machine?id=mach-auth")
	var httpErr *httpx.HTTPError
	if !errors.As(res.Err, &httpErr) ||
		httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("machine without key: %v", res.Err)
	}
	res = httpx.Get[[]byte](httpc, base+"/machine?id=mach-auth",
		httpx.Header("X-API-Key", "secret-2"))
	if res.Err != nil {
		t.Fatalf("machine with key: %v", res.Err)
	}
	if res.Response.StatusCode != http.StatusOK {
		t.Fatalf("machine with key: %d", res.Response.StatusCode)
	}

	// WS without a key
	_, wsResp, err := websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
	if err == nil {
		t.Fatal("expected a WS handshake error without a key")
	}
	if wsResp != nil {
		if wsResp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("ws no key: %d", wsResp.StatusCode)
		}
		_ = wsResp.Body.Close()
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
