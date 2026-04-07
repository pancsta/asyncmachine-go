package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// StatesDef contains all the states of the state machine.
type StatesDef struct {
	*am.StatesBase

	// ProcessingFile - file is being processed (async)
	ProcessingFile string
	// FileProcessed - file has been processed (async)
	FileProcessed string
	// InProgress - processing is in progress (sync, auto)
	InProgress string
}

// Schema represents all relations and properties of States.
var Schema = am.Schema{
	"ProcessingFile": {
		Remove: am.S{"FileProcessed"},
	},
	"FileProcessed": {
		Remove: am.S{"ProcessingFile"},
	},
	"InProgress": {
		Auto:    true,
		Require: am.S{"ProcessingFile"},
	},
}

// EXPORTS

var (
	// States contains all the states for the machine.
	States = am.NewStates(StatesDef{})
)
