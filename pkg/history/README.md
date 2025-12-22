# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/history

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

**/pkg/history** provides mutation history tracking and traversal, which plays an essential role in making informed
decision about state flow. It contains rich [machine time](/docs/manual.md#clock-and-context) information including
subsets for tracked states, various diffs, sums, and also binds mutations to human time. Because of storage constraints,
additional info about transitions is optional. Each history backend has it's own query mechanism, but all implement the
common [`Query`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history#Query) interface.

Another use case for `pkg/history` is resurrecting state machines after a restart, based on ticks and (optionally) a
journal - it happens manually inside the `MachineRestored` state, which is added by the `Machine.Import()` method.
During the import,
[`MachineTick`](/docs/manual.md#clock-and-context) increments by `+1`. [`MachineRecord`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history#MachineRecord)
can optionally store also the full schema and ordered list of state names, to restore dynamic state-machines.

This layer works with both local and network machines via the [Tracer API](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Tracer).
All times are in UTC, methods thread-safe, and IDs deterministic.

### History Backends

- [In-process](#in-process-history)
- [SQL](#sql-history)
- [Key-Value](#key-value-history)
- [Columnar](#columnar-history)

### TODO

- conditions for extra transition info
- proper time and space benchmarks
- more backends (sqlc)
- store time distances

Below are the key APIs, the rest can be found in the [godoc](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history).

```go
type TimeRecord struct {
    // TransitionId is an optional ID of the related [TransitionRecord].
    TransitionId string
    // MutType is a mutation type.
    MutType am.MutationType
    // MTimeSum is a machine time sum after this transition.
    MTimeSum uint64
    // MTimeSum is a machine time sum after this transition for tracked states
    // only.
    MTimeTrackedSum uint64
    // MTimeDiff is a machine time difference for this transition.
    MTimeDiff uint64
    // MTimeDiff is a machine time difference for this transition for tracked
    // states only.
    MTimeTrackedDiff uint64
    // MTimeRecordDiff is a machine time difference since the previous
    // [TimeRecord].
    MTimeRecordDiff uint64
    // HTime is a human time in UTC.
    HTime time.Time
    // MTime is a machine time after this mutation.
    MTimeTracked am.Time
    // MachTick is the machine tick at the time of this transition.
    MachTick uint32
}


type MemoryApi interface {
    // predefined queries

    ActivatedBetween(ctx context.Context, state string, start, end time.Time) bool
    ActiveBetween(ctx context.Context, state string, start, end time.Time) bool
    DeactivatedBetween(ctx context.Context, state string, start, end time.Time) bool
    InactiveBetween(ctx context.Context, state string, start, end time.Time) bool

    // DB queries

    Find(ctx context.Context, inclTx bool, cond Query) (*MemoryRecord, error)

    // converters

    ToTimeRecord(format any) (*TimeRecord, error)
    ToMachineRecord(format any) (*MachineRecord, error)
    ToTransitionRecord(format any) (*TransitionRecord, error)

    // misc

    Machine() am.Api
    Config() Config
    Context() context.Context
    MachineRecord() *MachineRecord
    Dispose() error
}
```

## In-Process History

This is the default backend and uses a simple Go slice.

Pros:

- lightweight
- concurrent reads
- no encode/decode overhead

Cons:

- no persistence
- manual pattern queries via slice indexes
- eats memory

Example:

```go
import amhist "github.com/pancsta/asyncmachine-go/pkg/states/history"

// ...

// var mach *am.Machine
// var ctx context.Context

// start tracking mutations of states A and C, with a 1k limit
cfg := Config{
    TrackedStates: am.S{"A", "C"},
    MaxRecords: 1_000,
}
mem, err := NewMemory(ctx, nil, mach, cfg, onErr)

// mutate A
mach.Add1("A", nil)

// run a query
now := time.Now()
mem.ActivatedBetween(ctx, "A", now.Add(-time.Second), now) // true
```

Benchmark:

```bash
=== RUN   TestTrackMany
    test_hist.go:27: rounds: 50000
    test_hist.go:34: mach: 218.917261ms
    test_hist.go:38: db: 218.971242ms
    test_hist.go:63: query: 219.027448ms
--- PASS: TestTrackMany (0.22s)
PASS
```

## SQL History

The SQL backend uses [GORM](https://gorm.io/), and ships with a [WASM-based SQLite](https://github.com/ncruces/go-sqlite3)
(WAL enabled), although it can be used with any SQL database.

Pros:

- StarTrek-ready, great tooling
- easy pattern queries via `WHERE` and `JOIN`
- can offload data over the network
- concurrent reads
- multiple DB connections

Cons:

- slow startup or provisioning required
- SQLite adds 1-3MBs to the binary size
- slow writes

<div align="center">
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/history-gorm.dark.png">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/history-gorm.light.png">
  <img  alt="SQL Schema Diagram" src="https://pancsta.github.io/assets/asyncmachine-go/history-gorm.light.png">
</picture></div>

Example:

```go
import (
    amhist "github.com/pancsta/asyncmachine-go/pkg/states/history"
    amhistg "github.com/pancsta/asyncmachine-go/pkg/states/history/gorm"
)

// ...

// var mach *am.Machine
// var ctx context.Context

// injected err handler
onErr := func(err error) {
    log.Print(err.Error())
}

// backend and base configs
cfg := Config{
    Config: amhist.Config{
        MaxRecords: 10 ^ 6,
        TrackedStates: am.S{"A", "C"},
    },
    EncJson: true,
}

// create amhist.sqlite
db, err := amhistg.NewSqlite("")
defer db.Close()

mem, err := amhistg.NewMemory(ctx, db, mach, cfg, onErr)

// mutate and query
mach.Add1("A", nil)
now := time.Now()
mem.ActivatedBetween(ctx, "A", now.Add(-time.Second), now) // true
```

Benchmark:

```bash
=== RUN   TestGormTrackMany
    test_hist.go:27: rounds: 50000
    test_hist.go:34: mach: 230.361104ms
    test_hist.go:38: db: 801.976497ms
    test_hist.go:63: query: 802.315625ms
--- PASS: TestGormTrackMany (1.28s)
PASS
```

## Key-Value History

The Key-Value store backend uses [etcd-io/bbolt](https://github.com/etcd-io/bbolt) with [vmihailenco/msgpack](https://github.com/vmihailenco/msgpack)
and writes to a single file. For debugging there's also JSON encoding, with 2x the size.

Pros:

- instant startup
- small binary size
- concurrent reads

Cons:

- abysmal tooling [[0]](https://github.com/devilcove/bboltEdit)[[1]](https://plugins.jetbrains.com/plugin/28440-boltdb-explorer)
- manual pattern queries via cursor scanning
- single DB connection only

Schema:

- `_machines`
- `MyMachId1`
  - `Times`
  - `Transitions`

Example:

```go
import (
    amhist "github.com/pancsta/asyncmachine-go/pkg/states/history"
    amhistb "github.com/pancsta/asyncmachine-go/pkg/states/history/bbolt"
)

// ...

// var mach *am.Machine
// var ctx context.Context

// injected err handler
onErr := func(err error) {
    log.Print(err.Error())
}

// backend and base configs
cfg := Config{
    Config: amhist.Config{
        MaxRecords: 10 ^ 6,
        TrackedStates: am.S{"A", "C"},
    },
    EncJson: true,
}

// create amhist.db
db, err := amhistb.NewDb("")
defer db.Close()

mem, err := amhistb.NewMemory(ctx, db, mach, cfg, onErr)

// mutate and query
mach.Add1("A", nil)
now := time.Now()
mem.ActivatedBetween(ctx, "A", now.Add(-time.Second), now) // true
```

Benchmark:

```bash
=== RUN   TestBboltTrackMany
    test_hist.go:27: rounds: 50000
    test_hist.go:34: mach: 227.534418ms
    test_hist.go:42: db: 249.641323ms
    test_hist.go:66: query: 249.81ms
    bbolt_test.go:116: write time: 156.384972ms
--- PASS: TestBboltTrackMany (0.26s)
PASS
```

## Columnar History

There's an experimental Columnar backend based on [FrostDB](https://github.com/polarsignals/frostdb/) and [Parquet](https://parquet.apache.org/)
in [/pkg/x/history/frostdb](/pkg/x/history/frostdb).

## Documentation

- [api /pkg/history](https://code.asyncmachine.dev/pkg/github.com/pancsta/asyncmachine-go/pkg/history.html)
- [api /pkg/history/gorm](https://code.asyncmachine.dev/pkg/github.com/pancsta/asyncmachine-go/pkg/history/gorm.html)
- [api /pkg/history/bbolt](https://code.asyncmachine.dev/pkg/github.com/pancsta/asyncmachine-go/pkg/history/bbolt.html)
- [godoc /pkg/history](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history)
- [godoc /pkg/history/gorm](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history/gorm)
- [godoc /pkg/history/bbolt](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history/bbolt)
- [web gorm](https://gorm.io/docs/)
- [web bbolt](https://github.com/etcd-io/bbolt)

## Status

Alpha, not semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
