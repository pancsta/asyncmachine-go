<br>
<div align="center">
  <a href="https://glasskube.dev?utm_source=github">
    <img src="/assets/logo.png" alt="asyncmachine-go logo" height="160">
  </a>

<h3 align="center">Declarative workflows with relations (state machine)</h3>

  <p align="center">
    <a href="#usage"><strong>Usage »</strong></a>
    <br>
    <a href="#demos"><strong>Demos »</strong></a>
    <br>
    <a href="#examples"><strong>Examples »</strong></a>
    <br>
    <a href="#documentation"><strong>Documentation »</strong></a>
    <br>
    <a href="#tools"><strong>Tools »</strong></a>
    <br>
    <a href="#integrations"><strong>Integrations »</strong></a>
    <br>
    <a href="#case-studies"><strong>Case Studies »</strong></a>
    <br>
  </p>
</div>

<hr>

![TUI Debugger](https://pancsta.github.io/assets/asyncmachine-go/am-dbg-teaser.gif)

## TL;DR

Relational state machine for workflows, with negotiation.

```go
mach := am.New(nil, am.Struct{
    "Foo": {Requires: "Bar"},
    "Bar": {},
}, nil)
mach.Add1("Foo")
mach.Is1("Foo") // false
```

- debugger
- traces
- metrics

## Intro

**asyncmachine-go** is a minimal implementation of [AsyncMachine](https://github.com/TobiaszCudnik/asyncmachine)
in Golang using **channels and context**. It aims at simplicity and speed.

It can be used as a lightweight in-memory [Temporal](https://github.com/temporalio/temporal)
alternative, worker for [Asynq](https://github.com/hibiken/asynq), or to create simple consensus engines, stateful
firewalls, telemetry, bots, etc.

> **asyncmachine-go** is a general purpose state machine for managing complex asynchronous workflows in a safe and
> structured way

### Comparison

Common differences from other state machines:

- many states can be active at the same time
- transitions between all the states are allowed
- states are connected by relations
- every transition can be rejected
- error is a state

### Buzzwords

> **AM tech:** event emitter, queue, dependency graph, AOP, logical clocks, <3k LoC, stdlib-only

> **AM provides:** states, events, thread-safety, logging, metrics, traces, debugger, history, flow constraints, scheduler

> **Flow constraints:** state mutations, negotiation, relations, "when" methods, state contexts, external contexts

## Usage

### Basics

```go
// ProcessingFile -> FileProcessed (1 async and 1 sync state)
package main

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func main() {
    // init the machine
    mach := am.New(nil, am.Struct{
        "ProcessingFile": {
            Add: am.S{"InProgress"},
            Remove: am.S{"FileProcessed"},
        },
        "FileProcessed": {
            Remove: am.S{"ProcessingFile", "InProgress"},
        },
        "InProgress": {},
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

### Waiting

```go
// wait until FileDownloaded becomes active
<-mach.When1("FileDownloaded", nil)

// wait until FileDownloaded becomes inactive
<-mach.WhenNot1("DownloadingFile", args, nil)

// wait for EventConnected to be activated with an arg ID=123
<-mach.WhenArgs("EventConnected", am.A{"ID": 123}, nil)

// wait for Foo to have a tick >= 6 and Bar tick >= 10
<-mach.WhenTime(am.S{"Foo", "Bar"}, am.T{6, 10}, nil)

// wait for DownloadingFile to have a tick increased by 2 since now
<-mach.WhenTicks("DownloadingFile", 2, nil)

// wait for an error
<-mach.WhenErr()
```

See [docs/cookbook.md](docs/cookbook.md) for more snippets.

## Demos

All demos have data produced by the go-libp2p-pubsub simulator [case study](#libp2p-pubsub-simulator).

- [**libp2p-pubsub simulator walkthrough**](https://pancsta.github.io/assets/asyncmachine-go/pubsub-sim-demo1.m4v)
  \- metrics, debugging, traces (text-only screencast)
- [**am-dbg over web**](http://188.166.101.108:8080/wetty/ssh/am-dbg?pass=am-dbg:8080/wetty/ssh/am-dbg?pass=am-dbg)
  \- interactively browse simulator's machines in the browser
- **am-dbg over ssh** - same as above, but in the terminal
  - `ssh 188.166.101.108 -p 4444`
- [**jaeger traces**](https://github.com/pancsta/asyncmachine-go/raw/main/assets/bench-jaeger-3h-10m.traces.json)
  \- import manually (interactive)
- [**grafana dashboard**](assets/grafana-mach-sim,sim-p1.json) - import manually

## Examples

### [FSM - Finite State Machine](/examples/fsm/fsm_test.go)

- [origin](https://en.wikipedia.org/wiki/Finite-state_machine)

<details>

<summary>States structure</summary>

```go
var (
    states = am.Struct{
        // input states
        InputPush: {},
        InputCoin: {},

        // "state" states
        Locked: {
            Auto:   true,
            Remove: groupUnlocked,
        },
        Unlocked: {Remove: groupUnlocked},
    }
)
```

</details>

### [NFA - Nondeterministic Finite Automaton](/examples/nfa/nfa_test.go)

- [origin](https://en.wikipedia.org/wiki/Nondeterministic_finite_automaton)

<details>

<summary>States structure</summary>

```go
var (
    states = am.Struct{
        // input states
        Input: {Multi: true},

        // action states
        Start: {Add: am.S{StepX}},

        // "state" states
        StepX: {Remove: groupSteps},
        Step0: {Remove: groupSteps},
        Step1: {Remove: groupSteps},
        Step2: {Remove: groupSteps},
        Step3: {Remove: groupSteps},
    }
)
```

</details>

### [PATH Watcher](/examples/watcher/watcher.go)

- [origin](https://github.com/pancsta/sway-yast/)

<details>

<summary>States structure</summary>

```go
// States map defines relations and properties of states (for files).
var States = am.Struct{
    Init: {Add: S{Watching}},

    Watching: {
        Add:   S{Init},
        After: S{Init},
    },
    ChangeEvent: {
        Multi:   true,
        Require: S{Watching},
    },

    Refreshing: {
        Multi:  true,
        Remove: S{AllRefreshed},
    },
    Refreshed:    {Multi: true},
    AllRefreshed: {},
}

// StatesDir map defines relations and properties of states (for directories).
var StatesDir = am.Struct{
    Refreshing:   {Remove: groupRefreshed},
    Refreshed:    {Remove: groupRefreshed},
    DirDebounced: {Remove: groupRefreshed},
    DirCached:    {},
}
```

</details>

### [Temporal Expense Workflow](/examples/temporal-expense/expense_test.go)

- [origin](https://github.com/temporalio/samples-go/blob/main/expense/)

<details>

<summary>States structure</summary>

```go
// States map defines relations and properties of states.
var States = am.Struct{
    CreatingExpense: {Remove: GroupExpense},
    ExpenseCreated:  {Remove: GroupExpense},
    WaitingForApproval: {
        Auto:   true,
        Remove: GroupApproval,
    },
    ApprovalGranted: {Remove: GroupApproval},
    PaymentInProgress: {
        Auto:   true,
        Remove: GroupPayment,
    },
    PaymentCompleted: {Remove: GroupPayment},
}

```

</details>

### [Temporal FileProcessing Workflow](/examples/temporal-fileprocessing/fileprocessing.go)

- [origin](https://github.com/temporalio/samples-go/blob/main/fileprocessing/)
- [Asynq worker version](examples/asynq-fileprocessing/fileprocessing_task.go)

<details>

<summary>States structure</summary>

```go
// States map defines relations and properties of states.
var States = am.Struct{
    DownloadingFile: {Remove: GroupFileDownloaded},
    FileDownloaded:  {Remove: GroupFileDownloaded},
    ProcessingFile: {
        Auto:    true,
        Require: S{FileDownloaded},
        Remove:  GroupFileProcessed,
    },
    FileProcessed: {Remove: GroupFileProcessed},
    UploadingFile: {
        Auto:    true,
        Require: S{FileProcessed},
        Remove:  GroupFileUploaded,
    },
    FileUploaded: {Remove: GroupFileUploaded},
}

// Groups of mutually exclusive states.

var (
    GroupFileDownloaded = S{DownloadingFile, FileDownloaded}
    GroupFileProcessed  = S{ProcessingFile, FileProcessed}
    GroupFileUploaded   = S{UploadingFile, FileUploaded}
)
```

</details>

## Documentation

- [godoc](https://godoc.org/github.com/pancsta/asyncmachine-go/pkg/machine)
- [cookbook](/docs/cookbook.md)
- [discussions](https://github.com/pancsta/asyncmachine-go/discussions)
- [manual.md](/docs/manual.md) \| [manual.pdf](/assets/manual.pdf)
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

## Tools

### Generator

`am-gen` will quickly bootstrap a typesafe states file for you.

`$ am-gen states-file Foo,Bar`

<details>

<summary>See the result for Foo and Bar</summary>

```go
package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
var States = am.Struct{
    Foo: {},
    Bar: {},
}

// Groups of mutually exclusive states.

//var (
//      GroupPlaying = S{Playing, Paused}
//)

//#region boilerplate defs

// Names of all the states (pkg enum).

const (
    Foo = "Foo"
    Bar = "Bar"
)

// Names is an ordered list of all the state names.
var Names = S{
    Foo,
    Bar,
    am.Exception,
}

//#endregion
```

</details>

See [`tools/cmd/am-gen`](tools/cmd/am-gen/README.md) for more info.

### Debugger

![am-dbg](assets/am-dbg.dark.png#gh-dark-mode-only)
![am-dbg](assets/am-dbg.light.png#gh-light-mode-only)

`am-dbg` is a lightweight, multi-client debugger for AM. It easily handles hundreds of
 client machines, which are simultaneously streaming telemetry data. Some features include:

- states tree
- log view
- time travel
- transition steps
- import / export
- filters
- matrix view

See [`tools/cmd/am-dbg`](tools/cmd/am-dbg/README.md) for more info, or [import a sample asset](https://github.com/pancsta/assets/blob/main/asyncmachine-go/am-dbg-sim.gob.br)
with `--import-data`.

## Integrations

### Open Telemetry

![Prometheus Grafana](assets/otel-jaeger.dark.png#gh-dark-mode-only)
![Prometheus Grafana](assets/otel-jaeger.light.png#gh-light-mode-only)

[`pkg/telemetry`](pkg/telemetry/README.md) provides [Open Telemetry](https://opentelemetry.io/) integration which exposes
machine's states and transitions as Otel traces, compatible with [Jaeger](https://www.jaegertracing.io/).

See [`pkg/telemetry`](pkg/telemetry/README.md) for more info, or [import a sample asset](assets/bench-jaeger-3h-10m.traces.json?raw=true).

### Prometheus

![Prometheus Grafana](assets/prometheus-grafana.dark.png#gh-dark-mode-only)
![Prometheus Grafana](assets/prometheus-grafana.light.png#gh-light-mode-only)

[`pkg/telemetry/prometheus`](pkg/telemetry/prometheus/README.md) binds to machine's transactions and averages the
values withing an interval window and exposes various metrics. Combined with [Grafana](https://grafana.com/), it can be
used to monitor the metrics of you machines.

See [`pkg/telemetry/prometheus`](pkg/telemetry/prometheus/README.md) for more info.

## Case Studies

Several case studies are available to show how to implement various types of machines, measure performance and produce
a lot of inspectable data.

### libp2p-pubsub benchmark

![Test duration chart](assets/bench.dark.jpg#gh-dark-mode-only)
![Test duration chart](assets/bench.light.png#gh-light-mode-only)

- **pubsub host** - eg `ps-17` (20 states)<br />
  PubSub machine is a simple event loop with [multi states](/docs/manual.md#multi-states) which get responses via arg
  channels. Heavy use of [`Machine.Eval()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Eval).
- **discovery** - eg `ps-17-disc` (10 states)<br />
  Discovery machine is a simple event loop with [multi states](/docs/manual.md#multi-states) and a periodic refresh state.
- **discovery bootstrap** - eg `ps-17-disc-bf3` (5 states)<br />
  `BootstrapFlow` is a non-linear flow for topic bootstrapping with some retry logic.

<details>

<summary>See states structure and relations (pubsub host)</summary>

```go
package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// States define relations between states
var States = am.Struct{
    // peers
    PeersPending: {},
    PeersDead:    {},
    GetPeers:     {Multi: true},

    // peer
    PeerNewStream:   {Multi: true},
    PeerCloseStream: {Multi: true},
    PeerError:       {Multi: true},
    PublishMessage:  {Multi: true},
    BlacklistPeer:   {Multi: true},

    // topic
    GetTopics:       {Multi: true},
    AddTopic:        {Multi: true},
    RemoveTopic:     {Multi: true},
    AnnouncingTopic: {Multi: true},
    TopicAnnounced:  {Multi: true},

    // subscription
    RemoveSubscription: {Multi: true},
    AddSubscription:    {Multi: true},

    // misc
    AddRelay:        {Multi: true},
    RemoveRelay:     {Multi: true},
    IncomingRPC:     {Multi: true},
    AddValidator:    {Multi: true},
    RemoveValidator: {Multi: true},
}
```

</details>

<details>

<summary>See states structure and relations (discovery & bootstrap)</summary>

```go
package discovery

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States define relations between states.
var States = am.Struct{
    Start: {
        Add: S{PoolTimer},
    },
    PoolTimer: {},
    RefreshingDiscoveries: {
        Require: S{Start},
    },
    DiscoveriesRefreshed: {
        Require: S{Start},
    },

    // topics

    DiscoveringTopic: {
        Multi: true,
    },
    TopicDiscovered: {
        Multi: true,
    },

    BootstrappingTopic: {
        Multi: true,
    },
    TopicBootstrapped: {
        Multi: true,
    },

    AdvertisingTopic: {
        Multi: true,
    },
    StopAdvertisingTopic: {
        Multi: true,
    },
}

// StatesBootstrapFlow define relations between states for the bootstrap flow.
var StatesBootstrapFlow = am.Struct{
    Start: {
        Add: S{BootstrapChecking},
    },
    BootstrapChecking: {
        Remove: BootstrapGroup,
    },
    DiscoveringTopic: {
        Remove: BootstrapGroup,
    },
    BootstrapDelay: {
        Remove: BootstrapGroup,
    },
    TopicBootstrapped: {
        Remove: BootstrapGroup,
    },
}

// Groups of mutually exclusive states.

var (
    BootstrapGroup = S{DiscoveringTopic, BootstrapDelay, BootstrapChecking, TopicBootstrapped}
)
```

</details>

See
[github.com/pancsta/**go-libp2p-pubsub-benchmark**](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-benchmark)
or the [pdf results](https://github.com/pancsta/go-libp2p-pubsub-benchmark/raw/main/assets/bench.pdf) for more info.

### libp2p-pubsub simulator

![Simulator grafana dashboard](assets/sim-grafana.dark.png#gh-dark-mode-only)
![Simulator grafana dashboard](assets/sim-grafana.light.png#gh-light-mode-only)

- **simulator** `sim` (14 states)<br />
  Root simulator machine, initializes the network and manipulates it during heartbeats according to frequency
  definitions. Heavily dependent on [state negotiation](/docs/manual.md#negotiation-handlers).
- **simulator's peer** - eg `sim-p17` (17 states)<br />
  Handles peer's connections, topics and messages. This machine has a decent amount of [relations](/docs/manual.md#states-relations).
  Each sim peer has its own pubsub host.
- **topics** - eg `sim-t-se7ev` (5 states)<br />
  State-only machine (no handlers, no goroutine). States represent correlations with peer machines.

<details>

<summary>See states structure and relations (simulator)</summary>

```go
package sim

import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
var States = am.Struct{
    Start:     {},
    Heartbeat: {Require: S{Start}},

    // simulation

    AddPeer:       {Require: S{Start}},
    RemovePeer:    {Require: S{Start}},
    AddTopic:      {Require: S{Start}},
    RemoveTopic:   {Require: S{Start}},
    PeakRandTopic: {Require: S{Start}},

    // peer (nested) states

    AddRandomFriend:  {Require: S{Start}},
    GC:               {Require: S{Start}},
    JoinRandomTopic:  {Require: S{Start}},
    JoinFriendsTopic: {Require: S{Start}},
    MsgRandomTopic:   {Require: S{Start}},
    VerifyMsgsRecv:   {Require: S{Start}},

    // metrics

    RefreshMetrics: {Require: S{Start}},
    // TODO history-based metrics, via pairs of counters, possibly using peer histories as well
}
```

</details>

<details>

<summary>See states structure and relations (simulator's peer)</summary>

```go
package sim

import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
var States = am.Struct{
    // Start (sync)
    Start: {},

    // DHT (sync)
    IsDHT: {},

    // Ready (sync auto)
    Ready: {
        Auto:    true,
        Require: S{Start, Connected},
    },

    // IdentityReady (async auto)
    IdentityReady: {Remove: groupIdentityReady},
    GenIdentity: {
        Auto:   true,
        Remove: groupIdentityReady,
    },

    BootstrapsConnected: {},

    // EventHostConnected (sync, external event)
    EventHostConnected: {
        Multi:   true,
        Require: S{Start},
        Add:     S{Connected},
    },

    // Connected (async bool auto)
    Connected: {
        Require: S{Start},
        Remove:  groupConnected,
    },
    Connecting: {
        Auto:    true,
        Require: S{Start, IdentityReady},
        Remove:  groupConnected,
    },
    Disconnecting: {
        Remove: am.SMerge(groupConnected, S{BootstrapsConnected}),
    },

    // TopicJoined (async)
    JoiningTopic: {
        Multi:   true,
        Require: S{Connected},
    },
    TopicJoined: {
        Multi:   true,
        Require: S{Connected},
        Add:     S{FwdToSim},
    },

    // TopicLeft (async)
    LeavingTopic: {
        Multi:   true,
        Require: S{Connected},
    },
    TopicLeft: {
        Multi:   true,
        Require: S{Connected},
        Add:     S{FwdToSim},
    },

    // MsgsSent (async)
    SendingMsgs: {
        Multi:   true,
        Require: S{Connected},
    },
    MsgsSent: {
        Multi:   true,
        Require: S{Connected},
        Add:     S{FwdToSim},
    },

    // call the mirror state in the main Sim machine, prefixed with Peer and peer ID added to Args
    // TODO
    FwdToSim: {},
}

//#region boilerplate defs

// Groups of mutually exclusive states.
var (
    groupConnected = S{Connecting, Connected, Disconnecting}
    //groupStarted       = S{Starting, Started}
    groupIdentityReady = S{GenIdentity, IdentityReady}
)
</details>

<details>

<summary>See states structure and relations (topics)</summary>

```go

package sim

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

type S = am.S

// States map defines relations and properties of states.
var States = am.Struct{
    Stale: {
        Auto: true,
    },
    HasPeers: {},
    Active: {
        Remove: S{Stale},
    },
    Peaking: {
        Remove:  S{Stale},
        Require: S{HasPeers},
    },
}

```

</details>

See
[github.com/pancsta/**go-libp2p-pubsub-benchmark**](https://github.com/pancsta/go-libp2p-pubsub-benchmark?tab=readme-ov-file#libp2p-pubsub-simulator)
for more info.

### am-dbg

am-dbg is a [cview](https://code.rocket9labs.com/tslocum/cview) TUI app with a single machine consisting of:

- input events (7 states)
- external state (11 states)
- actions (14 states)

This machine features a decent amount of relations within a large number of states and 4 state groups. It's also a good
example to see how easily an AM-based program can be controller with a script in [tools/cmd/am-dbg-teaser](tools/cmd/am-dbg-teaser/main_dbg_teaser.go).

<details>

<summary>See states structure and relations</summary>

```go

// States map defines relations and properties of states.
var States = am.Struct{
    ///// Input events

    ClientMsg:       {Multi: true},
    ConnectEvent:    {Multi: true},
    DisconnectEvent: {Multi: true},

    // user scrolling tx / steps
    UserFwd: {
        Add:    S{Fwd},
        Remove: GroupPlaying,
    },
    UserBack: {
        Add:    S{Back},
        Remove: GroupPlaying,
    },
    UserFwdStep: {
        Add:     S{FwdStep},
        Require: S{ClientSelected},
        Remove:  am.SMerge(GroupPlaying, S{LogUserScrolled}),
    },
    UserBackStep: {
        Add:     S{BackStep},
        Require: S{ClientSelected},
        Remove:  am.SMerge(GroupPlaying, S{LogUserScrolled}),
    },

    ///// External state (eg UI)

    // focus group

    TreeFocused:          {Remove: GroupFocused},
    LogFocused:           {Remove: GroupFocused},
    SidebarFocused:       {Remove: GroupFocused},
    TimelineTxsFocused:   {Remove: GroupFocused},
    TimelineStepsFocused: {Remove: GroupFocused},
    MatrixFocused:        {Remove: GroupFocused},
    DialogFocused:        {Remove: GroupFocused},

    StateNameSelected:    {Require: S{ClientSelected}},
    HelpDialog:           {Remove: GroupDialog},
    ExportDialog: {
        Require: S{ClientSelected},
        Remove:  GroupDialog,
    },
    LogUserScrolled: {},
    Ready:           {Require: S{Start}},

    ///// Actions

    Start: {},
    TreeLogView: {
        Auto:   true,
        Remove: GroupViews,
    },
    MatrixView:     {Remove: GroupViews},
    TreeMatrixView: {Remove: GroupViews},
    TailMode: {
        Require: S{ClientSelected},
        Remove:  GroupPlaying,
    },
    Playing: {
        Require: S{ClientSelected},
        Remove:  am.SMerge(GroupPlaying, S{LogUserScrolled}),
    },
    Paused: {
        Auto:    true,
        Require: S{ClientSelected},
        Remove:  GroupPlaying,
    },

    // tx / steps back / fwd

    Fwd: {
        Require: S{ClientSelected},
        Remove:  S{Playing},
    },
    Back: {
        Require: S{ClientSelected},
        Remove:  S{Playing},
    },
    FwdStep: {
        Require: S{ClientSelected},
        Remove:  S{Playing},
    },
    BackStep: {
        Require: S{ClientSelected},
        Remove:  S{Playing},
    },

    ScrollToTx: {Require: S{ClientSelected}},

    // client selection

    SelectingClient: {Remove: S{ClientSelected}},
    ClientSelected: {
        Remove: S{SelectingClient, LogUserScrolled},
    },
    RemoveClient: {Require: S{ClientSelected}},
}

```

</details>

See [tools/debugger/states](tools/debugger/states) for more info.

## Roadmap

- negotiation testers (eg `CanAdd`)
- helpers for composing networks of machines
- helpers for queue and history traversal
- "state-trace" navbar in am-dbg (via `AddFromEv`)
- go1.22 traces
- inference
- optimizations
- manual updated to a spec

See also [issues](https://github.com/pancsta/asyncmachine-go/issues).

## Development

- all PRs welcome
- before
  - `./scripts/dep-taskfile.sh`
  - `task install-deps`
- after
  - `task test`
  - `task format`
  - `task lint`

## Changelog

Latest release: `v0.6.4`

- test\(am-dbg\): add TUI integration tests [\#97](https://github.com/pancsta/asyncmachine-go/pull/97) (@pancsta)
- feat\(machine\): add export / import [\#96](https://github.com/pancsta/asyncmachine-go/pull/96) (@pancsta)
- feat\(am-dbg\): add ssh server [\#95](https://github.com/pancsta/asyncmachine-go/pull/95) (@pancsta)
- feat\(am-dbg\): render guidelines in tree relations [\#94](https://github.com/pancsta/asyncmachine-go/pull/94) (@pancsta)
- refac\(am-dbg\): refac cli apis [\#93](https://github.com/pancsta/asyncmachine-go/pull/93) (@pancsta)
- feat\(am-dbg\): switch compression to brotli [\#92](https://github.com/pancsta/asyncmachine-go/pull/92) (@pancsta)
- feat\(am-dbg\): add Start and Dispose methods [\#91](https://github.com/pancsta/asyncmachine-go/pull/91) (@pancsta)
- feat\(helpers\): add 5 helper funcs, eg Add1Sync, EnvLogLevel [\#90](https://github.com/pancsta/asyncmachine-go/pull/90)
    (@pancsta)

Maintenance release: `v0.5.1`

- fix\(machine\): fix Dispose\(\) deadlock [\#70](https://github.com/pancsta/asyncmachine-go/pull/70) (@pancsta)
- fix\(machine\): allow for nil ctx [\#69](https://github.com/pancsta/asyncmachine-go/pull/69) (@pancsta)
- fix\(am-dbg\): fix tail mode delay [\#68](https://github.com/pancsta/asyncmachine-go/pull/68) (@pancsta)
- fix\(am-dbg\): fix crash for machs with log level 0 [\#67](https://github.com/pancsta/asyncmachine-go/pull/67) (@pancsta)
- feat\(machine\): add WhenQueueEnds [\#65](https://github.com/pancsta/asyncmachine-go/pull/65) (@pancsta)
- docs: update manual to v0.5.0 [\#64](https://github.com/pancsta/asyncmachine-go/pull/64) (@pancsta)

See [CHANELOG.md](/CHANGELOG.md) for the full list.
