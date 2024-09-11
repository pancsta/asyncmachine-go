# aRPC Tutorial

[-> go back to monorepo /](/README.md)

This tutorial will present how to set up, work, and debug asyncmachine RPC (aRPC) servers and clients. The example used
will be integration tests having, both an in-memory instance (no UI), and an RPC worker showing a regular TUI. Code for
running both versions will be almost identical, thanks to network transparency.

[![Video Walkthrough](https://pancsta.github.io/assets/asyncmachine-go/rpc-demo1.png)](https://pancsta.github.io/assets/asyncmachine-go/rpc-demo1.m4v)

- top left: worker instance
  - manipulated by the test suite
  - exports telemetry to the debugger instance
- bottom left: debugger instance
  - receives telemetry from the worker (`d-rem-worker`)
  - receives telemetry from RPC clients (`c-TestUserFwd`)
  - receives telemetry from the RPC server (`s-worker`)
  - receives telemetry from the fixture state machine (`t-TestTailMode`)
- right: IDE with the remote test suite from [/tools/debugger/test/remote](/tools/debugger/test/remote/integration_remote_test.go)

Steps in the video:

1. Start the worker instance with telemetry
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
    amh "github.com/pancsta/asyncmachine-go/pkg/helpers"
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
    amh.Add1AsyncBlock(ctx, mach, ss.SwitchedClientTx, ss.SwitchingClientTx,
        am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

    // test
    res := amh.Add1Block(ctx, mach, ss.UserFwd, nil)

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
    amh.Add1AsyncBlock(ctx, c.Worker, ss.SwitchedClientTx, ss.SwitchingClientTx,
        am.A{"Client.id": "sim", "Client.cursorTx": cursorTx})

    // test
    res := amh.Add1Block(ctx, c.Worker, ss.UserFwd, nil)

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
    ss "github.com/pancsta/asyncmachine-go/internal/testing/states"
    "github.com/pancsta/asyncmachine-go/internal/testing/utils"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    ssCli "github.com/pancsta/asyncmachine-go/pkg/rpc/states/client"
    ssSrv "github.com/pancsta/asyncmachine-go/pkg/rpc/states/server"
)

func setup(ctx context.Context, addr string, worker *am.Machine) (*Server, *Client) {

    // read env
    amDbgAddr := os.Getenv("AM_DBG_ADDR")
    logLvl := am.EnvLogLevel("")

    // server init
    s, err := NewServer(ctx, addr, t.Name(), worker, nil)
    if err != nil {
        t.Fatal(err)
    }
    utils.MachDebug(s.Mach, amDbgAddr, logLvl, true)

    // client init
    c, err := NewClient(ctx, addr, t.Name(), worker.GetStruct(),
        worker.StateNames())
    if err != nil {
        t.Fatal(err)
    }
    utils.MachDebug(c.Mach, amDbgAddr, logLvl, true)

    // server start
    s.Start()
    <-s.Mach.When1(ssSrv.RpcReady, readyCtx)

    // client ready
    c.Start()
    <-c.Mach.When1(ssCli.Ready, readyCtx)

    // server ready
    <-s.Mach.When1(ssSrv.Ready, readyCtx)

    return s, c
}
```

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
