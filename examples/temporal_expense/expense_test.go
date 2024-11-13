// Based on https://github.com/temporalio/samples-go/blob/main/expense/
//
// This example shows a simple payment workflow with an async approval input,
// going from CreatingExpense to PaymentCompleted.
//
// Part of the workflow is implemented imperatively, just like in Temporal.
//
// Sample output (LogLevel == LogChanges):
//
// === RUN   TestExpense
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
// --- PASS: TestExpense (0.02s)
// PASS

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

	"github.com/joho/godotenv"

	ss "github.com/pancsta/asyncmachine-go/examples/temporal_expense/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var (
	expenseBackendURL string
	paymentBackendURL string
)

type Logger func(msg string, args ...any)

// MachineHandlers is a struct of handlers & their data for the Expense machine.
// None of the handlers can block.
type MachineHandlers struct {
	// default handler for the build in Exception state
	am.ExceptionHandler
	expenseID string
}

func init() {
	// load .env
	_ = godotenv.Load()

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetLogLevel(am.LogChanges)
}

// CreatingExpenseState is a _final_ entry handler for the CreatingExpense
// state.
func (h *MachineHandlers) CreatingExpenseState(e *am.Event) {
	// args definition
	h.expenseID = e.Args["expenseID"].(string)
	// tick-based ctx
	stateCtx := e.Machine.NewStateCtx(ss.CreatingExpense)

	// unblock
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}
		resp, err := http.Get(expenseBackendURL + "?id=" + h.expenseID)
		if err != nil {
			e.Machine.AddErr(err, nil)
			return
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			e.Machine.AddErr(err, nil)
			return
		}
		if string(body) != "SUCCEED" {
			e.Machine.AddErr(fmt.Errorf("%s", body), nil)
			return
		}

		// make sure this should still be happening
		if stateCtx.Err() != nil {
			return // expired
		}
		e.Machine.Add1(ss.ExpenseCreated, nil)
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
	// tick-based ctx
	stateCtx := e.Machine.NewStateCtx(ss.PaymentInProgress)

	// unblock
	go func() {
		if stateCtx.Err() != nil {
			return // expired
		}
		url := paymentBackendURL + "?id=" + h.expenseID
		resp, err := http.Get(url)
		if err != nil {
			e.Machine.AddErr(err, nil)
			return
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			e.Machine.AddErr(err, nil)
			return
		}

		if string(body) != "SUCCEED" {
			e.Machine.AddErr(fmt.Errorf("%s", body), nil)
			return
		}

		e.Machine.Add1(ss.PaymentCompleted, nil)
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
	mach := am.New(ctx, ss.States, &am.Opts{
		DontLogID: true,
	})
	amhelp.MachDebugEnv(mach)

	// different log granularity and a custom output
	mach.SetLogLevel(am.LogChanges)
	// machine.SetLogLevel(am.LogOps)
	// machine.SetLogLevel(am.LogDecisions)
	// machine.SetLogLevel(am.LogEverything)
	mach.SetLogger(func(l am.LogLevel, msg string, args ...any) {
		log(msg, args...)
	})

	// bind handlers
	err := mach.BindHandlers(&MachineHandlers{})
	if err != nil {
		return mach, err
	}

	// reusable error channel
	errCh := mach.WhenErr(nil)

	// start the flow
	mach.Add1(ss.CreatingExpense, am.A{"expenseID": expenseID})

	// CreatingExpense is an automatic state, it will add itself at this point.
	// string(machine) == [Enabled, CreatingExpense]

	// CreatingExpense to WaitingForApproval
	log("waiting: CreatingExpense to WaitingForApproval")
	select {
	case <-time.After(10 * time.Second):
		return mach, errors.New("timeout")
	case <-errCh:
		return mach, mach.Err()
	case <-mach.When1(ss.WaitingForApproval, nil):
		// WaitingForApproval is an automatic state
	}

	_ = mach.String() // (ExpenseCreated:1 WaitingForApproval:1)

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
				return mach, errors.New("approval channel closed")
			}

			// TRY to approve the existing expense
			log("received approval ID: %s", approvedID)
			mach.Add1(ss.ApprovalGranted, am.A{"approvedID": approvedID})

		// approval timeout
		case <-time.After(10 * time.Minute):
			return mach, errors.New("timeout")

		// error or machine disposed
		case <-errCh:
			return mach, mach.Err()

		// approval granted
		case <-mach.When1(ss.ApprovalGranted, nil):
			granted = true
		}
	}

	// PaymentInProgress is an automatic state, it will add itself at this point.
	_ = mach.String() // (ExpenseCreated:1 ApprovalGranted:1 PaymentInProgress:1)

	// PaymentInProgress to PaymentCompleted
	log("waiting: PaymentInProgress to PaymentCompleted")
	select {
	case <-errCh:
		return mach, mach.Err()
	case <-mach.When1(ss.PaymentCompleted, nil):
	}

	_ = mach.String() // (ExpenseCreated:1 ApprovalGranted:1 PaymentCompleted:1)

	return mach, nil
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
	mach, err := ExpenseFlow(context.Background(), t.Logf, approvalCh, "123")
	if err != nil {
		t.Fatal(err)
	}
	if !mach.Is1(ss.PaymentCompleted) {
		t.Fatal("not PaymentCompleted")
	}

	t.Log(mach.String())
	t.Log(mach.StringAll())
	t.Log(mach.Inspect(nil))
}
