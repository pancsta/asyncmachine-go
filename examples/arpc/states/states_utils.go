package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names. See [am.S].
type S = am.S

// State is a type alias for a state definition. See [am.State].
type State = am.State

// Schema is a type alias for a map of state names to state definitions. See [am.Schema].
type Schema = am.Schema

// Exception is a type alias for the Exception state. See [am.StateException].
var Exception = am.StateException

// Start is a type alias for the Start state. See [am.StateStart].
var Start = am.StateStart

// Ready is a type alias for the Ready state. See [am.StateReady].
var Ready = am.StateReady
