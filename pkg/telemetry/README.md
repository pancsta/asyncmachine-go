# /pkg/telemetry

[-> go back to monorepo /](/README.md)

> [!NOTE]
> **asyncmachine** can transform blocking APIs into controllable state machines with ease. It shares similarities with
> [Ergo's](https://github.com/ergo-services/ergo) actor model, and focuses on distributed workflows like [Temporal](https://github.com/temporalio/temporal).
> It's lightweight and most features are optional.

**/pkg/telemetry** provides several telemetry exporters:

- [dbg](#dbg)
- [Open Telemetry](#open-telemetry)
- [Prometheus](#prometheus)

## dbg

`dbg` is a simple telemetry used by [am-dbg TUI Debugger](/tools/cmd/am-dbg).
It delivers `DbgMsg` and `DbgMsgStruct` via standard `net/rpc`. It can also be consumed by a custom client.

## Open Telemetry

[Open Telemetry](https://opentelemetry.io/) integration exposes machine's states and transitions as Otel traces,
compatible with [Jaeger](https://www.jaegertracing.io/). Tracers are inherited from parent machines and form a tree.

![Prometheus Grafana](/assets/otel-jaeger.dark.png#gh-dark-mode-only)
![Prometheus Grafana](/assets/otel-jaeger.light.png#gh-light-mode-only)

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

You can [import an existing asset](/assets/bench-jaeger-3h-10m.traces.json) into your Jaeger instance for an interactive
demo.

### Otel Usage

```go
import "github.com/pancsta/asyncmachine-go/pkg/telemetry"

// ...

opts := &am.Opts{}
initTracing(ctx)
traceMach(opts, true)
mach, err := am.NewCommon(ctx, "mymach", ss.States, ss.Names, nil, nil, opts)

// ...

func initTracing(ctx context.Context) {
    // TODO NewOtelProvider
    tracer, provider, err := NewOtelProvider(ctx)
    if err != nil {
        log.Fatal(err)
    }
    _, rootSpan = tracer.Start(ctx, "sim")
    // TODO persist tracer, provider, and rootSpan
}

func traceMach(opts *am.Opts, traceTransitions bool) {
    machTracer := telemetry.NewOtelMachTracer(s.OtelTracer, &telemetry.OtelMachTracerOpts{
        SkipTransitions: !traceTransitions,
        Logf:            logNoop,
    })
    opts.Tracers = []am.Tracer{machTracer}
}
```

## Prometheus

[`pkg/telemetry/prometheus`](/pkg/telemetry/prometheus) binds to machine's transactions and averages the values within
an interval exposing various metrics. Combined with [Grafana](https://grafana.com/), it can be used to monitor the
metrics of you machines.

![Prometheus Grafana](/assets/prometheus-grafana.dark.png#gh-dark-mode-only)
![Prometheus Grafana](/assets/prometheus-grafana.light.png#gh-light-mode-only)

### Metrics

- queue size
- states: active, inactive, added, removed
- transition duration (machine time)
- transition duration (normal time)
- transition's steps amount
- exceptions count
- registered states

### Grafana Dashboards

- generated using `task gen-grafana-dashboard IDS=mach1,mach2`
- or imported from [assets](/assets/grafana-mach-sim,sim-p1.json)

### Prom Usage

```go
import "github.com/pancsta/asyncmachine-go/pkg/telemetry/prometheus"

// ...

mach := am.New(nil, am.Struct{
    "Foo": {Require: am.S{"Bar"}},
    "Bar": {},
}, nil)

metrics := TransitionsToPrometheus(mach, 5 * time.Minute)

promPusher.Collector(metrics.StatesAmount)
promPusher.Collector(metrics.RelAmount)
promPusher.Collector(metrics.RefStatesAmount)
promPusher.Collector(metrics.QueueSize)
promPusher.Collector(metrics.StepsAmount)
promPusher.Collector(metrics.HandlersAmount)
promPusher.Collector(metrics.TxTime)
promPusher.Collector(metrics.StatesActiveAmount)
promPusher.Collector(metrics.StatesInactiveAmount)
promPusher.Collector(metrics.TxTick)
promPusher.Collector(metrics.ExceptionsCount)
promPusher.Collector(metrics.StatesAdded)
promPusher.Collector(metrics.StatesRemoved)
promPusher.Collector(metrics.StatesTouched)
```

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
