// Package graph provides a graph or interconnected state-machines and their
// states, based on the dbg telemetry protocol.
package graph

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"

	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

var (
	ErrGraph      = errors.New("graph error")
	StateErrGraph = am.PrefixErr + "Graph"
)

// AddErrGraph adds [ErrGraph].
func AddErrGraph(
	event *am.Event, mach *am.Machine, err error, args ...am.A,
) am.Result {
	if err == nil {
		return am.Executed
	}
	err = fmt.Errorf("%w: %w", ErrGraph, err)

	return mach.EvAddErrState(event, ss.ErrGraph, err, am.OptArgs(args))
}

var ss = ssdbg.ServerStates

type Vertex struct {
	StateName string
	TagName   string
	MachId    string
}

type Edge = graph.Edge[*Vertex]

type EdgeData struct {
	// machine has a state
	MachHas *MachineHas
	// machine has an RPC connection to another machine
	MachConnectedTo bool
	// machine is a child of another machine
	MachChildOf bool
	// machine has pipes going to another machine
	MachPipesTo []*MachPipeTo
	// machine has a tag
	MachHasTag bool

	// state has relations with other states
	StateRelation []*StateRelation
}

// Client:
// - has State auto:bool multi:bool
// - connectedTo Client addr:string
// - pipeTo Client states:map[string]string
// - childOf Client
//
// State:
// - relation type:require|add|remove State
// - pipeTo Client|state add:bool

type MachineHas struct {
	Auto  bool
	Multi bool
}

type StateRelation struct {
	RelType am.Relation
}

type MachPipeTo struct {
	FromState string
	ToState   string
	MutType   am.MutationType
}

type Connection struct {
	Edge   *EdgeData
	Source *Vertex
	Target *Vertex
}

// Machine represents a single state machine withing the network graph.
type Machine = amtypes.Machine

type MachInspect struct {
	Child    string
	States   []string
	Conns    []string
	Pipes    map[string][]string
	Schema   am.Schema
	Tags     []string
	MTime    am.Time
	MTimeSum uint64
}

// ///// ///// /////

// ///// GRAPH

// ///// ///// /////

// Graph represents a network graph of state machines, states, and tags.
type Graph struct {
	ParentMach *am.Machine
	Machines   map[string]*Machine

	// G is a directed graph of machines and states with metadata.
	G graph.Graph[string, *Vertex]
	// Map is a unidirectional mirror of g, without metadata.
	Map graph.Graph[string, *Vertex]

	// lock for Clone(), called by a consumer
	loop sync.Mutex
}

func New(parentMach *am.Machine) (*Graph, error) {
	// TODO verify parentMach has ErrGraph

	g := &Graph{
		ParentMach: parentMach,
		G:          graph.New(hash, graph.Directed()),
		Map:        graph.New(hash),
		Machines:   make(map[string]*Machine),
	}

	// err := m.BindHandlers(g)
	// if err != nil {
	// 	return nil, err
	// }

	return g, nil
}

// Connection returns a Connection for the given source-target.
func (g *Graph) Connection(source, target string) (*Connection, error) {
	edge, err := g.G.Edge(source, target)
	if err != nil {
		return nil, err
	}
	data := edge.Properties.Data.(*EdgeData)
	targetVert, err := g.G.Vertex(target)
	if err != nil {
		return nil, err
	}
	sourceVert, err := g.G.Vertex(source)
	if err != nil {
		return nil, err
	}

	return &Connection{
		Edge:   data,
		Source: sourceVert,
		Target: targetVert,
	}, nil
}

func (g *Graph) AddMachine(gmach *Machine) error {
	c := gmach
	if c == nil {
		return nil
	}
	id := c.ID
	g.Machines[id] = c

	// add machine
	var err error
	if err = g.addMach(c.ID, c); err != nil {
		return err
	}

	// add tags
	tags := slices.Clone(c.Tags)
	c.Tags = nil
	g.setClientTags(c, tags)

	// parent
	if c.Parent != "" {
		err = g.addEdge(c.ID, c.Parent, &EdgeData{MachChildOf: true})
		if err != nil {
			return err
		}
		_ = g.Map.AddEdge(c.ID, c.Parent)
	}

	// add states
	for name, props := range c.Schema {
		// vertex
		err = g.G.AddVertex(&Vertex{
			MachId:    id,
			StateName: name,
		})
		if err != nil {
			return err
		}
		_ = g.Map.AddVertex(&Vertex{
			MachId:    id,
			StateName: name,
		})

		// edge
		err = g.G.AddEdge(id, id+":"+name, graph.EdgeData(&EdgeData{
			MachHas: &MachineHas{
				Auto:  props.Auto,
				Multi: props.Multi,
			},
		}))
		if err != nil {
			return err
		}
		_ = g.Map.AddEdge(id, id+":"+name)
	}

	// DEBUG
	// if c.ID == "rc-srv-browser2" {
	// 	ver, _ := g.G.Vertex("rc-srv-browser2")
	// 	ver = ver
	// 	print()
	// }

	type relation struct {
		states  am.S
		relType am.Relation
	}

	// add relations
	for name, state := range c.Schema {

		// define
		toAdd := []relation{
			{states: state.Require, relType: am.RelationRequire},
			{states: state.Add, relType: am.RelationAdd},
			{states: state.Remove, relType: am.RelationRemove},
		}

		// per relation
		for _, item := range toAdd {
			// per state
			for _, relState := range item.states {
				from := id + ":" + name
				to := id + ":" + relState

				// update an existing edge
				if edge, err := g.G.Edge(from, to); err == nil {
					data := edge.Properties.Data.(*EdgeData)
					data.StateRelation = append(data.StateRelation, &StateRelation{
						RelType: item.relType,
					})
					err = g.G.UpdateEdge(from, to, graph.EdgeData(data))
					if err != nil {
						return err
					}

					continue
				}

				// add if doesnt exist
				err = g.G.AddEdge(from, to, graph.EdgeData(&EdgeData{
					StateRelation: []*StateRelation{
						{RelType: item.relType},
					},
				}))
				if err != nil {
					return err
				}
				_ = g.Map.AddEdge(from, to)
			}
		}
	}

	// TODO add tags

	return nil
}

func (g *Graph) RemoveMachine(id string) error {
	g.loop.Lock()
	defer g.loop.Unlock()

	c, ok := g.Machines[id]
	if !ok {
		return nil
	}
	delete(g.Machines, id)

	adj, err := g.G.AdjacencyMap()
	if err != nil {
		return err
	}
	for _, edge := range adj[id] {
		_ = g.G.RemoveEdge(id, edge.Target)
		_ = g.Map.RemoveEdge(id, edge.Target)
	}
	pred, err := g.G.PredecessorMap()
	if err != nil {
		return err
	}
	for _, edge := range pred[id] {
		_ = g.G.RemoveEdge(edge.Source, id)
		_ = g.Map.RemoveEdge(edge.Source, id)
	}

	for name := range c.Schema {
		_ = g.G.RemoveVertex(id + ":" + name)
		_ = g.Map.RemoveVertex(id + ":" + name)
	}
	for _, tag := range c.Tags {
		g.removeTagVertex(id, tag)
	}
	_ = g.G.RemoveVertex(id)
	_ = g.Map.RemoveVertex(id)

	return nil
}

// RemoveTags removes all tag vertices and their edges of a machine, e.g. for
// render snapshots which don't need tags.
func (g *Graph) RemoveTags(id string) {
	g.loop.Lock()
	defer g.loop.Unlock()

	c, ok := g.Machines[id]
	if !ok || c == nil {
		return
	}
	for _, tag := range c.Tags {
		g.removeTagVertex(id, tag)
	}
}

// Clone returns a deep clone of the graph.
func (g *Graph) Clone() (*Graph, error) {
	// consumer lock
	g.loop.Lock()
	defer g.loop.Unlock()

	c1, err := g.G.Clone()
	if err != nil {
		return nil, err
	}

	c2, err := g.Map.Clone()
	if err != nil {
		return nil, err
	}

	g2 := &Graph{
		G:        c1,
		Map:      c2,
		Machines: make(map[string]*Machine, len(g.Machines)),
	}

	for id, c := range g.Machines {
		g2.Machines[id] = c.Clone()
	}

	return g2, nil
}

// Machine returns a copy of a client for safe concurrent reads.
func (g *Graph) Machine(id string) (*Machine, bool) {
	g.loop.Lock()
	defer g.loop.Unlock()

	c, ok := g.Machines[id]
	if !ok {
		return nil, false
	}
	return c.Clone(), true
}

func (g *Graph) Clear() {
	clear(g.Machines)
	g.G = graph.New(hash, graph.Directed())
	g.Map = graph.New(hash)
}

func (g *Graph) ParseMsgDbg(id string, msgTx *dbg.DbgMsgTx) {
	c := g.Machines[id]

	// TODO err
	if c == nil {
		return
	}

	var sum uint64
	for _, v := range msgTx.Clocks {
		sum += v
	}
	if msgTx.Tags != nil {
		g.setClientTags(c, msgTx.Tags)
	}
	index := c.StateNames

	// optimize space
	if len(msgTx.CalledStates) > 0 {
		msgTx.CalledStatesIdxs = amhelp.StatesToIndexes(index,
			msgTx.CalledStates)
		msgTx.MachineID = ""
		msgTx.CalledStates = nil
	}

	if err := g.parseRpcConns(c, msgTx); err != nil {
		AddErrGraph(nil, g.ParentMach, fmt.Errorf("ParseMsg: %w", err))
	}

	// TODO errors
	// var isErr bool
	// for _, name := range index {
	// 	if strings.HasPrefix(name, "Err") && msgTx.Is1(index, name) {
	// 		isErr = true
	// 		break
	// 	}
	// }
	// if isErr || msgTx.Is1(index, am.StateException) {
	// 	// prepend to errors TODO DB errors
	// 	// idx := SQL COUNT
	// 	c.errors = append([]int{idx}, c.errors...)
	// }

	if err := g.parseMsgDbgLog(c, msgTx); err != nil {
		AddErrGraph(nil, g.ParentMach, fmt.Errorf("parseMsgLog: %w", err))
	}

	// TODO dedicated error state, enable once stable
	// if err != nil {
	// g.Mach.AddErr(fmt.Errorf("Graph.parseMsgLog: %w", err), nil)
	// }
	// TODO assert clocks
	c.Time = msgTx.Clocks
	c.MTimeSum = sum
}

// ParseMsgRpc applies a network graph update to the local clock data.
func (g *Graph) ParseMsgRpc(update *MsgGraphUpdate) bool {
	if update == nil {
		return false
	}
	c := g.Machines[update.MachId]
	if c == nil {
		return false
	}
	if update.SchemaHash != c.SchemaHash {
		return false
	}
	// TODO move to update.Validate()
	if len(update.Indexes) != len(update.Ticks) {
		return false
	}

	mTime := slices.Clone(c.Time)
	for i, idx := range update.Indexes {
		if int(idx) >= len(mTime) {
			return false
		}
		mTime[idx] += uint64(update.Ticks[i])
	}

	qTick := c.QueueTick + uint64(update.QueueTick)
	machTick := c.MachineTick + uint32(update.MachTick)
	sum := mTime.Sum(nil)
	if arpc.Checksum(sum, qTick, machTick) != update.Checksum {
		return false
	}

	if update.Tags != nil && update.TagsTime >= c.TagsMTimeSum {
		g.setClientTags(c, update.Tags)
		c.TagsMTimeSum = update.TagsTime
	}

	c.Time = mTime
	c.MTimeSum = sum
	c.QueueTick = qTick
	c.MachineTick = machTick

	return true
}

// DumpGv will create a dot-format *.gv file of the graph. To create an SVG:
//
//	dot -Tsvg -O path
func (g *Graph) DumpGv(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	return draw.DOT(g.G, file)
}

func (g *Graph) Inspect() (map[string]*MachInspect, error) {
	adjacencyMap, err := g.G.AdjacencyMap()
	if err != nil {
		return nil, err
	}
	inspect := make(map[string]*MachInspect)
	for machId, adjs := range adjacencyMap {
		if len(strings.Split(machId, ":")) == 2 {
			continue
		}
		c, ok := g.Machines[machId]
		if !ok {
			// TODO err
			continue
		}

		// init
		_, ok = inspect[machId]
		if !ok {
			inspect[machId] = &MachInspect{
				Pipes:    make(map[string][]string),
				MTimeSum: c.MTimeSum,
				Schema:   c.Schema.Clone(),
				Tags:     slices.Clone(c.Tags),
				MTime:    slices.Clone(c.Time),
				States:   slices.Clone(c.StateNames),
			}
		}

		for _, edge := range adjs {
			conn, _ := g.Connection(machId, edge.Target)

			if conn.Edge.MachChildOf {
				inspect[machId].Child = edge.Target
			}
			if conn.Edge.MachConnectedTo {
				inspect[machId].Conns = append(inspect[machId].Conns, edge.Target)
			}
			if conn.Edge.MachPipesTo != nil {
				for _, pipe := range conn.Edge.MachPipesTo {
					inspect[machId].Pipes[edge.Target] = append(
						inspect[machId].Pipes[edge.Target], fmt.Sprintf(
							"[%s] %s -> %s", pipe.MutType, pipe.FromState, pipe.ToState,
						),
					)
				}
			}
		}
	}

	return inspect, nil
}

// private

// setClientTags updates machine tags and their RPC tag relations.
// TODO err
func (g *Graph) setClientTags(c *Machine, tags []string) {
	if c.Serialized == nil {
		return
	}
	oldTags := c.Tags
	oldSrc := amhelp.TagValue(oldTags, ssrpc.TagKeySrcId)
	newSrc := amhelp.TagValue(tags, ssrpc.TagKeySrcId)
	c.Tags = slices.Clone(tags)

	for _, tag := range oldTags {
		if !slices.Contains(tags, tag) {
			g.removeTagVertex(c.ID, tag)
		}
	}
	for _, tag := range tags {
		if !slices.Contains(oldTags, tag) {
			g.addTagVertex(c.ID, tag)
		}
	}

	if oldSrc != "" && oldSrc != newSrc {
		_ = g.G.RemoveEdge(c.ID, oldSrc)
		_ = g.Map.RemoveEdge(c.ID, oldSrc)
	}
	if newSrc != "" && newSrc != oldSrc {
		_ = g.addEdge(c.ID, newSrc, &EdgeData{MachConnectedTo: true})
	}
}

func (g *Graph) addTagVertex(id string, tag string) {
	v := &Vertex{MachId: id, TagName: tag}
	tagID := hash(v)
	_ = g.G.AddVertex(v)
	_ = g.Map.AddVertex(&Vertex{MachId: id, TagName: tag})
	_ = g.G.AddEdge(id, tagID, graph.EdgeData(&EdgeData{MachHasTag: true}))
	_ = g.Map.AddEdge(id, tagID)
}

func (g *Graph) removeTagVertex(id string, tag string) {
	v := &Vertex{MachId: id, TagName: tag}
	tagID := hash(v)
	_ = g.G.RemoveEdge(id, tagID)
	_ = g.Map.RemoveEdge(id, tagID)
	_ = g.G.RemoveVertex(tagID)
	_ = g.Map.RemoveVertex(tagID)
}

func (g *Graph) parseRpcConns(c *Machine, msgTx *dbg.DbgMsgTx) error {
	// link netmachs to sources
	if id := amhelp.TagValue(c.Tags, ssrpc.TagKeySrcId); id != "" {
		err := g.addEdge(c.ID, id, &EdgeData{MachConnectedTo: true})
		if err != nil {
			return err
		}
		_ = g.Map.AddEdge(c.ID, id)
	}

	return nil
}

func (g *Graph) addEdge(srcId, destId string, data *EdgeData) error {
	if err := g.addMach(srcId, nil); err != nil {
		return err
	}
	if err := g.addMach(destId, nil); err != nil {
		return err
	}

	err := g.G.AddEdge(srcId, destId, graph.EdgeData(data))
	if err == nil {
		_ = g.Map.AddEdge(srcId, destId)
	}

	return err
}

// addMach upsert a mach or a placeholder
func (g *Graph) addMach(id string, client *Machine) error {
	err := g.G.AddVertex(&Vertex{
		MachId: id,
	})
	if errors.Is(err, graph.ErrVertexAlreadyExists) {
		// update
		if client != nil && client.Serialized != nil {
			g.Machines[id] = client
		}

		return nil
	}
	if err != nil {
		return err
	}
	_ = g.Map.AddVertex(&Vertex{
		MachId: id,
	})
	return err
}

func (g *Graph) parseMsgDbgLog(c *Machine, msgTx *dbg.DbgMsgTx) error {
	// pre-tx log entries
	for _, entry := range msgTx.PreLogEntries {
		err := g.parseMsgDbgReader(c, entry, msgTx)
		if err != nil {
			return err
		}
	}

	// tx log entries
	for _, entry := range msgTx.LogEntries {
		err := g.parseMsgDbgReader(c, entry, msgTx)
		if err != nil {
			return err
		}
	}

	return nil
}

func (g *Graph) parseMsgDbgReader(
	c *Machine, log *am.LogEntry, tx *dbg.DbgMsgTx,
) error {
	// NEW PIPE

	if strings.HasPrefix(log.Text, "[pipe-in:add] ") ||
		strings.HasPrefix(log.Text, "[pipe-in:remove] ") ||
		strings.HasPrefix(log.Text, "[pipe-out:add] ") ||
		strings.HasPrefix(log.Text, "[pipe-out:remove] ") {

		isAdd := strings.HasPrefix(log.Text, "[pipe-in:add] ") ||
			strings.HasPrefix(log.Text, "[pipe-out:add] ")
		isPipeOut := strings.HasPrefix(log.Text, "[pipe-out")

		var msg []string
		if isPipeOut && isAdd {
			msg = strings.Split(log.Text[len("[pipe-out:add] "):], " to ")
		} else if !isPipeOut && isAdd {
			msg = strings.Split(log.Text[len("[pipe-in:add] "):], " from ")
		} else if isPipeOut && !isAdd {
			msg = strings.Split(log.Text[len("[pipe-out:remove] "):], " to ")
		} else if !isPipeOut && !isAdd {
			msg = strings.Split(log.Text[len("[pipe-in:remove] "):], " from ")
		}
		mut := am.MutationRemove
		if isAdd {
			mut = am.MutationAdd
		}

		// define what we know from this log line
		state := msg[0]
		otherMach := msg[1]
		var sourceMachId string
		var targetMachId string
		if isPipeOut {
			sourceMachId = c.ID
			targetMachId = otherMach
		} else {
			sourceMachId = otherMach
			targetMachId = c.ID
		}

		// get edge
		link, linkErr := g.G.Edge(sourceMachId, targetMachId)
		var data *EdgeData
		if linkErr != nil {
			data = &EdgeData{}
			err := g.G.AddEdge(sourceMachId, targetMachId, graph.EdgeData(data))
			if err != nil {
				// too early, add a dummy mach
				if err = g.addMach(otherMach, nil); err != nil {
					return err
				}
				err = g.G.AddEdge(sourceMachId, targetMachId, graph.EdgeData(data))
				if err != nil {
					return err
				}
			}
			_ = g.Map.AddEdge(sourceMachId, targetMachId)
		} else {
			data = link.Properties.Data.(*EdgeData)
		}

		// debug
		// stateDbg := state
		// if isAdd {
		// 	stateDbg += " add"
		// }
		// if isPipeOut {
		// 	stateDbg += " out"
		// }
		// dump.Println(c.ID, stateDbg, sourceMachId+"->"+targetMachId, data)

		// update the missing state from the other side of the pipe
		found := false
		for _, pipe := range data.MachPipesTo {
			// IN
			if !isPipeOut && pipe.MutType == mut && pipe.ToState == "" {

				pipe.ToState = state
				found = true

				// DUP
			} else if !isPipeOut && pipe.MutType == mut && pipe.ToState == state {
				found = true
			}

			// OUT
			if isPipeOut && pipe.MutType == mut && pipe.FromState == "" {

				pipe.FromState = state
				found = true

				// DUP
			} else if isPipeOut && pipe.MutType == mut && pipe.FromState == state {
				found = true
			}

			if found {
				break
			}
		}

		// add a new pipe to an existing edge
		if !found {
			pipe := &MachPipeTo{
				ToState: state,
				MutType: mut,
			}
			if isPipeOut {
				pipe = &MachPipeTo{
					FromState: state,
					MutType:   mut,
				}
			}

			data.MachPipesTo = append(data.MachPipesTo, pipe)
		}

		// REMOVE PIPE
	} else if strings.HasPrefix(log.Text, "[pipe:gc] ") {
		l := strings.Split(log.Text, " ")
		id := l[1]
		// TODO make it safe

		// outbound
		adjs, err := g.G.AdjacencyMap()
		if err != nil {
			return err
		}
		for _, edge := range adjs[id] {
			err := g.G.RemoveEdge(id, edge.Target)
			if err != nil {
				return err
			}
			_ = g.Map.RemoveEdge(id, edge.Target)
		}

		// inbound
		preds, err := g.G.PredecessorMap()
		if err != nil {
			return err
		}
		for _, edge := range preds[id] {
			err := g.G.RemoveEdge(edge.Source, id)
			if err != nil {
				return err
			}
			_ = g.Map.RemoveEdge(edge.Source, id)
		}
	}

	// TODO detached pipe handlers

	return nil
}

// SaveGob writes the graph to w using encoding/gob.
//
// Only the exported graph data is saved: clients ([Machine]), vertices, and
// edges with their [EdgeData]. Runtime-only state (ParentMach, locks, the
// dominikbraun graph internals) is not serialized; [Graph.LoadGob] rebuilds
// them.
func (g *Graph) SaveGob(w io.Writer) error {
	if g == nil {
		return errors.New("graph is nil")
	}

	g.loop.Lock()
	defer g.loop.Unlock()

	snap := gobSnapshot{
		Version: gobFormatVersion,
		Clients: make(map[string]*Machine, len(g.Machines)),
	}
	for id, client := range g.Machines {
		snap.Clients[id] = client.Clone()
	}

	adj, err := g.G.AdjacencyMap()
	if err != nil {
		return fmt.Errorf("graph adjacency: %w", err)
	}
	for vertexHash := range adj {
		v, err := g.G.Vertex(vertexHash)
		if err != nil {
			return fmt.Errorf("graph vertex %q: %w", vertexHash, err)
		}
		snap.Vertices = append(snap.Vertices, &Vertex{
			StateName: v.StateName,
			TagName:   v.TagName,
			MachId:    v.MachId,
		})
	}

	edges, err := g.G.Edges()
	if err != nil {
		return fmt.Errorf("graph edges: %w", err)
	}
	for _, edge := range edges {
		data, _ := edge.Properties.Data.(*EdgeData)
		snap.Edges = append(snap.Edges, gobEdge{
			Source: edge.Source,
			Target: edge.Target,
			Data:   cloneEdgeData(data),
		})
	}

	return gob.NewEncoder(w).Encode(&snap)
}

// LoadGob replaces the graph's contents with the data read from r, as written
// by [Graph.SaveGob]. ParentMach is preserved, everything else is rebuilt.
func (g *Graph) LoadGob(r io.Reader) error {
	if g == nil {
		return errors.New("graph is nil")
	}

	var snap gobSnapshot
	if err := gob.NewDecoder(r).Decode(&snap); err != nil {
		return err
	}
	if snap.Version != gobFormatVersion {
		return fmt.Errorf("graph: unsupported gob version %d", snap.Version)
	}

	g.loop.Lock()
	defer g.loop.Unlock()

	g.G = graph.New(hash, graph.Directed())
	g.Map = graph.New(hash)
	g.Machines = make(map[string]*Machine, len(snap.Clients))
	for id, client := range snap.Clients {
		g.Machines[id] = client
	}

	for _, v := range snap.Vertices {
		if err := g.G.AddVertex(&Vertex{
			StateName: v.StateName,
			TagName:   v.TagName,
			MachId:    v.MachId,
		}); err != nil {
			return fmt.Errorf("graph vertex %q: %w", hash(v), err)
		}
		if err := g.Map.AddVertex(&Vertex{
			StateName: v.StateName,
			TagName:   v.TagName,
			MachId:    v.MachId,
		}); err != nil {
			return fmt.Errorf("map vertex %q: %w", hash(v), err)
		}
	}

	for _, e := range snap.Edges {
		if err := g.G.AddEdge(e.Source, e.Target,
			graph.EdgeData(e.Data)); err != nil {
			return fmt.Errorf("graph edge %q->%q: %w", e.Source, e.Target, err)
		}
		if err := g.Map.AddEdge(e.Source, e.Target); err != nil {
			return fmt.Errorf("map edge %q->%q: %w", e.Source, e.Target, err)
		}
	}

	return nil
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// gobFormatVersion is the on-wire format of [Graph.SaveGob].
const gobFormatVersion = 1

// cloneEdgeData deep-copies edge metadata for serialization.
func cloneEdgeData(data *EdgeData) *EdgeData {
	if data == nil {
		return nil
	}
	cp := *data
	if data.MachHas != nil {
		has := *data.MachHas
		cp.MachHas = &has
	}
	if data.MachPipesTo != nil {
		cp.MachPipesTo = make([]*MachPipeTo, len(data.MachPipesTo))
		for i, pipe := range data.MachPipesTo {
			if pipe == nil {
				continue
			}
			p := *pipe
			cp.MachPipesTo[i] = &p
		}
	}
	if data.StateRelation != nil {
		cp.StateRelation = make([]*StateRelation, len(data.StateRelation))
		for i, rel := range data.StateRelation {
			if rel == nil {
				continue
			}
			r := *rel
			cp.StateRelation[i] = &r
		}
	}

	return &cp
}

// gobSnapshot is the serializable form of [Graph]: the clients, vertices, and
// edges with their metadata. Runtime state (ParentMach, mutexes, the
// dominikbraun internals) is rebuilt on load.
type gobSnapshot struct {
	Version  int
	Clients  map[string]*Machine
	Vertices []*Vertex
	Edges    []gobEdge
}

// gobEdge is one serialized edge with its metadata.
type gobEdge struct {
	Source string
	Target string
	Data   *EdgeData
}

// Markdown returns a Markdown format of the network graph, with state lists,
// with a flat hierarchy. It's useful for debugging.
func Markdown(inspect map[string]*MachInspect) string {
	keys := slices.Collect(maps.Keys(inspect))
	sort.Strings(keys)
	ret := "# am-vis inspect-dump\n\n"
	for _, machId := range keys {
		data := inspect[machId]
		ret += "## " + machId + "\n"
		// TODO format
		ret += fmt.Sprintf("Time: t%d\n", data.MTimeSum)
		if data.Child != "" {
			ret += fmt.Sprintf("Parent: %s\n\n", data.Child)
		} else {
			ret += "\n"
		}
		if len(data.States) > 0 {
			states := slices.Clone(data.States)
			sort.Strings(states)
			ret += "### States\n"
			for _, name := range states {
				if idx := slices.Index(data.States, name); idx >= 0 &&
					idx < len(data.MTime) {
					ret += fmt.Sprintf("- %s:%d\n", name, data.MTime[idx])
				} else {
					ret += fmt.Sprintf("- %s\n", name)
				}
			}
			ret += "\n"
		}
		if len(data.Conns) > 0 {
			sort.Strings(data.Conns)
			ret += "### RPC\n"
			ret += fmt.Sprintf("- %s\n", strings.Join(data.Conns, "\n- "))
			ret += "\n"
		}
		if len(data.Pipes) > 0 {
			ret += "### Pipes\n\n"
			keysPipes := slices.Collect(maps.Keys(data.Pipes))
			sort.Strings(keysPipes)
			for _, pipe := range keysPipes {
				sort.Strings(data.Pipes[pipe])
				ret += fmt.Sprintf("#### %s\n", pipe)
				ret += fmt.Sprintf("- %s\n", strings.Join(data.Pipes[pipe], "\n- "))
				ret += "\n"
			}
		}
		ret += "-----\n\n"
	}

	return ret
}

// Markup returns an XML format of the network graph, including full schemas,
// with a nested hierarchy. It's useful for CSS/XPath queries.
func Markup(inspect map[string]*MachInspect) string {
	// build parent -> []children map and collect roots
	children := make(map[string][]string)
	var roots []string
	for id, data := range inspect {
		if data.Child == "" {
			roots = append(roots, id)
		} else {
			children[data.Child] = append(children[data.Child], id)
		}
	}
	sort.Strings(roots)
	for k := range children {
		sort.Strings(children[k])
	}

	ret := "<graph>\n"
	for _, id := range roots {
		ret += "\n"
		ret += writeMachine(inspect, children, id, 1)
	}
	ret += "</graph>\n"
	return ret
}

func MsgDbgToMach(msg *dbg.DbgMsgStruct) *Machine {
	if msg == nil {
		return nil
	}
	return &Machine{
		Serialized: &am.Serialized{
			ID:          msg.ID,
			StateNames:  slices.Clone(msg.StatesIndex),
			Time:        make(am.Time, len(msg.StatesIndex)),
			QueueTick:   1,
			MachineTick: 0,
			Tags:        slices.Clone(msg.Tags),
		},
		SchemaHash: amhelp.SchemaHash(msg.States),
		Schema:     msg.States.Clone(),
		Parent:     msg.Parent,
	}
}

func hash(c *Vertex) string {
	if c.StateName != "" {
		return c.MachId + ":" + c.StateName
	}
	if c.TagName != "" {
		return c.MachId + "#" + c.TagName
	}
	return c.MachId
}

func writePipe(target, pipe, pad string) string {
	body := pipe
	attrs := fmt.Sprintf(" to=%q", target)

	if strings.HasPrefix(pipe, "[") {
		if close := strings.Index(pipe, "]"); close > 1 {
			mut := strings.TrimSpace(pipe[1:close])
			rest := strings.TrimSpace(pipe[close+1:])
			if mut == "remove" {
				attrs += fmt.Sprintf(" add=\"0\"")
			} else {
				attrs += fmt.Sprintf(" add=\"1\"")
			}

			from, to, ok := strings.Cut(rest, " -> ")
			if ok {
				from = strings.TrimSpace(from)
				to = strings.TrimSpace(to)
				if from != "" {
					attrs += fmt.Sprintf(" as=%q", from)
				}
				switch {
				case to != "":
					body = to
				case from != "":
					body = from
				default:
					body = rest
				}
			} else {
				body = rest
			}
		}
	}

	return fmt.Sprintf("%s<pipe%s>%s</pipe>\n", pad, attrs, body)
}

func writeMachine(
	inspect map[string]*MachInspect, children map[string][]string, id string,
	indent int,
) string {
	//

	data := inspect[id]
	pad := strings.Repeat("    ", indent)
	inner := pad + "    "
	ret := fmt.Sprintf("%s<machine id=%q time=\"%d\">\n", pad, id, data.MTimeSum)

	states := slices.Clone(data.States)
	sort.Strings(states)
	for _, s := range states {
		ret += writeState(s, data, inner)
	}

	conns := slices.Clone(data.Conns)
	sort.Strings(conns)
	for _, c := range conns {
		ret += fmt.Sprintf("%s<rpc>%s</rpc>\n", inner, c)
	}

	pipeKeys := slices.Collect(maps.Keys(data.Pipes))
	sort.Strings(pipeKeys)
	for _, target := range pipeKeys {
		pipes := slices.Clone(data.Pipes[target])
		sort.Strings(pipes)
		for _, p := range pipes {
			ret += writePipe(target, p, inner)
		}
	}

	tags := slices.Clone(data.Tags)
	sort.Strings(tags)
	for _, tag := range tags {
		ret += fmt.Sprintf("%s<tag>%s</tag>\n", inner, tag)
	}

	for _, childId := range children[id] {
		ret += "\n"
		ret += writeMachine(inspect, children, childId, indent+1)
	}

	ret += pad + "</machine>\n"
	return ret
}

func writeState(name string, data *MachInspect, pad string) string {
	state, ok := data.Schema[name]
	if !ok {
		return fmt.Sprintf("%s<state>%s</state>\n", pad, name)
	}

	attrs := ""
	if idx := slices.Index(data.States, name); idx >= 0 && idx < len(data.MTime) {
		attrs += fmt.Sprintf(` tick="%d"`, data.MTime[idx])
		if am.IsActiveTick(data.MTime[idx]) {
			attrs += fmt.Sprintf(` active="1"`)
		}
	}
	if state.Auto {
		attrs += ` auto="1"`
	}
	if state.Multi {
		attrs += ` multi="1"`
	}

	hasRelations := len(state.Require) > 0 || len(state.Add) > 0 ||
		len(state.Remove) > 0 || len(state.After) > 0
	if !hasRelations {
		return fmt.Sprintf("%s<state%s>%s</state>\n", pad, attrs, name)
	}

	ret := fmt.Sprintf("%s<state%s>\n", pad, attrs)
	ret += fmt.Sprintf("%s    %s\n", pad, name)

	writeRelations := func(tag string, rels am.S) {
		if len(rels) == 0 {
			return
		}
		items := slices.Clone(rels)
		sort.Strings(items)
		for _, rel := range items {
			ret += fmt.Sprintf("%s    <%s>%s</%s>\n", pad, tag, rel, tag)
		}
	}

	writeRelations("require", state.Require)
	writeRelations("add", state.Add)
	writeRelations("remove", state.Remove)
	writeRelations("after", state.After)

	ret += fmt.Sprintf("%s</state>\n", pad)
	return ret
}
