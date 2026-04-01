# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /tools/cmd/am-gen

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a pathless control-flow graph with a consensus (AOP, actor model, state-machine).

`am-gen` will quickly bootstrap a typesafe schema file, or a Grafana dashboard for you.

```bash
$ am-gen states-file --states State1,State2:multi \
    --inherit basic,connected \
    --groups Group1,Group2 \
    --name MyMach
```

This will create 2 files in the current directory - **ss_mymach.go** and **states_utils.go**, which should be placed in
`mypkg/states` (optional). Once generated, the file is edited manually. Read [Typesafe States in /docs/manual.md](/docs/manual.md#typesafe-states)
for more info.

## Installation

```bash
go install github.com/pancsta/asyncmachine-go/tools/cmd/am-gen@latest
```

### Usage

```bash
am-gen generates state files and Grafana dashboards for asyncmachine-go state machines.

Example:
$ am-gen states-file --states State1,State2:multi \
        --inherit basic,connected \
        --groups Group1,Group2 \
        --name MyMach

Example:
$ am-gen grafana --IDs MyMach1,MyMach2 \
        --sync grafana-host.com

Usage: am-gen [--version] <command> [<args>]

Options:
  --version, -v          Print version and exit
  --help, -h             display this help and exit

Commands:
  states-file            Generate state schema files
  grafana                Generate Grafana dashboards
```

## Schema Files

- define state names and properties (`:auto` and `:multi`)
- easily inherit from `pkg/states`, `pkg/rpc/states`, and `pkg/node/states`
- create own groups and inherit existing ones

### Example Schema File

```go
package states

import (
    am "github.com/pancsta/asyncmachine-go/pkg/machine"
    ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

// MyMachStatesDef contains all the states of the MyMach state machine.
type MyMachStatesDef struct {
    *am.StatesBase

    State1 string
    State2 string

    // inherit from BasicStatesDef
    *ss.BasicStatesDef
    // inherit from ConnectedStatesDef
    *ss.ConnectedStatesDef
}

// MyMachGroupsDef contains all the state groups MyMach state machine.
type MyMachGroupsDef struct {
    *ss.ConnectedGroupsDef
    Group1 S
    Group2 S
}

// MyMachSchema represents all relations and properties of MyMachStates.
var MyMachSchema = SchemaMerge(
    // inherit from BasicStruct
    ss.BasicStruct,
    // inherit from ConnectedStruct
    ss.ConnectedStruct,
    am.Schema{

        ssM.State1: {},
        ssM.State2: {
            Multi: true,
        },
})

// EXPORTS AND GROUPS

var (
    ssM = am.NewStates(MyMachStatesDef{})
    sgM = am.NewStateGroups(MyMachGroupsDef{
        Group1: S{},
        Group2: S{},
    }, ss.ConnectedGroups)

    // MyMachStates contains all the states for the MyMach machine.
    MyMachStates = ssM
    // MyMachGroups contains all the state groups for the MyMach machine.
    MyMachGroups = sgM
)
```

### Schema File Help

```bash
Usage: am-gen states-file [--version] --states STATES [--inherit INHERIT] [--groups GROUPS] --name NAME [--force] [--utils]

Options:
  --version
  --states STATES, -s STATES
                         State names to generate. Eg: State1,State2
  --inherit INHERIT, -i INHERIT
                         Inherit from built-in state-machines: basic,connected,rpc/statesrc,node/worker
  --groups GROUPS, -g GROUPS
                         Groups to generate. Eg: Group1,Group2
  --name NAME, -n NAME   Name of the state machine. Eg: MyMach
  --force, -f            Override output file (if any)
  --utils, -u            Generate states_utils.go in CWD. Overrides files. [default: true]

Global options:
  --version, -v          Print version and exit
  --help, -h             display this help and exit
```

## Grafana Dashboard

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/grafana.dark.png">
        <img style="min-height: 397px"
            src="https://pancsta.github.io/assets/asyncmachine-go/grafana.dark.png"
            alt="grafana dashboard" />
    </a>
</div>

Grafana dashboards need to be generated per "source" (e.g. process), by passing all monitored machine IDs and the source
name (`service_name` for Loki, `job` for Prometheus). It will
optionally auto-sync the dashboard using [K-Phoen/grabana](https://github.com/K-Phoen/grabana) (requires
`GRAFANA_TOKEN`).

- generates separate dashboard per source (job)
- supports [Loki and Prometheus](/pkg/telemetry/README.md)
- can sync via `GRAFANA_TOKEN` with `--grafana-url`
- automatically inherited by submachines (for automated setups)

Panels:

- Transitions
  - Number of transitions
- Errors
  - Heatmap
- Transition Mutations
  - Queue size
  - States added
  - States removed
  - States touched
- Transition Details
  - Transition ticks
  - Number of steps
  - Number of handlers
- States and Relations
  - Number of states
  - Number of relations
  - Referenced states
  - Active states
  - Inactive states
- Average Transition Time
  - Heatmap
- Log view
  - Loki logger

### Automatic Grafana Setup

See [/docs/env-configs.md](/docs/env-configs.md) for the required environment variables.

```go
import amgen "github.com/pancsta/asyncmachine-go/tools/generator"

// ...

var mach *am.Machine

// create a dedicated dashboard for [mach] and submachines
amgen.MachDashboardEnv(mach)
```

### Manual Grafana Setup

```go
import (
    amgen "github.com/pancsta/asyncmachine-go/tools/generator"
    amgencli "github.com/pancsta/asyncmachine-go/tools/generator/cli"
)

// ...

var mach *am.Machine
var service string
var url string
var token string

p := amgencli.GrafanaParams{
    Ids:        mach.Id(),
    Name:       mach.Id(),
    Folder:     "asyncmachine",
    GrafanaUrl: url,
    Token:      token,
    Source:     service,
}
t := &amgen.SyncTracer{p: p}

mach.BindTracer(t)
```

### Manual Grafana Setup (shell)

The command below will create a dashboard for machines with IDs `root,_rm-root,_rs-root-0,_rs-root-1,_rs-root-2`.
Without `--grafana-url`, it will output a JSON version of the same dashboard.

```bash
am-gen grafana \
    --name tree_state_source_root \
    --ids root,_rm-root,_rs-root-0,_rs-root-1,_rs-root-2 \
    --grafana-url http://localhost:3000 \
    --source tree_state_source_rep1
```

### Grafana Help

```bash
Usage: am-gen grafana --ids IDS [--grafana-url GRAFANA-URL] [--folder FOLDER] --name NAME --source SOURCE

Options:
  --ids IDS, -i IDS      Machine IDs (comma separated)
  --grafana-url GRAFANA-URL, -g GRAFANA-URL
                         Grafana URL to sync. Requires GRAFANA_TOKEN in CWD/.env
  --folder FOLDER, -f FOLDER
                         Dashboard folder (optional, requires --grafana-url)
  --name NAME, -n NAME   Dashboard name
  --source SOURCE, -s SOURCE
                         $source variable (service_name or job)

Global options:
  --version, -v          Print version and exit
  --help, -h             display this help and exit
```

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
