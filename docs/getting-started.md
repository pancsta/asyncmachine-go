# Getting Started

Getting started with **asyncmachine-go**, in this tutorial we will:

1. [Define a state machine schema](#1-define-schema)
2. [Generate a project boilerplate](#2-generate-a-project-boilerplate)
3. [Run test cases](#3-run-test-cases)
4. [Modify the state machine handlers](#4-modify-the-state-machine-handlers)
5. [Debug the machine](#5-debug-the-machine)
6. [Modify the state via the command line](#6-modify-the-state-via-the-command-line)
7. [Replace a relation with a negotiation handler](#7-replace-relation-with-a-negotiation-handler)

## 0. Prerequisites

We'll use these tools in the tutorial:

```bash
go install github.com/pancsta/asyncmachine-go/tools/cmd/am-gen@latest
go install github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
go install github.com/pancsta/asyncmachine-go/tools/cmd/arpc@latest
```

## 1. Define schema

We can define schemas using CLI params and add relations later, or using a simple YAML file.
Create `schema.yml` with a list of states and some relations:

```yaml
Wet:
  require: [ Water ]
Water:
  remove: [ Dry ]
  add: [ Wet ]
Dry:
  auto: true
  remove: [ Water ]
```

The same state schema without relations can be created via:

```bash
am-gen schema --name MyMach \
  --state Wet --state Water --state Dry:auto \
  --inherit basic --inherit disposed --inherit rpc/statesrc
```

## 2. Generate a project boilerplate

Now let's generate the project's starter kit via:

```bash
am-gen starter-kit schema.yml --name MyMach --uri mymach
```

This will create the following file structure:

```bash
$ tree my_mach
my_mach
├── go.mod
├── handlers.go
├── my_mach.go
├── my_mach_test.go
└── states
    └── ss_my_mach.go

2 directories, 5 files
```

## 3. Run test cases

Run the provided test case for the Start state:

```bash
cd my_mach
go test -v .
```

## 4. Modify the state machine handlers

Replace the following final handlers in `my_mach.go`:

```go
func (h *Handlers) WetState(e *am.Event) {
  fmt.Println("it is wet now")
}

func (h *Handlers) DryState(e *am.Event) {
  fmt.Println("it is dry now")
}
```

## 5. Debug the machine

Start am-dbg on the default port via `am-dbg --output-diagrams 1` and change `isDebug` to `true` in `my_mach.go`, then
`go test .` again. We should see the machine with transitions, tailing the input. The transition step sequence for the
current transition is available at:

- `am-dbg/diagrams/am-vis-steps.d2.svg`
- [localhost:6832/viewer/steps.svg](http://localhost:6832/viewer/steps.svg)
- `am-dbg/tx.md`

## 6. Modify the state via the command line

Create a simple program using the new machine by creating `./cmd/main.go`:

```go
package main

import (
  "context"

  "mymach"
  "mymach/states"
)

var ss = states.MyMachStates

func main() {
  ctx := context.Background()
  h, _ := mymach.New(ctx)
  h.Mach.Add1(ss.Start, nil)
  <-h.Mach.WhenNot1(ss.Start, nil)
}
```

`go run ./cmd` and it should pop up in the debugger, this time in a live session.
Now let's change the state manually via the CLI (or the REPL).

```bash
arpc -f my_mach.addr -- add . Water
arpc -f my_mach.addr -- remove . Water
# REPL via arpc -f my_mach.addr
```

## 7. Replace relation with a negotiation handler

Remove the `Wet:Require` relation in the schema file `states/ss_my_mach.go`, so the state looks like this:

```go
ssM.Wet: {},
```

Replace the `WetEnter` negotiation handler in `my_mach.go` with:

```go
func (h *Handlers) WetEnter(e *am.Event) bool {
  return e.Transition().TimeIndexAfter().Is1(ss.Water)
}
```

Now `go run .` it and verify that points [3](#3-run-test-cases) and [6](#6-modify-the-state-via-the-command-line) give
the same result, but the transition step diagram differs. The transition cancelation causes also show up in the
"Log Reader" pane.
