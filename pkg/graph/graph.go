// Package graph provides a graph or interconnected state-machines and their
// states, based on the dbg telemetry protocol.
package graph

// TODO fix GC

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

type Vertex struct {
	StateName string
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

	// state has relations with other states
	StateRelation []*StateRelation
}

// Client:
// - has State inherited:string auto:bool multi:bool
// - connectedTo Client addr:string
// - pipeTo Client states:map[string]string
// - childOf Client
//
// State:
// - relation type:require|add|remove State
// - pipeTo Client|state add:bool

type MachineHas struct {
	Inherited string
	Auto      bool
	Multi     bool
}

type StateRelation struct {
	RelType am.Relation
}

// TODO use am.Pipe
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

func hash(c *Vertex) string {
	if c.StateName != "" {
		return c.MachId + ":" + c.StateName
	}
	return c.MachId
}

// Client represents a single state machine withing the network graph.
type Client struct {
	Id string
	// TODO version schemas
	MsgSchema *dbg.DbgMsgStruct

	LatestMsgTx   *dbg.DbgMsgTx
	LatestTimeSum uint64
	LatestClock   am.Time
	ConnId        string
}

// ///// ///// /////

// ///// GRAPH

// ///// ///// /////

type Graph struct {
	Server  *am.Machine
	Clients map[string]*Client

	// G is a directed graph of machines and states with metadata.
	G graph.Graph[string, *Vertex]
	// Map is a unidirectional mirror of g, without metadata.
	Map graph.Graph[string, *Vertex]
}

func New(server *am.Machine) (*Graph, error) {
	if !server.Has(ssdbg.ServerStates.Names()) {
		return nil, fmt.Errorf(
			"Graph.New: server machine %s does not implement ssdbg.ServerStates",
			server.Id())
	}

	g := &Graph{
		Server:  server,
		G:       graph.New(hash, graph.Directed()),
		Map:     graph.New(hash),
		Clients: make(map[string]*Client),
	}

	// err := m.BindHandlers(g)
	// if err != nil {
	// 	return nil, err
	// }

	return g, nil
}

// Clone returns a deep clone of the graph.
func (g *Graph) Clone() (*Graph, error) {
	c1, err := g.G.Clone()
	if err != nil {
		return nil, err
	}

	c2, err := g.Map.Clone()
	if err != nil {
		return nil, err
	}

	g2 := &Graph{
		G:       c1,
		Map:     c2,
		Clients: make(map[string]*Client, len(g.Clients)),
	}

	for id, c := range g.Clients {
		g2.Clients[id] = &Client{
			Id:            id,
			MsgSchema:     c.MsgSchema,
			LatestClock:   c.LatestClock,
			LatestTimeSum: c.LatestTimeSum,
		}
	}

	return g2, nil
}

func (g *Graph) Clear() {
	g.Clients = make(map[string]*Client)
	g.G = graph.New(hash, graph.Directed())
	g.Map = graph.New(hash)
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

func (g *Graph) ParseMsg(id string, msgTx *dbg.DbgMsgTx) {
	c := g.Clients[id]

	var sum uint64
	for _, v := range msgTx.Clocks {
		sum += v
	}
	index := c.MsgSchema.StatesIndex

	// optimize space
	if len(msgTx.CalledStates) > 0 {
		msgTx.CalledStatesIdxs = amhelp.StatesToIndexes(index,
			msgTx.CalledStates)
		msgTx.MachineID = ""
		msgTx.CalledStates = nil
	}

	// detect RPC connections - read arg "id" for HandshakeDone, being the ID of
	// the RPC client
	// TODO extract to a func
	if c.LatestMsgTx != nil {
		prevTx := c.LatestMsgTx
		fakeTx := &am.Transition{
			TimeBefore: prevTx.Clocks,
			TimeAfter:  msgTx.Clocks,
		}
		added, _, _ := amhelp.GetTransitionStates(fakeTx, index)

		// RPC conns (requires LogLevel2)
		isRpcServer := slices.Contains(c.MsgSchema.Tags, "rpc-server")
		if slices.Contains(added, ssrpc.ServerStates.HandshakeDone) && isRpcServer {
			for _, item := range msgTx.LogEntries {
				if !strings.HasPrefix(item.Text, "[add] ") {
					continue
				}

				line := strings.Split(strings.TrimRight(item.Text, ")\n"), "(")
				for _, arg := range strings.Split(line[1], " ") {
					a := strings.Split(arg, "=")
					if a[0] == "id" {
						id := a[1]
						data := graph.EdgeData(&EdgeData{MachConnectedTo: true})
						err := g.G.AddEdge(id, c.Id, data)
						if err != nil {

							// wait for the other mach to show up TODO better approach

							g.Server.Log("waiting for RPC conn %s to show up", id)
							when := g.Server.WhenArgs(ss.InitClient, am.A{"id": a[1]}, nil)
							go func() {
								<-when
								g.Server.Log("Resuming RPC for %s", id)

								if err := g.G.AddEdge(a[1], c.Id, data); err != nil {
									g.Server.AddErr(fmt.Errorf("Graph.ParseMsg: %w", err), nil)
									return
								}
								if err = g.Map.AddEdge(a[1], c.Id); err != nil {
									g.Server.AddErr(fmt.Errorf("Graph.ParseMsg: %w", err), nil)
									return
								}
							}()

						} else {
							if err = g.Map.AddEdge(a[1], c.Id); err != nil {
								g.Server.AddErr(fmt.Errorf("Graph.ParseMsg: %w", err), nil)
								return
							}
						}
					}
				}
			}
		}
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

	err := g.parseMsgLog(c, msgTx)
	if err != nil {
		g.Server.AddErr(fmt.Errorf("Graph.parseMsgLog: %w", err), nil)
	}
	// TODO dedicated error state, enable once stable
	// if err != nil {
	// g.Mach.AddErr(fmt.Errorf("Graph.parseMsgLog: %w", err), nil)
	// }
	c.LatestMsgTx = msgTx
	// TODO assert clocks
	c.LatestClock = msgTx.Clocks
	c.LatestTimeSum = sum
}

func (g *Graph) RemoveClient(id string) error {
	// TODO
	return nil
}

func (g *Graph) AddClient(msg *dbg.DbgMsgStruct) error {
	// init
	id := msg.ID
	c := &Client{
		Id:          id,
		MsgSchema:   msg,
		LatestClock: make(am.Time, len(msg.States)),
	}
	g.Clients[id] = c

	// add machine
	err := g.G.AddVertex(&Vertex{
		MachId: c.Id,
	})
	if err != nil {
		return err
	}
	_ = g.Map.AddVertex(&Vertex{
		MachId: c.Id,
	})

	// parent
	if c.MsgSchema.Parent != "" {
		data := graph.EdgeData(&EdgeData{MachChildOf: true})
		err = g.G.AddEdge(c.Id, c.MsgSchema.Parent, data)
		if err != nil {

			// wait for the parent to show up
			g.Server.Log("waiting for parent %s to show up", c.MsgSchema.Parent)
			when := g.Server.WhenArgs(ss.InitClient,
				am.A{"id": c.MsgSchema.Parent}, nil)
			go func() {
				<-when
				g.Server.Log("resuming for %s", c.MsgSchema.Parent)
				err = g.G.AddEdge(c.Id, c.MsgSchema.Parent, data)
				if err == nil {
					_ = g.Map.AddEdge(c.Id, c.MsgSchema.Parent)
				}
				g.Server.AddErr(err, nil)
			}()
		} else {
			_ = g.Map.AddEdge(c.Id, c.MsgSchema.Parent)
		}
	}

	// add states
	for name, props := range c.MsgSchema.States {
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
				// TODO
				Inherited: "",
			},
		}))
		if err != nil {
			return err
		}
		_ = g.Map.AddEdge(id, id+":"+name)
	}

	// DEBUG
	// if c.Id == "rc-srv-browser2" {
	// 	ver, _ := g.G.Vertex("rc-srv-browser2")
	// 	ver = ver
	// 	print()
	// }

	type relation struct {
		states  am.S
		relType am.Relation
	}

	// add relations
	for name, state := range c.MsgSchema.States {

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

	return nil
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

type MachInspect struct {
	Child  string
	States []string
	Conns  []string
	Pipes  map[string][]string
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
		fmt.Println(machId)

		// init
		_, ok := inspect[machId]
		if !ok {
			inspect[machId] = &MachInspect{
				Pipes: make(map[string][]string),
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
			if conn.Edge.MachHas != nil {
				ids := strings.Split(edge.Target, ":")
				inspect[machId].States = append(inspect[machId].States, ids[1])
			}
			if conn.Edge.MachPipesTo != nil {
				if machId == "orchestrator" {
					print()
				}
				for _, pipe := range conn.Edge.MachPipesTo {
					inspect[machId].Pipes[edge.Target] = append(
						inspect[machId].Pipes[edge.Target], fmt.Sprintf(
							"[%s] %s -> %s", pipe.MutType, pipe.FromState, pipe.ToState,
						))
				}
			}
		}
	}

	return inspect, nil
}

func Markdown(inspect map[string]*MachInspect) string {
	keys := slices.Collect(maps.Keys(inspect))
	sort.Strings(keys)
	ret := "# am-vis inspect-dump\n\n"
	for _, machId := range keys {
		data := inspect[machId]
		ret += "## " + machId + "\n"
		if data.Child != "" {
			ret += fmt.Sprintf("Parent: %s\n\n", data.Child)
		} else {
			ret += "\n"
		}
		if len(data.States) > 0 {
			sort.Strings(data.States)
			ret += "### States\n"
			ret += fmt.Sprintf("- %s\n", strings.Join(data.States, "\n- "))
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

// private

func (g *Graph) parseMsgLog(c *Client, msgTx *dbg.DbgMsgTx) error {
	// pre-tx log entries
	for _, entry := range msgTx.PreLogEntries {
		err := g.parseMsgReader(c, entry, msgTx)
		if err != nil {
			return err
		}
	}

	// tx log entries
	for _, entry := range msgTx.LogEntries {
		err := g.parseMsgReader(c, entry, msgTx)
		if err != nil {
			return err
		}
	}

	return nil
}

func (g *Graph) parseMsgReader(
	c *Client, log *am.LogEntry, tx *dbg.DbgMsgTx,
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
		var sourceMachId string
		var targetMachId string
		if isPipeOut {
			sourceMachId = c.Id
			targetMachId = msg[1]
		} else {
			sourceMachId = msg[1]
			targetMachId = c.Id
		}

		// get edge
		link, linkErr := g.G.Edge(sourceMachId, targetMachId)
		var data *EdgeData
		if linkErr != nil {
			data = &EdgeData{}
			err := g.G.AddEdge(sourceMachId, targetMachId, graph.EdgeData(data))
			if err != nil {
				return err
			}
			_ = g.Map.AddEdge(sourceMachId, targetMachId)
		} else {
			data = link.Properties.Data.(*EdgeData)
		}

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
