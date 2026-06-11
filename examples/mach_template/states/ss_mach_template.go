// Package states contains a stateful schema-v2 for MachTemplate.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

// MachTemplateStatesDef contains all the states of the MachTemplate state machine.
type MachTemplateStatesDef struct {
	*am.StatesBase

	ErrExample string
	Foo        string
	Bar        string
	Baz        string
	BazDone    string
	Channel    string

	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
	// inherit from ConnectedStatesDef
	*ssam.ConnectedStatesDef
	// inherit from DisposedStatesDef
	*ssam.DisposedStatesDef
	// inherit from StateSourceStatesDef
	*ssrpc.StateSourceStatesDef
}

// MachTemplateGroupsDef contains all the state groups MachTemplate state machine.
type MachTemplateGroupsDef struct {
	*ssam.ConnectedGroupsDef
	Group1 S
	Group2 S
}

// MachTemplateSchema represents all relations and properties of MachTemplateStates.
var MachTemplateSchema = ssam.BasicSchema.Merge(
	// inherit from ConnectedSchema
	ssam.ConnectedSchema,
	// inherit from DisposedSchema
	ssam.DisposedSchema,
	// inherit from StateSourceStatesDef
	ssrpc.StateSourceSchema,
	am.Schema{

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
	}, ssam.ConnectedGroups)

	// MachTemplateStates contains all the states for the MachTemplate machine.
	MachTemplateStates = ssM
	// MachTemplateGroups contains all the state groups for the MachTemplate machine.
	MachTemplateGroups = sgM
)
