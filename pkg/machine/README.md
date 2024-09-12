# /pkg/machine

[-> go back to monorepo /](/README.md)

**asyncmachine-go** is a minimal implementation of [AsyncMachine](https://github.com/TobiaszCudnik/asyncmachine) (2012-2019)
in Golang using **channels and context**. It aims at simplicity and speed, while maintaining and extending ideas of the
original. It delivers a solid toolset and conventions for reliable state machines.

**asyncmachine** can transform blocking APIs into controllable state machines with ease. It shares similarities with
[Ergo's]() actor model, but focuses on workflows like [Temporal](). Unlike both mentioned frameworks, it's lightweight
and adds features progressively.

## Comparison

Common differences between asyncmachine and other state machines:

- many states can be active at the same time
- transitions between all the states are allowed
- states are connected by relations
- every transition can be rejected
- every state has a clock
- error is a state

## Buzzwords

> **AM tech:** event emitter, queue, dependency graph, AOP, logical clocks, 3k LoC, stdlib-only

> **AM provides:** states, events, thread-safety, logging, metrics, traces, debugger, history, flow constraints, scheduler

> **Flow constraints:** state mutations, negotiation, relations, "when" methods, state contexts, external contexts

## Basic Usage

```go
// ProcessingFile -> FileProcessed (1 async and 1 sync state)
package main

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func main() {
    // init the state machine
    mach := am.New(nil, am.Struct{
        "ProcessingFile": { // async
            Add: am.S{"InProgress"},
            Remove: am.S{"FileProcessed"},
        },
        "FileProcessed": { // async
            Remove: am.S{"ProcessingFile", "InProgress"},
        },
        "InProgress": {}, // sync
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
        println("err:", mach.Err)
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
    // lock-free critical zone
    return true
}

// final handler
func (h *Handlers) ProcessingFileState(e *am.Event) {
    // read & write ops
    // no blocking
    // lock-free critical zone
    mach := e.Machine
    // tick-based context
    stateCtx := mach.NewStateCtx("ProcessingFile")
    go func() {
        // block in the background, locks needed
        if stateCtx.Err() != nil {
            return // expired
        }
        // blocking call
        err := processFile(h.Filename, stateCtx)
        if err != nil {
            mach.AddErr(err)
            return
        }
        // re-check the tick ctx after a blocking call
        if stateCtx.Err() != nil {
            return // expired
        }
        // move to the next state in the flow
        mach.Add1("FileProcessed", nil)
    }()
}
```

## Waiting

```go
// wait until FileDownloaded becomes active
<-mach.When1("FileDownloaded", nil)

// wait until FileDownloaded becomes inactive
<-mach.WhenNot1("DownloadingFile", args, nil)

// wait for EventConnected to be activated with an arg ID=123
<-mach.WhenArgs("EventConnected", am.A{"ID": 123}, nil)

// wait for Foo to have a tick >= 6 and Bar tick >= 10
<-mach.WhenTime(am.S{"Foo", "Bar"}, am.Time{6, 10}, nil)

// wait for DownloadingFile to have a tick increased by 2 since now
<-mach.WhenTicks("DownloadingFile", 2, nil)

// wait for an error
<-mach.WhenErr()
```

See [docs/cookbook.md](docs/cookbook.md) for more snippets.

## Demo

Play with a live debugging session - interactively use the TUI debugger with data pre-generated by
libp2p-pubsub-simulator in:

- web browser: [http://188.166.101.108:8080/wetty/ssh](http://188.166.101.108:8080/wetty/ssh/am-dbg?pass=am-dbg:8080/wetty/ssh/am-dbg?pass=am-dbg)
- terminal: `ssh 188.166.101.108 -p 4444`

## Examples

All examples and benchmarks can be found in [/examples](), including ports of Temporal workflows.

## Tools

![am-dbg](../../assets/am-dbg.dark.png#gh-dark-mode-only)
![am-dbg](../../assets/am-dbg.light.png#gh-light-mode-only)

- [`/tools/cmd/am-gen`]()<br>
  am-gen is useful for bootstrapping states files.
- **[`/tools/cmd/am-dbg`]()**<br>
  am-dbg is a multi-client TUI debugger.
- **[`/pkg/telemetry`]()**<br>
  Telemetry exporters for am-dbg, Open Telemetry and Prometheus.

## Case Studies

Several case studies are available to show how to implement various types of state machines, measure performance and produce
a lot of inspectable data.

- [libp2p PubSub Simulator](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-simulator)
- [libp2p PubSub Benchmark](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-benchmark)
- [am-dbg TUI Debugger](/tools/debugger)

## Documentation

- [godoc](https://godoc.org/github.com/pancsta/asyncmachine-go/pkg/machine)
- [cookbook](/docs/cookbook.md)
- [discussions](https://github.com/pancsta/asyncmachine-go/discussions)
- [manual.md](/docs/manual.md) \| [manual.pdf](https://pancsta.github.io/assets/asyncmachine-go/manual.pdf)
  - [Machine and States](/docs/manual.md#machine-and-states)
      - [State Clocks and Context](/docs/manual.md#state-clocks-and-context)
      - [Auto States](/docs/manual.md#auto-states)
      - [Categories of States](/docs/manual.md#categories-of-states)
      - ...
  - [Changing State](/docs/manual.md#changing-state)
      - [State Mutations](/docs/manual.md#state-mutations)
      - [Transition Lifecycle](/docs/manual.md#transition-lifecycle)
      - [Calculating Target States](/docs/manual.md#calculating-target-states)
      - [Negotiation Handlers](/docs/manual.md#negotiation-handlers)
      - [Final Handlers](/docs/manual.md#final-handlers)
      - ...
  - [Advanced Topics](/docs/manual.md#advanced-topics)
      - [State's Relations](/docs/manual.md#states-relations)
      - [Queue and History](/docs/manual.md#queue-and-history)
      - [Typesafe States](/docs/manual.md#typesafe-states)
      - ...
  - [Cheatsheet](/docs/manual.md#cheatsheet)

## API

Most common API methods are listed below. There's more for local state machines, but all of these are also implemented
in the [transparent RPC layer](/pkg/rpc).

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

It's very easy to get a grasp of how asyncmachine works by reading the [idiomatic test suite](). Consider the example
below of a method used to wait for certain arguments passing via a state activation:

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
        t.Fatal("when shouldnt be resolved")
    default:
        // pass
    }

    // correct args
    m.Add1("B", A{"foo": "bar"})
    select {
    case <-whenCh:
        // pass
    default:
        t.Fatal("when should be resolved")
    }

    // dispose
    m.Dispose()
    <-m.WhenDisposed()
}
```

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.