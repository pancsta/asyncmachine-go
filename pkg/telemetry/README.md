# Telemetry

[-> go back to monorepo /](/README.md)

**asyncmachine-go** provides several telemetry exporters.

- [dbg](#dbg)
- [Open Telemetry](#open-telemetry)
- [Prometheus](#prometheus)

## dbg

`dbg` is a simple telemetry used by the [am-dbg TUI debugger](/tools/cmd/am-dbg).
am-dbg telemetry delivers `DbgMsg` and `DbgMsgStruct` via simple `net/rpc`. It can be consumed by a custom client as well.

## [Open Telemetry](https://opentelemetry.io/)

![Prometheus Grafana](/assets/otel-jaeger.dark.png#gh-dark-mode-only)
![Prometheus Grafana](/assets/otel-jaeger.light.png#gh-light-mode-only)

**Otel integration** exposes machine's states and transitions as Otel traces, compatible with
[Jaeger](https://www.jaegertracing.io/). Tracers are inherited from parent machines and form a tree.

Machine tree structure:

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

## Prometheus

![Prometheus Grafana](/assets/prometheus-grafana.dark.png#gh-dark-mode-only)
![Prometheus Grafana](/assets/prometheus-grafana.light.png#gh-light-mode-only)

[`pkg/telemetry/prometheus`](/pkg/telemetry/prometheus) binds to machine's transactions and averages the values withing
an interval exposing various metrics. Combined with [Grafana](https://grafana.com/), it can be used to monitor the
metrics of you machines.

Metrics:

- queue size
- states: active, inactive, added, removed
- transition duration (machine time)
- transition duration (normal time)
- transition's steps amount
- exceptions count
- registered states

Grafana dashboards can be:

- generated using `task gen-grafana-dashboard IDS=mach1,mach2`
- or imported from [assets](/assets/grafana-mach-sim,sim-p1.json)

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
