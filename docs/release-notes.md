# Release Notes

- [CHANGELOG.md](../CHANGELOG.md)

---

## [v0.19.1](https://github.com/pancsta/asyncmachine-go/releases/tag/v0.19.1) (2026-06-23)

The highlight of this release is state-zoom diagram.

<img width="1399" height="223" alt="ss-2026-06-19-20-28-56" src="https://github.com/user-attachments/assets/3d34354c-a71f-4ae4-acf5-f6066d97aa14" />




### feat: release v0.19.1

PR: [\#448](https://github.com/pancsta/asyncmachine-go/pull/448) (@pancsta)

The remainder of `v0.19.1` is in this PR.

### feat\(am-dbg\): release am-dbg v0.19.1

PR: [\#447](https://github.com/pancsta/asyncmachine-go/pull/447) (@pancsta)

The remainder of am-dbg `v0.19.1` is in this PR.

### feat\(am-dbg\): add loading spinner

PR: [\#446](https://github.com/pancsta/asyncmachine-go/pull/446) (@pancsta)

Because diagrams take a very long time to initially generate, the spinner will expose it to the user.

### feat\(am-dbg\): unify diagram viewer for graph, mach, state, steps

PR: [\#445](https://github.com/pancsta/asyncmachine-go/pull/445) (@pancsta)

- feat(am-dbg): add state "zoom" diagrams
- feat(am-dbg): filter steps diagrams with a selected group

The web viewer handles all 4 types of diagrams, and the Chrome extension isn't necessary anymore.
 
State-zoom diagram: a new type of diagrams for large schemas which show the selected state and neighbors of it's neighbors (max graph transversal - 2 relation edges). The state and its relations are enlarged (thus the name).

Steps diagram: the states visible on the transition sequence diagrams will now honor the **diag-group** button and only render the selected group.

### feat\(machine\): enable state ctx for inactive states

PR: [\#444](https://github.com/pancsta/asyncmachine-go/pull/444) (@pancsta)

This is needed to properly cancel `*End()` handlers.


---

## [v0.19.0](https://github.com/pancsta/asyncmachine-go/releases/tag/v0.19.0) (2026-06-11)

The highlight of this release are generic typed args.

```go
// common def

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
```



### feat: release v0.19.0

PR: [\#442](https://github.com/pancsta/asyncmachine-go/pull/442) (@pancsta)

- feat(machine): add `LogEv()`, `LogCtx()` for traced external logging lines
- feat(machine): log sources of panics in Go()
- test: fix test runner timeouts and coverage gen
- fix(rpc): fix `ActiveStates` states param
- feat(helpers): `RemoveSync` and `RemoveSync` return bool

The remainder of `v0.19.0` is in this PR.

### fix\(helpers\): fix NewReqRemove was adding not removing, add req.Event

PR: [\#441](https://github.com/pancsta/asyncmachine-go/pull/441) (@pancsta)

_No PR description provided._

### refac\(machine\): move funcs into methods of `S` and `Schema`

PR: [\#440](https://github.com/pancsta/asyncmachine-go/pull/440) (@pancsta)

This should make schema overloads and processing easier. Some new ones were also added, like `schema.FilterByTag("foo")` or

```go
schema1["Foo"] = schema2["Foo"].SetRels(am.State{
  Remove: states,
})
```

### chore: add badges generator

PR: [\#439](https://github.com/pancsta/asyncmachine-go/pull/439) (@pancsta)

_No PR description provided._

### feat\(machine\): reuse subscription channels

PR: [\#438](https://github.com/pancsta/asyncmachine-go/pull/438) (@pancsta)

- fix(machine): fix ctx-expired subscription chans not closed

Channels are reused, including a single closed channel.

### feat\(machine\): add `ErrInternal()` channel

PR: [\#437](https://github.com/pancsta/asyncmachine-go/pull/437) (@pancsta)

Useful passing handler timeouts into the app's log.

### fix\(machine\): partially accepted auto txs shown as Canceled

PR: [\#436](https://github.com/pancsta/asyncmachine-go/pull/436) (@pancsta)

_No PR description provided._

### feat\(machine\): improve nested eval detection

PR: [\#435](https://github.com/pancsta/asyncmachine-go/pull/435) (@pancsta)

Via goroutine IDs compared with the event loop's goroutine ID. Separate (but simple) detection for eval-in-eval, as evals don't run on the event loop goroutine.

### feat\(machine\): add per-state generic typed args

PR: [\#434](https://github.com/pancsta/asyncmachine-go/pull/434) (@pancsta)

The biggest gap of the library is now fixed in a short and generic way. Per-state structs open the doors to generating `CallSignagure`s easily but are especially useful in dealing with schemas extended on multiple levels. The new args are backward compatible with a single `A` struct (which by default belongs to `Any` state).

```go
// common def

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
```

### fix\(rpc\): fix state ctx never closed

PR: [\#433](https://github.com/pancsta/asyncmachine-go/pull/433) (@pancsta)

_No PR description provided._

### feat\(machine\): add BindHandlerMaps for reflection-less bindings

PR: [\#432](https://github.com/pancsta/asyncmachine-go/pull/432) (@pancsta)

- feat(states): return a binding ID from all bind funcs, incl pipes
- feat(machine): add prefix for allowlist binding, method prefix

This breaking change is crucial for TinyGo and proper WASM dead code elimination. Same API changes apply to tracers.

Prefix-based nesting is now more feasible thanks for state prefixes and method de-prefixing via `BindOpts`. The first step to proper schema nesting.

### fix\(integrations\): fix listing all args for untyped MCP mutations

PR: [\#430](https://github.com/pancsta/asyncmachine-go/pull/430) (@pancsta)

_No PR description provided._

---

## [v0.18.9](https://github.com/pancsta/asyncmachine-go/releases/tag/v0.18.9) (2026-05-11)

### feat: release v0.18.9

PR: [\#424](https://github.com/pancsta/asyncmachine-go/pull/424) (@pancsta)

- refac: add state refs
- fix(repl): fix completion arg duplication
- fix(machine): fix gc duplicate false-positive
- fix(machine): fix ctxs closed after final handlers
- feat(machine): add `Machine.WhenNextActive`
- feat(helpers): add `EvAdd1Async`, `Add1Async`
- feat(helpers): add `*Remove*Sync`
- feat(helpers): migrate from errgroup to pond
- feat(helpers): add `BlockChan`
- feat(helpers): add `LogToTestLog`
- feat(machine): add `Machine.GoAfter`
- feat(machine): add `StatesByTag`

The remainder of `v0.18.9` is in this PR.

### feat\(am-dbg\): release am-dbg v0.18.9

PR: [\#423](https://github.com/pancsta/asyncmachine-go/pull/423) (@pancsta)

- refac: add state refs
- fix: fix steps and mermaid diagrams
- fix: fix missing log entries
- fix: fix ctrl+c exit
- fix: fix touch counter
- fix: fix listening on 0.0.0.0
- fix: fix machine list rendering
- fix: fix `--output-log`
- feat: add network graph time in the status bar
- feat: add next/prev machine toolbar buttons
- feat: apply auto-canceled tx filter to queued txs 
- feat: add explicit and foldable tree links
- feat: add `[ ]out-log` toolbar button
- feat: add `[ ]rpc` filter and CLI

### feat\(am-vis\): improve transition steps diagrams, mach viewer

PR: [\#422](https://github.com/pancsta/asyncmachine-go/pull/422) (@pancsta)

Changes: maximize shortcut, sequence repeated headers, richer info with state trace, optimized rendering, unified colors.

Transition sequence diagrams are out of experimental and the main way to see steps in am-dbg (steps timeline becomes disabled by default).

<img width="5097" height="8816" alt="tx" src="https://github.com/user-attachments/assets/05e570cb-4843-435e-ac13-086cac6049e6" />

### feat\(history\): add BadgerDB backend

PR: [\#421](https://github.com/pancsta/asyncmachine-go/pull/421) (@pancsta)

Another K/V backend for `pkg/history`, useful for WASM, as bbolt doesn't compile at all. Still needs manual binding to IndexedDB or File System API for persistence.

### feat\(docs\): add editor templates and AI skills

PR: [\#420](https://github.com/pancsta/asyncmachine-go/pull/420) (@pancsta)

"Skills" are generated from docs, divided by token usage and purpose. Live templates are often faster for everyday coding tho. See `/docs/editors/README.md`.

### feat\(integrations\): add mcp-go server creator

PR: [\#419](https://github.com/pancsta/asyncmachine-go/pull/419) (@pancsta)

Every state machine with typed args can have an MCP server. See `am-dbg` for how to extend it with custom getters (as asyncmachine does not return data).

### feat\(am-dbg\): add --output-call-log for statically typed handler log

PR: [\#418](https://github.com/pancsta/asyncmachine-go/pull/418) (@pancsta)

An explicit list of code executed by the machine with foldable negotiation. Navigable inside editors and IDEs, references for handler names and active states, ctrl+f, LSP/PSI queries. Still very experimental, manual bootstrap required (later via semlogger).

- `init.go`
```go
package main

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"

	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

var (
	e      *am.Event
	active am.S

	// TODO import schema
	ss states.ClientStatesDef

	// TODO import handlers
	h0 *arpc.Client
	// h1 *MachHandlers2
)
```

- `0.go`

```go
// Code generated by am-dbg, not for execution. Edit init.go for type defs.
package main

// Omitted: Any*(), Heartbeat*(), Healthcheck*()
func callLog0() {
	// t2
	{
		h0.ConnectingEnter(e)
	}
	h0.StartState(e)
	h0.ConnectingState(e)
	// t5
	h0.ConnectedState(e)
	h0.HandshakingState(e)
	// t7
	{
		h0.HandshakeDoneEnter(e)
	}
	h0.HandshakeDoneState(e)
	// t8
	h1.ReadyState(e)

}
```

### feat\(am-dbg\): standardize machine URLs, add selection and query strings

PR: [\#417](https://github.com/pancsta/asyncmachine-go/pull/417) (@pancsta)

Examples:
 - `mach://cook/5b0e0574309ab28f36a2cd91249545d2-5/26?group=AgentBase&state=BaseDBSaving&t=76`
 - `mach://cook/?t=76`

Better support for mach time (`?t`), queue ticks (`?qt`), human time (`?ht`). Support for steps has been removed, docs are coming.

### feat\(graph\): add XML graph format

PR: [\#416](https://github.com/pancsta/asyncmachine-go/pull/416) (@pancsta)

- fix(graph): fix parsing pipes

The network graph can now be parsed as XML, available in `am-dbg --output-graph`, `am-vis inspect-dump`, and MCP/NetworkGraph. This is useful for a general overview in a single req.

```xml
<machine id="tool-searxng-cook" time="6">
    <state tick="1" active="1">
        Disposed
        <remove>Disposing</remove>
        <remove>Start</remove>
    </state>
    <state tick="2">
        Disposing
        <remove>Start</remove>
    </state>
    <state tick="1" active="1">
        DockerAvailable
        <require>Start</require>
        <remove>DockerChecking</remove>
    </state>
    <state tick="1" active="1">
        DockerChecking
        <require>Start</require>
        <remove>DockerAvailable</remove>
    </state>
    <state tick="0" auto="1">
        DockerStarting
        <require>DockerAvailable</require>
    </state>
    <state tick="0" multi="1">
        ErrHandlerTimeout
        <require>Exception</require>
        <add>Exception</add>
    </state>
    <state tick="0" multi="1">
        ErrNetwork
        <require>Exception</require>
        <add>Exception</add>
    </state>
    <state tick="0">
        ErrOnClient
        <require>Exception</require>
    </state>
    <state tick="1" active="1" multi="1">Exception</state>
    <state tick="0" multi="1">Healthcheck</state>
    <state tick="0">Heartbeat</state>
    <state tick="0" auto="1">
        Idle
        <require>Ready</require>
        <remove>Working</remove>
    </state>
    <state tick="0">
        Ready
        <require>Start</require>
        <remove>DockerStarting</remove>
    </state>
    <state tick="0" multi="1">RegisterDisposal</state>
    <state tick="0">Start</state>
    <state tick="0">
        Working
        <require>Ready</require>
        <remove>Idle</remove>
    </state>
    <pipe to="cook" add="1" as="Ready">Ready</pipe>
    <pipe to="cook" add="0" as="Ready">Ready</pipe>
    <tag>tool</tag>
</machine>
```

### feat\(am-dbg\): add full transition history navigation

PR: [\#415](https://github.com/pancsta/asyncmachine-go/pull/415) (@pancsta)

New buttons, full history traversal and by-machine jumps.

<img width="512" height="211" alt="ss-2026-05-10-19-17-56" src="https://github.com/user-attachments/assets/576407db-e8af-4324-a8a7-24dca02fa5df" />

### feat\(am-dbg\): add MCP server and REPL

PR: [\#414](https://github.com/pancsta/asyncmachine-go/pull/414) (@pancsta)

These changes allow for automating repetitive tasks within the debugger.
The MCP server, unlike the REPL, returns data and has predefined mutations via `/pkg/machine.CallSignature`.

Accessible via `--ui-mcp` and `--dbg-repl`.

<img width="924" height="794" alt="ss-2026-05-10-19-26-39" src="https://github.com/user-attachments/assets/2c7cd228-b18a-4b9e-8462-43be03768863" />

### feat\(machine\): include relations in TimeAfter for negotiation

PR: [\#413](https://github.com/pancsta/asyncmachine-go/pull/413) (@pancsta)

Usually used via TimeIndexAfter, previously only (shallow) called states were processed. Very useful for proper negotiation.
```go
func (d *Debugger) NarrowLayoutExit(e *am.Event) bool {
    // always allow to exit
    after := e.Transition().TimeIndexAfter()
    if after.Not1(ss.Start) {
        return true
    }

    return after.Not1(ss.UserNarrowLayout)
}
```

---

## [v0.18.8](https://github.com/pancsta/asyncmachine-go/releases/tag/v0.18.8) (2026-04-23)

The highlight of this release is a full state-trace.

<img width="412" height="399" alt="ss-2026-04-21-09-19-40" src="https://github.com/user-attachments/assets/1e23fb1a-2fe5-42f5-8665-fed71c568753" />




### feat: release v0.18.8

PR: [\#411](https://github.com/pancsta/asyncmachine-go/pull/411) (@pancsta)

The remainder of `v0.18.8` is in this PR.

### feat\(am-dbg\): release am-dbg v0.18.8

PR: [\#410](https://github.com/pancsta/asyncmachine-go/pull/410) (@pancsta)

- fix(am-dbg): remove pipes from disconnected clients
- feat(am-dbg): add narrow layout toolbar button
- feat(am-dbg): use filtered transitions for the timeline info bars, steps
- feat(am-dbg): add log wrapping
- fix(am-dbg): fill visible log with filter-matching transitions
- refac(am-dbg): migrate to schema-v2
- refac(am-dbg): trace mutations where possible
- refac(am-dbg): migrate to typed args

A lot of quality-related refacs and a pretty important navigation feature for the timeline. Also, duplicating pipes are now fixed.

### feat\(am-dbg\): add diagram filtering \(group, called, changed, touched, relations, selected\)

PR: [\#409](https://github.com/pancsta/asyncmachine-go/pull/409) (@pancsta)

The new diagram filter options are `--output-diag-tx` and `--output-diag-group` (also on the toolbar). The former one highlights the diagram based on the current transition, while the latter one skips or completely re-renders the graph for states from the selected group. State selection will also highlight the diagram (distinctively). The diagram viewer now also has better UX.

<img width="1391" height="1063" alt="ss-2026-04-23-17-15-14" src="https://github.com/user-attachments/assets/b7e81b55-0933-480f-a36a-663584b9926e" />

### feat\(am-dbg\): add light theme via `--view-theme light`

PR: [\#408](https://github.com/pancsta/asyncmachine-go/pull/408) (@pancsta)

The light theme can be useful for debugging outdoors. Some fade-out colors in the dark theme have been fixed.

<img width="1599" height="1078" alt="ss-2026-04-23-17-10-45" src="https://github.com/user-attachments/assets/26239981-7754-453d-a5fc-feca0d3295a3" />

### feat\(am-dbg\): add explicit focus management

PR: [\#407](https://github.com/pancsta/asyncmachine-go/pull/407) (@pancsta)

- feat(am-dbg): improve toolbar keyboard nav

While still not perfect, the focus management is now handled manually which fixes many issues, including a deadlock caused by cview. The navigation in toolbars now wraps and remembers the position index.

### feat\(am-dbg\): add stack traces to queued mutations

PR: [\#406](https://github.com/pancsta/asyncmachine-go/pull/406) (@pancsta)

Every queued mutation (toolbar button) now has the stack trace attached and accessible from the log reader, with the machine-related entries filtered out.

<img width="1294" height="758" alt="ss-2026-04-12-15-48-20" src="https://github.com/user-attachments/assets/a37bf346-57f5-4309-9263-7aeedfabd97b" />

### feat\(am-dbg\): add state-trace to the reader

PR: [\#405](https://github.com/pancsta/asyncmachine-go/pull/405) (@pancsta)

The log reader now lists the full "state trace", making it look skin to a stack trace. Machine URLs can be used to navigate / copy.

<img width="412" height="399" alt="ss-2026-04-21-09-19-40" src="https://github.com/user-attachments/assets/31840a23-d961-4ef0-aa18-dac8d1c0adb2" />

### feat\(machine\): add `Next*Active*` tick helpers

PR: [\#404](https://github.com/pancsta/asyncmachine-go/pull/404) (@pancsta)

`WhenQueue` combined with tick couting effectively allows using a sync state as an async one, and the new tick helpers make it easier. Example:

```go
// click more and wait for unclick
err = amhelp.WaitForErrAll(ctx, time.Second, mach,
    mach.WhenQueue(mach.EvAdd1(e, ss.ClickingMore, nil)),
    mach.WhenTicks(ss.ClickingMore, am.NextInactiveIn(mach.Tick(ss.ClickingMore)), ctx))
```

### feat\(machine\): add `CtxToEv` and `EvToCtx`

PR: [\#403](https://github.com/pancsta/asyncmachine-go/pull/403) (@pancsta)

A small yet helpful sugar for passing `Event` via context.

### fix\(machine\): remove counter-mut duplicate detection

PR: [\#402](https://github.com/pancsta/asyncmachine-go/pull/402) (@pancsta)

This magic caused random bugs.

### test: add tests for examples

PR: [\#401](https://github.com/pancsta/asyncmachine-go/pull/401) (@pancsta)

- fix(machine): pool init

Basic integration tests to make sure examples arent breaking.

---

## [v0.18.6](https://github.com/pancsta/asyncmachine-go/releases/tag/v0.18.6) (2026-04-07)

The highlight of this release is the updated cookbook.



### feat: release v0.18.6

PR: [\#400](https://github.com/pancsta/asyncmachine-go/pull/400) (@pancsta)

- fix(rpc): fix ID prefixing for proper pipe logs, drop rand IDs
- fix(graph): ignore duplicated pipes

The remainder of `v0.18.6 ` is in this PR.

### feat\(am-vis\): add inspect-dump cmd producing a markdown version of th…

PR: [\#399](https://github.com/pancsta/asyncmachine-go/pull/399) (@pancsta)

- fix: CLI overloads

We can now use `am-vis` to inspect the whole network as text.

```bash
am-vis inspect-dump am-dbg-dump.gob.br
```

```markdown
-----

##### rnm-srv-browser2
Parent: rc-srv-browser2

###### States
- Browser3Completed
- Browser3Disposed
- Browser3Disposing
- Browser3ErrHandlerTimeout
- Browser3ErrNetwork
- Browser3ErrOnClient
- Browser3Exception
- Browser3Failed
- Browser3Healthcheck
- Browser3Heartbeat
- Browser3Ready
- Browser3RegisterDisposal
- Browser3Retry
- Browser3Retrying
- Browser3RpcReady
- Browser3Start
- Browser3Work
- Browser3Working
- Browser4Completed
- Browser4Disposed
- Browser4Disposing
- Browser4ErrHandlerTimeout
- Browser4ErrNetwork
- Browser4ErrOnClient
- Browser4Exception
- Browser4Failed
- Browser4Healthcheck
- Browser4Heartbeat
- Browser4Ready
- Browser4RegisterDisposal
- Browser4Retry
- Browser4Retrying
- Browser4RpcReady
- Browser4Start
- Browser4Work
- Browser4Working
- Completed
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- Exception
- Failed
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Retrying
- RpcReady
- ServerPayload
- Start
- Working

###### Pipes

####### orchestrator
- [add] Browser3Completed -> Browser3Completed
- [add] Browser3Exception -> ErrBrowser3
- [add] Browser3Failed -> Browser3Failed
- [add] Browser3Ready -> Browser3Ready
- [add] Browser3Working -> Browser3Working
- [add] Browser4Completed -> Browser4Completed
- [add] Browser4Exception -> ErrBrowser4
- [add] Browser4Failed -> Browser4Failed
- [add] Browser4Ready -> Browser4Ready
- [add] Browser4Working -> Browser4Working
- [add] Completed -> Browser2Completed
- [add] Exception -> ErrBrowser2
- [add] Failed -> Browser2Failed
- [add] Ready -> Browser2Ready
- [add] Working -> Browser2Working
- [remove] Browser3Completed -> Browser3Completed
- [remove] Browser3Exception -> ErrBrowser3
- [remove] Browser3Failed -> Browser3Failed
- [remove] Browser3Ready -> Browser3Ready
- [remove] Browser3Working -> Browser3Working
- [remove] Browser4Completed -> Browser4Completed
- [remove] Browser4Exception -> ErrBrowser4
- [remove] Browser4Failed -> Browser4Failed
- [remove] Browser4Ready -> Browser4Ready
- [remove] Browser4Working -> Browser4Working
- [remove] Completed -> Browser2Completed
- [remove] Exception -> ErrBrowser2
- [remove] Failed -> Browser2Failed
- [remove] Ready -> Browser2Ready
- [remove] Working -> Browser2Working

-----

##### rs-browser1
Parent: browser1

###### States
- ClientConnected
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RpcAccepting
- RpcReady
- RpcStarting
- Start
- WebSocketTunnel

###### Pipes

####### browser1
- [add] Ready -> RpcReady
- [remove] Ready -> RpcReady

-----
```

### feat\(machine\): add SemLogger.Pipes\(\) returning currently bound pipes

PR: [\#398](https://github.com/pancsta/asyncmachine-go/pull/398) (@pancsta)

- feat(telemetry): push out pipes on first schema

Binding pipes before am-dbg is not a problem any more.

### feat\(machine\): add PoolFork, PoolSetLimit, PoolSetLimitGlobal for non-blocking pools

PR: [\#397](https://github.com/pancsta/asyncmachine-go/pull/397) (@pancsta)

The previous `amhelp.Pool` used a blocking `errgroup`. PubSub adjusted. It will handle a heavy load better.

---

## [v0.18.4](https://github.com/pancsta/asyncmachine-go/releases/tag/v0.18.4) (2026-04-01)

The highlight of this release is support for WebWorkers.

![example-wasm-workflow d2 dark](https://github.com/user-attachments/assets/97ad5bd4-e297-41e8-ab20-b3ef16e416b6)



### feat: release v0.18.4

PR: [\#392](https://github.com/pancsta/asyncmachine-go/pull/392) (@pancsta)

The remainder of `v0.18.4` is in this PR.

### feat\(machine\): add Tracer.TransitionFinals for more detailed tracing

PR: [\#390](https://github.com/pancsta/asyncmachine-go/pull/390) (@pancsta)

Tracer now splits negotiation and final handlers, for better timing results.

### feat\(states\): add ampipe.Sync for re-connects

PR: [\#389](https://github.com/pancsta/asyncmachine-go/pull/389) (@pancsta)

When piping states, the target should be synchronized first to catch up if the source have been previously used.

```go
err = ampipe.Sync(client.NetMach, o.mach, pipeSrc, pipeDest)
err = ampipe.BindMany(client.NetMach, o.mach, pipeSrc, pipeDest)
```

### docs\(examples\): add WebWorker workflow example

PR: [\#388](https://github.com/pancsta/asyncmachine-go/pull/388) (@pancsta)

This is the highlight of `v0.18.4` - tightly synchronized threads, enabling stateful multi-threading inside any web browser.

![example-wasm-workflow d2 dark](https://github.com/user-attachments/assets/97ad5bd4-e297-41e8-ab20-b3ef16e416b6)

### feat\(telemetry\): auto exchange the root trace ID via env

PR: [\#387](https://github.com/pancsta/asyncmachine-go/pull/387) (@pancsta)

- fix(telemetry): fix Otel states filtering
- feat(telemetry): keep a singleton Otel provider

Tracing multiple state machines under the same trace is now automatic within the same process. Exchanging IDs via env makes it work for N number of sources (like browsers).

<img width="1292" height="1992" alt="wasm-workflow-traces" src="https://github.com/user-attachments/assets/3da2a812-f6f6-45bf-b4b1-4c9baedb9732" />

### feat\(telemetry\): add WASM support for Otel

PR: [\#386](https://github.com/pancsta/asyncmachine-go/pull/386) (@pancsta)

Traces are even more useful when there's no step-through debugger. Some build flags and a fork, and just works.

See /docs/wasm.md.

### refac\(am-dbg\): migrate to go-arg, add missing CLI filters

PR: [\#385](https://github.com/pancsta/asyncmachine-go/pull/385) (@pancsta)

Long overdue CLI refac has landed. Adding new args is now simple and all the filters are finally covered:

```bash
  --filter-auto          Filter automatic transitions
  --filter-auto-canceled
                         Filter automatic canceled transitions
  --filter-canceled      Filter canceled transitions
  --filter-checks        Filter check (read-only) transitions
  --filter-disconn       Filter disconnected clients
  --filter-empty         Filter empty transitions
  --filter-group         Filter transitions by a selected group [default: true]
  --filter-health        Filter health-check transitions
  --filter-log-level FILTER-LOG-LEVEL
                         Filter transitions up to this log level, 0-5 (silent-everything) [default: 2]
  --filter-queued        Filter queued transitions
```

### fix\(rpc\): remove SendPayload state

PR: [\#384](https://github.com/pancsta/asyncmachine-go/pull/384) (@pancsta)

- use rpc.Server.SendPayload() directly

Sending a payload from server to client via a state has been removed. This simplifies the ambiguous flow and reduces reflection.

### fix\(machine\): fix disposal with detached ctx

PR: [\#383](https://github.com/pancsta/asyncmachine-go/pull/383) (@pancsta)

Yet another disposal round, this time works for fleets of machines across the network.

---

## [v0.19.1](https://github.com/pancsta/asyncmachine-go/releases/tag/v0.19.1) (2026-06-23)

The highlight of this release is state-zoom diagram.

<img width="1399" height="223" alt="ss-2026-06-19-20-28-56" src="https://github.com/user-attachments/assets/3d34354c-a71f-4ae4-acf5-f6066d97aa14" />




### feat: release v0.19.1

PR: [\#448](https://github.com/pancsta/asyncmachine-go/pull/448) (@pancsta)

The remainder of `v0.19.1` is in this PR.

### feat\(am-dbg\): release am-dbg v0.19.1

PR: [\#447](https://github.com/pancsta/asyncmachine-go/pull/447) (@pancsta)

The remainder of am-dbg `v0.19.1` is in this PR.

### feat\(am-dbg\): add loading spinner

PR: [\#446](https://github.com/pancsta/asyncmachine-go/pull/446) (@pancsta)

Because diagrams take a very long time to initially generate, the spinner will expose it to the user.

### feat\(am-dbg\): unify diagram viewer for graph, mach, state, steps

PR: [\#445](https://github.com/pancsta/asyncmachine-go/pull/445) (@pancsta)

- feat(am-dbg): add state "zoom" diagrams
- feat(am-dbg): filter steps diagrams with a selected group

The web viewer handles all 4 types of diagrams, and the Chrome extension isn't necessary anymore.
 
State-zoom diagram: a new type of diagrams for large schemas which show the selected state and neighbors of it's neighbors (max graph transversal - 2 relation edges). The state and its relations are enlarged (thus the name).

Steps diagram: the states visible on the transition sequence diagrams will now honor the **diag-group** button and only render the selected group.

### feat\(machine\): enable state ctx for inactive states

PR: [\#444](https://github.com/pancsta/asyncmachine-go/pull/444) (@pancsta)

This is needed to properly cancel `*End()` handlers.

