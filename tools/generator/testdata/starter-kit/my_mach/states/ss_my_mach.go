package states

import (
	"context"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// MyMachStatesDef contains all the states of the [MyMach] state-machine.
type MyMachStatesDef struct {
	*am.StatesBase

	Wet   string
	Water string
	Dry   string

	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
	// inherit from DisposedStatesDef
	*ssam.DisposedStatesDef
	// inherit from rpc/StateSourceStatesDef
	*ssrpc.StateSourceStatesDef
}

// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
type MyMachGroupsDef struct{}

// MyMachSchema represents all relations and properties of [MyMachStates].
var MyMachSchema = Schema{}.Merge(
	// inherit from BasicSchema
	ssam.BasicSchema,
	// inherit from DisposedSchema
	ssam.DisposedSchema,
	// inherit from rpc/StateSourceSchema
	ssrpc.StateSourceSchema,
	Schema{
		ssM.Wet: {},
		ssM.Water: {
			Add:    S{ssM.Wet},
			Remove: S{ssM.Dry},
		},
		ssM.Dry: {
			Auto:   true,
			Remove: S{ssM.Water},
		},
	},
)

// EXPORTS AND GROUPS
var (
	ssM = am.NewStates(MyMachStatesDef{})
	sgM = am.NewStateGroups(MyMachGroupsDef{})

	// MyMachStates contains all the states for the [MyMach] state-machine.
	MyMachStates = ssM
	// MyMachGroups contains all the state groups for the [MyMach] state-machine.
	MyMachGroups = sgM
)

// NewMyMach creates a new [MyMach] state-machine in the most basic form.
func NewMyMach(ctx context.Context) *am.Machine {
	return am.New(ctx, MyMachSchema, nil)
}
