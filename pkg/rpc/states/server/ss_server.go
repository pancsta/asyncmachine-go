package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// S is a type alias for a list of state names.
type S = am.S

// SMerge is a func alias for merging lists of states.
var SMerge = am.SAdd

// StructMerge is a func alias for extending an existing state structure.
var StructMerge = am.StructMerge

// StateAdd is a func alias for adding to an existing state definition.
var StateAdd = am.StateAdd

// States map defines relations and properties of states.
// Base on shared rpc states.
var States = StructMerge(ss.States, am.Struct{
	// Errors

	// Add a removal of ClientConn to ErrNetwork.
	ss.ErrNetwork: StateAdd(ss.States[ss.ErrNetwork], am.State{
		Remove: S{ClientConn},
	}),

	// Server

	ss.Start: {Add: S{RpcStarting}},
	ss.Ready: {
		Auto:    true,
		Require: S{HandshakeDone, RpcReady},
	},

	RpcStarting: {
		Require: S{ss.Start},
		Remove:  GroupRPC,
	},
	RpcReady: {
		Require: S{ss.Start},
		Remove:  GroupRPC,
	},

	ClientConn: {
		Require: S{RpcReady},
		Remove:  GroupClientConn,
	},
	ClientDisconn: {
		Auto:    true,
		Require: S{RpcReady},
		Remove:  GroupClientConn,
	},
	// TODO ClientBye for graceful shutdowns
})

// Groups of mutually exclusive states.

var (
	GroupRPC        = S{RpcStarting, RpcReady}
	GroupClientConn = S{ClientConn, ClientDisconn}
)

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	// Shared

	Start         = ss.Start
	Ready         = ss.Ready
	HandshakeDone = ss.HandshakeDone
	Handshaking   = ss.Handshaking

	// Server

	RpcStarting = "RpcStarting"
	RpcReady    = "RpcReady"

	ClientConn    = "ClientConn"
	ClientDisconn = "ClientDisconn"

	// Errors

	ErrNetwork = ss.ErrNetwork
	ErrClient  = ss.ErrRpc
)

// Names is an ordered list of all the state names.
var Names = SMerge(ss.Names, S{
	RpcStarting,
	RpcReady,

	ClientConn,
	ClientDisconn,
})

// #endregion
