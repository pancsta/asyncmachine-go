package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states (for files).
var States = am.Schema{
	Init: {
		Add: S{Watching},
	},
	Watching: {
		Add:   S{Init},
		After: S{Init},
	},
	ChangeEvent: {
		Multi:   true,
		Require: S{Watching},
	},
	Refreshing: {
		Multi:  true,
		Remove: S{AllRefreshed},
	},
	Refreshed: {
		Multi: true,
	},
	AllRefreshed: {},
}

// StatesDir map defines relations and properties of states (for directories).
var StatesDir = am.Schema{
	Refreshing: {
		Remove: groupRefreshed,
	},
	Refreshed: {
		Remove: groupRefreshed,
	},
	DirDebounced: {
		Remove: groupRefreshed,
	},
	DirCached: {},
}

// Groups of mutually exclusive states.

var groupRefreshed = S{Refreshing, Refreshed, DirDebounced}

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	Init         = "Init"
	Watching     = "Watching"
	ChangeEvent  = "ChangeEvent"
	Refreshing   = "Refreshing"
	Refreshed    = "Refreshed"
	AllRefreshed = "AllRefreshed"

	// dir-only states

	DirDebounced = "DirDebounced"
	DirCached    = "DirCached"
)

// Names is an ordered list of all the state names for files.
var Names = S{Init, Watching, ChangeEvent, Refreshing, Refreshed, AllRefreshed}

// NamesDir is an ordered list of all the state names for directories.
var NamesDir = S{Refreshing, Refreshed, DirDebounced, DirCached}

// #endregion
