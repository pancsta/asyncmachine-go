package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names. See [am.S].
type S = am.S

// State is a type alias for a state definition. See [am.State].
type State = am.State

// Schema is a type alias for a state definition. See [am.Schema].
type Schema = am.Schema

// Exception is a type alias for the Exception state. See [am.StateException].
var Exception = am.StateException

// Start is a type alias for the Start state. See [am.StateStart].
var Start = am.StateStart

// Ready is a type alias for the Ready state. See [am.StateReady].
var Ready = am.StateReady

// ----- deprecated

// SAdd is a func alias for merging lists of states. Deprecated.
var SAdd = am.SAdd

// StateAdd is a func alias for adding to an existing state definition.
// Deprecated.
var StateAdd = am.StateAdd

// StateSet is a func alias for replacing parts of an existing state
// definition. Deprecated.
var StateSet = am.StateSet

// SchemaMerge is a func alias for extending an existing state schema.
// Deprecated.
var SchemaMerge = am.SchemaMerge
