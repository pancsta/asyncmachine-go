package generator

import (
	"fmt"
	"strings"

	"github.com/lithammer/dedent"
)

func GenStatesFile(states []string) string {
	sStruct := ""
	sDefs := ""
	sNames := ""

	for _, state := range states {
		if state == "" {
			continue
		}

		// capitalize the 1st letter, if not already
		if state[0] >= 'a' && state[0] <= 'z' {
			state = string(state[0]-32) + state[1:]
		}

		sStruct += "\t" + state + ": {},\n"
		sDefs += "\t" + state + " = \"" + state + "\"\n"
		sNames += "\t" + state + ",\n"
	}

	// TODO linebreaks
	// TODO SMerge to SAdd
	//  SSub
	sNames = strings.Trim(sNames, ",")
	sStruct = strings.Trim(sStruct, "\n")
	sDefs = strings.Trim(sDefs, "\n")

	return fmt.Sprintf(dedent.Dedent(strings.Trim(`
		package states
		
		import am "github.com/pancsta/asyncmachine-go/pkg/machine"
		
		// S is a type alias for a list of state names.
		type S = am.S
		
		// State is a type alias for a single state definition.
		type State = am.State
		
		// SAdd is a func alias for merging lists of states.
		var SAdd = am.SAdd
		
		// StateAdd is a func alias for adding to an existing state definition.
		var StateAdd = am.StateAdd
		
		// StateSet is a func alias for replacing parts of an existing state
		// definition.
		var StateSet = am.StateSet
		
		// StructMerge is a func alias for extending an existing state structure.
		var StructMerge = am.StructMerge
		
		// States structure defines relations and properties of states.
		var States = am.Struct{
		%s
		}

		// Groups of mutually exclusive states.
		
		//var (
		//	GroupPlaying = S{Playing, Paused}
		//)
		
		//#region boilerplate defs
		
		// Names of all the states (pkg enum).
		
		const (
		%s
		)
		
		// Names is an ordered list of all the state names.
		var Names = S{
		am.Exception,
		%s
		}
		
		//#endregion
	
	`, "\n")), sStruct, sDefs, sNames)
}
