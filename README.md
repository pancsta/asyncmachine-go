<div align="center">
    <a href="#packages">Packages</a> |
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/examples/README.md">Examples</a> |
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/pkg/machine/README.md">Machine</a> |
    <a href="#case-studies">Case Studies</a> |
    <a href="#documentation">Docs</a> |
    <a href="#development">Dev</a> |
    <a href="#changelog">Changelog</a>
    <br />
</div>

# asyncmachine-go

![TUI Debugger](https://pancsta.github.io/assets/asyncmachine-go/video.gif)

> [!NOTE]
> **monorepo** of asyncmachine-go, a clock-based state machine with a TUI **debugger**, transparent **RPC**, and
> **telemetry** for Otel & Prom.

**asyncmachine** can transform blocking APIs into controllable state machines with ease. It shares similarities with
[Ergo's](https://github.com/ergo-services/ergo) actor model, and focuses on distributed workflows like [Temporal](https://github.com/temporalio/temporal).
It's lightweight and most features are optional.

```go
import am "github.com/pancsta/asyncmachine-go/pkg/machine"
mach := am.New(nil, am.Struct{
    "Foo": {Require: am.S{"Bar"}},
    "Bar": {},
}, nil)
mach.Add1("Foo", nil)
mach.Is1("Foo") // false
```

## Getting Started

To get started, it's recommended to read  [`/pkg/machine`](pkg/machine/README.md) first.

## Packages

- [`/pkg/helpers`](/pkg/helpers/README.md) Useful functions when working with async state machines.
- [`/pkg/history`](/pkg/history/README.md) History tracking and traversal.
- **[`/pkg/machine`](/pkg/machine/README.md)** State machine, the main package. Dependency free and semver compatible.
- **[`/pkg/node`](/pkg/node/README.md)** Work in progress.
- **[`/pkg/rpc`](/pkg/rpc/README.md)** Remote state machine, with the same API as a local one.
- [`/pkg/states`](/pkg/states/README.md) Reusable state definitions.
- **[`/pkg/telemetry`](/pkg/telemetry/README.md)** Telemetry exporters for am-dbg, OpenTelemetry and Prometheus.
- `/pkg/pubsub` Planned.
- **[`/tools/cmd/am-dbg`](/tools/cmd/am-dbg/README.md)** am-dbg is a multi-client TUI debugger.
- [`/tools/cmd/am-gen`](/tools/cmd/am-gen/README.md) am-gen generates states files.
- `/tools/cmd/am-vis` Planned.

## Case Studies

- [scraphouse]() - decentralized web scraping using [/pkg/node]()
- [libp2p PubSub Simulator](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-simulator) - sandbox
  simulator for libp2p-pubsub
- [libp2p PubSub Benchmark](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-benchmark) -
  benchmark of libp2p-pubsub ported to asyncmachine-go
- [am-dbg TUI Debugger](/tools/debugger) - single state machine TUI app

## Documentation

- [FAQ](/FAQ.md)
- [API](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine)
- [diagrams](/docs/diagrams.md)
- [cookbook](/docs/cookbook.md)
- [discussions](https://github.com/pancsta/asyncmachine-go/discussions)
- [manual.md](/docs/manual.md) \| [manual.pdf](https://pancsta.github.io/assets/asyncmachine-go/manual.pdf)
  - [Machine and States](/docs/manual.md#machine-and-states)
      - [Clock and Context](/docs/manual.md#clock-and-context)
      - [Auto States](/docs/manual.md#auto-states)
      - [Categories of States](/docs/manual.md#categories-of-states)
      - ...
  - [Changing State](/docs/manual.md#changing-state)
      - [State Mutations](/docs/manual.md#state-mutations)
      - [Transition Lifecycle](/docs/manual.md#transition-lifecycle)
      - [Target States](/docs/manual.md#calculating-target-states)
      - [Negotiation Handlers](/docs/manual.md#negotiation-handlers)
      - [Final Handlers](/docs/manual.md#final-handlers)
      - ...
  - [Advanced Topics](/docs/manual.md#advanced-topics)
      - [Relations](/docs/manual.md#states-relations)
      - [Queue and History](/docs/manual.md#queue-and-history)
      - [Typesafe States](/docs/manual.md#typesafe-states)
      - ...
  - [Remote Machines](/docs/manual.md#remote-machines)
      - ...
  - [Cheatsheet](/docs/manual.md#cheatsheet)

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

Latest release: `v0.7.0`

- docs: update readmes, manual, cookbook for v0.7 [\#127](https://github.com/pancsta/asyncmachine-go/pull/127) (@pancsta)
- feat: release v0.7 [\#126](https://github.com/pancsta/asyncmachine-go/pull/126) (@pancsta)
- refac\(telemetry\): switch telemetry to Tracer API [\#125](https://github.com/pancsta/asyncmachine-go/pull/125) (@pancsta)
- fix\(machine\): fix test -race [\#124](https://github.com/pancsta/asyncmachine-go/pull/124) (@pancsta)
- feat\(machine\): add Index\(\) and Time.Is\(index\) [\#123](https://github.com/pancsta/asyncmachine-go/pull/123) (@pancsta)
- feat\(machine\): add Eval\(\) detection tool [\#122](https://github.com/pancsta/asyncmachine-go/pull/122) (@pancsta)
- feat\(machine\): add EnvLogLevel [\#121](https://github.com/pancsta/asyncmachine-go/pull/121) (@pancsta)
- feat\(rpc\): add grpc benchmark [\#120](https://github.com/pancsta/asyncmachine-go/pull/120) (@pancsta)
- feat\(machine\): add PanicToErr, PanicToErrState [\#119](https://github.com/pancsta/asyncmachine-go/pull/119) (@pancsta)
- feat\(helpers\): add pkg/helpers \(Add1Block, Add1AsyncBlock, ...\) [\#118](https://github.com/pancsta/asyncmachine-go/pull/118)
  (@pancsta)
- feat\(rpc\): add pkg/rpc [\#117](https://github.com/pancsta/asyncmachine-go/pull/117) (@pancsta)
- feat\(states\): add pkg/states [\#116](https://github.com/pancsta/asyncmachine-go/pull/116) (@pancsta)
- feat\(machine\): add state def manipulations \(eg StateAdd\) [\#115](https://github.com/pancsta/asyncmachine-go/pull/115)
   (@pancsta)
- feat\(machine\): add new Tracer methods \(eg VerifyStates\) [\#114](https://github.com/pancsta/asyncmachine-go/pull/114)
   (@pancsta)
- feat\(rpc\): add rpc tests, including remote machine suite [\#113](https://github.com/pancsta/asyncmachine-go/pull/113)
   (@pancsta)
- feat\(machine\): add SetLoggerSimple, SetLoggerEmpty [\#112](https://github.com/pancsta/asyncmachine-go/pull/112) (@pancsta)
- feat\(machine\): add AddErrState and unified stack traces [\#111](https://github.com/pancsta/asyncmachine-go/pull/111)
   (@pancsta)

Maintenance release: `v0.6.5`

- fix\(am-dbg\): correct timeline tailing [\#110](https://github.com/pancsta/asyncmachine-go/pull/110) (@pancsta)
- fix\(am-dbg\): escape secondary logtxt brackets [\#109](https://github.com/pancsta/asyncmachine-go/pull/109) (@pancsta)
- fix\(am-dbg\): fix filtering in TailMode [\#108](https://github.com/pancsta/asyncmachine-go/pull/108) (@pancsta)
- fix\(am-dbg\): stop playing on timeline jumps [\#107](https://github.com/pancsta/asyncmachine-go/pull/107) (@pancsta)
- fix\(am-dbg\): fix changing log level removed trailing tx [\#106](https://github.com/pancsta/asyncmachine-go/pull/106)
   (@pancsta)
- fix\(am-dbg\): allow state jump after search as type \#100 [\#105](https://github.com/pancsta/asyncmachine-go/pull/105)
   (@pancsta)
- fix\(am-dbg\): align tree rel lines [\#104](https://github.com/pancsta/asyncmachine-go/pull/104) (@pancsta)
- fix\(am-dbg\): fix tree highlights for ref links [\#103](https://github.com/pancsta/asyncmachine-go/pull/103) (@pancsta)

### Changes

- [Full Changelog](CHANGELOG.md)
- [Breaking Changes](BREAKING.md)
- [Roadmap](ROADMAP.md)
