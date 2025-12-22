# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /tools/cmd/am-gen

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

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

`go install github.com/pancsta/asyncmachine-go/tools/cmd/am-gen@latest`

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
$ am-gen states-file --help
Usage:
  am-gen states-file --name MyMach --states State1,State2:multi [flags]

Flags:
  -g, --groups string    Groups to generate. Eg: Group1,Group2
  -h, --help             help for states-file
  -i, --inherit string   Inherit from a built-in states machine: basic,connected,disposed,rpc/netsrc,node/worker
  -n, --name string      Name of the state machine. Eg: MyMach
  -s, --states string    State names to generate. Eg: State1,State2
      --utils            Generate states_utils.go in CWD. Overrides files. (default true)
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
Usage:
  am-gen grafana --name MyDash --ids my-mach-1,my-mach-2 --source my-service [flags]

Flags:
  -f, --folder string        Dashboard folder. Optional. Requires --grafana-url
  -g, --grafana-url string   Grafana URL to sync. Requires GRAFANA_TOKEN in CWD/.env
  -h, --help                 help for grafana
  -i, --ids string           Machines IDs (comma separated). Required.
  -n, --name string          Dashboard name. Required.
  -s, --source string        $source variable (service_name or job). Required.
```

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
