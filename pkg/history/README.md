# /pkg/history

[-> go back to monorepo /](/README.md)

**/pkg/history** provides mutation history tracking and traversal. It's in its early stage, but it has a very important
role in making informed decision about state flow. Besides providing a log of changes, it also binds human time to
machine time.

## Basic Usage

```go
// create a history
hist := Track(mach, am.S{"A", "C"}, 0)

// collect some info
hist.ActivatedRecently("A", time.Second) // true
```

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
