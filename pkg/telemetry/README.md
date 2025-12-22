# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /pkg/telemetry

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

**/pkg/telemetry** provides several telemetry exporters and is accompanied by [generated Grafana dashboards](/tools/cmd/am-gen/README.md#grafana-dashboard):

- [dbg](#dbg)
- [OpenTelemetry Traces](#opentelemetry-traces)
- [OpenTelemetry Logger](#opentelemetry-logger)
- [Prometheus Metrics](#prometheus-metrics)
- [Loki Logger](#loki-logger)
- [Grafana Dashboards](#grafana-dashboard)

## dbg

`dbg` is simple telemetry used by [am-dbg TUI Debugger](/tools/cmd/am-dbg).
It delivers `DbgMsg` and `DbgMsgStruct` via standard `net/rpc`. It can also be consumed by a custom client.

## OpenTelemetry Traces

[Open Telemetry traces](https://opentelemetry.io/) integration exposes machine's states and transitions as Otel traces,
compatible with [Jaeger](https://www.jaegertracing.io/). Tracers are inherited from parent machines and
form a tree, with each machine getting a position index as a prefix, eg `0:mach:MyMachId`. The transitions are linked
to the states with [logged arguments](/docs/manual.md#arguments-logging) are added as trace's tags. Machine tags are
added as well.

<div align="center">
    <a href="https://github.com/pancsta/asyncmachine-go/blob/main/pkg/telemetry/README.md#open-telemetry-traces">
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://pancsta.github.io/assets/asyncmachine-go/otel-jaeger.dark.png">
  <source media="(prefers-color-scheme: light)" srcset="https://pancsta.github.io/assets/asyncmachine-go/otel-jaeger.light.png">
  <img alt="OpenTelemetry traces in Jaeger" src="https://pancsta.github.io/assets/asyncmachine-go/otel-jaeger.light.png">
</picture></a></div>

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
    - [add] Foo (trace)
    - ...
  - submachines
    - mach:ID2
      - ...
    - ...
```

### Otel Tracing Setup

### Automatic Otel Tracing

See [/docs/env-configs.md](/docs/env-configs.md) for the required environment variables.

```go
import amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"

// ...

var mach *am.Machine

// open telemetry traces
err = amtele.MachBindOtelEnv(mach)
if err != nil {
    mach.AddErr(err, nil)
}
```

### Manual Otel Tracing

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

### Automatic Prometheus Setup

See [/docs/env-configs.md](/docs/env-configs.md) for the required environment variables.

```go
import amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"

// ...

var mach *am.Machine

// export metrics to prometheus
metrics := amprom.MachMetricsEnv(mach)
```

### Manual Prometheus Setup

```go
import (
    "time"
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    amprom "github.com/pancsta/asyncmachine-go/pkg/telemetry/prometheus"
    "github.com/prometheus/client_golang/prometheus/push"
)

// ...

var mach *am.Machine
var promRegistry *prometheus.Registry
var promPusher *push.Pusher

// bind transition to metrics
metrics := amprom.BindMach(mach)

// bind metrics either a registry or a pusher
amprom.BindToRegistry(promRegistry)
amprom.BindToPusher(promPusher)
```

## Loki Logger

Loki is the easiest way to persist distributed logs from asyncmachine. You'll need a [promtail client](https://github.com/ic2hrmk/promtail).

### Automatic Loki Setup

See [/docs/env-configs.md](/docs/env-configs.md) for the required environment variables.

```go
import amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"

// ...

var mach *am.Machine

// export logs to Loki logger
amtele.BindLokiEnv(mach)
```

### Manual Loki Setup

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

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/grafana.dark.png">
        <img src="https://pancsta.github.io/assets/asyncmachine-go/grafana.dark.png"
            alt="grafana">
    </a>
</div>

More info about Grafana dashboards can be found in [/tools/cmd/am-gen](/tools/cmd/am-gen/README.md#grafana-dashboard).

### Automatic Grafana Setup

See [/docs/env-configs.md](/docs/env-configs.md) for the required environment variables.

```go
import amgen "github.com/pancsta/asyncmachine-go/tools/generator"

// ...

var mach *am.Machine

// create a dedicated dashboard for [mach] and submachines
amgen.MachDashboardEnv(mach)
```

### Manual Grafana Setup

```go
import (
    amgen "github.com/pancsta/asyncmachine-go/tools/generator"
    amgencli "github.com/pancsta/asyncmachine-go/tools/generator/cli"
)

// ...

var mach *am.Machine
var service string
var url string
var token string

p := amgencli.GrafanaParams{
    Ids:        mach.Id(),
    Name:       mach.Id(),
    Folder:     "asyncmachine",
    GrafanaUrl: url,
    Token:      token,
    Source:     service,
}
t := &amgen.SyncTracer{p: p}

mach.BindTracer(t)
```

## Inheritance

Most of the telemetry exporters are automatically inherited from parent machines, so the results come in automatically.
To define a submachine-parent relationship, use [`am.Opts.Parent`](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/machine#Opts.Parent)
while initializing a machine. Alternatively, tracers can be copied using `OptsWithParentTracers`, or manually via
`Machine.Tracers`.

## Documentation

- [list of supported env vars](/docs/env-configs.md)
- [api /pkg/telemetry](https://code.asyncmachine.dev/pkg/github.com/pancsta/asyncmachine-go/pkg/telemetry.html)
- [godoc /pkg/telemetry](https://pkg.go.dev/github.com/pancsta/asyncmachine-go/pkg/telemetry)

## Status

Testing, not semantically versioned.

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
