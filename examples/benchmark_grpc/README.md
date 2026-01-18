# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /examples/benchmark_grpc

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

## Benchmark: aRPC vs gRPC

![results - KiB transferred, number of calls](https://pancsta.github.io/assets/asyncmachine-go/arpc-vs-grpc.png)

This is a simple and opinionated benchmark of a subscribe-get-process scenario, implemented in both gRPC and aRPC.
Source code can be [found in /examples/benchmark_grpc](/examples/benchmark_grpc). It essentially manipulates a worker
state machine via various transports.

Steps:

1. **subscription**: wait for notifications
2. **getter**: get a value from the worker
3. **processing**: call an operation based on the value

Source available at [`/examples/benchmark_grpc`](/examples/benchmark_grpc).

### Plain Go Implementation

```go
i := 0
worker.Subscribe(func() {

    // loop
    i++
    if i > limit {
        close(end)
        return
    }

    // value (getter)
    value := worker.GetValue()

    // call op from value (processing)
    switch value {
    case Value1:
        go worker.CallOp(Op1)
    case Value2:
        go worker.CallOp(Op2)
    case Value3:
        go worker.CallOp(Op3)
    default:
        // err
        b.Fatalf("Unknown value: %v", value)
    }
})

worker.Start()
```

### Results

```text
$ task benchmark-grpc
...
BenchmarkClientArpc
    client_arpc_test.go:136: Transferred: 609 bytes
    client_arpc_test.go:137: Calls: 4
    client_arpc_test.go:138: Errors: 0
    client_arpc_test.go:136: Transferred: 1,149,424 bytes
    client_arpc_test.go:137: Calls: 10,003
    client_arpc_test.go:138: Errors: 0
BenchmarkClientArpc-8              10000            248913 ns/op           28405 B/op        766 allocs/op
BenchmarkClientGrpc
    client_grpc_test.go:117: Transferred: 1,113 bytes
    client_grpc_test.go:118: Calls: 9
    client_grpc_test.go:119: Errors: 0
    client_grpc_test.go:117: Transferred: 3,400,812 bytes
    client_grpc_test.go:118: Calls: 30,006
    client_grpc_test.go:119: Errors: 0
BenchmarkClientGrpc-8              10000            262693 ns/op           19593 B/op        391 allocs/op
BenchmarkClientLocal
BenchmarkClientLocal-8             10000               434.4 ns/op            16 B/op          1 allocs/op
PASS
ok      github.com/pancsta/asyncmachine-go/examples/benchmark_grpc      5.187s
```

### aRPC

Worker's states can be found below. For handlers, please [refer to the source](/examples/benchmark_grpc/server_arpc.go).

```go
// Machine schema defines relations and properties of states.
var States = am.Schema{
    // toggle
    Start: {},

    // ops
    CallOp: {
        Multi:   true,
        Require: S{Start},
    },

    // events
    Event: {
        Multi:   true,
        Require: S{Start},
    },

    // values
    Value1: {Remove: GroupValues},
    Value2: {Remove: GroupValues},
    Value3: {Remove: GroupValues},
}

// Groups of mutually exclusive states.

var (
    GroupValues = S{Value1, Value2, Value3}
)
```

## monorepo

- [`/pkg/rpc/README.md`](/pkg/rpc/README.md)
- [`/examples/arpc`](/examples/arpc)
- [`/pkg/rpc/HOWTO.md`](/pkg/rpc/HOWTO.md)

[Go back to the monorepo root](/README.md) to continue reading.
