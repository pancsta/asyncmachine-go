package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// ///// ///// /////

// ///// SHARED

// ///// ///// /////

// SharedStatesDef contains all the states of the Shared state machine.
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

// SharedGroupsDef contains all the state groups of the Shared state machine.
type SharedGroupsDef struct {

	// Work represents work-related states, 1 active at a time.
	Handshake S
}

// SharedSchema represents all relations and properties of NetSourceStates.
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

// ///// ///// /////

// ///// NETWORK SOURCE

// ///// ///// /////

// NetSourceStatesDef contains all the states of the Network Source state
// machine.
type NetSourceStatesDef struct {
	*am.StatesBase

	// errors

	// ErrOnClient indicates an error added on the Network Machine.
	ErrOnClient string
	// ErrProviding - Worker had issues providing the requested payload.
	ErrProviding string
	// ErrSendPayload - RPC server had issues sending the requested payload to
	// the RPC client.
	ErrSendPayload string

	// rpc getter

	// SendPayload - Net Source has delivered the requested payload to the RPC
	// server (not the Consumer) using rpc.Pass, rpc.A, and rpc.MsgSrvPayload.
	SendPayload string
}

// NetSourceSchema represents all relations and properties of NetSourceStates.
var NetSourceSchema = SchemaMerge(
	am.Schema{

		// errors

		ssNS.ErrOnClient:    {Require: S{Exception}},
		ssNS.ErrProviding:   {Require: S{Exception}},
		ssNS.ErrSendPayload: {Require: S{Exception}},

		// RPC getter

		ssNS.SendPayload: {Multi: true},
	})

// EXPORTS AND GROUPS

var (
	// ssNS are states from NetSourceStatesDef.
	ssNS = am.NewStates(NetSourceStatesDef{})

	// NetSourceStates contains all the states for the Network Source machine.
	NetSourceStates = ssNS
)

// ///// ///// /////

// ///// SERVER

// ///// ///// /////

// ServerStatesDef contains all the states of the Client state machine.
type ServerStatesDef struct {

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
	// overrides shared HandshakeDone
	HandshakeDone string

	// How many times the client requested a full sync.
	MetricSync string

	// inherit from SharedStatesDef
	*SharedStatesDef
}

// ServerGroupsDef contains all the state groups of the Client state machine.
type ServerGroupsDef struct {
	*SharedGroupsDef

	// Rpc is a group for RPC ready states.
	Rpc S
}

// ServerSchema represents all relations and properties of ClientStates.
var ServerSchema = SchemaMerge(
	// inherit from SharedStruct
	SharedSchema,
	am.Schema{

		ssS.ErrNetwork: {
			Require: S{am.StateException},
			Remove:  S{ssS.ClientConnected},
		},

		// inject Server states into HandshakeDone
		ssS.HandshakeDone: StateAdd(
			SharedSchema[ssS.HandshakeDone],
			am.State{
				Require: S{ssS.ClientConnected},
				// TODO why?
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

		ssS.MetricSync: {Multi: true},
	})

// EXPORTS AND GROUPS

var (
	ssS = am.NewStates(ServerStatesDef{})
	sgS = am.NewStateGroups(ServerGroupsDef{
		// TODO remove 2-state group?
		Rpc: S{ssS.RpcStarting, ssS.RpcReady},
	}, SharedGroups)

	// ServerStates contains all the states for the Client machine.
	ServerStates = ssS
	// ServerGroups contains all the state groups for the Client machine.
	ServerGroups = sgS
)

// ///// ///// /////

// ///// CLIENT

// ///// ///// /////

// ClientStatesDef contains all the states of the Client state machine.
type ClientStatesDef struct {
	// shadow duplicated StatesBase
	*am.StatesBase

	// failsafe

	RetryingCall string
	// TODO should be ErrCallRetry, req:Exception
	CallRetryFailed string
	RetryingConn    string
	// TODO should be ErrConnRetry, req:Exception
	ConnRetryFailed string

	// local overrides

	// Ready indicates the remote worker is ready to be used.
	Ready         string
	HandshakeDone string

	// worker delivers

	// WorkerDelivering is an optional indication that the server has started a
	// data transmission to the Client.
	WorkerDelivering string
	// WorkPayload allows the Consumer to bind his handlers and receive data
	// from the Client.
	WorkerPayload string

	// How many times the client requested a full sync.
	MetricSync string

	// inherit from SharedStatesDef
	*SharedStatesDef
	// inherit from ConnectedStatesDef
	*states.ConnectedStatesDef
}

// ClientGroupsDef contains all the state groups of the Client state machine.
type ClientGroupsDef struct {
	*SharedGroupsDef
	*states.ConnectedGroupsDef
	// TODO
}

// ClientSchema represents all relations and properties of ClientStates.
var ClientSchema = SchemaMerge(
	// inherit from SharedStruct
	SharedSchema,
	// inherit from ConnectedStruct
	states.ConnectedSchema,
	am.Schema{

		// Try to RetryingConn on ErrNetwork.
		ssC.ErrNetwork: {
			Require: S{am.StateException},
			Remove:  S{ssC.Connecting},
		},

		ssC.Start: {
			Add:    S{ssC.Connecting},
			Remove: S{ssC.ConnRetryFailed},
		},
		ssC.Ready: {
			Auto:    true,
			Require: S{ssC.HandshakeDone},
		},

		// inject Client states into Connected
		ssC.Connected: StateAdd(
			states.ConnectedSchema[states.ConnectedStates.Connected],
			am.State{
				Remove: S{ssC.RetryingConn},
				Add:    S{ssC.Handshaking},
			}),

		// inject Client states into Handshaking
		ssC.Handshaking: StateAdd(
			SharedSchema[s.Handshaking],
			am.State{
				Require: S{ssC.Connected},
			}),

		// inject Client states into HandshakeDone
		ssC.HandshakeDone: am.StateAdd(
			SharedSchema[ssC.HandshakeDone], am.State{
				// HandshakeDone will depend on Connected.2
				Require: S{ssC.Connected},
			}),

		// Retrying

		ssC.RetryingCall: {Require: S{ssC.Start}},
		ssC.CallRetryFailed: {
			Remove: S{ssC.RetryingCall},
			Add:    S{ssC.ErrNetwork, am.StateException},
		},
		ssC.RetryingConn:    {Require: S{ssC.Start}},
		ssC.ConnRetryFailed: {Remove: S{ssC.Start}},

		// worker delivers

		ssC.WorkerDelivering: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.WorkerPayload: {
			Multi:   true,
			Require: S{ssC.Connected},
		},

		ssC.MetricSync: {Multi: true},
	})

// EXPORTS AND GROUPS

var (
	ssC = am.NewStates(ClientStatesDef{})
	sgC = am.NewStateGroups(ClientGroupsDef{}, states.ConnectedGroups,
		SharedGroups)

	// ClientStates contains all the states for the Client machine.
	ClientStates = ssC
	// ClientGroups contains all the state groups for the Client machine.
	ClientGroups = sgC
)

// ///// ///// /////

// ///// MUX

// ///// ///// /////

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

// MuxSchema represents all relations and properties of MuxStatesDef.
var MuxSchema = SchemaMerge(
	states.BasicSchema,
	am.Schema{
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

// ///// ///// /////

// ///// CONSUMER

// ///// ///// /////

// ConsumerStatesDef contains all the states of the Consumer state machine.
type ConsumerStatesDef struct {
	*am.StatesBase
	Exception string

	// WorkerPayload RPC server delivers the requested payload to the Client.
	WorkerPayload string
}

// ConsumerSchema represents all relations and properties of ConsumerStates.
var ConsumerSchema = am.Schema{
	ssCo.WorkerPayload: {Multi: true},
}

// ConsumerHandlers is the required interface for Consumer's state handlers.
type ConsumerHandlers interface {
	WorkerPayloadState(e *am.Event)
}

// EXPORTS AND GROUPS

var (
	// ssCo is Consumer states from ConsumerStatesDef.
	ssCo = am.NewStates(ConsumerStatesDef{})

	// ConsumerStates contains all the states for the Consumer machine.
	ConsumerStates = ssCo
)
