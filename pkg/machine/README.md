# 🦾 /pkg/machine

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

**/pkg/machine** is a nondeterministic, multi-state, clock-based, relational, optionally accepting, and non-blocking
state machine. It's a form of a **rules engine** that can orchestrate blocking APIs into fully controllable async
state-machines. Write ops are [state mutations](/docs/manual.md#mutations), read ops are [state checking](/docs/manual.md#active-states),
and subscriptions are [state waiting](/docs/manual.md#waiting).

## Installation

```go
import am "github.com/pancsta/asyncmachine-go/pkg/machine"
```

## Features

Features are explained using [Mermaid flow diagrams](../../docs/diagrams.md), and headers link to relevant sections of
the [manual](/docs/manual.md).

### [Multi-state](/docs/manual.md#mutations)

Many states can be active at the same time.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_1.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_1.light.svg">
  <img alt="Diagram showing multi-state capability" src="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_1.light.svg">
</picture>

### [Clock and state contexts](/docs/manual.md#clock-and-context)

States have clocks that produce contexts (odd = active; even = inactive).

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_2.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_2.light.svg">
  <img alt="Diagram showing state clocks and contexts" src="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_2.light.svg">
</picture>

### [Queue](/docs/manual.md#queue-and-history)

Queue of mutations enables lock-free [Actor Model](https://en.wikipedia.org/wiki/Actor_model).

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_3.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_3.light.svg">
  <img alt="Diagram showing queue and mutations" src="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_3.light.svg">
</picture>

### [AOP handlers](/docs/manual.md#transition-handlers)

States are [Aspects](https://en.wikipedia.org/wiki/Aspect-oriented_programming) with Enter, State, Exit, and End
handlers.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_4.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_4.light.svg">
  <img alt="Diagram showing AOP handlers" src="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_4.light.svg">
</picture>

### [Negotiation](/docs/manual.md#transition-lifecycle)

Transitions are cancellable (during the negotiation phase).

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_5.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_5.light.svg">
  <img alt="Diagram showing negotiation phase" src="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_5.light.svg">
</picture>

### [Relations](/docs/manual.md#relations)

States are connected via Require, Remove, and Add relations.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_6.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_6.light.svg">
  <img alt="Diagram showing state relations" src="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_6.light.svg">
</picture>

### [Subscriptions](/docs/manual.md#waiting)

Channel-based broadcast for waiting on clock values.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_7.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_7.light.svg">
  <img alt="Diagram showing subscriptions" src="https://github.com/pancsta/assets/raw/main/asyncmachine-go/diagrams/diagram_7.light.svg">
</picture>

### [Error handling](/docs/manual.md#error-handling)

Error is a state, handled just like any other mutation.

```go
val, err := someOp()
if err != nil {
    mach.AddErr(err, nil)
    return // no err needed
}
```

### [Tracers](/docs/manual.md#tracing-and-metrics)

Synchronous tracers for internal events.

```text
TransitionInit TransitionStart TransitionEnd HandlerStart HandlerEnd
MachineInit MachineDispose NewSubmachine QueueEnd SchemaChange VerifyStates
```

## Usage

### [Raw Strings](/examples/raw_strings/raw_strings.go)

```go
// ProcessingFile to FileProcessed
// 1 async and 1 sync state
package main

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func main() {
    // init the state machine
    mach := am.New(nil, am.Schema{
        "ProcessingFile": { // async
            Remove: am.S{"FileProcessed"},
        },
        "FileProcessed": { // async
            Remove: am.S{"ProcessingFile"},
        },
        "InProgress": { // sync
            Auto:    true,
            Require: am.S{"ProcessingFile"},
        },
    }, nil)
    mach.BindHandlers(&Handlers{
        Filename: "README.md",
    })
    // change the state
    mach.Add1("ProcessingFile", nil)
    // wait for completed
    select {
    case <-time.After(5 * time.Second):
        println("timeout")
    case <-mach.WhenErr(nil):
        println("err:", mach.Err())
    case <-mach.When1("FileProcessed", nil):
        println("done")
    }
}

type Handlers struct {
    Filename string
}

// negotiation handler
func (h *Handlers) ProcessingFileEnter(e *am.Event) bool {
    // read-only ops
    // decide if moving fwd is ok
    // no blocking
    // lock-free critical section
    return true
}

// final handler
func (h *Handlers) ProcessingFileState(e *am.Event) {
    // read & write ops
    // no blocking
    // lock-free critical section
    mach := e.Machine
    // clock-based expiration context
    stateCtx := mach.NewStateCtx("ProcessingFile")
    // unblock
    go func() {
        // re-check the state ctx
        if stateCtx.Err() != nil {
            return // expired
        }
        // blocking call
        err := processFile(h.Filename, stateCtx)
        // re-check the state ctx after a blocking call
        if stateCtx.Err() != nil {
            return // expired
        }
        if err != nil {
            mach.AddErr(err, nil)
            return
        }
        // move to the next state in the flow
        mach.Add1("FileProcessed", nil)
    }()
}
```

### [Waiting](/examples/subscriptions/example_subscriptions.go)

Subscriptions do not allocate goroutines.

```go
// wait until FileDownloaded becomes active
<-mach.When1("FileDownloaded", nil)

// wait until FileDownloaded becomes inactive
<-mach.WhenNot1("DownloadingFile", nil)

// wait for EventConnected to be activated with an arg ID=123
<-mach.WhenArgs("EventConnected", am.A{"ID": 123}, nil)

// wait for Foo to have a tick >= 6
<-mach.WhenTime1("Foo", 6, nil)

// wait for Foo to have a tick >= 6 and Bar tick >= 10
<-mach.WhenTime(am.S{"Foo", "Bar"}, am.Time{6, 10}, nil)

// wait for DownloadingFile to have a tick increased by 2 since now
<-mach.WhenTicks("DownloadingFile", 2, nil)

// wait for an error
<-mach.WhenErr(nil)
```

### Schema File

```go
// BasicStatesDef contains all the states of the Basic state machine.
type BasicStatesDef struct {
    *am.StatesBase

    // ErrNetwork indicates a generic network error.
    ErrNetwork string
    // ErrHandlerTimeout indicates one of state machine handlers has timed out.
    ErrHandlerTimeout string

    // Start indicates the machine should be working. Removing start can force
    // stop the machine.
    Start string
    // Ready indicates the machine meets criteria to perform work, and requires
    // Start.
    Ready string
    // Healthcheck is a periodic request making sure that the machine is still
    // alive.
    Healthcheck string
}

var BasicSchema = am.Schema{

    // Errors

    ssB.ErrNetwork:        {Require: S{Exception}},
    ssB.ErrHandlerTimeout: {Require: S{Exception}},

    // Basics

    ssB.Start:       {},
    ssB.Ready:       {Require: S{ssB.Start}},
    ssB.Healthcheck: {},
}
```

### Passing Args

```go
// Example with typed state names (ssS) and typed arguments (A).
mach.Add1(ssS.KillingWorker, Pass(&A{
    ConnAddr:   ":5555",
    WorkerAddr: ":5556",
}))
```

### Mutations and Relations

While [mutations](/docs/manual.md#mutations) are the heartbeat of asyncmachine, it's the [relations](/docs/manual.md#relations)
which define the **rules of the flow**. Check out the [relations playground](https://play.golang.com/p/c89OjCUMxW-) and
quiz yourself (maybe a [fancier playground](https://goplay.tools/snippet/c89OjCUMxW-)).

```go
mach := newMach("DryWaterWet", am.Schema{
    "Wet": {
        Require: am.S{"Water"},
    },
    "Dry": {
        Remove: am.S{"Water"},
    },
    "Water": {
        Add:    am.S{"Wet"},
        Remove: am.S{"Dry"},
    },
})
mach.Add1("Dry", nil)
mach.Add1("Water", nil)
// TODO quiz: is Wet active?
```

## Demos

- [Relations playground](https://play.golang.com/p/c89OjCUMxW-)
- Interactively use the [TUI debugger](/tools/cmd/am-dbg) with data pre-generated by
  - **libp2p-pubsub-simulator** in
    - web terminal: [http://188.166.101.108:8080/wetty/ssh](http://188.166.101.108:8080/wetty/ssh/am-dbg?pass=am-dbg:8080/wetty/ssh/am-dbg?pass=am-dbg)
    - remote terminal: `ssh 188.166.101.108 -p 4444`
    - local terminal: `go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest --import-data https://pancsta.github.io/assets/asyncmachine-go/am-dbg-exports/pubsub-sim.gob.br`
  - **remote integration tests** in
    - web terminal: [http://188.166.101.108:8081/wetty/ssh](http://188.166.101.108:8081/wetty/ssh/am-dbg?pass=am-dbg:8081/wetty/ssh/am-dbg?pass=am-dbg)
    - remote terminal: `ssh 188.166.101.108 -p 4445`
    - local terminal: `go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest --import-data https://pancsta.github.io/assets/asyncmachine-go/am-dbg-exports/remote-tests.gob.br`

## [Examples](/examples/README.md)

All examples and benchmarks can be found in [/examples](/examples/README.md).

## Tools

[![am-dbg](https://pancsta.github.io/assets/asyncmachine-go/am-dbg-log.png)](/tools/cmd/am-dbg/README.md)

- [`/tools/cmd/am-dbg`](/tools/cmd/am-dbg/README.md) Multi-client TUI debugger.
- [`/tools/cmd/am-gen`](/tools/cmd/am-gen/README.md) Generates schema files and Grafana dashboards.
- [`/tools/cmd/am-vis`](https://github.com/pancsta/asyncmachine-go/pull/216) Generates diagrams of interconnected state machines.
- [`/tools/cmd/arpc`](/tools/cmd/arpc) Network-native REPL and CLI.

## Apps

- [secai](https://github.com/pancsta/secai) AI Agents framework.
- [arpc REPL](/tools/repl) Cobra-based REPL.
- [am-dbg TUI Debugger](/tools/debugger/README.md) Single state machine TUI app.
- [libp2p PubSub Simulator](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-simulator) Sandbox
  simulator for libp2p-pubsub.
- [libp2p PubSub Benchmark](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-benchmark)
  Benchmark of libp2p-pubsub ported to asyncmachine-go.

## Documentation

- [API](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine)
- [diagrams](/docs/diagrams.md) \| [cookbook](/docs/cookbook.md)
- [manual.md](/docs/manual.md) \| [manual.pdf](https://pancsta.github.io/assets/asyncmachine-go/manual.pdf)
  - [Machine and States](/docs/manual.md#machine-and-states)
  - [Changing State](/docs/manual.md#changing-state)
  - [Advanced Topics](/docs/manual.md#advanced-topics)
  - [Cheatsheet](/docs/manual.md#cheatsheet)

## API

The most common API methods are listed below. There's more for [local state machines](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine),
but all of these are also implemented in the [transparent RPC layer](/pkg/rpc/README.md).

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
    Set(states S, args A) Result
    AddErr(err error, args A) Result
    AddErrState(state string, err error, args A) Result

    // Traced mutations (remote)

    EvAdd1(event *Event, state string, args A) Result
    EvAdd(event *Event, states S, args A) Result
    EvRemove1(event *Event, state string, args A) Result
    EvRemove(event *Event, states S, args A) Result
    EvAddErr(event *Event, err error, args A) Result
    EvAddErrState(event *Event, state string, err error, args A) Result

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
    AddBreakpoint(added S, removed S, strict bool)
    Dispose()
    WhenDisposed() <-chan struct{}
    IsDisposed() bool
}
```

## Tests

It's very easy to get a grasp of how asyncmachine works by reading the [idiomatic test suite](/pkg/machine/machine_test.go).
Consider the example below of a method used to wait for certain arguments passing via a state activation:

```go
func TestWhenArgs(t *testing.T) {
    // init
    m := NewRels(t, nil)

    // bind
    whenCh := m.WhenArgs("B", A{"foo": "bar"}, nil)

    // incorrect args
    m.Add1("B", A{"foo": "foo"})
    select {
    case <-whenCh:
        t.Fatal("whenCh shouldnt be selected")
    default:
        // pass
    }

    // correct args
    m.Add1("B", A{"foo": "bar"})
    select {
    case <-whenCh:
        // pass
    default:
        t.Fatal("whenCh should be selected")
    }

    // dispose
    m.Dispose()
    <-m.WhenDisposed()
}
```

## Status

Release Candidate, semantically versioned, partially optimized.

## Concepts

**asyncmachine** is loosely based on the following concepts:

- [dependency graph](https://en.wikipedia.org/wiki/Dependency_graph)
- [async event emitter](https://en.wikipedia.org/wiki/Event-driven_architecture)
- [nondeterministic state machine](https://en.wikipedia.org/wiki/Nondeterministic_finite_automaton)
- [queue](https://en.wikipedia.org/wiki/Queue_(abstract_data_type))
- [aspect-oriented programming](https://en.wikipedia.org/wiki/Aspect-oriented_programming)
- [SQL relations](https://en.wikipedia.org/wiki/SQL)
- [Paxos negotiation](https://en.wikipedia.org/wiki/Paxos_(computer_science))
- [logical clock](https://en.wikipedia.org/wiki/Logical_clock)
- [programming by contract](https://en.wikipedia.org/wiki/Design_by_contract)
- [non-blocking](https://en.wikipedia.org/wiki/Non-blocking_algorithm)
- [Actor Model](https://en.wikipedia.org/wiki/Actor_model)
- [causal inference](https://en.wikipedia.org/wiki/Causal_inference)
- [declarative logic](https://en.wikipedia.org/wiki/Declarative_programming)

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
