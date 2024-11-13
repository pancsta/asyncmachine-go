[![go report](https://goreportcard.com/badge/github.com/pancsta/asyncmachine-go)](https://goreportcard.com/report/github.com/pancsta/asyncmachine-go)
[![coverage](https://codecov.io/gh/pancsta/asyncmachine-go/graph/badge.svg?token=B8553BI98P)](https://codecov.io/gh/pancsta/asyncmachine-go)
[![go reference](https://pkg.go.dev/badge/github.com/pancsta/asyncmachine-go.svg)](https://pkg.go.dev/github.com/pancsta/asyncmachine-go)
[![last commit](https://img.shields.io/github/last-commit/pancsta/asyncmachine-go/main)](https://github.com/pancsta/asyncmachine-go/commits/main/)
![release](https://img.shields.io/github/v/release/pancsta/asyncmachine-go)
[![matrix chat](https://matrix.to/img/matrix-badge.svg)](https://matrix.to/#/#room:asyncmachine)

# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/telemetry

[cd /](/README.md)

> [!NOTE]
> **Asyncmachine-go** is an AOP Actor Model library for distributed workflows, built on top of a lightweight state
> machine (nondeterministic, multi-state, clock-based, relational, optionally-accepting, and non-blocking). It has
> atomic transitions, RPC, logging, TUI debugger, metrics, tracing, and soon diagrams.

**/pkg/telemetry** provides several telemetry exporters and a [Grafana dashboard](#grafana-dashboard):

- [dbg](#dbg)
- [OpenTelemetry Traces](#open-telemetry-traces)
- [OpenTelemetry Logger](#open-telemetry)
- [Prometheus Metrics](#prometheus)
- [Loki Logger](#loki-logger)

## dbg

`dbg` is a simple telemetry used by [am-dbg TUI Debugger](/tools/cmd/am-dbg).
It delivers `DbgMsg` and `DbgMsgStruct` via standard `net/rpc`. It can also be consumed by a custom client.

## OpenTelemetry Traces

[Open Telemetry traces](https://opentelemetry.io/) integration exposes machine's states and transitions as Otel traces,
compatible with [Jaeger](https://www.jaegertracing.io/). Tracers are inherited from parent machines and form a tree.

![Prometheus Grafana](https://pancsta.github.io/assets/asyncmachine-go/otel-jaeger.dark.png#gh-dark-mode-only)
![Prometheus Grafana](https://pancsta.github.io/assets/asyncmachine-go/otel-jaeger.light.png#gh-light-mode-only)

### Tree Structure

```text
- mach:ID
  - states
    - Foo
      - Foo (trace)
      - Foo (trace)
      - ...
    - ...
  - transitions
    - [add] Foo
      - FooEnter (trace)
      - FooState (trace)
      - ...
    - ...
  - submachines
    - mach:ID2
      - ...
    - ...
```

You can [import an existing asset](https://pancsta.github.io/assets/asyncmachine-go/bench-jaeger-3h-10m.traces.json)
into your Jaeger instance for an interactive demo.

### Otel tracing Setup

```go
import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
    "go.opentelemetry.io/otel/trace"
)

// ...

var mach *am.Machine
var tracer trace.Tracer

machTracer := amtele.NewOtelMachTracer(tracer, &amtele.OtelMachTracerOpts{
    SkipTransitions: false,
})
mach.BindTracer(machTracer)
```

## OpenTelemetry Logger

[Open Telemetry logger](https://opentelemetry.io/) integration exposes machine's logs (any level) as structured Otlp
format. It can be very handy for stdout logging.

```go
import (
    "go.opentelemetry.io/otel/sdk/log"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

// ...

var mach *am.Machine
var logExporter log.Exporter

// activate logs
mach.SetLogLevel(am.LogLevelOps)

// create a log provider
logProvider := amtele.NewOtelLoggerProvider(logExporter)
// bind provider to a machine
amtele.BindOtelLogger(mach, logProvider, "myserviceid")
```

## Prometheus Metrics

[`pkg/telemetry/prometheus`](/pkg/telemetry/prometheus) binds to machine's transactions, collects values within
a defined interval, and exposes averaged metrics. Use it with the provided [Grafana dashboard](#grafana-dashboard).
Tracers are inherited from parent machines.

### Prometheus Setup

```go
import (
    "time"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    amprom "github.com/pancsta/asyncmachine-go/pkg/telemetry/prometheus"
    "github.com/prometheus/client_golang/prometheus/push"

// ...

var mach *am.Machine
var promRegistry *prometheus.Registry
var promPusher *push.Pusher

// bind transition to metrics
metrics := amprom.TransitionsToPrometheus(mach, 5 * time.Minute)

// bind metrics either a registry or a pusher
amprom.BindToRegistry(promRegistry)
amprom.BindToPusher(promPusher)
```

## Loki Logger

Loki is the easiest way to persist distributed logs from asyncmachine. You'll need a [promtail client](https://github.com/ic2hrmk/promtail).

```go
import (
    "github.com/ic2hrmk/promtail"

    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

// ...

var mach *am.Machine
var service string

// init promtail and bind AM logger
identifiers := map[string]string{
    "service_name": service,
}
promtailClient, err := promtail.NewJSONv1Client("localhost:3100", identifiers)
if err != nil {
    panic(err)
}
defer promtailClient.Close()
amtele.BindLokiLogger(mach, promtailClient)
```

## Grafana Dashboard

![grafana](https://pancsta.github.io/assets/asyncmachine-go/grafana.dark.png)

Grafana dashboards need to be generated per "source" (e.g. process), by passing all monitored machine IDs and the source
name (`service_name` for Loki, `job` for Prometheus). See [am-gen grafana](/tools/cmd/am-gen/README.md). It will
optionally auto-sync the dashboard using [K-Phoen/grabana](https://github.com/K-Phoen/grabana) (requires
`GRAFANA_TOKEN`).

```bash
am-gen grafana \
  --name "My Dashboard" \
  --ids MyMach1,MyMach2 \
  --source service_name_or_job \
  --grafana-url http://localhost:3000
```

Panels:

- Number of transitions
- Transition Mutations
  - Queue size
  - States added
  - States removed
  - States touched
- Transition Details
  - Transition ticks
  - Number of steps
  - Number of handlers
- States & Relations
  - Number of states
  - Number of relations
  - Referenced states
  - Active states
  - Inactive states
- Average Transition Time
- Errors
- Log view

## Inheritance

Most of the exporters are automatically inherited from parent machines, so the results come in automatically. To define
a sub-parent relationship, use [`am.Opts.Parent`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Opts.Parent)
while initializing a machine. Alternatively, tracers can be copied using `OptsWithParentTracers`, or manually via
`Machine.Tracers`.

## Documentation

- [godoc /pkg/telemetry](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/telemetry)

## Status

Testing, not semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
