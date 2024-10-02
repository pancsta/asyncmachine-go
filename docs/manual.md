# asyncmachine-go

<!-- TOC -->

- version `v0.7.0`
- [Introduction](#introduction)
  - [Comparison](#comparison)
  - [Considerations](#considerations)
  - [Legend](#legend)
- [Machine and States](#machine-and-states)
  - [Defining States](#defining-states)
  - [Asynchronous States](#asynchronous-states)
  - [Machine Init](#machine-init)
  - [Clock and Context](#clock-and-context)
  - [Active States](#active-states)
  - [Inspecting States](#inspecting-states)
  - [Auto States](#auto-states)
  - [Multi States](#multi-states)
  - [Categories of States](#categories-of-states)
- [Changing State](#changing-state)
  - [State Mutations](#state-mutations)
  - [Mutation Arguments](#mutation-arguments)
  - [Transition Lifecycle](#transition-lifecycle)
  - [Transition Handlers](#transition-handlers)
  - [Self Handlers](#self-handlers)
  - [Defining Handlers](#defining-handlers)
  - [Event Struct](#event-struct)
  - [Calculating Target States](#calculating-target-states)
  - [Negotiation Handlers](#negotiation-handlers)
  - [Final Handlers](#final-handlers)
  - [Global Handlers](#global-handler)
- [Advanced Topics](#advanced-topics)
  - [Relations](#states-relations)
    - [`Add` relation](#add-relation)
    - [`Remove` relation](#remove-relation)
    - [`Require` relation](#require-relation)
    - [`After` relation](#after-relation)
  - [Waiting](#waiting)
  - [Error Handling](#error-handling)
  - [Catching Panics](#catching-panics)
    - [Panic in a negotiation handler](#panic-in-a-negotiation-handler)
    - [Panic in a final handler](#panic-in-a-final-handler)
    - [Panic anywhere else](#panic-anywhere-else)
  - [Queue and History](#queue-and-history)
  - [Logging](#logging)
    - [Customizing Logging](#customizing-logging)
  - [Debugging](#debugging)
    - [Steps To Debug](#steps-to-debug)
    - [Enabling Telemetry](#enabling-telemetry)
  - [Typesafe States](#typesafe-states)
  - [Tracing and Metrics](#tracing-and-metrics)
  - [Optimizing Data Input](#optimizing-data-input)
- [Remote Machines](#remote-machines)
  - [Server](#server)
  - [Client](#client)
- [Other packages](#other-packages)
  - [Helpers](#helpers)
- [Cheatsheet](#cheatsheet)
- [Other sources](#other-sources)

<!-- TOC -->

## Introduction

> **asyncmachine-go** is a general purpose state machine for managing complex asynchronous workflows in a safe and
> structured way

**asyncmachine-go** abstracts everything and all (but only if necessary) as a state. Many states can be active at the same
time, some of them can mutually exclude each other, some can be divided into several smaller states, some states can be
nested sub-machines, and finally, some states can aggregate groups of nested sub-machines.

The purpose of **asyncmachine-go** is never to block (ie always be synchronous), which is achieved by splitting long-running
actions into steps and orchestrating blocking calls according to a predefined convention. Another goal is to give
structure to non-determinism, by embracing it.

### Comparison

Common differences from other state machines:

- many [states](#defining-states) can be [active](#checking-active-states) at the same time
- [events](#categories-of-states) are just states
- [transitions](#transition-lifecycle) between all the states are allowed
- states are connected by [relations](#states-relations)
- every transition can be [rejected](#transition-lifecycle)
- every state has a [clock value](#state-clocks-and-context)
- [error](#error-handling) is a state

### Considerations

These considerations will help to better understand how **asyncmachine** works:

- it doesn't hold any data other than
  - machine time
  - last error
  - queued mutations
  - state structure (given)
- only **state** can be trusted (e.g. [`Is()`], [`Not()`], [`Any()`])
- mutations resulting in async states need to be waited on (eg [`When()`], [`WhenTime()`])
- flow can be redirected or constrained by checking the current and previous **state** within handlers
- **state** is just an uint64 slice ([`am.Time{0,1,0,2}`])
- only synchronous handlers are safe, but one can use [`Eval()`] from the outside

### Legend

Examples here use a string representations of state machines in the format of [`(ActiveState:\d)[InactiveState:\d]`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.StringAll)
, eg `(Foo:1)[Bar:0 Baz:0]`. Variables with state machines are called `mach` and [pkg/machine](/pkg/machine)
aliased as `am`.

## Machine and States

### Defining States

**States** are defined using [`am.Struct`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Struct),
a string-keyed map of [`am.State` struct](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#State),
which consists of **properties and [relations](#states-relations)**. List of **state names** have a readability shorthand
of [`am.S`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#S)
and can be combined using [`am.SMerge`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#SMerge).

```go
am.Struct{
    "StateName": {

        // properties
        Auto:    true,
        Multi:   true,

        // relations
        Require: am.S{"AnotherState1"},
        Add:     am.S{"AnotherState2"},
        Remove:  am.S{"AnotherState3", "AnotherState4"},
        After:   am.S{"AnotherState2"},
    }
}
```

State names have a predefined **naming convention** which is `CamelCase`.

**Example** - synchronous state

```go
Ready: {},
```

### Asynchronous States

If a state represents a change from `A` to `B`, then it's considered as an **asynchronous state**. Async states can be
represented **by 2 to 4 states**, depending on how granular information we need from them. More than 4
states representing a single abstraction in time is called a Flow.

**Example** - asynchronous state (double)

```go
DownloadingFile: {
    Remove: groupFileDownloaded,
},
FileDownloaded: {
    Remove: groupFileDownloaded,
},
```

**Example** - asynchronous boolean state (triple)

```go
Connected: {
    Remove:  groupConnected,
},
Connecting: {
    Remove:  groupConnected,
},
Disconnecting: {
    Remove: groupConnected,
},
```

**Example** - full asynchronous boolean state (quadruple)

```go
Connected: {
    Remove:  groupConnected,
},
Connecting: {
    Remove:  groupConnected,
},
Disconnecting: {
    Remove: groupConnected,
},
Disconnected: {
    Auto: true,
    Remove: groupConnected,
},
```

### Machine Init

There are two ways to initialize a machine - using
[`am.New`](https://github.com/pancsta/asyncmachine-go/pkg/machine/machine.go#New) or [`am.NewCommon`](https://github.com/pancsta/asyncmachine-go/pkg/machine/machine.go#NewCommon)
. The former one always returns an instance of
[`Machine`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine), but it's limited to only
initializing the states structure and basic customizations via
[`Opts`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Opts). The latter one is more feature-rich
and provides [states verification](#typesafe-states), [handler binding](#transition-handlers), and
[debugging](#debugging). It may also return an error.

```go
import am "github.com/pancsta/asyncmachine-go/pkg/machine"
// ...
ctx := context.Background()
states := am.Struct{"Foo":{}, "Bar":{}}
mach := am.New(ctx, states, &am.Opts{
    ID: "foo1",
    LogLevel: am.LogChanges,
})
```

Each machine has an [ID](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#Machine.ID) (via [`Opts.ID`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#Opts.ID)
or a random one) and the build-in [Exception](#error-handling) state.

### Clock and Context

**Every state has a tick value**, which
increments ("ticks") every time a state gets activated or de-activated. **Odd ticks mean active, while even ticks mean
inactive**. A list (slice) of state ticks forms a **machine clock**. The sum of all the state ticks represents the
current **machine time**.

Machine clock is a [logical clock](https://en.wikipedia.org/wiki/Logical_clock), which purpose is to distinguish
different instances of the same state. It's most commonly used by in the form of `context.Context` via
[`Machine.NewStateCtx(state string)`](https://github.com/pancsta/asyncmachine-go/pkg/machine/machine.go#NewStateCtx),
but it also provides methods on its own data type [`am.Time`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Time).

Other related methods and functions:

- [`Machine.Clock(state string) uint64`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Clock)
- [`Machine.Time(states S) Time`](https://github.com/pancsta/asyncmachine-go/pkg/machine/machine.go#Time)
- [`Machine.TimeSum(states S) Time`](https://github.com/pancsta/asyncmachine-go/pkg/machine/machine.go#TimeSum)
- [`Machine.IsTime(time Time) bool`](https://github.com/pancsta/asyncmachine-go/pkg/machine/machine.go#IsTime)
- [`Machine.IsClock(clock Clock) bool`](https://github.com/pancsta/asyncmachine-go/pkg/machine/machine.go#IsClock)
- [`IsTimeAfter(t1 Time, t2 Time)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#IsTimeAfter)
- [`IsActiveTick(tick uint64)`](https://github.com/pancsta/asyncmachine-go/pkg/machine/misc.go#IsActiveTick)
- [`Time.Is(stateIdxs []int)`](https://github.com/pancsta/asyncmachine-go/pkg/machine/misc.go#Time.Is)
- [`Time.Is1(stateIdx int)`](https://github.com/pancsta/asyncmachine-go/pkg/machine/misc.go#Time.Is1)

**Example** - clocks

```go
// ()[Foo:0 Bar:0 Baz:0 Exception:0]
mach.Clock("Foo") // ->0

mach.Add1("Foo", nil)
mach.Add1("Foo", nil)
// (Foo:1)[Bar:0 Baz:0 Exception:0]
mach.Clock("Foo") // ->1

mach.Remove1("Foo", nil)
mach.Add1("Foo", nil)
// (Foo:3)[Bar:0 Baz:0 Exception:0]
mach.Clock("Foo") // ->3
````

**Example** - state context

```go
func (h *Handlers) DownloadingFileState(e *am.Event) {
    // open until the state remains active
    ctx := e.Machine.NewStateCtx("DownloadingFile")
    // fork to unblock
    go func() {
        // check if still valid
        if ctx.Err() != nil {
            return // expired
        }
    }()
}
```

### Active States

Each state can be **active** or **inactive**, determined by its [state clock](#state-clocks-and-context). You can check
the current state at any time, [without a long delay](#transition-handlers), which makes it a dependable source of
decisions.

Methods to check the active states:

- [`Machine.Is(states)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Is)
- [`Machine.Is1(state)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Is1)
- [`Not(states)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Not)
- [`Not1(state)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Not1)
- [`Any(states1, states2...)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Any)
- [`Any1(state1, state2...)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Any1)

Methods to inspect / dump the currently active states:

- [`Machine.String()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.String)
- [`Machine.StringAll()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.StringAll)
- [`Machine.Inspect(states)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Inspect)

`Is` checks if all the passed states are active.

```go
// ()[Foo:0 Bar:0 Baz:0 Exception:0]

mach.Add1("Foo", nil)
// (Foo:1)[Bar:0 Baz:0 Exception:0]

mach.Is1("Foo") // true
mach.Is(am.S{"Foo", "Bar"}) // false
```

`Not` checks if none of the passed states is active.

```go
// ()[A:0 B:0 C:0 D:0 Exception:0]

mach.Add(am.S{"A", "B"}, nil)
// (A:1 B:1)[C:0 D:0 Exception:0]

// not(A) and not(C)
mach.Not(am.S{"A", "C"}) // false
// not(C) and not(D)
mach.Not(am.S{"C", "D"}) // true
```

`Any` is group call to `Is`, returns true if any of the params return true from `Is`.

```go
// ()[Foo:0 Bar:0 Baz:0 Exception:0]

mach.Add1("Foo", nil)
// (Foo:1)[Bar:0 Baz:0 Exception:0]

// is(Foo, Bar) or is(Bar)
mach.Any(am.S{"Foo", "Bar"}, am.S{"Bar"}) // false
// is(Foo) or is(Bar)
mach.Any(am.S{"Foo"}, am.S{"Bar"}) // true
```

### Inspecting States

Being able to inspect your machine at any given step is VERY important. These are the basic method which don't require
any additional [debugging tools](#debugging).

**Example** - inspecting active states and their [clocks](#state-clocks-and-context)

```go
mach.StringAll() // ->()[Foo:0 Bar:0 Baz:0 Exception:0]
mach.String() // ->()

mach.Add1("Foo")

mach.StringAll() // ->(Foo:1)[Bar:0 Baz:0 Exception:0]
mach.String() // ->(Foo:1)
```

**Example** - inspecting relations

```go
// From examples/temporal-fileprocessing/fileprocessing.go
mach.Inspect()
// Exception:
//   State:   false 0
//
// DownloadingFile:
//   State:   false 1
//   Remove:  FileDownloaded
//
// FileDownloaded:
//   State:   true 1
//   Remove:  DownloadingFile
//
// ProcessingFile:
//   State:   false 1
//   Auto:    true
//   Require: FileDownloaded
//   Remove:  FileProcessed
//
// FileProcessed:
//   State:   true 1
//   Remove:  ProcessingFile
//
// UploadingFile:
//   State:   false 1
//   Auto:    true
//   Require: FileProcessed
//   Remove:  FileUploaded
//
// FileUploaded:
//   State:   true 1
//   Remove:  UploadingFile
```

### Auto States

Automatic states (`Auto` property) are one of the most important concepts of **asyncmachine-go**. After every
[transition](#transition-lifecycle) with a [clock change](#state-clocks-and-context) (tick), `Auto` states will try to
active themselves via an auto mutation.

- `Auto` states can be set partially (within the same [mutation](#state-mutations))
- auto mutation is **prepended** to the [queue](#queue-and-history)
- `Remove` relation of `Auto` states isn't enforced within the auto mutation

**Example** - log for FileProcessed causes an `Auto` state UploadingFile to activate

```text
// [state] +FileProcessed -ProcessingFile
// [external] cleanup /tmp/temporal_sample1133869176
// [state:auto] +UploadingFile
```

### Multi States

Multi-state (`Multi` property) describes a state which can be activated many times, without being de-activated in the
meantime. It always triggers `Enter` and `State` [transition handlers](#transition-handlers), plus the
[clock](#state-clocks-and-context) is always incremented. It's useful for describing many instances of the same event
(e.g. network input) without having to define more than one transition handler. [`Exception`](#error-handling) is a good
example of a `Multi` state (many errors can happen, and we want to know all of them). The downside is that `Multi`
states don't have [state contexts](#state-clocks-and-context).

### Categories of States

States usually belong to one of these categories:

1. Input states (e.g. RPC msgs)
2. Read-only states (e.g. external state / UI state / summaries)
3. Action states (e.g. Start, ShowModal, public API methods)

Action states often de-activate themselves after they are done, as a part of their [final handler](#final-handlers).

**Example** - self removal

```go
func (h *Handlers) ClickState(e *am.Event) {
    // add removal to the queue
    e.Machine.Remove1("Click")
}
```

## Changing State

### State Mutations

**Mutation** is a request to change the currently [active states](#checking-active-states) of a machine. Each mutation
has a list of states knows as **called states**, which are different from **target states**
([calculated during a transition](#calculating-target-states)).

Mutations are [queued](#queue-and-history), thus they are never nested - one can happen only after the previous one has
been processed. Mutation methods return a
[`Result`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Result), which can be:

- `Executed`
- `Canceled`
- `Queued`

You can check if the machine is busy executing a transition by calling [`Machine.DuringTransition()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Result)
(see [queue](#queue-and-history)) or wait until it's done with [`<-Machine.WhenQueueEnds()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.DuringTransition).

There are 3 types of mutations:

- add
- remove
- set

[`Machine.Add(states, args)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Add) is the most
common method, as it preserves the currently [active states](#checking-active-states). Each activation increases the
[states' clock](#state-clocks-and-context) to an odd number.

**Example** - Add mutation

```go
// ()[Foo:0 Bar:0 Baz:0 Exception:0]

mach.Add(am.S{"Foo"}, nil)
// (Foo:1)[Bar:0 Baz:0 Exception:0]

mach.Add(am.S{"Bar"}, nil)
// (Foo:1 Bar:1)[Baz:0 Exception:0]

mach.Add1("Bar", nil)
// (Foo:1 Bar:1)[Baz:0 Exception:0]
```

[`Machine.Remove(states, args)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Remove)
deactivates only the specified states. Each de-activation increases the [states' clock](#state-clocks-and-context) to an
even number.

**Example** - Remove mutation

```go
// ()[Foo:0 Bar:0 Baz:0 Exception:0]

mach.Add(am.S{"Foo", "Bar"}, nil)
// (Foo:1 Bar:1)[Baz:0 Exception:0]

mach.Remove(am.S{"Foo"}, nil)
// (Bar:1)[Foo:2 Baz:0 Exception:0]

mach.Remove1("Bar", nil)
// [Foo:2 Bar:2 Baz:0 Exception:0]
```

[`Machine.Set(states, args)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Set)
de-activates all but the passed states and activates the remaining ones.

**Example** - Set mutation

```go
// ()[Foo:0 Bar:0 Baz:0 Exception:0]

mach.Add1("Foo", nil)
// (Foo:1)[Bar:0 Baz:0 Exception:0]

mach.Set(am.S{"Bar"}, nil)
// (Bar:1)[Foo:2 Baz:0 Exception:0]
```

### Mutation Arguments

Each [mutation](#state-mutations) has an optional map of
arguments of type [`am.A`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#A), passed to
[handlers](#defining-handlers) via the [`am.Event` struct](#event-struct).

**Example** - passing arguments to handlers

```go
args := am.A{"val": "key"}
mach.Add1("Foo", args)

// ...

// wait for a mutation like the one above
<-mach.WhenArgs("Foo", am.A{"val": "key"}, nil)
```

### Transition Lifecycle

**Transition** is created from a [mutation](#state-mutations) and tries to execute it, which can result in the changing
of machine's [active states](#checking-active-states). Each transition has several steps and (optionally) calls several
[handlers](#transition-handlers) (for each of the [bindings](#defining-handlers)).

Once a transition begins to execute, it goes through the following steps:

1. [Calculating Target States](#calculating-target-states) - collecting target states based on
   [relations](#states-relations), currently [active states](#checking-active-states) and
   [called states](#state-mutations). Transition can already be `Canceled` at this point.
2. [Negotiation handlers](#negotiation-handlers) - methods called for each state about-to-be activated or deactivated.
   Each of these handlers can return `false`, which will cause the mutation to be [`Canceled`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#Canceled)
  and [`Transition.Accepted`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#Transition.Accepted)
  to be `false`.
3. Apply the **target states** to the machine - from this point `Is` (
   [and other checking methods](#checking-active-states)) will reflect the target states.
4. [Final handlers](#final-handlers) - methods called for each state about-to-be activated or deactivated, as well as
   self handlers of currently active ones. Transition cannot be canceled at this point.

### Transition Handlers

State handler is a struct method with a predefined suffix or prefix, which receives an [Event struct](#event-struct).
There are [negotiation handlers](#negotiation-handlers) (returning a `bool`) and [final handlers](#final-handlers) (with
no return). Order of the handlers depends on currently [active states](#checking-active-states) and relations of
[active](#checking-active-states) and [target states](#calculating-target-states).

**Example** - handlers for the state Foo

```go
func (h *Handlers) FooEnter(e *am.Event) bool {}
func (h *Handlers) FooState(e *am.Event) {}
func (h *Handlers) FooExit(e *am.Event) bool {}
func (h *Handlers) FooEnd(e *am.Event) {}
```

List of handlers during a transition from `Foo` to `Bar`, in the order of execution:

- `FooExit` - [negotiation handler](#negotiation-handlers)
- `FooBar` - [negotiation handler](#negotiation-handlers)
- `FooAny` - [negotiation handler](#negotiation-handlers)
- `AnyBar` - [negotiation handler](#negotiation-handlers)
- `BarEnter` - [negotiation handler](#negotiation-handlers)
- `FooEnd` - [final handler](#final-handlers)
- `BarState` - [final handler](#final-handlers)

All handlers execute in a series, one by one, thus they don't need to mutually exclude each other for accessing
resources. This reduces the number of locks needed. No blocking is allowed in the body of a handler, unless it's in
a goroutine. Additionally, each handler has a limited time to complete (**100ms** with the default handler timeout),
which can be set via [`am.Opts`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Opts).

### Self Handlers

Self handler is a final handler for states which were active **before and after** a transition (all no-change active
states). The name is a doubled name of the state (eg `FooFoo`).

List of handlers during a transition from `Foo` to `Foo Bar`, in the order of execution:

- `AnyBar` - [negotiation handler](#negotiation-handlers)
- `BarEnter` - [negotiation handler](#negotiation-handlers)
- `FooFoo` - [final handler](#final-handlers) and **self handler**
- `BarState` - [final handler](#final-handlers)

Self handlers provide a simple alternative to [`Multi` states](#multi-states), while fully maintaining [state clocks](#state-clocks-and-context).

### Defining Handlers

Handlers are defined as struct methods. Each machine can have many handler structs bound to itself using
[`Machine.BindHandlers`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.BindHandlers),
although at least one of the structs should embed the provided `am.ExceptionHandler` (or provide its own). Any existing
struct can be used for handlers, as long as there's no name conflict.

**Example** - define `FooState` and `FooEnter`

```go
type Handlers struct {
    // default handler for the build in Exception state
    *am.ExceptionHandler
}

func (h *Handlers) FooState(e *am.Event) {
    // final activation handler for Foo
}

func (h *Handlers) FooEnter(e *am.Event) bool {
    // negotiation activation handler for Foo
    return true // accept this transition by Foo
}

func main() {
    // ...
    err := mach.BindHandlers(&Handlers{})
}
```

Log output:

```text
[add] Foo
[handler] FooEnter
[state] +Foo
[handler] FooState
```

### Event Struct

Every handler receives a pointer to an `Event` struct, with `Name`, `Machine` and `Args`.

```go
// definition
type Event struct {
    Name    string
    Machine *Machine
    Args    A
}
// send args
mach.Add(am.S{"Foo"}, A{"test": 123})
// ...
// receive args
func (h *Handlers) FooState(e *am.Event) {
  test := e.Args["test"].(string)
}
```

### Calculating Target States

[Called states](#state-mutations) combined with currently [active states](#checking-active-states) and
a [relations resolver](#states-relations) result in **target states** of a transition. This phase is **cancelable** - if
**any** of the [called states](#state-mutations) gets rejected, the **transition is canceled**. This isn't true for
[Auto states](#auto-states), which can be partially rejected.

Transition exposes the currently called, target and previous states using:

- [`e.Transition.StatesBefore`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Transition.StatesBefore)
- [`e.Transition.TargetStates`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Transition.TargetStates)
- [`e.Transition.ClockBefore()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Transition.ClockBefore)
- [`e.Transition.ClockAfter()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Transition.ClockAfter)
- [`e.Transition.Mutation.CalledStates`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Transition.CalledStates)

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {
        Add: am.S{"Bar"},
    },
    "Bar": {}
}, nil)

// ...

// handlers
func (h *Handlers) FooEnter(e *am.Event) bool {
    e.Transition.StatesBefore // ()
    e.Transition.TargetStates // (Foo Bar)
    e.Machine.Transition.CalledStates() // (Foo)

    e.Machine.Is(am.S{"Foo", "Bar"}) // false
    return true
}
func (h *Handlers) FooState(e *am.Event) {
    e.Transition.StatesBefore // ()
    e.Transition.TargetStates // (Foo Bar)
    e.Machine.Transition.CalledStates() // (Foo)

    e.Machine.Is(am.S{"Foo", "Bar"}) // true
}

// ...

// usage
mach.Add1("Foo", nil)
```

```text
[add] Foo
[implied] Bar
[handler] FooEnter
FooEnter
| From: []
| To: [Foo Bar]
| Called: [Foo]
()[Bar:0 Foo:0 Exception:0]
[state] +Foo +Bar
[handler] FooState
FooState
| From: []
| To: [Foo Bar]
| Called: [Foo]
(Bar:1 Foo:1)[Exception:0]
end
(Bar:1 Foo:1)[Exception:0]
```

### Negotiation Handlers

```go
func (h *Handlers) FooEnter(e *am.Event) bool {}
func (h *Handlers) FooExit(e *am.Event) bool {}
```

**Negotiation handlers** `Enter` and `Exit` are called for every state which is going to be activated or de-activated. They
are allowed to cancel a transition by optionally returning `false`. **Negotiation handlers** are limited to read-only
operations, or at least to side effects free ones. Their purpose is to make sure that
[final transition handlers](#final-handlers) are good to go.

```go
// negotiation handler
func (h *Handlers) ProcessingFileEnter(e *am.Event) bool {
    // read-only ops
    // decide if moving fwd is ok
    // no blocking
    // lock-free critical zone
    return true
}
```

**Example** - rejected negotiation

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {
        Add: am.S{"Bar"},
    },
    "Bar": {},
}, nil)

// ...

// handlers
func (h *Handlers) FooEnter(e *am.Event) bool {
    return false
}

// ...

// usage
mach.Add1("Foo", nil) // ->am.Canceled
// ()[Bar:0 Foo:0 Exception:0]
```

```text
[add] Foo
[implied] Bar
[handler] FooEnter
[cancel:ad0d8] (Foo Bar) by FooEnter
()[Bar:0 Foo:0 Exception:0]
```

### Final Handlers

```go
func (h *Handlers) FooState(e *am.Event) {}
func (h *Handlers) FooEnd(e *am.Event) {}
func (h *Handlers) FooBar(e *am.Event) {}
func (h *Handlers) BarFoo(e *am.Event) {}
func (h *Handlers) AnyFoo(e *am.Event) {}
func (h *Handlers) FooAny(e *am.Event) {}
```

Final handlers `State` and `End` are where the main handler logic resides. After the transition gets accepted by
relations and negotiation handlers, final handlers will allocate and dispose resources, call APIs, and perform other
blocking actions with side effects. Just like [negotiation handlers](#negotiation-handlers), they are called for every
state which is going to be activated or de-activated. Additionally, the [Self handlers](#self-handlers) are called for
states which remained active.

Like any handler, final handlers cannot block the mutation. That's why they need to start a goroutine and continue their
execution within it, while asserting the [state context](#state-clocks-and-context) is still valid.

```go
func (h *Handlers) ProcessingFileState(e *am.Event) {
    // read & write ops
    // no blocking
    // lock-free critical zone
    mach := e.Machine
    // tick-based context
    stateCtx := mach.NewStateCtx("ProcessingFile")
    go func() {
        // block in the background, locks needed
        if stateCtx.Err() != nil {
            return // expired
        }
        // blocking call
        err := processFile(h.Filename, stateCtx)
        if err != nil {
            mach.AddErr(err, nil)
            return
        }
        // re-check the tick ctx after a blocking call
        if stateCtx.Err() != nil {
            return // expired
        }
        // move to the next state in the flow
        mach.Add1("FileProcessed", nil)
    }()
}
```

### Global Handler

`AnyAny` is the first final handler and always gets executed. This makes it a global/catch-all
handler. Using a global handler make the "Empty" filter useless, as every transition always triggers a handler.

```go
func (d *Debugger) AnyAny(e *am.Event) {
    tx := e.Transition()

    // redraw on auto states
    if tx.IsAuto() && tx.Accepted {
        d.updateTxBars()
        d.draw()
    }
}
```

## Advanced Topics

### Relations

Each [state](#defining-states) can have 4 types of **relations**. Each relation accepts a list of [state names](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#S).
Relations are handled by [RelationsResolver](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#RelationsResolver),
which should be extendable and potentially even replaceable.

#### `Add` relation

The `Add` relation tries to activate listed states, whenever the owner state gets activated.

Their activation is optional, meaning if any of those won't get accepted, the transition will still be `Executed`.

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {
        Add: am.S{"Bar"},
    },
    "Bar": {},
}, nil)

// usage
mach.Add1("Foo", nil) // ->Executed
// (Foo:1 Bar:1)[Exception:0]
```

```text
[add] Foo
[implied] Bar
[state] +Foo +Bar
(Foo:1 Bar:1)[Exception:0]
```

#### `Remove` relation

The `Remove` relation prevents from activating, or deactivates listed states.

If some of the [called states](#state-mutations) `Remove` other [called states](#state-mutations), or some of the
[active states](#checking-active-states) `Remove` some of the [called states](#state-mutations), the
[transition](#transition-lifecycle) will be [`Canceled`](#transition-lifecycle).

Example of an [accepted transition](#transition-lifecycle) involving a `Remove` relation:

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {
        Remove: am.S{"Bar"},
    },
    "Bar": {},
}, nil)

// usage
mach.Add1("Foo", nil) // ->Executed
mach.Add1("Bar", nil) // ->Executed
println(m.StringAll()) // (Foo:1)[Bar:0 Exception:0]
```

```text
[add] Foo
[state] +Foo
[add] Bar
[cancel:reject] Bar
(Foo:1)[Bar:0 Exception:0]
```

Example of a [canceled transition](#transition-lifecycle) involving a `Remove` relation - some of the
[called states](#state-mutations) `Remove` other Called States.

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {},
    "Bar": {
        Remove: am.S{"Foo"},
    },
}, nil)

// usage
m.Add(am.S{"Foo", "Bar"}, nil) // ->Canceled
m.Not1("Bar") // true
// ()[Foo:0 Bar:0 Exception:0]
```

```text
[add] Foo Bar
[cancel:reject] Foo
()[Exception:0 Foo:0 Bar:0]
```

Example of a [canceled transition](#transition-lifecycle) involving a `Remove` relation - some of the [active states](#checking-active-states)
`Remove` some of the [called states](#state-mutations).

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {},
    "Bar": {
        Remove: am.S{"Foo"},
    },
}, nil)

// usage
mach.Add1("Bar", nil) // ->Executed
mach.Add1("Foo", nil) // ->Canceled
m.StringAll() // (Foo:1)[Bar:0 Exception:0]
```

```text
[add] Bar
[state] +Bar
[add] Foo
[cancel:reject] Foo
(Bar:1)[Foo:0 Exception:0]
```

#### `Require` relation

The `Require` relation describes the states required for this one to be activated.

Example of an [accepted transition](#transition-lifecycle) involving a `Require` relation:

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {},
    "Bar": {
        Require: am.S{"Foo"},
    }
}, nil)

// usage
mach.Add1("Foo", nil) // ->Executed
mach.Add1("Bar", nil) // ->Executed
// (Foo:1 Bar:1)[Exception:0]
```

```text
[add] Foo
[state] +Foo
[add] Bar
[state] +Bar
(Foo:1 Bar:1)[Exception:0]
```

Example of a [canceled transition](#transition-lifecycle) involving a `Require` relation:

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {},
    "Bar": {
        Require: am.S{"Foo"},
    },
}, nil)

// usage
mach.Add1("Bar", nil) // ->Canceled
// ()[Foo:0 Bar:0 Exception:0]
```

```text
[add] Bar
[reject] Bar(-Foo)
[cancel:reject] Bar
()[Foo:0 Bar:0 Exception:0]
```

#### `After` relation

The `After` relation decides about the order of execution of [transition handlers](#transition-handlers). Handlers from
the defined state will be executed **after** handlers from listed states.

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {
        After: am.S{"Bar"},
    },
    "Bar": {
        Require: am.S{"Foo"},
    },
}, nil)

// ...

// handlers
func (h *Handlers) FooState(e *am.Event) {
    println("Foo")
}
func (h *Handlers) BarState(e *am.Event) {
    println("Bar")
}

// ...

// usage
m.Add(am.S{"Foo", "Bar"}, nil) // ->Executed
```

```text
[add] Foo Bar
[state] +Bar +Foo
[handler] BarState
Bar
[handler] FooState
Foo
```

### Waiting

```go
// wait until FileDownloaded becomes active
<-mach.When1("FileDownloaded", nil)

// wait until FileDownloaded becomes inactive
<-mach.WhenNot1("DownloadingFile", args, nil)

// wait for EventConnected to be activated with an arg ID=123
<-mach.WhenArgs("EventConnected", am.A{"ID": 123}, nil)

// wait for Foo to have a tick >= 6 and Bar tick >= 10
<-mach.WhenTime(am.S{"Foo", "Bar"}, am.T{6, 10}, nil)

// wait for DownloadingFile to have a tick increased by 2 since now
<-mach.WhenTicks("DownloadingFile", 2, nil)
```

Almost all "when" methods return a share channel which closes when an event happens (or the optionally passed context is
canceled). They are used to wait until a certain moment, when we know the execution can proceed. Using "when" methods
creates new channels and should be used with caution, possibly making use of the early disposal context. In the future,
these channels will be reused and should scale way better.

"When" methods are:

- [`Machine.When(states, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.When)
- [`Machine.WhenNot(states, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenNot)
- [`Machine.When1(state, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.When1)
- [`Machine.WhenNot1(state, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenNot1)
- [`Machine.WhenArgs(state, args, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenArgs)
- [`Machine.WhenTime(states, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenTime)
- [`Machine.WhenTicks(state, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenTick)
- [`Machine.WhenTicksEq(state, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenTickEq)
- [`Machine.WhenQueueEnds(state, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenQueueEnds)
- [`Machine.WhenErr(state, ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenErr)

**Example** - waiting for states `Foo` and `Bar` to being active at the same time:

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {
        Add: am.S{"Bar"},
    },
    "Bar": {},
})

// ...

// usage
select {
    case <-mach.When(am.S{"Foo", "Bar"}, nil):
        println("Foo Bar")
    }
}

// ...

// state change
mach.Add1("Foo", nil)
mach.Add1("Bar", nil)
// (Foo:1 Bar:1)[Exception:0]
```

```text
[add] Foo
[implied] Bar
[state] +Foo +Bar
Foo Bar
(Bar:1 Foo:1)[Exception:0]
```

### Error Handling

Considering that everything meaningful can be a state, so can errors. Every machine has a predefined [`Exception`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#Exception)
state (which is a [`Multi` state](#multi-states)) and an optional [`ExceptionHandler`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#ExceptionHandler),
which can be embedded into [handler structs](#defining-handlers).

Creating more detailed error states which have the
[`Require` relation](#states-relations) to the `Exception` state is one way of handing errors. One can take
[`pkg/rpc/shared.go`](https://github.com/pancsta/asyncmachine-go/blob/main/pkg/rpc/shared.go#L194) as an example of this
pattern. It's not possible to use `Machine.AddErr*` methods inside `Exception*` handlers.

- [`Machine.AddErr(error, Args)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.AddErr)
- [`Machine.AddErrState(string, error, Args)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.AddErrState)
- [`Machine.WhenErr(ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenErr)
- [`Machine.Err()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Err)
- [`Machine.IsErr()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.IsErr)

**Example** - arbitrary error states

```go
// States map defines relations and properties of states.
var States = am.Struct{
    // Errors
    ErrNetwork:  {Require: S{am.Exception}},
    ErrRpc:      {Require: S{am.Exception}},
    ErrOnClient: {Require: S{am.Exception}},

    // ...
}
```

**Example** - timeout flow with error handling

```go
select {
case <-time.After(10 * time.Second):
    // timeout
case <-mach.WhenErr(nil):
    // error or machine disposed
    fmt.Printf("err: %s\n", mach.Err())
case <-mach.When1("Bar", nil):
    // state Bar active
}
```

```text
[add] Foo
[state] +Foo
[add] Exception
[state] +Exception
err: fake err
(Foo:1 Exception:1)[Bar:0]
```

**Example** - activate error states based on sentinel errors

```go
func (h *handlers) ExceptionState(e *am.Event) {
    // call super
    h.ExceptionHandler.ExceptionState(e)
    mach := e.Machine
    err := e.Args["err"].(error)

    // handle sentinel errors to states
    if errors.Is(err, ErrNetwork) || errors.Is(err, ErrNetworkTimeout) {
        mach.Add1(ss.ErrNetwork, nil)
    } else if errors.Is(err, ErrInvalidParams) {
        mach.Add1(ss.ErrRpc, nil)
    } else if errors.Is(err, ErrInvalidResp) {
        mach.Add1(ss.ErrRpc, nil)
    } else if errors.Is(err, ErrRpc) {
        mach.Add1(ss.ErrRpc, nil)
    }
}
```

### Catching Panics

**Panics** are automatically caught and transformed into the [`Exception` state](#error-handling) in case they take
place in the main body of any handler method. This can be disabled using [`Machine.PanicToException`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.PanicToException)
or [`Opts.DontPanicToException`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#Opts.DontPanicToException).
Same goes for [`Machine.LogStackTrace`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.PrintExceptions)
and [`DontLogStackTrace`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#Opts.DontPrintExceptions),
which decides about printing stack traces to the [log sink](#logging).

In case of a panic inside a transition handler, the recovery flow depends on the type of the erroneous handler.

#### Panic in a [negotiation handler](#negotiation-handlers)

1. Cancels the whole transition.
2. [Active states](#checking-active-states) of the machine stay untouched.
3. [Add mutation](#state-mutations) for the `Exception` state is prepended to the queue.

#### Panic in a [final handler](#final-handlers)

1. Transition has been accepted and [target states](#calculating-target-states) has been set as [active states](#state-mutations).
2. Not all the [final handlers](#final-handlers) have been executed, so the states from non-executed handlers are
  removed from [active states](#state-mutations).
3. [Add mutation](#state-mutations) for the `Exception` state is prepended to the queue and the integrity should
  be restored manually (e.g. [relations](#states-relations), resources involved).

```go
// TestPartialFinalPanic
type TestPartialFinalPanicHandlers struct {
    *ExceptionHandler
}

func (h *TestPartialFinalPanicHandlers) BState(_ *Event) {
    panic("BState panic")
}

func TestPartialFinalPanic(t *testing.T) {
    // init
    mach := NewNoRels(t, nil)
    // ()[A:0 B:0 C:0 D:0]

    // logger
    log := ""
    captureLog(t, m, &log)

    // bind handlers
    err := m.BindHandlers(&TestPartialFinalPanicHandlers{})
    assert.NoError(t, err)

    // test
    m.Add(S{"A", "B", "C"}, nil)

    // assert
    assertStates(t, m, S{"A", "Exception"})
}
```

#### Panic anywhere else

For places like goroutines and functions called from the outside (e.g. request handlers), there are dedicated methods
to catch panics. They support `Exception` as well as arbitrary error states.

- [`Machine.PanicToErr(args)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.PanicToErr)
- [`Machine.PanicToErrState(string, args)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.PanicToErrState)

```go
var mach *am.Machine
func getHandler(w http.ResponseWriter, r *http.Request) {
    defer mach.PanicToErr(nil)
    // ...
}
```

### Queue and History

The purpose of **asyncmachine-go** is to synchronize actions, which results in only one handler being executed at the same
time. Every mutation happening inside the handler, will be queued and the [mutation call](#state-mutations) will return [`Queued`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/machine#Queued).

Queue itself can be accessed via [`Machine.Queue()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Queue)
and checked using [`Machine.IsQueued()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.IsQueued).
After the execution, queue creates history, which can be captured using a dedicated package [`pkg/history`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/history).
Both sources can help to make informed decisions based on scheduled and past actions.

- [`Machine.Queue()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Queue)
- [`Machine.IsQueued(mutationType, states, withoutArgsOnly, statesStrictEqual, startIndex)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.IsQueued)
- [`Machine.Transition()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.DuringTransition)
- [`History.ActivatedRecently(state, duration)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/history#History.ActivatedRecently)
- [`Machine.WhenQueueEnds()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.WhenQueueEnds)
- // TODO WillBe

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {},
    "Bar": {}
}, nil)

// ...

// handlers
func (h *Handlers) FooState(e *am.Event) {
    e.Machine.Add1("Bar", nil) // ->Queued
}

// ...

// usage
mach.Add1("Foo", nil) // ->Executed
```

```text
[add] Foo
[state] +Foo
[handler] FooState
[queue:add] Bar
[postpone] queue running (1 item)
[add] Bar
[state] +Bar
```

TODO describe duplicate detection rules

### Logging

Besides [inspecting methods](#inspecting-states), **asyncmachine-go** offers a very verbose logging system with 4
levels of granularity:

- `LogNothing` (default)
- `LogChanges` state changes and important messages
- `LogOps` detailed relations resolution, called handlers, queued and rejected mutations
- `LogDecisions` more verbose variant of Ops, explaining the reasoning behind
- `LogEverything` useful only for deep debugging

Example of all log levels for the same code snippet:

```go
// machine
mach := am.New(ctx, am.Struct{
    "Foo": {},
    "Bar": {
        Auto: true,
    },
    // disable ID logging
}, &am.Opts{DontLogID: true})
m.SetLogLevel(am.LogOps)

// ...

// handlers
func (h *Handlers) FooState(e *am.Event) {
    // empty
}
func (h *Handlers) BarEnter(e *am.Event) bool {
    return false
}

// ...

// usage
mach.Add1("Foo", nil) // Executed
```

- log level `LogChanges`

```text
[state] +Foo
```

- log level `LogOps`

```text
[add] Foo
[state] +Foo
[handler] FooState
[auto] Bar
[handler] BarEnter
[cancel:4a0bc] (Bar Foo) by BarEnter
```

- log level `LogDecisions`

```text
[add] Foo
[state] +Foo
[handler] FooState
[auto] Bar
[add:auto] Bar
[handler] BarEnter
[cancel:2daed] (Bar Foo) by BarEnter
```

- log level `LogEveryting`

```text
[start] handleEmitterLoop Handlers
[add] Foo
[emit:Handlers:d7a58] AnyFoo
[emit:Handlers:d32cd] FooEnter
[state] +Foo
[emit:Handlers:aa38c] FooState
[handler] FooState
[auto] Bar
[add:auto] Bar
[emit:Handlers:f353d] AnyBar
[emit:Handlers:82e34] BarEnter
[handler] BarEnter
[cancel:82e34] (Bar Foo) by BarEnter
```

#### Customizing Logging

**Example** - binding to a test logger

```go
// test log with the minimal log level
mach.SetLoggerSimple(t.Logf, am.LogChanges)
```

**Example** - logging [mutation arguments](#mutation-arguments)

```go
// include some args in the log and traces
mach.SetLogArgs(am.NewArgsMapper([]string{"id", "name"}, 20))
```

**Example** - custom logger

```go
// max out the log level
mach.SetLogLevel(am.LogEverything)
// level based dispatcher
mach.SetLogger(func(level LogLevel, msg string, args ...any) {
    if level > am.LogChanges {
        customLogDetails(msg, args...)
        return
    }
    customLog(msg, args...)

})
```

### Debugging

**asyncmachine-go** comes with [`am-dbg TUI Debugger`](/tools/cmd/am-dbg), which makes it very easy to hook into any
state machine on a [transition's](#transition-lifecycle) step-level, and retain [state machine's log](#logging). It
also combines very well with the Golang debugger when stepping through code.

Environment variables used for debugging can be found in [config/env/README.md](/config/env/README.md).

#### Steps To Debug

1. Install `go install github.com/pancsta/asyncmachine-go/tools/am-dbg@latest`
2. Run `am-dbg`
3. [Enable telemetry](#enabling-telemetry)
4. Run your code with `env AM_DEBUG=1` to increase timeouts and enable stack traces

#### Enabling Telemetry

Telemetry for **am-dbg** can be enabled manually using [`/pkg/telemetry`](/pkg/telemetry/README.md), or with a helper
from [`/pkg/helpers`](/pkg/helpers/README.md).

- [`MachDebug(*am.Machine, string, am.LogLevel, bool)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#MachDebug)
- [`MachDebugt(*am.Machine, bool)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#MachDebugT)

**Example** - enable telemetry manually

```go
import "github.com/pancsta/asyncmachine-go/pkg/telemetry"
// ...
err := telemetry.TransitionsToDBG(mach, "")
```

**Example** - enable telemetry using helpers

```go
// read env
amDbgAddr := os.Getenv("AM_DBG_ADDR")
logLvl := am.EnvLogLevel("")

// debug
amhelp.MachDebug(mach, amDbgAddr, logLvl, true)
```

### Typesafe States

While it's perfectly possible to operate on pure string names for state names (e.g. for prototyping), it's not type
safe, leads to errors, doesn't support godoc, nor looking for references in IDEs. [`/tools/cmd/am-gen`](/tools/cmd/am-gen)
will generate a conventional type-safe states file, similar to an enum package. It also aliases common functions to
manipulates state lists, relations, and structure. After the initial bootstrapping, the file should be edited manually.
Imports of states files are commonly aliased as `ss` (first-last rune). `ss.Names` is used to ["verify" states](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.VerifyStates),
as map keys have a random order.

**Example** - using am-gen to bootstrap a states file

```go
// generate using either
// $ am-gen states-file Foo,Bar
// $ task am-gen -- Foo,Bar
package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// State is a type alias for a single state definition.
type State = am.State

// SAdd is a func alias for merging lists of states.
var SAdd = am.SAdd

// StateAdd is a func alias for adding to an existing state definition.
var StateAdd = am.StateAdd

// StateSet is a func alias for replacing parts of an existing state
// definition.
var StateSet = am.StateSet

// StructMerge is a func alias for extending an existing state structure.
var StructMerge = am.StructMerge

// States structure defines relations and properties of states.
var States = am.Struct{
    Start: {},
    Heartbeat: {},
}

// Groups of mutually exclusive states.

//var (
//    GroupPlaying = S{Playing, Paused}
//)

//#region boilerplate defs

// Names of all the states (pkg enum).

const (
    Start = "Start"
    Heartbeat = "Heartbeat"
)

// Names is an ordered list of all the state names.
var Names = S{
    am.Exception,
    Start,
    Heartbeat,

}

//#endregion
```

**Example** - using a states file

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"

    ss "github.com/owner/repo/states"
)

mach := am.New(ctx, ss.States, nil)
err := mach.VerifyStates(ss.Names)
```

### Tracing and Metrics

TODO

- [`pkg/telemetry`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/telemetry) - Open Telemetry
  and am-dbg
- [`pkg/telemetry/prometheus`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go@v0.7.0-pre1/pkg/telemetry/prometheus)
  \- Grafana

### Optimizing Data Input

It's a good practice to batch frequent operations, so the [relation resolution](#states-relations) and
[transition lifecycle](#transition-lifecycle) doesn't execute for no reason. It's especially true for network packets.
Below a simple debounce with a queue.

**Example** - batch data into a single transition every 1s

```go
var debounce = time.Second

var queue []*Msg
var queueMx sync.Mutex
var scheduled bool

func Msg(msgTx *Msg) {
    queueMx.Lock()
    defer queueMx.Unlock()

    if !scheduled {
        scheduled = true
        go func() {
            // wait some time
            time.Sleep(debounce)

            queueMx.Lock()
            defer queueMx.Unlock()

            // add in bulk
            mach.Add1("Msgs", am.A{"msgs": queue})
            queue = nil
            scheduled = false
        }()
    }
    // enqueue
    queue = append(queue, msgTx)
}
```

## Remote Machines

[`/pkg/rpc`](/pkg/rpc/README.md) provides efficient network transparency for any state machine, including local ones.
Both the server and client has to have access to worker's [state file](#typesafe-states).

```go

import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    ssCli "github.com/pancsta/asyncmachine-go/pkg/rpc/states/client"
    ssSrv "github.com/pancsta/asyncmachine-go/pkg/rpc/states/server"
)
```

### Server

```go
// init
s, err := NewServer(ctx, addr, worker.ID, worker, nil)
if err != nil {
    panic(err)
}

// start and wait
s.Start()
<-s.Mach.When1(ssSrv.RpcReady, nil)
```

### Client

```go
// init
c, err := NewClient(ctx, addr, "clientid", worker.GetStruct(),
    worker.StateNames())
if err != nil {
    panic(err)
}

// start and wait
c.Start()
<-c.Mach.When1(ssCli.Ready, nil)
```

## Other packages

**asyncmachine-go** provides additional tooling and extensions in the form of separate packages, available at
[`/pkg`](https://github.com/pancsta/asyncmachine-go/tree/main/pkg) and [`/tools`](https://github.com/pancsta/asyncmachine-go/tree/main/tools).
Each of them has a readme with examples:

- **[`/pkg/helpers`](/pkg/helpers/README.md)**<br>
  Useful functions when working with state machines.
- [`/pkg/history`](/pkg/history/README.md)<br>
  History tracking and traversal.
- **[`/pkg/rpc`](/pkg/rpc/README.md)**<br>
  Clock-based remote state machines, with the same API as local ones.
- [`/pkg/states`](/pkg/states/README.md)<br>
  Repository of common state definitions, so APIs can be more unifed.
- **[`/pkg/telemetry`](/pkg/telemetry/README.md)**<br>
  Telemetry exporters for am-dbg, Open Telemetry and Prometheus.
- [`/pkg/x/helpers`](/pkg/x/helpers)<br>
  Not-so useful functions when working with state machines.
- **[`/tools/cmd/am-dbg`](/tools/cmd/am-dbg/README.md)**<br>
  am-dbg is a multi-client TUI debugger.
- [`/tools/cmd/am-gen`](/tools/cmd/am-gen/README.md)<br>
  am-gen is useful for bootstrapping states files.

### Helpers

Helper functions are a notable mention, as many people may look for synchronous wrappers of async state machine calls.
These assume a single, blocking scenario which is bound to the passed context. [Multi states](#multi-states) are handled
automatically.

- [`Add1Block(context.Context, types.MachineApi, string, am.A)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#Add1Block)
- [`Add1AsyncBlock(context.Context, types.MachineApi, string, string, am.A)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#Add1AsyncBlock)

**Example** - add state `StateNameSelected` and wait until it becomes active

```go
res := amhelp.Add1Block(ctx, mach, ss.StateNameSelected, am.A{"state": state})
print(mach.Is1(ss.StateNameSelected)) // true
print(res) // am.Executed or am.Canceled, never am.Queued
```

**Example** - wait for `ScrollToTx`, triggered by `ScrollToMutTx`

```go
res := amhelp.Add1AsyncBlock(ctx, mach, ss.ScrollToTx, ss.ScrollToMutTx, am.A{
    "state": state,
    "fwd":   true,
})
print(mach.Is1(ss.ScrollToTx)) // true
print(res) // am.Executed or am.Canceled, never am.Queued
```

## Cheatsheet

- **State**: main entity of the [machine](#machine-init), higher-level abstraction of a meaningful workflow step
- **Active states**: [states](#defining-states) currently activated in the machine, `0-n` where `n == len(states)`
- **Called states**: [states](#defining-states) passed to a [mutation method](#state-mutations), explicitly requested
- **Target states**: [states](#defining-states) after resolving [relations](#states-relations), based on previously
  [active states](#checking-active-states), about to become new [active states](#transition-lifecycle)
- **Mutation**: change to currently [active states](#checking-active-states), created by [mutation methods](#state-mutations)
- **Transition**: container struct for a [mutation](#state-mutations), handles [relations](#states-relations) and [events](#dynamic-handlers)
- **Accepted transition**: [transition](#transition-lifecycle) which [mutation](#state-mutations) has passed
  [negotiation](#negotiation-handlers) and [relations](#states-relations)
- **Canceled transition**: transition which [mutation](#state-mutations) has NOT passed [negotiation](#negotiation-handlers)
  or [relations](#states-relations)
- **Queued transition**: [transition](#transition-lifecycle) which couldn't execute immediately, as another one was in
  progress, and was added to the [queue](#queue-and-history) instead
- **Transition handlers**: methods [defined on a handler struct](#defining-handlers), which are triggered during a [transition](#transition-lifecycle)
- **Negotiation handlers**: [handlers](#defining-handlers) executed as the first ones, used to make a decision if the
  [transition](#transition-lifecycle) should be
   accepted
- **Final handlers**: [handlers](#defining-handlers) executed as the last ones, used for operations with side effects

## Other sources

Please refer to the [cookbook](/docs/cookbook.md) and [examples](/examples).
