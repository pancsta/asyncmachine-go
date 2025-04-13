# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/pubsub

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor, state-machine).

**/pkg/pubsub** is a trustful decentralized synchronization network for asyncmachine-go. Each peer exposes several state
machines, then starts gossiping about them and other ones known to him. Remote state machines are then visible to other
peers as `/pkg/rpc.LocalWorker`. PubSub can be used to match Clients with Workers from [/pkg/node](/pkg/node/README.md).

Under the hood it's based on [**libp2p gossipsub**](https://github.com/libp2p/go-libp2p-pubsub), which is a mesh-based
PubSub, also based on gossipping, but for the purpose of network topology. **libp2p** gossips are separate from gossips
of this package.

## Support

- state checking YES
- state mutations NO
- state waiting YES

## Features

- gossip-based discovery
- gossip-based clock updates
- gossip-based checksums via machine time
- rate limitting
- no leaders, no elections

## Screenshot

[am-dbg](/tools/cmd/am-dbg/README.md) view of a PubSub with 6 peers, with p1-p5 exposing a single state machine each.

![](https://pancsta.github.io/assets/asyncmachine-go/am-dbg/pubsub.png)

## Schema

State schema from [/pkg/pubsub/states/](/pkg/pubsub/states/ss_topic.go).

![worker schena](https://pancsta.github.io/assets/asyncmachine-go/schemas/pubsub.svg)

## TODO

- more rate limitting
- confirmed handler timeouts
- faster discovery
- load test
- mDNS & DHT & auth
- optimizations
- documentation
  - discovery protocol
  - sequence diagrams

## Usage

```go
import (
    ma "github.com/multiformats/go-multiaddr"
    ampubsub "github.com/pancsta/asyncmachine-go/pkg/pubsub"
)

// ...

// init a pubsub peer
ps, _ := ampubsub.NewTopic(ctx, t.Name(), name, machs, nil)
// prep a libp2p multi address
a, _ := ma.NewMultiaddr("/ip4/127.0.0.1/udp/75343/quic-v1")
addrs := []ma.Multiaddr{a}
ps.ConnAddrs = addrs
ps.Start()
<-ps.Mach.When1(ss.Connected, ctx)
ps.Mach.Add1()
```

## Status

Alpha, work in progress, not semantically versioned.

## Credits

- [libp2p](https://libp2p.io/)

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
