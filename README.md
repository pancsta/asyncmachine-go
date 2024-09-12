<div align="center">
  <img src="assets/logo.png" alt="asyncmachine-go logo" height="160">
    <br />
    <br />
    <a href="#packages">Packages</a> |
    <a href="#documentatin">Docs</a> |
    <a href="#case-studies">Case Studies</a> |
    <a href="#development">Dev</a> |
    <a href="#changelog">Changelog</a>
    <br />
    <br />
</div>

# asyncmachine-go

![TUI Debugger](https://pancsta.github.io/assets/asyncmachine-go/video.gif)

This is the monorepo of **asyncmachine-go**, a clock-based state machine for managing complex asynchronous workflows in
an inspectable, safe, and structured way. It features a TUI debugger, transparent RPC, and telemetry for Otel & Prom.

**asyncmachine** can transform blocking APIs into controllable state machines with ease. It shares similarities with
[Ergo's](https://github.com/ergo-services/ergo) actor model, but focuses on workflows like [Temporal](https://github.com/temporalio/temporal).
Unlike both mentioned frameworks, it's lightweight (3k LoC, stdlib-only) and adds features progressively.

```go
mach := am.New(nil, am.Struct{
    "Foo": {Requires: "Bar"},
    "Bar": {},
}, nil)
mach.Add1("Foo")
mach.Is1("Foo") // false
```

<details>

<summary>Expand the full example</summary>

```go
// ProcessingFile -> FileProcessed (1 async and 1 sync state)
package main

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func main() {
    // init the state machine
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

</details>

## Packages

To get started, it's recommended to read  [`/pkg/machine`](pkg/machine/README.md) first.

- **[`/pkg/helpers`](/pkg/helpers/README.md)**<br>
  Useful functions when working with state machines.
- [`/pkg/history`](/pkg/history/README.md)<br>
  History tracking and traversal.
- **[`/pkg/machine`](/pkg/machine/README.md)**<br>
  State machine, this package always gets shipped to production, and is semver compatible.
- **[`/pkg/rpc`](/pkg/rpc/README.md)**<br>
  Clock-based remote state machines, with the same API as local ones.
- [`/pkg/states`](/pkg/states/README.md)<br>
  Repository of common state definitions, so APIs can be more unifed.
- **[`/pkg/telemetry`](/pkg/telemetry/README.md)**<br>
  Telemetry exporters for am-dbg, Open Telemetry and Prometheus.
- `/pkg/pubsub`<br>
  Planned.
- [`/pkg/x/helpers`](/pkg/x/helpers)<br>
  Not-so useful functions when working with state machines.
- **[`/tools/cmd/am-dbg`](/tools/cmd/am-dbg/README.md)**<br>
  am-dbg is a multi-client TUI debugger.
- [`/tools/cmd/am-gen`](/tools/cmd/am-gen/README.md)<br>
  am-gen is useful for bootstrapping states files.

## Case Studies

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

Changes:

- [Full Changelog](CHANGELOG.md)
- [Breaking Changes](BREAKING.md)
- [Roadmap](ROADMAP.md)
