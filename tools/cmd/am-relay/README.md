# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /tools/cmd/arpc

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

## am-relay

`am-relay` converts formats and relays connections. It's an early version that for now can only rotate [dbg telemetry](/pkg/telemetry/README.md#dbg)
into chunked file dumps.

```bash
.rw-r--r--@ 713k foo 17 Nov 12:19 am-dbg-dump-2025-11-17_12-19-35.gob.br
.rw-r--r--@ 737k foo 17 Nov 12:20 am-dbg-dump-2025-11-17_12-20-02.gob.br
.rw-r--r--@ 749k foo 17 Nov 12:20 am-dbg-dump-2025-11-17_12-20-28.gob.br
```

## Installation

- [Download a release binary](https://github.com/pancsta/asyncmachine-go/releases/latest)
- Install `go install github.com/pancsta/asyncmachine-go/tools/cmd/am-relay@latest`
- Run directly `go run github.com/pancsta/asyncmachine-go/tools/cmd/am-relay@latest`

## Features

- rotate dbg telemetry
- TODO websocket-to-tcp
- TODO convert gob to JSON

`$ am-relay --help`

```bash
Usage: am-relay [--debug] <command> [<args>]

Options:
  --debug                Enable debugging for asyncmachine
  --help, -h             display this help and exit

Commands:
  rotate-dbg             Rotate dbg protocol with fragmented dump files
```

`$ am-relay rotate-dbg --help`

```text
Usage: am-relay rotate-dbg [--listen-addr LISTEN-ADDR] [--fwd-addr FWD-ADDR] [--interval-tx INTERVAL-TX] [--interval-duration INTERVAL-DURATION] [--output OUTPUT] [--dir DIR]

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

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
