// Package states contains a stateful schema-v2 for a CLI.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// CliStatesDef contains all the states of the Cli state machine.
type CliStatesDef struct {
	*am.StatesBase

	Foo1 string
	Bar2 string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from DisposedStatesDef
	*ss.DisposedStatesDef
}

// CliGroupsDef contains all the state groups Cli state machine.
type CliGroupsDef struct {
}

// CliSchema represents all relations and properties of CliStates.
var CliSchema = SchemaMerge(
	// inherit from BasicStruct
	ss.BasicSchema,
	// inherit from DisposedStruct
	ss.DisposedSchema,
	am.Schema{
		ssV.Foo1: {
			Add:   S{ssV.Start},
			After: S{ssV.Start},
		},
		ssV.Bar2: {
			Add:   S{ssV.Start},
			After: S{ssV.Start},
		},
	})

// EXPORTS AND GROUPS

var (
	ssV = am.NewStates(CliStatesDef{})
	sgV = am.NewStateGroups(CliGroupsDef{})

	// CliStates contains all the states for the Cli machine.
	CliStates = ssV
	// CliGroups contains all the state groups for the Cli machine.
	CliGroups = sgV
)
