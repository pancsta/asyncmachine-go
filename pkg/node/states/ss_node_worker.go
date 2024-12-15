package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// WorkerStatesDef contains all the states of the Worker state machine.
type WorkerStatesDef struct {
	*am.StatesBase

	// errors

	ErrWork        string
	ErrWorkTimeout string
	ErrClient      string
	ErrSupervisor  string

	// basics

	// Ready - Worker is able to perform work.
	Ready string

	// rpc

	// LocalRpcReady - Supervisor RPC server is ready for connections.
	LocalRpcReady string
	// PublicRpcReady - Client RPC server is ready for connections.
	PublicRpcReady string
	// RpcReady - both RPC servers are ready.
	RpcReady string
	// SuperConnected - Worker is connected to the Supervisor.
	SuperConnected string
	// ServeClient - Worker is requested to accept a connection from client
	// am.A["id"].
	ServeClient string
	// ClientConnected - Worker is connected to a client.
	ClientConnected   string
	ClientSendPayload string
	SuperSendPayload  string

	// work

	Idle          string
	WorkRequested string
	Working       string
	WorkReady     string

	// inherit from WorkerStatesDef
	*ssrpc.WorkerStatesDef
}

// WorkerGroupsDef contains all the state groups of the Worker state machine.
type WorkerGroupsDef struct {

	// WorkStatus represents work-related states, 1 active at a time. This group
	// has to be bound by the implementation, e.g. using Add relation from custom
	// work states.
	WorkStatus S
}

// WorkerStruct represents all relations and properties of WorkerStates.
var WorkerStruct = StructMerge(
	// inherit from BasicStruct
	ssrpc.WorkerStruct,
	am.Struct{

		// errors

		ssW.ErrWork:        {Require: S{Exception}},
		ssW.ErrWorkTimeout: {Require: S{Exception}},
		ssW.ErrClient:      {Require: S{Exception}},
		ssW.ErrSupervisor:  {Require: S{Exception}},

		// piped

		ssW.LocalRpcReady:   {Require: S{ssW.Start}},
		ssW.PublicRpcReady:  {Require: S{ssW.Start}},
		ssW.SuperConnected:  {Require: S{ssW.Start}},
		ssW.ClientConnected: {Require: S{ssW.Start}},

		// basics

		ssW.Ready: {Require: S{ssW.LocalRpcReady}},

		// rpc

		ssW.RpcReady: {
			Auto:    true,
			Require: S{ssW.LocalRpcReady, ssW.PublicRpcReady},
		},
		ssW.ServeClient: {Require: S{ssW.PublicRpcReady}},
		ssW.ClientSendPayload: {
			Require: S{ssW.PublicRpcReady},
		},
		ssW.SuperSendPayload: {
			Require: S{ssW.LocalRpcReady},
		},

		// disable SendPayload
		ssW.SendPayload: {Add: S{ssW.ErrSendPayload, ssW.Exception}},

		// work

		ssW.Idle: {
			Auto:    true,
			Require: S{ssW.Ready},
			Remove:  sgW.WorkStatus,
		},
		ssW.WorkRequested: {
			Require: S{ssW.Ready},
			Remove:  sgW.WorkStatus,
		},
		ssW.Working: {
			Require: S{ssW.Ready},
			Remove:  sgW.WorkStatus,
		},
		ssW.WorkReady: {
			Require: S{ssW.Ready},
			Remove:  sgW.WorkStatus,
		},
	})

// EXPORTS AND GROUPS

var (
	// ssW is worker states from WorkerStatesDef.
	ssW = am.NewStates(WorkerStatesDef{})

	// sgW is worker groups from WorkerGroupsDef.
	sgW = am.NewStateGroups(WorkerGroupsDef{
		WorkStatus: S{ssW.WorkRequested, ssW.Working, ssW.WorkReady, ssW.Idle},
	})

	// WorkerStates contains all the states for the Worker machine.
	WorkerStates = ssW

	// WorkerGroups contains all the state groups for the Worker machine.
	WorkerGroups = sgW
)
