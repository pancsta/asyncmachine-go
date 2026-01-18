// Package states contains a stateful schema-v2 for a CLI.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// DaemonStatesDef contains all the states of the Daemon state machine.
type DaemonStatesDef struct {
	*am.StatesBase

	OpFoo1 string
	OpBar2 string

	// Cmd send a cobra command to the daemon. TODO
	Cmd string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from DisposedStatesDef
	*ss.DisposedStatesDef
	// inherit from NetSourceStatesDef
	*ssrpc.NetSourceStatesDef
}

// DaemonGroupsDef contains all the state groups Daemon state machine.
type DaemonGroupsDef struct {
	Ops S
}

// DaemonSchema represents all relations and properties of DaemonStates.
var DaemonSchema = SchemaMerge(
	// inherit from BasicStruct
	ss.BasicSchema,
	// inherit from DisposedStruct
	ss.DisposedSchema,
	// inherit from WorkerSchema
	ssrpc.NetSourceSchema,
	am.Schema{
		ssD.OpFoo1: {
			Require: S{ssD.Start},
			Remove:  sgD.Ops,
		},
		ssD.OpBar2: {
			Require: S{ssD.Start},
			Remove:  sgD.Ops,
		},
		ssD.Cmd: {},
	})

// EXPORTS AND GROUPS

var (
	ssD = am.NewStates(DaemonStatesDef{})
	sgD = am.NewStateGroups(DaemonGroupsDef{
		Ops: S{ssD.OpFoo1, ssD.OpBar2},
	})

	// DaemonStates contains all the states for the Daemon machine.
	DaemonStates = ssD
	// DaemonGroups contains all the state groups for the Daemon machine.
	DaemonGroups = sgD
)
