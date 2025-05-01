package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

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
