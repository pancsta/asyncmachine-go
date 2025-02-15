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
    <a href="#samples">Samples</a> |
    <a href="#getting-started">Getting Started</a> |
    <a href="#packages">Packages</a> |
    <a href="#case-studies">Case Studies</a> |
    <a href="#documentation">Docs</a> |
    <a href="#community">Community</a> |
    <a href="#development">Status</a> |
    <a href="#development">Dev</a> |
    <a href="#development">FAQ</a> |
    <a href="#changes">Changes</a>
    <br />
</div>

# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> asyncmachine-go

<div align="center">
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/tools/cmd/am-dbg/README.md">
    <img src="https://github.com/pancsta/assets/blob/main/asyncmachine-go/video-mouse.gif?raw=true" alt="TUI Debugger" /></a>
</div>

> [!NOTE]
> State machines communicate through states (mutations, checking, and waiting).

**asyncmachine-go** is a declarative control flow library implementing [AOP](https://en.wikipedia.org/wiki/Aspect-oriented_programming)
and [Actor Model](https://en.wikipedia.org/wiki/Actor_model) through a **[clock-based state machine](/pkg/machine/README.md)**.
It has atomic transitions, relations, [transparent RPC](/pkg/rpc/README.md), [TUI debugger](/tools/cmd/am-dbg/README.md),
[telemetry](/pkg/telemetry/README.md), [workers](/pkg/node/README.md), and [soon diagrams](https://github.com/pancsta/asyncmachine-go/pull/216).

Its main purpose is workflows (in-process or distributed), although it can be used for a wide range of
stateful applications - daemons, UIs, configs, bots, agents, firewalls, consensus algos, etc. **asyncmachine** can
precisely (and transparently) target a specific point in a scenario and easily bring structure to event-based systems.
It takes care of most contexts, `select` statements, and panics.

It aims at creating **autonomous** workflows with **organic** control flow and **stateful** APIs:

- **autonomous** - automatic states, relations, context-based decisions
- **organic** - relations, negotiation, cancellation
- **stateful** - maintaing context, responsive, atomic

[![diagrams](https://github.com/pancsta/assets/blob/main/asyncmachine-go/am-vis.svg?raw=true)](https://github.com/pancsta/asyncmachine-go/pull/216)

## Stack

Top layers depend on the bottom ones.

<table>
  <tr>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td>.</td>
    <td colspan="1" align=center><a href="pkg/pubsub/README.md">PubSub</a></td>
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
    <td colspan="3" align=center><a href="pkg/node/README.md">Workers</a></td>
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
    <td colspan="5" align=center><a href="pkg/rpc/README.md">RPC</a></td>
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
    <td colspan="9" align=center>🦾 <a href="pkg/machine/README.md">Machine API</a></td>
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
mach := am.New(nil, am.Struct{
    "Foo": {Require: am.S{"Bar"}},
    "Bar": {},
}, nil)
mach.Add1("Foo", nil)
mach.Is1("Foo") // false
```

**Complicated** - wait on a multi state (event) and Ready state with a 1s
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
    mach2.When1(ss.BasicStatesDef.Ready, nil),
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
// client/WorkerPayload and mach2/Ready activated
```

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

## Getting Started

🦾 **[`/pkg/machine`](pkg/machine/README.md)** is the main package, while [`/pkg/node`](pkg/node) is
the high-level usage. Examples in [`/examples`](/examples/README.md) are good for a general grasp, while [`/docs/manual.md`](/docs/manual.md)
and [`/docs/diagrams.md`](/docs/diagrams.md) go deeper into implementation details. Reading tests is always a good idea.

## [Packages](/pkg)

This monorepo offers the following importable packages and runnable tools:

- [`/pkg/helpers`](/pkg/helpers/README.md) Useful functions when working with async state machines.
- [`/pkg/history`](/pkg/history/README.md) History tracking and traversal.
- 🦾 **[`/pkg/machine`](/pkg/machine/README.md) State machine, dependency free, semver compatible.**
- [`/pkg/node`](/pkg/node/README.md) Distributed worker pools with supervisors.
- [`/pkg/rpc`](/pkg/rpc/README.md) Remote state machines, with the same API as local ones.
- [`/pkg/states`](/pkg/states/README.md) Reusable state definitions, handlers, and piping.
- [`/pkg/telemetry`](/pkg/telemetry/README.md) Telemetry exporters for metrics, traces, and logs.
- [`/pkg/pubsub`](/pkg/pubsub/README.md) Decentralized PubSub based on libp2p.
- [`/tools/cmd/am-dbg`](/tools/cmd/am-dbg/README.md) Multi-client TUI debugger.
- [`/tools/cmd/am-gen`](/tools/cmd/am-gen/README.md) Generates states files and Grafana dashboards.
- [`/tools/cmd/am-vis`](https://github.com/pancsta/asyncmachine-go/pull/216) Generates diagrams of interconnected state machines.

[![dashboard](https://pancsta.github.io/assets/asyncmachine-go/am-dbg-dashboard.png)](/tools/cmd/am-dbg/README.md)

## Case Studies

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

## Community

- [GH discussions](https://github.com/pancsta/asyncmachine-go/discussions)
- [Matrix chat](https://matrix.to/#/#room:asyncmachine)

## Status

Under development, status depends on each package. The bottom layers seem prod grade, the top ones are alpha or testing.

## Development

- all PRs welcome
- before
    - `./scripts/dep-taskfile.sh`
    - `task install-deps`
- after
    - `task test`
    - `task format`
    - `task lint`

<div align="center">
    <img src="https://github.com/pancsta/assets/blob/main/asyncmachine-go/video.gif?raw=true" alt="TUI Debugger" />
</div>

## [FAQ](./FAQ.md)

### How does asyncmachine work?

It calls struct methods according to conventions and currently active states.

### What is a "state" in asyncmachine?

State as in status / switch / flag, eg "process RUNNING" or "car BROKEN".

### What does "clock-based" mean?

Each state has a counter of activations & de-activations, and all state counters create "machine time".

### What's the difference between states and events?

Same event happening many times will cause only 1 state activation, until the state becomes inactive.

## Changes

- [Changelog](CHANGELOG.md)
- [Breaking Changes](BREAKING.md)
- [Roadmap](ROADMAP.md)
