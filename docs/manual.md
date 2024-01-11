# asyncmachine-go manual

## States

### Everything is a state

The fundamental idea is that **everything is a state**. A state represents certain abstraction in time. A simple
example of a problem which can be easily solved using the concept of states is an elevator - it can be on one of the
floors, it may be going down or up, it may have buttons pressed, or it may stand still, etc. Some of those states can
happen simultaneously (going up with having buttons pressed), others can be mutually exclusive (going up and down).

In AsyncMachine, multiple states can be active at the same time, unlike in a classic [FSM](https://en.wikipedia.org/wiki/Finite-state_machine).

### Defining States

States definition `am.States` is a map of `am.S` structs, which consist of a name, properties and
[relations](#relations):

```go
am.States{
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

Examples here use a string representation of a machine in the format of `(ActiveState:\d)[InactiveState:\d]`
(see [debug methods](#debug-methods)).

### What to consider a "state"

States should represent a higher-level view on what's happening in your workflow.

In case of the elevator example mentioned above, you probably don't want to make every passenger a state described by
its name, although it could be useful to have states like `Empty`, `Boarded` and `OverCapacity`.

### Asynchronous states

Considering that everything is a state, representing async actions can be achieved by creating two separate states -
*in progress** and **completed**.

For example, when downloading a file, it would be `DownloadingFile` and `FileDownloaded`. Having every meaningful action
and event encapsulated as a *state* allows us to precisely react on input events. In case of the "file download" example
, we could've had another states called `ButtonPressed`, which triggers the download, but makes a different decision in
case the state `DownloadingFile` is currently active. Same goes for `FileDownloaded` which can behave differently if
there's been another `DownloadingFile` state since the first download process had begun.

## Mutations

**Mutation methods** change the currently **Active States** for a machine. Each method receives a set of state names
(0-n), these are **Called States** and are different from **Target States**, which are
[calculated during a Transition](#calculating-target-states).

*Every state mutation is non-blocking**, so the machine can constantly make decisions and update the currently active
states, or put them into the [queue](#queue). All the mutation methods accepts multiple states.

*Mutations aren't nested** - one can happen only after the previous one has finished. That's why mutation methods
return a `Result`, which can be:

- `Executed`
- `Canceled`
- `Queued`

You can check prior to mutating if the machine is busy executing a transition by calling `DuringTransition()` method
(see [Locks](#queue)).

### `Add` mutation

*Add** is the most common method, as it preserves the currently active states.

```go
func (m *Machine) Add(states S, args A) Result
```

Example:

```go
machine.StringAll() // ()[Foo:0 Bar:0 Baz:0 Exception:0]
machine.Add(am.S{"Foo"})
machine.StringAll() // (Foo:1)[Bar:0 Baz:0 Exception:0]
```

### `Remove` mutation

*Remove** deactivates only specified states.

```go
func (m *Machine) Remove(states S, args A) Result
```

Example:

```go
machine.StringAll() // ()[Foo:0 Bar:0 Baz:0 Exception:0]
machine.Add(am.S{"Foo", "Bar"})
machine.StringAll() // (Foo:1 Bar:1)[Baz:0 Exception:0]
machine.Remove(am.S{"Foo"})
machine.StringAll() // (Foo:1)[Bar:1 Baz:0 Exception:0]
```

### `Set` mutation

*Set** removes all the currently active states, but the ones passed.

```go
func (m *Machine) Set(states S, args A) Result
```

Example:

```go
machine.StringAll() // ()[Foo:0 Bar:0 Baz:0 Exception:0]
machine.Add(am.S{"Foo"})
machine.StringAll() // (Foo:1)[Bar:0 Baz:0 Exception:0]
machine.Set(am.S{"Bar"})
machine.StringAll() // (Bar:1)[Foo:1 Baz:0 Exception:0]
```

### Machine init

To initialize a machine, besides [state definitions](#defining-states), we also need a `context` and optional
`Opts` struct (in case one want's to alter the defaults).

```go
// From examples/temporal-fileprocessing/fileprocessing.go
ctx := context.Background()
machine := am.New(ctx, am.States{
    "DownloadingFile": {
        Remove: am.S{"FileDownloaded"},
    },
    "FileDownloaded": {
        Remove: am.S{"DownloadingFile"},
    },
    "ProcessingFile": {
        Auto:    true,
        Require: am.S{"FileDownloaded"},
        Remove:  am.S{"FileProcessed"},
    },
    "FileProcessed": {
        Remove: am.S{"ProcessingFile"},
    },
    "UploadingFile": {
        Auto:    true,
        Require: am.S{"FileProcessed"},
        Remove:  am.S{"FileUploaded"},
    },
    "FileUploaded": {
        Remove: am.S{"UploadingFile"},
    },
}, nil)
```

Each machine is given a random `ID`, its own `context` and the [Exception](#error-handling) state.

### State Clocks

*Every state has a clock**. State clocks are [logical clocks](https://en.wikipedia.org/wiki/Logical_clock) which
increment ("tick") every time the state becomes active. Combination of all the state clocks makes the machine clock.

The purpose of a state clock is to distinguish different instances of the same state. Like in the "file download"
example about a button and download process, the `FileDownloaded` state can check if it has been originated by the
current `FileDownloading` state, simply by checking its clock. This helps to prevent race conditions.

Each machine provides `Clock(state string) uint64`, `Time(states S) T`, `TimeSum(states S) T` methods. There's also a
helper function `IsTimeAfter(t1 T, t2 T)`.

Example:

```go
machine.StringAll() // ()[Foo:0 Bar:0 Baz:0 Exception:0]
machine.Clock("Foo") // 0

machine.Add(am.S{"Foo"})
machine.Add(am.S{"Foo"})
machine.StringAll() // (Foo:1)[Bar:0 Baz:0 Exception:0]
machine.Clock("Foo") // 1

machine.Remove(am.S{"Foo"})
machine.Add(am.S{"Foo"})
machine.StringAll() // (Foo:2)[Bar:0 Baz:0 Exception:0]
machine.Clock("Foo") // 2
````

### Checking Active States

The currently active states can be checked using `Is(states S) bool`, `Not(states S) bool`, and `Any(states ...S) bool`.
There are also debug methods `String()`, `StringAll()` and `Inspect(states S)`.

#### Is() Method

`Is` checks if all the passed states are active.

```go
machine.StringAll() // ()[Foo:0 Bar:0 Baz:0 Exception:0]
machine.Add(am.S{"Foo"})
machine.Is(am.S{"Foo"}) // true
machine.Is(am.S{"Foo", "Bar"}) // false
```

#### Not() Method

`Not` checks if none of the passed states are active.

```go
machine.StringAll() // ()[A:0 B:0 C:0 D:0 Exception:0]
machine.Add(am.S{"A", "B"})
// not(A) and not(C)
machine.Not(am.S{"A", "C"}) // false
// not(C) and not(D)
machine.Not(am.S{"C", "D"}) // true
```

#### Any() Method

`Any` is group call to `Is`, returns true if any of the params return true from `Is`.

```go
machine.StringAll() // ()[Foo:0 Bar:0 Baz:0 Exception:0 Exception:0]
machine.Add(am.S{"Foo"})
// is(Foo, Bar) or is(Bar)
machine.Any(am.S{"Foo", "Bar"}, am.S{"Bar"}) // false
// is(Foo) or is(Bar)
machine.Any(am.S{"Foo"}, am.S{"Bar"}) // true
```

#### Debug Methods

Being able to inspect your machine at any given step is VERY important. These are the basic method which don't require
any additional tools.

Inspecting active states and their [clocks](#state-clocks):

```go
machine.StringAll() // ()[Foo:0 Bar:0 Baz:0 Exception:0]
machine.String() // ()
machine.Add(am.S{"Foo"})
machine.StringAll() // (Foo:1)[Bar:0 Baz:0 Exception:0]
machine.String() // (Foo:1)
```

Inspecting relations:

```go
// From examples/temporal-fileprocessing/fileprocessing.go
machine.Inspect()
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

### Auto states

Automatic states (`Auto` property) are one of the most powerful concepts of AsyncMachine. **After every mutation, auto
states will try to activate themselves**, assuming their dependencies are met. After every transition with a state
change (`tick`), there will be a try to activate all not-active auto states. Auto states can be set partially. This
mutation is **prepended** to the queue (instead of _appended_ like in case of _manual_ mutations).

Example log output:

```text
// [state] +FileProcessed -ProcessingFile
// [external] cleanup /tmp/temporal_sample1133869176
// [state:auto] +UploadingFile
```

### Multi states

Multi-state (`Multi` property) describes a state which can be activated many times, without being de-activated in the
meantime. It always triggers `Enter` and `State` [transition handlers](#transition-handlers), plus
the [clock](#state-clocks) is always incremented. It's useful for describing many instances of the same event (eg
network input) without having to define more than one transition handler. `Exception` is a good example of a
`Multi` state.

### "Action" states

A "state" usually describes something more "persistent" then an action, eg `Click` is an action while `Downloadeded`
is a persistent state. "Action" states differ from [multi states](#multi-states) as they usually don't happen in bulk.
Therefor there's no need to use `Multi` and a self-removal inside a [final transition handler](#final-handlers) is
enough.

Example:

```go
machine.StringAll() // ()[Foo:0 Bar:0 Baz:0 Exception:0]
func (h *MachineHandlers) ClickState(e *am.Event) {
    // this will get queued and eventually executed
    h.Machine.Remove(am.S{"Click"})
}
```

This way the state `Click` stays integral (on / off) and you can reference the input action using
[state relations](#relations) to perform further workflow steps.

## Transitions

Transition performs a mutation of machine's active states to a different subset of possible states (>= 0). Eg a machine
with states `Foo Bar Baz` can have active states mutated from `Foo` to `Foo Baz` or from nil to `Bar Baz`.
Each transition has several steps and (optionally) calls several [handlers](#transition-handlers) (for each binding).

### Transition handlers

State handler is a function on struct, with a predefined name, which receives an [Event object](#event-object). There
are [negotiation handlers](#negotiation-handlers) (return `bool`) and [final handlers](#final-handlers) (no return).
Order of the handlers depends on currently active states and relations.

```go
func (h *MachineHandlers) DownloadingFileState(e *am.Event) {
```

List of handlers during a transition from `Foo` to `Bar`, in the order of execution:

- `FooExit` - [negotiation handler](#negotiation-handlers)
- `FooBar` - [negotiation handler](#negotiation-handlers)
- `FooAny` - [negotiation handler](#negotiation-handlers)
- `AnyBar` - [negotiation handler](#negotiation-handlers)
- `BarEnter` - [negotiation handler](#negotiation-handlers)
- `FooEnd` - [final handler](#final-handlers)
- `BarState` - [final handler](#final-handlers)

### Self handlers

Self handler is a final handler for states which were active **before and after** a transition.

List of handlers during a transition from `Foo` to `Foo Bar`, in the order of execution:

- `AnyBar` - [negotiation handler](#negotiation-handlers)
- `BarEnter` - [negotiation handler](#negotiation-handlers)
- `FooSelf` - [final handler](#final-handlers) and **self handler**
- `BarState` - [final handler](#final-handlers)

### Defining Handlers

Each machine can have many handler structs bound to itself using `BindHandlers`, although at least one of the structs
should embed provided `am.ExceptionHandler` (or equivalent).

```go
type MachineHandlers struct {
    // default handler for the build in Exception state
    am.ExceptionHandler
}
func (h *MachineHandlers) FooState(e *am.Event) {
    // final entry handler for Foo
}
func (h *MachineHandlers) FooEnter(e *am.Event) bool {
    // negotiation entry handler for Foo
    return true // accept this transition by Foo
}
```

Example of binding handlers:

- [go playground](https://goplay.tools/snippet/EQRPFbqBPzo)

```go
binding, err := m.BindHandlers(&MachineHandlers{})
if err != nil {
    panic(err)
}
<-binding.Ready
```

### Event object

Every handler receives a pointer to an `Event` object, with a `Name` and `Machine`. When calling any
[mutation method](#mutations), one can also pass an arguments struct `A` of type `map[string]any`. It will
be attached to the `Event` object and delivered to all (executed) transition handlers.

```go
// definition
type Event struct {
    Name    string
    Machine *Machine
    Args    A
}
// send args
machine.Add(am.S{"Foo"}, A{"test": 123})
// ...
// receive args
func (h *MachineHandlers) FooState(e *am.Event) {
  test := e.Args["test"].(string)
}
```

### Transition lifecycle

Once a transition begins to execute, it goes through the following steps:

1. [Calculating Target States](#calculating-target-states) - collecting target states based on [relations](#relations),
  currently active states and called states. Transition can already be `Canceled` at this point.
2. [Negotiation handlers](#negotiation-handlers) - methods called for each state about-to-be activated or deactivated. Each
  of these handlers can return `false` which will make the transition `Canceled`.
3. Apply the target states to the machine - from this point `Is` will reflect the target states.
4. [Final handlers](#final-handlers) - methods called for each state about-to-be activated or deactivated and self
  handlers of currently active ones. Transition cannot be canceled at this point.

### Calculating Target States

[Called States](#mutations) combined with currently [Active States](#mutations) and a [relations resolver](#relations)
result in **target states** of a transition. This phase is **cancelable** - if any of the [Called States](#mutations)
gets rejected, the *transition is canceled**. This isn't true for [Auto States](#auto-states), which can be accepted
partially.

Target states can be accessed with `machine.To()` or `machine.From()`.

- [go playground](https://goplay.tools/snippet/gQhFetorZyz)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {
        Add: am.S{"Bar"},
    },
    "Bar": {}
}, nil)
// ...
// handlers
func (h *MachineHandlers) FooEnter(e *am.Event) bool {
    e.Machine.From() // ()
    e.Machine.To() // (Foo Bar)
    e.Machine.Transition.CalledStates() // (Foo)

    e.Machine.Is(am.S{"Foo", "Bar"}) // false
    return true
}
func (h *MachineHandlers) FooState(e *am.Event) {
    e.Machine.From() // ()
    e.Machine.To() // (Foo Bar)
    e.Machine.Transition.CalledStates() // (Foo)

    e.Machine.Is(am.S{"Foo", "Bar"}) // true
}
// ...
// usage
m.Add(am.S{"Foo"}, nil)
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
// with a return
func (h *MachineHandlers) FooEnter(e *am.Event) bool {}
// without a return (always true)
func (h *MachineHandlers) FooEnter(e *am.Event) {}
```

Negotiation handlers `Enter` and `Exit` are called for every state which is going to be activated or de-activated.
They are allowed to cancel a transition by optionally returning `false`. Negotiation handlers shouldn't perform any
action which cannot be undone. Their purpose is to make sure that [final transition handlers](#final-handlers) are good
to go.

- [go playground](https://goplay.tools/snippet/GxklYYyq0Uo)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {
        Add: am.S{"Bar"},
    },
    "Bar": {},
}, nil)
// ...
// handlers
func (h *MachineHandlers) FooEnter(e *am.Event) bool {
    return false
}
// ...
// usage
m.Add(am.S{"Foo"}, nil) // am.Canceled
m.StringAll() // ()[Bar:0 Foo:0 Exception:0]
```

```text
[add] Foo
[implied] Bar
[handler] FooEnter
[cancel:ad0d8] (Foo Bar) by FooEnter
()[Bar:0 Foo:0 Exception:0]
```

### Final Handlers

Final handlers `State` and `End` are where the main handler logic resides. After the transition gets accepted by
relations and negotiation handlers, final handlers will allocate and dispose resources, call APIs, and perform other
actions with side effects. Just like [negotiation handlers](#negotiation-handlers), they are called for every state
which is going to be activated or de-activated. Additionally, the `Self` handlers are called for states which remained
active.

Like any handler, final handlers cannot block the mutation. That's why they need to start a goroutine and continue their
execution within it, while asserting the [State Context](#state-context) is still valid.

- [go playground](https://goplay.tools/snippet/5q4TTYbR7iN)

```go
func (h *MachineHandlers) FooState(e *am.Event) {
    // critical zone
    name := e.Args["name"].(string)
    // fork
    go func() {
        // API calls, etc...
    }()
}
```

```text
[add] Foo
[state] +Foo
[handler] FooState
blocking API call start fake name
blocking API call end fake name
(Foo:1)[Exception:0]
```

### Dynamic Handlers

`On` returns a channel that will be notified with `*Event`, when any of the
passed events happen. It's quick substitute for a predefined transition
handler, although it does not guarantee a deterministic order of execution. It also accepts an optional context.

The main difference of **dynamic handlers** is that they don't return and the machine continues the execution right
after notifying the channel.

- [go playground](https://goplay.tools/snippet/Qbp7SCVLyde)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {},
    "Bar": {},
})
// ...
// usage
fooEnter := m.On([]string{"FooEnter"}, nil)
go func() {
    <-fooEnter
    println("On(FooEnter)")
})
```

```text
[add] Foo
[state] +Foo
On(FooEnter)
```

### State Context

State Context is a `context.Context()` with a lifetime of a **single state's instance**
 ([clock tick](#state-clocks)).

Consider following transitions (represented by [StringAll()](#debug-methods)):

1. `(Foo:1)[Bar:0 Baz:0 Exception:0]`
2. `()[Foo:1 Bar:0 Baz:0 Exception:0]`
3. `(Foo:2)[Bar:0 Baz:0 Exception:0]`

Steps 1 and 3 will have a different state context for `Foo`, because `Foo`'s clock has changed.

Example usage of state clocks:

- [go playground](https://goplay.tools/snippet/Vp_Cp2ZffQx)

```go
func (h *MachineHandlers) ProcessingFileState(e *am.Event) {
    stateCtx := e.Machine.GetStateCtx("ProcessingFile")
    go func () {
        // check the state context
        if stateCtx.Err() != nil {
            println("state context canceled")
            // no longer ProcessingFile, at least not in the same instance (the state could have been activated again)
            return
        }
        // continue ops
    }()
}
```

```text
[add] ProcessingFile
[state] +ProcessingFile
[handler] ProcessingFileState
[remove] ProcessingFile
[state] -ProcessingFile
[add] ProcessingFile
[state] +ProcessingFile
[handler] ProcessingFileState
state context canceled
(ProcessingFile:2)[Exception:0]
```

## Wait Methods

Wait Methods help to react on certain states are active (or not). Those are `When(states S, ctx)` and
`WhenNot(states S, ctx)` which return a channel which closes, once the machine sets on the requested combination.
They optionally accept a context to be disposed earlier then the machine itself.

Example of waiting for states `Foo` and `Bar` being active at the same time:

- [go playground](https://goplay.tools/snippet/vMuTHCWsSeL)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {
        Add: am.S{"Bar"},
    },
    "Bar": {},
})
// ...
// usage
select {
    case <-machine.When(am.S{"Foo", "Bar"}, nil):
        println("Foo Bar")
    }
}
// ...
// state change
m.Add(am.S{"Foo"}, nil)
m.Add(am.S{"Bar"}, nil)
println(m.StringAll()) // (Foo:1 Bar:1)[Exception:0]
```

```text
[add] Foo
[implied] Bar
[state] +Foo +Bar
Foo Bar
(Bar:1 Foo:1)[Exception:0]
```

## Error Handling

Considering that everything (meaningful) is a state, so should be errors. Every machine has a predefined `Exception`
state and an optional `ExceptionHandler` struct, which can be embedded into own handlers.

There are helper methods to make dealing with the `Exception` state, those are `AddErr(error)`, `AddErrStr(string)`,
`WhenErr(ctx)` and the `Err error` property.

Example timeout flow with error handling:

- [go playground](https://goplay.tools/snippet/lV1V8yc0yak)

```go
select {
case <-time.After(10 * time.Second):
    // timeout
case <-machine.WhenErr(nil):
    // error or machine disposed
    fmt.Printf("err: %s\n", machine.Err)
case <-machine.When(am.S{"Bar"}, nil):
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

*Panics** are automatically caught and transformed into `Exception`. This can be disabled using
`Machine.PanicToException` or `Opts.DontPanicToException`. Same goes for `Machine.PrintExceptions` and
`DontPrintExceptions`.

### Panics In Handlers

In case of a panic inside a transition handler, the recovery goes as follows:

*Panic in a [negotiation handler](#negotiation-handlers):**

1. Cancels the whole transition.
2. Active states of the machine stay untouched.
3. [Add mutation](#mutations) for the `Exception` state is prepended to the queue.

*Panic in a [final handler](#final-handlers):**

1. Transition is accepted, Target States has been set as Active State.
2. Not all the [final handlers](#final-handlers) have been executed, so the states from non-executed handlers are
  removed from active states.
3. [Add mutation](#mutations) for the `Exception` state is prepended to the queue and the integrity should
  be restored manually.

// TODO example

## Relations

Each state can have 4 types of **relations** and 2 **properties**. Each relation accepts a list of state names. Below
the `am.State` struct (see [defining states](#defining-states) for more):

```go
type S []string
type State struct {

    // properties
    Auto    bool
    Multi   bool

    // relations
    Require S
    Add     S
    Remove  S
    After   S
}
```

### `Add` Relation

The `Add` relation tries to activate listed states, along with the owner state.

Their activation is optional, meaning if any of those won't get accepted, the transition will still be `Executed`.

- [go playground](https://goplay.tools/snippet/rCrDqWZRN9d)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {
        Add: am.S{"Bar"},
    },
    "Bar": {},
}, nil)
// usage
m.Add(am.S{"Foo"}, nil)
println(m.StringAll()) // (Foo:1 Bar:1)[Exception:0]
```

```text
[add] Foo
[implied] Bar
[state] +Foo +Bar
(Foo:1 Bar:1)[Exception:0]
```

### `Remove` Relation

The `Remove` relation prevents from activating, or deactivates listed states.

If some of the Called States `Remove` other Called States, or some of the Active States `Remove` some of the Called
States, the [Transition](#transitions) will be `Canceled`.

Example of an [accepted transition](#transition-lifecycle) involving a `Remove` relation:

- [go playground](https://goplay.tools/snippet/3878ksPe2cN)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {
        Remove: am.S{"Bar"},
    },
    "Bar": {},
}, nil)
// usage
m.Add(am.S{"Foo"}, nil) // Executed
m.Add(am.S{"Bar"}, nil) // Executed
println(m.StringAll()) // (Foo:1)[Bar:0 Exception:0]
```

```text
[add] Foo
[state] +Foo
[add] Bar
[cancel:reject] Bar
(Foo:1)[Bar:0 Exception:0]
```

Example of a [canceled transition](#transition-lifecycle) involving a `Remove` relation - some of the Called States
`Remove` other Called States.

- [go playground](https://goplay.tools/snippet/gkZnrLNKnTy)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {},
    "Bar": {
        Remove: am.S{"Foo"},
    },
}, nil)
// usage
m.Add(am.S{"Foo", "Bar"}, nil) // Canceled
m.Not(am.S{"Bar"}) // true
m.StringAll() // ()[Foo:0 Bar:0 Exception:0]
```

```text
[add] Foo Bar
[cancel:reject] Foo
()[Exception:0 Foo:0 Bar:0]
```

Example of a [Canceled Transition](#transition-lifecycle) involving a `Remove` relation - some of the Active States
`Remove` some of the Called States.

- [go playground](https://goplay.tools/snippet/Nv7SvnZbGzN)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {},
    "Bar": {
        Remove: am.S{"Foo"},
    },
}, nil)
// usage
m.Add(am.S{"Bar"}, nil) // Executed
m.Add(am.S{"Foo"}, nil) // Canceled
m.StringAll() // (Foo:1)[Bar:0 Exception:0]
```

```text
[add] Bar
[state] +Bar
[add] Foo
[cancel:reject] Foo
(Bar:1)[Foo:0 Exception:0]
```

### `Require` Relation

The `Require` relation describes the states required for this one to be activated.

Example of an [Accepted Transition](#transition-lifecycle) involving a `Require` relation:

- [go playground](https://goplay.tools/snippet/zjX4ShKoTPt)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {},
    "Bar": {
        Require: am.S{"Foo"},
    }
}, nil)
// usage
m.Add(am.S{"Foo"}, nil) // Executed
m.Add(am.S{"Bar"}, nil) // Executed
println(m.StringAll()) // (Foo:1 Bar:0)[Exception:0]
```

```text
[add] Foo
[state] +Foo
[add] Bar
[state] +Bar
(Foo:1 Bar:1)[Exception:0]
```

Example of a [Canceled Transition](#transition-lifecycle) involving a `Require` relation:

- [go playground](https://goplay.tools/snippet/FPXbIX49fAU)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {},
    "Bar": {
        Require: am.S{"Foo"},
    },
}, nil)
// usage
m.Add(am.S{"Bar"}, nil) // Canceled
println(m.StringAll()) // ()[Foo:0 Bar:0 Exception:0]
```

```text
[add] Bar
[reject] Bar(-Foo)
[cancel:reject] Bar
()[Foo:0 Bar:0 Exception:0]
```

### `After` Relation

The `After` relation decides about the order of the [Transition Handlers](#transition-handlers). Handlers from
the defined state will be executed **after** handlers from listed states.

- [go playground](https://goplay.tools/snippet/09D9CQQQnm7)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {
        After: am.S{"Bar"},
    },
    "Bar": {
        Require: am.S{"Foo"},
    },
}, nil)
// ...
// handlers
func (h *MachineHandlers) FooState(e *am.Event) {
    println("Foo")
}
func (h *MachineHandlers) BarState(e *am.Event) {
    println("Bar")
}
// ...
// usage
m.Add(am.S{"Foo", "Bar"}, nil) // prints "Bar" and then "Foo"
```

```text
[add] Foo Bar
[state] +Bar +Foo
[handler] BarState
Bar
[handler] FooState
Foo
```

## Queue

The purpose of AsyncMachine is to synchronize actions, which results in only one handler being executed at the same
time. Every mutation happening inside the handler, will be queued and the mutation call will return `Queued`.

Queue itself can be accessed via `Machine.Queue` whereas there's also a helper function `IsQueued`, which checks if a
particular mutation has been queued. It's especially helpful in making decisions based on scheduled actions.

- [go playground](https://goplay.tools/snippet/IClGUHGtNhP)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {},
    "Bar": {}
}, nil)
// ...
// handlers
func (h *MachineHandlers) FooState(e *am.Event) {
    e.Machine.Add(am.S{"Bar"}, nil) // Queued
}
// ...
// usage
m.Add(am.S{"Foo"}, nil) // Executed
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

## Logging

Besides debugging methods, AsyncMachine offer a very verbose logging system with 4 levels of details:

- `LogNothing` (default)
- `LogChanges` state changes and important messages
- `LogOps` detailed relations resolution, called handlers, queued and rejected mutations
- `LogDecisions` more verbose variant of Ops, explaining the reasoning behind
- `LogEverything` useful only for deep debugging

Example of all log level for the same code snippet:

- [go playground](https://goplay.tools/snippet/NJFej5X7Io5)

```go
// machine
m := am.New(ctx, am.States{
    "Foo": {},
    "Bar": {
        Auto: true,
    },
    // disable ID logging
}, &am.Opts{DontLogID: true})
m.SetLogLevel(am.LogOps)
// ...
// handlers
func (h *MachineHandlers) FooState(e *am.Event) {
    // empty
}
func (h *MachineHandlers) BarEnter(e *am.Event) bool {
    return false
}
// ...
// usage
m.Add(am.S{"Foo"}, nil) // Executed
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
[start] handleEmitterLoop MachineHandlers
[add] Foo
[emit:MachineHandlers:d7a58] AnyFoo
[emit:MachineHandlers:d32cd] FooEnter
[state] +Foo
[emit:MachineHandlers:aa38c] FooState
[handler] FooState
[auto] Bar
[add:auto] Bar
[emit:MachineHandlers:f353d] AnyBar
[emit:MachineHandlers:82e34] BarEnter
[handler] BarEnter
[cancel:82e34] (Bar Foo) by BarEnter
```

## Cheatsheet

- State
- Active states
- Called states
- Target states
- Mutation
- Transition
- Accepted transition
- Canceled transition
- Queued transition
- Negotiation handlers
- Final handlers
