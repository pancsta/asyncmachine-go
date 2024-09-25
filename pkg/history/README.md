# /pkg/history

[-> go back to monorepo /](/README.md)

> [!NOTE]
> **asyncmachine** can transform blocking APIs into controllable state machines with ease. It shares similarities with
> [Ergo's](https://github.com/ergo-services/ergo) actor model, and focuses on distributed workflows like [Temporal](https://github.com/temporalio/temporal).
> It's lightweight and most features are optional.

**/pkg/history** provides mutation history tracking and traversal. It's in its early stage, but it has a very important
role in making informed decision about state flow. Besides providing a log of changes, it also binds human time to
machine time.

- [`History.ActivatedRecently(state, duration)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history#History.ActivatedRecently)

## Import

```go
import "github.com/pancsta/asyncmachine-go/pkg/history"
```

## Usage

```go
// create a history
hist := Track(mach, am.S{"A", "C"}, 0)

// collect some info
hist.ActivatedRecently("A", time.Second) // true
```

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
