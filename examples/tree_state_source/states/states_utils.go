package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names. See [am.S].
type S = am.S

// State is a type alias for a state definition. See [am.State].
type State = am.State

// SAdd is a func alias for merging lists of states. See [am.SAdd].
var SAdd = am.SAdd

// StateAdd is a func alias for adding to an existing state definition. See [am.StateAdd].
var StateAdd = am.StateAdd

// StateSet is a func alias for replacing parts of an existing state
// definition. See [am.StateSet].
var StateSet = am.StateSet

// SchemaMerge is a func alias for extending an existing state structure. See [am.SchemaMerge].
var SchemaMerge = am.SchemaMerge
