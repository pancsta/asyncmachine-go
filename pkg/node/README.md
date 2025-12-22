# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/node

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

**/pkg/node** provides distributed workflows via state-based orchestration of worker pools. Features a failsafe
supervision, as well as state machines for workers and clients. All actors communicate via [aRPC](/pkg/rpc/README.md),
as each worker is started in a separate OS process.

### Installation

```go
import amnode "github.com/pancsta/asyncmachine-go/pkg/node"
```

## Workflow Rules

- worker can serve only **1 client**
- supervisor can serve **many clients**
- client has a **direct connection** to the worker
- supervisor can supervise only **1 kind** of workers
- client can be connected to **1 supervisor**

> [!NOTE]
> Node Worker and RPC Worker are different things. RPC Worker reports it's state to the RPC Client, which can mutate it,
> while a Node Worker performs work for a Node Client. Node Worker is also an RPC Worker, just like Node Supervisor is.

## Flow

- OS starts the supervisor
- supervisor opens a public port
- supervisor starts workers based on pool criteria
- supervisor connects to all the workers via private ports
- client connects to supervisor's public port and requests a worker
- supervisor confirms with a worker and provides the client with connection info
- client connects to the worker, and delegates work via states
- worker reports state changes to both the client and supervisor
- supervisor maintains the worker, eg triggers log rotation, monitors errors and restarts
- worker delivers payload to the client via ClientSendPayload

<div align="center"><picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/node-worker-pool.mermaid.dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/diagrams/node-worker-pool.mermaid.light.svg">
  <img alt="" src="https://pancsta.github.io/assets/asyncmachine-go/diagrams/node-worker-pool.mermaid.light.svg">
</picture></div>

## Components

### Worker

Any state machine can be exposed as a Node Worker, as long as it implements `/pkg/node/states.WorkerStructDef`. This can
be done either manually, or by using state helpers ([SchemaMerge](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.TimeSum),
[SAdd](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#SAdd)), or by generating a schema file with [am-gen](/tools/cmd/am-gen/README.md).
It's also required to have the states verified by [Machine.VerifyStates](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.VerifyStates).
Worker should respond to `WorkRequested` and produce `ClientSendPayload` to send data to the client.

- [schema file](/pkg/node/states/ss_node_worker.go)

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    amnode "github.com/pancsta/asyncmachine-go/pkg/node"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

// ...

type workerHandlers struct {
    t *testing.T
}

func (w *workerHandlers) WorkRequestedState(e *am.Event) {
    // client-defined input
    input := e.Args["input"].(int)

    // create payload
    payload := &rpc.ArgsPayload{
        Name:   "mypayload",
        Data:   input * input,
        Source: e.Machine.ID,
    }

    // send payload
    e.Machine.Add1(ssW.ClientSendPayload, arpc.Pass(&arpc.A{
        Name:    payload.Name,
        Payload: payload,
    }))
}

// ...

// inherit from Node worker
schema := am.SchemaMerge(ssnode.WorkerStruct, am.Schema{
    "Foo": {Require: am.S{"Bar"}},
    "Bar": {},
})
names := am.SAdd(ssnode.WorkerStates.Names(), am.S{"Foo", "Bar"})

// init
mach := am.New(ctx, schema, nil)
mach.VerifyStates(names)
worker, err := NewWorker(ctx, workerKind, mach.Schema(), mach.StateNames(), nil)
```

#### Worker Schema

State schema from [/pkg/node/states/ss_node_worker.go](/pkg/node/states/ss_node_worker.go).

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/schemas/node-worker.svg?raw=true">
        <img style="min-height: 201px"
            src="https://pancsta.github.io/assets/asyncmachine-go/schemas/node-worker.svg?raw=true"
            alt="worker schema" />
    </a>
</div>

### Supervisor

Supervisor needs a path to the worker's binary (with optional parameters) for `exec.Command`. It exposes states like
`Ready`, `PoolReady`, `WorkersAvailable` and awaits `ProvideWorker`.

- [schema file](/pkg/node/states/ss_supervisor.go)

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    amnode "github.com/pancsta/asyncmachine-go/pkg/node"
)

// ...

// var names am.S
// var schema am.Schema
// var workerKind string
// var workerBin []string

// supervisor
super, err := amnode.NewSupervisor(ctx, workerKind, workerBin, schema, nil)
if err != nil {
    t.Fatal(err)
}
super.Start("localhost:1234")
err := amhelp.WaitForAll(ctx, 2*time.Second,
    super.When1(ssnode.SupervisorStates.PoolReady, ctx))
```

#### Supervisor Schema

State schema from [/pkg/node/states/ss_supervisor.go](/pkg/node/states/ss_supervisor.go).

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/schemas/node-supervisor.svg?raw=true">
        <img style="min-height: 302px"
            src="https://pancsta.github.io/assets/asyncmachine-go/schemas/node-supervisor.svg?raw=true"
            alt="worker schema" />
    </a>
</div>

## Client

Any state machine can be a Node Client, as long as it implements `/pkg/node/states.ClientStructDef`. This can be done
either manually, or by using state helpers ([SchemaMerge](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.TimeSum),
[SAdd](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#SAdd)), or by generating a schema file with [am-gen](/tools/cmd/am-gen/README.md).
Client also needs to know his worker's machine schema. To connect to the worker pool, client accepts a list
of supervisor addresses (PubSub discovery in on the roadmap), and will be trying to connect to them in order. After
`SuperReady` activates, client can call [`Client.ReqWorker(ctx)`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Client.ReqWorker),
which will request a worker from the supervisor, resulting in `WorkerReady`. At this point client can access the worker
at [`Client.WorkerRpc.NetMach`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/rpc#Client), then add the
`WorkRequested` state multiple times, and handle `WorkerPayload`.

- [schema file](/pkg/node/states/ss_node_client.go)

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    amnode "github.com/pancsta/asyncmachine-go/pkg/node"
)

// ...

// var workerKind string
// var workerSchema am.Schema
// var superAddrs []string

// inherit from Node client
schema := am.SchemaMerge(ssnode.ClientSchema, am.Schema{
    "Foo": {Require: am.S{"Bar"}},
    "Bar": {},
})
names := am.SAdd(ssnode.ClientStates.Names(), am.S{"Foo", "Bar"})

// describe client
opts := &amnode.ClientOpts{
    ClientSchema:  schema,
    ClientStates:  names,
}

// init
client, err := amnode.NewClient(ctx, "myclient", workerKind, workerSchema, opts)
client.Start(superAddrs)
err := amhelp.WaitForAll(ctx, 2*time.Second,
    super.When1(ssnode.ClientStates.SuperReady, ctx))

// request a worker
client.ReqWorker(ctx)
err := amhelp.WaitForAll(ctx, 2*time.Second,
    super.When1(ssnode.ClientStates.WorkerReady, ctx))
worker := client.WorkerRpc.NetMach
worker.Add1(ssnode.WorkerStates.WorkRequested, am.A{"input": 2})
```

#### Client Schema

State schema from [/pkg/node/states/ss_node_client.go](/pkg/node/states/ss_node_client.go).

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/schemas/node-client.svg?raw=true">
        <img style="min-height: 327px"
            src="https://pancsta.github.io/assets/asyncmachine-go/schemas/node-client.svg?raw=true"
            alt="worker schema" />
    </a>
</div>

## TODO

- supervisor redundancy
- connecting to a pool of supervisors +rotation

## Documentation

- [api /pkg/node](https://code.asyncmachine.dev/pkg/github.com/pancsta/asyncmachine-go/pkg/node.html)
- [godoc /pkg/node](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/node)
- [diagrams](/docs/diagrams.md#worker-pool-architecture)

## Tests

At this point only a test suite exists, but a reference implementation is on the way. See [/pkg/node/node_test.go](/pkg/node/node_test.go),
eg `TestClientWorkerPayload` and uncomment `amhelp.EnableDebugging`. Below a command to run an exported debugging
session (no installation needed).

```bash
go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest \
  --import-data https://pancsta.github.io/assets/asyncmachine-go/am-dbg-exports/node-payload.gob.br
```

## Status

Alpha, work in progress, not semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
