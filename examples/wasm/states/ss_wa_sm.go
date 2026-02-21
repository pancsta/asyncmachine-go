package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

//

// FOO (server)

//

// FooStatesDef contains all the states of the Server state machine "foo".
type FooStatesDef struct {
	*am.StatesBase

	// Timeout without msgs.
	Bored string

	// inherit from SharedStatesDef
	*SharedStatesDef
}

// FooGroupsDef contains all the state groups Foo state machine.
type FooGroupsDef struct {
	// GroupName S
}

// FooSchema represents all relations and properties of FooStates.
var FooSchema = SchemaMerge(
	// inherit from SharedSchema
	SharedSchema,
	am.Schema{

		ssF.Bored: {},
		ssF.Msg: StateAdd(
			SharedSchema[ssF.Msg],
			am.State{
				Remove: S{ssF.Bored},
			}),
	})

// EXPORTS AND GROUPS

var (
	ssF = am.NewStates(FooStatesDef{})
	sgF = am.NewStateGroups(FooGroupsDef{
		// GroupName: S{...},
	})

	// FooStates contains all the states for the Foo machine.
	FooStates = ssF
	// FooGroups contains all the state groups for the Foo machine.
	FooGroups = sgF
)

//

// BAR (browser)

//

// BarStatesDef contains all the states of the Browser state machine "bar".
type BarStatesDef struct {
	*am.StatesBase

	SubmitMsg string

	// inherit from SharedStatesDef
	*SharedStatesDef
}

// BarGroupsDef contains all the state groups Bar state machine.
type BarGroupsDef struct {
	// GroupName S
}

// BarSchema represents all relations and properties of BarStates.
var BarSchema = SchemaMerge(
	// inherit from SharedSchema
	SharedSchema,
	am.Schema{

		ssB.SubmitMsg: {Multi: true},
	})

// EXPORTS AND GROUPS

var (
	ssB = am.NewStates(BarStatesDef{})
	sgB = am.NewStateGroups(BarGroupsDef{
		// GroupName: S{...},
	})

	// BarStates contains all the states for the Bar machine.
	BarStates = ssB
	// BarGroups contains all the state groups for the Bar machine.
	BarGroups = sgB
)

//

// SHARED (common for both server and browser)

//

// SharedStatesDef contains all the states of the Shared state machine.
type SharedStatesDef struct {
	*am.StatesBase

	// Message received and should be processed.
	Msg string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from rpc/StateSourceStatesDef
	*ssrpc.StateSourceStatesDef
}

// SharedGroupsDef contains all the state groups Shared state machine.
type SharedGroupsDef struct {
	// GroupName S
}

// SharedSchema represents all relations and properties of SharedStates.
var SharedSchema = SchemaMerge(
	// inherit from BasicSchema
	ss.BasicSchema,
	// inherit from rpc/WorkerSchema
	ssrpc.StateSourceSchema,
	am.Schema{

		ssS.Msg: {Multi: true},
	})

// EXPORTS AND GROUPS

var (
	ssS = am.NewStates(SharedStatesDef{})
	sgS = am.NewStateGroups(SharedGroupsDef{
		// GroupName: S{...},
	})

	// SharedStates contains all the states for the Shared machine.
	SharedStates = ssS
	// SharedGroups contains all the state groups for the Shared machine.
	SharedGroups = sgS
)
