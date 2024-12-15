package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ServerStatesDef contains all the states of the Client state machine.
type ServerStatesDef struct {

	// errors

	// ErrOnClient indicates an error added on the RPC worker, not the source
	// worker.
	ErrOnClient string

	// basics

	// Ready - Client is fully connected to the server.
	Ready string

	// rpc

	RpcStarting string
	RpcReady    string

	// TODO failsafe
	// RetryingCall    string
	// CallRetryFailed string

	ClientConnected string

	// inherit from SharedStatesDef
	*SharedStatesDef
}

// ServerGroupsDef contains all the state groups of the Client state machine.
type ServerGroupsDef struct {
	*SharedGroupsDef

	// Rpc is a group for RPC ready states.
	Rpc S
}

// ServerStruct represents all relations and properties of ClientStates.
var ServerStruct = StructMerge(
	// inherit from SharedStruct
	SharedStruct,
	am.Struct{

		ssS.ErrOnClient: {Require: S{Exception}},
		ssS.ErrNetwork: {
			Require: S{am.Exception},
			Remove:  S{ssS.ClientConnected},
		},

		// inject Server states into HandshakeDone
		ssS.HandshakeDone: StateAdd(
			SharedStruct[ssS.HandshakeDone],
			am.State{
				Remove: S{Exception},
			}),

		// Server

		ssS.Start: {Add: S{ssS.RpcStarting}},
		ssS.Ready: {
			Auto:    true,
			Require: S{ssS.HandshakeDone, ssS.RpcReady},
		},

		ssS.RpcStarting: {
			Require: S{ssS.Start},
			Remove:  sgS.Rpc,
		},
		ssS.RpcReady: {
			Require: S{ssS.Start},
			Remove:  sgS.Rpc,
		},

		ssS.ClientConnected: {
			Require: S{ssS.RpcReady},
		},
		// TODO ClientBye for graceful shutdowns
	})

// EXPORTS AND GROUPS

var (
	ssS = am.NewStates(ServerStatesDef{})
	sgS = am.NewStateGroups(ServerGroupsDef{
		Rpc: S{ssS.RpcStarting, ssS.RpcReady},
	}, SharedGroups)

	// ServerStates contains all the states for the Client machine.
	ServerStates = ssS
	// ServerGroups contains all the state groups for the Client machine.
	ServerGroups = sgS
)
