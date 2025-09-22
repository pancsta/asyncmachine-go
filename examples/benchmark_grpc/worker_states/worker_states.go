package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// State is a type alias for a single state definition.
type State = am.State

// States structure defines relations and properties of states.
var States = am.Schema{
	// toggle
	Start: {},

	// ops
	CallOp: {
		Multi:   true,
		Require: S{Start},
	},

	// events
	Event: {
		Multi:   true,
		Require: S{Start},
	},

	// values
	Value1: {Remove: GroupValues},
	Value2: {Remove: GroupValues},
	Value3: {Remove: GroupValues},
}

// Groups of mutually exclusive states.

var (
	GroupValues = S{Value1, Value2, Value3}
)

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	Start  = "Start"
	Event  = "Event"
	Value1 = "Value1"
	Value2 = "Value2"
	Value3 = "Value3"
	CallOp = "CallOp"
)

// Names is an ordered list of all the state names.
var Names = S{
	am.StateException,
	Start,
	Event,
	Value1,
	Value2,
	Value3,
	CallOp,
}

// #endregion
