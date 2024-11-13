[![go report](https://goreportcard.com/badge/github.com/pancsta/asyncmachine-go)](https://goreportcard.com/report/github.com/pancsta/asyncmachine-go)
[![coverage](https://codecov.io/gh/pancsta/asyncmachine-go/graph/badge.svg?token=B8553BI98P)](https://codecov.io/gh/pancsta/asyncmachine-go)
[![go reference](https://pkg.go.dev/badge/github.com/pancsta/asyncmachine-go.svg)](https://pkg.go.dev/github.com/pancsta/asyncmachine-go)
[![last commit](https://img.shields.io/github/last-commit/pancsta/asyncmachine-go/main)](https://github.com/pancsta/asyncmachine-go/commits/main/)
![release](https://img.shields.io/github/v/release/pancsta/asyncmachine-go)
[![matrix chat](https://matrix.to/img/matrix-badge.svg)](https://matrix.to/#/#room:asyncmachine)

# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/helpers

[cd /](/README.md)

> [!NOTE]
> **Asyncmachine-go** is an AOP Actor Model library for distributed workflows, built on top of a lightweight state
> machine (nondeterministic, multi-state, clock-based, relational, optionally-accepting, and non-blocking). It has
> atomic transitions, RPC, logging, TUI debugger, metrics, tracing, and soon diagrams.

**/pkg/helpers** is the swiss army knife for async state machines. It encapsulates many low-level approaches from
`/pkg/machine` and provide easy to reason about functions. It specifically deals with [machine time](/docs/manual.md#clock-and-context)
and [multi states](/docs/manual.md#multi-states), which can be quite verbose.

Highlights:

- [synchronous calls](#synchronous-calls)
- [waiting helpers](#waiting-helpers)
- [failsafe Request object](#failsafe-request-object)
- [test helpers](#test-helpers)
- [debugging helpers](#debugging-helpers)
- [misc utils](#misc-utils)

## Import

```go
import amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
// for tests
import amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
```

## Synchronous Calls

Synchronous wrappers of async state machine calls, which assume a single, blocking scenario controlled with context.
[Multi states](/docs/manual.md#multi-states) are handled automatically.

- Add1Block
- Add1AsyncBlock

**Example** - add state `StateNameSelected` and wait until it becomes active

```go
res := amhelp.Add1Block(ctx, mach, ss.StateNameSelected, am.A{"state": state})
print(mach.Is1(ss.StateNameSelected)) // true
print(res) // am.Executed or am.Canceled, never am.Queued
```

**Example** - wait for `ScrollToTx`, triggered by `ScrollToMutTx`

```go
res := amhelp.Add1AsyncBlock(ctx, mach, ss.ScrollToTx, ss.ScrollToMutTx, am.A{
    "state": state,
    "fwd":   true,
})

print(mach.Is1(ss.ScrollToTx)) // true
print(res) // am.Executed or am.Canceled, never am.Queued
```

## Debugging Helpers

Activate debugging in 1 line - either for a single machine, or for the whole process.

- EnableDebugging
- SetLogLevel
- MachDebugEnv
- MachDebug
- Healthcheck
- IsDebug

**Example** - enable dbg telemetry using [conventional env vars](/config/env/README.md).

```go
// set env vars
amhelp.EnableDebugging(true)
// debug
amhelp.MachDebugEnv(mach)
```

## Waiting Helpers

Waiting helpers mostly provide wrappers for [when methods](/docs/manual.md#waiting) in `select` statements with a
[state context](/docs/manual.md#clock-and-context), and timeout context. State context is checked before, and timeout
transformed into `am.ErrTimeout`. It reduces state context checking, if used as a first call after a blocking call /
start (which is a common case).

- WaitForAll
- WaitForErrAll
- WaitForAny
- WaitForErrAny
- GroupWhen1
- Wait
- ExecAndClose

**Example** - wait for bootstrap RPC to become ready.

```go
//
err := amhelp.WaitForAll(ctx, s.ConnTimeout,
    boot.server.Mach.When1(ssrpc.ServerStates.RpcReady, nil))
if ctx.Err() != nil {
    return // expired
}
if err != nil {
    AddErrWorker(s.Mach, err, Pass(argsOut))
    return
}
```

**Example** - wait for all clients to be ready.

```go
var clients []*node.Client

amhelp.WaitForAll(t, ctx, 2*time.Second,
    amhelp.GroupWhen1(clients, ssC.Ready, nil)...)
```

**Example** - wait 1s with a state context.

```go
if !amhelp.Wait(ctx, 1*time.Second) {
    return // expired
}
// wait ok
```

**Example** - wait for Foo, Bar, or Exception.

```go
ctx := client.Mach.NewStateCtx("Start")
err := amhelp.WaitForErrAll(ctx, 1*time.Second, mach,
        mach.When1("Foo", nil),
        mach.When1("Bar", nil))
if ctx.Err() != nil {
    // no err
    return nil // expired
}
if err != nil {
    // either timeout happened or Exception has been activated
    return err
}
```

## Failsafe Request Object

Failsafe mutation requests use [failsafe-go](https://github.com/failsafe-go/failsafe-go) policies when mutating a state
machine. Useful for network communication, or when waiting on a state isn't an option, but also helps with overloaded
machines and populated queues. Policies can be customized and ``MutRequest`` objects cloned.

- NewReqAdd
- NewReqAdd1
- NewReqRemove
- NewReqRemove1
- NewMutRequest

**Example** - try to add state `WorkerRequested` with 10 retries, 100ms delay, 5s backoff, and 5s max duration.

```go
// failsafe worker request
_, err := amhelp.NewReqAdd1(c.Mach, ssC.WorkerRequested, nil).Run(ctx)
if err != nil {
    return err
}
```

## Test Helpers

Test helpers mostly provide assertions for [stretchr/testify](https://github.com/stretchr/testify), but also wrappers
for regular helpers which go `t.Fatal` on errors (to save some lines).

- AssertIs
- AssertIs1
- AssertNot
- AssertNot1
- AssertNoErrNow
- AssertNoErrEver
- AssertErr
- MachDebug
- MachDebugEnv
- Wait
- WaitForAll
- WaitForAny
- WaitForErrAll
- WaitForErrAny
- GroupWhen1

**Example** - assert `mach` has `ClientConnected` active.

```go
amhelpt.AssertIs1(t, mach, "ClientConnected")
```

**Example** - assert `mach` never had an error (not just currently).

```go
amhelpt.AssertNoErrEver(t, mach)
```

## Misc utils

- Implements
- Activations
- IsMulti
- RemoveMulti
- Toggle
- StatesToIndexes
- IndexesToStates
- GetTransitionStates
- ArgsToLogMap
- ArgsToArgs

## Documentation

- [godoc /pkg/helpers](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers)

## Status

Testing, not semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
