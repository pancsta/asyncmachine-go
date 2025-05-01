// Package global should be imported into the package's global scope with:
//
//	import _ "github.com/pancsta/asyncmachine-go/pkg/states/global"
//
// This removes the need for manual updates, with the cost of an implicit
// import.
package global

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// State is a type alias for a state definition. See [am.State].
type State = am.State

// SAdd is a func alias for merging lists of states.
var SAdd = am.SAdd

// StateAdd is a func alias for adding to an existing state definition.
var StateAdd = am.StateAdd

// StateSet is a func alias for replacing parts of an existing state
// definition.
var StateSet = am.StateSet

// SchemaMerge is a func alias for extending an existing state schema.
var SchemaMerge = am.SchemaMerge

// Exception is a type alias for the exception state.
var Exception = am.Exception
