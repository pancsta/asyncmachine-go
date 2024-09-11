package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
// Base on shared rpc states.
var States = am.StructMerge(ss.States, am.Struct{
	ss.Start: {Add: S{Connecting}},
	ss.Ready: {
		Auto:    true,
		Require: S{HandshakeDone},
	},

	// Connection
	Connecting: {
		Require: S{Start},
		Remove:  GroupConnected,
	},
	Connected: {
		Require: S{Start},
		Remove:  GroupConnected,
		Add:     S{Handshaking},
	},
	Disconnecting: {
		Remove: GroupConnected,
	},
	Disconnected: {
		Auto:   true,
		Remove: GroupConnected,
	},

	// Add a dependency on Connected to HandshakeDone.
	HandshakeDone: am.StateAdd(ss.States[ss.HandshakeDone], am.State{
		Require: S{Connected},
	}),
})

// Groups of mutually exclusive states.

var (
	GroupConnected = S{Connecting, Connected, Disconnecting, Disconnected}
)

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	// Shared

	ErrOnClient = ss.ErrOnClient

	Start         = ss.Start
	Ready         = ss.Ready
	HandshakeDone = ss.HandshakeDone
	Handshaking   = ss.Handshaking

	// Client

	Connecting    = "Connecting"
	Connected     = "Connected"
	Disconnecting = "Disconnecting"
	Disconnected  = "Disconnected"
)

// Names is an ordered list of all the state names.
var Names = am.SAdd(ss.Names, S{
	Connecting,
	Connected,
	Disconnecting,
	Disconnected,
})

// #endregion
