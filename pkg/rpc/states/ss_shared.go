package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
var States = am.Struct{
	// Errors
	ErrNetwork:  {Require: S{am.Exception}},
	ErrRpc:      {Require: S{am.Exception}},
	ErrOnClient: {Require: S{am.Exception}},

	Start: {},

	// Handshake
	Handshaking: {
		Require: S{Start},
		Remove:  GroupHandshake,
	},
	HandshakeDone: {
		Require: S{Start},
		Remove:  GroupHandshake,
	},
}

// Groups of mutually exclusive states.

var (
	GroupHandshake = S{Handshaking, HandshakeDone}
)

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	ErrNetwork  = "ErrNetwork"
	ErrRpc      = "ErrRpc"
	ErrOnClient = "ErrOnClient"

	Start = "Start"
	Ready = "Ready"

	HandshakeDone = "HandshakeDone"
	Handshaking   = "Handshaking"
)

// Names is an ordered list of all the state names.
var Names = S{
	am.Exception,

	ErrNetwork,
	ErrRpc,

	// ErrOnClient indicates that the error happened on the RPC client, and not
	// on the remote machine.
	ErrOnClient,

	Start,
	Ready,

	HandshakeDone,
	Handshaking,
}

// #endregion
