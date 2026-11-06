package states

import (
	"errors"
	"fmt"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ERRORS

// sentinel errors

var ErrConnecting = errors.New("error connecting")

// error mutations

// AddErrConnecting wraps an error in the ErrConnecting sentinel and adds to a
// machine.
func AddErrConnecting(
	event *am.Event, mach *am.Machine, err error, args am.A,
) am.Result {
	// TODO opt args (semver break)
	err = fmt.Errorf("%w: %w", ErrConnecting, err)
	return mach.EvAddErrState(event, ssC.ErrConnecting, err, args)
}
