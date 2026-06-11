package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// ///// ///// /////

// ///// SERVER

// ///// ///// /////

// ServerStatesDef contains all the states of the Client state machine.
type ServerStatesDef struct {
	*am.StatesBase

	// basics

	// Ready - Client is fully connected to the server.
	Ready string

	// rpc

	// Starting listening
	RpcStarting string
	// setting up RPC accepting
	RpcAccepting string
	// RPC is accepting or has accepted connections
	RpcReady string

	// RPC client connected (technically)
	ClientConnected string
	// RPC client fully usable
	HandshakeDone string

	// How many times the client requested a full sync.
	MetricSync string
	// TCP tunneled over websocket
	WebSocketTunnel string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
}

// ServerGroupsDef contains all the state groups of the Client state machine.
type ServerGroupsDef struct {
	// Rpc is a group for RPC ready states.
	Rpc S
}

// ServerSchema represents all relations and properties of ClientStates.
var ServerSchema = ss.BasicSchema.Merge(
	am.Schema{

		ssS.ErrNetwork: {
			Require: S{am.StateException},
			Remove:  S{ssS.ClientConnected},
		},

		// inject Server states into HandshakeDone
		ssS.HandshakeDone: {
			Require: S{ssS.ClientConnected},
			Remove:  S{Exception},
		},

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
		ssS.RpcAccepting: {
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

		ssS.MetricSync:      {Multi: true},
		ssS.WebSocketTunnel: {},
	})

// EXPORTS AND GROUPS

var (
	ssS = am.NewStates(ServerStatesDef{})
	sgS = am.NewStateGroups(ServerGroupsDef{
		Rpc: S{ssS.RpcStarting, ssS.RpcAccepting, ssS.RpcReady},
	})

	// ServerStates contains all the states for the Client machine.
	ServerStates = ssS
	// ServerGroups contains all the state groups for the Client machine.
	ServerGroups = sgS
)
