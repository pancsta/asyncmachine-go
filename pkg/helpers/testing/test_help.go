// Package testing provides testing helpers for state machines using testify.
package testing

import (
	"context"
	"net"
	"os"
	"strings"
	"sync/atomic"
	stdtest "testing"
	"time"

	"github.com/lithammer/dedent"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// MachDebug sets up a machine for debugging in tests, based on the AM_DEBUG
// env var, passed am-dbg address, log level and stdout flag.
func MachDebug(t *stdtest.T, mach am.Api, amDbgAddr string,
	logLvl am.LogLevel, stdout bool,
) {
	if stdout {
		mach.SemLogger().SetSimple(t.Logf, logLvl)
	} else if amDbgAddr == "" {
		mach.SemLogger().SetSimple(t.Logf, logLvl)

		return
	}

	// expand the default addr
	if amDbgAddr == "1" {
		amDbgAddr = dbg.DbgAddr
	}

	err := amhelp.MachDebug(mach, amDbgAddr, logLvl, stdout,
		amhelp.SemConfigEnv(true))
	require.NoError(t, err)
}

// MachDebugEnv sets up a machine for debugging in tests, based on env vars
// only: AM_DBG_ADDR, AM_LOG, and AM_DEBUG.
func MachDebugEnv(t *stdtest.T, mach am.Api) {
	amDbgAddr := os.Getenv(dbg.EnvAmDbgAddr)
	logLvl := am.EnvLogLevel("")
	stdout := os.Getenv(amhelp.EnvAmLogPrint) != ""

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

// MeteredUDP wraps a net.PacketConn to track bytes and packet counts.
type MeteredUDP struct {
	net.PacketConn
	bytesIn    atomic.Uint64
	bytesOut   atomic.Uint64
	packetsIn  atomic.Uint64
	packetsOut atomic.Uint64
}

func WrapUDP(conn net.PacketConn) *MeteredUDP {
	return &MeteredUDP{PacketConn: conn}
}

func (m *MeteredUDP) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := m.PacketConn.ReadFrom(p)
	if n > 0 {
		m.bytesIn.Add(uint64(n))
		m.packetsIn.Add(1)
	}
	return n, addr, err
}

func (m *MeteredUDP) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := m.PacketConn.WriteTo(p, addr)
	if n > 0 {
		m.bytesOut.Add(uint64(n))
		m.packetsOut.Add(1)
	}
	return n, err
}

// Stats getters
func (m *MeteredUDP) BytesIn() uint64    { return m.bytesIn.Load() }
func (m *MeteredUDP) BytesOut() uint64   { return m.bytesOut.Load() }
func (m *MeteredUDP) PacketsIn() uint64  { return m.packetsIn.Load() }
func (m *MeteredUDP) PacketsOut() uint64 { return m.packetsOut.Load() }

// AssertIs asserts that the machine is in the given states.
func AssertIs(t *stdtest.T, mach am.Api, states am.S, msgAndArgs ...any) {
	if len(msgAndArgs) == 0 {
		msgAndArgs = []any{"%s expected"}
	}
	assert.Subset(t, mach.ActiveStates(nil), states, msgAndArgs...)
}

// AssertIs1 asserts that the machine is in the given state.
func AssertIs1(t *stdtest.T, mach am.Api, state string, msgAndArgs ...any) {
	if len(msgAndArgs) == 0 {
		msgAndArgs = []any{"%s expected"}
	}
	assert.Subset(t, mach.ActiveStates(nil), am.S{state}, msgAndArgs...)
}

// AssertNot asserts that the machine is not in the given states.
func AssertNot(t *stdtest.T, mach am.Api, states am.S, msgAndArgs ...any) {
	if len(msgAndArgs) == 0 {
		msgAndArgs = []any{"%s not expected"}
	}
	assert.NotSubset(t, mach.ActiveStates(nil), states, msgAndArgs...)
}

// AssertNot1 asserts that the machine is not in the given state.
func AssertNot1(t *stdtest.T, mach am.Api, state string, msgAndArgs ...any) {
	if len(msgAndArgs) == 0 {
		msgAndArgs = []any{"%s not expected"}
	}
	assert.NotSubset(t, mach.ActiveStates(nil), am.S{state}, msgAndArgs...)
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
	if mach.Tick(am.StateException) > 0 && t.Context().Err() == nil {
		err := mach.Err()
		if err != nil {
			t.Fatalf("Unexpected error in %s", mach.Id())
		} else {
			t.Fatalf("Unexpected PAST error in %s", mach.Id())
		}
	}
	machQueue, ok := mach.(*am.Machine)
	if ok {
		AssertNoErrQueued(t, machQueue)
	}
}

func AssertNoErrQueued(t *stdtest.T, mach *am.Machine) {
	if mach.WillBe1(am.StateException) && t.Context().Err() == nil {
		t.Fatalf("Unexpected queued error in %s", mach.Id())
	}
}

// AssertErr asserts that the machine is in the Exception state.
func AssertErr(t *stdtest.T, mach am.Api) {
	if !mach.IsErr() && t.Context().Err() == nil {
		t.Fatal("expected " + am.StateException)
	}
}

// LogToTestLog will fwd machine's log to `go test` log.
func LogToTestLog(t *stdtest.T, mach am.Api, maxLvl am.LogLevel) {
	logOld := mach.SemLogger().Logger()
	mach.SemLogger().SetLogger(func(level am.LogLevel, msg string, args ...any) {
		if level <= maxLvl {
			t.Logf(msg, args...)
		}
		if logOld != nil {
			logOld(level, msg, args...)
		}
	})
}

func AssertTime(t *stdtest.T, m am.Api, states am.S, time am.Time,
	msgAndArgs ...any,
) {
	assert.Subset(t, m.Time(states), time, msgAndArgs...)
}

func AssertString(
	t *stdtest.T, m am.Api, expected string, states am.S,
) {
	assert.Equal(t,
		strings.Trim(dedent.Dedent(expected), "\n"),
		strings.Trim(m.Inspect(states), "\n"))
}
