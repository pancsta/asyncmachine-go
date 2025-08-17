package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

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

	// local docs

	// Ready indicates the remote worker is ready to be used.
	Ready string

	// worker delivers

	// WorkerDelivering is an optional indication that the server has started a
	// data transmission to the Client.
	WorkerDelivering string
	// WorkPayload allows the Consumer to bind his handlers and receive data
	// from the Client.
	WorkerPayload string

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
			Require: S{am.Exception},
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
			Add:    S{ssC.ErrNetwork, am.Exception},
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
