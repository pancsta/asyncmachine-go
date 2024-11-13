package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/states"
)

// WorkerStatesDef contains all the states of the Worker state machine.
type WorkerStatesDef struct {

	// errors

	// ErrProviding - Worker had issues providing the requested payload.
	ErrProviding string
	// ErrSendPayload - RPC server had issues sending the requested payload to
	// the RPC client.
	ErrSendPayload string

	// rpc getter

	// SendPayload - Worker delivered requested payload to the RPC server as
	// A{"payload": ArgsPayload, "name": "name"}. TODO use rpc.Pass
	SendPayload string

	// inherit from BasicStatesDef
	*states.BasicStatesDef
}

// WorkerStruct represents all relations and properties of WorkerStates.
var WorkerStruct = StructMerge(
	// inherit from BasicStruct
	states.BasicStruct,
	am.Struct{

		// errors

		ssW.ErrProviding:   {Require: S{am.Exception}},
		ssW.ErrSendPayload: {Require: S{am.Exception}},

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
