package types

import (
	"context"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// MachineApi is a subset of `pkg/machine#Machine` for alternative
// implementations.
type MachineApi interface {
	// ///// REMOTE

	// Mutations (remote)

	Add1(state string, args am.A) am.Result
	Add(states am.S, args am.A) am.Result
	Remove1(state string, args am.A) am.Result
	Remove(states am.S, args am.A) am.Result
	Set(states am.S, args am.A) am.Result
	AddErr(err error, args am.A) am.Result
	AddErrState(state string, err error, args am.A) am.Result

	// Waiting (remote)

	WhenArgs(state string, args am.A, ctx context.Context) <-chan struct{}

	// Getters (remote)

	Err() error

	// Misc (remote)

	Log(msg string, args ...any)

	// ///// LOCAL

	// Checking (local)

	IsErr() bool
	Is(states am.S) bool
	Is1(state string) bool
	Not(states am.S) bool
	Not1(state string) bool
	Any(states ...am.S) bool
	Any1(state ...string) bool
	Has(states am.S) bool
	Has1(state string) bool
	IsTime(time am.Time, states am.S) bool
	IsClock(clock am.Clock) bool

	// Waiting (local)

	When(states am.S, ctx context.Context) <-chan struct{}
	When1(state string, ctx context.Context) <-chan struct{}
	WhenNot(states am.S, ctx context.Context) <-chan struct{}
	WhenNot1(state string, ctx context.Context) <-chan struct{}
	WhenTime(
		states am.S, times am.Time, ctx context.Context) <-chan struct{}
	WhenTicks(state string, ticks int, ctx context.Context) <-chan struct{}
	WhenTicksEq(state string, tick uint64, ctx context.Context) <-chan struct{}
	WhenErr(ctx context.Context) <-chan struct{}

	// Getters (local)

	StateNames() am.S
	ActiveStates() am.S
	Tick(state string) uint64
	Clock(states am.S) am.Clock
	Time(states am.S) am.Time
	TimeSum(states am.S) uint64
	NewStateCtx(state string) context.Context
	Export() *am.Serialized
	GetStruct() am.Struct

	// Misc (local)

	String() string
	StringAll() string
	Inspect(states am.S) string
	Index(state string) int
	Dispose()
	WhenDisposed() <-chan struct{}
}
