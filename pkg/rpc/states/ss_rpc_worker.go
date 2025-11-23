package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// WorkerStatesDef contains all the states of the Worker state machine.
type WorkerStatesDef struct {
	*am.StatesBase

	// errors

	// ErrProviding - Worker had issues providing the requested payload.
	ErrProviding string
	// ErrSendPayload - RPC server had issues sending the requested payload to
	// the RPC client.
	ErrSendPayload string

	// rpc getter

	// SendPayload - Worker delivered the requested payload to the RPC server
	// using rpc.Pass, rpc.A, and rpc.ArgsPayload.
	SendPayload string
}

// WorkerSchema represents all relations and properties of WorkerStates.
var WorkerSchema = SchemaMerge(
	am.Schema{

		// errors

		ssW.ErrProviding:   {Require: S{am.StateException}},
		ssW.ErrSendPayload: {Require: S{am.StateException}},

		// rcp getter

		ssW.SendPayload: {Multi: true},
	})

// EXPORTS AND GROUPS

var (
	// ssW is worker states from WorkerStatesDef.
	ssW = am.NewStates(WorkerStatesDef{})

	// WorkerStates contains all the states for the Worker machine.
	WorkerStates = ssW
)
