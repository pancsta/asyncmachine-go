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
	ss.ProcessingFile: {
		Remove: am.S{ss.FileProcessed},
	},
	ss.FileProcessed: {
		Remove: am.S{ss.ProcessingFile},
	},
	ss.InProgress: {
		Auto:    true,
		Require: am.S{ss.ProcessingFile},
	},
}

// EXPORTS

var (
	ss = am.NewStates(StatesDef{})
	// States contains all the states for the machine.
	States = ss
)
