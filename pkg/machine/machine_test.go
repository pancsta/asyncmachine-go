package machine_test

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type (
	S = am.S
	A = am.A
	T = am.T
)

// NewNoRels creates a new machine with no relations between states.
func NewNoRels(t *testing.T, initialState S) *am.Machine {
	m := am.New(context.Background(), am.States{
		"A": {},
		"B": {},
		"C": {},
		"D": {},
	}, nil)
	m.SetLogger(func(i am.LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	if os.Getenv("AM_DEBUG") != "" {
		m.SetLogLevel(am.LogEverything)
		m.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		m.Set(initialState, nil)
	}
	return m
}

// NewRels creates a new machine with basic relations between states.
func NewRels(t *testing.T, initialState S) *am.Machine {
	m := am.New(context.Background(), am.States{
		"A": {
			Auto:    true,
			Require: S{"C"},
		},
		"B": {
			Multi: true,
			Add:   S{"C"},
		},
		"C": {
			After: S{"D"},
		},
		"D": {
			Add: S{"C", "B"},
		},
	}, nil)
	m.SetLogger(func(i am.LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	if os.Getenv("AM_DEBUG") != "" {
		m.SetLogLevel(am.LogEverything)
		m.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		m.Set(initialState, nil)
	}
	return m
}

// NewNoRels creates a new machine with no relations between states.
func NewCustomStates(t *testing.T, states am.States) *am.Machine {
	m := am.New(context.Background(), states, nil)
	m.SetLogger(func(i am.LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	if os.Getenv("AM_DEBUG") != "" {
		m.SetLogLevel(am.LogEverything)
		m.HandlerTimeout = 2 * time.Minute
	}
	return m
}

func TestSingleStateActive(t *testing.T) {
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	assertStates(t, m, S{"A"})
}

func TestMultipleStatesActive(t *testing.T) {
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	m.Add(S{"B"}, nil)
	assertStates(t, m, S{"A", "B"})
}

func TestExposeAllStateNames(t *testing.T) {
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	assert.ElementsMatch(t, S{"A", "B", "C", "D", "Exception"}, m.StateNames)
}

func TestStateSet(t *testing.T) {
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	m.Set(S{"B"}, nil)
	assertStates(t, m, S{"B"})
}

func TestStateAdd(t *testing.T) {
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	m.Add(S{"B"}, nil)
	assertStates(t, m, S{"A", "B"})
}

func TestStateRemove(t *testing.T) {
	m := NewNoRels(t, S{"B", "C"})
	defer m.Dispose()
	m.Remove(S{"C"}, nil)
	assertStates(t, m, S{"B"})
}

func TestPanicWhenStateIsUnknown(t *testing.T) {
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	assert.Panics(t, func() {
		m.Set(S{"E"}, nil)
	})
}

func TestGetStateRelations(t *testing.T) {
	m := am.New(context.Background(), am.States{
		"A": {
			Add:     S{"B"},
			Require: S{"B"},
			Auto:    true,
		},
		"B": {},
	}, nil)
	defer m.Dispose()
	assert.Equal(t, []am.Relation{am.RelationAdd, am.RelationRequire},
		m.GetRelationsOf("A"))
}

func TestGetRelationsBetweenStates(t *testing.T) {
	m := am.New(context.Background(), am.States{
		"A": {
			Add:     S{"B"},
			Require: S{"C"},
			Auto:    true,
		},
		"B": {},
		"C": {},
	}, nil)
	defer m.Dispose()
	assert.Equal(t, []am.Relation{am.RelationAdd},
		m.GetRelationsBetween("A", "B"))
}

func TestSingleToSingleStateTransition(t *testing.T) {
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()
	events := []string{
		"AExit", "AC", "AAny", "BExit", "BC", "BAny", "AnyC", "CEnter",
	}
	history := trackTransitions(m, events)
	// transition
	m.Set(S{"C"}, nil)
	// assert the final state
	assert.ElementsMatch(t, S{"C"}, m.ActiveStates)
	// assert the order
	assert.Equal(t, events, history.Order)
	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, 1, count)
	}
}

func TestSingleToMultiStateTransition(t *testing.T) {
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	events := []string{
		"AExit", "AB", "AC", "AAny", "AnyB", "BEnter", "AnyC", "CEnter",
		"BState", "CState",
	}
	history := trackTransitions(m, events)
	// transition
	m.Set(S{"B", "C"}, nil)
	// assert the final state
	assert.ElementsMatch(t, S{"B", "C"}, m.ActiveStates)
	// assert the order
	assert.Equal(t, events, history.Order)
	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, 1, count)
	}
}

func TestMultiToMultiStateTransition(t *testing.T) {
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()
	events := []string{
		"AExit", "AD", "AC", "AAny", "BExit", "BD", "BC", "BAny",
		"AnyD", "DEnter", "AnyC", "CEnter",
	}
	history := trackTransitions(m, events)
	// transition
	m.Set(S{"D", "C"}, nil)
	// assert the final state
	assert.ElementsMatch(t, S{"D", "C"}, m.ActiveStates)
	// assert the order
	assert.Equal(t, events, history.Order)
	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, 1, count)
	}
}

func TestMultiToSingleStateTransition(t *testing.T) {
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()
	events := []string{
		"AExit", "AC", "AAny", "BExit", "BC", "BAny", "AnyC",
		"CEnter",
	}
	history := trackTransitions(m, events)
	// transition
	m.Set(S{"C"}, nil)
	// assert the final state
	assert.ElementsMatch(t, S{"C"}, m.ActiveStates)
	// assert the order
	assert.Equal(t, events, history.Order)
	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, 1, count)
	}
}

func TestTransitionToActiveState(t *testing.T) {
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()
	// history
	events := []string{"AExit", "AnyA", "AnyA"}
	history := trackTransitions(m, events)
	// transition
	m.Set(S{"A"}, nil)
	// assert the final state
	assert.ElementsMatch(t, S{"A"}, m.ActiveStates)
	// assert event counts (none should happen)
	for _, count := range history.Counter {
		assert.Equal(t, 0, count)
	}
}

func TestAfterRelationWhenEntering(t *testing.T) {
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()
	// relations
	m.States["C"] = am.State{After: S{"D"}}
	m.States["A"] = am.State{After: S{"B"}}
	// history
	events := []string{"AD", "AC", "AnyD", "DEnter", "AnyC", "CEnter"}
	history := trackTransitions(m, events)
	// transition
	m.Set(S{"C", "D"}, nil)
	// assert the final state
	assertStates(t, m, S{"C", "D"})
	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, 1, count)
	}
}

func TestAfterRelationWhenExiting(t *testing.T) {
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()
	// relations
	m.States["C"] = am.State{After: S{"D"}}
	m.States["A"] = am.State{After: S{"B"}}
	// history
	events := []string{"BExit", "BD", "BC", "BAny", "AExit", "AD", "AC", "AAny"}
	history := trackTransitions(m, events)
	// transition
	m.Set(S{"C", "D"}, nil)
	// assert the final state
	assertStates(t, m, S{"C", "D"})
	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, 1, count)
	}
}

func TestRemoveRelation(t *testing.T) {
	m := NewNoRels(t, S{"D"})
	defer m.Dispose()
	// relations
	m.States["C"] = am.State{Remove: S{"D"}}
	// C deactivates D
	m.Add(S{"C"}, nil)
	assertStates(t, m, S{"C"})
}

func TestRemoveRelationSimultaneous(t *testing.T) {
	// init
	m := NewNoRels(t, S{"D"})
	defer m.Dispose()
	// logger
	log := ""
	captureLog(t, m, &log)
	// relations
	m.States["C"] = am.State{Remove: S{"D"}}
	// test
	r := m.Set(S{"C", "D"}, nil)
	// assert
	assert.Equal(t, am.Canceled, r)
	assert.Contains(t, log, "[rel:remove] D by C")
	assertStates(t, m, S{"D"})
}

func TestRemoveRelationCrossBlocking(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, m *am.Machine)
	}{
		{
			"using Set should de-activate the old one",
			func(t *testing.T, m *am.Machine) {
				// m = (D)
				m.Set(S{"C"}, nil)
				assertStates(t, m, S{"C"})
			},
		},
		{
			"using Set should work both ways",
			func(t *testing.T, m *am.Machine) {
				// m = (D)
				m.Set(S{"C"}, nil)
				assertStates(t, m, S{"C"})
				m.Set(S{"D"}, nil)
				assertStates(t, m, S{"D"})
			},
		},
		{
			"using Add should de-activate the old one",
			func(t *testing.T, m *am.Machine) {
				// m = (D)
				m.Add(S{"C"}, nil)
				assertStates(t, m, S{"C"})
			},
		},
		{
			"using Add should work both ways",
			func(t *testing.T, m *am.Machine) {
				// m = (D)
				m.Add(S{"C"}, nil)
				assertStates(t, m, S{"C"})
				m.Add(S{"D"}, nil)
				assertStates(t, m, S{"D"})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := NewNoRels(t, S{"D"})
			defer m.Dispose()
			// C and D are cross blocking each other via Remove
			m.States["C"] = am.State{Remove: S{"D"}}
			m.States["D"] = am.State{Remove: S{"C"}}
			test.fn(t, m)
		})
	}
}

func TestAddRelation(t *testing.T) {
	m := NewNoRels(t, nil)
	defer m.Dispose()
	// relations
	m.States["A"] = am.State{Remove: S{"D"}}
	m.States["C"] = am.State{Add: S{"D"}}
	// test
	m.Set(S{"C"}, nil)
	// assert
	assertStates(t, m, S{"C", "D"}, "state should be activated")
	m.Set(S{"A", "C"}, nil)
	assertStates(t, m, S{"A", "C"}, "state should be skipped if "+
		"blocked at the same time")
}

func TestRequireRelation(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// relations
	m.States["C"] = am.State{Require: S{"D"}}
	// run the test
	m.Set(S{"C", "D"}, nil)
	// assert
	assertStates(t, m, S{"C", "D"})
}

func TestRequireRelationWhenRequiredIsntActive(t *testing.T) {
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// relations
	m.States["C"] = am.State{Require: S{"D"}}
	// logger
	log := ""
	captureLog(t, m, &log)
	// test
	m.Set(S{"C", "A"}, nil)
	// assert
	assertStates(t, m, S{"A"}, "target state shouldnt be activated")
	assert.Contains(t, log, "[reject] C(-D)",
		"log should explain the reason of rejection")
}

// TestQueue
type TestQueueHandlers struct{}

func (h *TestQueueHandlers) BEnter(e *am.Event) bool {
	e.Machine.Add(S{"C"}, nil)
	return true
}

func TestQueue(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// history
	events := []string{"CEnter", "AExit"}
	history := trackTransitions(m, events)
	// handlers
	bindings, err := m.BindHandlers(&TestQueueHandlers{})
	assert.NoError(t, err)
	<-bindings.Ready
	// triggers Add(C) from BEnter
	m.Set(S{"B"}, nil)
	// assert
	assertEventCounts(t, history, 1)
	assertStates(t, m, S{"C", "B"})
}

// TestNegotiationCancel
type TestNegotiationCancelHandlers struct{}

func (h *TestNegotiationCancelHandlers) DEnter(_ *am.Event) bool {
	return false
}

// TODO when transition is canceled, it should not set the auto states
func TestNegotiationCancel(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, m *am.Machine) am.Result
		log  *regexp.Regexp
	}{
		{
			"using set",
			func(t *testing.T, m *am.Machine) am.Result {
				// m = (A)
				// DEnter will cancel the transition
				return m.Set(S{"D"}, nil)
			},
			regexp.MustCompile(`\[cancel:\w+?] \(D\) by DEnter`),
		},
		{
			"using add",
			func(t *testing.T, m *am.Machine) am.Result {
				// m = (A)
				// DEnter will cancel the transition
				return m.Add(S{"D"}, nil)
			},
			regexp.MustCompile(`\[cancel:\w+?] \(D A\) by DEnter`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// init
			m := NewNoRels(t, S{"A"})
			defer m.Dispose()
			// bind handlers
			bindings, err := m.BindHandlers(&TestNegotiationCancelHandlers{})
			assert.NoError(t, err)
			<-bindings.Ready
			// bind logger
			log := ""
			captureLog(t, m, &log)
			// run the test
			result := test.fn(t, m)
			// assert
			assert.Equal(t, am.Canceled, result, "transition should be canceled")
			assertStates(t, m, S{"A"}, "state shouldnt be changed")
			assert.Regexp(t, test.log, log,
				"log should explain the reason of cancellation")
		})
	}
}

func TestAutoStates(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	defer m.Dispose()
	// relations
	m.States["B"] = am.State{
		Auto:    true,
		Require: S{"A"},
	}
	// bind logger
	log := ""
	captureLog(t, m, &log)
	// run the test
	result := m.Add(S{"A"}, nil)
	// assert
	assert.Equal(t, am.Executed, result, "transition should be executed")
	assertStates(t, m, S{"A", "B"}, "dependant auto state should be set")
	assert.Contains(t, log, "[auto] B", "log should mention the auto state")
}

// TestNegotiationRemove
type TestNegotiationRemoveHandlers struct{}

func (h *TestNegotiationRemoveHandlers) AExit(_ *am.Event) bool {
	return false
}

func TestNegotiationRemove(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// bind handlers
	bindings, err := m.BindHandlers(&TestNegotiationRemoveHandlers{})
	assert.NoError(t, err)
	<-bindings.Ready
	// bind logger
	log := ""
	captureLog(t, m, &log)
	// run the test
	// AExit will cancel the transition
	result := m.Remove(S{"A"}, nil)
	// assert
	assert.Equal(t, am.Canceled, result, "transition should be canceled")
	assertStates(t, m, S{"A"}, "state shouldnt be changed")
	assert.Regexp(t, `\[cancel:\w+?] \(\) by AExit`, log,
		"log should explain the reason of cancellation")
}

// TestHandlerStateInfo
type TestHandlerStateInfoHandlers struct{}

func (h *TestHandlerStateInfoHandlers) DEnter(e *am.Event) {
	t := e.Args["t"].(*testing.T)
	assert.ElementsMatch(t, S{"A"}, e.Machine.ActiveStates,
		"provide the previous states of the transition")
	assert.ElementsMatch(t, S{"D"}, e.Machine.To(),
		"provide the target states of the transition")
}

func TestHandlerStateInfo(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// bind history
	events := []string{"DEnter"}
	history := trackTransitions(m, events)
	// bind handlers
	bindings, err := m.BindHandlers(&TestHandlerStateInfoHandlers{})
	assert.NoError(t, err)
	<-bindings.Ready
	// bind logger
	log := ""
	captureLog(t, m, &log)
	// run the test
	// DEnter will assert
	m.Set(S{"D"}, A{"t": t})
	// assert
	assertEventCounts(t, history, 1)
}

// TestHandlerArgs
type TestHandlerArgsHandlers struct{}

func (h *TestHandlerArgsHandlers) BEnter(e *am.Event) {
	t := e.Args["t"].(*testing.T)
	foo := e.Args["foo"].(string)
	assert.Equal(t, "bar", foo)
}

func (h *TestHandlerArgsHandlers) AExit(e *am.Event) {
	t := e.Args["t"].(*testing.T)
	foo := e.Args["foo"].(string)
	assert.Equal(t, "bar", foo)
}

func (h *TestHandlerArgsHandlers) CState(e *am.Event) {
	t := e.Args["t"].(*testing.T)
	foo := e.Args["foo"].(string)
	assert.Equal(t, "bar", foo)
}

func TestHandlerArgs(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// bind history
	events := []string{"AExit", "BEnter", "CState"}
	history := trackTransitions(m, events)
	// bind handlers
	bindings, err := m.BindHandlers(&TestHandlerArgsHandlers{})
	assert.NoError(t, err)
	<-bindings.Ready
	// run the test
	// handlers will assert
	m.Add(S{"B"}, A{"t": t, "foo": "bar"})
	m.Remove(S{"A"}, A{"t": t, "foo": "bar"})
	m.Set(S{"C"}, A{"t": t, "foo": "bar"})
	// assert
	assertEventCountsMin(t, history, 1)
}

// TestSelfHandlersCancellable
type TestSelfHandlersCancellableHandlers struct{}

func (h *TestSelfHandlersCancellableHandlers) AA(_ *am.Event) bool {
	return false
}

func TestSelfHandlersCancellable(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// bind history
	events := []string{"AA", "AnyB"}
	history := trackTransitions(m, events)
	// bind handlers
	bindings, err := m.BindHandlers(&TestSelfHandlersCancellableHandlers{})
	assert.NoError(t, err)
	<-bindings.Ready
	// run the test
	// handlers will assert
	m.Set(S{"A", "B"}, nil)
	// assert
	assert.Equal(t, 1, history.Counter["AA"], "AA call count")
	assert.Equal(t, 0, history.Counter["AnyB"], "AnyB call count")
}

func TestSelfHandlersOrder(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// bind history
	events := []string{"AA", "AnyB", "BEnter"}
	history := trackTransitions(m, events)
	// run the test
	m.Set(S{"A", "B"}, nil)
	// assert
	assert.Equal(t, events, history.Order)
}

func TestSelfHandlersForCalledOnly(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// bind history
	events := []string{"AA", "BB"}
	history := trackTransitions(m, events)
	// run the test
	m.Add(S{"B"}, nil)
	m.Add(S{"A"}, nil)
	// assert
	assert.Equal(t, 1, history.Counter["AA"], "AA call count")
	assert.Equal(t, 0, history.Counter["BB"], "BB call count")
}

func TestRegressionRemoveCrossBlockedByImplied(t *testing.T) {
	// init
	m := NewCustomStates(t, am.States{
		"A": {Remove: S{"B"}},
		"B": {Remove: S{"A"}},
		"Z": {Add: S{"B"}},
	})
	defer m.Dispose()
	// run the test
	m.Set(S{"Z"}, nil)
	// assert
	assertStates(t, m, S{"Z", "B"})
}

func TestRegressionImpliedBlockByBeingRemoved(t *testing.T) {
	// init
	m := NewCustomStates(t, am.States{
		"Wet":   {Require: S{"Water"}},
		"Dry":   {Remove: S{"Wet"}},
		"Water": {Add: S{"Wet"}, Remove: S{"Dry"}},
	})
	defer m.Dispose()
	// run the test
	m.Set(S{"Dry"}, nil)
	m.Set(S{"Water"}, nil)
	// assert
	assertStates(t, m, S{"Water", "Wet"})
}

// TestWhen
type TestWhenHandlers struct{}

func (h *TestWhenHandlers) AState(e *am.Event) {
	go func() {
		time.Sleep(10 * time.Millisecond)
		e.Machine.Add(S{"B"}, nil)
		time.Sleep(10 * time.Millisecond)
		e.Machine.Add(S{"C"}, nil)
	}()
}

func TestWhen(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	defer m.Dispose()
	// bind handlers
	bindings, err := m.BindHandlers(&TestWhenHandlers{})
	assert.NoError(t, err)
	<-bindings.Ready
	// run the test
	m.Set(S{"A"}, nil)
	<-m.When(S{"B", "C"}, nil)
	// assert
	assertStates(t, m, S{"A", "B", "C"})
}

func TestWhenActive(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// run the test
	<-m.When(S{"A"}, nil)
	// assert
	assertStates(t, m, S{"A"})
}

// TestWhenNot
type TestWhenNotHandlers struct{}

func (h *TestWhenNotHandlers) AState(e *am.Event) {
	go func() {
		time.Sleep(10 * time.Millisecond)
		e.Machine.Remove(S{"B"}, nil)
		time.Sleep(10 * time.Millisecond)
		e.Machine.Remove(S{"C"}, nil)
	}()
}

func TestWhenNot(t *testing.T) {
	// init
	m := NewNoRels(t, S{"B", "C"})
	defer m.Dispose()
	// bind handlers
	bindings, err := m.BindHandlers(&TestWhenNotHandlers{})
	assert.NoError(t, err)
	<-bindings.Ready
	// run the test
	m.Add(S{"A"}, nil)
	<-m.WhenNot(S{"B", "C"}, nil)
	// assert
	assertStates(t, m, S{"A"})
	assertNoException(t, m)
}

func TestWhenNotActive(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// run the test
	<-m.WhenNot(S{"B"}, nil)
	// assert
	assertStates(t, m, S{"A"})
}

// TestPartialNegotiationPanic
type TestPartialNegotiationPanicHandlers struct {
	am.ExceptionHandler
}

func (h *TestPartialNegotiationPanicHandlers) BEnter(_ *am.Event) {
	panic("BEnter panic")
}

func TestPartialNegotiationPanic(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// logger
	log := ""
	captureLog(t, m, &log)
	// bind handlers
	bindings, err := m.BindHandlers(&TestPartialNegotiationPanicHandlers{})
	assert.NoError(t, err)
	<-bindings.Ready
	// run the test
	assert.Equal(t, am.Canceled, m.Add(S{"B"}, nil))
	// assert
	assertStates(t, m, S{"A", "Exception"})
	assert.Regexp(t, `\[cancel:\w+?] \(B A\) by recover`, log,
		"log contains the target states and handler")
}

// TestPartialFinalPanic
type TestPartialFinalPanicHandlers struct {
	am.ExceptionHandler
}

func (h *TestPartialFinalPanicHandlers) BState(_ *am.Event) {
	panic("BState panic")
}

func TestPartialFinalPanic(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	defer m.Dispose()
	// logger
	log := ""
	captureLog(t, m, &log)
	// bind handlers
	bindings, err := m.BindHandlers(&TestPartialFinalPanicHandlers{})
	assert.NoError(t, err)
	<-bindings.Ready
	// run the test
	m.Add(S{"A", "B", "C"}, nil)
	// assert
	assertStates(t, m, S{"A", "Exception"})
	assert.Contains(t, log, "[error:add] A B C (BState",
		"log contains the target states and handler")
	assert.Contains(t, log, "[error:trace] goroutine",
		"log contains the stack trace")
	assert.Regexp(t, `\[cancel:\w+?] \(A B C\) by recover`, log,
		"log contains the target states and handler")
}

// TestStateCtx
type TestStateCtxHandlers struct {
	am.ExceptionHandler
	callbackCh chan bool
}

func (h *TestStateCtxHandlers) AState(e *am.Event) {
	t := e.Args["t"].(*testing.T)
	stepCh := e.Args["stepCh"].(chan bool)
	stateCtx := e.Machine.GetStateCtx("A")
	h.callbackCh = make(chan bool)
	go func() {
		<-stepCh
		assertStates(t, e.Machine, S{})
		assert.Error(t, stateCtx.Err(), "state context should be canceled")
		h.callbackCh <- true
	}()
}

func TestStateCtx(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	defer m.Dispose()
	// bind handlers
	handlers := &TestStateCtxHandlers{}
	bindings, err := m.BindHandlers(handlers)
	assert.NoError(t, err)
	<-bindings.Ready
	// run the test
	// BState will assert
	stepCh := make(chan bool)
	m.Add(S{"A"}, A{
		"t":      t,
		"stepCh": stepCh,
	})
	m.Remove(S{"A"}, nil)
	stepCh <- true
	<-handlers.callbackCh
	// assert
	assertStates(t, m, S{})
}

// TestQueueCheckable
type TestQueueCheckableHandlers struct {
	am.ExceptionHandler
	assertsCount int
}

func (h *TestQueueCheckableHandlers) AState(e *am.Event) {
	t := e.Args["t"].(*testing.T)
	e.Machine.Add(S{"B"}, nil)
	e.Machine.Add(S{"C"}, nil)
	e.Machine.Add(S{"D"}, nil)
	assert.Len(t, e.Machine.Queue, 3, "queue should have 3 mutations scheduled")
	h.assertsCount++
	assert.Equal(t, 1,
		e.Machine.IsQueued(am.MutationTypeAdd, S{"C"}, false, false, 0),
		"C should be queued")
	h.assertsCount++
	assert.Equal(t, -1,
		e.Machine.IsQueued(am.MutationTypeAdd, S{"A"}, false, false, 0),
		"A should NOT be queued")
	h.assertsCount++
}

func TestQueueCheckable(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	defer m.Dispose()
	// bind handlers
	handlers := &TestQueueCheckableHandlers{}
	bindings, err := m.BindHandlers(handlers)
	assert.NoError(t, err)
	<-bindings.Ready
	// test
	m.Add(S{"A"}, A{"t": t})
	// assert
	assert.Equal(t, 3, handlers.assertsCount, "asserts executed")
	assertNoException(t, m)
}

func TestPartialAuto(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	defer m.Dispose()
	// relations
	m.States["C"] = am.State{
		Auto:    true,
		Require: S{"B"},
	}
	m.States["D"] = am.State{
		Auto:    true,
		Require: S{"B"},
	}
	// logger
	log := ""
	captureLog(t, m, &log)
	// run the test
	m.Add(S{"A"}, nil)
	// assert
	assertStates(t, m, S{"A"})
	assert.Regexp(t, `\[cancel:reject\] [C D]{3}`, log)
}

func TestClock(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	// relations
	m.States["B"] = am.State{Multi: true}

	// test 1
	// ()[]
	m.Add(S{"A", "B"}, nil)
	// (A B)[A1 B1]
	m.Add(S{"A", "B"}, nil)
	// (A B)[A1 B2]
	m.Add(S{"A", "B", "C"}, nil)
	// (A B C)[A1 B3 C1]
	m.Set(S{"D"}, nil)
	// (D)[A1 B3 C1 D1]
	m.Add(S{"D", "C"}, nil)
	// (D C)[A1 B3 C2 D1]
	m.Remove(S{"B", "C"}, nil)
	// (D)[A1 B3 C2 D1]
	m.Add(S{"A", "B"}, nil)
	// (D A B)[A2 B4 C2 D1]

	// assert
	assertStates(t, m, S{"A", "B", "D"})
	assert.Equal(t, m.Time(S{"A", "B", "C", "D"}), T{2, 4, 2, 1})

	// test 2
	order := S{"A", "B", "C", "D"}
	before := m.Time(order)
	m.Add(S{"C"}, nil)
	now := m.Time(order)

	// assert
	assertStates(t, m, S{"A", "B", "D", "C"})
	assert.Equal(t, m.Time(order), T{2, 4, 3, 1})
	assert.True(t, am.IsTimeAfter(now, before))
	assert.False(t, am.IsTimeAfter(before, now))
}

func TestWhenCtx(t *testing.T) {
	// TODO TestWhenCtx
	// wait on 2 Whens with a step context
	// make sure everything gets disposed
	t.Skip()
}

func TestConfig(t *testing.T) {
	// TODO TestConfig
	t.Skip()
}

func TestPanic(t *testing.T) {
	// TODO TestPanic
	t.Skip()
}

func TestIs(t *testing.T) {
	// TODO TestIs
	t.Skip()
}

func TestNot(t *testing.T) {
	// TODO TestNot
	t.Skip()
}

func TestAny(t *testing.T) {
	// TODO TestAny
	t.Skip()
}

func TestIsClock(t *testing.T) {
	// TODO TestIsClock
	t.Skip()
}

func TestDynamicHandlers(t *testing.T) {
	// TODO TestDynamicHandlers On(), On(..., ctx)
	t.Skip()
}

func TestInspect(t *testing.T) {
	m := NewRels(t, S{"A", "C"})
	names := S{"A", "B", "C", "D", "Exception"}
	expected := `
		A:
		  State:   true 1
		  Auto:    true
		  Require: C
		
		B:
		  State:   false 0
		  Multi:   true
		  Add:     C
		
		C:
		  State:   true 1
		  After:   D
		
		D:
		  State:   false 0
		  Add:     C B
	
		Exception:
		  State:   false 0
		  Multi:   true
		`
	assertString(t, m, expected, names)
	m.Add(S{"B"}, nil)
	m.Remove(S{"C"}, nil)
	m.Add(S{"D"}, nil)
	expected = `
		A:
		  State:   true 2
		  Auto:    true
		  Require: C
		
		B:
		  State:   true 1
		  Multi:   true
		  Add:     C
		
		C:
		  State:   true 2
		  After:   D
		
		D:
		  State:   true 1
		  Add:     C B
		
		Exception:
		  State:   false 0
		  Multi:   true
		`
	assertString(t, m, expected, names)
}
