package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
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

	// SendPayload - Worker delivered requested payload to the RPC server using
	// rpc.Pass, rpc.A, and rpc.ArgsPayload.
	SendPayload string

	// inherit from BasicStatesDef
	*states.BasicStatesDef
}

// WorkerSchema represents all relations and properties of WorkerStates.
var WorkerSchema = SchemaMerge(
	// inherit from BasicStruct
	states.BasicSchema,
	am.Schema{

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
