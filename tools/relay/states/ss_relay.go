// Package states contains a stateful schema-v2 for Relay.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

// RelayStatesDef contains all the states of the Relay state machine.
type RelayStatesDef struct {
	*am.StatesBase

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from DisposedStatesDef
	*ss.DisposedStatesDef
	// inherit from [ssdbg.ServerStatesDef]
	*ssdbg.ServerStatesDef
}

// RelayGroupsDef contains all the state groups Relay state machine.
type RelayGroupsDef struct {
}

// RelaySchema represents all relations and properties of RelayStates.
var RelaySchema = SchemaMerge(
	// inherit from BasicStruct
	ss.BasicSchema,
	// inherit from DisposedStruct
	ss.DisposedSchema,
	// inherit from ServerSchema
	ssdbg.ServerSchema,
	am.Schema{
		// TODO own states
	})

// EXPORTS AND GROUPS

var (
	ssV = am.NewStates(RelayStatesDef{})
	sgV = am.NewStateGroups(RelayGroupsDef{})

	// RelayStates contains all the states for the Relay machine.
	RelayStates = ssV
	// RelayGroups contains all the state groups for the Relay machine.
	RelayGroups = sgV
)
