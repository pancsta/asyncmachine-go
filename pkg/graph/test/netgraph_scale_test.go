//go:build bench_netgraph && linux

package test

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	"github.com/pancsta/asyncmachine-go/pkg/graph/gochickpeas"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

const (
	// scaleDefaultNodes is a perfect binary tree of depth 19 (2^20-1).
	scaleDefaultNodes = 1<<20 - 1
	// scalePort is the fixed UDP port; uniqueness comes from the loopback IP.
	scalePort = 7000
)

// TestNetGraphFullUDP builds a real NetGraph per node via the public API and
// measures a deep state change until it is visible in the final sink's GQL DB.
// It is gated behind the "bench_netgraph" tag and needs ~2 fds per node
// (listener + EmitToAddr sink).
func TestNetGraphFullUDP(t *testing.T) {
	nodes := scaleNodes()
	iters := scaleIters()
	requireScale(t, nodes)

	ctx := t.Context()
	prevGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(prevGC)

	t.Logf("nodes=%d iters=%d fd-limit-need=%d", nodes, iters, nodes*2+1024)
	setupStart := time.Now()

	db, err := gochickpeas.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}

	// one handler-less parent machine shared by all graphs
	parentMach := am.New(ctx, am.Schema{
		"ErrGraph": {Require: am.S{am.StateException}},
	}, &am.Opts{Id: "srv-scale"})
	t.Cleanup(parentMach.Dispose)

	probeMach := am.New(ctx, ssrel.States, &am.Opts{Id: "probe"})
	if err := probeMach.VerifyStates(ssrel.Names); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(probeMach.Dispose)
	probeSchema := amgraph.SchemaInfo{
		Schema:      probeMach.Schema(),
		StatesIndex: probeMach.StateNames(),
	}
	probeHash := amhelp.SchemaHash(probeMach.Schema())

	// active path: deepest leaf -> root
	path := make([]bool, nodes)
	for i := nodes - 1; ; i = (i - 1) / 2 {
		path[i] = true
		if i == 0 {
			break
		}
	}

	nets := make([]*amgraph.NetGraph, nodes)
	created := 0
	t.Cleanup(func() {
		for i := created - 1; i >= 0; i-- {
			_ = nets[i].Close()
		}
		_ = db.Close()
	})

	// root / final sink with the GQL DB
	rootG, err := amgraph.New(parentMach)
	if err != nil {
		t.Fatal(err)
	}
	root, err := amgraph.NewNetGraph(ctx, scaleNodeAddr(0), rootG,
		amgraph.NetGraphOpts{
			Db:            db,
			PushInterval:  NetGraphPushInterval,
			ReadBufSize:   2048,
			SinkLocalAddr: scaleSinkAddr(0),
		})
	if err != nil {
		t.Fatal(err)
	}
	nets[0] = root
	created = 1

	addr, err := root.StartDbHttpServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr

	// real NetGraph per node, 10ms push only on the active path
	for i := 1; i < nodes; i++ {
		g, err := amgraph.New(parentMach)
		if err != nil {
			t.Fatal(err)
		}
		interval := time.Hour
		if path[i] {
			interval = NetGraphPushInterval
		}
		ng, err := amgraph.NewNetGraph(ctx, scaleNodeAddr(i), g,
			amgraph.NetGraphOpts{
				PushInterval:  interval,
				ReadBufSize:   2048,
				SinkLocalAddr: scaleSinkAddr(i),
			})
		if err != nil {
			t.Fatalf("node %d: %v", i, err)
		}
		nets[i] = ng
		created = i + 1
		if i%100000 == 0 {
			t.Logf("created %d/%d", i, nodes)
		}
	}

	// schema is needed only on the active path
	for i := range nodes {
		if path[i] {
			nets[i].SchemaStore[probeHash] = probeSchema
		}
	}

	// connect every node to its parent via the public API
	for i := 1; i < nodes; i++ {
		nets[i].EmitToAddr(nets[(i-1)/2].Addr)
		if i%100000 == 0 {
			t.Logf("linked %d/%d", i, nodes)
		}
	}

	// trace a single real source machine at the deepest leaf
	if _, err := nets[nodes-1].TraceMach(probeMach); err != nil {
		t.Fatal(err)
	}

	setupDur := time.Since(setupStart)
	debug.SetGCPercent(prevGC)
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("setup=%s goroutines=%d alloc=%dMB sys=%dMB",
		setupDur, runtime.NumGoroutine(), ms.Alloc>>20, ms.Sys>>20)

	// wait until the sink's GQL DB sees the probe in the given state
	waitActive := func(active bool) {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for {
			rows, err := queryGql(base, fmt.Sprintf(
				"MATCH (m:Machine {id: '%s'})-[:HAS]->(s:State) "+
					"WHERE s.name = '%s' AND s.active = %t "+
					"RETURN m.id AS id", probeMach.Id(), ssrel.B, active))
			if err == nil && len(rows) > 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s active=%t in GQL: %v",
					probeMach.Id(), active, err)
			}
			time.Sleep(time.Millisecond)
		}
	}

	// warm up, includes lazy machine creation in the sink graph/DB
	probeMach.Add1(ssrel.B, nil)
	<-probeMach.When1(ssrel.B, ctx)
	waitActive(true)
	t.Logf("sink GQL discovered the probe machine")

	var samples []time.Duration
	for i := range iters {
		start := time.Now()
		probeMach.Remove1(ssrel.B, nil)
		<-probeMach.WhenNot1(ssrel.B, ctx)
		waitActive(false)
		removed := time.Since(start)

		start = time.Now()
		probeMach.Add1(ssrel.B, nil)
		<-probeMach.When1(ssrel.B, ctx)
		waitActive(true)
		added := time.Since(start)

		samples = append(samples, removed, added)
		t.Logf("round %d: remove %s, add %s", i+1, removed, added)
	}
	slices.Sort(samples)
	t.Logf("deep state change -> GQL: min=%s p50=%s max=%s (n=%d)",
		samples[0], samples[len(samples)/2], samples[len(samples)-1],
		len(samples))
}

// TestNetGraphDeep runs the production NetGraph/RPC discovery path on a deeper
// live tree with a single machine per node.
func TestNetGraphDeep(t *testing.T) {
	ctx := t.Context()

	db, err := gochickpeas.NewDbGraph()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	depth := 6
	if v := os.Getenv("AM_NETGRAPH_DEPTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 2 && n <= 12 {
			depth = n
		}
	}
	spec := TreeSpec(depth, 2) // 127 nodes at depth 6
	f := NewMachineGraph(t, ctx, spec, GraphOpts{
		SinkDb:          db,
		MachinesPerNode: 1,
		Quiet:           true,
		NoRandomTags:    true,
		WaitTimeout:     30 * time.Second,
	})
	addr, err := f.Sink.Net.StartDbHttpServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr

	f.AddState(ssrel.B)
	for _, mach := range f.Machines {
		<-mach.When1(ssrel.B, ctx)
	}
	for _, n := range f.Nodes {
		if n == f.Sink {
			continue
		}
		if got := len(n.Net.Sinks()); got != 1 {
			t.Fatalf("%s: %d sinks", n.ID, got)
		}
		if parent := f.ByID[n.ParentID]; parent != nil &&
			!slices.Contains(n.Net.Sinks(), parent.Net.Addr) {
			t.Fatalf("%s: sink %v, want %s", n.ID, n.Net.Sinks(),
				parent.Net.Addr)
		}
	}
	f.WaitParents()
	f.WaitSink()

	f.StartDiscovery(DiscoveryOpts{SinkOnly: true})
	f.WaitDiscovery(60 * time.Second)

	deep := f.Machines[len(f.Machines)-1]
	deepID := deep.Id()

	waitActive := func(active bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			rows, err := queryGql(base, fmt.Sprintf(
				"MATCH (m:Machine {id: '%s'})-[:HAS]->(s:State) "+
					"WHERE s.name = '%s' AND s.active = %t "+
					"RETURN m.id AS id", deepID, ssrel.B, active))
			if err == nil && len(rows) > 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s active=%t in GQL: %v",
					deepID, active, err)
			}
			time.Sleep(time.Millisecond)
		}
	}

	waitActive(true)

	var samples []time.Duration
	for i := range 5 {
		start := time.Now()
		deep.Remove1(ssrel.B, nil)
		<-deep.WhenNot1(ssrel.B, ctx)
		waitActive(false)
		removed := time.Since(start)

		start = time.Now()
		deep.Add1(ssrel.B, nil)
		<-deep.When1(ssrel.B, ctx)
		waitActive(true)
		added := time.Since(start)

		samples = append(samples, removed, added)
		t.Logf("round %d: remove %s, add %s", i+1, removed, added)
	}
	slices.Sort(samples)
	t.Logf("depth=%d nodes=%d deep state change -> GQL: min=%s p50=%s max=%s",
		depth, len(f.Nodes), samples[0], samples[len(samples)/2],
		samples[len(samples)-1])
}

// scaleNodeAddr returns a unique loopback address for a node. The loopback /8
// is local on Linux, so 1M nodes do not exhaust the ephemeral port range.
func scaleNodeAddr(i int) string {
	if i == 0 {
		return "127.0.0.1:0"
	}
	return fmt.Sprintf("%s:%d", scaleNodeIP(i), scalePort)
}

// scaleSinkAddr returns the unique local address for a node's outgoing sinks.
// Port 0 picks an ephemeral port per unique IP, avoiding the global range.
func scaleSinkAddr(i int) string {
	if i == 0 {
		return ""
	}
	return scaleNodeIP(i) + ":0"
}

func scaleNodeIP(i int) string {
	n := i - 1
	c := 1 + n%254
	b := (n / 254) % 256
	a := 1 + (n/(254*256))%255
	return fmt.Sprintf("127.%d.%d.%d", a, b, c)
}

func scaleNodes() int {
	nodes := scaleDefaultNodes
	if v := os.Getenv("AM_NETGRAPH_NODES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 3 {
			nodes = n
		}
	}
	return nodes
}

func scaleIters() int {
	iters := 10
	if v := os.Getenv("AM_NETGRAPH_ITERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			iters = n
		}
	}
	return iters
}

// requireScale skips the test when the process lacks fds or memory.
func requireScale(t *testing.T, nodes int) {
	t.Helper()

	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		t.Fatal(err)
	}
	need := uint64(nodes*2 + 1024)
	if lim.Cur < need {
		t.Skipf("need ulimit -n >= %d (listener+sink per node), have %d; "+
			"run: ulimit -n %d", need, lim.Cur, need)
	}

	avail := memAvailableGB()
	minGB := 8.0 + float64(nodes)/50000.0
	if avail > 0 && avail < minGB {
		t.Skipf("need ~%.0fGB available, have %.1fGB", minGB, avail)
	}
	t.Logf("fd limit=%d, available=%.1fGB, need>=%.0fGB",
		lim.Cur, avail, minGB)
}

func memAvailableGB() float64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0
		}
		return kb / 1024 / 1024
	}
	return 0
}
