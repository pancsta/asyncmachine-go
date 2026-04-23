# Cookbook

This cookbook containing numerous copy-pasta snippets of common patterns for **asyncmachine-go**; version `v0.18.5`.
See also:

- [/examples/basic](/examples/basic/basic.go)
- [/examples/mach_template](/examples/mach_template/mach_template.go)
- [/docs/env-configs.md](/docs/env-configs.md)

<!-- TOC -->

- [Cookbook](#cookbook)
  - [Activation handler with negotiation](#activation-handler-with-negotiation)
  - [De-activation handler with negotiation](#de-activation-handler-with-negotiation)
  - [State to state handlers](#state-to-state-handlers)
  - [Global negotiation handler](#global-negotiation-handler)
  - [Global final handler](#global-final-handler)
  - [Common imports](#common-imports)
  - [Common env vars](#common-env-vars)
  - [Debugging a machine](#debugging-a-machine)
  - [Enable env debugging](#enable-env-debugging)
  - [Simple logging](#simple-logging)
  - [Custom logging](#custom-logging)
  - [Logging args](#logging-args)
  - [Minimal machine init](#minimal-machine-init)
  - [Common machine init](#common-machine-init)
  - [Waiting (subscriptions)](#waiting-subscriptions)
  - [State Targeting](#state-targeting)
  - [Synchronous state (single)](#synchronous-state-single)
  - [Asynchronous state (double)](#asynchronous-state-double)
  - [Asynchronous boolean state (triple)](#asynchronous-boolean-state-triple)
  - [Full asynchronous boolean state (quadruple)](#full-asynchronous-boolean-state-quadruple)
  - [Input `Multi` state](#input-multi-state)
  - [Throttled Input `Multi` state](#throttled-input-multi-state)
  - [Self removal state](#self-removal-state)
  - [State context fork (raw)](#state-context-fork-raw)
  - [State context fork (sugar)](#state-context-fork-sugar)
  - [Step context (raw)](#step-context-raw)
  - [Step context (sugar)](#step-context-sugar)
  - [Nested forking](#nested-forking)
  - [Wait for multiple subscriptions](#wait-for-multiple-subscriptions)
  - [Channel responses via arguments](#channel-responses-via-arguments)
  - [Equivalent of `select` write with `default`](#equivalent-of-select-write-with-default)
  - [Custom exception handler](#custom-exception-handler)
  - [State definition](#state-definition)
  - [Schema template](#schema-template)
  - [Transition-aware handler](#transition-aware-handler)
  - [Batch data into a single transition](#batch-data-into-a-single-transition)
  - [Switch a state group](#switch-a-state-group)
  - [DiffStates to navigate the flow](#diffstates-to-navigate-the-flow)
  - [Pass data from a negotiation handler to the final handler](#pass-data-from-a-negotiation-handler-to-the-final-handler)
  - [Check if a part of a group of states is active](#check-if-a-part-of-a-group-of-states-is-active)
  - [Open Telemetry](#open-telemetry)
  - [RPC Server](#rpc-server)
  - [RPC Client](#rpc-client)
  - [RPC Multiplexer](#rpc-multiplexer)
  - [RPC Multiplexer with custom server](#rpc-multiplexer-with-custom-server)
  - [RPC REPL with args name completion](#rpc-repl-with-args-name-completion)
  - [Pass typesafe args](#pass-typesafe-args)
  - [Parse typesafe args](#parse-typesafe-args)
  - [Validate args in negotiation](#validate-args-in-negotiation)
  - [Error handling](#error-handling)
  - [Error setters](#error-setters)
  - [Block until state is added](#block-until-state-is-added)
  - [Block until async state is added](#block-until-async-state-is-added)
  - [Async disposal handlers](#async-disposal-handlers)
  - [Traced mutations](#traced-mutations)

<!-- TOC -->

## Activation handler with negotiation

```go
// negotiation handler
func (h *Handlers) NameEnter(e *am.Event) bool {}
// final handler
func (h *Handlers) NameState(e *am.Event) {}
```

## De-activation handler with negotiation

```go
// negotiation handler
func (h *Handlers) NameExit(e *am.Event) bool {}
// final handler
func (h *Handlers) NameEnd(e *am.Event) {}
```

## State to state handlers

```go
// with Foo active, can Bar activate? (negotiation)
func (h *Handlers) FooBar(e *am.Event) {}
// with Bar active, can Baz activate? (negotiation)
func (h *Handlers) BarBaz(e *am.Event) {}
```

## Global negotiation handler

```go
// called at the end of negotiation
func (h *Handlers) AnyEnter(e *am.Event) bool {}
```

## Global final handler

```go
// called as the last final handler
func (h *Handlers) AnyState(e *am.Event) {}
```

## Common imports

```go
import (
    amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
    amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
)
```

## Common env vars

```bash
AM_DEBUG=1
AM_DBG_ADDR=1
AM_LOG=3
AM_LOG_FULL=1
AM_HEALTHCHECK=1
AM_SERVICE=orchestrator
AM_OTEL_TRACE=1
AM_OTEL_TRACE_TXS=1
AM_OTEL_TRACE_SKIP_STATES_RE=(^rm-|relay-|rc-)|(RegisterDisposal$)
#
#AM_RPC_LOG_SERVER=1
#AM_RPC_LOG_CLIENT=1
#AM_RPC_LOG_MUX=1
#AM_RPC_DBG=1
#AM_RELAY_DBG=1
```

## Debugging a machine

```bash
am-dbg --dir tmp
```

```go
amhelp.MachDebugEnv(mach)
```

## Enable env debugging

```go
// instead of setting using .env
amhelp.EnableDebugging(false)
```

```go
amhelp.MachDebugEnv(mach)
```

## Simple logging

```go
mach.SemLogger().SetLevel(am.LogChanges)
```

## Custom logging

```go
// max the log level
mach.SemLogger().SetLevel(am.LogEverything)
// level based dispatcher
mach.SemLogger().SetLogger(func(level LogLevel, msg string, args ...any) {
    if level > am.LogChanges {
        customLogDetails(msg, args...)
        return
    }
    customLog(msg, args...)

})
```

## Logging args

```go
// custom list
mach.SemLogger().SetArgsMapper(am.NewLogArgsMapper(0, []string{"id", "name"}))
// defaults + custom
mach.SemLogger().SetArgsMapperDef("id", "name")
// log typed args
mach.SemLogger().SetArgsMapper(LogArgs)
```

## Minimal machine init

```go
import am "github.com/pancsta/asyncmachine-go/pkg/machine"
// ...
states := am.Schema{"Foo":{}, "Bar":{Require: am.S{"Foo"}}}
mach := am.New(ctx, states, &am.Opts{
    Id: "mach1",
})
```

## Common machine init

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    ss "PACKAGE/states"
)
// ...
mach, err := am.NewCommon(ctx, "mach1", ss.Schema, ss.Names(), nil, nil, &am.Opts{
    LogLevel: am.LogChanges,
    Parent: machParent,
})
```

## Waiting (subscriptions)

```go
// wait until Foo becomes active
<-mach.When1("Foo", nil)

// wait until Foo becomes inactive
<-mach.WhenNot1("Foo", nil)

// wait for Foo to be activated with an arg ID=123
<-mach.WhenArgs("Foo", am.A{"ID": 123}, nil)

// wait for Foo to have a tick >= 6
<-mach.WhenTime1("Foo", 6, nil)

// wait for Foo to have a tick >= 6 and Bar tick >= 10
<-mach.WhenTime(am.S{"Foo", "Bar"}, am.Time{6, 10}, nil)

// wait for Foo to have a tick increased by 2
<-mach.WhenTicks("Foo", 2, nil)

// wait for a mutation to execute
<-mach.WhenQueue(mach.Add1("Foo", nil))

// wait for an error
<-mach.WhenErr(nil)

// wait on a time query
<-mach.WhenQuery(func(c am.Clock) bool {
    // Foo activated >5 times and Bar activated twice as much
    return c["Foo"] >= 10 && c["Bar"] >= 2*c["Foo"]
}, nil)
```

## State Targeting

```go
var (
    tx am.Transition
    t1 am.Time
    t2 am.Time
)

// was Foo added and Bar removed?
added, removed := tx.TimeIndexDiff()
added.Is1("Foo") && removed.Is1("Bar")

// was Foo called?
tx.TimeIndexCalled().Is1("Foo")

// was Foo active before?
tx.TimeIndexBefore().Is1("Foo")

// will Foo be active after?
tx.TimeIndexAfter().Is1("Foo")

// is Foo queued?
mach.WillBe1("Foo")

// number of states which ticked
len(tx.TimeIndexTimeDiff().ActiveStates())

// did Foo tick between t1 and t2?
t2.DiffSince(t1).NonZeroStates().Is1("Foo")
```

## Synchronous state (single)

```go
Ready: {},
```

## Asynchronous state (double)

```go
DownloadingFile: {
    Remove: groupFileDownloaded,
},
FileDownloaded: {
    Remove: groupFileDownloaded,
},
```

## Asynchronous boolean state (triple)

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

## Full asynchronous boolean state (quadruple)

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
    Auto: 1,
    Remove: groupConnected,
},
```

## Input `Multi` state

```go
var States = am.Schema{
    ConnectEvent:    {Multi: true},
}
// ...
// called even if the state is already active
func (h *Handlers) ConnectEventState(e *am.Event) {}
```

## Throttled Input `Multi` state

```go
var States = am.Schema{
    ConnectEvent:    {Multi: true},
}
// ...
mach.PoolSetLimit("ConnectEvent", 10)
// ...
func (h *Handlers) ConnectEventState(e *am.Event) {
// called even if the state is already active
ctx := h.mach.NewStateCtx("Start")
  mach.PoolFork(ctx, e, func() {
    // max 10 concurrent forks
  })
}
```

## Self removal state

```go
func (h *Handlers) FwdStepState(_ *am.Event) {
    // removes itself AFTER the end of the handler
    // like defer, but with a queue
    h.Mach.Remove1("FwdStep", nil)
}
```

## State context fork (raw)

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
        print("foo")
    }()
}
```

## State context fork (sugar)

```go
// sugar (preferred)
func (h *Handlers) DownloadingFileState(e *am.Event) {
    // open until the state remains active
    ctx := h.mach.NewStateCtx("DownloadingFile")
    // fork to unblock
    h.mach.Fork(ctx, e, func() {
        print("foo")
    })
}
```

## Step context (raw)

```go
var ctx context.Context
// create a ctx just for this select statement (step)
ctxStep, cancelStep = context.WithCancel(context.Background())
defer cancel()
select {
    case ctx.Err() != nil:
        return nil // expired
    case <-mach.WhenErr(ctxStep):
        return mach.Err
    case <-time.After(5 * time.Second):
        return am.ErrTimeout
    case <-mach.When1("Foo", ctxStep):
    case <-mach.WhenArgs("Bar", am.A{"id": 1}, ctxStep):
}
// dispose "when" listeners as soon as they aren't needed
cancel()
```

## Step context (sugar)

```go
var ctx context.Context
// create a ctx just for this select statement (step)
ctxStep, cancelStep = context.WithCancel(context.Background())
err := amhelp.WaitForErrAny(ctx, 5 * time.Second, mach,
    mach.When1("Foo", ctxStep),
    mach.WhenArgs("Bar", am.A{"id": 1}, ctxStep),
)
// dispose "when" listeners as soon as they aren't needed
cancelStep()
switch {
    case ctx.Err() != nil:
        return nil // expired
    case err != nil {
        // includes mach.Err() if mach.IsErr()
        return err
    }
}
// either Foo or Bar[id=1] active
```

## Nested forking

```go
func (h *Handlers) DownloadingFileState(e *am.Event) {
    ctx := h.mach.NewStateCtx("DownloadingFile")
    h.mach.Fork(ctx, e, func() {
        print("DownloadingFileState.Fork")
        // nested unblocking goes without [e], which is not valid at this point
        h.Mach.Go(ctx, func() {
            fmt.Println("DownloadingFileState.Go")
        })
    })
}
```

## Wait for multiple subscriptions

```go
// create a ctx just for this select statement (step)
err := amhelp.WaitForAll(ctx, 5 * time.Second,
    mach.When1("Foo", ctx),
    mach.WhenArgs("Bar", am.A{"id": 1}, ctx),
)
switch {
    case ctx.Err() != nil:
        return nil // expired
    case err != nil {
        return err
    case mach.IsErr() != nil {
        return mach.Err()
    }
}
// both Foo or Bar[id=1] active
```

## Channel responses via arguments

```go
// buffered channels
req := &myReq{
    resp:  make(chan []string, 1),
    err:   make(chan error, 1),
}
defer req.resp.Close()
defer req.err.Close()
// async push to the handler
mach.Add1("GetPeers", am.A{"myReq": req})
// await resp and err
select {
case resp := <-req.resp:
    return resp, nil
case err := <-req.err:
    return nil, err
case <-mach.Ctx.Done():
    return nil, mach.Ctx.Err()
```

```go
func (p *PubSub) GetPeersState(e *am.Event) {
    p.Mach.Remove1(ss.GetPeers, nil)

    req := e.Args["myReq"].(*myReq)

    // ...

    // buffered
    req.resp <- out
}
```

## Equivalent of `select` write with `default`

```go
// vanilla
select {
case p.newPeers <- struct{}{}:
default:
    // canceled
}

// asyncmachine (empty queue)
res := mach.Add1("PeersPending", nil)
if res == am.Canceled {
    // canceled
}

// asyncmachine (with queue)
<-mach.WhenQueue(
    mach.Add1("PeersPending", nil)
)
if mach.Not1("PeersPending") {
    // canceled
}
```

## Custom exception handler

```go
type Handlers struct {
    *am.ExceptionHandler
}

func (h *Handlers) ExceptionState(e *am.Event) {
    // custom handling logic
    // ...

    // call the parent error handler (or not)
    h.ExceptionHandler.ExceptionState(e)
}
```

## State definition

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

## Schema template

```go
// BasicStatesDef contains all the states of the Basic state machine.
type BasicStatesDef struct {
    *am.StatesBase

    // ErrNetwork indicates a generic network error.
    ErrNetwork string
    // ErrHandlerTimeout indicates one of state machine handlers has timed out.
    ErrHandlerTimeout string

    // Start indicates the machine should be working. Removing start can force
    // stop the machine.
    Start string
    // Ready indicates the machine meets criteria to perform work, and requires
    // Start.
    Ready string
    // Healthcheck is a periodic request making sure that the machine is still
    // alive.
    Healthcheck string
}

var BasicSchema = am.Schema{

    // Errors

    ssB.ErrNetwork:        {Require: S{Exception}},
    ssB.ErrHandlerTimeout: {Require: S{Exception}},

    // Basics

    ssB.Start:       {},
    ssB.Ready:       {Require: S{ssB.Start}},
    ssB.Healthcheck: {},
}
```

## Transition-aware handler

See also [State Targeting](#state-targeting).

```go
func (h *Handlers) ClientSelectedEnd(e *am.Event) {
    // clean up, except when switching to SelectingClient
    if e.Transition().TimeIndexCalled().Is1(ss.SelectingClient) {
        return
    }
    h.cleanUp()
}
```

## Batch data into a single transition

```go
var queue []*Msg
var queueMx sync.Mutex
var scheduled bool

func Msg(msgTx *Msg) {
    queueMx.Lock()
    defer queueMx.Unlock()

    if !scheduled {
        scheduled = true
        go func() {
            // wait
            time.Sleep(time.Second)

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

## Switch a state group

```go
switch mach.Switch(ss.GroupPlaying) {
case ss.Playing:
case ss.TailMode:
default:
}
```

## DiffStates to navigate the flow

```go
func (h *Handlers) HelpDialogEnd(e *am.Event) {
    diff := am.StatesDiff(ss.GroupDialog, e.Transition().TargetStates())
    if len(diff) == len(ss.GroupDialog) {
        // all dialogs closed, show main
        h.layoutRoot.SendToFront("main")
    }
}
```

## Pass data from a negotiation handler to the final handler

Not recommended...

```go

func (s *Sim) MsgRandomTopicEnter(e *am.Event) bool {

    p := s.pickRandPeer()
    if p == nil || len(p.simTopics) == 0 {
        // not enough topics
        return false
    }
    randTopic := p.simTopics[rand.Intn(len(p.simTopics))]

    if len(s.GetTopicPeers(randTopic)) < 2 {
        // Not enough peers in topic
        return false
    }

    // pass the topic
    e.Args["Topic.id"] = randTopic

    return len(s.topics) > 0
}
```

## Check if a part of a group of states is active

```go
if d.Mach.Any1(ss.GroupDialog...) {
    d.Mach.Remove(ss.GroupDialog, nil)
    return nil
}
```

## Open Telemetry

```go
err := amtele.MachBindOtelEnv(mach)
if err != nil {
    return err
}
```

## RPC Server

```go
srv, err := arpc.NewServer(ctx, addr, "server", sourceMach, &arpc.ServerOpts{
    Parent: machParent,
})
if err != nil {
    panic(err)
}

// start
srv.Start()
err = amhelp.WaitForAll(ctx, 2*time.Second,
    srv.Mach.When1(ssrpc.ServerStates.RpcReady, ctx))
if ctx.Err() != nil {
    return
}
if err != nil {
    return err
}
```

## RPC Client

```go
client, err := arpc.NewClient(ctx, addr, "clientid", schema, &arpc.ClientOpts{
    Consumer: consumer,
    Parent: machParent,
})

// start
client.Start()
err := amhelp.WaitForAll(ctx, 2*time.Second,
    c.Mach.When1(ssrpc.ClientStates.Ready, ctx))
if ctx.Err() != nil {
    return
}
```

## RPC Multiplexer

```go
mux, err := arpc.NewMux(ctx, addr, source.Id()), sourceMach, &arpc.MuxOpts{
    Parent: parentMach,
}
mux.Listener = listener // or mux.Addr := ":1234"
mux.Start()
err := amhelp.WaitForAll(ctx, 2*time.Second,
    mux.Mach.When1(ssrpc.MuxStates.Ready, ctx))
```

## RPC Multiplexer with custom server

```go
mux, err := arpc.NewMux(ctx, cfg.Web.AddrAgent(), "server-"+mach.Id(), mach, &arpc.MuxOpts{
    Parent: mach,
    NewServerFn: func(mux *arpc.Mux, id string, conn net.Conn) (*arpc.Server, error) {
        srv, err := mux.NewDefaultServer(id)
        if err != nil {
            return nil, err
        }

        // instant push
        srv.PushInterval.Store(new(10 * time.Millisecond))
        srv.Conn = conn
        srv.Start(e)

        return srv, nil
    },
})
```

## RPC REPL with args name completion

```go
arpc.MachReplEnv(mach, &arpc.ReplOpts{
    AddrDir:  "tmp",
    Args:     ARpc{},
    ParseRpc: ParseRpc,
})
```

## Pass typesafe args

```go
// Example with typed state names (ss) and typed arguments (A).
mach.Add1(ss.KillingWorker, Pass(&A{
    ConnAddr:   ":5555",
    WorkerAddr: ":5556",
}))
```

## Parse typesafe args

```go
func (p *BasePage) ConfigState(e *am.Event) {
    args := ParseArgs(e.Args)

    p.boot.Config = args.Config
}
```

## Validate args in negotiation

```go
func (p *BasePage) ConfigEnter(e *am.Event) bool {
    return ParseArgs(e.Args.Config) != nil
}

func (p *BasePage) ConfigState(e *am.Event) {
    args := ParseArgs(e.Args)

    p.boot.Config = args.Config
}
```

## Error handling

```go
err := t.tab.Runtime.Enable(ctx)
// check ctx first
if ctx.Err() != nil {
    return // expired
}
// check err second
if err != nil {
    // mutation to the Exception state
    mach.EvAddErrState(e, ss.ErrConnecting, err, nil)
    return
}
```

## Error setters

```go
// AddErr adds [ErrWeb].
func AddErr(
    event *am.Event, mach *am.Machine, err error, args ...am.A,
) am.Result {
    if err == nil {
        return am.Executed
    }
    err = fmt.Errorf("%w: %w", ErrWeb, err)

    return mach.EvAddErrState(event, ss.ErrWeb, err, am.OptArgs(args))
}
```

## Block until state is added

```go
amhelp.Add1Sync(ctx, mach, ss.Foo, args)
```

## Block until async state is added

```go
// wait for Foo, by adding Bar
amhelp.Add1Async(ctx, mach, ss.Foo, ss.Bar, args)
```

## Async disposal handlers

```go
type Dispatcher struct {
  *am.ExceptionHandler
  *ssam.DisposedHandlers
}

func NewDispatcher() *Dispatcher {
  a := &Dispatcher{
      // init required
      DisposedHandlers: &ssam.DisposedHandlers{},
  }
}

// ...

var dispose am.HandlerDispose = func(id string, ctx context.Context) {
    pt.Close()
}
mach.Add1(ssam.DisposedStates.RegisterDisposal, am.A{
    ssam.DisposedArgHandler: dispose,
})

// ...

func (h *Dispatcher) DisposingState(e *am.Event) {
    // self disposal
    for _, client := range h.clients {
        client.NetMach.Add1(ssam.DisposedStates.Disposing, nil)
    }
    // call super
    h.DisposedHandlers.DisposingState(e)
}
```

## Traced mutations

```go
// pass [e] as the first param
func (a *Agent) StartState(e *am.Event) {
  mach.EvAdd1(e, ss.Mock, nil)
  mach.EvRemove1(e, s.State, nil)
  // only mutates if err != nil
  mach.EvAddErr(e,
    mach.BindHandlers(a.handlersWeb), nil)
}
```

## Eval getter

```go
tx, err := amhelp.EvalGetter(ctx, "PrevTx", 3, mach,
    func() (*dbg.DbgMsgTx, error) {
        return d.hPrevTx(), nil
    })
```
