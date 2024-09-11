package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// State is a type alias for a single state definition.
type State = am.State

// SAdd is a func alias for adding lists of states to a list of states.
var SAdd = am.SAdd

// SExtend is a func alias for extending an existing state definition.
var SExtend = am.StateAdd

// StructMerge is a func alias for extending an existing state structure.
var StructMerge = am.StructMerge

// States map defines relations and properties of states.
var States = am.Struct{
	// Errors

	ErrNetwork: {Require: S{am.Exception}},

	Start: {},
	Ready: {},

	// Disconnected -> Connected

	Connecting: {
		Require: S{Start},
		Remove:  GroupConnected,
	},
	Connected: {
		Require: S{Start},
		Remove:  GroupConnected,
	},
	Disconnecting: {Remove: GroupConnected},
	Disconnected: {
		Auto:   true,
		Remove: GroupConnected,
	},
}

// Groups of mutually exclusive states.

var GroupConnected = S{Connecting, Connected, Disconnecting, Disconnected}

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	ErrNetwork = "ErrNetwork"

	Start = "Start"
	Ready = "Ready"

	Connecting    = "Connecting"
	Connected     = "Connected"
	Disconnecting = "Disconnecting"
	Disconnected  = "Disconnected"
)

// Names is an ordered list of all the state names.
var Names = S{
	am.Exception,

	ErrNetwork,

	Start,
	Ready,

	Connecting,
	Connected,
	Disconnecting,
	Disconnected,
}

// #endregion
