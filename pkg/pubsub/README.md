# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/pubsub

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

**/pkg/pubsub** is a trustful and decentralized synchronization network for asyncmachine-go. Each peer exposes several state
machines, then starts gossiping about them and other ones known to him. Remote state machines are then visible to other
peers as `/pkg/rpc.Worker`. PubSub can be used to match **Clients** with **Supervisors** from [/pkg/node](/pkg/node/README.md).

Under the hood it's based on [**libp2p gossipsub**](https://github.com/libp2p/go-libp2p-pubsub), which is a mesh-based
PubSub, also based on gossipping:

- **libp2p** gossips create and maintain the network topology
- **pkg/pubsub** gossips synchronize machine schemas and clocks

## Support

- state checking YES
- state mutations NO
- state waiting YES

## Features

- gossip-based discovery
- gossip-based clock updates
- gossip-based checksums via machine time
- rate limiting
- no leaders, no elections

## Screenshot

[am-dbg](/tools/cmd/am-dbg/README.md) view of a PubSub with 6 peers, with p1-p5 exposing a single state machine each.

![](https://pancsta.github.io/assets/asyncmachine-go/am-dbg/pubsub.png)

## Schema

State schema from [/pkg/pubsub/states/](/pkg/pubsub/states/ss_topic.go).

![worker schema](https://pancsta.github.io/assets/asyncmachine-go/schemas/pubsub.svg)

## TODO

- more protocol-level rate limiting
- confirmed handler timeouts #220
- faster discovery
- 1k peer load test
- mDNS & DHT & auth
- optimizations
- documentation
  - discovery protocol
  - sequence diagrams

## Usage

### Peer Init

```go
import (
    ma "github.com/multiformats/go-multiaddr"
    ampubsub "github.com/pancsta/asyncmachine-go/pkg/pubsub"
    ssps "github.com/pancsta/asyncmachine-go/pkg/pubsub/states"
)

var ss = states.TopicStates

// ...

// new pubsub peer
ps, _ := ampubsub.NewTopic(ctx, t.Name(), name, machs, nil)
// address of an existing peer
a, _ := ma.NewMultiaddr("/ip4/127.0.0.1/udp/75343/quic-v1")
addrs := []ma.Multiaddr{a}

// connect
ps.ConnAddrs = addrs
ps.Start()
<-ps.Mach.When1(ss.Connected, ctx)
ps.Mach.Add1(ss.Joining, nil)
```

## Remote Workers

```go
import (
    ma "github.com/multiformats/go-multiaddr"
    ampubsub "github.com/pancsta/asyncmachine-go/pkg/pubsub"
)

var ss = states.TopicStates

// ...

var remotePeerId string
var ps *ampusub.Topic

// list machines exported by [remotePeerId]
ch := make(chan []*rpc.Worker, 1)
args := &A{
    WorkersCh: ch,
    ListFilters: &ListFilters{
        PeerId: remotePeerId,
    },
}
_ = ps.Mach.Add1(ss.ListMachines, Pass(args))
workers := <-ch
close(ch)s

// find a worker tagged "foo", add state "Bar", and wait for state "Baz"
for _, mach := range workers {
    if amhelp.TagValue(mach.Tags(), "foo") == "" {
        continue
    }
    mach.Add1("Bar", nil)
    println("Bar set on " + mach.Id())

    <-mach.When1("Baz", nil)
    println("Baz active on " + mach.Id())
    break
}
```

## Status

Alpha, work in progress, not semantically versioned.

## Credits

- [libp2p](https://libp2p.io/)

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
