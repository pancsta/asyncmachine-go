# asyncmachine RPC

[-> go back to monorepo /](/README.md)

**aRPC** is a transparent RPC for state machines implemented using [asyncmachine-go](/). It's
clock-based and features many optimizations, e.g. having most of the API methods executed locally (as the state
information is encoded as clock values). It's build on top of [cenkalti/rpc2](https://github.com/cenkalti/rpc2) and
`net/rpc`. There's a [benchmark](/examples/benchmark_grpc/README.md) and [how-to](/pkg/rpc/HOWTO.md) available.

Implemented:

- mutation and wait methods
- payload file upload
- server getters

Not implemented (yet):

- `WhenArgs`, `Err()`
- client getters
- chunked encoding
- TLS
- reconnect
- compression
- multiplexing

## Usage

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    ssCli "github.com/pancsta/asyncmachine-go/pkg/rpc/states/client"
    ssSrv "github.com/pancsta/asyncmachine-go/pkg/rpc/states/server"
)
```

### Server

```go
// init
s, err := NewServer(ctx, addr, worker.ID, worker, nil)
if err != nil {
    panic(err)
}

// start
s.Start()
<-s.Mach.When1("RpcReady", nil)

// react to the client
<-worker.When1("Foo", nil)
print("Client added Foo")
worker.Add1("Bar", nil)
```

### Client

```go
// init
c, err := NewClient(ctx, addr, "clientid", ss.States, ss.Names)
if err != nil {
    panic(err)
}

// start
c.Start()
<-c.Mach.When1("Ready", nil)

// use the remote worker
c.Worker.Add1("Foo", nil)
<-c.Worker.When1("Bar", nil)
print("Server added Bar")
```

## Documentation

- [godoc /pkg/rpc](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc)
- [manual.md](/docs/manual.md)

## Benchmark: aRPC vs gRPC

A simple and opinionated benchmark showing a `subscribe-get-process` scenario, implemented in both gRPC and aRPC. See
[/examples/benchmark_grpc/README.md](/examples/benchmark_grpc/README.md) for details and source code.

![results - KiB transferred, number of calls](/assets/arpc-vs-grpc.png)

```text
> task benchmark-grpc
...
BenchmarkClientArpc
    client_arpc_test.go:136: Transferred: 609 bytes
    client_arpc_test.go:137: Calls: 4
    client_arpc_test.go:138: Errors: 0
    client_arpc_test.go:136: Transferred: 1,149,424 bytes
    client_arpc_test.go:137: Calls: 10,003
    client_arpc_test.go:138: Errors: 0
BenchmarkClientArpc-8              10000            248913 ns/op           28405 B/op        766 allocs/op
BenchmarkClientGrpc
    client_grpc_test.go:117: Transferred: 1,113 bytes
    client_grpc_test.go:118: Calls: 9
    client_grpc_test.go:119: Errors: 0
    client_grpc_test.go:117: Transferred: 3,400,812 bytes
    client_grpc_test.go:118: Calls: 30,006
    client_grpc_test.go:119: Errors: 0
BenchmarkClientGrpc-8              10000            262693 ns/op           19593 B/op        391 allocs/op
BenchmarkClientLocal
BenchmarkClientLocal-8             10000               434.4 ns/op            16 B/op          1 allocs/op
PASS
ok      github.com/pancsta/asyncmachine-go/examples/benchmark_grpc      5.187s
```

## API

**aRPC** implements `MachineApi`, which is a large subset of `Machine` methods. Below the full list, with distinction
which methods happen where (locally or on remote).

<details>

<summary>Expand MachineApi</summary>

```go
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
```

</details>

## Tests

**aRPC** passes the [whole test suite](/pkg/rpc/rpc_machine_test.go) of [`/pkg/machine`](/pkg/machine/machine_test.go)
for the exposed methods and provides a couple of [optimization-focused tests](/pkg/rpc/rpc_test.go), on top of tests for
basic RPC.

## Optimizations

**aRPC** implements several optimization strategies to achieve the results.

- binary format of `encoding/gob`
- index-based clock
  - `[0, 100, 0, 120]`
- diff-based clock updates
  - `[0, 1, 0, 1]`
- debounced server-mutation clock pushes
  - `[0, 5, 2, 1]`
- partial clock updates
  - `[[1, 1], [3, 1]]`

## monorepo

- [`/pkg/rpc/HOWTO.md`](/pkg/rpc/HOWTO.md)
- [`/examples/benchmark_grpc/README.md`](/examples/benchmark_grpc/README.md)

[Go back to the monorepo root](/README.md) to continue reading.
