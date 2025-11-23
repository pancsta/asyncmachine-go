# FAQ

## How does asyncmachine work?

It calls struct methods according to conventions, a schema, and currently active states (eg `BarEnter`, `FooFoo`,
`FooBar`, `BarState`). It tackles nondeterminism by embracing it - like an UDP event stream with structure.

## What is a "state" in asyncmachine?

State is a binary name as in status / switch / flag, eg "process RUNNING" or "car BROKEN".

## What does "clock-based" mean?

Each state has a counter of activations & deactivations, and all state counters create "machine time". These are logical
clocks, and the queue is also (partially) counted.

## What's the difference between states and events?

The same event happening many times will cause only 1 state activation until the state becomes inactive.

## What's the difference between subscribing to events and waiting on states?

Event subscription will tigger as many times as the event, while waiting followed by a new waiting can miss some
"state instances" (those can be inferred from the state's clock).

## How does asyncmachine compare to [Temporal](https://github.com/temporalio/temporal)?

Temporal is an all-in-one solution with data persistence, which is its limitation. Asyncmachine doesn't hold any data by
itself and has progressive layers, making it usable in a wide variety of use cases (e.g. asyncmachine could do workflows
for a desktop app).

## How does asyncmachine compare to [Ergo](https://github.com/ergo-services/ergo)?

Ergo is a great framework, but it leans on old ideas and has web-based tooling. It also isn't natively based on state
machines. Asyncmachine provides productivity-focused TUI tooling and rich integrations while having every component
natively state-based (even the [code generator](/tools/generator/states/ss_generator.go)).

## Can asyncmachine be integrated with other frameworks?

Yes, asyncmachine is more of a set of libraries following the same conventions, than an actual framework. It can
integrate with anything via [state-based APIs](/pkg/machine/README.md#api) or [JSON](/pkg/integrations/README.md).

## What's the overhead?

According to early benchmarks of [libp2p-pubsub ported to asyncmachine v0.5.0](https://github.com/pancsta/go-libp2p-pubsub-benchmark/blob/main/bench.md)
([PDF version](https://raw.githubusercontent.com/pancsta/go-libp2p-pubsub-benchmark/refs/heads/main/assets/bench.pdf)),
the CPU overhead is around 15-20%, while the memory ceiling remains the same.

## Does aRPC auto sync data?

aRPC auto syncs only clock values, which technically is `[n]uint64` (`n` = number of states), plus other minor clocks.

## What's the main use case?

Modeling non-deterministic logic at scale.

## Does asyncmachine return data?

No, just yes/no/later (`Executed`, `Canceled`, `Queued`). Use channels in mutation args for returning local data and
the `SendPayload` state for aRPC.

## Does asyncmachine return errors?

No, but there's an error state (`Exception`) and the `Err()` getter. Optionally, there are also detailed error states
(e.g. `ErrNetwork`).

## Why does asyncmachine avoid blocking?

The lack of blocking allows for immediate adjustments to incoming changes and is backed by solid cancellation support.

## What's the purpose of asyncmachine?

The purpose of asyncmachine is a **total** control of the flow, in a declarative and network transparent way.

## Is asyncmachine thread safe?

Yes, all the public methods of `pkg/machine` are thread safe.

## Is asyncmachine single-threaded?

The queue executes on the thread which caused the mutation, until fully drained. The handlers execute serially in
separate goroutines each. Each handler usually forks another goroutine to unblock the next handler. The amount of active
goroutines can be limited with `amhelp.Pool` (`errgroup`) per a state, machine, or both. It's a form of structured
concurrency.

## What should be a "state"?

Only interesting things - if we don't care about something to be a separate entity, there's no need to orchestrate it
as a separate state.

## What's the origin of asyncmachine?

It was a [self-research prototype](https://github.com/TobiaszCudnik/asyncmachine) between 2012 and 2019. There's a
[video demo](http://tobiaszcudnik.github.io/asyncmachine-inspector/sample.mp4).

## How do I do X/Y/Z in asyncmachine?

Usually, the answer is "Make it a state".
