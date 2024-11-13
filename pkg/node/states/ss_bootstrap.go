package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// BootstrapStatesDef contains all the states of the Bootstrap state machine.
// The target state is WorkerAddr, activated by an aRPC client.
type BootstrapStatesDef struct {

	// WorkerAddr - The awaited worker passed its connection details.
	WorkerAddr string

	// inherit from WorkerStatesDef
	*ssrpc.WorkerStatesDef
}

// BootstrapStruct represents all relations and properties of
// BootstrapStatesDef.
var BootstrapStruct = StructMerge(
	// inherit from WorkerStruct
	ssrpc.WorkerStruct,
	am.Struct{
		cos.WorkerAddr: {},
	})

// EXPORTS AND GROUPS

var (
	cos = am.NewStates(BootstrapStatesDef{})

	// BootstrapStates contains all the states for the Bootstrap machine.
	BootstrapStates = cos
)
