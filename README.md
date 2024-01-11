# asyncmachine-go

`asyncmachine-go` is a minimal implementation of [AsyncMachine](https://github.com/TobiaszCudnik/asyncmachine) in
Golang using _channels_ and _context_. It aims at simplicity and speed.

It can be used as a lightweight in-memory [Temporal](https://github.com/temporalio/temporal) alternative, worker for
[Asynq](https://github.com/hibiken/asynq), or to write simple consensus engines, stateful firewalls, telemetry, bots,
etc.

AsyncMachine is a relational state machine which never blocks.

## Examples

- [Expense Workflow](/examples/temporal-expense/expense_test.go) \|
  [Temporal version](https://github.com/temporalio/samples-go/blob/main/expense/) \| [Go playground](https://goplay.tools/snippet/KAxlf3gm7pH)

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
  [Temporal version](https://github.com/temporalio/samples-go/blob/main/fileprocessing/) \| [Go playground](https://goplay.tools/snippet/aTo4hsyJZck)

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

## Status

**Beta** - although the ideas behind AsyncMachine have been proven to work, the golang implementation is fairly fresh
and may suffer from bugs, memory leaks, race conditions and even panics.
[Please report bugs](https://github.com/pancsta/asyncmachine-go/issues/new).
