# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/states

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a pathless control-flow graph with a consensus (AOP, actor model, state-machine).

**/pkg/states** contains common state schema mixins to make state-based APIs easier to compose and exchange. It
also offers tooling for [piping](#piping) states between state machines.

## Available State Schemas

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
// inherit BasicSchema
schema := am.SchemaMerge(ssam.BasicSchema, am.Schema{
    "Foo": {Require: am.S{"Bar"}},
    "Bar": {},
})
names := am.SAdd(ssam.BasicStates.Names(), am.S{"Foo", "Bar"})
```

### Inherit from BasicStatesDef via a schema definition

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
    // inherit from BasicSchema
    ss.BasicSchema,
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

A "pipe" binds a [transition handler](/docs/manual.md#transition-handlers) to a source state-machine and [mutates](/docs/manual.md#mutations)
the target state-machine. Only [final handlers](/docs/manual.md#final-handlers) are subject to piping and the resulting
mutation won't block the source transition (it will be [queued instead](/docs/manual.md#queue-and-history)). The target
state-machine can reject the mutation, as a part of the [negotiation phase](/docs/manual.md#negotiation-handlers).

Pipes work only within the same Golang process, but when combined with [`/pkg/rpc`](/pkg/rpc), we can effectively pipe
states over the network. Some packages export predefined pipes for their state machines (eg [`/pkg/rpc`](/pkg/rpc) and
[`/pkg/node`](/pkg/node)).

### Installation Pipes

```go
import ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
```

### Predefined Pipes

These predefined functions should cover most of the use cases:

- `BindErr`
- `BindReady`
- `BindConnected`
- `Bind`
- `BindMany`
- `BindAny`

### Using Pipes

```go
// pipe RpcReady to RpcReady from rpcClient.Mach to myMach
ampipe.BindReady(rpcClient.Mach, myMach, "RpcReady", "")

// pipe Foo to FooActive and FooInactive from myMach1 to myMach2
ampipe.Bind(myMach1, myMach2, "Foo", "FooActive", "FooInactive")
```

### Piping Manually

```go
var source *am.Machine
var target *am.Machine

h := &struct {
    ReadyState am.HandlerFinal
    ReadyEnd   am.HandlerFinal
}{
    ReadyState: ampipe.Add(source, target, "Ready", "RpcReady"),
    ReadyEnd:   ampipe.Remove(source, target, "Ready", "RpcReady"),
}

source.BindHandlers(h)
```

## Extending States

The purpose of this package is to share reusable schemas, but inheriting a full schema isn't enough. States can also be
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

// see state_utils.go
```

## Documentation

- [API /pkg/states](https://code.asyncmachine.dev/pkg/github.com/pancsta/asyncmachine-go/pkg/states.html)
- [API /pkg/states/pipes](https://code.asyncmachine.dev/pkg/github.com/pancsta/asyncmachine-go/pkg/states/pipes.html)
- [pkg.go.dev - /pkg/states](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/states)
- [pkg.go.dev - /pkg/states/pipes](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/states/pipes)
- [`/docs/schema.md`](/docs/schema.md)
- [`/examples/pipes`](/examples/pipes/example_pipes.go)

## Status

Testing, semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
