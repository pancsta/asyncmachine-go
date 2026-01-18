# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/rpc

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

**aRPC** is a transparent RPC for state machines implemented using [asyncmachine-go](/). It's
clock-based and features many optimizations, e.g. having most of the API methods executed locally (as state changes are
regularly pushed to the client). It's built on top of [cenkalti/rpc2](https://github.com/cenkalti/rpc2), `net/rpc`,
and [soheilhy/cmux](https://github.com/soheilhy/cmux). Check out a [dedicated example](/examples/arpc/), [gRPC benchmark](/examples/benchmark_grpc/README.md),
and an [integration tests tutorial](/pkg/rpc/HOWTO.md).

## Features

- mutation methods
- wait methods
- clock pushes (from source mutations)
- remote contexts
- multiplexing
- reconnect / fail-safety
- network machine sending payloads to the client
- [REPL](/tools/cmd/arpc/README.md)
- queue ticks support
- initial optimizations

Not implemented (yet):

- `WhenArgs`, `Err()`
- `PushAllTicks`
- chunked payloads
- TLS
- compression
- msgpack encoding

Each RPC server can handle 1 RPC client at a time, but 1 state source (asyncmachine) can have many RPC servers attached
to itself (via [Tracer API](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Tracer)).
Additionally, remote RPC network-machines can also have RPC servers attached to themselves, creating a tree structure
(see [/examples/benchmark_state_source](/examples/benchmark_state_source/README.md)).

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc.d2.dark.svg">
        <img style="min-height: 135px"
            src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc.d2.dark.svg"
            alt="diagram" />
    </a>
</div>

## Components

### Network Machine

Any state machine can be exposed as an RPC network machine, as long as it implements [`/pkg/rpc/states/Network MachineStructDef`](/pkg/rpc/states/ss_rpc.go).
This can be done either manually, or by using state helpers ([SchemaMerge](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.TimeSum),
[SAdd](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#SAdd)), or by generating a schema file with
[am-gen](/tools/cmd/am-gen/README.md). It's also required to have the states verified by [Machine.VerifyStates](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.VerifyStates).
Network Machine can send data to the client via the `SendPayload` state.

- [schema file](/pkg/rpc/states/ss_rpc.go)

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// ...

// inherit from RPC network machine
schema := am.SchemaMerge(ssrpc.Network MachineStruct, am.Schema{
    "Foo": {Require: am.S{"Bar"}},
    "Bar": {},
})
name := am.SAdd(ssrpc.Network MachineStates.Names(), am.S{"Foo", "Bar"})

// init
network machine := am.New(ctx, schema, nil)
netMach.VerifyStates(name)

// ...

// send data to the client
netMach.Add1(ssrpc.Network MachineStates.SendPayload, arpc.Pass(&arpc.A{
    Name: "mypayload",
    Payload: &arpc.ArgsPayload{
        Name: "mypayload",
        Source: "network machine1",
        Data: []byte{1,2,3},
    },
}))
```

#### Network Machine Schema

State schema from [/pkg/rpc/states/ss_rpc.go](/pkg/rpc/states/ss_rpc.go).

```go
type NetMachStatesDef struct {
    ErrProviding string
    ErrSendPayload string
    SendPayload string
}
```

### Server

Each RPC server can handle 1 client at a time. Both client and server need the same network machine states definition (structure
map and ordered list of states). After the initial handshake, server will be pushing local state changes every [PushInterval](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Server),
while state changes made by an RPC client are delivered synchronously. Server starts listening on either
[Addr](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Server), [Listener](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Server),
or [Conn](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Server). Basic ACL is possible via [AllowId](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Server).

- [schema file](/pkg/rpc/states/ss_rpc.go)

```go
import (
    amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// ...

var addr string
var netMach *am.Machine

// init
s, err := arpc.NewServer(ctx, addr, netMach.ID, network machine, nil)
if err != nil {
    panic(err)
}

// start
s.Start()
err = amhelp.WaitForAll(ctx, 2*time.Second,
    s.Mach.When1(ssrpc.ServerStates.RpcReady, ctx))
if ctx.Err() != nil {
    return
}
if err != nil {
    return err
}

// react to the client
<-netMach.When1("Foo", nil)
print("Client added Foo")
netMach.Add1("Bar", nil)
```

#### Server Schema

State schema from [/pkg/rpc/states/ss_rpc.go](/pkg/rpc/states/ss_rpc.go).

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/schemas/rpc-server.svg?raw=true">
        <img style="min-height: 293px"
            src="https://pancsta.github.io/assets/asyncmachine-go/schemas/rpc-server.svg?raw=true"
            alt="server schema" />
    </a>
</div>

### Client

Each RPC client can connect to 1 server and needs to know network machine's machine schema and order. Data send by a
network machine via `SendPayload` will be received by a [Consumer machine](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc/states#ConsumerStatesDef)
(passed via [ClientOpts.Consumer](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#ClientOpts)) as an Add
mutation of the `Network MachinePayload` state (see a [detailed diagram](/docs/diagrams.md#rpc-getter-flow)). Client supports
fail-safety for both connection (eg [ConnRetries](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Client),
[ConnRetryBackoff](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Client)) and calls (eg [CallRetries](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Client),
[CallRetryBackoff](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Client)).

After the client's `Ready` state becomes active, it exposes a remote network machine at `client.Network Machine`. Remote
network machine implements most of
[Machine](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine)'s methods, many of which
are evaluated locally (like [Is](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#NetMach.Is), [When](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#NetMach.When),
[NewStateCtx](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#NetMach.NewStateCtx)). See [machine.Api](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Api)
for a full list.

- [schema file](/pkg/rpc/states/ss_rpc.go)

```go
import (
    amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// ...

var addr string
// network machine state schema
var schema am.Schema
// network machine state names
var names am.S

// consumer
consumer := am.New(ctx, ssrpc.ConsumerSchema, nil)

// init
c, err := arpc.NewClient(ctx, addr, "clientid", schema, &arpc.ClientOpts{
    Consumer: consumer,
})

// start
c.Start()
err := amhelp.WaitForAll(ctx, 2*time.Second,
    c.Mach.When1(ssrpc.ClientStates.Ready, ctx))
if ctx.Err() != nil {
    return
}

// use the remote network machine
c.NetMach.Add1("Foo", nil)
<-c.NetMach.When1("Bar", nil)
print("Server added Bar")
```

#### Client Schema

State schema from [/pkg/rpc/states/ss_rpc.go](/pkg/rpc/states/ss_rpc.go).

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/schemas/rpc-client.svg?raw=true">
        <img style="min-height: 167px"
            src="https://pancsta.github.io/assets/asyncmachine-go/schemas/rpc-client.svg?raw=true"
            alt="client schema" />
    </a>
</div>

### Multiplexer

Because 1 server can serve only 1 client (for simplicity), it's often required to use a port multiplexer. It's very
simple to create one using [NewMux](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#NewMux) and a callback
function, which returns a new server instance.

- [schema file](/pkg/rpc/states/ss_rpc.go)

```go
import (
    amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// ...

var ctx context.Context

// new server per each new client (optional)
var newServer arpc.MuxNewServer = func(num int64, _ net.Conn) (*Server, error) {
    name := fmt.Sprintf("%s-%d", t.Name(), num)
    s, err := NewServer(ctx, "", name, w, nil)
    if err != nil {
        t.Fatal(err)
    }

    return s, nil
}

// start cmux
mux, err := arpc.NewMux(ctx, t.Name(), newServer, nil)
mux.Listener = listener // or mux.Addr := ":1234"
mux.Start()
err := amhelp.WaitForAll(ctx, 2*time.Second,
    mux.Mach.When1(ssrpc.MuxStates.Ready, ctx))
```

#### Multiplexer Schema

State schema from [/pkg/rpc/states/ss_rpc.go](/pkg/rpc/states/ss_rpc.go).

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/schemas/rpc-mux.svg?raw=true">
        <img style="min-height: 241px"
            src="https://pancsta.github.io/assets/asyncmachine-go/schemas/rpc-mux.svg?raw=true"
            alt="multiplexer schema" />
    </a>
</div>

## Selective Distribution

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc-dist-mach.d2.dark.svg)

Every state-machine can be partially distributed over the network and updated on a different granularity level.

Synchronization scenarios:

- full sync (state names, schema, machine time)
- no schema (state names, machine time)
- selected states (state names, schema, selective ticks)
- shallow clocks (state names, schema, binary ticks)

Synchronization granularity:

- 1 clock diff per N mutations
- N clock diffs per N mutations

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc-sync-details.d2.dark.svg)

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc-sync-clocks.d2.dark.svg)

Scenarios can be mixed with each other to a certain degree (eg shallow clocks for selected states). **Selective
Distribution** is like [state piping](/pkg/states/README.md#piping), but over the network and utilizes all other aRPC
optimizations.

## Network Handlers

Locally piped Network Machine can then run local handlers (via a local [`/pkg/machine.Machine`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine)
instance) and mutate itself, which will effectively mutate the Network Source. Combining network handlers with selective
distribution can lead to large network coverage (number of hosts) consuming a tiny bandwidth usage. The structure of the
network has to be fixed, unlike in case of [`/pkg/pubsub`](/pkg/pubsub/README.md).

By combining **Network Handlers** with **Selective Distribution** received by a redundant number of clients (per state),
we can create multidimensional graphs. Those could be developed further with "voting receivers" (vote-based firewalls)
to create more organic systems.

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc-handlers.d2.dark.svg)

## Implementation details

- schema is always full-or-nothing
- state indexes and time slices are always source-bound
  - unless there's no schema sync, then client-bound
- time sum is always client-bound for checksums
  - from a binary time slice for shallow clocks
- aRPC mutations don't have arguments - hash `am.Time` and machine IDs to create a remote address for payloads

**aRPC** implements several optimization strategies to achieve the results:

- `net/rpc` method names as runes
- binary format of `encoding/gob`
- index-based clock
  - `[0, 100, 0, 120]`
- diff-based clock updates
  - `[0, 1, 0, 1]`
- debounced server-mutation clock pushes
  - `[0, 5, 2, 1]`
- partial clock updates
  - `[[1, 1], [3, 1]]`

## Documentation

- [api /pkg/rpc](https://code.asyncmachine.dev/pkg/github.com/pancsta/asyncmachine-go/pkg/rpc.html)
- [godoc /pkg/rpc](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc)
- [Example - Setup](/examples/arpc)
- [Example - Tree State Source](/examples/tree_state_source/README.md)
- [diagrams](/docs/diagrams.md#arpc)

## Benchmark: aRPC vs gRPC

A simple and opinionated benchmark showing a `subscribe-get-process` scenario, implemented in both gRPC and aRPC. See
[/examples/benchmark_grpc](/examples/benchmark_grpc/README.md) for details and source code.

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/arpc-vs-grpc.png?raw=true">
        <img style="min-height: 319px"
            src="https://pancsta.github.io/assets/asyncmachine-go/arpc-vs-grpc.png?raw=true"
            alt="results - KiB transferred, number of calls" />
    </a>
</div>

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

**aRPC** implements `/pkg/machine#Api`, which is a large subset of `/pkg/machine#Machine` methods. Below the full list,
with distinction which methods happen where (locally or on remote).

```go
// TODO update
// A (arguments) is a map of named arguments for a Mutation.
type A map[string]any
// S (state names) is a string list of state names.
type S []string
type Time []uint64
type Clock map[string]uint64
type Result int
type Schema = map[string]State

// Api is a subset of Machine for alternative implementations.
type Api interface {
    // ///// REMOTE

    // Mutations (remote)

    Add1(state string, args A) Result
    Add(states S, args A) Result
    Remove1(state string, args A) Result
    Remove(states S, args A) Result
    AddErr(err error, args A) Result
    AddErrState(state string, err error, args A) Result
    Toggle(states S, args A) Result
    Toggle1(state string, args A) Result
    Set(states S, args A) Result

    // Traced mutations (remote)

    EvAdd1(event *Event, state string, args A) Result
    EvAdd(event *Event, states S, args A) Result
    EvRemove1(event *Event, state string, args A) Result
    EvRemove(event *Event, states S, args A) Result
    EvAddErr(event *Event, err error, args A) Result
    EvAddErrState(event *Event, state string, err error, args A) Result
    EvToggle(event *Event, states S, args A) Result
    EvToggle1(event *Event, state string, args A) Result

    // Waiting (remote)

    WhenArgs(state string, args A, ctx context.Context) <-chan struct{}

    // Getters (remote)

    Err() error

    // ///// LOCAL

    // Checking (local)

    IsErr() bool
    Is(states S) bool
    Is1(state string) bool
    Any(states ...S) bool
    Any1(state ...string) bool
    Not(states S) bool
    Not1(state string) bool
    IsTime(time Time, states S) bool
    WasTime(time Time, states S) bool
    IsClock(clock Clock) bool
    WasClock(clock Clock) bool
    Has(states S) bool
    Has1(state string) bool
    CanAdd(states S, args A) Result
    CanAdd1(state string, args A) Result
    CanRemove(states S, args A) Result
    CanRemove1(state string, args A) Result
    CountActive(states S) int

    // Waiting (local)

    When(states S, ctx context.Context) <-chan struct{}
    When1(state string, ctx context.Context) <-chan struct{}
    WhenNot(states S, ctx context.Context) <-chan struct{}
    WhenNot1(state string, ctx context.Context) <-chan struct{}
    WhenTime(states S, times Time, ctx context.Context) <-chan struct{}
    WhenTime1(state string, tick uint64, ctx context.Context) <-chan struct{}
    WhenTicks(state string, ticks int, ctx context.Context) <-chan struct{}
    WhenQuery(query func(clock Clock) bool, ctx context.Context) <-chan struct{}
    WhenErr(ctx context.Context) <-chan struct{}
    WhenQueue(tick Result) <-chan struct{}

    // Getters (local)

    StateNames() S
    StateNamesMatch(re *regexp.Regexp) S
    ActiveStates() S
    Tick(state string) uint64
    Clock(states S) Clock
    Time(states S) Time
    TimeSum(states S) uint64
    QueueTick() uint64
    NewStateCtx(state string) context.Context
    Export() *Serialized
    Schema() Schema
    Switch(groups ...S) string
    Groups() (map[string][]int, []string)
    Index(states S) []int
    Index1(state string) int

    // Misc (local)

    Id() string
    ParentId() string
    Tags() []string
    Ctx() context.Context
    String() string
    StringAll() string
    Log(msg string, args ...any)
    SemLogger() SemLogger
    Inspect(states S) string
    BindHandlers(handlers any) error
    DetachHandlers(handlers any) error
    HasHandlers() bool
    StatesVerified() bool
    Tracers() []Tracer
    DetachTracer(tracer Tracer) error
    BindTracer(tracer Tracer) error
    AddBreakpoint1(added string, removed string, strict bool)
    AddBreakpoint(added S, removed S, strict bool)
    Dispose()
    WhenDisposed() <-chan struct{}
    IsDisposed() bool
}
```

## Tests

**aRPC** passes the [whole test suite](/pkg/rpc/rpc_machine_test.go) of [`/pkg/machine`](/pkg/machine/machine_test.go)
for the exposed methods and provides a couple of [optimization-focused tests](/pkg/rpc/rpc_test.go) (on top of tests for
basic RPC).

## Status

Testing, not semantically versioned.

## monorepo

- [`/examples/arpc`](/examples/arpc)
- [`/examples/tree_state_source`](/examples/tree_state_source/README.md)
- [`/pkg/rpc/HOWTO.md`](/pkg/rpc/HOWTO.md)
- [`/examples/benchmark_grpc`](/examples/benchmark_grpc/README.md)
- [`/tools/cmd/arpc`](/tools/cmd/arpc/README.mdhow )

[Go back to the monorepo root](/README.md) to continue reading.
