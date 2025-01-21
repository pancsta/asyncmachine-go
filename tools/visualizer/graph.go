package visualizer

import (
	"github.com/dominikbraun/graph"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
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
	fromState string
	toState   string
	mutType   am.MutationType
}

type GraphConnection struct {
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

func (v *Visualizer) InitGraph() {
	v.g = graph.New(hash, graph.Directed())
	v.gMap = graph.New(hash)
}
