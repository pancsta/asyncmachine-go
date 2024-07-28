# am-dbg

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

## Example Data

For a quick demo you can browse an exported dump located in `assets/am-dbg-sim.gob.bz2` by running:

`$ am-dbg -i assets/am-dbg-sim.gob.bz2 -m sim-p1 -t 25`

## Steps To Debug

1. Set up telemetry:

    ```go
    import "github.com/pancsta/asyncmachine-go/pkg/telemetry"
    // ...
    err := telemetry.MonitorTransitions(mach, telemetry.RpcHost)
    ```

2. Run `am-dbg`
3. Run your code
4. Your machine should show up in the debugger

## FAQ

### How to debug steps of a transition?

Go to the steps timelines (bottom one) using the Tab key, then press left/right like before.

### How to export data?

Press `alt+s` and Enter.

### How to filter out canceled transitions?

Press `alt+f` or `Tab` until the bottom filter bar receives focus. Now select "Skip Canceled".

### How to access the help screen?

Press `?` to show the help popup.
