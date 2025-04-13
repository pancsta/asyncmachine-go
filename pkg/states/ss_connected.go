package states

// TODO rename to Schem
// TODO rename to Schema

import (
	"errors"
	"fmt"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ///// ///// /////

// ///// CONECTED

// TODO document both
// ///// ///// /////

type ConnectedState = string

// ConnectedStatesDef contains states for a connection status.
// Required states:
// - Start
type ConnectedStatesDef struct {
	// ErrConnecting is a detailed connection error, eg no access.
	ErrConnecting ConnectedState

	Connecting    ConnectedState
	Connected     ConnectedState
	Disconnecting ConnectedState
	Disconnected  ConnectedState

	*am.StatesBase
}

// ConnectedGroupsDef contains all the state groups of the Connected state
// schema.
type ConnectedGroupsDef struct {
	Connected S
}

// ConnectedStruct represents all relations and properties of ConnectedStates.
var ConnectedStruct = am.Schema{
	ssC.ErrConnecting: {Require: S{Exception}},

	ssC.Connecting: {
		Require: S{ssB.Start},
		Remove:  sgC.Connected,
	},
	ssC.Connected: {
		Require: S{ssB.Start},
		Remove:  sgC.Connected,
	},
	ssC.Disconnecting: {Remove: sgC.Connected},
	ssC.Disconnected: {
		Auto:   true,
		Remove: sgC.Connected,
	},
}

// EXPORTS AND GROUPS

var (
	ssC = am.NewStates(ConnectedStatesDef{})
	sgC = am.NewStateGroups(ConnectedGroupsDef{
		Connected: S{
			ssC.Connecting, ssC.Connected, ssC.Disconnecting,
			ssC.Disconnected,
		},
	})

	// ConnectedStates contains all the states for the Connected state schema.
	ConnectedStates = ssC

	// ConnectedGroups contains all the state groups for the Connected state
	// schema.
	ConnectedGroups = sgC
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
	mach.EvAddErrState(event, ssC.ErrConnecting, err, args)

	return err
}

// ///// ///// /////

// ///// CONNECTION POOL

// Connection to multiple hosts, states aggregating a pool of connections.
// ///// ///// /////

type ConnPoolState = string

// ConnPoolStatesDef contains states for a connection status.
// Required states:
// - Start
type ConnPoolStatesDef struct {
	// ErrConnecting is a detailed connection error, eg no access.
	ErrConnecting ConnPoolState

	Connecting     ConnPoolState
	Connected      ConnPoolState
	ConnectedFully ConnPoolState
	Disconnecting  ConnPoolState
	Disconnected   ConnPoolState

	*am.StatesBase
}

// ConnPoolGroupsDef contains all the state groups of the ConnPool state schema.
type ConnPoolGroupsDef struct {
	Connected S
}

// ConnPoolSchema represents all relations and properties of ConnPoolStates.
var ConnPoolSchema = am.Schema{
	ssPc.ErrConnecting: {Require: S{Exception}},

	ssPc.Disconnected: {
		Remove: S{ssPc.Connecting, ssPc.ConnectedFully, ssPc.Disconnecting},
	},
	ssPc.Connecting: {
		Require: S{ssB.Start},
		Remove:  S{ssPc.Disconnecting},
	},
	ssPc.Connected: {
		Require: S{ssB.Start},
		Remove:  S{ssPc.Disconnected},
	},
	ssPc.ConnectedFully: {
		Require: S{ssPc.Connected},
		Remove:  S{ssPc.Disconnected},
	},
	ssPc.Disconnecting: {
		Remove: S{ssPc.ConnectedFully, ssPc.Connected, ssPc.Connecting},
	},
}

// EXPORTS AND GROUPS

var (
	ssPc = am.NewStates(ConnPoolStatesDef{})
	sgCp = am.NewStateGroups(ConnPoolGroupsDef{
		Connected: S{ssPc.Connected, ssPc.Disconnected},
	})

	// ConnPoolStates contains all the states for the ConnPool state schema.
	ConnPoolStates = ssPc

	// ConnPoolGroups contains all the state groups for the ConnPool state schema.
	ConnPoolGroups = sgCp
)
