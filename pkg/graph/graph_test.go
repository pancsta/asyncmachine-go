package graph

import (
	"bytes"
	"net"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/mlange-42/ark/ecs"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/types"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// ///// ///// /////

// ///// GRAPH

// ///// ///// /////

func TestGraphGob(t *testing.T) {
	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-gob"})
	t.Cleanup(server.Dispose)
	g, err := New(server)
	if err != nil {
		t.Fatal(err)
	}

	// a parent and a child (CHILD_OF + CONNECTED_TO via the src-id tag)
	parent := &Machine{
		Serialized: &am.Serialized{
			ID:          "parent-1",
			StateNames:  ssrel.Names,
			Time:        am.Time{0, 1, 2, 3, 4},
			QueueTick:   7,
			MachineTick: 3,
			Tags:        []string{"tag-a"},
		},
		SchemaHash:   "hash-parent",
		Schema:       ssrel.States,
		MTimeSum:     10,
		TagsMTimeSum: 5,
	}
	child := &Machine{
		Serialized: &am.Serialized{
			ID:          "child-1",
			StateNames:  ssrel.Names,
			Time:        am.Time{0, 4, 5, 0, 9},
			QueueTick:   3,
			MachineTick: 1,
			Tags:        []string{"src-id:src-1", "tag-b"},
		},
		SchemaHash:   "hash-child",
		Schema:       ssrel.States,
		Parent:       "parent-1",
		MTimeSum:     18,
		TagsMTimeSum: 12,
	}
	src := &Machine{
		Serialized: &am.Serialized{
			ID:         "src-1",
			StateNames: ssrel.Names,
			Time:       make(am.Time, len(ssrel.Names)),
			QueueTick:  1,
		},
		SchemaHash: "hash-src",
		Schema:     ssrel.States,
	}
	for _, mach := range []*Machine{parent, child, src} {
		if err := g.AddMachine(mach); err != nil {
			t.Fatal(err)
		}
	}

	// save
	var buf bytes.Buffer
	if err := g.SaveGob(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty gob")
	}

	// restore into a fresh graph with the same parent machine
	g2, err := New(server)
	if err != nil {
		t.Fatal(err)
	}
	if err := g2.LoadGob(&buf); err != nil {
		t.Fatal(err)
	}

	// machines
	for _, id := range []string{"parent-1", "child-1", "src-1"} {
		m1, ok := g.Machine(id)
		if !ok {
			t.Fatalf("%s missing before save", id)
		}
		m2, ok := g2.Machine(id)
		if !ok {
			t.Fatalf("%s missing after load", id)
		}
		if !reflect.DeepEqual(m1, m2) {
			t.Fatalf("%s mismatch:\n%#v\n%#v", id, m1, m2)
		}
	}

	// vertices and edges with metadata
	adj1 := g.AdjacencyMap()
	adj2 := g2.AdjacencyMap()
	if !reflect.DeepEqual(adj1, adj2) {
		t.Fatalf("adjacency mismatch:\n%#v\n%#v", adj1, adj2)
	}

	// inspect + markdown
	inspect1, err := g.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	inspect2, err := g2.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspect1, inspect2) {
		t.Fatalf("inspect mismatch:\n%#v\n%#v", inspect1, inspect2)
	}
	if Markdown(inspect1) != Markdown(inspect2) {
		t.Fatal("markdown mismatch")
	}
}

func TestGraphGobEmpty(t *testing.T) {
	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-gob-empty"})
	t.Cleanup(server.Dispose)
	g, err := New(server)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := g.SaveGob(&buf); err != nil {
		t.Fatal(err)
	}

	g2, err := New(server)
	if err != nil {
		t.Fatal(err)
	}
	if err := g2.LoadGob(&buf); err != nil {
		t.Fatal(err)
	}
	if inspect, err := g2.Inspect(); err != nil || len(inspect) != 0 {
		t.Fatalf("unexpected inspect: %v %v", inspect, err)
	}
}

// TestAddMachineIdempotent asserts that re-adding a machine doesn't duplicate
// graph vertices or edges.
func TestAddMachineIdempotent(t *testing.T) {
	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-add"})
	t.Cleanup(server.Dispose)
	g, err := New(server)
	if err != nil {
		t.Fatal(err)
	}

	newMach := func() *Machine {
		return &Machine{
			Serialized: &am.Serialized{
				ID:         "mach-1",
				StateNames: ssrel.Names,
				Time:       make(am.Time, len(ssrel.Names)),
				QueueTick:  1,
				Tags:       []string{"tag-a"},
			},
			SchemaHash: "hash-1",
			Schema:     ssrel.States,
		}
	}

	count := func() (int, int) {
		adj := g.AdjacencyMap()
		var edges int
		for _, targets := range adj {
			edges += len(targets)
		}
		return len(adj), edges
	}

	if err := g.AddMachine(newMach()); err != nil {
		t.Fatal(err)
	}
	verts, edges := count()
	used := g.store.world.Stats().Entities.Used

	if err := g.AddMachine(newMach()); err != nil {
		t.Fatal(err)
	}
	verts2, edges2 := count()
	if verts2 != verts || edges2 != edges {
		t.Fatalf("graph grew after re-add: %d/%d != %d/%d",
			verts2, edges2, verts, edges)
	}
	if used2 := g.store.world.Stats().Entities.Used; used2 != used {
		t.Fatalf("entities grew after re-add: %d != %d", used2, used)
	}

	edge, err := g.Edge("mach-1", "mach-1:"+ssrel.B)
	if err != nil {
		t.Fatal(err)
	}
	data := edge.data()
	if data == nil || data.MachHas == nil || !data.MachHas.Multi {
		t.Fatalf("unexpected state edge: %#v", data)
	}
}

// TestWorldInjection asserts that a graph shares an injected ark world and
// leaves custom entities intact on Clear.
func TestWorldInjection(t *testing.T) {
	type custom struct {
		Val int
	}

	world := ecs.NewWorld()
	g, err := NewWithWorld(nil, world)
	if err != nil {
		t.Fatal(err)
	}

	customMap := ecs.NewMap1[custom](world)
	customEnt := customMap.NewEntity(&custom{Val: 1})

	mach := &Machine{
		Serialized: &am.Serialized{
			ID:         "mach-world",
			StateNames: ssrel.Names,
			Time:       make(am.Time, len(ssrel.Names)),
			QueueTick:  1,
		},
		SchemaHash: "hash-world",
		Schema:     ssrel.States,
	}
	if err := g.AddMachine(mach); err != nil {
		t.Fatal(err)
	}
	if len(g.AdjacencyMap()) == 0 {
		t.Fatal("graph is empty")
	}

	g.Clear()
	if got := len(g.AdjacencyMap()); got != 0 {
		t.Fatalf("graph not cleared: %d", got)
	}
	if !world.Alive(customEnt) || !customMap.HasAll(customEnt) {
		t.Fatal("custom world entity was removed by Clear")
	}
}

// ///// ///// /////

// ///// EDGE KINDS

// ///// ///// /////

// TestEdgesOfKind asserts GQL-style relationship queries over the typed ark
// store, e.g. MATCH (m:Machine)-[:HAS]->(s:State), (m)-[:HAS_TAG]->(t:Tag).
func TestEdgesOfKind(t *testing.T) {
	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-kinds"})
	t.Cleanup(server.Dispose)
	g, err := New(server)
	if err != nil {
		t.Fatal(err)
	}

	mach := &Machine{
		Serialized: &am.Serialized{
			ID:         "m-1",
			StateNames: ssrel.Names,
			Time:       make(am.Time, len(ssrel.Names)),
			QueueTick:  1,
			Tags:       []string{"tag-x", "src-id:p-1"},
		},
		SchemaHash: "hash-kinds",
		Schema:     ssrel.States,
		Parent:     "p-1",
	}
	if err := g.AddMachine(mach); err != nil {
		t.Fatal(err)
	}
	// pipes are stored as separate edge entities
	err = g.store.addEdge("m-1", "p-1", &EdgeData{
		MachPipesTo: []*MachPipeTo{
			{FromState: ssrel.A, ToState: ssrel.B, MutType: am.MutationAdd},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// MATCH (m:Machine)-[:HAS]->(s:State)
	has := g.EdgesOfKind(amtypes.RelHas)
	if len(has) != len(ssrel.States) {
		t.Fatalf("HAS edges = %d, want %d", len(has), len(ssrel.States))
	}
	for _, edge := range has {
		data := edge.Properties.Data.(*EdgeData)
		if edge.Source != "m-1" || data.MachHas == nil {
			t.Fatalf("unexpected HAS edge: %#v", edge)
		}
	}

	// MATCH (m:Machine)-[:HAS]->(s:State), (m)-[:HAS_TAG]->(t:Tag)
	tags := g.EdgesFrom(has[0].Source, amtypes.RelHasTag)
	if len(tags) != 2 {
		t.Fatalf("HAS_TAG edges = %d, want 2", len(tags))
	}

	// state relations
	for kind, want := range map[amtypes.RelKind]int{
		amtypes.RelRequire: 1, // A -> C
		amtypes.RelAdd:     3, // B -> C, D -> C, D -> B
		amtypes.RelRemove:  0,
	} {
		if got := len(g.EdgesOfKind(kind)); got != want {
			t.Fatalf("%s edges = %d, want %d", kind.Value, got, want)
		}
	}

	// pipes are returned one edge per pipe
	pipes := g.EdgesOfKind(amtypes.RelPipe)
	if len(pipes) != 1 {
		t.Fatalf("PIPE edges = %d, want 1", len(pipes))
	}
	pipeData := pipes[0].Properties.Data.(*EdgeData)
	if len(pipeData.MachPipesTo) != 1 ||
		pipeData.MachPipesTo[0].FromState != ssrel.A {
		t.Fatalf("unexpected PIPE edge: %#v", pipes[0])
	}

	// the pair edge aggregates all metadata, including pipes
	conn, err := g.Edge("m-1", "p-1")
	if err != nil {
		t.Fatal(err)
	}
	data := conn.data()
	if data == nil || !data.MachChildOf || !data.MachConnectedTo ||
		len(data.MachPipesTo) != 1 {
		t.Fatalf("unexpected pair edge: %#v", conn)
	}

	// unknown kinds are a no-op
	if got := g.EdgesOfKind(amtypes.RelKind{Value: "NOPE"}); len(got) != 0 {
		t.Fatalf("unknown kind returned %d edges", len(got))
	}
}

// TestRelKinds asserts the relationship kind enum.
func TestRelKinds(t *testing.T) {
	if got := amtypes.RelKinds.Len(); got != 9 {
		t.Fatalf("RelKinds = %d, want 9", got)
	}
	if !amtypes.RelKinds.Contains(amtypes.RelPipe) {
		t.Fatal("RelKinds misses RelPipe")
	}
	if kind := amtypes.RelKinds.Parse("HAS"); kind == nil ||
		*kind != amtypes.RelHas {
		t.Fatalf("unexpected HAS kind: %v", kind)
	}
	if kind := amtypes.RelKinds.Parse("NOPE"); kind != nil {
		t.Fatalf("unexpected NOPE kind: %v", kind)
	}
}

// TestPipeLog asserts pipe log parsing is persisted through the typed store.
func TestPipeLog(t *testing.T) {
	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-pipes"})
	t.Cleanup(server.Dispose)
	g, err := New(server)
	if err != nil {
		t.Fatal(err)
	}

	m1 := &Machine{Serialized: &am.Serialized{ID: "p-1"}}
	m2 := &Machine{Serialized: &am.Serialized{ID: "p-2"}}

	// OUT from p-1
	tx := &dbg.DbgMsgTx{LogEntries: []*am.LogEntry{
		{Text: "[pipe-out:add] A to p-2"},
	}}
	if err := g.parseMsgDbgLog(m1, tx); err != nil {
		t.Fatal(err)
	}
	// IN to p-2, twice (the second one is a dup)
	tx = &dbg.DbgMsgTx{LogEntries: []*am.LogEntry{
		{Text: "[pipe-in:add] B from p-1"},
		{Text: "[pipe-in:add] B from p-1"},
	}}
	if err := g.parseMsgDbgLog(m2, tx); err != nil {
		t.Fatal(err)
	}

	edge, err := g.Edge("p-1", "p-2")
	if err != nil {
		t.Fatal(err)
	}
	data := edge.data()
	if data == nil || len(data.MachPipesTo) != 1 {
		t.Fatalf("unexpected pipes: %#v", data)
	}
	pipe := data.MachPipesTo[0]
	if pipe.FromState != "A" || pipe.ToState != "B" ||
		pipe.MutType != am.MutationAdd {
		t.Fatalf("unexpected pipe: %#v", pipe)
	}

	// pipe GC removes incident edges
	tx = &dbg.DbgMsgTx{LogEntries: []*am.LogEntry{
		{Text: "[pipe:gc] p-1"},
	}}
	if err := g.parseMsgDbgLog(m1, tx); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Edge("p-1", "p-2"); err == nil {
		t.Fatal("pipe edges not removed by gc")
	}
}

// ///// ///// /////

// ///// NET GRAPH

// ///// ///// /////

func TestNetworkGraphResend(t *testing.T) {
	ctx := t.Context()

	mach := am.New(ctx, ssrel.States, &am.Opts{Id: "resend-1"})
	if err := mach.VerifyStates(ssrel.Names); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mach.Dispose)

	newGraph := func(id string) (*Graph, *NetworkGraph) {
		server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-" + id})
		g, err := New(server)
		if err != nil {
			t.Fatal(err)
		}
		ng, err := NewNetworkGraph(ctx, "127.0.0.1:0", g)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = ng.Close()
			server.Dispose()
		})
		return g, ng
	}

	_, sender := newGraph("sender")
	recvGraph, receiver := newGraph("receiver")

	schemaHash := amhelp.SchemaHash(mach.Schema())
	receiver.SchemaStore[schemaHash] = SchemaInfo{
		Schema:      mach.Schema(),
		StatesIndex: mach.StateNames(),
	}

	if _, err := sender.TraceMach(mach); err != nil {
		t.Fatal(err)
	}
	sender.EmitToAddr(receiver.Addr)

	waitClock := func() {
		t.Helper()
		expected := mach.Time(nil)
		deadline := time.Now().Add(3 * time.Second)
		for {
			var got am.Time
			if c, ok := recvGraph.Machine(mach.Id()); ok {
				got = slices.Clone(c.Time)
			}
			if slices.Equal(got, expected) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("receiver clock did not sync: %v != %v", got, expected)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	mach.Add1(ssrel.B, nil)
	<-mach.When1(ssrel.B, ctx)
	waitClock()

	// Corrupt checksum forces the receiver to request a full resend.
	bad := &MsgGraphUpdate{
		MsgSrvUpdate: &arpc.MsgSrvUpdate{
			Indexes:   []uint16{0},
			Ticks:     []uint32{1},
			QueueTick: 1,
			Checksum:  arpc.Checksum(1, 2, 0) + 1,
		},
		MachId:     mach.Id(),
		SchemaHash: schemaHash,
	}
	if err := sender.Broadcast(bad); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for receiver.MetricResendRequests.Load() == 0 ||
		sender.MetricResendsHandled.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("resend request was not handled")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Drop updates for a while, then disable drops and let the graph sync.
	sender.TestBroadcastRandomDrops.Store(true)
	for range 60 {
		res := mach.Toggle1(ssrel.B, nil)
		if res >= am.Queued {
			<-mach.WhenQueue(res)
		}
		time.Sleep(20 * time.Millisecond)
	}
	sender.TestBroadcastRandomDrops.Store(false)

	waitClock()
}

// TestSinkReconnect asserts that a broken sink is dropped and re-dialed.
func TestSinkReconnect(t *testing.T) {
	ctx := t.Context()

	server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-reconn"})
	t.Cleanup(server.Dispose)
	g, err := New(server)
	if err != nil {
		t.Fatal(err)
	}
	ng, err := NewNetworkGraph(ctx, "127.0.0.1:0", g)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ng.Close() })

	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	addr := peer.LocalAddr().String()

	_, cancel := ng.EmitToAddr(addr)
	defer cancel()

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
			time.Sleep(5 * time.Millisecond)
		}
	}

	// the sink connects in the background
	wait("initial sink", func() bool {
		return slices.Contains(ng.Sinks(), addr)
	})
	if got := ng.MetricReconnects.Load(); got != 1 {
		t.Fatalf("dials: %d, want 1", got)
	}

	// simulate a broken sink: close the conn behind Broadcast's back
	ng.sinksMx.Lock()
	conn := ng.sinks[addr]
	ng.sinksMx.Unlock()
	if conn == nil {
		t.Fatal("no sink registered")
	}
	_ = conn.Close()

	// the next broadcast notices the write error, drops the sink and re-dials
	_ = ng.Broadcast(&MsgGraphUpdate{MachId: "x"})
	wait("reconnect", func() bool {
		return ng.MetricReconnects.Load() >= 2
	})
	wait("sink restored", func() bool {
		return slices.Contains(ng.Sinks(), addr)
	})

	// the recovered sink still receives updates
	if err := ng.Broadcast(&MsgGraphUpdate{MachId: "y"}); err != nil {
		t.Fatal(err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1024)
	if _, _, err := peer.ReadFromUDP(buf); err != nil {
		t.Fatalf("peer read: %v", err)
	}
}
