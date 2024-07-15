# Telemetry

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="../../assets/otel-jaeger.dark.png?raw=true">
  <source media="(prefers-color-scheme: light)" srcset="../../assets/otel-jaeger.light.png?raw=true">
  <img alt="Test duration chart" src="../../assets/otel-jaeger.dark.png?raw=true">
</picture>

- Open Telemetry
- am-dbg

## [Open Telemetry](https://opentelemetry.io/)

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

See [`pkg/telemetry`](pkg/telemetry) for more info or [import an existing asset](../../assets/bench-jaeger-3h-10m.traces.json)

## am-dbg

am-dbg telemetry delivers `DbgMsg` and `DbgMsgStruct` via simple `net/rpc` to `tools/cmd/am-dbg`. It can be consumed by
a custom client as well.
