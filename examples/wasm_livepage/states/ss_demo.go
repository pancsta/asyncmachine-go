//go:build !tinygo

//go:generate go run ./gen

package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

// DemoStatesDef contains all the states of the demo state machine.
type DemoStatesDef struct {
	*am.StatesBase

	// Incr increments the counter.
	Incr string
	// Msg is a message from the server.
	Msg string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from rpc/StateSourceStatesDef
	*ssrpc.StateSourceStatesDef
}

// DemoGroupsDef contains all the state groups of the demo state machine.
type DemoGroupsDef struct {
	// GroupName S
}

// DemoSchema represents all relations and properties of DemoStates.
var DemoSchema = ss.BasicSchema.Merge(
	// inherit from rpc/StateSourceSchema
	ssrpc.StateSourceSchema,
	am.Schema{

		ssD.Incr: {Multi: true},
		ssD.Msg:  {Multi: true},
	})

// EXPORTS AND GROUPS

var (
	ssD = am.NewStates(DemoStatesDef{})
	sgD = am.NewStateGroups(DemoGroupsDef{
		// GroupName: S{...},
	})

	// DemoStates contains all the states for the Demo machine.
	DemoStates = ssD
	// DemoGroups contains all the state groups for the Demo machine.
	DemoGroups = sgD
)
