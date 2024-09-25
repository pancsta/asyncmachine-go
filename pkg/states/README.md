# /pkg/states

[-> go back to monorepo /](/README.md)

> [!NOTE]
> **asyncmachine** can transform blocking APIs into controllable state machines with ease. It shares similarities with
> [Ergo's](https://github.com/ergo-services/ergo) actor model, and focuses on distributed workflows like [Temporal](https://github.com/temporalio/temporal).
> It's lightweight and most features are optional.

Repository of common state definitions, used to make state-based API easier to compose and exchange. It's in a very
early stage right now.

## State Sets

- [basic](/pkg/states/ss_basic.go) - absolute basics with some network states

In the future, this package may also provide reusable handlers.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
