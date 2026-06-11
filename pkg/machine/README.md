# 🦾 /pkg/machine

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a pathless control-flow graph with a consensus (AOP, actor model, state-machine).

**`/pkg/machine`** is a nondeterministic, multi-state, clock-based, relational, optionally accepting, and non-blocking
**state machine**. It's a form of a _rules engine_ that can orchestrate blocking APIs into fully controllable async
state-machines. Write ops are [state mutations](/docs/manual.md#mutations), read ops are [state checking](/docs/manual.md#active-states),
and subscriptions are [state waiting](/docs/manual.md#waiting). It's dependency-free and a building block of a [larger project](https://asyncmachine.dev).

## Installation

```go
import am "github.com/pancsta/asyncmachine-go/pkg/machine"
```

## Features

Features are explained using [Mermaid flow diagrams](/docs/diagrams.md), and the headers link to relevant sections of
the [manual](/docs/manual.md).

### [Multi-state](/docs/manual.md#mutations)

Many states can be active at the same time.

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-multi.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-multi.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-multi.mermaid.light.svg">
</picture></div>

### [Clock and state contexts](/docs/manual.md#clock-and-context)

States have clocks that produce contexts (odd = active; even = inactive).

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-clocks.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-clocks.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-clocks.mermaid.light.svg">
</picture></div>

### [Queue](/docs/manual.md#queue-and-history)

Queue of mutations enable lock-free [actor model](https://en.wikipedia.org/wiki/Actor_model).

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-queue.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-queue.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-queue.mermaid.light.svg">
</picture></div>

### [AOP handlers](/docs/manual.md#transition-handlers)

States are [Aspects](https://en.wikipedia.org/wiki/Aspect-oriented_programming) with Enter, State, Exit, and End handlers.

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-handlers.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-handlers.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-handlers.mermaid.light.svg">
</picture></div>

### [Negotiation](/docs/manual.md#transition-lifecycle)

Transitions are cancellable (during the negotiation phase).

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-negotiation.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-negotiation.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-negotiation.mermaid.light.svg">
</picture></div>

### [Relations](/docs/manual.md#relations)

States are connected via Require, Remove, and Add relations.

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-relations.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-relations.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-relations.mermaid.light.svg">
</picture></div>

### [Subscriptions](/docs/manual.md#waiting)

Channel-broadcast waiting on clock values.

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-subscriptions.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-subscriptions.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/basics-subscriptions.mermaid.light.svg">
</picture></div>

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
    mach.HandlersBind(&Handlers{
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

Subscriptions do not allocate goroutines and channels are reused.

```go
// wait until Foo becomes active
<-mach.When1("Foo", nil)

// wait until Foo becomes inactive
<-mach.WhenNot1("Foo", nil)

// wait for Foo to be activated with an arg ID=123
<-mach.WhenArgs("Foo", am.A{"ID": 123}, nil)

// wait for Foo to have a tick >= 6
<-mach.WhenTime1("Foo", 6, nil)

// wait for Foo to have a tick >= 6 and Bar tick >= 10
<-mach.WhenTime(am.S{"Foo", "Bar"}, am.Time{6, 10}, nil)

// wait for Foo to have a tick increased by 2
<-mach.WhenTicks("Foo", 2, nil)

// wait for next time Foo is active (even if currently active)
<-mach.WhenNextActive("Foo", nil)

// wait for a mutation to execute
<-mach.WhenQueue(mach.Add1("Foo", nil))

// wait for an error
<-mach.WhenErr(nil)

// wait on a time query
<-mach.WhenQuery(func(c am.Clock) bool {
    // Foo activated >5 times and Bar activated twice as much
    return c["Foo"] >= 10 && c["Bar"] >= 2*c["Foo"]
}, nil)
```

### State Targeting

[Transition](/docs/manual.md#transition-lifecycle) is available within [transition handlers](/docs/manual.md#transition-handlers).

```go
var (
    tx am.Transition
    t1 am.Time
    t2 am.Time
)

// was Foo added and Bar removed?
added, removed := tx.TimeIndexDiff()
added.Is1("Foo") && removed.Is1("Bar")

// was Foo called?
tx.TimeIndexCalled().Is1("Foo")

// was Foo active before?
tx.TimeIndexBefore().Is1("Foo")

// will Foo be active after?
tx.TimeIndexAfter().Is1("Foo")

// is Foo queued?
mach.WillBe1("Foo")

// is Foo queued at the end?
mach.WillBe1("Foo", am.PositionLast)

// number of states which ticked
len(tx.TimeIndexTimeDiff().ActiveStates())

// did Foo tick between t1 and t2?
t2.DiffSince(t1).NonZeroStates().Is1("Foo")
```

### [Schema File](/docs/schema.md)

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

### [Passing Args](/docs/manual.md#typesafe-arguments)

```go
// Example with typed state names (ss) and typed arguments (A).
mach.Add1(ss.KillingWorker, Pass(&A{
    ConnAddr:   ":5555",
    WorkerAddr: ":5556",
}))
```

### Mutations and Relations

While [mutations](/docs/manual.md#mutations) are the heartbeat of asyncmachine, it's the [relations](/docs/manual.md#relations)
which define the **rules of the flow**. Check out the [relations playground](https://play.golang.com/p/FgetijX8r0T) and
quiz yourself (maybe a [fancier playground](https://goplay.tools/snippet/FgetijX8r0T)).

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

- [Relations playground](https://play.golang.com/p/FgetijX8r0T)
- Interactively use the [TUI debugger](/tools/cmd/am-dbg/README.md) with data pre-generated by a [secai bot](https://github.com/pancsta/secai):

```bash
go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest \
  --import-data https://pancsta.github.io/assets/asyncmachine-go/am-dbg-exports/secai-cook.gob.br \
  mach://cook
```

## [Examples](/examples/README.md)

All examples and benchmarks can be found in [/examples](/examples/README.md).

## Devtools

<div align="center">
    <a href="/tools/cmd/am-dbg/README.md">
        <img src="https://pancsta.github.io/assets/asyncmachine-go/am-dbg-log.png"
            alt="am-dbg"></a>
</div>

- [`/tools/cmd/am-dbg`](/tools/cmd/am-dbg/README.md) Multi-client TUI debugger.
- [`/tools/cmd/arpc`](/tools/cmd/arpc) Network-native REPL and CLI.
- [`/tools/cmd/am-vis`](/tools/cmd/am-vis) Generates D2 diagrams.
- [`/tools/cmd/am-gen`](/tools/cmd/am-gen) Generates schema files and Grafana dashboards.
- [`/tools/cmd/am-relay`](/tools/cmd/am-relay) Rotates logs and relays WASM.

## Apps

**asyncmachine-go** synchronizes state for the following projects:

- [secai](https://github.com/pancsta/secai) - AI Workflows framework
- [secai Web UI](https://github.com/pancsta/secai/tree/main/web) - WebAssembly [go-app](https://go-app.dev/) PWA
- Self-hosting of [pkg/rpc](pkg/rpc/states), [pkg/node](pkg/node/states), [pkg/pubsub](pkg/pubsub/states)
- [arpc REPL](/tools/repl/states) - Cobra-based REPL
- [am-dbg TUI Debugger](/tools/debugger/states) - Single state-machine TUI app
- [libp2p PubSub Simulator](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-simulator) - Sandbox
  simulator for libp2p-pubsub
- [libp2p PubSub Benchmark](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-benchmark) -
  Benchmark of libp2p-pubsub ported to asyncmachine-go

## Documentation

- [API](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine)
- [diagrams](/docs/diagrams.md) \| [cookbook](/docs/cookbook.md)
- [manual.md](/docs/manual.md) \| [manual.pdf](https://pancsta.github.io/assets/asyncmachine-go/manual.pdf)
  - [Machine and States](/docs/manual.md#machine-and-states)
  - [Changing State](/docs/manual.md#changing-state)
  - [Advanced Topics](/docs/manual.md#advanced-topics)
  - [Cheatsheet](/docs/manual.md#cheatsheet)

## API

The common API methods are listed below. There's more for [local state machines](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine),
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

  // Add1 is [Machine.Add1].
  Add1(state string, args A) Result
  // Add is [Machine.Add].
  Add(states S, args A) Result
  // Remove1 is [Machine.Remove1].
  Remove1(state string, args A) Result
  // Remove is [Machine.Remove].
  Remove(states S, args A) Result
  // AddErr is [Machine.AddErr].
  AddErr(err error, args A) Result
  // AddErrState is [Machine.AddErrState].
  AddErrState(state string, err error, args A) Result
  // Toggle is [Machine.Toggle].
  Toggle(states S, args A) Result
  // Toggle1 is [Machine.Toggle1].
  Toggle1(state string, args A) Result
  // Set is [Machine.Set].
  Set(states S, args A) Result

  // Traced mutations (remote)

  // EvAdd1 is [Machine.EvAdd1].
  EvAdd1(event *Event, state string, args A) Result
  // EvAdd is [Machine.EvAdd].
  EvAdd(event *Event, states S, args A) Result
  // EvRemove1 is [Machine.EvRemove1].
  EvRemove1(event *Event, state string, args A) Result
  // EvRemove is [Machine.EvRemove].
  EvRemove(event *Event, states S, args A) Result
  // EvAddErr is [Machine.EvAddErr].
  EvAddErr(event *Event, err error, args A) Result
  // EvAddErrState is [Machine.EvAddErrState].
  EvAddErrState(event *Event, state string, err error, args A) Result
  // EvToggle is [Machine.Toggle].
  EvToggle(event *Event, states S, args A) Result
  // EvToggle1 is [Machine.Toggle1].
  EvToggle1(event *Event, state string, args A) Result

  // Waiting (remote)

  // WhenArgs is [Machine.WhenArgs].
  WhenArgs(state string, args A, ctx context.Context) <-chan struct{}

  // ///// LOCAL

  // Checking (local)

  // Err is [Machine.Err].
  Err() error
  // IsErr is [Machine.IsErr].
  IsErr() bool
  // Is is [Machine.Is].
  Is(states S) bool
  // Is1 is [Machine.Is1].
  Is1(state string) bool
  // Any is [Machine.Any].
  Any(states ...S) bool
  // Any1 is [Machine.Any1].
  Any1(state ...string) bool
  // Not is [Machine.Not].
  Not(states S) bool
  // Not1 is [Machine.Not1].
  Not1(state string) bool
  // IsTime is [Machine.IsTime].
  IsTime(time Time, states S) bool
  // WasTime is [Machine.WasTime].
  WasTime(time Time, states S) bool
  // IsClock is [Machine.IsClock].
  IsClock(clock Clock) bool
  // WasClock is [Machine.WasClock].
  WasClock(clock Clock) bool
  // Has is [Machine.Has].
  Has(states S) bool
  // Has1 is [Machine.Has1].
  Has1(state string) bool
  // CanAdd is [Machine.CanAdd].
  CanAdd(states S, args A) Result
  // CanAdd1 is [Machine.CanAdd1].
  CanAdd1(state string, args A) Result
  // CanRemove is [Machine.CanRemove].
  CanRemove(states S, args A) Result
  // CanRemove1 is [Machine.CanRemove1].
  CanRemove1(state string, args A) Result
  // Transition is [Machine.Transition].
  Transition() *Transition
  // IsLocal is [Machine.IsLocal].
  IsLocal() bool
  // ErrInternal is [Machine.ErrInternal].
  ErrInternal() <-chan error

  // Waiting (local)

  // When is [Machine.When].
  When(states S, ctx context.Context) <-chan struct{}
  // When1 is [Machine.When1].
  When1(state string, ctx context.Context) <-chan struct{}
  // WhenNot is [Machine.WhenNot].
  WhenNot(states S, ctx context.Context) <-chan struct{}
  // WhenNot1 is [Machine.WhenNot1].
  WhenNot1(state string, ctx context.Context) <-chan struct{}
  // WhenTime is [Machine.WhenTime].
  WhenTime(states S, times Time, ctx context.Context) <-chan struct{}
  // WhenTime1 is [Machine.WhenTime1].
  WhenTime1(state string, tick uint64, ctx context.Context) <-chan struct{}
  // WhenTicks is [Machine.WhenTicks].
  WhenTicks(state string, ticks int, ctx context.Context) <-chan struct{}
  // WhenNextActive is [Machine.WhenNextActive].
  WhenNextActive(state string, ctx context.Context) <-chan struct{}

  // WhenQuery is [Machine.WhenQuery].
  WhenQuery(query func(clock Clock) bool, ctx context.Context) <-chan struct{}
  // WhenErr is [Machine.WhenErr].
  WhenErr(ctx context.Context) <-chan struct{}
  // WhenQueue is [Machine.WhenQueue].
  WhenQueue(tick Result) <-chan struct{}

  // Getters (local)

  // StateNames is [Machine.StateNames].
  StateNames() S
  // ActiveStates is [Machine.ActiveStates].
  ActiveStates(states S) S
  // Tick is [Machine.Tick].
  Tick(state string) uint64
  // Clock is [Machine.Clock].
  Clock(states S) Clock
  // Time is [Machine.Time].
  Time(states S) Time
  // QueueTick is [Machine.QueueTick].
  QueueTick() uint64
  // MachineTick is [Machine.MachineTick].
  MachineTick() uint32
  // QueueLen is [Machine.QueueLen].
  QueueLen() uint16
  // NewStateCtx is [Machine.NewStateCtx].
  NewStateCtx(state string, e ...*Event) context.Context
  // Export is [Machine.Export].
  Export() (*Serialized, Schema, error)
  // Schema is [Machine.Schema].
  Schema() Schema
  // Switch is [Machine.Switch].
  Switch(groups ...S) string
  // Groups is [Machine.Groups].
  Groups() (map[string][]int, []string)
  // Index is [Machine.Index].
  Index(states S) []int
  // Index1 is [Machine.Index1].
  Index1(state string) int

  // Misc (local)

  // Id is [Machine.Id].
  Id() string
  // ParentId is [Machine.ParentId].
  ParentId() string
  // ParseStates is [Machine.ParseStates].
  ParseStates(states S) S
  // Tags is [Machine.Tags].
  Tags() []string
  // Context is [Machine.Ctx].
  Context() context.Context
  // ContextParent is [Machine.ContextParent].
  ContextParent() context.Context
  // String is [Machine.String].
  String() string
  // StringAll is [Machine.StringAll].
  StringAll() string
  // Log is [Machine.Log].
  Log(msg string, args ...any)
  // SemLogger is [Machine.SemLogger].
  SemLogger() SemLogger
  // Inspect is [Machine.Inspect].
  Inspect(states S) string
  // HandlersBind is [Machine.BindHandlers].
  HandlersBind(handlers any, opts ...BindOpts) (string, error)
  // HandlersBindMaps is [Machine.HandlerBindMaps].
  HandlersBindMaps(negotiations map[string]HandlerNegotiation,
    finals map[string]HandlerFinal, opts ...BindOpts) (string, error)
  // HandlersDetach is [Machine.DetachHandlers].
  HandlersDetach(bindingId string) error
  // Handlers is [Machine.Handlers].
  Handlers() []string
  // StatesVerified is [Machine.StatesVerified].
  StatesVerified() bool
  // Tracers is [Machine.Tracers].
  Tracers() []Tracer
  // TracerDetach is [Machine.TracerDetach].
  TracerDetach(id string) error
  // TracerBind is [Machine.TracerBind].
  TracerBind(tracer Tracer) (string, error)
  // AddBreakpoint is [Machine.AddBreakpoint].
  AddBreakpoint(added S, removed S, strict bool)
  // AddBreakpoint1 is [Machine.AddBreakpoint1].
  AddBreakpoint1(added string, removed string, strict bool)
  // Dispose is [Machine.Dispose].
  Dispose()
  // WhenDisposed is [Machine.WhenDisposed].
  WhenDisposed() <-chan struct{}
  // IsDisposed is [Machine.IsDisposed].
  IsDisposed() bool
  // OnDispose is [Machine.OnDispose].
  OnDispose(fn HandlerDispose)
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
- [actor model](https://en.wikipedia.org/wiki/Actor_model)
- [causal inference](https://en.wikipedia.org/wiki/Causal_inference)
- [declarative logic](https://en.wikipedia.org/wiki/Declarative_programming)

### State Oriented Programming

This is a new term which could possibly encapsulate the unique way of modeling the flow using **asyncmachine**. Unlike
common state machines, there are no transition paths between states, and the activation / deactivation is decided by the
state consensus. The consensus is calculated from a mutation, active states, relations between states, and negotiating
methods. Just like object-oriented programming solves domain complexity, state-oriented programming tries to solve the
unpredictability of the flow.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
