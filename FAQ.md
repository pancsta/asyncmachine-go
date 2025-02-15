# FAQ

## How does asyncmachine work?

It calls struct methods according to conventions and currently active states (eg `BarEnter`, `FooFoo`, `FooBar`, `BarState`).

## What is a "state" in asyncmachine?

State as in status / switch / flag, eg "process RUNNING" or "car BROKEN".

## What does "clock-based" mean?

Each state has a counter of activations & de-activations, and all state counters create "machine time".

## What's the difference between states and events?

The same event happening many times will cause only 1 state activation until the state becomes inactive.

## What's the difference between subscribing to events and waiting on states?

Event subscription will tigger as many times as the event, while waiting followed by a new waiting can miss some
"state instances" (those can be infered from the state's clock).

## How does asyncmachine compare to [Temporal](https://github.com/temporalio/temporal)?

Temporal is an all-in-one solution with data persistence, which is its limitation. Asyncmachine doesn't hold any data by
itself and has progressive layers, making it usable in a wide variety of use cases (e.g. asyncmachine could do workflows
for a desktop app).

## How does asyncmachine compare to [Ergo](https://github.com/ergo-services/ergo)?

Ergo is a great framework, but it leans on old ideas and has web-based tooling. It also isn't natively based on state
machines. Asyncmachine provides productivity-focused TUI tooling and rich integrations while having every component
natively state-based (even the [code generator](/tools/generator/states/ss_generator.go)).

## Can asyncmachine be integrated with other frameworks?

Yes, asyncmachine is more of a set of libraries following the same conventions than an actual framework. It can
integrate with anything via state-based APIs.

## Does aRPC auto sync data?

aRPC auto syncs only clock values, which technically is `[n]uint64` (`n` = number of states).

## Does asyncmachine return data?

No, just yes/no/later (`Executed`, `Canceled`, `Queued`).

## Does asyncmachine return errors?

No, but there's an error state (`Exception`) and `Err()` getter. Optionally, there are also detailed error states
(e.g. `ErrNetwork`).

## Why does asyncmachine avoid blocking?

The lack of blocking allows for immediate adjustments to incoming changes and is backed by solid cancellation support.

## Is asyncmachine thread safe?

Yes, all the public methods of `pkg/machine` are thread safe.

## Is asyncmachine single threaded?

The queue executes on the thread which caused the mutation, while handlers execute in separate goroutines each,
later usually forking another goroutine to unblock other handlers.

## What's the origin of asyncmachine?

It was a [research project](https://github.com/TobiaszCudnik/asyncmachine) between 2012 and 2019. [Video demo](http://tobiaszcudnik.github.io/asyncmachine-inspector/sample.mp4).

## How do I do X/Y/Z in asyncmachine?

Usually, the answer is "Make it a state".
