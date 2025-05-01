# Cookbook

Cookbook for asyncmachine-go contains numerous copy-pasta snippets of common patterns.

// TODO update to v0.8.0

## ToC

<!-- TOC -->

- [Cookbook](#cookbook)
  - [ToC](#toc)
  - [Activation handler with negotiation](#activation-handler-with-negotiation)
  - [De-activation handler with negotiation](#de-activation-handler-with-negotiation)
  - [State to state handlers](#state-to-state-handlers)
  - [Common imports](#common-imports)
  - [Common env vars](#common-env-vars)
  - [Debugging a machine](#debugging-a-machine)
  - [Simple logging](#simple-logging)
  - [Custom logging](#custom-logging)
  - [Minimal machine init](#minimal-machine-init)
  - [Common machine init](#common-machine-init)
  - [Wait for a state activation](#wait-for-a-state-activation)
  - [Wait for a state activation with argument values](#wait-for-a-state-activation-with-argument-values)
  - [Wait for a state de-activation](#wait-for-a-state-de-activation)
  - [Wait for a specific machine time](#wait-for-a-specific-machine-time)
  - [Wait for a relative state tick](#wait-for-a-relative-state-tick)
  - [Wait for a specific state tick](#wait-for-a-specific-state-tick)
  - [Synchronous state (single)](#synchronous-state-single)
  - [Asynchronous state (double)](#asynchronous-state-double)
  - [Asynchronous boolean state (triple)](#asynchronous-boolean-state-triple)
  - [Full asynchronous boolean state (quadruple)](#full-asynchronous-boolean-state-quadruple)
  - [Input `Multi` state](#input-multi-state)
  - [Self removal state](#self-removal-state)
  - [State context](#state-context)
  - [Step context](#step-context)
  - [Event handlers](#event-handlers)
  - [Channel responses via arguments](#channel-responses-via-arguments)
  - [Equivalent of `select` write with `default`](#equivalent-of-select-write-with-default)
  - [Custom exception handler](#custom-exception-handler)
  - [Verify states at runtime](#verify-states-at-runtime)
  - [Typesafe states pkg template](#typesafe-states-pkg-template)
  - [State Mutex Groups](#state-mutex-groups)
  - [Transition-aware handler](#transition-aware-handler)
  - [Batch data into a single transition](#batch-data-into-a-single-transition)
    - [Switch a state group](#switch-a-state-group)
    - [DiffStates to navigate the flow](#diffstates-to-navigate-the-flow)
    - [Pass data from a negotiation handler to the final handler](#pass-data-from-a-negotiation-handler-to-the-final-handler)
    - [Check if a group od states is active](#check-if-a-group-od-states-is-active)

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
// all state-state handlers are final (non-negotiable)
func (h *Handlers) FooBar(e *am.Event) {}
func (h *Handlers) BarFoo(e *am.Event) {}
func (h *Handlers) BarAny(e *am.Event) {}
func (h *Handlers) AnyFoo(e *am.Event) {}
```

## Global negotiation handler

```go
// always called as a transition from Any state to Any state
func (h *Handlers) AnyEnter(e *am.Event) bool {}
```

## Global final handler

```go
// always called for accepted transitions
func (h *Handlers) AnyState(e *am.Event) {}
```

## Common imports

```go
import (
    amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
)
```

## Common env vars

```shell
# enable simple debugging
# eg long timeouts, stack traces
AM_DEBUG=1

# export telemetry to a real debugger
# address of a running am-dbg instance
AM_DBG_ADDR=localhost:6831

# set the log level (0-4)
AM_LOG=2

# detect evals directly in handlers
# use in tests
AM_DETECT_EVAL=1
```

## Debugging a machine

```bash
$ am-dbg
```

```go
import amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
// ...
ready, err := amtele.TransitionsToDBG(ctx, mach, "")
<-ready
```

```bash
# use AM_DEBUG to increase timeouts of NewCommon machines
AM_DEBUG=1
```

## Simple logging

```go
mach.SetLogLevel(am.LogChanges)
```

## Custom logging

```go
// max the log level
mach.SetLogLevel(am.LogEverything)
// level based dispatcher
mach.SetLogger(func(level LogLevel, msg string, args ...any) {
    if level > am.LogChanges {
        customLogDetails(msg, args...)
        return
    }
    customLog(msg, args...)

})
// include some args in the log and traces
mach.SetLogArgs(am.NewArgsMapper([]string{"id", "name"}, 20))
```

## Minimal machine init

```go
import am "github.com/pancsta/asyncmachine-go/pkg/machine"
// ...
ctx := context.TODO()
states := am.Schema{"Foo":{}, "Bar":{Require: am.S{"Foo"}}}
// state machine
mach := am.New(ctx, states, &am.Opts{
    ID: "mach1",
})
```

## Common machine init

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    // task gen-states Foo,Bar
    ss "repo/project/states"
)
// ...
ctx := context.Background()
mach, err := am.NewCommon(ctx, "mach1", ss.States, ss.Names, nil, nil, &am.Opts{
    LogLevel: am.LogChanges,
})
```

## Wait for a state activation

```go
// create a wait channel
when := mach.When1("FileDownloaded", nil)
// change the state
mach.Add1("DownloadingFile", nil)
// wait with err and timeout
select {
case <-time.After(5 * time.Second):
    return am.ErrTimeout
case <-mach.WhenErr(nil):
    return mach.Err
case <-when:
    // FileDownloaded active
}
```

## Wait for a state activation with argument values

See also:

- [multi states](#input-multi-state)

```go
// define args
args := am.A{"id": 123}
// crate a wait channel
when := mach.WhenArgs("EventConnected", args, nil)
// wait with err and timeout
select {
case <-time.After(5 * time.Second):
    return am.ErrTimeout
case <-mach.WhenErr(nil):
    return mach.Err
case <-when:
    // EventConnected activated with (id=1232)
}
```

## Wait for a state de-activation

```go
// crate a wait channel
when := mach.WhenNot1("DownloadingFile", args, nil)
// wait with err and timeout
select {
case <-time.After(5 * time.Second):
    return am.ErrTimeout
case <-mach.WhenErr(nil):
    return mach.Err
case <-when:
    // DownloadingFile inactive
}
```

## Wait for a specific machine time

```go
// create a wait channel
when := mach.WhenTime(am.S{"DownloadingFile"}, am.Time{6}, nil)
// wait with err and timeout
select {
case <-time.After(5 * time.Second):
    return am.ErrTimeout
case <-mach.WhenErr(nil):
    return mach.Err
case <-when:
    // DownloadingFile has tick >= 6
}
```

## Wait for a relative state tick

```go
// create a wait channel
when := mach.WhenTick("DownloadingFile", 2, nil)
// wait with err and timeout
select {
case <-time.After(5 * time.Second):z
    return am.ErrTimeout
case <-mach.WhenErr(nil):
    return mach.Err
case <-when:
    // DownloadingFile tick has increased by 2
}
```

## Wait for a specific state tick

```go
// create a wait channel
when := mach.WhenTickEq("DownloadingFile", 6, nil)
// wait with err and timeout
select {
case <-time.After(5 * time.Second):
    return am.ErrTimeout
case <-mach.WhenErr(nil):
    return mach.Err
case <-when:
    // DownloadingFile has tick >= 6
}
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

See also:

- [batching input data](#batch-data-into-a-single-transition)
- [waiting on multi states](#wait-for-a-state-activation-with-argument-values)

```go
var States = am.Schema{
    ConnectEvent:    {Multi: true},
}

func (h *Handlers) ConnectEventState(e *am.Event) {
    // called even if the state is already active
}
```

## Self removal state

```go
func (h *Handlers) FwdStepState(_ *am.Event) {
    // removes itself AFTER the end of the handler
    // like defer, but with a queue
    h.Mach.Remove1("FwdStep", nil)
    // ... handler
}
```

## State context

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

## Step context

```go
// create a ctx just for this select statement (step)
ctx, cancel = context.WithCancel(context.Background())
select {
    case <-mach.WhenErr(ctx):
        cancel()
        return mach.Err
    case <-time.After(5 * time.Second):
        cancel()
        return am.ErrTimeout
    case <-mach.When1("Foo", ctx):
    case <-mach.WhenArgs("Bar", am.A{"id": 1}, ctx):
}
// dispose "when" listeners as soon as they aren't needed
cancel()
```

## Event handlers

```go
events := []string{"FileDownloaded", am.EventQueueEnd}
ch := mach.OnEvent(events, nil)
for e, ok := <-ch; ok; {
    if e.Name == EventQueueEnd {
        continue
    }
}
```

## Channel responses via arguments

```go
// buffered channels
req := &myReq{
    resp:  make(chan []string, 1),
    err:   make(chan error, 1),
}
// sync push to the handler
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

// asyncmachine
res := p.mach.Add1("PeersPending", nil)
if res == am.Canceled {
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

## Verify states at runtime

See also:

- [typesafe states pkg](#typesafe-states-pkg-template)

```go
package sim

import (
    ss "URL/states/NAME"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func main() {
    ctx := context.Background()
    m := am.New(ctx, ss.States, nil)
    err := m.VerifyStates(ss.Names)
    if err != nil {
        print(err)
    }
    print(m.Inspect(nil))
}
```

## Typesafe states pkg template

```go
// generate using either
// $ am-gen states-file Foo,Bar
// $ task gen-states-file Foo,Bar
package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
var States = am.Schema{
    Start:     {},
    Heartbeat: {Require: S{Start}},
}

//#region boilerplate defs

// Names of all the states (pkg enum).

const (
    Start             = "Start"
    Heartbeat         = "Heartbeat"
)

// Names is an ordered list of all the state names.
var Names = S{Start, Heartbeat}

//#endregion

```

## State Mutex Groups

```go
var States = am.Schema{
    Connected: {Remove: groupConnected},
    Connecting: {Remove: groupConnected},
    Disconnecting: {Remove: groupConnected},
}

// Only 1 of the group can be active at a given time.
var groupConnected = am.S{Connecting, Connected, Disconnecting}
```

## Transition-aware handler

```go
func (h *Handlers) ClientSelectedEnd(e *am.Event) {
    // clean up, except when switching to SelectingClient
    if e.Mutation().CalledStatesHas(ss.SelectingClient) {
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
            // wait some time
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

### Switch a state group

```go
switch mach.Switch(ss.GroupPlaying...) {
case ss.Playing:
case ss.TailMode:
default:
}
```

### DiffStates to navigate the flow

```go
func (h *Handlers) HelpDialogEnd(e *am.Event) {
    diff := am.DiffStates(ss.GroupDialog, e.Transition().TargetStates)
    if len(diff) == len(ss.GroupDialog) {
        // all dialogs closed, show main
        h.layoutRoot.SendToFront("main")
    }
}
```

### Pass data from a negotiation handler to the final handler

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

### Check if a group of states is active

```go
if d.Mach.Any1(ss.GroupDialog...) {
    d.Mach.Remove(ss.GroupDialog, nil)
    return nil
}
```

### Open Telemetry

```go
import "github.com/pancsta/asyncmachine-go/pkg/telemetry"

// ...

opts := &am.Opts{}
initTracing(ctx)
traceMach(opts, true)
mach, err := am.NewCommon(ctx, "mymach", ss.States, ss.Names, nil, nil, opts)

// ...

func initTracing(ctx context.Context) {
    tracer, provider, err := NewOtelProvider(ctx)
    if err != nil {
        log.Fatal(err)
    }
    _, rootSpan = tracer.Start(ctx, "sim")
}

func traceMach(opts *am.Opts, traceTransitions bool) {
    machTracer := telemetry.NewOtelMachTracer(s.OtelTracer, &telemetry.OtelMachTracerOpts{
        SkipTransitions: !traceTransitions,
        Logf:            logNoop,
    })
    opts.Tracers = []am.Tracer{machTracer}
}
```
