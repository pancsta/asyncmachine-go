// Package helpers is a set of useful functions when working with state
// machines.
package helpers

import (
	"context"
	"os"
	"slices"
	"testing"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/pkg/types"
)

// Add1Block activates a state and waits until it becomes active. If it's a
// multi state, it also waits for it te de-activate. Returns early if a
// non-multi state is already active. Useful to avoid the queue.
func Add1Block(
	ctx context.Context, mach types.MachineApi, state string, args am.A,
) am.Result {
	// TODO support args["sync_token"] via WhenArgs

	// support for multi states

	if IsMulti(mach, state) {
		when := mach.WhenTicks(state, 2, ctx)
		res := mach.Add1(state, args)
		<-when

		return res
	}

	if mach.Is1(state) {
		return am.ResultNoOp
	}

	ctxWhen, cancel := context.WithCancel(ctx)
	defer cancel()

	when := mach.WhenTicks(state, 1, ctxWhen)
	res := mach.Add1(state, args)
	if res == am.Canceled {
		// dispose "when" ch early
		cancel()

		return res
	}
	<-when

	return res
}

// Add1AsyncBlock adds a state from an async op and waits for another one
// from the op to become active. Theoretically, it should work with any state
// pair, including Multi states.
func Add1AsyncBlock(
	ctx context.Context, mach types.MachineApi, waitState string,
	addState string, args am.A,
) am.Result {
	ticks := 1
	// wait 2 ticks for multi states (assuming they remove themselves)
	if IsMulti(mach, waitState) {
		ticks = 2
	}

	ctxWhen, cancel := context.WithCancel(ctx)
	defer cancel()

	when := mach.WhenTicks(waitState, ticks, ctxWhen)
	res := mach.Add1(addState, args)
	if res == am.Canceled {
		// dispose "when" ch early
		cancel()

		return res
	}
	<-when

	return res
}

// TODO AddSync, Add1AsyncBlock

// IsMulti returns true if a state is a multi state.
func IsMulti(mach types.MachineApi, state string) bool {
	return mach.GetStruct()[state].Multi
}

// StatesToIndexes converts a list of state names to a list of state indexes,
// for a given machine.
func StatesToIndexes(mach types.MachineApi, states am.S) []int {
	var indexes []int
	for _, state := range states {
		indexes = append(indexes, slices.Index(mach.StateNames(), state))
	}

	return indexes
}

// IndexesToStates converts a list of state indexes to a list of state names,
// for a given machine.
func IndexesToStates(mach types.MachineApi, indexes []int) am.S {
	states := am.S{}
	for _, index := range indexes {
		states = append(states, mach.StateNames()[index])
	}

	return states
}

// MachDebugT sets up a machine for debugging in tests, based on the AM_DEBUG
// env var, passed am-dbg address, log level and stdout flag.
func MachDebugT(t *testing.T, mach *am.Machine, amDbgAddr string,
	logLvl am.LogLevel, stdout bool,
) {
	if os.Getenv("AM_DEBUG") == "" {
		return
	}

	if stdout {
		mach.SetLoggerSimple(t.Logf, logLvl)
	} else if amDbgAddr == "" {
		mach.SetLoggerSimple(t.Logf, logLvl)

		return
	}

	MachDebug(mach, amDbgAddr, logLvl, stdout)
}

// MachDebug sets up a machine for debugging, based on the AM_DEBUG env var,
// passed am-dbg address, log level and stdout flag.
func MachDebug(mach *am.Machine, amDbgAddr string, logLvl am.LogLevel,
	stdout bool,
) {
	if amDbgAddr == "" {
		return
	}

	if stdout {
		mach.SetLogLevel(logLvl)
	} else {
		mach.SetLoggerEmpty(logLvl)
	}

	// trace to telemetry
	err := telemetry.TransitionsToDbg(mach, amDbgAddr)
	if err != nil {
		panic(err)
	}
}

// MachDebugEnv sets up a machine for debugging, based on env vars only.
func MachDebugEnv(mach *am.Machine, stdout bool) {
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")
	MachDebug(mach, amDbgAddr, logLvl, stdout)
}
