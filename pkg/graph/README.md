# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo-25.png" /> /pkg/history

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a declarative execution model based on AOP, actor model, and state machines.

**/pkg/graph** handles a [directional multigraph](https://en.wikipedia.org/wiki/Multigraph)
of connected state machines. It's mostly used by the debugging tools (`am-dbg`
and `am-vis`), but an experimental `NetGraph` with a swappable DB engine is
being worked on to replace `/pkg/pubsub` and `/pkg/integrations/nats`. The graph
is an essential tool to handle large swarms of state machines.

## Graph

The in-memory graph is based on [github.com/dominikbraun/graph](https://github.com/dominikbraun/graph)
and consumes `/pkg/telemetry/dbg` (large and explicit) or
`/pkg/rpc.MsgServerUpdate` (checksumed diffs). It outputs as MD, DOT, SVG, D2,
Mermaid, and `encoding/gob`. RPC connections are inferred from machine tags.

## NetGraph

**NetGraph** is a UDP-based synchronization node for **Graph** instances. It can
form any custom mesh network topology to create the desired redundancy of streams
and leaf nodes. It does not have a DHT or encryption, so it needs to run over
**Wireguard** for now. It also requires a locally injected `SchemaStore` for all the
observed state machines.

### Features

- UDP diffs with checksums and on-demand full sync
- emit to multiple downstreams
- receive from multiple upstreams
- deduplication via machine time (cycles allowed)

## DbGraph

**DbGraph** is a swappable DB backend serving over HTTP and WebSocket and
speaking [Graph Query Language](https://en.wikipedia.org/wiki/Graph_Query_Language)
(not GraphQL) in read-only mode. It can be attached to a **NetGraph**, which
usually happens on redundant leaf nodes.

Dictionary:

```gql
(:Machine {id, parent, schema_hash, mtime_sum, queue_tick, mach_tick})
(:State   {mach, name, tick, active})
(:Tag     {mach, name, value})
(Machine)-[:HAS]->(State)
(Machine)-[:HAS_TAG]->(Tag)
(Machine)-[:CHILD_OF]->(Machine)
(Machine)-[:CONNECTED_TO]->(Machine)   // rpc conns (src-id tags)
(State)-[:REQUIRE|ADD|REMOVE|AFTER]->(State)
```

Examples:

A machine with an active state and a tag value:

```gql
MATCH (m:Machine)-[:HAS]->(s:State), (m)-[:HAS_TAG]->(t:Tag)
WHERE s.name = 'Available' AND s.active = true AND t.value = 'db'
RETURN m.id AS id
```

A child of a machine with a tag value:

```gql
MATCH (m:Machine)-[:CHILD_OF]->(p:Machine)-[:HAS_TAG]->(pt:Tag)
WHERE p.id = 'somemach' AND pt.value = 'sometag'
RETURN m.id AS id
```

RPC conns (from `src-id` tags):

```gql
MATCH (m:Machine)-[:CONNECTED_TO]->(src:Machine)
RETURN m.id AS id, src.id AS src
```

### Backend

#### gochickpeas

The [gochickpeas](https://github.com/freeeve/gochickpeas) backend is the fastest backend as a native Go port of [RustyChickpeas](https://github.com/freeeve/rustychickpeas).

| benchmark | ns/op | tps | allocs/op |
|---|---:|---:|---:|
| `MachineListExec` | 39 k | 25321 | 299 |
| `MachineListExecParallel` | 24 k | 42022 | 299 |
| `MachineListHttp` | 215 k | 4654 | 1048 |
| `MachineListHttpParallel` | 49 k | 20511 | 1077 |
| `MachineSerializedHttp` | 76 k | 13152 | 226 |
| `WsQueryJson` | 158 k | 6320 | 1010 |
| `WsQueryMsgpack` | 114 k | 8770 | 762 |
| `WsQueryMsgpackParallel` | 40 k | 25169 | 762 |

#### FrogDB

The [FrogDB](https://github.com/pleiad/frogql) backend uses the official WASM
build translated to Golang via [ncruces/wasm2go](https://github.com/ncruces/wasm2go).
A custom build step is required to download and translate the binary locally.
Behind `frogql` tag, see `task gen-graph-frogql`.

| benchmark | ns/op | tps | allocs/op |
|---|---:|---:|---:|
| `MachineListExec` | 489 k | 2045 | 723 |
| `MachineListExecParallel` | 506 k | 1976 | 723 |
| `MachineListHttp` | 942 k | 1062 | 1472 |
| `MachineListHttpParallel` | 571 k | 1752 | 1472 |
| `MachineSerializedHttp` | 887 k | 1127 | 197 |
| `WsQueryJson` | 628 k | 1593 | 1434 |
| `WsQueryMsgpack` | 584 k | 1713 | 1186 |
| `WsQueryMsgpackParallel` | 548 k | 1824 | 1188 |

#### Native

The native backend simply traverses `/pkg/graph.Graph` using a GQL parser from
**gochickpeas**. It's the most lightweight, but also the slowest.

| benchmark | ns/op | tps | allocs/op |
|---|---:|---:|---:|
| `MachineListExec` | 1.17 M | 858 | 5304 |
| `MachineListExecParallel` | 489 k | 2047 | 5304 |
| `MachineListHttp` | 1.29 M | 778 | 6067 |
| `MachineListHttpParallel` | 469 k | 2131 | 6085 |
| `MachineSerializedHttp` | 1.10 M | 911 | 4901 |
| `WsQueryJson` | 1.17 M | 852 | 6020 |
| `WsQueryMsgpack` | 1.12 M | 897 | 5769 |
| `WsQueryMsgpackParallel` | 457 k | 2188 | 5772 |

## TODO

- cyclic and multi-graph edges redundancy tests
- sync time benchmark
- full network simulation with discovery resulting in iroh P2P connections for writes
- optimizations
- UDP encryption

## Status

Alpha, work in progress, not semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
