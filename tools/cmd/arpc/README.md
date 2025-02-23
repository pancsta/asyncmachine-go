# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /tools/cmd/arpc

[`cd /`](/README.md)

## aRPC REPL

![am-dbg](https://pancsta.github.io/assets/asyncmachine-go/arpc.png)

`arpc` is a network-native REPL and CLI to manage one or many asyncmachines. Combined with `am-dbg` and a dedicated
machine, it can act as a debugging agent, showing results in the debugger, while accepting commands in the REPL.
It's also useful to modify live systems without restarting them.

## Installation

- [Download a release binary](https://github.com/pancsta/asyncmachine-go/releases/latest)
- Install `go install github.com/pancsta/asyncmachine-go/tools/cmd/arpc@latest`
- Run directly `go run github.com/pancsta/asyncmachine-go/tools/cmd/arpc@latest`

> [!NOTE]
> **asyncmachine-go** is a declarative control flow library implementing [AOP](https://en.wikipedia.org/wiki/Aspect-oriented_programming)
> and [Actor Model](https://en.wikipedia.org/wiki/Actor_model) through a **[clock-based state machine](/pkg/machine/README.md)**.

## Features

- **mutations**: change the state of 1 or N connected machines.
- **checking**: filtering connected machines and inspect them.
- **waiting**: wait on certain state or machine time of a single state machine.
- **CLI**: all commands can also be called via CLI, eg in a bash script.
- **readline shell**: not just a simple REPL, supports inputrc.
- **aRPC**: connects to any aRPC endpoint.
- **helpers**: easy connect to existing machines with a oneliner.
- **multi-client**: connect to as many machines as you want.
- **group mutations**: perform mutations on large groups of state machines.

```text
aRPC REPL for asyncmachine.dev

Usage:
  arpc ADDR [flags]
  arpc [command]

Examples:
- REPL from an addr
  $ arpc localhost:6452
- REPL from a file
  $ arpc -f mymach.addr
- CLI add states with args
  $ arpc localhost:6452 -- add mach1 Foo Bar \
    --arg name1 --val val1
- CLI add states with args to the first machine
    $ arpc localhost:6452 -- add . Foo Bar \
    --arg name1 --val val1
- CLI wait on 2 states to be active
  $ arpc localhost:6452 -- when mach1 Foo Bar
- CLI add states to a group of machines
  $ arpc localhost:6452 -- group-add -r ma.\* Foo Bar

Mutations
  add          Add states to a single machine
  group-add    Add states to a group of machines
  group-remove Remove states from a group of machines
  remove       Removed states from a single machine

Waiting
  when         Wait for active states of a single machine
  when-not     Wait for inactive states of a single machine
  when-time    Wait for a specific machine time of a single machine

Checking
  inspect      Inspect a single machine
  mach         Show states of a single machine
  time         Show the time of a single machine

REPL
  cheatsheet   Show cheatsheet
  exit         Exit the REPL
  help         Help about any command
  list         List connected machines
  script       Execute a REPL script from a file
  status       Show the connection status

Additional Commands:
  completion   Generate the autocompletion script for the specified shell

Flags:
  -d, --dir    Load *.addr files from a directory
  -f, --file   Load addresses from a file
  -h, --help   help for arpc

Use "arpc [command] --help" for more information about a command.
```

## Steps to connect

1. Set up the connection:

    ```go
    import arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    // ...
    // create an *.addr file in ./tmp
    arpc.arpc.MachRepl(mach, "", "tmp", nil)
    ```

2. Run `arpc -d tmp`
3. REPL should start and connect to all addresses from the `./tmp` dir

## Demo

![repl demo](https://pancsta.github.io/assets/asyncmachine-go/videos/repl-demo1.gif)

## Credits

- [reeflective/console](https://github.com/reeflective/console/)

## monorepo

- [`/examples/repl`](/examples/repl)

[Go back to the monorepo root](/README.md) to continue reading.
