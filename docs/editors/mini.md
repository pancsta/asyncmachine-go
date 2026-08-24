## Handlers

### Activation handler with negotiation

```go
// negotiation handler
func (h *Handlers) NameEnter(e *am.Event) bool {}
// final handler
func (h *Handlers) NameState(e *am.Event) {}
```

### De-activation handler with negotiation

```go
// negotiation handler
func (h *Handlers) NameExit(e *am.Event) bool {}
// final handler
func (h *Handlers) NameEnd(e *am.Event) {}
```

### State to state handlers

```go
// with Foo active, can Bar activate? (negotiation)
func (h *Handlers) FooBar(e *am.Event) {}
// with Bar active, can Baz activate? (negotiation)
func (h *Handlers) BarBaz(e *am.Event) {}
```

## Machine

### Common init

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    ss "PACKAGE/states"
)
// ...
mach, err := am.NewCommon(ctx, "mach1", ss.States, ss.Names, nil, nil, &am.Opts{
  LogLevel: am.LogChanges,
  Parent: machParent,
})
```

### Arguments

```go
// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "template"

type Args struct {
  am.ArgsBase `json:"-"`
}

func (Args) ArgsPrefix() string {
  return APrefix
}

// ----- per state def

type ABaz struct {
  // shared pkg args
  Args `json:"-"`
  // Address with logging.
  Addr string `log:"addr"`
}

func (ABaz) ArgsState() string {
  return ss.Baz
}

// ArgsRpc will be available in the aRPC and REPL.
var ArgsRpc = []am.ArgsApi{ABaz{}}

func init() {
  for _, arg := range ArgsRpc {
    gob.Register(arg)
  }
}
```

## Schema

### State definition

```go
am.Schema{
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

### Schema definition

```go
import (
    . "github.com/pancsta/asyncmachine-go/pkg/states/global"
    ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

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

  // inherit from BasicStatesDef
  *states.BasicStatesDef
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
