package graph

import (
	"bytes"
	"net"
	"reflect"
	"slices"
	"testing"
	"time"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
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
	adj1, err := g.G.AdjacencyMap()
	if err != nil {
		t.Fatal(err)
	}
	adj2, err := g2.G.AdjacencyMap()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(adj1, adj2) {
		t.Fatalf("adjacency mismatch:\n%#v\n%#v", adj1, adj2)
	}
	map1, err := g.Map.AdjacencyMap()
	if err != nil {
		t.Fatal(err)
	}
	map2, err := g2.Map.AdjacencyMap()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(map1, map2) {
		t.Fatalf("map adjacency mismatch:\n%#v\n%#v", map1, map2)
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

// ///// ///// /////

// ///// NET GRAPH

// ///// ///// /////

func TestNetGraphResend(t *testing.T) {
	ctx := t.Context()

	mach := am.New(ctx, ssrel.States, &am.Opts{Id: "resend-1"})
	if err := mach.VerifyStates(ssrel.Names); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mach.Dispose)

	newGraph := func(id string) (*Graph, *NetGraph) {
		server := am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-" + id})
		g, err := New(server)
		if err != nil {
			t.Fatal(err)
		}
		ng, err := NewNetGraph(ctx, "127.0.0.1:0", g)
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
	ng, err := NewNetGraph(ctx, "127.0.0.1:0", g)
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
