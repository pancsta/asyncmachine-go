package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// SharedStatesDef contains all the states of the Worker state machine.
type SharedStatesDef struct {

	// errors

	ErrNetworkTimeout string
	ErrRpc            string
	ErrDelivery       string

	// connection

	HandshakeDone string
	Handshaking   string

	// inherit from BasicStatesDef
	*states.BasicStatesDef
}

// SharedGroupsDef contains all the state groups of the Worker state machine.
type SharedGroupsDef struct {

	// Work represents work-related states, 1 active at a time.
	Handshake S
}

// SharedSchema represents all relations and properties of WorkerStates.
var SharedSchema = SchemaMerge(
	// inherit from BasicStruct
	states.BasicSchema,
	am.Schema{

		// Errors
		s.ErrNetworkTimeout: {
			Add:     S{s.Exception},
			Require: S{s.Exception},
		},
		s.ErrRpc: {
			Add:     S{s.Exception},
			Require: S{s.Exception},
		},
		s.ErrDelivery: {
			Add:     S{s.Exception},
			Require: S{s.Exception},
		},

		// Handshake
		s.Handshaking: {
			Require: S{s.Start},
			Remove:  g.Handshake,
		},
		s.HandshakeDone: {
			Require: S{s.Start},
			Remove:  g.Handshake,
		},
	})

// EXPORTS AND GROUPS

var (
	// ws is worker states from SharedStatesDef.
	s = am.NewStates(SharedStatesDef{})

	// wg is worker groups from SharedGroupsDef.
	g = am.NewStateGroups(SharedGroupsDef{
		Handshake: S{s.Handshaking, s.HandshakeDone},
	})

	// SharedStates contains all the states shared RPC states.
	SharedStates = s

	// SharedGroups contains all the shared state groups for RPC.
	SharedGroups = g
)
