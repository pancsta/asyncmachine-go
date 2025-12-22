package states

import (
	"context"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

// TuiStatesDef contains all the states of the Tui state-machine.
type TuiStatesDef struct {
	*am.StatesBase

	Resized string
	IncomingData string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
}

// TuiGroupsDef contains all the state groups Tui state-machine.
type TuiGroupsDef struct {
}

// TuiSchema represents all relations and properties of TuiStates.
var TuiSchema = SchemaMerge(
	// inherit from BasicSchema
	ss.BasicSchema,
	am.Schema{

		ssT.Resized: {},
		ssT.IncomingData: {},
})

// EXPORTS AND GROUPS

var (
	ssT = am.NewStates(TuiStatesDef{})
	sgT = am.NewStateGroups(TuiGroupsDef{})

	// TuiStates contains all the states for the Tui state-machine.
	TuiStates = ssT
	// TuiGroups contains all the state groups for the Tui state-machine.
	TuiGroups = sgT
)

// NewTui creates a new Tui state-machine in the most basic form.
func NewTui(ctx context.Context) *am.Machine {
	return am.New(ctx, TuiSchema, nil)
}
