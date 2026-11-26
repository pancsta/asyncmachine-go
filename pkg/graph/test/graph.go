package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// ///// ///// /////

// ///// MACHINE GRAPH

// ///// ///// /////

// MachinesPerNode is the number of source machines per graph node.
const MachinesPerNode = 4

// NetGraphPushInterval is the graph sync interval used by the simulation.
const NetGraphPushInterval = 10 * time.Millisecond

// MachineIds returns the machine IDs owned by a graph node.
func MachineIds(nodeId string) []string {
	return machineIds(nodeId, MachinesPerNode)
}

// machineIds returns count machine IDs owned by a graph node.
func machineIds(nodeId string, count int) []string {
	res := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		res = append(res, fmt.Sprintf("%s-mach-%d", nodeId, i))
	}
	return res
}

const DefaultGraphSpec = `
sink

node-1
node-2
node-3
node-4

node-1-1
node-1-2
node-1-3
node-1-4

node-2-1
node-2-2
node-2-3
node-2-4

node-3-1
node-3-2
node-3-3
node-3-4

node-4-1
node-4-2
node-4-3
node-4-4
`

// Node is a parsed Graph node.
type Node struct {
	ID       string
	Parent   string
	Children []string
}

// Graph is a parsed Graph specification.
type Graph struct {
	Root     string
	Nodes    []Node
	ByID     map[string]*Node
	Children map[string][]string
}

func parseSpec(t *testing.T, specText string) Graph {
	t.Helper()

	var ids []string
	seen := make(map[string]struct{})
	for _, line := range strings.Split(specText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			t.Fatalf("duplicate node ID %s", line)
		}
		seen[line] = struct{}{}
		ids = append(ids, line)
	}
	if len(ids) < 2 {
		t.Fatalf("Graph spec needs at least a root and one node")
	}

	g := Graph{
		Root:     ids[0],
		ByID:     make(map[string]*Node),
		Children: make(map[string][]string),
	}
	g.ByID[g.Root] = &Node{ID: g.Root}

	for _, id := range ids[1:] {
		parent := g.Root
		if i := strings.LastIndex(id, "-"); i > 0 {
			candidate := id[:i]
			if _, ok := seen[candidate]; ok {
				parent = candidate
			}
		}
		g.Nodes = append(g.Nodes, Node{ID: id, Parent: parent})
		g.ByID[id] = &g.Nodes[len(g.Nodes)-1]
		g.Children[parent] = append(g.Children[parent], id)
	}

	return g
}

// TreeSpec returns a deterministic tree spec with the given depth and fanout.
// The root is "sink"; children are IDs like "node-1", "node-1-2", which
// [parseSpec] links by trimming the last "-" segment.
func TreeSpec(depth, fanout int) string {
	var b strings.Builder
	b.WriteString("sink\n")

	level := []string{"node"}
	for range depth {
		next := make([]string, 0, len(level)*fanout)
		for _, parent := range level {
			for i := 1; i <= fanout; i++ {
				id := fmt.Sprintf("%s-%d", parent, i)
				b.WriteString(id + "\n")
				next = append(next, id)
			}
		}
		level = next
	}

	return b.String()
}

// NetNode is one NetGraph node with locally traced machines.
type NetNode struct {
	ID       string
	ParentID string
	Machines []*am.Machine
	Server   *am.Machine
	Graph    *amgraph.Graph
	Net      *amgraph.NetGraph

	// discovered maps source machine IDs already dialed by this node.
	discovered map[string]struct{}
}

// MachineGraph is a reusable multi-node NetGraph test fixture.
type MachineGraph struct {
	T             *testing.T
	Nodes         []*NetNode
	ByID          map[string]*NetNode
	Sink          *NetNode
	Parents       []string
	Children      map[string][]*NetNode
	RelayExpected map[string][]*am.Machine
	NodeMachines  map[string][]*am.Machine
	Machines      []*am.Machine
	Idx           int
	waitTimeout   time.Duration

	ctx              context.Context
	Servers          map[string]*arpc.Server
	Clients          []*arpc.Client
	discoverySources map[string][]string
	discoveryWant    int
	discoveryMx      sync.Mutex
	discoveryWg      sync.WaitGroup
}

// DiscoveryOpts configures [MachineGraph.StartDiscovery].
type DiscoveryOpts struct {
	// SinkOnly runs discovery only on the sink, dialing every source machine
	// visible to it.
	SinkOnly bool
}

// GraphOpts configures [NewMachineGraph].
type GraphOpts struct {
	// SinkDb mirrors the sink's graph into a DB from the start.
	SinkDb amtypes.DbGraph
	// MachinesPerNode is the number of source machines per node. Defaults to
	// [MachinesPerNode].
	MachinesPerNode int
	// Quiet disables printing the topology for large graphs.
	Quiet bool
	// NoRandomTags disables random src-id tags on source machines.
	NoRandomTags bool
	// WaitTimeout is the graph sync wait timeout. Defaults to 3s.
	WaitTimeout time.Duration
}

// NewMachineGraph creates the supplied machine topology over UDP.
func NewMachineGraph(
	t *testing.T, ctx context.Context, specText string, opts ...GraphOpts,
) *MachineGraph {
	t.Helper()

	o := GraphOpts{
		MachinesPerNode: MachinesPerNode,
		WaitTimeout:     3 * time.Second,
	}
	if len(opts) > 0 {
		o = opts[0]
		if o.MachinesPerNode == 0 {
			o.MachinesPerNode = MachinesPerNode
		}
		if o.WaitTimeout == 0 {
			o.WaitTimeout = 3 * time.Second
		}
	}

	spec := parseSpec(t, specText)
	f := &MachineGraph{
		T:             t,
		ByID:          make(map[string]*NetNode),
		Children:      make(map[string][]*NetNode),
		RelayExpected: make(map[string][]*am.Machine),
		NodeMachines:  make(map[string][]*am.Machine),
		ctx:           ctx,
		waitTimeout:   o.WaitTimeout,
	}
	t.Cleanup(func() {
		for i := len(f.Nodes) - 1; i >= 0; i-- {
			_ = f.Nodes[i].Net.Close()
			for _, mach := range f.Nodes[i].Machines {
				mach.Dispose()
			}
			f.Nodes[i].Server.Dispose()
		}
	})

	var allMachines []string
	for _, node := range spec.Nodes {
		allMachines = append(allMachines, machineIds(node.ID, o.MachinesPerNode)...)
	}

	newNode := func(id string, machineCount int) *NetNode {
		n := &NetNode{
			ID:         id,
			discovered: make(map[string]struct{}),
		}
		for _, machID := range machineIds(id, machineCount) {
			mach := am.New(ctx, ssrel.States, &am.Opts{Id: machID})
			if err := mach.VerifyStates(ssrel.Names); err != nil {
				t.Fatal(err)
			}

			// simulate a random RPC conn via a src-id tag, like real RPC
			// clients (pkg/rpc/rpc_client.go)
			if !o.NoRandomTags {
				if target := allMachines[rand.IntN(len(allMachines))]; target != machID &&
					rand.IntN(2) == 0 {
					mach.SetTags([]string{ssrpc.TagKeySrcId + ":" + target})
				}
			}

			n.Machines = append(n.Machines, mach)
		}
		// TODO ssdbg.ServerSchema unnecessary, StateErrGraph only needed
		n.Server = am.New(ctx, ssdbg.ServerSchema, &am.Opts{Id: "srv-" + id})
		var err error
		n.Graph, err = amgraph.New(n.Server)
		if err != nil {
			t.Fatal(err)
		}
		// mirror the sink into the DB from the start, so no late attach races
		var db amtypes.DbGraph
		if id == spec.Root {
			db = o.SinkDb
		}
		n.Net, err = amgraph.NewNetGraph(ctx, "127.0.0.1:0", n.Graph,
			amgraph.NetGraphOpts{
				Db:           db,
				PushInterval: NetGraphPushInterval,
			})
		if err != nil {
			t.Fatal(err)
		}
		f.Nodes = append(f.Nodes, n)
		f.ByID[id] = n
		f.NodeMachines[id] = n.Machines
		f.Machines = append(f.Machines, n.Machines...)
		return n
	}

	f.Sink = newNode(spec.Root, 0)

	for _, n := range spec.Nodes {
		newNode(n.ID, o.MachinesPerNode)
	}

	for _, n := range spec.Nodes {
		node := f.ByID[n.ID]
		parent := f.ByID[n.Parent]
		node.ParentID = n.Parent
		node.Net.EmitToAddr(parent.Net.Addr)
		f.Children[parent.ID] = append(f.Children[parent.ID], node)
		if len(spec.Children[n.ID]) > 0 {
			f.Parents = append(f.Parents, n.ID)
		}
	}

	for _, parentID := range f.Parents {
		for _, child := range f.Children[parentID] {
			f.RelayExpected[parentID] = append(f.RelayExpected[parentID],
				child.Machines...)
		}
	}

	schemaHash := amhelp.SchemaHash(f.Machines[0].Schema())
	schema := amgraph.SchemaInfo{
		Schema:      f.Machines[0].Schema(),
		StatesIndex: f.Machines[0].StateNames(),
	}
	for _, n := range f.Nodes {
		n.Net.SchemaStore[schemaHash] = schema
	}

	if !o.Quiet && len(f.Nodes) <= 64 {
		printTopology(f)
	}

	for _, n := range f.Nodes {
		for _, mach := range n.Machines {
			if _, err := n.Net.TraceMach(mach); err != nil {
				t.Fatal(err)
			}
		}
	}

	idx := slices.Index(f.Machines[0].StateNames(), ssrel.B)
	if idx < 0 {
		t.Fatalf("state %s missing", ssrel.B)
	}
	f.Idx = idx

	return f
}

// AddState applies a state to every traced machine.
func (f *MachineGraph) AddState(state string) {
	for _, mach := range f.Machines {
		mach.Add1(state, nil)
	}
}

// WaitParents waits until every forwarding node mirrors its children.
func (f *MachineGraph) WaitParents() {
	f.T.Helper()
	for _, parentID := range f.Parents {
		f.WaitGraph(f.ByID[parentID].Graph, f.RelayExpected[parentID])
	}
}

// WaitSink waits until the sink mirrors all machines.
func (f *MachineGraph) WaitSink() {
	f.T.Helper()
	f.WaitGraph(f.Sink.Graph, f.Machines)
}

// WaitGraph waits until all machines are present in a graph.
func (f *MachineGraph) WaitGraph(g *amgraph.Graph, machines []*am.Machine) {
	f.T.Helper()
	f.wait(g, machines, func(c *amgraph.Machine) bool {
		return c.Time.Is1(f.Idx)
	})
}

// WaitTags waits until a sink client reflects the expected tags.
func (f *MachineGraph) WaitTags(machID string, tags []string) {
	f.T.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if c, ok := f.Sink.Graph.Machine(machID); ok &&
			slices.Equal(c.Tags, tags) {
			return
		}
		if time.Now().After(deadline) {
			f.T.Fatalf("timed out waiting for tags of %s", machID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// WaitClients waits until all machines have any clock data in a graph.
func (f *MachineGraph) WaitClients(g *amgraph.Graph, machines []*am.Machine) {
	f.T.Helper()
	f.wait(g, machines, func(c *amgraph.Machine) bool {
		return c.MTimeSum > 0
	})
}

// ///// ///// /////

// ///// DISCOVERY

// ///// ///// /////

// StartDiscovery starts one RPC server per node, advertises it as an #addr
// tag, and dials every #addr discovered through the local NetGraph. Every
// connection is a real TCP RPC client, mirrored as a CONNECTED_TO edge.
func (f *MachineGraph) StartDiscovery(opts ...DiscoveryOpts) {
	f.T.Helper()

	o := DiscoveryOpts{}
	if len(opts) > 0 {
		o = opts[0]
	}

	ctx, cancel := context.WithCancel(f.ctx)
	f.Servers = make(map[string]*arpc.Server)

	// start the servers first, so all #addr tags can propagate at once
	for _, n := range f.Nodes {
		if len(n.Machines) == 0 {
			continue
		}
		src := n.Machines[0]
		srv, err := arpc.NewServer(ctx, "127.0.0.1:0", n.ID, src, nil)
		if err != nil {
			f.T.Fatal(err)
		}
		srv.Start(nil)
		select {
		case <-srv.Mach.When1(ssrpc.ServerStates.RpcReady, ctx):
		case <-time.After(5 * time.Second):
			f.T.Fatalf("rpc server %s did not start", n.ID)
		}
		// advertise the TCP listener on the traced source machine
		amhelp.TagSetOnMach(src, ssrpc.TagKeyAddr, srv.Addr)
		f.Servers[n.ID] = srv
	}

	// precompute the source machines visible to each node
	f.discoverySources = make(map[string][]string, len(f.Nodes))
	for _, n := range f.Nodes {
		f.discoverySources[n.ID] = f.subtreeSources(n.ID)
	}

	f.discoveryWant = 0
	for _, n := range f.Nodes {
		if o.SinkOnly && n != f.Sink {
			continue
		}
		f.discoveryWant += len(f.discoverySources[n.ID])
		f.discoveryWg.Add(1)
		go f.discoveryLoop(ctx, n)
	}

	f.T.Cleanup(func() {
		cancel()
		f.discoveryWg.Wait()
		for _, cl := range f.Clients {
			if cl.NetMach != nil {
				cl.Stop(nil, nil, true)
			} else {
				cl.Mach.Dispose()
			}
		}
		for _, srv := range f.Servers {
			srv.Stop(nil, true)
		}
	})
}

// WaitDiscovery waits until the configured scope dialed all its descendants.
func (f *MachineGraph) WaitDiscovery(timeout time.Duration) {
	f.T.Helper()

	want := f.discoveryWant
	if want == 0 {
		for _, ids := range f.discoverySources {
			want += len(ids)
		}
	}

	deadline := time.Now().Add(timeout)
	for {
		f.discoveryMx.Lock()
		got := len(f.Clients)
		f.discoveryMx.Unlock()
		if got >= want {
			f.T.Logf("discovered %d TCP RPC conns", got)
			return
		}
		if time.Now().After(deadline) {
			f.T.Fatalf("discovered %d/%d TCP RPC conns", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// subtreeSources returns the source machine IDs of all descendants of a node.
func (f *MachineGraph) subtreeSources(id string) []string {
	var res []string
	for _, child := range f.Children[id] {
		if len(child.Machines) > 0 {
			res = append(res, child.Machines[0].Id())
		}
		res = append(res, f.subtreeSources(child.ID)...)
	}
	return res
}

func (f *MachineGraph) discoveryLoop(ctx context.Context, n *NetNode) {
	defer f.discoveryWg.Done()

	ticker := time.NewTicker(NetGraphPushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, srcID := range f.discoverySources[n.ID] {
				f.dialSource(ctx, n, srcID)
			}
		}
	}
}

// dialSource connects to a source machine once its #addr tag is mirrored.
func (f *MachineGraph) dialSource(
	ctx context.Context, n *NetNode, srcID string,
) {
	if _, ok := n.discovered[srcID]; ok {
		return
	}

	src, ok := n.Graph.Machine(srcID)
	if !ok {
		return
	}
	addr := amhelp.TagValue(src.Tags, ssrpc.TagKeyAddr)
	if addr == "" {
		return
	}
	n.discovered[srcID] = struct{}{}

	cl, err := arpc.NewClient(ctx, addr, "cl-"+n.ID+"-"+srcID, src.Schema,
		&arpc.ClientOpts{Parent: n.Server})
	if err != nil {
		f.T.Logf("rpc client %s: %v", srcID, err)
		delete(n.discovered, srcID)
		return
	}
	cl.Start(nil)
	select {
	case <-cl.Mach.When1(ssrpc.ClientStates.HandshakeDone, ctx):
	case <-time.After(5 * time.Second):
		cl.Stop(nil, nil, true)
		delete(n.discovered, srcID)
		return
	case <-ctx.Done():
		return
	}

	// the netmach carries the src-id tag, which becomes a CONNECTED_TO edge.
	// Only the sink mirrors its clients: its graph is the GQL source and it has
	// no upstream to relay late-bound netmach baselines to.
	if n == f.Sink {
		if _, err := n.Net.TraceMach(cl.NetMach); err != nil {
			cl.Stop(nil, nil, true)
			delete(n.discovered, srcID)
			return
		}
	}

	f.discoveryMx.Lock()
	f.Clients = append(f.Clients, cl)
	f.discoveryMx.Unlock()
}

// queryGql runs a GQL query against a Db HTTP server and returns its rows.
func queryGql(base string, q string) ([]map[string]any, error) {
	resp, err := http.Post(base+"/query", "text/plain", strings.NewReader(q))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%d %s", resp.StatusCode, body)
	}

	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (f *MachineGraph) wait(
	g *amgraph.Graph, machines []*am.Machine, readyFn func(*amgraph.Machine) bool,
) {
	f.T.Helper()

	deadline := time.Now().Add(f.waitTimeout)
	for {
		ready := true
		var missing []string
		for _, mach := range machines {
			c, ok := g.Machine(mach.Id())
			if !ok || !readyFn(c) {
				ready = false
				missing = append(missing, mach.Id())
			}
		}
		if ready {
			return
		}
		if time.Now().After(deadline) {
			f.T.Fatalf("timed out waiting for graph updates: %v", missing)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// AssertSinkGraph validates the sink graph's machines, clocks, and relations.
func (f *MachineGraph) AssertSinkGraph() {
	f.T.Helper()

	inspect, err := f.Sink.Graph.Inspect()
	if err != nil {
		f.T.Fatal(err)
	}
	if len(inspect) != len(f.Machines) {
		f.T.Fatalf("expected %d machines in sink graph, got %d",
			len(f.Machines), len(inspect))
	}

	for _, mach := range f.Machines {
		info, ok := inspect[mach.Id()]
		if !ok {
			f.T.Fatalf("missing machine %s in sink graph", mach.Id())
		}
		if !slices.Equal(info.States, mach.StateNames()) {
			f.T.Fatalf("%s: state index mismatch: %v != %v",
				mach.Id(), info.States, mach.StateNames())
		}
		if len(info.Schema) != len(mach.Schema()) {
			f.T.Fatalf("%s: schema size mismatch", mach.Id())
		}
		if info.MTimeSum == 0 {
			f.T.Fatalf("%s: no clock updates in sink graph", mach.Id())
		}
	}

	machID := f.Machines[0].Id()
	conn, err := f.Sink.Graph.Connection(machID, machID+":"+ssrel.A)
	if err != nil || conn.Edge.MachHas == nil {
		f.T.Fatalf("%s: missing machine-state edge: %v", machID, err)
	}
	conn, err = f.Sink.Graph.Connection(
		machID+":"+ssrel.B, machID+":"+ssrel.C)
	if err != nil {
		f.T.Fatalf("%s: missing B->C relation: %v", machID, err)
	}
	found := false
	for _, rel := range conn.Edge.StateRelation {
		if rel != nil && rel.RelType == am.RelationAdd {
			found = true
			break
		}
	}
	if !found {
		f.T.Fatalf("%s: B->C is not an add relation", machID)
	}
}

func printTopology(f *MachineGraph) {
	var b strings.Builder
	b.WriteString(f.Sink.ID + "\n")
	for i, child := range f.Children[f.Sink.ID] {
		printNode(&b, f, child, "", i == len(f.Children[f.Sink.ID])-1)
	}
	print(b.String())
}

func printNode(
	b *strings.Builder, f *MachineGraph, node *NetNode, prefix string,
	last bool,
) {
	branch := "├── "
	childPrefix := prefix + "│   "
	if last {
		branch = "└── "
		childPrefix = prefix + "    "
	}
	b.WriteString(prefix + branch + node.ID)
	printMachines(b, node.Machines)
	b.WriteString("\n")

	children := f.Children[node.ID]
	for i, child := range children {
		printNode(b, f, child, childPrefix, i == len(children)-1)
	}
}

func printMachines(b *strings.Builder, machs []*am.Machine) {
	if len(machs) == 0 {
		return
	}
	fmt.Fprintf(b, " [%s..%s]", machs[0].Id(), machs[len(machs)-1].Id())
}

// ///// ///// /////

// ///// SUITE

// ///// ///// /////

// RunDbMirrorSuite attaches db to f's sink, serves it over HTTP+JSON, triggers
// a fresh update burst, and asserts the mirrored model against the fixture.
// It's backend-agnostic, so any [amtypes.DbGraph] implementation can run it.
func RunDbMirrorSuite(t *testing.T, f *MachineGraph, db amtypes.DbGraph) {
	t.Helper()

	// mirror the sink's graph
	f.Sink.Net.Db = db
	addr, err := f.Sink.Net.StartDbHttpServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + addr

	// trigger a fresh burst of updates, so every machine hits the DB
	f.AddState(ssrel.D)

	// time queries and print elapsed
	query := func(q string) []map[string]any {
		t.Helper()
		start := time.Now()

		resp, err := http.Post(base+"/query", "text/plain", strings.NewReader(q))
		if err != nil {
			t.Fatalf("query %q: %v", q, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("query %q: %d %s", q, resp.StatusCode, body)
		}
		var rows []map[string]any
		if err := json.Unmarshal(body, &rows); err != nil {
			t.Fatalf("query %q: %v: %s", q, err, body)
		}

		t.Logf("query %q: %s (%d rows)", q, time.Since(start), len(rows))
		return rows
	}

	wait := func(what string, fn func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			if fn() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// all machines mirrored
	deadline := time.Now().Add(20 * time.Second)
	for {
		rows := query("MATCH (m:Machine) RETURN m.id AS id")
		dbIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			if id, ok := row["id"].(string); ok {
				dbIDs = append(dbIDs, id)
			}
		}
		if len(dbIDs) == len(f.Machines) {
			break
		}
		if time.Now().After(deadline) {
			want := make([]string, 0, len(f.Machines))
			for _, m := range f.Machines {
				want = append(want, m.Id())
			}
			slices.Sort(want)
			slices.Sort(dbIDs)
			var missing []string
			for _, id := range want {
				if !slices.Contains(dbIDs, id) {
					missing = append(missing, id)
				}
			}
			t.Fatalf("mirror incomplete: %d/%d, missing %v", len(dbIDs),
				len(want), missing)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// states + relations per machine
	for _, mach := range f.Machines {
		lit := "'" + mach.Id() + "'"
		rows := query(fmt.Sprintf(
			"MATCH (s:State {mach: %s}) RETURN s.name AS name", lit))
		if len(rows) != len(ssrel.Names) {
			t.Fatalf("%s: %d states, want %d", mach.Id(), len(rows),
				len(ssrel.Names))
		}

		rows = query(fmt.Sprintf(
			"MATCH (m:Machine {id: %s})-[:HAS]->(s:State) RETURN s.name AS name",
			lit))
		if len(rows) != len(ssrel.Names) {
			t.Fatalf("%s: %d HAS edges, want %d", mach.Id(), len(rows),
				len(ssrel.Names))
		}
	}

	// RPC conns, as tagged by the fixture
	wantConns := 0
	for _, mach := range f.Machines {
		for _, tag := range mach.Tags() {
			if strings.HasPrefix(tag, ssrpc.TagKeySrcId+":") {
				wantConns++
				break
			}
		}
	}
	connDeadline := time.Now().Add(10 * time.Second)
	for {
		rows := query("MATCH (a)-[:CONNECTED_TO]->(b) RETURN a.id AS id")
		if len(rows) == wantConns {
			break
		}
		if time.Now().After(connDeadline) {
			tags := query("MATCH (t:Tag) RETURN t.mach AS mach, t.name AS name")
			t.Fatalf("rpc conns: %d/%d (tags: %d, sample: %v)", len(rows),
				wantConns, len(tags), tags[:min(len(tags), 5)])
		}
		time.Sleep(10 * time.Millisecond)
	}

	// serialized machine over HTTP, compared to the sink's mirror
	mach := f.Machines[0]
	wait("serialized clocks", func() bool {
		resp, err := http.Get(base + "/machine?id=" + mach.Id())
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK {
			return false
		}

		var ser am.Serialized
		if err := json.Unmarshal(body, &ser); err != nil {
			return false
		}
		if ser.ID != mach.Id() || !slices.Equal(ser.StateNames, ssrel.Names) {
			t.Fatalf("unexpected machine: %#v", ser)
		}
		dbTags := slices.Clone(ser.Tags)
		srcTags := slices.Clone(mach.Tags())
		slices.Sort(dbTags)
		slices.Sort(srcTags)
		if !slices.Equal(dbTags, srcTags) {
			t.Fatalf("%s: tags %v, want %v", mach.Id(), ser.Tags, mach.Tags())
		}

		mirror, ok := f.Sink.Graph.Machine(mach.Id())
		if !ok {
			return false
		}

		return slices.Equal(ser.Time, mirror.Time)
	})

	// error path
	resp, err := http.Post(base+"/query", "text/plain",
		strings.NewReader("NOPE ("))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad query: %d", resp.StatusCode)
	}
}
