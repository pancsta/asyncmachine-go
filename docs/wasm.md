# WebAssembly Support

**asyncmachine-go** support WebAssembly through WebSockets-to-TCP tunnels and MessageChannels between WebWorkers for all
packages besides `/pkg/pubsub` and `/pkg/node` (not due to technical reasons). The distributed nature of the framework
makes a perfect fit for a [single-threaded environment of WASM](https://github.com/tinygo-org/tinygo/issues/2630),
which can easily synchronize and chunk work between CPU cores.

Extensive information about WASM for **asyncmachine** can be found at:

- [`/examples/wasm`](/examples/wasm)
- [`/examples/wasm_workflow`](/examples/wasm_workflow)
- [`/tools/cmd/am-relay`](/tools/cmd/am-relay/README.md#relay-of-webassembly)

Building WASM is as simple as:

```bash
env GOOS=js GOARCH=wasm go build -o main.wasm ./cmd
```

## Code Differences

Network constructs require additional options for WASM, although sane defaults are mostly in place.

### aRPC Server

```go
srv, err := arpc.NewServer(ctx, addrRelay, mach.Id(), mach, &arpc.ServerOpts{
    // eg localhost:8080/listen/bar/localhost:7070 opens 7070 for "bar"
    WebSocketTunnel: arpc.WsListenPath(mach.Id(), addrListen),
})
```

### aRPC Client

```go
client, err := arpc.NewClient(ctx, addrRelay, fooHandlerMach.Id(), states.FooSchema, &arpc.ClientOpts{
    // eg localhost:8080/dial/bar/localhost:7070 dials 7070 for "bar"
    WebSocket: arpc.WsDialPath(mach.Id(), addrDial),
})
```

### REPL

```go
repl, err := arpc.MachReplWs(mach, addrRelay, &arpc.ReplOpts{
    // eg localhost:8080/listen/bar/localhost:7070 opens 7070 for "bar"
    WebSocketTunnel: arpc.WsListenPath("repl-"+mach.Id(), addrListen),
})
```

## Debugging

The delve debugger [doesn't support the WASM target yet](https://github.com/go-delve/delve/issues/1325), but there are
tools that can help today:

- [`am-dbg`](/tools/cmd/am-dbg)
- OpenTelemetry from [`/pkg/telemetry`](/pkg/telemetry/README.md#automatic-otel-tracing)
- browser's profiler
- `runtime/trace`
- `go test` with mocked handlers

## Known Issues

- stock HTTP Otel expoter is broken in WASM and needs a `replace` directive (see [PR#8120](https://github.com/open-telemetry/opentelemetry-go/pull/8120))

```go
replace go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp => github.com/pancsta/opentelemetry-go/exporters/otlp/otlptrace/otlptracehttp v0.0.0-20260331193851-f087fcb1fc48
```

- `/pkg/rpc.(*Mux)` doesn't work in WASM, every clients needs a manual RPC server created
