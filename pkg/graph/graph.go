// Package graph provides a graph or interconnected state-machines and their
// states, based on the dbg telemetry protocol.
package graph

// TODO fix GC

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dominikbraun/graph"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
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

// Machine:
// - has State inherited:string auto:bool multi:bool
// - connectedTo Machine addr:string
// - pipeTo Machine states:map[string]string
// - childOf Machine
//
// State:
// - relation type:require|add|remove State
// - pipeTo Machine|state add:bool

type MachineHas struct {
	inherited string
	auto      bool
	multi     bool
}

type StateRelation struct {
	relType am.Relation
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

func hash(c *Vertex) string {
	if c.StateName != "" {
		return c.MachId + ":" + c.StateName
	}
	return c.MachId
}

type Exportable struct {
	MsgTxs []*telemetry.DbgMsgTx
}

type Client struct {
	// bits which get saved into the go file
	// Exportable
	MsgStruct *telemetry.DbgMsgStruct

	Id string

	// indexes of txs with errors, desc order for bisects
	// TODO refresh on GC
	LatestMsgTx   *telemetry.DbgMsgTx
	LatestTimeSum uint64
	LatestClock   am.Time
}

// ///// ///// /////

// ///// GRAPH

// ///// ///// /////

type Graph struct {
	Mach *am.Machine

	clients map[string]*Client
	// g is a directed graph of machines and states with metadata.
	g graph.Graph[string, *Vertex]
	// gMap is a unidirectional mirror of g, without metadata.
	gMap graph.Graph[string, *Vertex]
}

func New(m *am.Machine) (*Graph, error) {
	g := &Graph{
		g:       graph.New(hash, graph.Directed()),
		gMap:    graph.New(hash),
		Mach:    m,
		clients: make(map[string]*Client),
	}

	// TODO state-check the machine for InitClient

	// err := m.BindHandlers(g)
	// if err != nil {
	// 	return nil, err
	// }

	return g, nil
}

// Clone returns a deep clone of the graph.
func (g *Graph) Clone() (*Graph, error) {
	c1, err := g.g.Clone()
	if err != nil {
		return nil, err
	}

	c2, err := g.gMap.Clone()
	if err != nil {
		return nil, err
	}

	g2 := &Graph{
		g:       c1,
		gMap:    c2,
		clients: make(map[string]*Client, len(g.clients)),
	}

	for id, c := range g.clients {
		g2.clients[id] = &Client{
			Id:            id,
			MsgStruct:     c.MsgStruct,
			LatestClock:   c.LatestClock,
			LatestTimeSum: c.LatestTimeSum,
		}
	}

	return g2, nil
}

func (g *Graph) Clients() map[string]*Client {
	return g.clients
}

func (g *Graph) G() graph.Graph[string, *Vertex] {
	return g.g
}

func (g *Graph) Map() graph.Graph[string, *Vertex] {
	return g.gMap
}

func (g *Graph) Clear() {
	g.clients = make(map[string]*Client)
	g.g = graph.New(hash, graph.Directed())
	g.gMap = graph.New(hash)
}

// Connection returns a Connection for the given source-target.
func (g *Graph) Connection(source, target string) (*Connection, error) {
	edge, err := g.g.Edge(source, target)
	if err != nil {
		return nil, err
	}
	data := edge.Properties.Data.(*EdgeData)
	targetVert, err := g.g.Vertex(target)
	if err != nil {
		return nil, err
	}
	sourceVert, err := g.g.Vertex(source)
	if err != nil {
		return nil, err
	}

	return &Connection{
		Edge:   data,
		Source: sourceVert,
		Target: targetVert,
	}, nil
}

func (g *Graph) ParseMsg(id string, msgTx *telemetry.DbgMsgTx) {
	c := g.clients[id]

	var sum uint64
	for _, v := range msgTx.Clocks {
		sum += v
	}
	index := c.MsgStruct.StatesIndex

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
		isRpcServer := slices.Contains(c.MsgStruct.Tags, "rpc-server")
		if slices.Contains(added, ssrpc.ServerStates.HandshakeDone) && isRpcServer {
			for _, item := range msgTx.LogEntries {
				if !strings.HasPrefix(item.Text, "[add] ") {
					continue
				}

				line := strings.Split(strings.TrimRight(item.Text, ")\n"), "(")
				for _, arg := range strings.Split(line[1], " ") {
					a := strings.Split(arg, "=")
					if a[0] == "id" {
						data := graph.EdgeData(&EdgeData{MachConnectedTo: true})
						err := g.g.AddEdge(a[1], c.Id, data)
						if err != nil {

							// wait for the other mach to show up TODO better approach
							when := g.Mach.WhenArgs(ss.InitClient, am.A{"id": a[1]}, nil)
							go func() {
								<-when

								if err := g.g.AddEdge(a[1], c.Id, data); err != nil {
									g.Mach.AddErr(fmt.Errorf("Graph.ParseMsg: %w", err), nil)
									return
								}
								if err = g.gMap.AddEdge(a[1], c.Id); err != nil {
									g.Mach.AddErr(fmt.Errorf("Graph.ParseMsg: %w", err), nil)
									return
								}
							}()

						} else {
							if err = g.gMap.AddEdge(a[1], c.Id); err != nil {
								g.Mach.AddErr(fmt.Errorf("Graph.ParseMsg: %w", err), nil)
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
	// if isErr || msgTx.Is1(index, am.Exception) {
	// 	// prepend to errors TODO DB errors
	// 	// idx := SQL COUNT
	// 	c.errors = append([]int{idx}, c.errors...)
	// }

	_ = g.parseMsgLog(c, msgTx)
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

func (g *Graph) AddClient(msg *telemetry.DbgMsgStruct) error {
	// init
	id := msg.ID
	c := &Client{
		Id:          id,
		MsgStruct:   msg,
		LatestClock: make(am.Time, len(msg.States)),
	}
	g.clients[id] = c

	// add machine
	err := g.g.AddVertex(&Vertex{
		MachId: c.Id,
	})
	if err != nil {
		return err
	}
	_ = g.gMap.AddVertex(&Vertex{
		MachId: c.Id,
	})

	// parent
	if c.MsgStruct.Parent != "" {
		data := graph.EdgeData(&EdgeData{MachChildOf: true})
		err = g.g.AddEdge(c.Id, c.MsgStruct.Parent, data)
		if err != nil {

			// wait for the parent to show up
			when := g.Mach.WhenArgs(ss.InitClient,
				am.A{"id": c.MsgStruct.Parent}, nil)
			go func() {
				<-when
				err = g.g.AddEdge(c.Id, c.MsgStruct.Parent, data)
				if err == nil {
					_ = g.gMap.AddEdge(c.Id, c.MsgStruct.Parent)
				}
			}()
		} else {
			_ = g.gMap.AddEdge(c.Id, c.MsgStruct.Parent)
		}
	}

	// add states
	for name, props := range c.MsgStruct.States {
		// vertex
		err = g.g.AddVertex(&Vertex{
			MachId:    id,
			StateName: name,
		})
		if err != nil {
			return err
		}
		_ = g.gMap.AddVertex(&Vertex{
			MachId:    id,
			StateName: name,
		})

		// edge
		err = g.g.AddEdge(id, id+":"+name, graph.EdgeData(&EdgeData{
			MachHas: &MachineHas{
				auto:  props.Auto,
				multi: props.Multi,
				// TODO
				inherited: "",
			},
		}))
		if err != nil {
			return err
		}
		_ = g.gMap.AddEdge(id, id+":"+name)
	}

	type relation struct {
		states  am.S
		relType am.Relation
	}

	// add relations
	for name, state := range c.MsgStruct.States {

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
				if edge, err := g.g.Edge(from, to); err == nil {
					data := edge.Properties.Data.(*EdgeData)
					data.StateRelation = append(data.StateRelation, &StateRelation{
						relType: item.relType,
					})
					err = g.g.UpdateEdge(from, to, graph.EdgeData(data))
					if err != nil {
						panic(err)
					}

					continue
				}

				// add if doesnt exist
				err = g.g.AddEdge(from, to, graph.EdgeData(&EdgeData{
					StateRelation: []*StateRelation{
						{relType: item.relType},
					},
				}))
				if err != nil {
					panic(err)
				}
				_ = g.gMap.AddEdge(from, to)
			}
		}
	}

	return nil
}

func (g *Graph) parseMsgLog(c *Client, msgTx *telemetry.DbgMsgTx) error {
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
	c *Client, log *am.LogEntry, tx *telemetry.DbgMsgTx,
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
		link, linkErr := g.g.Edge(sourceMachId, targetMachId)

		// edge exists - update
		if linkErr == nil {
			data := link.Properties.Data.(*EdgeData)
			found := false

			// update the missing state from the other side of the pipe
			for _, pipe := range data.MachPipesTo {
				if !isPipeOut && pipe.MutType == mut && pipe.ToState == "" {

					pipe.ToState = state
					found = true
					break
				}
				if isPipeOut && pipe.MutType == mut && pipe.FromState == "" {

					pipe.FromState = state
					found = true
					break
				}
			}

			// add a new pipe to an existing edge TODO extract
			if !found {
				var pipe *MachPipeTo
				if isPipeOut {
					pipe = &MachPipeTo{
						FromState: state,
						MutType:   mut,
					}
				} else {
					pipe = &MachPipeTo{
						ToState: state,
						MutType: mut,
					}
				}

				data.MachPipesTo = append(data.MachPipesTo, pipe)
			}

		} else {

			// create a new edge with a single pipe
			pipe := &MachPipeTo{
				ToState: state,
				MutType: mut,
			}
			if isPipeOut {
				pipe = &MachPipeTo{
					FromState: state,
				}
			}
			data := &EdgeData{MachPipesTo: []*MachPipeTo{pipe}}
			err := g.g.AddEdge(sourceMachId, targetMachId, graph.EdgeData(data))
			if err != nil {
				return err
			}
			_ = g.gMap.AddEdge(sourceMachId, targetMachId)
		}

		// REMOVE PIPE
	} else if strings.HasPrefix(log.Text, "[pipe:gc] ") {
		l := strings.Split(log.Text, " ")
		id := l[1]
		// TODO make it safe

		// outbound
		adjs, err := g.g.AdjacencyMap()
		if err != nil {
			panic(err)
		}
		for _, edge := range adjs[id] {
			err := g.g.RemoveEdge(id, edge.Target)
			if err != nil {
				panic(err)
			}
			_ = g.gMap.RemoveEdge(id, edge.Target)
		}

		// inbound
		preds, err := g.g.PredecessorMap()
		if err != nil {
			panic(err)
		}
		for _, edge := range preds[id] {
			err := g.g.RemoveEdge(edge.Source, id)
			if err != nil {
				panic(err)
			}
			_ = g.gMap.RemoveEdge(edge.Source, id)
		}
	}

	// TODO detached pipe handlers

	return nil
}
