# Schema Formats

This document contains a brief overview of `asyncmachine-go` schema file formats. All schemas are simply an executable Golang code.

## schema-v0

- type safety **NO**
- reflection **NO**
- bootstrapped **NO**
- inheritance **NO**
- generated **NO**

Schemas for `asyncmachine-go` are optional and for simple things, we don't need a typed schema (nor type safety), and we can construct the machine like so:

```go
mach := newMach("DryWaterWet", am.Schema{
    "Wet": {
        Require: am.S{"Water"},
    },
    "Dry": {
        Remove: am.S{"Water"},
    },
    "Water": {
        Add:    am.S{"Wet"},
        Remove: am.S{"Dry"},
    },
})
```

All the state-related calls happen via raw strings. It's good for prototyping and nothing else.

## Schema-v1

- type safety **YES**
- reflection **NO**
- bootstrapped **NO**
- inheritance **NO**
- generated **NO**

The `v1` is the simplest schema file format:

```go
package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
var States = am.Schema{
	CreatingExpense: {Remove: GroupExpense},
	ExpenseCreated:  {Remove: GroupExpense},
	WaitingForApproval: {
		Auto:   true,
		Remove: GroupApproval,
	},
	ApprovalGranted: {Remove: GroupApproval},
	PaymentInProgress: {
		Auto:   true,
		Remove: GroupPayment,
	},
	PaymentCompleted: {Remove: GroupPayment},
}

// Groups of mutually exclusive states.

var (
	GroupExpense  = S{CreatingExpense, ExpenseCreated}
	GroupApproval = S{WaitingForApproval, ApprovalGranted}
	GroupPayment  = S{PaymentInProgress, PaymentCompleted}
)

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	CreatingExpense    = "CreatingExpense"
	ExpenseCreated     = "ExpenseCreated"
	WaitingForApproval = "WaitingForApproval"
	ApprovalGranted    = "ApprovalGranted"
	PaymentInProgress  = "PaymentInProgress"
	PaymentCompleted   = "PaymentCompleted"
)

// Names is an ordered list of all the state names.
var Names = S{CreatingExpense, ExpenseCreated, WaitingForApproval, ApprovalGranted, PaymentInProgress, PaymentCompleted}

// #endregion
```

Usage:

```go
import ss "github.com/pancsta/asyncmachine-go/examples/temporal_expense/states"

// ...

h := &MachineHandlers{}
mach, err := am.NewCommon(ctx, "expense", ss.Schema, ss.Names, h, nil, nil)
```

## Schema-v2

- type safety **YES**
- reflection **YES**
- bootstrapped **YES**
- inheritance **YES**
- generated **NO**

The `v2` is the actual schema file format schema file format and should be used everywhere. The problem is the use of
reflection that causes issues with **TinyGo**.

```go
import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// ...

// ServerStatesDef contains all the states of the Client state machine.
type ServerStatesDef struct {
	*am.StatesBase

	// basics

	// Ready - Client is fully connected to the server.
	Ready string

	// rpc

	// Starting listening
	RpcStarting string
	// setting up RPC accepting
	RpcAccepting string
	// RPC is accepting or has accepted connections
	RpcReady string

	// RPC client connected (technically)
	ClientConnected string
	// RPC client fully usable
	HandshakeDone string

	// How many times the client requested a full sync.
	MetricSync string
	// TCP tunneled over websocket
	WebSocketTunnel string

	// inherit from SharedStatesDef
	*SharedStatesDef
}

// ServerGroupsDef contains all the state groups of the Client state machine.
type ServerGroupsDef struct {
	*SharedGroupsDef

	// Rpc is a group for RPC ready states.
	Rpc S
}

// ServerSchema represents all relations and properties of ClientStates.
var ServerSchema = SchemaMerge(
	// inherit from SharedStruct
	SharedSchema,
	am.Schema{

		ssS.ErrNetwork: {
			Require: S{am.StateException},
			Remove:  S{ssS.ClientConnected},
		},

		// inject Server states into HandshakeDone
		ssS.HandshakeDone: StateAdd(
			SharedSchema[ssS.HandshakeDone],
			am.State{
				Require: S{ssS.ClientConnected},
				// TODO why?
				Remove: S{Exception},
			}),

		// Server

		ssS.Start: {Add: S{ssS.RpcStarting}},
		ssS.Ready: {
			Auto:    true,
			Require: S{ssS.HandshakeDone, ssS.RpcReady},
		},

		ssS.RpcStarting: {
			Require: S{ssS.Start},
			Remove:  sgS.Rpc,
		},
		ssS.RpcAccepting: {
			Require: S{ssS.Start},
			Remove:  sgS.Rpc,
		},
		ssS.RpcReady: {
			Require: S{ssS.Start},
			Remove:  sgS.Rpc,
		},
		ssS.ClientConnected: {
			Require: S{ssS.RpcReady},
		},

		ssS.MetricSync:      {Multi: true},
		ssS.WebSocketTunnel: {},
	})

// EXPORTS AND GROUPS

var (
	ssS = am.NewStates(ServerStatesDef{})
	sgS = am.NewStateGroups(ServerGroupsDef{
		Rpc: S{ssS.RpcStarting, ssS.RpcAccepting, ssS.RpcReady},
	}, SharedGroups)

	// ServerStates contains all the states for the Client machine.
	ServerStates = ssS
	// ServerGroups contains all the state groups for the Client machine.
	ServerGroups = sgS
)
```

See [`/tools/cmd/am-gen`](/tools/cmd/am-gen/README.md) for schema bootstrapping commands.

## Schema-v3

- type safety **YES**
- reflection **NO**
- bootstrapped **NO**
- inheritance **NO**
- generated **YES**

The `v3` schema file format is a flattened `v2` via a code generation script rooted in each `states` directory. It works
well with **TinyGo** and can also be useful to examine the whole inherited schema in a single file. It's API compatible
with `v2`, so no code changes are required besides adding the `tinygo` build tag.

```go
// TODO rpc.Server example
```

See [`/pkg/machine`](/pkg/machine/README.md) for a generation scripts.
