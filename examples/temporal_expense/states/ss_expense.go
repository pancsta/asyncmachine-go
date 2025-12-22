// TODO update to v2 schema file

package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

// States map defines relations and properties of states.
var States = am.Schema{
	CreatingExpense: {Remove: GroupExpense},
	ExpenseCreated:  {Remove: GroupExpense},
	WaitingForApproval: {
		Auto:   true,
		Remove: GroupApproval,
	},
	ApprovalGranted: {Remove: GroupApproval},
	PaymentInProgress: {
		Auto:   true,
		Remove: GroupPayment,
	},
	PaymentCompleted: {Remove: GroupPayment},
}

// Groups of mutually exclusive states.

var (
	GroupExpense  = S{CreatingExpense, ExpenseCreated}
	GroupApproval = S{WaitingForApproval, ApprovalGranted}
	GroupPayment  = S{PaymentInProgress, PaymentCompleted}
)

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	CreatingExpense    = "CreatingExpense"
	ExpenseCreated     = "ExpenseCreated"
	WaitingForApproval = "WaitingForApproval"
	ApprovalGranted    = "ApprovalGranted"
	PaymentInProgress  = "PaymentInProgress"
	PaymentCompleted   = "PaymentCompleted"
)

// Names is an ordered list of all the state names.
var Names = S{CreatingExpense, ExpenseCreated, WaitingForApproval, ApprovalGranted, PaymentInProgress, PaymentCompleted}

// #endregion
