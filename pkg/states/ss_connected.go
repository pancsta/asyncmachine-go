package states

import (
	"errors"
	"fmt"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ConnectedStatesDef contains states for a connection status.
// Required states:
// - Start
type ConnectedStatesDef struct {
	// ErrConnecting is a detailed connection error, eg no access.
	ErrConnecting string

	Connecting    string
	Connected     string
	Disconnecting string
	Disconnected  string

	*am.StatesBase
}

// ConnectedGroupsDef contains all the state groups of the Connected state set.
type ConnectedGroupsDef struct {
	Connected S
}

// ConnectedStruct represents all relations and properties of ConnectedStates.
var ConnectedStruct = am.Struct{
	cs.ErrConnecting: {Require: S{Exception}},

	cs.Connecting: {
		Require: S{ssB.Start},
		Remove:  cg.Connected,
	},
	cs.Connected: {
		Require: S{ssB.Start},
		Remove:  cg.Connected,
	},
	cs.Disconnecting: {Remove: cg.Connected},
	cs.Disconnected: {
		Auto:   true,
		Remove: cg.Connected,
	},
}

// EXPORTS AND GROUPS

var (
	cs = am.NewStates(ConnectedStatesDef{})
	cg = am.NewStateGroups(ConnectedGroupsDef{
		Connected: S{
			cs.Connecting, cs.Connected, cs.Disconnecting,
			cs.Disconnected,
		},
	})

	// ConnectedStates contains all the states for the Connected state set.
	ConnectedStates = cs

	// ConnectedGroups contains all the state groups for the Connected state set.
	ConnectedGroups = cg
)

// ERRORS

// sentinel errors

var ErrConnecting = errors.New("error connecting")

// error mutations

// AddErrConnecting wraps an error in the ErrConnecting sentinel and adds to a
// machine.
func AddErrConnecting(
	event *am.Event, mach *am.Machine, err error, args am.A,
) error {
	err = fmt.Errorf("%w: %w", ErrConnecting, err)
	mach.EvAddErrState(event, cs.ErrConnecting, err, args)

	return err
}
