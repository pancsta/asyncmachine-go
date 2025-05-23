# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/states

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

**/pkg/states** contains common state definitions to make state-based API easier to compose and exchange. Additionally it
offers tooling for "piping" states between state machines.

## Available State Definitions

- [BasicStatesDef](/pkg/states/ss_basic.go): Start, Ready, Healthcheck
- [ConnectedStatesDef](/pkg/states/ss_connected.go): Client connection in 4 states
- [DisposedStatesDef](/pkg/states/ss_disposed.go): Async disposal

## Installation States

```go
import ssam "github.com/pancsta/asyncmachine-go/pkg/states"
```

## Examples

### Inherit from BasicStatesDef manually

```go
// inherit from RPC worker
ssStruct := am.SchemaMerge(ssam.BasicStruct, am.Schema{
    "Foo": {Require: am.S{"Bar"}},
    "Bar": {},
})
ssNames := am.SAdd(ssam.BasicStates.Names(), am.S{"Foo", "Bar"})
```

### Inherit from BasicStatesDef via a definition

```go
// MyMachStatesDef contains all the states of the MyMach state machine.
type MyMachStatesDef struct {
    *am.StatesBase

    State1 string
    State2 string

    // inherit from BasicStatesDef
    *ss.BasicStatesDef
}

// MyMachSchema represents all relations and properties of MyMachStates.
var MyMachSchema = SchemaMerge(
    // inherit from BasicStruct
    ss.BasicStruct,
    am.Schema{

        ssM.State1: {},
        ssM.State2: {
            Multi: true,
        },
})
```

### Inherit from BasicStatesDef via the [generator](/tools/cmd/am-gen/README.md)

```bash
$ am-gen --name MyMach \
  --states State1,State2 \
  --inherit basic
```

## Piping

A "pipe" binds a handler of a source machine, to a mutation in a target machine. Currently, only [final handlers](/docs/manual.md#final-handlers)
are supported.

Each module can export their own pipes, like [`/pkg/rpc`](/pkg/rpc) and [`/pkg/node`](/pkg/node).

### Available Pipes

- `BindConnected`
- `BindErr`
- `BindReady`

### Installation Pipes

```go
import ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
```

### Using Pipes

```go
ampipe.BindReady(rpcClient.Mach, myMach, "RpcReady", "")
```

### Piping Manually

```go
var source *am.Machine
var target *am.Machine

h := &struct {
    ReadyState am.HandlerFinal
    ReadyEnd   am.HandlerFinal
}{
    ReadyState: Add1(source, target, "Ready", "RpcReady"),
    ReadyEnd:   Remove1(source, target, "Ready", "RpcReady"),
}

source.BindHandlers(h)
```

## Extending States

The purpose of this package is to share reusable states, but inheriting a definition isn't enough. States can also be
extended on relations-level using helpers from `states_utils.go` (generated), although sometimes it's better to override
a state (eg Ready).

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    "github.com/pancsta/asyncmachine-go/pkg/states"
    ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

// ...

MyMychStruct = am.Schema{

    // inject Client states into Connected
    "Connected": StateAdd(
        states.ConnectedStruct[states.ConnectedStates.Connected],
        am.State{
            Remove: S{"RetryingConn"},
            Add:    S{"Handshaking"},
    }),
}
```

## Documentation

- [godoc /pkg/states](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/states)
- [/examples/pipes](/examples/pipes/example_pipes.go)

## Status

Testing, semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
