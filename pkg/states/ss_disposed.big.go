//go:build !tinygo

package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// DisposedStatesDef contains all the states of the Disposed state machine.
// One a machine implements this state mixing, it HAS TO be disposed using the
// Disposing state (instead of [am.Machine.Dispose]).
//
// Required states:
// - Start
type DisposedStatesDef struct {
	*am.StatesBase

	// RegisterDisposal registers a disposal handler passed under the
	// DisposedArgHandler key. Requires [DisposedHandlers] to be bound prior to
	// the registration. Handlers registered via RegisterDisposal can block.
	RegisterDisposal string
	// Disposing starts the machine disposal - first state-based and then calls
	// [am.Machine.Dispose].
	Disposing string
	// Disposed indicates that the machine has disposed allocated resources
	// and is ready to be garbage collected by calling [am.Machine.Dispose].
	Disposed string
}

// DisposedGroupsDef contains all the state groups Disposed state machine.
type DisposedGroupsDef struct{}

// DisposedSchema represents all relations and properties of DisposedStates.
var DisposedSchema = am.Schema{
	ssD.RegisterDisposal: {Multi: true},
	ssD.Disposing:        {Remove: S{ssB.Start}},
	ssD.Disposed:         {Remove: S{ssD.Disposing, ssB.Start}},
}

// EXPORTS AND GROUPS

var (
	ssD = am.NewStates(DisposedStatesDef{})
	sgD = am.NewStateGroups(DisposedGroupsDef{})

	// DisposedStates contains all the states for the Disposed machine.
	DisposedStates = ssD
	// DisposedGroups contains all the state groups for the Disposed machine.
	DisposedGroups = sgD
)
