# FAQ

## How does asyncmachine work?

Code is bound to states (graph nodes), not transitions (graph edges in FSMs). When a state "Foo" activates, it checks
the `FooEnter(e)` method first, then runs `FooState(e)`. When it deactivates, checks the `FooExit(e)` method, then runs
`FooEnd(e)`. `FooState(e)` binds context and collects data, then forks background processes bound to that instance of
the "Foo" state. Code executed during a transition is dynamically composed of deactivating and activating states.
Transitions between states are not defined, and many states can be active simultaneously. It should be used to solve 
complexity in time.

## What is a "state" in asyncmachine?

State is a binary ID as in status / switch / flag, eg "process RUNNING" or "car BROKEN".

## What does "clock-based" mean?

Each state has a counter of activations & deactivations, and all state counters create "machine time". These are logical
clocks, and the queue is also (partially) counted.

## What's the difference between states and events?

The same event happening many times will cause only 1 state activation until the state becomes inactive.

## What's the difference between subscribing to events and waiting on states?

Event subscription will tigger as many times as the event, while waiting followed by a new waiting can miss some
"state instances" (those can be inferred from the state's clock).

## Can asyncmachine be integrated with other frameworks?

Yes, asyncmachine is more of a set of libraries built on top of the same engine, not an actual framework. It can
integrate with anything via [state-based APIs](/pkg/machine/README.md#api) or [JSON](/pkg/integrations/README.md).

## What's the overhead?

According to early benchmarks of [libp2p-pubsub ported to asyncmachine v0.5.0](https://github.com/pancsta/go-libp2p-pubsub-benchmark/blob/main/bench.md)
([PDF version](https://raw.githubusercontent.com/pancsta/go-libp2p-pubsub-benchmark/refs/heads/main/assets/bench.pdf)),
the CPU overhead is around 15-20%, while the memory ceiling remains the same.

## Does aRPC auto sync data?

aRPC auto syncs only clock values, which technically is `[n]uint64` (`n` = number of states), plus other minor clocks.

## Does asyncmachine return data?

No, just yes/no/later (`Executed`, `Canceled`, `Queued`). Use channels in mutation args for returning local data and
the `SendPayload` state for aRPC.

## Does asyncmachine return errors?

No, but there's an error state (`Exception`) and the `Err()` getter. Optionally, there are also detailed error states
(e.g. `ErrNetwork`).

## Why does asyncmachine avoid blocking?

The lack of blocking allows for immediate adjustments to incoming changes and is backed by solid cancellation support.

## What's the purpose of asyncmachine?

To create autonomous workflows with organic control flow and stateful APIs, while maintaining total control of the flow.

## Is asyncmachine thread safe?

Yes, all the public methods of `pkg/machine` are thread safe.

## Is asyncmachine single-threaded?

The queue executes on the thread which caused the mutation, until fully drained. The handlers execute serially in
a dedicated goroutine (per machine). Each handler often forks a background goroutine to unblock the next handler. The
amount of active goroutines can be limited with `Machine.PoolFork` per-state, machine, or both. It's a
form of structured concurrency.

## What are the downsides?

High abstraction, boilerplate, reflection, stack traces split into state-traces, reduced type safety, incomplete
documentation, TUI devtools, using a [non-mainstream paradigm](/pkg/machine/README.md#state-oriented-programming).

## What should be a "state"?

Only flow-related nodes - if we don't care about something to be a separate entity-in-time, there's no need to
orchestrate it as a separate state.

## What's the origin of asyncmachine?

It was a [solo-research prototype](https://github.com/TobiaszCudnik/asyncmachine) between 2012 and 2019. There's a
[video demo](http://tobiaszcudnik.github.io/asyncmachine-inspector/sample.mp4).

## How do I do X/Y/Z in asyncmachine?

Usually, the answer is "Make it a state".
