// Package states contains a stateful schema-v2 for MachTemplate.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

// MachTemplateStatesDef contains all the states of the MachTemplate state machine.
type MachTemplateStatesDef struct {
	*am.StatesBase

	ErrExample string
	Foo string
	Bar string
	Baz string
	BazDone string
	Channel string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from ConnectedStatesDef
	*ss.ConnectedStatesDef
	// inherit from DisposedStatesDef
	*ss.DisposedStatesDef
}

// MachTemplateGroupsDef contains all the state groups MachTemplate state machine.
type MachTemplateGroupsDef struct {
	*ss.ConnectedGroupsDef
	Group1 S
	Group2 S
}

// MachTemplateStruct represents all relations and properties of MachTemplateStates.
var MachTemplateStruct = StructMerge(
	// inherit from BasicStruct
	ss.BasicStruct,
	// inherit from ConnectedStruct
	ss.ConnectedStruct,
	// inherit from DisposedStruct
	ss.DisposedStruct,
	am.Struct{

		ssM.ErrExample: {
			Require: S{ssM.Exception},
		},
		ssM.Foo: {
			Require: S{ssM.Bar},
		},
		ssM.Bar: {},
		ssM.Baz: {
			Multi: true,
		},
		ssM.BazDone: {
			Multi: true,
		},
		ssM.Channel: {},
})

// EXPORTS AND GROUPS

var (
	ssM = am.NewStates(MachTemplateStatesDef{})
	sgM = am.NewStateGroups(MachTemplateGroupsDef{
		Group1: S{},
		Group2: S{},
	}, ss.ConnectedGroups)

	// MachTemplateStates contains all the states for the MachTemplate machine.
	MachTemplateStates = ssM
	// MachTemplateGroups contains all the state groups for the MachTemplate machine.
	MachTemplateGroups = sgM
)
