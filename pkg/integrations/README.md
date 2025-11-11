# 🦾 /pkg/integrations

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

**/pkg/integrations** is responsible for exposing state machines over various
JSON transports, with currently only NATS being implemented. In the future,
this may include email, Kafka, or HTTP.

## JSON

JSON types cover **mutations**, **subscriptions**, and **data getters**. Each of these is divided in request and
response objects which have a (/docs/jsonschema). Their usage depends on the specific implementation, eg in NATS each
machine has a dedicated subtopic for mutation requests.

- [`MutationReq`](/pkg/integrations/integrations.go) ([jsonschema](/docs/jsonschema/mutation_req.json))
- [`MutationResp`](/pkg/integrations/integrations.go) ([jsonschema](/docs/jsonschema/mutation_resp.json))
- [`WaitingReq`](/pkg/integrations/integrations.go) ([jsonschema](/docs/jsonschema/waiting_req.json))
- [`WaitingResp`](/pkg/integrations/integrations.go) ([jsonschema](/docs/jsonschema/waiting_resp.json))
- [`GetterReq`](/pkg/integrations/integrations.go) ([jsonschema](/docs/jsonschema/getter_req.json))
- [`GetterResp`](/pkg/integrations/integrations.go) ([jsonschema](/docs/jsonschema/getter_resp.json))

```go
import amjson "github.com/pancsta/asyncmachine-go/pkg/integration"

// create a subscription to Foo
reqSub := integrations.NewWaitingReq()
reqSub.States = am.S{"Foo"}
j, err := json.Marshal(reqSub)
```

## NATS

[NATS](https://github.com/nats-io/nats-server/) is a popular and high-performance messaging system made in Go.
State machines are exposed under a **topic**, with each state machine also being subscribed to a dedicated subtopic
"\[topic\].\[machineID\]" for mutation requests. Optional [queue] allows to load-balance requests across multiple
subscribers.

```go
import am "github.com/pancsta/asyncmachine-go"
import nats "github.com/pancsta/asyncmachine-go/pkg/integration/nats"

// ...

// var mach *am.Machine
// var ctx context.Context
// var nc *nats.Conn

// expose mach under mytopic
_ = nats.ExposeMachine(ctx, mach, nc, "mytopic", "")
// mutate - add Foo
res, _ := nats.Add(ctx, nc, topic, mach.Id(), am.S{"Foo"}, nil)
if res == am.Executed {
    print("Foo added to mach")
}
```

## TODO

- recipient matching (filters similar to the REPL ones)
- better error handling (avoid overreporting)

## Status

Alpha, work in progress, not semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
