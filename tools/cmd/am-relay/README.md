# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /tools/cmd/arpc

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a pathless control-flow graph with a consensus (AOP, actor model, state-machine).

## am-relay

`am-relay` converts formats and relays connections. It's an early version that for now can only rotate [dbg telemetry](/pkg/telemetry/README.md#dbg)
into chunked file dumps.

```bash
.rw-r--r--@ 713k foo 17 Nov 12:19 am-dbg-dump-2025-11-17_12-19-35.gob.br
.rw-r--r--@ 737k foo 17 Nov 12:20 am-dbg-dump-2025-11-17_12-20-02.gob.br
.rw-r--r--@ 749k foo 17 Nov 12:20 am-dbg-dump-2025-11-17_12-20-28.gob.br
```

## Installation

1. [Download a release binary](https://github.com/pancsta/asyncmachine-go/releases/latest)
2. Install `go install github.com/pancsta/asyncmachine-go/tools/cmd/am-relay@latest`
3. Run directly `go run github.com/pancsta/asyncmachine-go/tools/cmd/am-relay@latest`

## Features

- rotate dbg telemetry
- websocket-to-tcp listen
- websocket-to-tcp dial
- TODO convert gob to JSON

`$ am-relay --help`

```bash
Usage: am-relay [--debug] [--name NAME] <command> [<args>]

Options:
  --debug                Enable debugging for asyncmachine
  --name NAME, -n NAME   Name of this relay
  --help, -h             display this help and exit

Commands:
  rotate-dbg             Rotate dbg protocol with fragmented dump files
  wasm                   WebSockets to local TCP listeners for WASM
```

### `dbg` protocol rotation

`$ am-relay rotate-dbg --help`

```text
Usage: am-relay rotate-dbg [--listen-addr LISTEN-ADDR] [--fwd-addr FWD-ADDR] [--interval-tx INTERVAL-TX]
 [--interval-duration INTERVAL-DURATION] [--output OUTPUT] [--dir DIR]

Options:
  --listen-addr LISTEN-ADDR, -l LISTEN-ADDR
                         Listen address for RPC server [default: localhost:2732]
  --fwd-addr FWD-ADDR, -f FWD-ADDR
                         Address of an RPC server to forward data to (repeatable)
  --interval-tx INTERVAL-TX
                         Amount of transitions to create a dump file [default: 10000]
  --interval-duration INTERVAL-DURATION
                         Amount of human time to create a dump file [default: 24h]
  --output OUTPUT, -o OUTPUT
                         Output file base name [default: am-dbg-dump]
  --dir DIR, -d DIR      Output directory [default: .]

Global options:
  --debug                Enable debugging for asyncmachine
  --help, -h             display this help and exit
```

### Relay of WebAssembly

`$ am-relay wasm --help`

```text
Usage: am-relay wasm [--listen-addr LISTEN-ADDR] [--static-dir STATIC-DIR] [--repl-addr-dir REPL-ADDR-DIR]

Options:
  --listen-addr LISTEN-ADDR, -l LISTEN-ADDR
                         Listen address for HTTP server [default: localhost:12733]
  --static-dir STATIC-DIR, -s STATIC-DIR
                         Directory with static files to serve (optional)
  --repl-addr-dir REPL-ADDR-DIR, -r REPL-ADDR-DIR
                         Directory for creating REPL addr files (optional)

Global options:
  --debug                Enable debugging for asyncmachine
  --name NAME, -n NAME   Name of this relay
  --help, -h             display this help and exit
```

WASM relay is also usable as a library - this allows passing the connection directly to aRPC clients and servers within
the same process.

```go
import (
    amrelay "github.com/pancsta/asyncmachine-go/tools/relay"
    amrelayt "github.com/pancsta/asyncmachine-go/tools/relay/types"
)

// ...

relay, err := amrelay.New(ctx, amrelayt.Args{
    Name:   "wasm-demo",
    Debug:  true,
    Parent: fooMach,
    Wasm: &amrelayt.ArgsWasm{
        ListenAddr: example.EnvRelayHttpAddr,
        StaticDir:  "./client",
        ReplAddrDir: "tmp",
        TunnelMatchers: []amrelayt.TunnelMatcher{{
            Id:        regexp.MustCompile("^browser-bar-"),
            NewClient: newClient,
        }},
        DialMatchers: []amrelayt.DialMatcher{{
            Id: regexp.MustCompile("^browser-foo-"),
            NewServer: func(ctx context.Context, id string, conn net.Conn) (*arpc.Server, error) {
                return mux.NewServer(nil, id, conn)
            },
        }},
    },
})
if err != nil {
    panic(err)
}
relay.Start(nil)
```

## monorepo

- [`/examples/wasm`](/examples/wasm)
- [`/pkg/rpc`](/pkg/rpc)

[Go back to the monorepo root](/README.md) to continue reading.
