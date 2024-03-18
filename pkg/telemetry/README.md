# Telemetry

![AM traces in jaeger via otel](../../assets/otel-jaeger.png)

[`pkg/telemetry`](pkg/telemetry) provides various telemetry exporters.

## [Open Telemetry](https://opentelemetry.io/)

Otel integration exposes machine's states and transitions as Otel traces, compatible with
[Jaeger](https://www.jaegertracing.io/). Tracers are inherited from parent machines and provide the following tree view:

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

See [`pkg/telemetry`](pkg/telemetry) for more info or [import an existing asset](assets/json)

## am-dbg

am-dbg telemetry delivers `DbgMsg` and `DbgMsgStruct` via simple `net/rpc` to `tools/cmd/am-dbg`. It can be consumed by
a custom client as well.
