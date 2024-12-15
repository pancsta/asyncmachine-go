package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/states"
)

// ClientStatesDef contains all the states of the Client state machine.
type ClientStatesDef struct {
	*am.StatesBase

	Exception     string
	ErrWorker     string
	ErrSupervisor string

	// worker

	WorkerDisconnected  string
	WorkerConnecting    string
	WorkerConnected     string
	WorkerDisconnecting string
	WorkerReady         string
	// Ready - Client is connected to a worker and ready to delegate work and
	// receive payloads.
	Ready string

	// supervisor

	SuperDisconnected  string
	SuperConnecting    string
	SuperConnected     string
	SuperDisconnecting string
	// SuperReady - Client is fully connected to the Supervisor.
	SuperReady string
	// WorkerRequested - Client has requested a Worker from the Supervisor.
	WorkerRequested string

	// inherit from BasicStatesDef
	*states.BasicStatesDef
	// inherit from ConsumerStatesDef
	*ssrpc.ConsumerStatesDef
}

// ClientGroupsDef contains all the state groups of the Client state machine.
type ClientGroupsDef struct {
	*states.ConnectedGroupsDef
	// TODO?
}

// ClientStruct represents all relations and properties of ClientStates.
var ClientStruct = StructMerge(
	// inherit from BasicStruct
	states.BasicStruct,
	// inherit from ConsumerStruct
	ssrpc.ConsumerStruct,
	am.Struct{

		// errors

		ssC.ErrWorker:     {Require: S{Exception}},
		ssC.ErrSupervisor: {Require: S{Exception}},

		// piped

		ssC.SuperDisconnected:  {},
		ssC.SuperConnecting:    {},
		ssC.SuperConnected:     {},
		ssC.SuperDisconnecting: {},
		ssC.SuperReady:         {},

		ssC.WorkerDisconnected:  {},
		ssC.WorkerConnecting:    {},
		ssC.WorkerConnected:     {},
		ssC.WorkerDisconnecting: {},
		ssC.WorkerReady:         {Remove: S{ssC.WorkerRequested}},

		// client

		ssC.WorkerRequested: {Require: S{ssC.SuperReady}},
		ssC.Ready: {
			Auto:    true,
			Require: S{ssC.WorkerReady},
		},
	})

// TODO handlers iface

// EXPORTS AND GROUPS

var (
	ssC = am.NewStates(ClientStatesDef{})
	sgC = am.NewStateGroups(ClientGroupsDef{}, states.ConnectedGroups)

	// ClientStates contains all the states for the Client machine.
	ClientStates = ssC
	// ClientGroups contains all the state groups for the Client machine.
	ClientGroups = sgC
)
