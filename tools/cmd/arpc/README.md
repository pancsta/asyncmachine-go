# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /tools/cmd/arpc

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor, state-machine).

## aRPC REPL

![am-dbg](https://pancsta.github.io/assets/asyncmachine-go/arpc.png)

`arpc` is a [network-native](/pkg/rpc/README.md) REPL and CLI to manage one or many asyncmachines. Combined with
`am-dbg` and a dedicated state machine, it can act as a debugging agent, showing results in the debugger, while
accepting commands in the REPL. It's also very useful to modify live systems without restarting them, including
filtered group operations.

## Installation

- [Download a release binary](https://github.com/pancsta/asyncmachine-go/releases/latest)
- Install `go install github.com/pancsta/asyncmachine-go/tools/cmd/arpc@latest`
- Run directly `go run github.com/pancsta/asyncmachine-go/tools/cmd/arpc@latest`

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor, state-machine).

## Features

- **mutations**: change the state of connected state machines.
- **checking**: filter connected state machines and inspect them.
- **waiting**: wait on multiple states or machine time of a single state machine.
- **CLI**: all commands can also be called via CLI, eg in a bash script, including [waiting methods](/docs/manual.md#waiting).
- **readline shell**: not just a simple REPL, supports [inputrc keybindings](https://www.gnu.org/software/bash/manual/html_node/Readline-Init-File.html).
- **aRPC**: connect to any [aRPC endpoint](/pkg/rpc/README.md).
- **helpers**: easily enable a REPL for existing state machines with a oneliner.
- **multi-client**: connect to as many state machines as you want.
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

<details>
<summary>$ arpc list --help</summary>

```text
List connected machines

Usage:
  arpc list [flags]

Examples:
list -a Foo -a Bar --mtime-min 1631

Flags:
  -a, --active stringArray         Filter by an active state (repeatable)
  -d, --disconn                    Show disconnected (default true)
  -f, --from int                   Start from this index
  -h, --help                       help for list
  -I, --id-partial string          Substring to match machine IDs
  -p, --id-prefix string           Prefix to match machine IDs
  -r, --id-regexp string           Regexp to match machine IDs
  -s, --id-suffix string           Suffix to match machine IDs
  -i, --inactive stringArray       Filter by an inactive state (repeatable)
  -l, --limit int                  Mutate up to N machines
  -T, --mtime-max uint             Max machine time, e.g. "1631616000"
  -t, --mtime-min uint             Min machine time, e.g. "1631616000"
  -m, --mtime-states stringArray   Take machine time only from these states (repeatable)
  -P, --parent string              Filter by parent ID
  -z, --states                     Print states (default true)
  -Z, --states-all                 Print all states (requires --states)
```

</details>

<details>
<summary>$ arpc add --help</summary>

```text
Add states to a single machine

Usage:
  arpc add MACH STATES [flags]

Examples:
$ add mach1 Foo Bar \
  --arg name1 --val val1 \
  --arg name2 --val val2

Flags:
      --arg stringArray   Argument name (repeatable)
  -h, --help              help for add
      --val stringArray   Argument value (repeatable)
```

</details>

<details>
<summary>$ arpc when-time --help</summary>

```text
Wait for a specific machine time of a single machine

Usage:
  arpc when-time MACH STATES TIMES [flags]

Examples:
when-time mach1 -s Foo -t 1000 -s Bar -t 2000

Flags:
  -h, --help                help for when-time
  -s, --state stringArray   State name (repeatable)
  -t, --time stringArray    Machine time (repeatable)
```

</details>

## Steps to connect

1. Enable a REPL per each state machine:

    ```go
    import arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
    // ...
    // create an address file in ./tmp/MACH-ID.addr
    arpc.MachRepl(mach, "", "tmp", nil)
    ```

2. Run `arpc -d tmp`
3. The REPL should start and connect to all addresses from the `./tmp` dir

## Demo

<div align="center">
    <img src="https://pancsta.github.io/assets/asyncmachine-go/videos/repl-demo1.gif" alt="REPL" />
</div>

## Credits

- [reeflective/console](https://github.com/reeflective/console/)

## monorepo

- [`/examples/repl`](/examples/repl)

[Go back to the monorepo root](/README.md) to continue reading.
