# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/rpc/HOWTO.md

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a declarative control flow library implementing [AOP](https://en.wikipedia.org/wiki/Aspect-oriented_programming)
> and [Actor Model](https://en.wikipedia.org/wiki/Actor_model) through a **[clock-based state machine](/pkg/machine/README.md)**.

## aRPC Integration Tests Tutorial

Target version: `v0.7.0`. For an up-to-date version, check [the package readme](/pkg/rpc/README.md).

This tutorial will present how to set up, work, and debug asyncmachine RPC (aRPC) servers and clients for integration
tests. It will use [am-dbg](/tools/debugger/README.md) as an example - both an in-memory instance (no UI), and an RPC
worker showing a regular TUI. Code for running both versions will be almost identical, thanks to network transparency.

[![Video Walkthrough](https://pancsta.github.io/assets/asyncmachine-go/rpc-demo1.png)](https://pancsta.github.io/assets/asyncmachine-go/rpc-demo1.mkv)

- top left: worker instance
  - manipulated by the test suite
  - exports telemetry to the debugger instance (bottom left)
  - has fixtures from libp2p-pubsub-simulator
  - receives telemetry from the fixture state machine (`t-TestTailMode`)
  - live view: [wetty](http://188.166.101.108:8080/wetty/ssh/am-dbg?pass=am-dbg:8080/wetty/ssh/am-dbg?pass=am-dbg) or
    `ssh 188.166.101.108 -p 4444`
- bottom left: debugger instance
  - receives telemetry from the worker (`d-rem-worker`)
  - receives telemetry from RPC clients (`c-TestUserFwd` etc)
  - receives telemetry from the RPC server (`s-worker`)
  - receives telemetry from the fixture state machine (`t-TestTailMode`)
  - live view: [wetty](http://188.166.101.108:8081/wetty/ssh/am-dbg?pass=am-dbg:8081/wetty/ssh/am-dbg?pass=am-dbg) or
    `ssh 188.166.101.108 -p 4445`
- right: IDE with the remote test suite from [/tools/debugger/test/remote](/tools/debugger/test/remote/integration_remote_test.go)

Steps in the video:

1. Start the worker instance with telemetry<br />
   `env (cat config/env/debug-telemetry.env) task am-dbg-worker`
2. Worker connects to the debugger instance
3. Run the whole test suite from the IDE
4. All the test clients and 1 fixture state machine connects to the debugger
5. The fixture machine also connects to the worker instance, as this connection is being tested
6. Run the whole test suite again, this time with breakpoints in `TestTailModeRemote`
7. Stepping through breakpoints is reflected in both instances
8. At one point, both instances are showing the same ABCD fixture state machine
9. Second run passes and afterward the debugger instance briefly presents mutations from all the state machines

## Local Version

```go
import (
    amtest "github.com/pancsta/asyncmachine-go/internal/testing"
    amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    "github.com/pancsta/asyncmachine-go/tools/debugger/server"
    ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func TestUserFwd(t *testing.T) {

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    mach := worker.Mach

    // fixtures
    cursorTx := 20
    amhelp.Add1AsyncBlock(ctx, mach, ss.SwitchedClientTx, ss.SwitchingClientTx,
        am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

    // test
    res := amhelp.Add1Block(ctx, mach, ss.UserFwd, nil)

    // assert
    assert.NotEqual(t, res, am.Canceled)
    assert.Equal(t, cursorTx+1, worker.C.CursorTx)
}
```

## RPC Version

```go
func TestUserFwdRemote(t *testing.T) {

    // init rpc
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    c := amtest.NewRpcClient(t, ctx, workerRpcAddr, ss.States, ss.Names)

    // fixtures
    cursorTx := 20
    amhelp.Add1AsyncBlock(ctx, c.Worker, ss.SwitchedClientTx, ss.SwitchingClientTx,
        am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

    // test
    res := amhelp.Add1Block(ctx, c.Worker, ss.UserFwd, nil)

    // assert
    assert.NotEqual(t, res, am.Canceled)
    assert.Equal(t, cursorTx+1, get(t, c, server.GetCursorTx, 0))

    c.Stop(ctx, true)
}
```

The only real difference between these 2 versions is the test init - the local version uses pre-inited worker instance,
while the remote one connects to one via RPC using `NewRpcClient`. Both operate on state machines only, not on the
topmost `Debugger` struct. Additionally, the remote version uses an [RPC getter](/tools/debugger/utils.go) to fetch data
for assertions, as **asyncmachine's** API doesn't expose any data by itself (`Debugger` does). For workflows, aRPC
offers `Server.SendPayload` method, which can be used to push data back from the worker, once the task completed.

## Setting Up

Code below is a simplified version of [`NewTest()`](/pkg/rpc/rpc_test.go), showing how to set up and start both server
and client to expose a worker state machine. It also handles environment-based debugging, but does not include an RPC
getter function.

```go
import (
    "github.com/pancsta/asyncmachine-go/internal/testing/utils"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    arpcstates "github.com/pancsta/asyncmachine-go/pkg/rpc/states"

    "owner/project/states"
)


var ssC = arpcstates.ClientStates
var ssS = arpcstates.ServerStates
var ssW = workerstates.WorkerStates

func main() {
    ctx := context.TODO()
    id := "example"
    worker := am.New(ctx, states.WorkerStruct, nil)
    s := startServer(ctx, ":1234", worker)
    c := startClient(ctx, ":1234", worker)
    <-s.Mach.When1(ssS.Ready, nil)

    // ready, test it

    worker.Add1(ssW.Foo, nil)
    if c.Worker.Is1(ssW.Foo) {
        log.Println("OK")
    }
}

func startServer(ctx context.Context, addr string, worker *am.Machine) (*Server) {

    // server init
    s, err := arpc.NewServer(ctx, addr, id, worker, nil)
    if err != nil {
        panic(err)
    }
    utils.MachDebugEnv(s.Mach)

    // server start
    s.Start()
    <-s.Mach.When1(ssS.RpcReady, nil)

    return s
}

func startClient(ctx context.Context, addr string) (*Client) {

    // client init
    c, err := arpc.NewClient(ctx, addr, id, states.WorkerStruct, ssW.Names())
    if err != nil {
        panic(err)
    }
    utils.MachDebugEnv(c.Mach)

    // client ready
    c.Start()
    <-c.Mach.When1(ssC.Ready, nil)

    return c
}
```

## monorepo

- [`/examples/arpc`](/examples/arpc)
- [`/pkg/rpc/README.md`](/pkg/rpc/README.md)
- [`/examples/benchmark_grpc/README.md`](/examples/benchmark_grpc/README.md)

[Go back to the monorepo root](/README.md) to continue reading.
