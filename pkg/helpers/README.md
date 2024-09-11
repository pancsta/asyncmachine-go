# /pkg/helpers

[-> go back to monorepo /](/README.md)

Helper functions are a notable mention, as many people may look for synchronous wrappers of async state machine calls.
These assume a single, blocking scenario which is controller with the passed context. [Multi states](/docs/manual.md#multi-states)
are handled automatically.

**Example** - add state `StateNameSelected` and wait until it becomes active

```go
res := amh.Add1Block(ctx, mach, ss.StateNameSelected, am.A{"state": state})
print(mach.Is1(ss.StateNameSelected)) // true
print(res) // am.Executed or am.Canceled, never am.Queued
```

**Example** - wait for `ScrollToTx`, triggered by `ScrollToMutTx`

```go
res := amh.Add1AsyncBlock(ctx, mach, ss.ScrollToTx, ss.ScrollToMutTx, am.A{
    "state": state,
    "fwd":   true,
})
print(mach.Is1(ss.ScrollToTx)) // true
print(res) // am.Executed or am.Canceled, never am.Queued
```

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
