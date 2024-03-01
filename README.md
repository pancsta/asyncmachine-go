# asyncmachine-go

`asyncmachine-go` is a minimal implementation of [AsyncMachine](https://github.com/TobiaszCudnik/asyncmachine) in
Golang using **channels and context**. It aims at simplicity and speed.

It can be used as a lightweight in-memory [Temporal](https://github.com/temporalio/temporal) alternative, worker for
[Asynq](https://github.com/hibiken/asynq), or to write simple consensus engines, stateful firewalls, telemetry, bots,
etc.

AsyncMachine is a relational state machine which never blocks.

```go
package main

import (
    "context"
    "fmt"

    am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func main() {
    ctx := context.Background()
    m := am.New(ctx, am.States{
        "Foo": {
            Add: am.S{"Bar"}
        },
        "Bar": {},
    }, nil)
    m.Add(am.S{"Foo"}, nil)
    fmt.Printf("%s", m) // (Foo:1 Bar:1)
}
```

## Examples

- [Expense Workflow](/examples/temporal-expense/expense_test.go) \|
  [Temporal version](https://github.com/temporalio/samples-go/blob/main/expense/) \| [Go playground](https://play.golang.com/p/P1eg6tKh6E4)

```go
am.States{
    // CreateExpenseActivity
    "CreatingExpense": {
        Remove: am.S{"ExpenseCreated"},
    },
    "ExpenseCreated": {
        Remove: am.S{"CreatingExpense"},
    },
    // WaitForDecisionActivity
    "WaitingForApproval": {
        Auto:    true,
        Require: am.S{"ExpenseCreated"},
        Remove:  am.S{"ApprovalGranted"},
    },
    "ApprovalGranted": {
        Require: am.S{"ExpenseCreated"},
        Remove:  am.S{"WaitingForApproval"},
    },
    // PaymentActivity
    "PaymentInProgress": {
        Auto:    true,
        Require: am.S{"ApprovalGranted"},
        Remove:  am.S{"PaymentCompleted"},
    },
    "PaymentCompleted": {
        Require: am.S{"ExpenseCreated", "ApprovalGranted"},
        Remove:  am.S{"PaymentInProgress"},
    },
}
```

- [FileProcessing workflow](/examples/temporal-fileprocessing/fileprocessing.go) \|
  [Temporal version](https://github.com/temporalio/samples-go/blob/main/fileprocessing/) \| [Go playground](https://play.golang.com/p/Fv92Xpzlzv6)

```go
am.States{
    // DownloadFileActivity
    "DownloadingFile": {
        Remove: am.S{"FileDownloaded"},
    },
    "FileDownloaded": {
        Remove: am.S{"DownloadingFile"},
    },
    // ProcessFileActivity
    "ProcessingFile": {
        Auto:    true,
        Require: am.S{"FileDownloaded"},
        Remove:  am.S{"FileProcessed"},
    },
    "FileProcessed": {
        Remove: am.S{"ProcessingFile"},
    },
    // UploadFileActivity
    "UploadingFile": {
        Auto:    true,
        Require: am.S{"FileProcessed"},
        Remove:  am.S{"FileUploaded"},
    },
    "FileUploaded": {
        Remove: am.S{"UploadingFile"},
    },
}
```

- [FileProcessing in an Asynq worker](examples/asynq-fileprocessing/fileprocessing_task.go)

```go
func HandleFileProcessingTask(ctx context.Context, t *asynq.Task) error {
    var p FileProcessingPayload
    if err := json.Unmarshal(t.Payload(), &p); err != nil {
        return fmt.Errorf("json.Unmarshal failed: %v: %w", err, asynq.SkipRetry)
    }
    log.Printf("Processing file %s", p.Filename)
    // use the FileProcessing workflow ported from Temporal
    machine, err := processor.FileProcessingFlow(ctx, log.Printf, p.Filename)
    if err != nil {
        return err
    }
    // save the machine state as the result
    ret := machine.String()
    if _, err := t.ResultWriter().Write([]byte(ret)); err != nil {
        return fmt.Errorf("failed to write task result: %v", err)
    }
    return nil
}
```

## Documentation

- [API godoc](https://godoc.org/github.com/pancsta/asyncmachine-go/pkg/machine)
- [Manual](/docs/manual.md)
   - [States](/docs/manual.md#states)
      - [State Clocks](/docs/manual.md#state-clocks)
      - [Auto States](/docs/manual.md#auto-states)
   - [Transitions](/docs/manual.md#transitions)
      - [Negotiation](/docs/manual.md#negotiation-handlers)
   - [Relations](/docs/manual.md#relations)
   - [Queue](/docs/manual.md#queue)

## TUI Debugger

`am-dbg` is a simple, yet effective tool to debug your machines, including:

- states with relations
- time travel
- transition steps
- logs

![TUI Debugger](assets/am-dbg.png)

### Installation

`go install github.com/pancsta/asyncmachine-go/tools/am-dbg@latest`

### Usage

Set up telemetry:

```go
import (
    "github.com/pancsta/asyncmachine-go/pkg/telemetry"
)
// ...
err := telemetry.MonitorTransitions(machine, telemetry.RpcHost)
```

Run `am-dbg`:

```text
Usage:
  am-dbg [flags]

Flags:
      --am-dbg-url string   Debug this instance of am-dbg with another one
      --enable-mouse        Enable mouse support
  -h, --help                help for am-dbg
      --log-file string     Log file path (default "am-dbg.log")
      --log-level int       Log level, 0-5 (silent-everything)
      --log-machine-id      Include machine ID in log messages (default true)
      --server-url string   Host and port for the server to listen on (default "localhost:9823")
```

## Changelog

Latest release: `v0.3.1`

See [CHANELOG.md](/CHANGELOG.md) for details.

## Status

**Beta** - although the ideas behind AsyncMachine have been proven to work, the golang implementation is fairly fresh
and may suffer from bugs, memory leaks, race conditions and even panics.
[Please report bugs](https://github.com/pancsta/asyncmachine-go/issues/new).

## TODO

See [issues](https://github.com/pancsta/asyncmachine-go/issues).
