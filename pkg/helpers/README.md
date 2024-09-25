# /pkg/helpers

[-> go back to monorepo /](/README.md)

> [!NOTE]
> **asyncmachine** can transform blocking APIs into controllable state machines with ease. It shares similarities with
> [Ergo's](https://github.com/ergo-services/ergo) actor model, and focuses on distributed workflows like [Temporal](https://github.com/temporalio/temporal).
> It's lightweight and most features are optional.

**/pkg/helpers** - because of the minimalistic approach of asyncmachine, helpers and sugars often end up in this package.

## Import

```go
import amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
```

## Synchronous Calls

Synchronous wrappers of async state machine calls, which assume a single, blocking scenario controlled with context.
[Multi states](/docs/manual.md#multi-states) are handled automatically.

- [`Add1Block(context.Context, types.MachineApi, string, am.A)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#Add1Block)
- [`Add1AsyncBlock(context.Context, types.MachineApi, string, string, am.A)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#Add1AsyncBlock)

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

## Debugging

- [`MachDebug(*am.Machine, string, am.LogLevel, bool)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#MachDebug)
- [`MachDebugt(*am.Machine, bool)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#MachDebugT)

**Example** - enable telemetry for [am-dbg](/tools/cmd/am-dbg/README.md)

```go
// read env
amDbgAddr := os.Getenv("AM_DBG_ADDR")
logLvl := am.EnvLogLevel("")

// debug
amhelp.MachDebug(mach, amDbgAddr, logLvl, true)
```

**Example** - enable telemetry for [am-dbg](/tools/cmd/am-dbg/README.md) using [conventional env vars](/config/env/README.md).

```go
// debug
amhelp.MachDebugEnv(mach, true)
```

## Documentation

- [godoc /pkg/helpers](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers)
- [manual.md](/docs/manual.md)

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
