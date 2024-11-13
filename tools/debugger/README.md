[![go report](https://goreportcard.com/badge/github.com/pancsta/asyncmachine-go)](https://goreportcard.com/report/github.com/pancsta/asyncmachine-go)
[![coverage](https://codecov.io/gh/pancsta/asyncmachine-go/graph/badge.svg?token=B8553BI98P)](https://codecov.io/gh/pancsta/asyncmachine-go)
[![go reference](https://pkg.go.dev/badge/github.com/pancsta/asyncmachine-go.svg)](https://pkg.go.dev/github.com/pancsta/asyncmachine-go)
[![last commit](https://img.shields.io/github/last-commit/pancsta/asyncmachine-go/main)](https://github.com/pancsta/asyncmachine-go/commits/main/)
![release](https://img.shields.io/github/v/release/pancsta/asyncmachine-go)
[![matrix chat](https://matrix.to/img/matrix-badge.svg)](https://matrix.to/#/#room:asyncmachine)

# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /tools/debugger

[cd /](/README.md)

> [!NOTE]
> **Asyncmachine-go** is an AOP Actor Model library for distributed workflows, built on top of a lightweight state
> machine (nondeterministic, multi-state, clock-based, relational, optionally-accepting, and non-blocking). It has
> atomic transitions, RPC, logging, TUI debugger, metrics, tracing, and soon diagrams.

To read about **am-dbg**, go to [/tools/cmd/am-dbg](/tools/cmd/am-dbg/README.md). This package is about the
implementation, not the end-user application.

`/tools/debugger` is a [cview](https://code.rocket9labs.com/tslocum/cview) TUI app with a single state machine
consisting of:

- input events (7 states)
- external state (25 states)
- actions (18 states)

This state machine features a decent amount of relations within a large number of states and 5 state groups. It's also a
good example to see how easily an AM-based program can be controller with a script in [/internal/cmd/am-dbg-video](/internal/cmd/am-dbg-video/main_dbg_video.go).

<details>

<summary>See states structure and relations</summary>

```go
// States map defines relations and properties of states.
var States = am.Struct{

    // ///// Input events

    ClientMsg:       {Multi: true},
    ConnectEvent:    {Multi: true},
    DisconnectEvent: {Multi: true},

    // user scrolling tx / steps
    UserFwd: {
        Add:    S{Fwd},
        Remove: GroupPlaying,
    },
    UserBack: {
        Add:    S{Back},
        Remove: GroupPlaying,
    },
    UserFwdStep: {
        Add:     S{FwdStep},
        Require: S{ClientSelected},
        Remove:  SAdd(GroupPlaying, S{LogUserScrolled}),
    },
    UserBackStep: {
        Add:     S{BackStep},
        Require: S{ClientSelected},
        Remove:  SAdd(GroupPlaying, S{LogUserScrolled}),
    },

    // ///// Read-only states (e.g. UI)

    // focus group

    TreeFocused:          {Remove: GroupFocused},
    LogFocused:           {Remove: GroupFocused},
    SidebarFocused:       {Remove: GroupFocused},
    TimelineTxsFocused:   {Remove: GroupFocused},
    TimelineStepsFocused: {Remove: GroupFocused},
    MatrixFocused:        {Remove: GroupFocused},
    DialogFocused:        {Remove: GroupFocused},
    FiltersFocused:       {Remove: GroupFocused},

    StateNameSelected:     {Require: S{ClientSelected}},
    TimelineStepsScrolled: {Require: S{ClientSelected}},
    HelpDialog:            {Remove: GroupDialog},
    ExportDialog: {
        Require: S{ClientSelected},
        Remove:  GroupDialog,
    },
    LogUserScrolled: {
        Remove: S{Playing, TailMode},
        // TODO remove the requirement once its possible to go back
        //  to timeline-scroll somehow
        Require: S{LogFocused},
    },
    Ready:            {Require: S{Start}},
    FilterAutoTx:     {},
    FilterCanceledTx: {},
    FilterEmptyTx:    {},

    // ///// Actions

    Start: {},
    TreeLogView: {
        Auto:   true,
        Remove: SAdd(GroupViews, S{MatrixRain}),
    },
    MatrixView:     {Remove: GroupViews},
    TreeMatrixView: {Remove: GroupViews},
    TailMode: {
        Require: S{ClientSelected},
        Remove:  SAdd(GroupPlaying, S{LogUserScrolled}),
    },
    Playing: {
        Require: S{ClientSelected},
        Remove:  SAdd(GroupPlaying, S{LogUserScrolled}),
    },
    Paused: {
        Auto:    true,
        Require: S{ClientSelected},
        Remove:  GroupPlaying,
    },
    ToggleFilter: {},
    SwitchingClientTx: {
        Require: S{Ready},
        Remove:  GroupSwitchedClientTx,
    },
    SwitchedClientTx: {
        Require: S{Ready},
        Remove:  GroupSwitchedClientTx,
    },
    ScrollToMutTx: {Require: S{ClientSelected}},
    MatrixRain:    {},

    // tx / steps back / fwd

    Fwd: {
        Require: S{ClientSelected},
    },
    Back: {
        Require: S{ClientSelected},
    },
    FwdStep: {
        Require: S{ClientSelected},
    },
    BackStep: {
        Require: S{ClientSelected},
    },

    ScrollToTx: {
        Require: S{ClientSelected},
        Remove:  S{TailMode, Playing},
    },

    // client selection

    SelectingClient: {
        Require: S{Start},
        Remove:  S{ClientSelected},
    },
    ClientSelected: {
        Require: S{Start},
        Remove:  S{SelectingClient},
    },
    RemoveClient: {Require: S{ClientSelected}},
}
```

</details>

You can read the source at [/tools/debugger](/tools/debugger) and states at [/tools/debugger/states](/tools/debugger/states/ss_dbg.go).

## Integration Tests

[/tools/debugger/test](/tools/debugger/test/integration_test.go) contains integration tests which use a **local worker**
instance, which is then controlled with [Add1Block](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#Add1Block)
and [Add1AsyncBlock](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/helpers#Add1AsyncBlock) from [/pkg/helpers](/pkg/helpers)
to perform test scenarios. None of the tests ever calls `time.Sleep`, as everything is synchronized via **asyncmachine's**
clock. The local test worker doesn't have UI and uses **tcell.SimulationScreen** instead.

### Remote Worker

[/tools/debugger/test/remote](/tools/debugger/test/remote/integration_remote_test.go) contains integration tests which
use a **remote worker** to execute the same suite. The worker has to be started separately using `task am-dbg-worker`,
and because it's a regular TUI app, this one has a UI which can be seen and controlled by the user (on top of the test
suite itself).

### Debugging Workers

Because both local and remote workers are state machines, they can export telemetry to a debugger instance. In
[aRPC Tutorial](/pkg/rpc/HOWTO.md) one can find a video demo presenting a debugging session of a remote worker. To
activate remote debugging, please set `AM_TEST_DEBUG=1` and run `task am-dbg-dbg` prior to tests. Remote tests are run
via `task test-debugger-remote`.

[![Video Walkthrough](https://pancsta.github.io/assets/asyncmachine-go/asyncmachine-go/rpc-demo1.png)](https://pancsta.github.io/assets/asyncmachine-go/asyncmachine-go/rpc-demo1.m4v)

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
