[![](https://goreportcard.com/badge/github.com/pancsta/asyncmachine-go)](https://goreportcard.com/report/github.com/pancsta/asyncmachine-go)
[![](https://pkg.go.dev/badge/github.com/pancsta/asyncmachine-go.svg)](https://pkg.go.dev/github.com/pancsta/asyncmachine-go)
![](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/pancsta/c6032233dc1d632732ecdc1a4c119850/raw/loc.json)
![](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/pancsta/c6032233dc1d632732ecdc1a4c119850/raw/loc-pkg.json)
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
    <a href="#development">Dev</a> .
    <a href="#faq">FAQ</a> .
    <a href="#changes">Changes</a>
    <br />
</div>

# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> asyncmachine-go

<div align="center">
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/tools/cmd/am-dbg/README.md">
    <img src="https://github.com/pancsta/assets/blob/main/asyncmachine-go/video-mouse.gif?raw=true" alt="TUI Debugger" /></a>
</div>

> [!NOTE]
> State machines communicate through states.

**asyncmachine-go** is a batteries-included graph control flow library implementing [AOP](https://en.wikipedia.org/wiki/Aspect-oriented_programming)
and [Actor Model](https://en.wikipedia.org/wiki/Actor_model) through a **[clock-based state-machine](/pkg/machine/README.md)**.
It features [atomic transitions](/docs/manual.md#transition-lifecycle), [relations with consensus](/docs/manual.md#relations),
[transparent RPC](/pkg/rpc/README.md), [TUI debugger](/tools/cmd/am-dbg/README.md),
[telemetry](/pkg/telemetry/README.md), [REPL](/tools/cmd/arpc/README.md), [remote workers](/pkg/node/README.md),
and [diagrams](https://github.com/pancsta/asyncmachine-go/pull/216).

As a control flow library, it decides about running of predefined bits of code (transition handlers) - their order and
which ones to run, according to currently active states (flags). Thanks to a [novel state machine](/pkg/machine/README.md),
the number of handlers can be minimized while maximizing scenario coverage. It's lightweight, fault-tolerant by design,
has rule-based mutations, and can be used to target virtually any step-in-time, in any workflow.

**asyncmachine-go** takes care of `context`, `select`, and `panic`, while allowing for graph-structured concurrency
with goroutine cancellation. The history log and relations have vector formats.

It aims to create **autonomous** workflows with **organic** control flow and **stateful** APIs:

- **autonomous** - automatic states, relations, context-based decisions
- **organic** - relations, negotiation, cancellation
- **stateful** - maintaining context, responsive, atomic

Each state represents:

- binary flag
- node with relations
- AOP aspect
- **logical clock**
- subscription topic
- multiple methods
- metric
- trace
- lock
- breakpoint

Besides workflows, it can be used for **stateful applications** of any size - daemons, UIs, configs, bots, firewalls,
synchronization consensus, games, smart graphs, microservice orchestration, robots, contracts, streams, DI containers,
test scenarios, simulators, as well as **"real-time" systems** which rely on instant cancelation.

![diagram](https://github.com/pancsta/assets/blob/main/asyncmachine-go/am-vis.svg?raw=true)

## Stack

The top layers depend on the bottom ones.

<table>
  <tr>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td colspan="1" align=center><a href="pkg/pubsub">PubSub</a></td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
  </tr>

  <tr>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td colspan="3" align=center><a href="pkg/node">Workers</a></td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
  </tr>

  <tr>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td colspan="5" align=center><a href="pkg/rpc">RPC</a></td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
  </tr>

  <tr>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td colspan="7" align=center><a href="pkg/machine/README.md#aop-handlers">Handlers</a></td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
  </tr>

  <tr>
    <td>.</td>
    <td>.</td>
    <td colspan="9" align=center>🐇 <a href="pkg/machine">Machine API</a></td>
    <td>.</td>
    <td>.</td>
  </tr>

  <tr>
    <td>.</td>
    <td colspan="11" align=center><a href="pkg/machine/README.md#relations">Relations</a></td>
    <td>.</td>
  </tr>

  <tr>
    <td colspan="13" align=center><a href="pkg/machine/README.md#multi-state"><b><u>
        States
    </u></b></a></td>
  </tr>
</table>

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
client.WorkerRpc.Worker.Add1(ssW.WorkRequested, Pass(&A{
    Input: 2}))
// WaitFor* wraps select statements
err := amhelp.WaitForAll(ctx, time.Second,
    // post-mutation subscription
    mach.When1(ss.BasicStatesDef.Ready, nil),
    // pre-mutation subscription
    whenPayload)
// check cancellation
if ctx.Err() != nil {
    return // state ctx expired
}
// check error
if err != nil {
    // err state mutation
    client.Mach.AddErr(err, nil)
    return // no err required
}
// client/WorkerPayload and mach/Ready activated
```

<div align="center">
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/tools/cmd/arpc/README.md">
    <img src="https://pancsta.github.io/assets/asyncmachine-go/videos/repl-demo1.gif" alt="REPL" />
    </a>
</div>

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
    *ssrpc.WorkerStatesDef
}
```

All examples and benchmarks can be found in [`/examples`](/examples/README.md).

<div align="center">
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/pkg/telemetry/README.md#open-telemetry-traces">
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/otel-jaeger.dark.png">
  <source media="(prefers-color-scheme: light)" srcset="https://github.com/pancsta/assets/raw/main/asyncmachine-go/otel-jaeger.light.png">
  <img alt="OpenTelemetry traces in Jaeger" src="https://github.com/pancsta/assets/raw/main/asyncmachine-go/otel-jaeger.light.png">
</picture></a></div>

## Getting Started

- 🦾 **[`/pkg/machine`](pkg/machine/README.md)** is the main package
- [`/pkg/node`](pkg/node) shows a high-level usage
- examples in [`/examples`](/examples/README.md) are good for a general grasp
    - with [`/examples/mach_template`](/examples/mach_template) being ready for copy-paste
- [`/docs/manual.md`](/docs/manual.md)
  and [`/docs/diagrams.md`](/docs/diagrams.md) go deeper into implementation details
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

- [`/pkg/graph`](/pkg/graph) Multigraph of interconnected state machines.
- [`/pkg/history`](/pkg/history) History tracking and traversal.
- [`/pkg/integrations`](/pkg/integrations) NATS and other JSON integrations.
- [`/pkg/node`](/pkg/node) Distributed worker pools with supervisors.
- [`/pkg/rpc`](/pkg/rpc) Remote state machines, with the same API as local ones.
- [`/pkg/pubsub`](/pkg/pubsub) Decentralized PubSub based on libp2p gossipsub.

## [Devtools](/tools)

- [`/tools/cmd/am-dbg`](/tools/cmd/am-dbg) Multi-client TUI debugger.
- [`/tools/cmd/am-gen`](/tools/cmd/am-gen) Generates schema files and Grafana dashboards.
- [`/tools/cmd/am-vis`](https://github.com/pancsta/asyncmachine-go/pull/216) Generates diagrams of interconnected state machines.
- [`/tools/cmd/arpc`](/tools/cmd/arpc) Network-native REPL and CLI.

[![dashboard](https://pancsta.github.io/assets/asyncmachine-go/am-dbg-dashboard.png)](/tools/cmd/am-dbg)

## Apps

- [secai](https://github.com/pancsta/secai) AI Agents framework.
- [arpc REPL](/tools/repl) Cobra-based REPL.
- [am-dbg TUI Debugger](/tools/debugger) Single state-machine TUI app.
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

## Goals

- scale up, not down
- defaults work by default
- everything can be traced and debugged
- automation is evolution
- state != data

## Community

- [GitHub discussions](https://github.com/pancsta/asyncmachine-go/discussions)
- [Matrix chat](https://matrix.to/#/#room:asyncmachine)

## Status

Under development, status depends on each package. The bottom layers seem prod grade, the top ones are alpha or testing.

## Development

- before
    - `./scripts/dep-taskfile.sh`
    - `task install-deps`
- after
    - `task test`
    - `task format`
    - `task lint`
    - `task precommit`
- [good first issues](https://github.com/pancsta/asyncmachine-go/issues?q=is%3Aissue%20state%3Aopen%20label%3A%22good%20first%20issue%22)

<div align="center">
    <img src="https://github.com/pancsta/assets/blob/main/asyncmachine-go/video.gif?raw=true" alt="TUI Debugger" />
</div>

## [FAQ](./FAQ.md)

### How does asyncmachine work?

It calls struct methods according to conventions, a schema, and currently active states (eg `BarEnter`, `FooFoo`,
`FooBar`, `BarState`). It tackles nondeterminism by embracing it - like an UDP event stream with structure.

### What is a "state" in asyncmachine?

State is a binary name as in status / switch / flag, eg "process RUNNING" or "car BROKEN".

### What does "clock-based" mean?

Each state has a counter of activations & deactivations, and all state counters create "machine time". These are logical
clocks.

### What's the difference between states and events?

The same event happening many times will cause only 1 state activation, until the state becomes inactive.

The complete FAQ is available at [FAQ.md](/FAQ.md).

## Changes

- [Changelog](CHANGELOG.md)
- [Breaking Changes](BREAKING.md)
- [Roadmap](ROADMAP.md)
