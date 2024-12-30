# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/history

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a declarative control flow library implementing [AOP](https://en.wikipedia.org/wiki/Aspect-oriented_programming)
> and [Actor Model](https://en.wikipedia.org/wiki/Actor_model) through a **[clock-based state machine](/pkg/machine/README.md)**.

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
