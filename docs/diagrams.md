# Diagrams

* [Machine Basics](#machine-basics)
  * [Multi-state](#multi-state)
  * [Clock and state contexts](#clock-and-state-contexts)
  * [Queue](#queue)
  * [AOP handlers](#aop-handlers)
  * [Negotiation](#negotiation)
  * [Relations](#relations)
  * [Subscriptions](#subscriptions)
* [Transition Lifecycle](#transition-lifecycle)
* [aRPC](#arpc)
  * [aRPC Architecture](#arpc-architecture)
  * [aRPC Sync (Details)](#arpc-sync-details)
  * [aRPC Sync (Clocks)](#arpc-sync-clocks)
  * [aRPC Partial Distribution](#arpc-partial-distribution)
  * [aRPC Handler Mutations](#arpc-handler-mutations)
* [Worker Pool Architecture](#worker-pool-architecture)
* [Flows](#flows)
  * [Legend](#legend)
  * [RPC Getter Flow](#rpc-getter-flow)
  * [Worker Bootstrap](#worker-bootstrap)
* [Examples](#examples)
  * [Tree State Source](#tree-state-source)
  * [Benchmark State Source](#benchmark-state-source)

## Machine Basics

Features are explained using Mermaid flow diagrams, and headers link to relevant sections of the [manual](/docs/manual.md).

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

Queue of mutations enable lock-free [Actor Model](https://en.wikipedia.org/wiki/Actor_model).

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

## Transition Lifecycle

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/tx-lifecycle.d2.dark.svg)

## aRPC

### aRPC Architecture

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc.d2.dark.svg)

### aRPC Sync (Details)

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc-sync-details.d2.dark.svg)

### aRPC Sync (Clocks)

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc-sync-clocks.d2.dark.svg)

### aRPC Partial Distribution

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc-dist-mach.d2.dark.svg)

### aRPC Handler Mutations

![](https://pancsta.github.io/assets/asyncmachine-go/diagrams/arpc-handlers.d2.dark.svg)

## Worker Pool Architecture

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/node-worker-pool.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/node-worker-pool.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/node-worker-pool.mermaid.light.svg">
</picture></div>

## Flows

### Legend

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/flows-legend.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/flows-legend.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/flows-legend.mermaid.light.svg">
</picture></div>

### RPC Getter Flow

Consumer requests payload from a remote worker.

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/flows-rpc-getter.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/flows-rpc-getter.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/flows-rpc-getter.mermaid.light.svg">
</picture></div>

### Worker Bootstrap

[Node Supervisor](/pkg/node/README.md) forks a new worker process for the pool.

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/flows-worker-bootstrap.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/flows-worker-bootstrap.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/flows-worker-bootstrap.mermaid.light.svg">
</picture></div>

## Examples

### [Tree State Source](/examples/tree_state_source/README.md)

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/example-tree-state-source.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/example-tree-state-source.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/example-tree-state-source.mermaid.light.svg">
</picture></div>

### [Benchmark State Source](/examples/benchmark_grpc/README.md)

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/example-bench-state-source.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/example-bench-state-source.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/example-bench-state-source.mermaid.light.svg">
</picture></div>
