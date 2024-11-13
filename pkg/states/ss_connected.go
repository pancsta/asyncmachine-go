package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ConnectedStatesDef contains all the states of the Connected state machine.
type ConnectedStatesDef struct {
	Connecting    string
	Connected     string
	Disconnecting string
	Disconnected  string

	*am.StatesBase
}

// ConnectedGroupsDef contains all the state groups of the Connected state set.
type ConnectedGroupsDef struct {
	Connected S
}

// ConnectedStruct represents all relations and properties of ConnectedStates.
var ConnectedStruct = am.Struct{
	cs.Connecting: {
		Require: S{ssB.Start},
		Remove:  cg.Connected,
	},
	cs.Connected: {
		Require: S{ssB.Start},
		Remove:  cg.Connected,
	},
	cs.Disconnecting: {Remove: cg.Connected},
	cs.Disconnected: {
		Auto:   true,
		Remove: cg.Connected,
	},
}

// EXPORTS AND GROUPS

var (
	cs = am.NewStates(ConnectedStatesDef{})
	cg = am.NewStateGroups(ConnectedGroupsDef{
		Connected: S{
			cs.Connecting, cs.Connected, cs.Disconnecting,
			cs.Disconnected,
		},
	})

	// ConnectedStates contains all the states for the Connected state set.
	ConnectedStates = cs

	// ConnectedGroups contains all the state groups for the Connected state set.
	ConnectedGroups = cg
)
