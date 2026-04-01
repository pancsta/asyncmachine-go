package states

import (
	"slices"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

//

// ORCHESTRATOR

//

// OrchestratorStatesDef contains all the states of the Server state machine "orchestrator".
type OrchestratorStatesDef struct {
	*am.StatesBase

	ErrRpc string

	Ready string
	// all workers ready
	WorkersReady string
	// network ready (all workers ready + orchestrator ready)
	NetworkReady string
	// new conn from a browser
	BrowserConn string

	// trigger state per worker

	// StartWork will restart the workflow
	StartWork    string
	Browser1Work string
	Browser2Work string
	Browser3Work string
	Browser4Work string

	// payload delivered to the orchestrator

	Browser1Delivered string
	Browser2Delivered string
	Browser3Delivered string
	Browser4Delivered string
	// final workflow state
	WorkDelivered string

	// PIPED

	// RPC conn statuses (piped)

	Browser1Conn string
	Browser2Conn string

	// errors from workers (piped)

	ErrBrowser1 string
	ErrBrowser2 string
	ErrBrowser3 string
	ErrBrowser4 string

	// worker ready statuses (piped)

	Browser1Ready string
	Browser2Ready string
	Browser3Ready string
	Browser4Ready string

	// worker is working on a task (piped)

	Browser1Working string
	Browser2Working string
	Browser3Working string
	Browser4Working string

	// worker completed the task (piped)

	Browser1Completed string
	Browser2Completed string
	Browser3Completed string
	Browser4Completed string

	// worker failed the task after retries (piped)

	Browser1Failed string
	Browser2Failed string
	Browser3Failed string
	Browser4Failed string

	// inherit from [ss.BasicStatesDef]
	*ss.BasicStatesDef
	// 	inherit from [ssrpc.ConsumerStatesDef]
	*ssrpc.ConsumerStatesDef
	// inherit from [ssrpc.StateSourceStatesDef] (for REPL)
	*ssrpc.StateSourceStatesDef
}

// OrchestratorGroupsDef contains all the state groups Orchestrator state machine.
type OrchestratorGroupsDef struct {
	Delivered S
	Own       S
	Piped     S
}

// OrchestratorSchema represents all relations and properties of OrchestratorStates.
var OrchestratorSchema = SchemaMerge(
	// inherit from rpc/ConsumerSchema
	ssrpc.ConsumerSchema,
	// inherit from rpc/StateSourceSchema (for REPL)
	ssrpc.StateSourceSchema,
	// inherit from BasicSchema
	ss.BasicSchema,
	am.Schema{

		ssO.Ready: {},
		ssO.WorkersReady: {
			Auto: true,
			Require: S{
				ssO.Browser1Ready,
				ssO.Browser2Ready, ssO.Browser3Ready, ssO.Browser4Ready,
			},
		},
		ssO.NetworkReady: {
			Auto:    true,
			Require: S{ssO.Ready, ssO.WorkersReady},
			Add:     S{ssO.StartWork},
		},
		ssO.BrowserConn: {
			Multi:   true,
			Require: S{ssO.Start},
		},

		// WORKFLOW DAG

		// entry state (manual)
		ssO.StartWork: {
			Require: S{ssO.NetworkReady},
			Remove:  sgO.Delivered,
		},

		// further steps (automatic)
		ssO.Browser1Work: {Require: S{ssO.NetworkReady}},
		ssO.Browser2Work: {Require: S{ssO.NetworkReady}},
		ssO.Browser3Work: {Require: S{ssO.Browser1Delivered, ssO.NetworkReady}},
		ssO.Browser4Work: {Require: S{ssO.Browser2Delivered, ssO.NetworkReady}},

		// delivered states means payload delivered to the orchestrator

		ssO.Browser1Delivered: {Add: S{ssO.Browser3Work}},
		ssO.Browser2Delivered: {Add: S{ssO.Browser4Work}},
		ssO.Browser3Delivered: {},
		ssO.Browser4Delivered: {},

		// Workflow end
		ssO.WorkDelivered: {
			Auto:    true,
			Require: S{ssO.Browser3Delivered, ssO.Browser4Delivered},
		},

		// PIPED

		// from rpc

		ssO.ErrRpc:       {},
		ssO.Browser1Conn: {},
		ssO.Browser2Conn: {},

		// from netmachs

		ssO.ErrBrowser1: {},
		ssO.ErrBrowser2: {},
		ssO.ErrBrowser3: {},
		ssO.ErrBrowser4: {},

		ssO.Browser1Ready: {Require: S{ssO.Browser1Conn}},
		ssO.Browser2Ready: {Require: S{ssO.Browser2Conn}},
		ssO.Browser3Ready: {Require: S{ssO.Browser2Conn}},
		ssO.Browser4Ready: {Require: S{ssO.Browser2Conn}},

		ssO.Browser1Completed: {Require: S{ssO.Browser1Conn}},
		ssO.Browser2Completed: {Require: S{ssO.Browser2Conn}},
		ssO.Browser3Completed: {Require: S{ssO.Browser2Conn}},
		ssO.Browser4Completed: {Require: S{ssO.Browser2Conn}},

		ssO.Browser1Working: {Require: S{ssO.Browser1Conn}},
		ssO.Browser2Working: {Require: S{ssO.Browser2Conn}},
		ssO.Browser3Working: {Require: S{ssO.Browser2Conn}},
		ssO.Browser4Working: {Require: S{ssO.Browser2Conn}},

		ssO.Browser1Failed: {Require: S{ssO.Browser1Conn}},
		ssO.Browser2Failed: {Require: S{ssO.Browser2Conn}},
		ssO.Browser3Failed: {Require: S{ssO.Browser2Conn}},
		ssO.Browser4Failed: {Require: S{ssO.Browser2Conn}},
	})

// EXPORTS AND GROUPS

var (
	ssO = am.NewStates(OrchestratorStatesDef{})
	sgO = am.NewStateGroups(OrchestratorGroupsDef{
		Delivered: S{ssO.Browser1Delivered, ssO.Browser2Delivered, ssO.Browser3Delivered, ssO.Browser4Delivered},
		Own:       S{ssO.WorkersReady, ssO.NetworkReady, ssO.BrowserConn, ssO.StartWork, ssO.Browser1Work, ssO.Browser2Work, ssO.Browser3Work, ssO.Browser4Work, ssO.Browser1Delivered, ssO.Browser2Delivered, ssO.Browser3Delivered, ssO.Browser4Delivered, ssO.WorkDelivered},
		Piped:     S{ssO.ErrRpc, ssO.Browser1Conn, ssO.Browser2Conn, ssO.ErrBrowser1, ssO.ErrBrowser2, ssO.Browser1Ready, ssO.Browser2Ready, ssO.Browser1Completed, ssO.Browser2Completed, ssO.Browser1Working, ssO.Browser2Working, ssO.Browser1Failed, ssO.Browser2Failed},
	})

	// OrchestratorStates contains all the states for the Orchestrator machine.
	OrchestratorStates = ssO
	// OrchestratorGroups contains all the state groups for the Orchestrator machine.
	OrchestratorGroups = sgO
)

//

// WORKER

//

// WorkerStatesDef contains all the states of the Browser state machine "worker".
type WorkerStatesDef struct {
	*am.StatesBase

	// Working is the trigger state to start computing the hash (needs args)
	Working string
	Failed  string
	// Retrying is like Working, but stateful (doesn't need args)
	Retrying  string
	Completed string

	// piped

	RpcReady string

	// inherit from [ssrpc.StateSourceStatesDef]
	*ssrpc.StateSourceStatesDef
	// inherit from [ss.DisposedStatesDef]
	*ss.DisposedStatesDef
	// inherit from [ss.BasicStatesDef]
	*ss.BasicStatesDef
}

// WorkerGroupsDef contains all the state groups Worker state machine.
type WorkerGroupsDef struct {
	Work S
	// GroupName S
}

// WorkerSchema represents all relations and properties of WorkerStates.
var WorkerSchema = SchemaMerge(
	// inherit from rpc/StateSourceSchema
	ssrpc.StateSourceSchema,
	// inherit from DisposedSchema
	ss.DisposedSchema,
	// inherit from BasicSchema
	ss.BasicSchema,
	am.Schema{

		// basics

		// Start will unset on disconn, effectively creating a redo
		ssW.Start: {Require: S{ssW.RpcReady}},
		ssW.Exception: {
			Multi:  true,
			Remove: sgW.Work,
		},

		// self

		ssW.Working: {
			Require: S{ssW.Start},
			Remove:  SAdd(sgW.Work, S{Exception}),
		},
		ssW.Failed: {
			Require: S{ssW.Start},
			Remove:  sgW.Work,
		},
		ssW.Completed: {
			Require: S{ssW.Start},
			Remove:  sgW.Work,
		},
		ssW.Retrying: {
			Require: S{ssW.Start},
			Remove:  SAdd(sgW.Work, S{Exception}),
		},

		// piped

		ssW.RpcReady: {},
	})

// EXPORTS AND GROUPS

var (
	ssW = am.NewStates(WorkerStatesDef{})
	sgW = am.NewStateGroups(WorkerGroupsDef{
		Work: S{ssW.Working, ssW.Failed, ssW.Completed, ssW.Retrying},
	})

	// WorkerStates contains all the states for the Worker machine.
	WorkerStates = ssW
	// WorkerGroups contains all the state groups for the Worker machine.
	WorkerGroups = sgW
)

//

// BROWSER DISPATCHER

//
// The dispatcher proxies an aRPC conn for other workers (browser3 and borwser4), while itself
// being a worker.

// DispatcherStatesDef contains all the states of the Dispatcher state machine.
type DispatcherStatesDef struct {
	*am.StatesBase

	// init() merges states for piping out

	// work triggering states (outbound pipe, will pass args)
	Browser3Work string
	Browser4Work string

	Browser3Retry string
	Browser4Retry string

	// inherit from rpc/ConsumerStatesDef
	*ssrpc.ConsumerStatesDef
	// inherit from WorkerStatesDef
	*WorkerStatesDef
	// inherit from [ss.BasicStatesDef]
	*ss.BasicStatesDef
}

// DispatcherGroupsDef contains all the state groups Dispatcher state machine.
type DispatcherGroupsDef struct {
	*WorkerGroupsDef
}

// DispatcherSchema represents all relations and properties of DispatcherStates.
var DispatcherSchema = SchemaMerge(
	// inherit from rpc/ConsumerSchema
	ssrpc.ConsumerSchema,
	// inherit from WorkerSchema
	WorkerSchema,
	// inherit from BasicSchema
	ss.BasicSchema,
	am.Schema{

		// init() merges states for piping out

		ssD.Browser3Work: {Multi: true},
		ssD.Browser4Work: {Multi: true},

		ssD.Browser3Retry: {Multi: true},
		ssD.Browser4Retry: {Multi: true},
	})

// EXPORTS AND GROUPS

var (
	ssD = am.NewStates(DispatcherStatesDef{})
	sgD = am.NewStateGroups(DispatcherGroupsDef{}, sgW)

	// DispatcherStates contains all the states for the Dispatcher machine.
	DispatcherStates = ssD
	// DispatcherGroups contains all the state groups for the Dispatcher machine.
	DispatcherGroups = sgD
)

func init() {
	// append piped schemas for indirect workers (dynamic states)
	DispatcherSchema = am.SchemaMerge(
		DispatcherSchema,
		am.SchemaPrefix(WorkerSchema, "Browser3", false, nil, nil),
		am.SchemaPrefix(WorkerSchema, "Browser4", false, nil, nil),
	)
	ssD.SetNames(slices.Concat(
		ssD.Names(),
		am.StatesPrefix("Browser3", ssW.Names()),
		am.StatesPrefix("Browser4", ssW.Names()),
	))
}
