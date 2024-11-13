package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/states"
)

// MuxStatesDef contains all the states of the Mux state machine.
// The target state is PortInfo, activated by an aRPC client.
type MuxStatesDef struct {
	// shadow duplicated StatesBase
	*am.StatesBase

	// basics

	// Ready - mux is ready to accept new clients.
	Ready string

	ClientConnected string
	HasClients      string
	// NewServerErr - new server returned an error. The mux is still running.
	NewServerErr string

	// inherit from BasicStatesDef
	*states.BasicStatesDef
}

// MuxStruct represents all relations and properties of MuxStatesDef.
var MuxStruct = states.StructMerge(
	states.BasicStruct,
	am.Struct{
		ssD.Exception: {
			Multi:  true,
			Remove: S{ssS.Ready},
		},

		ssD.Ready: {
			Require: S{ssS.Start},
		},

		ssD.ClientConnected: {
			Multi:   true,
			Require: states.S{ssD.Start},
		},
		ssD.HasClients:   {Require: states.S{ssD.Start}},
		ssD.NewServerErr: {},
	})

// EXPORTS AND GROUPS

var (
	ssD = am.NewStates(MuxStatesDef{})

	// MuxStates contains all the states for the Mux machine.
	MuxStates = ssD
)
