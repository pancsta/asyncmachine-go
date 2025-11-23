# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/helpers

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

**/pkg/helpers** is the swiss army knife for async state machines. It encapsulates many low-level details of
[`/pkg/machine`](/pkg/machine/README.md) and provide easy to reason about functions. It specifically deals with
[machine time](/docs/manual.md#clock-and-context) and [multi states](/docs/manual.md#multi-states), which can be quite
verbose.

Highlights:

- [synchronous calls](#synchronous-calls)
- [debugging helpers](#debugging-helpers)
- [waiting helpers](#waiting-helpers)
- [failsafe Request object](#failsafe-request-object)
- [test helpers](#test-helpers)
- [miscellaneous utils](#miscellaneous-utils)

## Installation

```go
// prod
import amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
// tests
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

**Example** - enable dbg telemetry using [conventional env vars](/docs/env-configs.md).

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

## Complete API

```go
Package-Level Functions (total 56)

     func Activations(u uint64) int
     func Add1Async(ctx context.Context, mach am.Api, waitState string, addState string, args am.A) am.Result
     func Add1Block(ctx context.Context, mach am.Api, state string, args am.A) am.Result
     func Add1Sync(ctx context.Context, mach am.Api, state string, args am.A) am.Result
     func ArgsToArgs[T](src interface{}, dest T) T
     func ArgsToLogMap(args interface{}, maxLen int) map[string]string
     func AskAdd(mach am.Api, states am.S, args am.A) am.Result
     func AskAdd1(mach am.Api, state string, args am.A) am.Result
     func AskEvAdd(e *am.Event, mach am.Api, states am.S, args am.A) am.Result
     func AskEvAdd1(e *am.Event, mach am.Api, state string, args am.A) am.Result
     func AskEvRemove(e *am.Event, mach am.Api, states am.S, args am.A) am.Result
     func AskEvRemove1(e *am.Event, mach am.Api, state string, args am.A) am.Result
     func AskRemove(mach am.Api, states am.S, args am.A) am.Result
     func AskRemove1(mach am.Api, state string, args am.A) am.Result
     func CantAdd(mach am.Api, states am.S, args am.A) bool
     func CantAdd1(mach am.Api, state string, args am.A) bool
     func CantRemove(mach am.Api, states am.S, args am.A) bool
     func CantRemove1(mach am.Api, state string, args am.A) bool
     func CopySchema(source am.Schema, target *am.Machine, states am.S) error
     func CountRelations(state *am.State) int
     func EnableDebugging(stdout bool)
     func EvalGetter[T](ctx context.Context, source string, maxTries int, mach *am.Machine, eval func() (T, error)) (T, error)
     func ExecAndClose(fn func()) <-chan struct{}
     func GetTransitionStates(tx *am.Transition, index am.S) (added am.S, removed am.S, touched am.S)
     func GroupWhen1(machs []am.Api, state string, ctx context.Context) ([]<-chan struct{}, error)
     func Healthcheck(mach am.Api)
     func Implements(statesChecked, statesNeeded am.S) error
     func IndexesToStates(allStates am.S, indexes []int) am.S
     func Interval(ctx context.Context, length time.Duration, interval time.Duration, fn func() bool) error
     func IsDebug() bool
     func IsMulti(mach am.Api, state string) bool
     func IsTelemetry() bool
     func IsTestRunner() bool
     func MachDebug(mach am.Api, amDbgAddr string, logLvl am.LogLevel, stdout bool, semConfig *am.SemConfig)
     func MachDebugEnv(mach am.Api)
     func NewMirror(id string, flat bool, source *am.Machine, handlers any, states am.S) (*am.Machine, error)
     func NewMutRequest(mach am.Api, mutType am.MutationType, states am.S, args am.A) *MutRequest
     func NewReqAdd(mach am.Api, states am.S, args am.A) *MutRequest
     func NewReqAdd1(mach am.Api, state string, args am.A) *MutRequest
     func NewReqRemove(mach am.Api, states am.S, args am.A) *MutRequest
     func NewReqRemove1(mach am.Api, state string, args am.A) *MutRequest
     func NewStateLoop(mach *am.Machine, loopState string, optCheck func() bool) *StateLoop
     func Pool(limit int) *errgroup.Group
     func PrefixStates(schema am.Schema, prefix string, removeDups bool, optWhitelist, optBlacklist S) am.Schema
     func RemoveMulti(mach am.Api, state string) am.HandlerFinal
     func ResultToErr(result am.Result) error
     func SchemaHash(schema am.Schema) string
     func SemConfig(forceFull bool) *am.SemConfig
     func SetEnvLogLevel(level am.LogLevel)
     func StatesToIndexes(allStates am.S, states am.S) []int
     func TagValue(tags []string, key string) string
     func Wait(ctx context.Context, length time.Duration) bool
     func WaitForAll(ctx context.Context, timeout time.Duration, chans ...<-chan struct{}) error
     func WaitForAny(ctx context.Context, timeout time.Duration, chans ...<-chan struct{}) error
     func WaitForErrAll(ctx context.Context, timeout time.Duration, mach am.Api, chans ...<-chan struct{}) error
     func WaitForErrAny(ctx context.Context, timeout time.Duration, mach *am.Machine, chans ...<-chan struct{}) error

Package-Level Variables (only one)

      var SlogToMachLogOpts *slog.HandlerOptions

Package-Level Constants (total 11)

    const EnvAmHealthcheck = "AM_HEALTHCHECK"
    const EnvAmLogArgs = "AM_LOG_ARGS"
    const EnvAmLogChecks = "AM_LOG_CHECKS"
    const EnvAmLogFile = "AM_LOG_FILE"
    const EnvAmLogFull = "AM_LOG_FULL"
    const EnvAmLogGraph = "AM_LOG_GRAPH"
    const EnvAmLogQueued = "AM_LOG_QUEUED"
    const EnvAmLogStateCtx = "AM_LOG_STATE_CTX"
    const EnvAmLogSteps = "AM_LOG_STEPS"
    const EnvAmLogWhen = "AM_LOG_WHEN"
    const EnvAmTestRunner = "AM_TEST_RUNNER"
```

## Documentation

- [api /pkg/helpers](https://code.asyncmachine.dev/pkg/github.com/pancsta/asyncmachine-go/pkg/helpers.html)
- [godoc /pkg/helpers](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers)

## Status

Testing, not semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
