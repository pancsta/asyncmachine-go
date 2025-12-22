package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// BootstrapStatesDef contains all the states of the Bootstrap state machine.
// The target state is WorkerAddr, activated by an aRPC client.
type BootstrapStatesDef struct {
	*am.StatesBase

	// WorkerAddr - The awaited worker passed its connection details.
	WorkerAddr string

	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
	// inherit from NetSourceStatesDef
	*ssrpc.NetSourceStatesDef
}

// BootstrapSchema represents all relations and properties of
// BootstrapStatesDef.
var BootstrapSchema = SchemaMerge(
	// inherit from BasicSchema
	ssam.BasicSchema,
	// inherit from WorkerStruct
	ssrpc.NetSourceSchema,
	am.Schema{
		cos.WorkerAddr: {},
	})

// EXPORTS AND GROUPS

var (
	cos = am.NewStates(BootstrapStatesDef{})

	// BootstrapStates contains all the states for the Bootstrap machine.
	BootstrapStates = cos
)
