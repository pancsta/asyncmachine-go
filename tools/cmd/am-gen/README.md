[![go report](https://goreportcard.com/badge/github.com/pancsta/asyncmachine-go)](https://goreportcard.com/report/github.com/pancsta/asyncmachine-go)
[![coverage](https://codecov.io/gh/pancsta/asyncmachine-go/graph/badge.svg?token=B8553BI98P)](https://codecov.io/gh/pancsta/asyncmachine-go)
[![go reference](https://pkg.go.dev/badge/github.com/pancsta/asyncmachine-go.svg)](https://pkg.go.dev/github.com/pancsta/asyncmachine-go)
[![last commit](https://img.shields.io/github/last-commit/pancsta/asyncmachine-go/main)](https://github.com/pancsta/asyncmachine-go/commits/main/)
![release](https://img.shields.io/github/v/release/pancsta/asyncmachine-go)
[![matrix chat](https://matrix.to/img/matrix-badge.svg)](https://matrix.to/#/#room:asyncmachine)

# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /tools/cmd/am-gen _ [cd /](/)

> [!NOTE]
> **Asyncmachine-go** is an AOP Actor Model library for distributed workflows, built on top of a lightweight state
> machine (nondeterministic, multi-state, clock-based, relational, optionally-accepting, and non-blocking). It has
> atomic transitions, RPC, logging, TUI debugger, metrics, tracing, and soon diagrams.

`am-gen` will quickly bootstrap a typesafe states file, or a Grafana dashboard for you.

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

## States Files

- define state names and properties (`:auto` and `:multi`)
- easily inherit from `pkg/states`, `pkg/rpc/states`, and `pkg/node/states`
- create own groups and inherit existing ones

### Example States File

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

// MyMachStruct represents all relations and properties of MyMachStates.
var MyMachStruct = StructMerge(
    // inherit from BasicStruct
    ss.BasicStruct,
    // inherit from ConnectedStruct
    ss.ConnectedStruct,
    am.Struct{

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

### States File Help

```bash
$ am-gen states-file --help
Usage:
  am-gen states-file --name MyMach --states State1,State2:multi [flags]

Flags:
  -g, --groups string    Groups to generate. Eg: Group1,Group2
  -h, --help             help for states-file
  -i, --inherit string   Inherit from a built-in states machine: basic,connected,rpc/worker,node/worker
  -n, --name string      Name of the state machine. Eg: MyMach
  -s, --states string    State names to generate. Eg: State1,State2
      --utils            Generate states_utils.go in CWD. Overrides files. (default true)
```

## Grafana Dashboard

![grafana](https://pancsta.github.io/assets/asyncmachine-go/grafana.dark.png)

- generates separate dashboard per source (job)
- supports [Loki and Prometheus](/pkg/telemetry/README.md)
- can sync via `GRAFANA_TOKEN` via `--grafana-url`

```bash
am-gen grafana
    --name tree_state_source_root
    --ids root,_rm-root,_rs-root-0,_rs-root-1,_rs-root-2
    --grafana-url http://localhost:3000
    --source tree_state_source_rep1
```

### Grafana Help

```text
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
