package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// ExampleStatesDef contains all the states of the Example state machine.
type ExampleStatesDef struct {
	*am.StatesBase

	Foo string
	Bar string
	Baz string

	// inherit from rpc/WorkerStatesDef
	*ssrpc.WorkerStatesDef
}

// ExampleGroupsDef contains all the state groups Example state machine.
type ExampleGroupsDef struct {
	Mutex S
}

// ExampleStruct represents all relations and properties of ExampleStates.
var ExampleStruct = StructMerge(
	// inherit from rpc/WorkerStruct
	ssrpc.WorkerStruct,
	am.Struct{

		ssE.Foo: {Remove: sgE.Mutex},
		ssE.Bar: {Remove: sgE.Mutex},
		ssE.Baz: {Remove: sgE.Mutex},
})

// EXPORTS AND GROUPS

var (
	ssE = am.NewStates(ExampleStatesDef{})
	sgE = am.NewStateGroups(ExampleGroupsDef{
		Mutex: S{ssE.Foo, ssE.Bar, ssE.Baz},
	})

	// ExampleStates contains all the states for the Example machine.
	ExampleStates = ssE
	// ExampleGroups contains all the state groups for the Example machine.
	ExampleGroups = sgE
)
