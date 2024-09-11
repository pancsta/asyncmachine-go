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

![TUI Debugger](https://pancsta.github.io/assets/asyncmachine-go/am-dbg-teaser.gif)

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

Latest release: `v0.7.0-pre1`

- ...  (@pancsta)
- ...  (@pancsta)
- ...  (@pancsta)

Maintenance release: `v0.6.5`

- fix\(am-dbg\): ... (@pancsta)
- fix\(am-dbg\): ... (@pancsta)
- fix\(am-dbg\): ... (@pancsta)

Changes:

- [Full Changelog](CHANGELOG.md)
- [Breaking Changes](BREAKING.md)
- [Roadmap](ROADMAP.md)
