package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
var States = am.Struct{
	A: {
		Auto:    true,
		Require: S{C},
	},
	B: {
		Multi: true,
		Add:   S{C},
	},
	C: {
		After: S{D},
	},
	D: {
		Add: S{C, B},
	},
}

// Groups of mutually exclusive states.

//var (
//	GroupPlaying = S{Playing, Paused}
//)

//#region boilerplate defs

// Names of all the states (pkg enum).

const (
	A = "A"
	B = "B"
	C = "C"
	D = "D"
)

// Names is an ordered list of all the state names.
var Names = S{
	am.Exception,
	A,
	B,
	C,
	D,
}

//#endregion
