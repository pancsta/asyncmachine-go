# /tools/cmd/am-dbg

[-> go back to monorepo /](/README.md)

## am-dbg TUI Debugger

`am-dbg` is a lightweight, multi-client debugger for [asyncmachine-go](https://github.com/pancsta/asyncmachine-go). It
easily handles >100 client machines simultaneously streaming telemetry data (and potentially many more).

![am-dbg](../../../assets/am-dbg.dark.png#gh-dark-mode-only)
![am-dbg](../../../assets/am-dbg.light.png#gh-light-mode-only)

## Features

- states tree
- log view
- time travel
- transition steps
- import / export
- filters
- matrix view

```text
Usage:
  am-dbg [flags]

Flags:
      --am-dbg-url string       Debug this instance of am-dbg with another one
      --clean-on-connect        Clean up disconnected clients on the 1st connection
      --enable-mouse            Enable mouse support (experimental)
  -h, --help                    help for am-dbg
  -i, --import-data string      Import an exported gob.bz2 file
  -l, --listen-on string        Host and port for the debugger to listen on (default "localhost:6831")
      --log-file string         Log file path (default "am-dbg.log")
      --log-level int           Log level, 0-5 (silent-everything)
  -c, --select-connected        Select the newly connected machine, if no other is connected
  -m, --select-machine string   Select a machine by ID on startup (requires --import-data)
  -t, --select-transition int   Select a transaction by _number_ on startup (requires --select-machine)
      --version                 Print version and exit
  -v, --view string             Initial view (tree-log, tree-matrix, matrix) (default "tree-log")
```

## Installation

[Download a release binary](https://github.com/pancsta/asyncmachine-go/releases/latest) or use `go install`:

`go install github.com/pancsta/asyncmachine-go/tools/am-dbg@latest`

## Demos

For a quick demo, you can browse an exported dump located in `assets/am-dbg-sim.gob.br` by running:

`$ am-dbg --import-data assets/am-dbg-sim.gob.br --select-machine sim-p1 --select-transition 25`

### Live Debugging Sessions

Interactively use the TUI debugger with data pre-generated by **libp2p-pubsub-simulator** in:

- web browser: [http://188.166.101.108:8080/wetty/ssh](http://188.166.101.108:8080/wetty/ssh/am-dbg?pass=am-dbg:8080/wetty/ssh/am-dbg?pass=am-dbg)
- terminal: `ssh 188.166.101.108 -p 4444`

Interactively use the TUI debugger with data pre-generated by **remote integration tests** in:

- web browser: [http://188.166.101.108:8081/wetty/ssh](http://188.166.101.108:8081/wetty/ssh/am-dbg?pass=am-dbg:8081/wetty/ssh/am-dbg?pass=am-dbg)
- terminal: `ssh 188.166.101.108 -p 4445`

## Steps To Debug

1. Set up telemetry:

    ```go
    import amt "github.com/pancsta/asyncmachine-go/pkg/telemetry"
    // ...
    err := amt.MonitorTransitions(ctx, mach, "")
    ```

2. Run `am-dbg`
3. Run your code
4. Your machine should show up in the debugger

## Steps For SSH Server

[Download an SSH release binary](https://github.com/pancsta/asyncmachine-go/releases/latest) or use `go install`:

`go install github.com/pancsta/asyncmachine-go/tools/am-dbg-ssh@latest`

```text
am-dbg-ssh is an SSH version of asyncmachine-go debugger.

You can connect to a running instance with any SSH client.

Usage:
  am-dbg-ssh -s localhost:4444 [flags]
```

## FAQ

### How to debug steps of a transition?

Go to the steps timelines (bottom one) using the Tab key, then press left/right like before.

### How to export data?

Press `alt+s` and Enter.

### How to filter out canceled transitions?

Press `alt+f` or `Tab` until the bottom filter bar receives focus. Now select "Skip Canceled".

### How to access the help screen?

Press `?` to show the help popup.

## monorepo

- [`/tools/debugger`](/tools/debugger/README.md)

[Go back to the monorepo root](/README.md) to continue reading.
