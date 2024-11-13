# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /examples/benchmark_libp2p_pubsub

- [cd /](/)

> [!NOTE]
> **Asyncmachine-go** is an AOP Actor Model library for distributed workflows, built on top of a lightweight state
> machine (nondeterministic, multi-state, clock-based, relational, optionally-accepting, and non-blocking). It has
> atomic transitions, RPC, logging, TUI debugger, metrics, tracing, and soon diagrams.

[**go-libp2p-pubsub-benchmark**](https://github.com/pancsta/go-libp2p-pubsub-benchmark) compares the default
[go-libp2p-pubsub](https://github.com/libp2p/go-libp2p-pubsub) implementation to the [asyncmachine version](https://github.com/pancsta/go-libp2p-pubsub/).
It runs `TestSimpleDiscovery` for various host/msg configurations and presents a median for each iteration. The best
way to view the results is [bench.md](bench.https://github.com/pancsta/go-libp2p-pubsub-benchmark/blob/main/bench.md),
[bench.pdf](https://github.com/pancsta/go-libp2p-pubsub-benchmark/blob/main/assets/bench.pdf). Single runs can
be viewed in Jaeger and am-dbg after `task test-discovery`. Benchmark uses go1.22 traces, thus needs at least this
version.

![Test duration chart](https://pancsta.github.io/assets/asyncmachine-go/libp2-bench.dark.jpg#gh-dark-mode-only)
![Test duration chart](https://pancsta.github.io/assets/asyncmachine-go/libp2-bench.light.png#gh-light-mode-only)

- **pubsub host** - eg `ps-17` (20 states)<br />
  PubSub state machine is a simple event loop with [multi states](/docs/manual.md#multi-states) which get responses via arg
  channels. Heavy use of [`Machine.Eval()`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Machine.Eval).
- **discovery** - eg `ps-17-disc` (10 states)<br />
  Discovery state machine is a simple event loop with [multi states](/docs/manual.md#multi-states) and a periodic
  refresh state.
- **discovery bootstrap** - eg `ps-17-disc-bf3` (5 states)<br />
  `BootstrapFlow` is a non-linear flow for topic bootstrapping with some retry logic.

<details>

<summary>See states structure and relations (pubsub host)</summary>

```go
package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// States define relations between states
var States = am.Struct{
    // peers
    PeersPending: {},
    PeersDead:    {},
    GetPeers:     {Multi: true},

    // peer
    PeerNewStream:   {Multi: true},
    PeerCloseStream: {Multi: true},
    PeerError:       {Multi: true},
    PublishMessage:  {Multi: true},
    BlacklistPeer:   {Multi: true},

    // topic
    GetTopics:       {Multi: true},
    AddTopic:        {Multi: true},
    RemoveTopic:     {Multi: true},
    AnnouncingTopic: {Multi: true},
    TopicAnnounced:  {Multi: true},

    // subscription
    RemoveSubscription: {Multi: true},
    AddSubscription:    {Multi: true},

    // misc
    AddRelay:        {Multi: true},
    RemoveRelay:     {Multi: true},
    IncomingRPC:     {Multi: true},
    AddValidator:    {Multi: true},
    RemoveValidator: {Multi: true},
}
```

</details>

<details>

<summary>See states structure and relations (discovery & bootstrap)</summary>

```go
package discovery

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States define relations between states.
var States = am.Struct{
    Start: {
        Add: S{PoolTimer},
    },
    PoolTimer: {},
    RefreshingDiscoveries: {
        Require: S{Start},
    },
    DiscoveriesRefreshed: {
        Require: S{Start},
    },

    // topics

    DiscoveringTopic: {
        Multi: true,
    },
    TopicDiscovered: {
        Multi: true,
    },

    BootstrappingTopic: {
        Multi: true,
    },
    TopicBootstrapped: {
        Multi: true,
    },

    AdvertisingTopic: {
        Multi: true,
    },
    StopAdvertisingTopic: {
        Multi: true,
    },
}

// StatesBootstrapFlow define relations between states for the bootstrap flow.
var StatesBootstrapFlow = am.Struct{
    Start: {
        Add: S{BootstrapChecking},
    },
    BootstrapChecking: {
        Remove: BootstrapGroup,
    },
    DiscoveringTopic: {
        Remove: BootstrapGroup,
    },
    BootstrapDelay: {
        Remove: BootstrapGroup,
    },
    TopicBootstrapped: {
        Remove: BootstrapGroup,
    },
}

// Groups of mutually exclusive states.

var (
    BootstrapGroup = S{DiscoveringTopic, BootstrapDelay, BootstrapChecking, TopicBootstrapped}
)
```

</details>

See
[github.com/pancsta/**go-libp2p-pubsub-benchmark**](https://github.com/pancsta/go-libp2p-pubsub-benchmark/#libp2p-pubsub-benchmark)
or the [pdf results](https://github.com/pancsta/go-libp2p-pubsub-benchmark/raw/main/assets/bench.pdf) for more info.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
