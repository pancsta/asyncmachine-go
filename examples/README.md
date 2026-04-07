# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /examples

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a pathless control-flow graph with a consensus (AOP, actor model, state-machine).

- [Examples](#examples)
  - [**aRPC**](/examples/arpc)
  - [**Basic**](/examples/basic)
  - [**CLI**](/examples/cli)
  - [**CLI Daemon**](/examples/cli_daemon)
  - [**Mach Template**](/examples/mach_template/mach_template.go)
  - [**Tree State Source**](/examples/tree_state_source)
  - [**TUI**](/examples/tui)
  - [**WASM**](/examples/wasm)
  - [**WASM Workflow**](/examples/wasm_workflow)
  - [DAG Dependency Graph](/examples/dag_dependency_graph/dependency_graph.go)
  - [Fan Out Fan In](/examples/fan_out_in/example_fan_out_in.go)
  - [FSM - Finite State Machine](/examples/fsm/fsm_test.go)
  - [NFA - Nondeterministic Finite Automaton](/examples/nfa/nfa_test.go)
  - [PATH Watcher](/examples/path_watcher/watcher.go)
  - [Pipes](/examples/pipes/example_pipes.go)
  - [Raw Strings](/examples/raw_strings/raw_strings.go)
  - [Relations Playground](/examples/relations_playground/relations_playground.go)
  - [REPL](/examples/repl)
  - [Subscriptions](/examples/subscriptions/example_subscriptions.go)
  - [Temporal Expense Workflow](/examples/temporal_expense/expense_test.go)
  - [Temporal FileProcessing Workflow](/examples/temporal_fileprocessing/fileprocessing.go)
- [Benchmarks](#benchmarks)
  - [**Benchmark State Source**](/examples/benchmark_state_source)
  - [Benchmark gRPC](/examples/benchmark_grpc)
  - [Benchmark libp2p PubSub](/examples/benchmark_libp2p_pubsub)
- [Demos](#demos)
  - [`am-dbg` Debugger](#am-dbg-debugger)
- [Apps](#apps)

## Examples

Headers link more info about each example.

### [aRPC](/examples/arpc)

- `#rpc #client #server`
- [origin](/pkg/rpc/README.md)

### [Basic](/examples/basic)

- `#relations #handlers #async #negotiation #auto`

### [CLI](/examples/cli)

- `#template #relations #handlers`

### [CLI Daemon](/examples/cli_daemon)

- `#template #relations #handlers #arpc #payload`

### [Mach Template](/examples/mach_template/mach_template.go)

- `#template #handlers #telemetry #repl #relations #generator #negotiation #multi`

### [Tree State Source](/examples/tree_state_source)

- `#relations #negotiation #auto #arpc #otel #metrics #grafana #generator #data`
- [origin](/pkg/rpc/README.md)

### [TUI](/examples/tui)

- `#relations #handlers #cview #ui #global-handlers`

### [WASM](/examples/wasm)

- `#relations #handlers #arpc-handlers #arpc #relay #ui #websocket`

### [WASM Workflow](/examples/wasm_workflow)

- `#relations #handlers #arpc-handlers #arpc #relay #websocket #webworkers #payload`

### [DAG Dependency Graph](/examples/dag_dependency_graph/dependency_graph.go)

- `#relations #handlers #async #auto #dependency-graph`

### [Fan Out Fan In](/examples/fan_out_in/example_fan_out_in.go)

- `#relations #handlers #async #auto #concurrency`

### [FSM - Finite State Machine](/examples/fsm/fsm_test.go)

- `#relations #handlers #negotiation #auto`
- [origin](https://en.wikipedia.org/wiki/Finite-state_machine)

### [NFA - Nondeterministic Finite Automaton](/examples/nfa/nfa_test.go)

- `#relations #handlers #async #multi`
- [origin](https://en.wikipedia.org/wiki/Nondeterministic_finite_automaton)

### [PATH Watcher](/examples/path_watcher/watcher.go)

- `#relations #handlers #async #negotiation #multi`
- [origin](https://github.com/pancsta/sway-yasm/)

### [Pipes](/examples/pipes/example_pipes.go)

- `#handlers #composition`
- [origin](/pkg/states/README.md#piping)

### [Raw Strings](/examples/raw_strings/raw_strings.go)

- `#relations #handlers #async #negotiation #auto`
- [origin](/pkg/machine/README.md#raw-strings)

### [Relations Playground](/examples/relations_playground/relations_playground.go)

- `#relations`
- [origin](/pkg/machine/README.md#mutations-and-relations)

### [REPL](/examples/repl)

- `#arpc #repl`
- [origin](/tools/cmd/arpc/README.md)

### [Subscriptions](/examples/subscriptions/example_subscriptions.go)

- `#waiting`
- [origin](/pkg/machine/README.md#waiting)

### [Temporal Expense Workflow](/examples/temporal_expense/expense_test.go)

- `#relations #handlers #async #negotiation #auto #temporal`
- [origin](https://github.com/temporalio/samples-go/blob/main/expense/)

### [Temporal FileProcessing Workflow](/examples/temporal_fileprocessing/fileprocessing.go)

- `#relations #handlers #async #auto #temporal`
- [origin](https://github.com/temporalio/samples-go/blob/main/fileprocessing/)
- [Asynq worker version](/examples/asynq_fileprocessing/fileprocessing_task.go)

## Benchmarks

### [Benchmark State Source](/examples/benchmark_state_source)

- `#docker #go-wrt #caddy`
- [origin](/examples/tree_state_source)

### [Benchmark gRPC](/examples/benchmark_grpc)

- `#relations #handlers #negotiation #arpc #grpc`
- [origin](/pkg/rpc/README.md#benchmark-arpc-vs-grpc)

### [Benchmark libp2p PubSub](/examples/benchmark_libp2p_pubsub)

- `#relations #handlers #async #negotiation #libp2p`

## Demos

- [RPC integration tests tutorial](/pkg/rpc/HOWTO.md)
- [Jaeger traces JSON file](https://pancsta.github.io/assets/asyncmachine-go/bench-jaeger-3h-10m.traces.json)

### `am-dbg` Debugger

Interactively use the [TUI debugger](/tools/cmd/am-dbg/README.md) with data pre-generated by a [secai bot](https://github.com/pancsta/secai):

```bash
go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest \
  --import-data https://pancsta.github.io/assets/asyncmachine-go/am-dbg-exports/secai-cook.gob.br \
  mach://cook
```

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

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
