//go:build bench_netgraph && linux

package test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"gorm.io/gorm"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	"github.com/pancsta/asyncmachine-go/pkg/graph/gochickpeas"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/types"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	amhistgorm "github.com/pancsta/asyncmachine-go/pkg/history/gorm"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

const (
	// scaleDefaultNodes is a perfect binary tree of depth 19 (2^20-1).
	scaleDefaultNodes = 1<<20 - 1
	// scalePort is the fixed UDP port; uniqueness comes from the loopback IP.
	scalePort = 7000
)

// TestNetworkGraphFullUDP builds a real NetworkGraph per node via the public
// API and measures a deep state change until it is visible in the final sink's
// GQL DB. It is gated behind the "bench_netgraph" tag and needs ~2 fds per node
// (listener + EmitToAddr sink).
func TestNetworkGraphFullUdp(t *testing.T) {
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

	nets := make([]*amgraph.NetworkGraph, nodes)
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
	root, err := amgraph.NewNetworkGraph(ctx, scaleNodeAddr(0), rootG,
		amgraph.NetworkGraphOpts{
			Db:            db,
			PushInterval:  NetworkGraphPushInterval,
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

	// real NetworkGraph per node, 10ms push only on the active path
	for i := 1; i < nodes; i++ {
		g, err := amgraph.New(parentMach)
		if err != nil {
			t.Fatal(err)
		}
		interval := time.Hour
		if path[i] {
			interval = NetworkGraphPushInterval
		}
		ng, err := amgraph.NewNetworkGraph(ctx, scaleNodeAddr(i), g,
			amgraph.NetworkGraphOpts{
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

// TestNetworkGraphDeep runs the production NetworkGraph/RPC discovery path on
// a deeper live tree with a single machine per node.
func TestNetworkGraphDeep(t *testing.T) {
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

// TestNetworkGraphMockHistory runs an in-memory, lossy NetworkGraph star and
// measures the graph mirror and a sharded GORM history at scale. The default is
// 1M observed machines / 1M histories; see the AM_NETGRAPH_* env vars.
//
// Machines are registered once, then AM_NETGRAPH_ACTIVE machines get random
// state changes 4x/second for AM_NETGRAPH_DUR. The run is repeated with and
// without history attached, sequentially.
func TestNetworkGraphMockHistory(t *testing.T) {
	star := mockStar()
	perNode := (mockMachines() + star - 1) / star
	machines := perNode * star
	requireMockScale(t, machines)

	base := mockOpts{
		star:     star,
		perNode:  perNode,
		shards:   mockShards(),
		dur:      mockDuration(),
		active:   mockActive(machines),
		loss:     mockLoss(),
		regBatch: mockRegBatch(),
		regPause: mockRegPause(),
	}

	var runs []*mockRun
	for _, withHistory := range []bool{false, true} {
		o := base
		o.withHistory = withHistory
		o.name = "no-history"
		if withHistory {
			o.name = fmt.Sprintf("history-%d", o.shards)
		}

		t.Run(o.name, func(t *testing.T) {
			runs = append(runs, runMockHistory(t, o))
		})
	}

	t.Logf("comparison (machines=%d loss=%.2f active=%d):", machines,
		base.loss, base.active)
	for _, r := range runs {
		t.Logf("  %-11s reg=%s load=%s txs=%d rate=%.0f/s mirror=%d "+
			"stale=%d lag=%d dropped=%d alloc=%dMB sys=%dMB tracked=%d db=%.1fMB",
			r.name, r.reg.Round(time.Millisecond),
			r.load.Round(time.Millisecond), r.txs, r.rate, r.mirrored,
			r.stale, r.lag, r.dropped, r.allocMB, r.sysMB, r.tracked, r.dbMB)
	}
}

type mockOpts struct {
	name        string
	star        int
	perNode     int
	shards      int
	dur         time.Duration
	active      int
	loss        float64
	regBatch    int
	regPause    time.Duration
	withHistory bool
	seed        uint64
}

type mockRun struct {
	name     string
	machines int
	reg      time.Duration
	load     time.Duration
	txs      int64
	rate     float64

	mirrored int
	stale    int
	lag      uint64

	sent    int64
	dropped int64

	allocMB    uint64
	sysMB      uint64
	totalMB    uint64
	gc         uint32
	goroutines int

	shardCounts []int
	dbMB        float64
	tracked     int
	histErrs    int64
	histSample  int
	histOK      bool
}

func runMockHistory(t *testing.T, o mockOpts) *mockRun {
	t.Helper()
	ctx := t.Context()

	// no loss while registering: a lost single registration update would
	// leave a machine unobserved and without a history
	hub := newMockNet(o.seed, 0)
	f := NewMachineGraph(t, ctx, TreeSpec(1, o.star), GraphOpts{
		MachinesPerNode: o.perNode,
		Quiet:           true,
		NoRandomTags:    true,
		WaitTimeout:     30 * time.Minute,
		NetOpts: amgraph.NetworkGraphOpts{
			Listener: hub.Listen,
			Dial:     hub.Dial,
		},
	})
	machines := len(f.Machines)

	// lossy runs would flood the log with resend warnings
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, n := range f.Nodes {
		n.Net.Logger = quiet
	}

	var hg *amgraph.HistoryGraph
	var dbPaths []string
	var histErrs atomic.Int64
	if o.withHistory {
		dbs := make([]*gorm.DB, o.shards)
		for i := range dbs {
			path := filepath.Join(t.TempDir(), fmt.Sprintf("hist-%d", i))
			db, sqlDb, err := amhistgorm.NewDb(path, false)
			if err != nil {
				t.Fatal(err)
			}
			sqlDb.SetMaxOpenConns(1)
			dbs[i] = db
			dbPaths = append(dbPaths, path+".sqlite")
			t.Cleanup(func() { _ = sqlDb.Close() })
		}

		var err error
		hg, err = amgraph.NewHistoryGraph(f.Sink.Graph, dbs, amhistgorm.Config{
			BaseConfig: amhist.BaseConfig{StoreTransitions: true},
			QueueBatch: 64,
			SavePool:   1,
		}, amgraph.HistoryGraphOpts{
			OnErr: func(err error) {
				if n := histErrs.Add(1); n <= 3 {
					t.Logf("history: %v", err)
				}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = hg.Close() })
	}

	// register every machine at the sink, in paced batches
	regStart := time.Now()
	for start := 0; start < machines; start += o.regBatch {
		end := min(start+o.regBatch, machines)
		mutateMachines(t, f.Machines[start:end])
		waitMockMirror(t, f, end, 30*time.Minute)
		time.Sleep(o.regPause)
	}
	regDur := time.Since(regStart)
	if hg != nil {
		waitMockTracked(t, hg, machines, 30*time.Minute)
	}

	runtime.GC()
	var ms0, ms runtime.MemStats
	runtime.ReadMemStats(&ms0)

	// measured load, with loss
	hub.SetLoss(o.loss)
	loadStart := time.Now()
	txs := runMockLoad(t, f.Machines, o.active, o.dur, o.seed)
	loadDur := time.Since(loadStart)

	// let the network queues drain
	time.Sleep(2 * time.Second)

	// sample histories before stopping the network
	var histSample int
	var histOK bool
	if hg != nil {
		histSample, histOK = sampleHistories(t, ctx, hg, f)
	}

	// stop the network, so the sink graph stops changing
	for _, n := range f.Nodes {
		_ = n.Net.Close()
	}

	runtime.GC()
	runtime.ReadMemStats(&ms)
	goroutines := runtime.NumGoroutine()

	mirrored := f.Sink.Graph.MachineCount()
	stale, lag := 0, uint64(0)
	for _, m := range f.Machines {
		src := m.Time(nil).Sum(nil)
		c, ok := f.Sink.Graph.Machine(m.Id())
		if !ok || c == nil {
			stale++
			lag += src
			continue
		}
		if c.MTimeSum != src {
			stale++
			if c.MTimeSum < src {
				lag += src - c.MTimeSum
			}
		}
	}

	sent, dropped := hub.Stats()
	r := &mockRun{
		name:       o.name,
		machines:   machines,
		reg:        regDur,
		load:       loadDur,
		txs:        txs,
		rate:       float64(txs) / loadDur.Seconds(),
		mirrored:   mirrored,
		stale:      stale,
		lag:        lag,
		sent:       sent,
		dropped:    dropped,
		allocMB:    ms.Alloc >> 20,
		sysMB:      ms.Sys >> 20,
		totalMB:    (ms.TotalAlloc - ms0.TotalAlloc) >> 20,
		gc:         ms.NumGC - ms0.NumGC,
		goroutines: goroutines,
		histErrs:   histErrs.Load(),
		histSample: histSample,
		histOK:     histOK,
	}
	if hg != nil {
		r.tracked = hg.TrackedCount()
		r.shardCounts = hg.ShardCounts()
		for _, path := range dbPaths {
			if fi, err := os.Stat(path); err == nil {
				r.dbMB += float64(fi.Size()) / (1 << 20)
			}
		}
	}

	t.Logf("%s: machines=%d reg=%s load=%s txs=%d rate=%.0f/s", r.name,
		r.machines, r.reg.Round(time.Millisecond),
		r.load.Round(time.Millisecond), r.txs, r.rate)
	t.Logf("%s: mirror=%d/%d stale=%d lag=%d | mock sent=%d dropped=%d (%.2f%%)",
		r.name, r.mirrored, r.machines, r.stale, r.lag, r.sent, r.dropped,
		float64(r.dropped)/float64(max(r.sent, 1))*100)
	t.Logf("%s: runtime alloc=%dMB sys=%dMB totalAlloc=%dMB gc=%d "+
		"goroutines=%d", r.name, r.allocMB, r.sysMB, r.totalMB, r.gc,
		r.goroutines)
	if hg != nil {
		t.Logf("%s: history tracked=%d errors=%d shards=%v db=%.1fMB "+
			"sample=%d ok=%t", r.name, r.tracked, r.histErrs, r.shardCounts,
			r.dbMB, r.histSample, r.histOK)
	}

	// assertions
	if o.loss > 0 && r.dropped == 0 {
		t.Fatalf("no datagrams dropped")
	}
	if r.mirrored != r.machines {
		t.Fatalf("mirror %d/%d", r.mirrored, r.machines)
	}
	if hg != nil {
		if r.tracked != r.machines {
			t.Fatalf("history tracked %d/%d", r.tracked, r.machines)
		}
		if r.histErrs != 0 {
			t.Fatalf("history errors: %d", r.histErrs)
		}
		if !r.histOK {
			t.Fatalf("history sample: %d records", r.histSample)
		}
		lo, hi := shardBounds(r.shardCounts)
		if hi-lo > 1 {
			t.Fatalf("unbalanced shards: %v", r.shardCounts)
		}
	}

	return r
}

// mutateMachines toggles state B on all machines, max 256 at a time.
func mutateMachines(t *testing.T, machs []*am.Machine) {
	t.Helper()

	sem := make(chan struct{}, 256)
	var wg sync.WaitGroup
	for _, m := range machs {
		sem <- struct{}{}
		wg.Add(1)
		go func(m *am.Machine) {
			defer wg.Done()
			defer func() { <-sem }()

			res := m.Toggle1(ssrel.B, nil)
			if res >= am.Queued {
				<-m.WhenQueue(res)
			}
		}(m)
	}
	wg.Wait()
}

// runMockLoad toggles random states on the active machines 4x/second.
func runMockLoad(
	t *testing.T, machs []*am.Machine, active int, dur time.Duration,
	seed uint64,
) int64 {
	t.Helper()

	rnd := rand.New(rand.NewPCG(seed, seed^0x5851f42d4c957f2d))
	order := rnd.Perm(len(machs))[:min(active, len(machs))]
	names := []string{ssrel.B, ssrel.C, ssrel.D}
	ticks := int(dur / (time.Second / 4))
	if ticks < 1 {
		ticks = 1
	}

	ticker := time.NewTicker(time.Second / 4)
	defer ticker.Stop()

	var txs int64
	for range ticks {
		<-ticker.C

		waits := make([]<-chan struct{}, 0, len(order))
		for _, idx := range order {
			m := machs[idx]
			res := m.Toggle1(names[rnd.IntN(len(names))], nil)
			if res >= am.Queued {
				waits = append(waits, m.WhenQueue(res))
			}
			txs++
		}
		for _, w := range waits {
			<-w
		}
	}

	return txs
}

// sampleHistories asserts that a few current machines have a history.
func sampleHistories(
	t *testing.T, ctx context.Context, hg *amgraph.HistoryGraph,
	f *MachineGraph,
) (records int, ok bool) {
	t.Helper()

	for i, m := range f.Machines {
		if i >= 1000 {
			break
		}
		c, found := f.Sink.Graph.Machine(m.Id())
		if !found || c == nil || c.MTimeSum != m.Time(nil).Sum(nil) {
			// stale machines can miss their last records
			continue
		}

		resp, err := hg.History(ctx, m.Id(), amtypes.HistFindLatest,
			amgraph.HistParams{Limit: 1})
		if err != nil {
			t.Logf("history sample %s: %v", m.Id(), err)
			continue
		}

		return len(resp.Records),
			len(resp.Records) > 0 && resp.Records[0].Transition != nil
	}

	return 0, false
}

// waitMockMirror waits until the sink mirrors n machines.
func waitMockMirror(
	t *testing.T, f *MachineGraph, n int, timeout time.Duration,
) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if got := f.Sink.Graph.MachineCount(); got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("mirror %d/%d machines",
				f.Sink.Graph.MachineCount(), n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitMockTracked waits until hg has n registered machines.
func waitMockTracked(
	t *testing.T, hg *amgraph.HistoryGraph, n int, timeout time.Duration,
) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if got := hg.TrackedCount(); got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("history tracked %d/%d machines", hg.TrackedCount(), n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// requireMockScale skips the test when the host lacks RAM or disk for the
// configured number of machines. Set AM_NETGRAPH_FORCE=1 to override.
func requireMockScale(t *testing.T, machines int) {
	t.Helper()
	if os.Getenv("AM_NETGRAPH_FORCE") != "" {
		return
	}

	avail := memAvailableGB()
	minGB := 8.0 + float64(machines)/50000.0
	if avail > 0 && avail < minGB {
		t.Skipf("need ~%.0fGB available for %d machines, have %.1fGB; "+
			"set AM_NETGRAPH_FORCE=1 to override", minGB, machines, avail)
	}

	var st syscall.Statfs_t
	if err := syscall.Statfs(os.TempDir(), &st); err != nil {
		return
	}
	freeGB := float64(st.Bavail) * float64(st.Bsize) / (1 << 30)
	needGB := 8.0 + float64(machines)/200000.0
	if freeGB < needGB {
		t.Skipf("need ~%.0fGB free in %s, have %.1fGB; "+
			"set AM_NETGRAPH_FORCE=1 to override", needGB, os.TempDir(), freeGB)
	}
}

func shardBounds(counts []int) (lo, hi int) {
	if len(counts) == 0 {
		return 0, 0
	}

	lo, hi = counts[0], counts[0]
	for _, n := range counts[1:] {
		lo = min(lo, n)
		hi = max(hi, n)
	}

	return lo, hi
}

func mockStar() int {
	n := 16
	if v := os.Getenv("AM_NETGRAPH_STAR"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1 {
			n = x
		}
	}

	return n
}

func mockMachines() int {
	n := 1 << 20
	if v := os.Getenv("AM_NETGRAPH_MACHINES"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x > 0 {
			n = x
		}
	}

	return n
}

func mockShards() int {
	n := 8
	if v := os.Getenv("AM_NETGRAPH_SHARDS"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1 && x <= 255 {
			n = x
		}
	}

	return n
}

func mockDuration() time.Duration {
	d := time.Minute
	if v := os.Getenv("AM_NETGRAPH_DUR"); v != "" {
		if x, err := time.ParseDuration(v); err == nil && x > 0 {
			d = x
		}
	}

	return d
}

func mockActive(machines int) int {
	n := 10000
	if v := os.Getenv("AM_NETGRAPH_ACTIVE"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x > 0 {
			n = x
		}
	}

	return min(n, machines)
}

func mockLoss() float64 {
	loss := 0.05
	if v := os.Getenv("AM_NETGRAPH_LOSS"); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x >= 0 && x < 1 {
			loss = x
		}
	}

	return loss
}

func mockRegBatch() int {
	n := 50000
	if v := os.Getenv("AM_NETGRAPH_REG_BATCH"); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1 {
			n = x
		}
	}

	return n
}

func mockRegPause() time.Duration {
	d := 20 * time.Millisecond
	if v := os.Getenv("AM_NETGRAPH_REG_PAUSE"); v != "" {
		if x, err := time.ParseDuration(v); err == nil && x >= 0 {
			d = x
		}
	}

	return d
}
