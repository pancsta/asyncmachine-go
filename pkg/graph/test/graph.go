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
	"testing"
	"time"

	ssrel "github.com/pancsta/asyncmachine-go/internal/testing/states"
	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// ///// ///// /////

// ///// MACHINE GRAPH

// ///// ///// /////

// MachinesPerNode is the number of source machines per graph node.
const MachinesPerNode = 4

// MachineIds returns the machine IDs owned by a graph node.
func MachineIds(nodeId string) []string {
	res := make([]string, 0, MachinesPerNode)
	for i := 1; i <= MachinesPerNode; i++ {
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

// NetNode is one NetGraph node with locally traced machines.
type NetNode struct {
	ID       string
	Machines []*am.Machine
	Server   *am.Machine
	Graph    *amgraph.Graph
	Net      *amgraph.NetGraph
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
}

// NewMachineGraph creates the supplied machine topology over UDP.
func NewMachineGraph(
	t *testing.T, ctx context.Context, specText string,
) *MachineGraph {
	t.Helper()

	spec := parseSpec(t, specText)
	f := &MachineGraph{
		T:             t,
		ByID:          make(map[string]*NetNode),
		Children:      make(map[string][]*NetNode),
		RelayExpected: make(map[string][]*am.Machine),
		NodeMachines:  make(map[string][]*am.Machine),
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
		allMachines = append(allMachines, MachineIds(node.ID)...)
	}

	newNode := func(id string, machineCount int) *NetNode {
		n := &NetNode{ID: id}
		for _, machID := range MachineIds(id)[:machineCount] {
			mach := am.New(ctx, ssrel.States, &am.Opts{Id: machID})
			if err := mach.VerifyStates(ssrel.Names); err != nil {
				t.Fatal(err)
			}

			// simulate a random RPC conn via a src-id tag, like real RPC
			// clients (pkg/rpc/rpc_client.go)
			if target := allMachines[rand.IntN(len(allMachines))]; target != machID &&
				rand.IntN(2) == 0 {
				mach.SetTags([]string{ssrpc.TagKeySrcId + ":" + target})
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
		n.Net, err = amgraph.NewNetGraph(ctx, "127.0.0.1:0", n.Graph)
		if err != nil {
			t.Fatal(err)
		}
		n.Net.PushInterval = 10 * time.Millisecond
		f.Nodes = append(f.Nodes, n)
		f.ByID[id] = n
		f.NodeMachines[id] = n.Machines
		f.Machines = append(f.Machines, n.Machines...)
		return n
	}

	f.Sink = newNode(spec.Root, 0)

	for _, n := range spec.Nodes {
		newNode(n.ID, MachinesPerNode)
	}

	for _, n := range spec.Nodes {
		node := f.ByID[n.ID]
		parent := f.ByID[n.Parent]
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

	printTopology(f)

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

func (f *MachineGraph) wait(
	g *amgraph.Graph, machines []*am.Machine, readyFn func(*amgraph.Machine) bool,
) {
	f.T.Helper()

	deadline := time.Now().Add(3 * time.Second)
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
