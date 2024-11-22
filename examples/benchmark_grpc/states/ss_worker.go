package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// WorkerStatesDef contains all the states of the Worker state machine.
type WorkerStatesDef struct {
	Event  string
	Value1 string
	Value2 string
	Value3 string
	CallOp string

	// inherit from WorkerStatesDef
	*ssrpc.WorkerStatesDef
}

// WorkerGroupsDef contains all the state groups of the Worker state machine.
type WorkerGroupsDef struct {

	// Values group contains mutually exclusive values.
	Values S
}

// WorkerStruct represents all relations and properties of WorkerStates.
var WorkerStruct = StructMerge(
	// inherit from BasicStruct
	ssrpc.WorkerStruct,
	am.Struct{

		// ops
		ws.CallOp: {
			Multi:   true,
			Require: S{ws.Start},
		},

		// events
		ws.Event: {
			Multi:   true,
			Require: S{ws.Start},
		},

		// values
		ws.Value1: {Remove: wg.Values},
		ws.Value2: {Remove: wg.Values},
		ws.Value3: {Remove: wg.Values},
	})

// EXPORTS AND GROUPS

var (
	// ws is worker states from WorkerStatesDef.
	ws = am.NewStates(WorkerStatesDef{})

	// wg is worker groups from WorkerGroupsDef.
	wg = am.NewStateGroups(WorkerGroupsDef{
		Values: S{ws.Value1, ws.Value2, ws.Value3},
	})

	// WorkerStates contains all the states for the Worker machine.
	WorkerStates = ws

	// WorkerGroups contains all the state groups for the Worker machine.
	WorkerGroups = wg
)