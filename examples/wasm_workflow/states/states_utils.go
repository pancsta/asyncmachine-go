package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// State is a type alias for a state definition. See [am.State].
type State = am.State

// Exception is a type alias for the Exception state.
var Exception = am.StateException

// Start is a type alias for the Start state.
var Start = am.StateStart

// Ready is a type alias for the Ready state.
var Ready = am.StateReady
