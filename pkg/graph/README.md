# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo-25.png" /> /pkg/history

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a declarative execution model based on AOP, actor model, and state machines.

**/pkg/graph** is a [directional multigraph with edge identity](https://en.wikipedia.org/wiki/Multigraph)
of connected state machines. It's used by the debugging tools (`am-dbg`, `am-vis`) and the `NetworkGraph` based discovery
network. The graph is an essential tool to handle large swarms of state machines.

## Graph

The in-memory graph is based on [ark ECS](https://github.com/mlange-42/ark) (Entity Component System) which gives it scalability and typesafe queries. It
consumes either `/pkg/telemetry/dbg` (large and explicit) or
`/pkg/rpc.MsgServerUpdate` (checksumed diffs). It outputs as MD, DOT, SVG, D2,
Mermaid, and `encoding/gob`. aRPC connections are inferred from machine tags.

## NetworkGraph

**NetworkGraph** is a UDP-based synchronization node for **Graph** instances. It can
form any custom mesh network topology to create the desired redundancy of streams
and sink nodes (cycles allowed). It features basic encryption, but running over
**Wireguard** is advised. It also requires a locally injected `SchemaStore` for all the
observed state machines.

### Features

- UDP diffs with checksums and on-demand full sync
- emit to multiple downstreams
- receive from multiple upstreams
- deduplication via machine time (cycles allowed)
- optional ChaCha20-Poly1305 PSK datagram encryption

```go
cipher, _ := graph.NewCipher(1, psk) // 32-byte key
cipher.AddKey(0, oldPsk)             // optional rotation grace period
ng, _ := graph.NewNetworkGraph(ctx, addr, g,
    graph.NetworkGraphOpts{Cipher: cipher})
```

## DbGraph

**DbGraph** is a swappable DB backend serving over HTTP and WebSocket and
speaking [Graph Query Language](https://en.wikipedia.org/wiki/Graph_Query_Language)
(not GraphQL) in read-only mode. It can be attached to a **NetworkGraph**, which
usually happens on redundant sink nodes.

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
| `MachineListExec` | 38 k | 26278 | 305 |
| `MachineListExecParallel` | 24 k | 41476 | 305 |
| `MachineListHttp` | 246 k | 4070 | 1108 |
| `MachineListHttpParallel` | 53 k | 18978 | 1136 |
| `MachineSerializedHttp` | 110 k | 9080 | 308 |
| `WsQueryJson` | 158 k | 6319 | 1016 |
| `WsQueryMsgpack` | 118 k | 8480 | 768 |
| `WsQueryMsgpackParallel` | 37 k | 27062 | 768 |

#### FrogDB

The [FrogDB](https://github.com/pleiad/frogql) backend uses the official WASM
build translated to Golang via [ncruces/wasm2go](https://github.com/ncruces/wasm2go).
A custom build step is required to download and translate the binary locally.
Behind `frogql` tag, see `task gen-graph-frogql`.

| benchmark | ns/op | tps | allocs/op |
|---|---:|---:|---:|
| `MachineListExec` | 483 k | 2071 | 723 |
| `MachineListExecParallel` | 496 k | 2015 | 723 |
| `MachineListHttp` | 820 k | 1219 | 1527 |
| `MachineListHttpParallel` | 564 k | 1772 | 1527 |
| `MachineSerializedHttp` | 920 k | 1087 | 247 |
| `WsQueryJson` | 645 k | 1549 | 1434 |
| `WsQueryMsgpack` | 595 k | 1680 | 1186 |
| `WsQueryMsgpackParallel` | 554 k | 1804 | 1188 |

#### Native

The native backend simply traverses `/pkg/graph.Graph` using a GQL parser from
**gochickpeas**. It's the most lightweight, but also the slowest.

| benchmark | ns/op | tps | allocs/op |
|---|---:|---:|---:|
| `MachineListExec` | 761 k | 1314 | 6971 |
| `MachineListExecParallel` | 326 k | 3066 | 6971 |
| `MachineListHttp` | 1.09 M | 922 | 7788 |
| `MachineListHttpParallel` | 375 k | 2669 | 7809 |
| `MachineSerializedHttp` | 913 k | 1095 | 6619 |
| `WsQueryJson` | 947 k | 1056 | 7687 |
| `WsQueryMsgpack` | 894 k | 1119 | 7435 |
| `WsQueryMsgpackParallel` | 348 k | 2874 | 7438 |

## HistoryGraph

**HistoryGraph** wraps **Graph** and tracks history via `/pkg/history/gorm` (N-shards) for all machine present in the
graph. **NetworkGraph** supports running
[MemoryApi](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history#MemoryApi) queries over the `/history`
endpoint.

### Topology Examples

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/diagrams/netgraph-ring.d2.dark.svg">
        <img style="min-height: 135px"
            src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/netgraph-ring.d2.dark.svg"
            alt="4-cluster GQL ring diagram" />
    </a>
</div>

Example of clustered **NetworkGraph**s in tree structures with a single sink per cluster. The sink nodes have public
endpoints, but also (as netgraphs) synchronize the graph between each other in a ring. Machine-time backed
de-duplication prevents an infinite sync loop. This is both a suggested topology and a runnable test.

## TODO

- optimizations

## Status

Testing, work in progress, not semantically versioned.

## monorepo

- [`/pkg/integrations/ark`](/pkg/integrations/ark)

[Go back to the monorepo root](/README.md) to continue reading.
