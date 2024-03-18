// Based on https://github.com/temporalio/samples-go/blob/main/expense/
//
// This example shows a simple payment workflow with an async approval input,
// going from CreatingExpense to PaymentCompleted.
//
// Part of the workflow is implemented imperatively, just like in Temporal.
//
// Sample output (LogLevel == LogChanges):
//
//=== RUN   TestExpense
//    expense_test.go:145: [state] +CreatingExpense
//    expense_test.go:145: [state:auto] +WaitingForApproval +PaymentInProgress
//    expense_test.go:164: waiting: CreatingExpense to WaitingForApproval
//    expense_test.go:180: waiting: WaitingForApproval to ApprovalGranted
//    expense_test.go:250: expense API called
//    expense_test.go:233: approval request received: 123
//    expense_test.go:263: payment API called
//    expense_test.go:145: [state] +ExpenseCreated -CreatingExpense
//    expense_test.go:145: [state] +PaymentCompleted -PaymentInProgress
//    expense_test.go:237: granting fake approval
//    expense_test.go:239: sent fake approval
//    expense_test.go:192: received approval ID: fake
//    expense_test.go:180: waiting: WaitingForApproval to ApprovalGranted
//    expense_test.go:243: granting real approval
//    expense_test.go:192: received approval ID: 123
//    expense_test.go:245: sent real approval
//    expense_test.go:145: [state] +ApprovalGranted -WaitingForApproval
//    expense_test.go:180: waiting: WaitingForApproval to ApprovalGranted
//    expense_test.go:213: waiting: PaymentInProgress to PaymentCompleted
//    expense_test.go:280:
//        (ApprovalGranted:1 PaymentCompleted:1 ExpenseCreated:1)
//    expense_test.go:281:
//        (ApprovalGranted:1 ExpenseCreated:1 PaymentCompleted:1)[CreatingExpense:2 Exception:0 PaymentInProgress:2 WaitingForApproval:2]
//    expense_test.go:282:
//        ApprovalGranted:
//          State:   true 1
//          Remove:  WaitingForApproval
//
//        CreatingExpense:
//          State:   false 2
//          Remove:  ExpenseCreated
//
//        Exception:
//          State:   false 0
//          Multi:   true
//
//        ExpenseCreated:
//          State:   true 1
//          Remove:  CreatingExpense
//
//        PaymentCompleted:
//          State:   true 1
//          Remove:  PaymentInProgress
//
//        PaymentInProgress:
//          State:   false 2
//          Auto:    true
//          Remove:  PaymentCompleted
//
//        WaitingForApproval:
//          State:   false 2
//          Auto:    true
//          Remove:  ApprovalGranted
//
//
//--- PASS: TestExpense (0.02s)
//PASS

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sse "github.com/pancsta/asyncmachine-go/examples/temporal-expense/states"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	//"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

var (
	expenseBackendURL string
	paymentBackendURL string
)

type Logger func(msg string, args ...any)

// NewMachine creates a new Expense machine with a CreatingExpense to
// PaymentCompleted flow.
func NewMachine(ctx context.Context) *am.Machine {
	// define states
	return am.New(ctx, sse.States, &am.Opts{
		DontLogID: true,
	})
}

// MachineHandlers is a struct of handlers & their data for the Expense machine.
// None of the handlers can block.
type MachineHandlers struct {
	// default handler for the build in Exception state
	am.ExceptionHandler
	expenseID string
}

// CreatingExpenseState is a _final_ entry handler for the CreatingExpense
// state.
func (h *MachineHandlers) CreatingExpenseState(e *am.Event) {
	// args definition
	h.expenseID = e.Args["expenseID"].(string)
	// get a context of this particular state's instance (clock's tick)
	stateCtx := e.Machine.NewStateCtx(sse.CreatingExpense)

	// never block in a handler, always fork
	go func() {
		resp, err := http.Get(expenseBackendURL + "?id=" + h.expenseID)
		if err != nil {
			e.Machine.AddErr(err)
			return
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			e.Machine.AddErr(err)
			return
		}
		if string(body) != "SUCCEED" {
			e.Machine.AddErrStr(string(body))
			return
		}

		// make sure this should still be happening
		if stateCtx.Err() != nil {
			return // expired
		}
		e.Machine.Add1(sse.ExpenseCreated, nil)
	}()
}

// ApprovalGrantedEnter is a _negotiation_ entry handler for the ApprovalGranted
// state. It can cancel the transition by returning false.
func (h *MachineHandlers) ApprovalGrantedEnter(e *am.Event) bool {
	// args definition
	approvedID := e.Args["approvedID"].(string)

	// return TRUE for when negotiating for ApprovalGranted
	// only if approved and expense is the same ID
	return approvedID == h.expenseID
}

// PaymentInProgressState is a _final_ entry handler for the PaymentInProgress
// state.
func (h *MachineHandlers) PaymentInProgressState(e *am.Event) {
	// never block in a handler, always "go func" it
	go func() {
		url := paymentBackendURL + "?id=" + h.expenseID
		resp, err := http.Get(url)
		if err != nil {
			e.Machine.AddErr(err)
			return
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			e.Machine.AddErr(err)
			return
		}

		if string(body) != "SUCCEED" {
			e.Machine.AddErrStr(string(body))
			return
		}

		e.Machine.Add1(sse.PaymentCompleted, nil)
	}()
}

// ExpenseFlow is an example of how to use the Expense machine.
//
// NOTE: this whole flow could be implemented declaratively as states,
// but mimics the `SampleExpenseWorkflow` function from the Temporal example
// for clarity.
//
// approvalCh: channel producing approved expense IDs
func ExpenseFlow(
	ctx context.Context, log Logger, approvalCh chan string, expenseID string,
) (*am.Machine, error) {
	// init
	machine := NewMachine(ctx)

	// different log granularity and a custom output
	machine.SetLogLevel(am.LogChanges)
	// machine.SetLogLevel(am.LogOps)
	// machine.SetLogLevel(am.LogDecisions)
	// machine.SetLogLevel(am.LogEverything)
	machine.SetLogger(func(l am.LogLevel, msg string, args ...any) {
		log(msg, args...)
	})

	// bind handlers
	err := machine.BindHandlers(&MachineHandlers{})
	if err != nil {
		return machine, err
	}

	// reusable error channel
	errCh := machine.WhenErr(nil)

	// start the flow
	machine.Add1(sse.CreatingExpense, am.A{"expenseID": expenseID})

	// CreatingExpense is an automatic state, it will add itself at this point.
	// string(machine) == [Enabled, CreatingExpense]

	// CreatingExpense to WaitingForApproval
	log("waiting: CreatingExpense to WaitingForApproval")
	select {
	case <-time.After(10 * time.Second):
		return machine, errors.New("timeout")
	case <-errCh:
		return machine, machine.Err
	case <-machine.When1(sse.WaitingForApproval, nil):
		// WaitingForApproval is an automatic state
	}

	_ = machine.String() // (ExpenseCreated:1 WaitingForApproval:1)

	// WaitingForApproval to ApprovalGranted
	// Passes all new approval IDs to the machine, multiple times.
	granted := false
	for !granted {
		log("waiting: WaitingForApproval to ApprovalGranted")

		// wait with a timeout
		select {

		// new approvals
		case approvedID, ok := <-approvalCh:
			if !ok {
				return machine, errors.New("approval channel closed")
			}

			// TRY to approve the existing expense
			log("received approval ID: %s", approvedID)
			machine.Add1(sse.ApprovalGranted, am.A{"approvedID": approvedID})

		// approval timeout
		case <-time.After(10 * time.Minute):
			return machine, errors.New("timeout")

		// error or machine disposed
		case <-errCh:
			return machine, machine.Err

		// approval granted
		case <-machine.When1(sse.ApprovalGranted, nil):
			granted = true
		}
	}

	// PaymentInProgress is an automatic state, it will add itself at this point.
	_ = machine.String() // (ExpenseCreated:1 ApprovalGranted:1 PaymentInProgress:1)

	// PaymentInProgress to PaymentCompleted
	log("waiting: PaymentInProgress to PaymentCompleted")
	select {
	case <-errCh:
		return machine, machine.Err
	case <-machine.When1(sse.PaymentCompleted, nil):
	}

	_ = machine.String() // (ExpenseCreated:1 ApprovalGranted:1 PaymentCompleted:1)

	return machine, nil
}

func TestExpense(t *testing.T) {
	// init
	expenseCh := make(chan string)
	approvalCh := make(chan string)

	// mock an external approval flow
	go func() {
		expenseId := <-expenseCh
		t.Log("approval request received: " + expenseId)
		time.Sleep(10 * time.Millisecond)

		// approve a random ID
		t.Log("granting fake approval")
		approvalCh <- "fake"
		t.Log("sent fake approval")
		time.Sleep(10 * time.Millisecond)

		// approve our ID
		t.Log("granting real approval")
		approvalCh <- expenseId
		t.Log("sent real approval")
	}()

	// mock the expense API
	expenseAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Log("expense API called")
		// new expense ID
		fmt.Fprint(w, "SUCCEED")
		// start the external approval flow
		expenseCh <- "123"
	}))
	defer expenseAPI.Close()
	expenseBackendURL = expenseAPI.URL

	// mock the payment API
	paymentAPI := httptest.NewServer(http.HandlerFunc(func(w http.
		ResponseWriter, r *http.Request,
	) {
		t.Log("payment API called")
		// payment successful
		_, _ = fmt.Fprint(w, "SUCCEED")
	}))
	defer paymentAPI.Close()
	paymentBackendURL = paymentAPI.URL

	// start the flow and wait for the result
	machine, err := ExpenseFlow(context.Background(), t.Logf, approvalCh, "123")
	if err != nil {
		t.Fatal(err)
	}
	if !machine.Is1(sse.PaymentCompleted) {
		t.Fatal("not PaymentCompleted")
	}

	// how it looks at the end
	t.Log("\n" + machine.String())
	t.Log("\n" + machine.StringAll())
	t.Log("\n" + machine.Inspect(nil))
}
