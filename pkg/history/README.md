[![go report](https://goreportcard.com/badge/github.com/pancsta/asyncmachine-go)](https://goreportcard.com/report/github.com/pancsta/asyncmachine-go)
[![coverage](https://codecov.io/gh/pancsta/asyncmachine-go/graph/badge.svg?token=B8553BI98P)](https://codecov.io/gh/pancsta/asyncmachine-go)
[![go reference](https://pkg.go.dev/badge/github.com/pancsta/asyncmachine-go.svg)](https://pkg.go.dev/github.com/pancsta/asyncmachine-go)
[![last commit](https://img.shields.io/github/last-commit/pancsta/asyncmachine-go/main)](https://github.com/pancsta/asyncmachine-go/commits/main/)
![release](https://img.shields.io/github/v/release/pancsta/asyncmachine-go)
[![matrix chat](https://matrix.to/img/matrix-badge.svg)](https://matrix.to/#/#room:asyncmachine)

# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/history

[cd /](/README.md)

> [!NOTE]
> **Asyncmachine-go** is an AOP Actor Model library for distributed workflows, built on top of a lightweight state
> machine (nondeterministic, multi-state, clock-based, relational, optionally-accepting, and non-blocking). It has
> atomic transitions, RPC, logging, TUI debugger, metrics, tracing, and soon diagrams.

**/pkg/history** provides mutation history tracking and traversal. It's in an early stage, but it has a very important
role in making informed decision about state flow. Besides providing a log of changes, it also binds human time to
[machine time](/docs/manual.md#clock-and-context).

- [`History.ActivatedRecently(state, duration)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history#History.ActivatedRecently)

## Import

```go
import "github.com/pancsta/asyncmachine-go/pkg/history"
```

## Usage

```go
// create a history
hist := Track(mach, am.S{"A", "C"}, 0)

// mutate
mach.Add1("A", nil)

// run a query
hist.ActivatedRecently("A", time.Second) // true
```

## TODO

- MatchEntries
- StatesActiveDuring
- StatesInactiveDuring
- MaxLimits

## Documentation

- [godoc /pkg/history](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history)

## Status

Testing, not semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
