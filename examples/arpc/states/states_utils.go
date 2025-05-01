package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// SAdd is a func alias for merging lists of states.
var SAdd = am.SAdd

// StateAdd is a func alias for adding to an existing state definition.
var StateAdd = am.StateAdd

// StateSet is a func alias for replacing parts of an existing state
// definition.
var StateSet = am.StateSet

// SchemaMerge is a func alias for extending an existing state structure.
var SchemaMerge = am.SchemaMerge
