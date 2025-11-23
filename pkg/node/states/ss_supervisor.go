package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// SupervisorStatesDef contains all the states of the Supervisor state machine.
type SupervisorStatesDef struct {
	*am.StatesBase

	// errors

	ErrWorker string
	ErrPool   string

	// network

	LocalRpcReady  string
	PublicRpcReady string
	// Ready - Supervisor is ready to accept new clients.
	Ready string
	// Heartbeat checks the health of the worker pool and network connections.
	Heartbeat string

	// pool

	// PoolStarting - Supervisor is starting workers to meet the pool definition.
	PoolStarting string
	// NormalizingPool - Supervisor is re-spawning some workers.
	NormalizingPool string
	// PoolNormalized - Supervisor has normalized the pool. Check PoolReady for
	// the result.
	PoolNormalized string
	// PoolReady - Minimum amount of workers are ready.
	PoolReady string
	// TODO all warm warkers ready
	// PoolWarm string
	// WorkersAvailable - There are some idle workers in the pool.
	WorkersAvailable string

	// worker

	// ListWorkers is a getter returning a list of workers via a channel, with
	// filters.
	ListWorkers string
	// SetWorker is a setter for a worker, which can also delete worker entries.
	SetWorker string
	// ForkWorker - Supervisor starts forking a new worker by creating a new aRPC
	// server.
	ForkWorker string
	// ForkingWorker - Supervisor is forking a new worker.
	ForkingWorker string
	// WorkerForked - New worker connected to bootstrap.
	WorkerConnected string
	// WorkerForked - Supervisor has successfully forked a new worker.
	WorkerForked  string
	KillWorker    string
	KillingWorker string
	WorkerKilled  string
	// WorkerReady - One of the workers become ready.
	WorkerReady string
	// WorkerGone - One of the workers has disconnected.
	WorkerGone string

	// client

	// ClientConnected - At least 1 client is connected to the supervisor.
	ClientConnected string
	// ClientDisconnected - 1 Client has disconnected from the supervisor.
	ClientDisconnected string
	// ProvideWorker - Client requests a new worker.
	ProvideWorker string
	// WorkerIssues - Client complains about the worker.
	WorkerIssues string
	// ClientSendPayload - payload delivered to the RPC server with for clients
	// as mutation args.
	ClientSendPayload string

	// supervisor

	SuperConnected    string
	SuperDisconnected string
	// SuperSendPayload - payload delivered to the RPC server for supervisors
	// as mutation args.
	SuperSendPayload string

	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
	// inherit from WorkerStatesDef
	*ssrpc.WorkerStatesDef
}

// SupervisorGroupsDef contains all the state groups of the Supervisor state
// machine.
type SupervisorGroupsDef struct {
	*ssam.ConnectedGroupsDef

	// PoolStatus are pool's possible statuses, 1 active at a time.
	PoolStatus S
	// Errors list all possible errors of Supervisor.
	Errors S
	// PoolNormalized async
	PoolNormalized S
}

// SupervisorSchema represents all relations and properties of SupervisorStates.
var SupervisorSchema = SchemaMerge(
	// inherit from BasicSchema
	ssam.BasicSchema,
	// inherit from WorkerStruct
	ssrpc.WorkerSchema,
	am.Schema{

		// errors

		ssS.ErrWorker: {
			Require: S{ssS.Exception},
			Add:     S{ssS.NormalizingPool, ssS.Heartbeat},
		},
		ssS.ErrPool: {
			Require: S{ssS.Exception},
			Remove:  S{ssS.PoolNormalized},
			Add:     S{ssS.NormalizingPool},
		},

		// piped

		ssS.ClientConnected:    {Multi: true},
		ssS.ClientDisconnected: {Multi: true},
		ssS.SuperConnected:     {Multi: true},
		ssS.SuperDisconnected:  {Multi: true},
		ssS.LocalRpcReady:      {Require: S{ssS.Start}},
		ssS.PublicRpcReady:     {Require: S{ssS.Start}},
		ssS.WorkerReady: {
			Multi:   true,
			Require: S{ssS.Start},
			Add:     S{ssS.PoolReady},
		},
		ssS.WorkerGone: {
			Multi:   true,
			Require: S{ssS.Start},
			Remove:  S{ssS.PoolReady},
		},

		// basics

		ssS.Start: {Add: S{ssS.PoolStarting}},
		ssS.Ready: {Require: S{
			ssS.LocalRpcReady, ssS.PublicRpcReady, ssS.PoolReady}},
		ssS.Heartbeat: {Require: S{ssS.Start}},

		// rpc

		// disable SendPayload
		ssW.SendPayload: {Add: S{ssW.ErrSendPayload, ssW.Exception}},

		// worker pool

		ssS.PoolStarting: {
			Remove: sgS.PoolStatus,
			Add:    S{ssS.NormalizingPool},
		},
		ssS.PoolReady: {
			Require: S{ssS.Start},
			Remove:  sgS.PoolStatus,
			Add:     S{ssS.Heartbeat},
		},
		ssS.NormalizingPool: {
			Require: S{ssS.Start},
			Remove:  sgS.PoolNormalized,
			// ErrWorker marks problematic workers
			After: S{ssS.ErrWorker},
		},
		ssS.PoolNormalized: {
			Require: S{ssS.Start},
			Remove:  sgS.PoolNormalized,
			Add:     S{ssS.Heartbeat},
		},
		ssS.WorkersAvailable: {Require: S{ssS.PoolReady}},

		// worker

		ssS.ListWorkers: {
			Multi:   true,
			Require: S{ssS.Start},
		},
		ssS.SetWorker: {
			Multi:   true,
			Require: S{ssS.Start},
		},

		ssS.ForkWorker: {
			Multi:   true,
			Require: S{ssS.Start},
		},
		ssS.ForkingWorker: {
			Multi:   true,
			Require: S{ssS.Start},
		},
		ssS.WorkerConnected: {
			Multi:   true,
			Require: S{ssS.Start},
		},
		ssS.WorkerForked: {
			Multi:   true,
			Require: S{ssS.Start},
			Add:     S{ssS.Heartbeat},
		},
		ssS.KillWorker:    {Multi: true},
		ssS.KillingWorker: {Multi: true},
		ssS.WorkerKilled: {
			Multi: true,
			Add:   S{ssS.NormalizingPool, ssS.Heartbeat},
		},

		// client

		ssS.ProvideWorker: {
			Multi:   true,
			Require: S{ssS.WorkersAvailable},
		},
		ssS.WorkerIssues: {
			Multi: true,
		},
		ssS.ClientSendPayload: {Multi: true},

		// supervisor

		ssS.SuperSendPayload: {Multi: true},
	})

// EXPORTS AND GROUPS

var (
	// ssS is supervisor states from SupervisorStatesDef.
	ssS = am.NewStates(SupervisorStatesDef{})

	// sgS is supervisor groups from SupervisorGroupsDef.
	sgS = am.NewStateGroups(SupervisorGroupsDef{
		PoolStatus:     S{ssS.PoolStarting, ssS.PoolReady},
		Errors:         S{ssS.ErrWorker, ssS.ErrPool},
		PoolNormalized: S{ssS.PoolNormalized, ssS.NormalizingPool},
	})

	// SupervisorStates contains all the states for the Supervisor machine.
	SupervisorStates = ssS

	// SupervisorGroups contains all the state groups for the Supervisor machine.
	SupervisorGroups = sgS
)
