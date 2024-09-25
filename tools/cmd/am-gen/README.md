# /tools/cmd/am-gen

[-> go back to monorepo /](/README.md)

> [!NOTE]
> **asyncmachine** can transform blocking APIs into controllable state machines with ease. It shares similarities with
> [Ergo's](https://github.com/ergo-services/ergo) actor model, and focuses on distributed workflows like [Temporal](https://github.com/temporalio/temporal).
> It's lightweight and most features are optional.

`am-gen` will quickly bootstrap a typesafe states file for you.

`$ am-gen states-file Start,Heartbeat`

## Installation

`go install github.com/pancsta/asyncmachine-go/tools/am-gen@latest`

## Example File

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

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
