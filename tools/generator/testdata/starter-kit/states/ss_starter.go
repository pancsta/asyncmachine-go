package states

import (
	"context"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// StarterStatesDef contains all the states of the [Starter] state-machine.
type StarterStatesDef struct {
	*am.StatesBase

	Start            string
	BaseDBReady      string
	BaseDBSaving     string
	BaseDBStarting   string
	CharacterReady   string
	CheckStories     string
	CheckingMenuRefs string
	RestoreCharacter string
	GenCharacter     string

	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
	// inherit from DisposedStatesDef
	*ssam.DisposedStatesDef
}

// StarterGroupsDef contains all the state groups [Starter] state-machine.
type StarterGroupsDef struct{}

// StarterSchema represents all relations and properties of [StarterStates].
var StarterSchema = am.Schema{}.Merge(
	// inherit from BasicSchema
	ssam.BasicSchema,
	// inherit from DisposedSchema
	ssam.DisposedSchema,
	am.Schema{
		ssS.Start: {},
		ssS.BaseDBReady: {
			Remove: S{ssS.BaseDBStarting},
		},
		ssS.BaseDBSaving: {
			Multi: true,
		},
		ssS.BaseDBStarting: {
			Remove: S{ssS.BaseDBReady},
		},
		ssS.CharacterReady: {
			Remove: S{ssS.RestoreCharacter, ssS.GenCharacter},
		},
		ssS.CheckStories: {
			Multi:   true,
			Require: S{ssS.Start},
		},
		ssS.CheckingMenuRefs: {
			Multi:   true,
			Require: S{ssS.Start},
			Tags:    []string{"args"},
		},
		ssS.RestoreCharacter: {},
		ssS.GenCharacter:     {},
	},
)

// EXPORTS AND GROUPS
var (
	ssS = am.NewStates(StarterStatesDef{})
	sgS = am.NewStateGroups(StarterGroupsDef{})

	// StarterStates contains all the states for the [Starter] state-machine.
	StarterStates = ssS
	// StarterGroups contains all the state groups for the [Starter] state-machine.
	StarterGroups = sgS
)

// NewStarter creates a new [Starter] state-machine in the most basic form.
func NewStarter(ctx context.Context) *am.Machine {
	return am.New(ctx, StarterSchema, nil)
}
