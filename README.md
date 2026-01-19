[![](https://goreportcard.com/badge/github.com/pancsta/asyncmachine-go)](https://goreportcard.com/report/github.com/pancsta/asyncmachine-go)
[![](https://pkg.go.dev/badge/github.com/pancsta/asyncmachine-go.svg)](https://pkg.go.dev/github.com/pancsta/asyncmachine-go)
[![website](https://img.shields.io/badge/asyncmachine-.dev-blue)](https://asyncmachine.dev)
![](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/pancsta/c6032233dc1d632732ecdc1a4c119850/raw/loc-pkg.json)
![](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/pancsta/c6032233dc1d632732ecdc1a4c119850/raw/loc-tools.json)
![](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/pancsta/c6032233dc1d632732ecdc1a4c119850/raw/tests.json)
![](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/pancsta/c6032233dc1d632732ecdc1a4c119850/raw/tests-pkg.json)
![](https://img.shields.io/github/v/release/pancsta/asyncmachine-go)
[![](https://img.shields.io/github/last-commit/pancsta/asyncmachine-go/main)](https://github.com/pancsta/asyncmachine-go/commits/main/)
[![](https://matrix.to/img/matrix-badge.svg)](https://matrix.to/#/#room:asyncmachine)

<div align="center">
    <a href="#samples">Samples</a> .
    <a href="#getting-started">Getting Started</a> .
    <a href="#packages">Packages</a> .
    <a href="#devtools">Devtools</a> .
    <a href="#apps">Apps</a> .
    <a href="#documentation">Docs</a> .
    <a href="#faq">FAQ</a> .
    <a href="#changes">Changes</a>
    <br />
</div>

# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25" width="25" /> asyncmachine-go

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/lifecycle.dark.png">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/lifecycle.light.png">
  <img alt="OpenTelemetry traces in Jaeger" src="https://pancsta.github.io/assets/asyncmachine-go/lifecycle.light.png">
</picture></div>

> [!NOTE]
> State machines communicate through states.

**asyncmachine-go** is a batteries-included graph control flow library implementing [AOP](https://en.wikipedia.org/wiki/Aspect-oriented_programming)
and [Actor Model](https://en.wikipedia.org/wiki/Actor_model) through a **[clock-based state-machine](/pkg/machine/README.md)**.
It features [atomic transitions](/docs/manual.md#transition-lifecycle), [relations with consensus](/docs/manual.md#relations),
[transparent RPC](/pkg/rpc/README.md), [TUI debugger](/tools/cmd/am-dbg/README.md),
[telemetry](/pkg/telemetry/README.md), [REPL](/tools/cmd/arpc/README.md), [selective distribution](/pkg/rpc/README.md#selective-distribution),
[remote workers](/pkg/node/README.md), and [diagrams](/tools/cmd/am-vis).

As a control flow library, it decides about running of predefined bits of code (transition handlers) - their order and
which ones to run, according to currently active states (flags). Thanks to a [novel state machine](/pkg/machine/README.md),
the number of handlers can be minimized while maximizing scenario coverage. It's lightweight, fault-tolerant by design,
has rule-based mutations, and can be used to target virtually *any* step-in-time, in *any* workflow. It's a low-level
tool with acceptable performance.

**asyncmachine-go** takes care of `context`, `select`, and `panic`, while allowing for graph-structured concurrency
with [goroutine cancelation](https://github.com/pancsta/asyncmachine-go/pull/261). The history log and relations have
vector formats. It aims to create **autonomous** workflows with **organic** control flow and **stateful** APIs.

<div align="center" class="video">
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/tools/cmd/am-dbg/README.md">
        <img style="min-height: 820px"
            src="https://github.com/user-attachments/assets/84613447-87dc-48da-9e76-7b4c76705fd3"
            alt="TUI Debugger" />
    </a>
</div>

#### *Each state represents*

- binary flag
- node with relations
- AOP aspect
- ***logical clock***
- subscription topic
- multiple methods
- metric
- trace
- lock
- breakpoint

Besides the main use-case of **workflows**, it can be used for **stateful applications of any size** - daemons, UIs,
configs, bots, firewalls, synchronization consensus, games, smart graphs, microservice orchestration, robots, contracts,
streams, DI containers, test scenarios, simulators, as well as **"real-time" systems** which rely on instant
cancelation.

<div align="center">
    <a href="https://github.com/pancsta/assets/blob/main/asyncmachine-go/am-vis.svg?raw=true">
        <img style="min-height: 263px"
            src="https://pancsta.github.io/assets/asyncmachine-go/am-vis.svg?raw=true" alt="am-vis" />
    </a>
</div>

> [!NOTE]
> Flow is state, and state is flow, in a graph.

## Samples

**Minimal** - an untyped definition of 2 states and 1 relation, then 1 mutation and a check.

```go
import am "github.com/pancsta/asyncmachine-go/pkg/machine"
// ...
mach := am.New(nil, am.Schema{
    "Foo": {Require: am.S{"Bar"}},
    "Bar": {},
}, nil)
mach.Add1("Foo", nil)
mach.Is1("Foo") // false
```

**Complicated** - wait on a multi state (event) and the Ready state with a 1s
timeout, then mutate with typed args, on top of a state context.

```go
// state ctx is an expiration ctx
ctx := client.Mach.NewStateCtx(ssC.WorkerReady)
// clock-based subscription
whenPayload := client.Mach.WhenTicks(ssC.WorkerPayload, 1, ctx)
// state mutation
client.RpcWorker.NetMach.Add1(ssW.WorkRequested, Pass(&A{
    Input: 2}))
// WaitFor* wraps select statements
err := amhelp.WaitForAll(ctx, time.Second,
    // post-mutation subscription
    mach.When1(ss.BasicStatesDef.Ready, nil),
    // pre-mutation subscription
    whenPayload)
// check cancelation
if ctx.Err() != nil {
    return // state ctx expired
}
// check error
if err != nil {
    // error state mutation
    client.Mach.AddErr(err, nil)
    return // no err required
}
// client/WorkerPayload and mach/Ready activated
```

<div align="center">
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/tools/cmd/arpc/README.md">
        <img src="https://pancsta.github.io/assets/asyncmachine-go/videos/repl-demo1.gif"
            alt="REPL" />
    </a>
</div>

> [!NOTE]
> Clock-based navigation in time.

**Handlers** - [Aspect Oriented](https://en.wikipedia.org/wiki/Aspect-oriented_programming) transition handlers.

```go
// can Foo activate?
func (h *Handlers) FooEnter(e *am.Event) bool {
    return true
}
// with Foo active, can Bar activate?
func (h *Handlers) FooBar(e *am.Event) bool {
    return true
}
// Foo activates
func (h *Handlers) FooState(e *am.Event) {
    h.foo = NewConn()
}
// Foo de-activates
func (h *Handlers) FooEnd(e *am.Event) {
    h.foo.Close()
}
```

**Schema** - states of a [node worker](/pkg/node/README.md).

```go
type WorkerStatesDef struct {
    ErrWork        string
    ErrWorkTimeout string
    ErrClient      string
    ErrSupervisor  string

    LocalRpcReady     string
    PublicRpcReady    string
    RpcReady          string
    SuperConnected    string
    ServeClient       string
    ClientConnected   string
    ClientSendPayload string
    SuperSendPayload  string

    Idle          string
    WorkRequested string
    Working       string
    WorkReady     string

    // inherit from rpc worker
    *ssrpc.NetMachStatesDef
}
```

All examples and benchmarks can be found in [`/examples`](/examples/README.md).

<div align="center">
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/pkg/telemetry/README.md#open-telemetry-traces">
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/otel-jaeger.dark.png">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/otel-jaeger.light.png">
  <img alt="OpenTelemetry traces in Jaeger" src="https://pancsta.github.io/assets/asyncmachine-go/otel-jaeger.light.png">
</picture>
</a></div>

## Getting Started

- 🦾 **[`/pkg/machine`](pkg/machine/README.md)** is the main package
- [`/docs/diagrams.md`](/docs/diagrams.md) try to explain things visually
- [`/pkg/node`](pkg/node) shows a high-level usage
- examples in [`/examples`](/examples/README.md) are good for a general grasp
    - with [`/examples/mach_template`](/examples/mach_template) being ready for copy-paste
- [`/docs/manual.md`](/docs/manual.md) is the go-to
- [`/tools/cmd/am-gen`](/tools/cmd/am-gen) will bootstrap
- [`/tools/cmd/am-dbg`](/tools/cmd/am-dbg) will record every detail
- and [reading tests](https://github.com/search?q=repo%3Apancsta%2Fasyncmachine-go+path%3A%2F.*_test.go%2F&type=code)
  is always a good idea

## [Packages](/pkg)

This monorepo offers the following importable packages, especially:

- 🦾 **[`/pkg/machine`](/pkg/machine) State machine, dependency free, semver compatible.**
- [`/pkg/states`](/pkg/states) Reusable state schemas, handlers, and piping.
- [`/pkg/helpers`](/pkg/helpers) Useful functions when working with async state machines.
- [`/pkg/telemetry`](/pkg/telemetry) Telemetry exporters for dbg, metrics, traces, and logs.

Other packages:

- [`/pkg/rpc`](/pkg/rpc) Remote state machines, with the same API as local ones.
- [`/pkg/history`](/pkg/history) History tracking and traversal, including Key-Value and SQL.
- [`/pkg/integrations`](/pkg/integrations) Integrations with NATS and JSON.
- [`/pkg/graph`](/pkg/graph) Directional multigraph of connected state machines.
- [`/pkg/node`](/pkg/node) Distributed worker pools with supervisors.
- [`/pkg/pubsub`](/pkg/pubsub) Decentralized PubSub based on libp2p gossipsub.

## [Devtools](/tools)

- [`/tools/cmd/am-dbg`](/tools/cmd/am-dbg) Multi-client TUI debugger.
- [`/tools/cmd/am-gen`](/tools/cmd/am-gen) Generates schema files and Grafana dashboards.
- [`/tools/cmd/arpc`](/tools/cmd/arpc) Network-native REPL and CLI.
- [`/tools/cmd/am-vis`](/tools/cmd/am-vis) Generates D2 diagrams.
- [`/tools/cmd/am-relay`](/tools/cmd/am-relay) Rotates logs.

<div align="center">
    <a href="/tools/cmd/am-dbg/README.md">
        <img src="https://pancsta.github.io/assets/asyncmachine-go/am-dbg-dashboard.png"
            alt="am-dbg">
    </a>
</div>

> [!NOTE]
> Inspecting cause-and-effect in distributed systems.

## Apps

- [secai](https://github.com/pancsta/secai) AI Agents framework.
- [arpc REPL](/tools/repl) Cobra-based REPL.
- [am-dbg TUI Debugger](/tools/debugger) Single state-machine TUI app.
- [libp2p PubSub Simulator](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-simulator) Sandbox
  simulator for libp2p-pubsub.
- [libp2p PubSub Benchmark](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-benchmark)
  Benchmark of libp2p-pubsub ported to asyncmachine-go.

## Documentation

- API: [go.dev](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine) / [code.asyncmachine.dev](https://code.asyncmachine.dev)
- [diagrams](/docs/diagrams.md) / [cookbook](/docs/cookbook.md)
- [manual MD](/docs/manual.md) / [manual PDF](https://pancsta.github.io/assets/asyncmachine-go/manual.pdf)
    - [Machine and States](/docs/manual.md#machine-and-states)
    - [Changing State](/docs/manual.md#changing-state)
    - [Advanced Topics](/docs/manual.md#advanced-topics)
    - [Cheatsheet](/docs/manual.md#cheatsheet)

## Goals

- scale up, not down
- defaults work by default
- everything can be traced and debugged
- automation is evolution
- state != data

## Community

- [GitHub discussions](https://github.com/pancsta/asyncmachine-go/discussions)
- [Matrix chat](https://matrix.to/#/#room:asyncmachine)
- [Author's Feed](https://blogic.tech/feed)

<div align="center">
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://asyncmachine.dev/chart_dark.png?nocache">
  <source media="(prefers-color-scheme: light)" srcset="https://asyncmachine.dev/chart_light.png?nocache">
  <img alt="OpenTelemetry traces in Jaeger" src="https://asyncmachine.dev/chart_light.png?nocache">
</picture></div>

> [!NOTE]
> Hundreds of clones.

## Status

Under development, status depends on each package. The bottom layers seem prod grade, the top ones are alpha or testing.

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/grafana.dark.png">
        <img style="min-height: 397px"
            src="https://pancsta.github.io/assets/asyncmachine-go/grafana.dark.png"
            alt="grafana dashboard" />
    </a>
</div>

> [!NOTE]
> Managing distributed concurrency.

## Development

- [good first issues](https://github.com/pancsta/asyncmachine-go/issues?q=is%3Aissue%20state%3Aopen%20label%3A%22good%20first%20issue%22)
- before
    - `./scripts/dep-taskfile.sh`
    - `task install-deps`
- after
    - `task test`
    - `task format`
    - `task lint`
    - `task precommit`

### Roadmap

- more tooling
- bug fixes and optimizations
- [ROADMAP.md](/ROADMAP.md)

## [FAQ](/FAQ.md)

### How does asyncmachine work?

It calls struct methods according to conventions, a schema, and currently active states (eg `BarEnter`, `FooFoo`,
`FooBar`, `BarState`). It tackles nondeterminism by embracing it - like an UDP event stream with structure.

### What is a "state" in asyncmachine?

State is a binary ID as in status / switch / flag, eg "process RUNNING" or "car BROKEN".

### What does "clock-based" mean?

Each state has a counter of activations & deactivations, and all state counters create "machine time". These are logical
clocks, and the queue is also (partially) counted.

### What's the difference between states and events?

The same event happening many times will cause only 1 state activation, until the state becomes inactive.

The complete FAQ is available at [FAQ.md](/FAQ.md).

## Changes

- [Changelog](/CHANGELOG.md)
- [Breaking Changes](/BREAKING.md)
- [Repo Traffic](https://github.com/pancsta/asyncmachine-go/pulse)
- [Release Feed](https://github.com/pancsta/asyncmachine-go/releases.atom)

<div align="center">
    <img src="https://pancsta.github.io/assets/asyncmachine-go/video.gif?raw=true"
        alt="TUI Debugger" />
</div>

> [!NOTE]
> Don't lose your sync.
