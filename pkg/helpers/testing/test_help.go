// Package testing provides testing helpers for state machines using testify.
package testing

import (
	"context"
	"os"
	stdtest "testing"
	"time"

	"github.com/stretchr/testify/assert"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

// MachDebug sets up a machine for debugging in tests, based on the AM_DEBUG
// env var, passed am-dbg address, log level and stdout flag.
func MachDebug(t *stdtest.T, mach am.Api, amDbgAddr string,
	logLvl am.LogLevel, stdout bool,
) {
	if stdout {
		mach.SetLoggerSimple(t.Logf, logLvl)
	} else if amDbgAddr == "" {
		mach.SetLoggerSimple(t.Logf, logLvl)

		return
	}

	amhelp.MachDebug(mach, amDbgAddr, logLvl, stdout)
}

// MachDebugEnv sets up a machine for debugging in tests, based on env vars
// only: AM_DBG_ADDR, AM_LOG, and AM_DEBUG.
func MachDebugEnv(t *stdtest.T, mach am.Api) {
	amDbgAddr := os.Getenv(telemetry.EnvAmDbgAddr)
	logLvl := am.EnvLogLevel("")
	stdout := os.Getenv(am.EnvAmDebug) == "2"

	MachDebug(t, mach, amDbgAddr, logLvl, stdout)
}

// Wait is a test version of [amhelp.Wait], which errors instead of returning
// false.
func Wait(
	t *stdtest.T, errMsg string, ctx context.Context, length time.Duration,
) {
	if !amhelp.Wait(ctx, length) {
		if t.Context().Err() == nil {
			t.Fatal("ctx expired")
		}
	}
}

// WaitForAll is a test version of [amhelp.WaitForAll], which errors instead of
// returning an error.
func WaitForAll(
	t *stdtest.T, source string, ctx context.Context, timeout time.Duration,
	chans ...<-chan struct{},
) {
	if err := amhelp.WaitForAll(ctx, timeout, chans...); err != nil {
		if t.Context().Err() == nil {
			t.Fatal("error for " + source + ": " + err.Error())
		}
	}
}

// WaitForErrAll is a test version of [amhelp.WaitForErrAll], which errors
// instead of returning an error.
func WaitForErrAll(
	t *stdtest.T, source string, ctx context.Context, mach am.Api,
	timeout time.Duration, chans ...<-chan struct{},
) {
	if err := amhelp.WaitForErrAll(ctx, timeout, mach, chans...); err != nil {
		if t.Context().Err() == nil {
			t.Fatal("error for " + source + ": " + err.Error())
		}
	}
}

// WaitForAny is a test version of [amhelp.WaitForAny], which errors instead of
// returning an error.
func WaitForAny(
	t *stdtest.T, source string, ctx context.Context, timeout time.Duration,
	chans ...<-chan struct{},
) {
	if err := amhelp.WaitForAny(ctx, timeout, chans...); err != nil {
		if t.Context().Err() == nil {
			t.Fatal("error for " + source + ": " + err.Error())
		}
	}
}

// GroupWhen1 is a test version of [amhelp.GroupWhen1], which errors instead of
// returning an error.
func GroupWhen1(
	t *stdtest.T, mach []am.Api, state string, ctx context.Context,
) []<-chan struct{} {
	chs, err := amhelp.GroupWhen1(mach, state, ctx)
	if err != nil {
		if t.Context().Err() == nil {
			t.Fatal(err)
		}
	}

	return chs
}

// AssertIs asserts that the machine is in the given states.
func AssertIs(t *stdtest.T, mach am.Api, states am.S) {
	assert.Subset(t, mach.ActiveStates(), states, "%s expected", states)
}

// AssertIs1 asserts that the machine is in the given state.
func AssertIs1(t *stdtest.T, mach am.Api, state string) {
	assert.Subset(t, mach.ActiveStates(), am.S{state}, "%s expected", state)
}

// AssertNot asserts that the machine is not in the given states.
func AssertNot(t *stdtest.T, mach am.Api, states am.S) {
	assert.NotSubset(t, mach.ActiveStates(), states, "%s not expected", states)
}

// AssertNot1 asserts that the machine is not in the given state.
func AssertNot1(t *stdtest.T, mach am.Api, state string) {
	assert.NotSubset(t, mach.ActiveStates(), am.S{state}, "%s not expected",
		state)
}

// AssertNoErrNow asserts that the machine is not in the Exception state.
func AssertNoErrNow(t *stdtest.T, mach am.Api) {
	if mach.IsErr() && t.Context().Err() == nil {
		err := mach.Err()
		if err != nil {
			t.Fatalf("Unexpected error in %s: %s", mach.Id(), err.Error())
		} else {
			t.Fatalf("Unexpected error in %s", mach.Id())
		}
	}
}

// AssertNoErrEver asserts that the machine never was in the Exception state.
func AssertNoErrEver(t *stdtest.T, mach am.Api) {
	if mach.Tick(am.Exception) > 0 && t.Context().Err() == nil {
		err := mach.Err()
		if err != nil {
			t.Fatalf("Unexpected error in %s", mach.Id())
		} else {
			t.Fatalf("Unexpected PAST error in %s", mach.Id())
		}
	}
}

// AssertErr asserts that the machine is in the Exception state.
func AssertErr(t *stdtest.T, mach am.Api) {
	if !mach.IsErr() && t.Context().Err() == nil {
		t.Fatal("expected " + am.Exception)
	}
}
