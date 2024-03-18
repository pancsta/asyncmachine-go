package machine

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TODO ExampleNew
func ExampleNew() {
	t := &testing.T{} // Replace this with actual *testing.T in real test cases
	initialState := S{"A"}

	mach := NewNoRels(t, initialState)

	// Use the machine m here
	_ = mach
}

// TODO ExampleNewCommon
func ExampleNewCommon() {
	t := &testing.T{} // Replace this with actual *testing.T in real test cases
	initialState := S{"A"}

	mach := NewNoRels(t, initialState)

	// Use the machine m here
	_ = mach
}

// NewNoRels creates a new machine with no relations between states.
func NewNoRels(t *testing.T, initialState S) *Machine {
	m := New(context.Background(), Struct{
		"A": {},
		"B": {},
		"C": {},
		"D": {},
	}, nil)

	m.SetLogger(func(i LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})

	if os.Getenv("AM_DEBUG") != "" {
		m.SetLogLevel(LogEverything)
		m.HandlerTimeout = 2 * time.Minute
	}

	if initialState != nil {
		m.Set(initialState, nil)
	}

	return m
}

// NewRels creates a new machine with basic relations between states.
func NewRels(t *testing.T, initialState S) *Machine {
	m := New(context.Background(), Struct{
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
	m.SetLogger(func(i LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	if os.Getenv("AM_DEBUG") != "" {
		m.SetLogLevel(LogEverything)
		m.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		m.Set(initialState, nil)
	}
	return m
}

// NewNoRels creates a new machine with no relations between states.
func NewCustomStates(t *testing.T, states Struct) *Machine {
	m := New(context.Background(), states, nil)
	m.SetLogger(func(i LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	if os.Getenv("AM_DEBUG") != "" {
		m.SetLogLevel(LogEverything)
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
	m := New(context.Background(), Struct{
		"A": {
			Add:     S{"B"},
			Require: S{"B"},
			Auto:    true,
		},
		"B": {},
	}, nil)
	defer m.Dispose()

	of, err := m.Resolver.GetRelationsOf("A")

	assert.NoErrorf(t, err, "all states are known")
	assert.Nil(t, err)
	assert.Equal(t, []Relation{RelationAdd, RelationRequire},
		of)
}

func TestGetRelationsBetweenStates(t *testing.T) {
	m := New(context.Background(), Struct{
		"A": {
			Add:     S{"B"},
			Require: S{"C"},
			Auto:    true,
		},
		"B": {},
		"C": {},
	}, nil)
	defer m.Dispose()

	between, err := m.Resolver.GetRelationsBetween("A", "B")

	assert.NoError(t, err)
	assert.Equal(t, []Relation{RelationAdd},
		between)
}

func TestSingleToSingleStateTransition(t *testing.T) {
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()

	// expectations
	events := []string{
		"AExit", "AC", "AAny", "BExit", "BC", "BAny", "AnyC", "CEnter",
	}
	history := trackTransitions(m, events)

	// transition
	m.Set(S{"C"}, nil)

	// assert the final state
	assert.ElementsMatch(t, S{"C"}, m.activeStates)

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert the order
	assert.Equal(t, events, history.Order)
	// assert event counts
	assertEventCounts(t, history, 1)
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
	assert.ElementsMatch(t, S{"B", "C"}, m.activeStates)

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert the order
	assert.Equal(t, events, history.Order)
	// assert event counts
	assertEventCounts(t, history, 1)
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
	assert.ElementsMatch(t, S{"D", "C"}, m.activeStates)

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert the order
	assert.Equal(t, events, history.Order)
	// assert event counts
	assertEventCounts(t, history, 1)
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
	assert.ElementsMatch(t, S{"C"}, m.activeStates)

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert the order
	assert.Equal(t, events, history.Order)
	// assert event counts
	assertEventCounts(t, history, 1)
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
	assert.ElementsMatch(t, S{"A"}, m.activeStates)
	// assert event counts (none should happen)
	for _, count := range history.Counter {
		assert.Equal(t, 0, count)
	}
}

func TestAfterRelationWhenEntering(t *testing.T) {
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()
	// relations
	m.states["C"] = State{After: S{"D"}}
	m.states["A"] = State{After: S{"B"}}
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
	m.states["C"] = State{After: S{"D"}}
	m.states["A"] = State{After: S{"B"}}
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
	m.states["C"] = State{Remove: S{"D"}}
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
	m.states["C"] = State{Remove: S{"D"}}
	// test
	r := m.Set(S{"C", "D"}, nil)
	// assert
	assert.Equal(t, Canceled, r)
	assert.Contains(t, log, "[rel:remove] D by C")
	assertStates(t, m, S{"D"})
}

func TestRemoveRelationCrossBlocking(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, m *Machine)
	}{
		{
			"using Set should de-activate the old one",
			func(t *testing.T, m *Machine) {
				// m = (D)
				m.Set(S{"C"}, nil)
				assertStates(t, m, S{"C"})
			},
		},
		{
			"using Set should work both ways",
			func(t *testing.T, m *Machine) {
				// m = (D)
				m.Set(S{"C"}, nil)
				assertStates(t, m, S{"C"})
				m.Set(S{"D"}, nil)
				assertStates(t, m, S{"D"})
			},
		},
		{
			"using Add should de-activate the old one",
			func(t *testing.T, m *Machine) {
				// m = (D)
				m.Add(S{"C"}, nil)
				assertStates(t, m, S{"C"})
			},
		},
		{
			"using Add should work both ways",
			func(t *testing.T, m *Machine) {
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
			m.states["C"] = State{Remove: S{"D"}}
			m.states["D"] = State{Remove: S{"C"}}
			test.fn(t, m)
		})
	}
}

func TestAddRelation(t *testing.T) {
	m := NewNoRels(t, nil)
	defer m.Dispose()

	// relations
	m.states["A"] = State{Remove: S{"D"}}
	m.states["C"] = State{Add: S{"D"}}

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
	m.states["C"] = State{Require: S{"D"}}
	// run the test
	m.Set(S{"C", "D"}, nil)
	// assert
	assertStates(t, m, S{"C", "D"})
}

func TestRequireRelationWhenRequiredIsntActive(t *testing.T) {
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// relations
	m.states["C"] = State{Require: S{"D"}}
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

func (h *TestQueueHandlers) BEnter(e *Event) bool {
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
	err := m.BindHandlers(&TestQueueHandlers{})
	assert.NoError(t, err)
	// triggers Add(C) from BEnter
	m.Set(S{"B"}, nil)
	// assert
	assertEventCounts(t, history, 1)
	assertStates(t, m, S{"C", "B"})
}

// TestNegotiationCancel
type TestNegotiationCancelHandlers struct{}

func (h *TestNegotiationCancelHandlers) DEnter(_ *Event) bool {
	return false
}

func TestNegotiationCancel(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, m *Machine) Result
		log  *regexp.Regexp
	}{
		{
			"using set",
			func(t *testing.T, m *Machine) Result {
				// m = (A)
				// DEnter will cancel the transition
				return m.Set(S{"D"}, nil)
			},
			regexp.MustCompile(`\[cancel] \(D\) by DEnter`),
		},
		{
			"using add",
			func(t *testing.T, m *Machine) Result {
				// m = (A)
				// DEnter will cancel the transition
				return m.Add(S{"D"}, nil)
			},
			regexp.MustCompile(`\[cancel] \(D A\) by DEnter`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// init
			m := NewNoRels(t, S{"A"})
			defer m.Dispose()
			// bind handlers
			err := m.BindHandlers(&TestNegotiationCancelHandlers{})
			assert.NoError(t, err)
			// bind logger
			log := ""
			captureLog(t, m, &log)
			// run the test
			result := test.fn(t, m)
			// assert
			assert.Equal(t, Canceled, result, "transition should be canceled")
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
	m.states["B"] = State{
		Auto:    true,
		Require: S{"A"},
	}
	// bind logger
	log := ""
	captureLog(t, m, &log)
	// run the test
	result := m.Add(S{"A"}, nil)
	// assert
	assert.Equal(t, Executed, result, "transition should be executed")
	assertStates(t, m, S{"A", "B"}, "dependant auto state should be set")
	assert.Contains(t, log, "[auto] B", "log should mention the auto state")
}

// TestNegotiationRemove
type TestNegotiationRemoveHandlers struct{}

func (h *TestNegotiationRemoveHandlers) AExit(_ *Event) bool {
	return false
}

func TestNegotiationRemove(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	// bind handlers
	err := m.BindHandlers(&TestNegotiationRemoveHandlers{})
	assert.NoError(t, err)

	// bind logger
	log := ""
	captureLog(t, m, &log)
	// run the test
	// AExit will cancel the transition
	result := m.Remove(S{"A"}, nil)
	// assert
	assert.Equal(t, Canceled, result, "transition should be canceled")
	assertStates(t, m, S{"A"}, "state shouldnt be changed")
	assert.Regexp(t, `\[cancel] \(\) by AExit`, log,
		"log should explain the reason of cancellation")
}

// TestHandlerStateInfo
type TestHandlerStateInfoHandlers struct{}

func (h *TestHandlerStateInfoHandlers) DEnter(e *Event) {
	t := e.Args["t"].(*testing.T)
	assert.ElementsMatch(t, S{"A"}, e.Machine.ActiveStates(),
		"provide the previous states of the transition")
	assert.ElementsMatch(t, S{"D"}, e.Transition().TargetStates,
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
	err := m.BindHandlers(&TestHandlerStateInfoHandlers{})
	assert.NoError(t, err)

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

func (h *TestHandlerArgsHandlers) BEnter(e *Event) {
	t := e.Args["t"].(*testing.T)
	foo := e.Args["foo"].(string)
	assert.Equal(t, "bar", foo)
}

func (h *TestHandlerArgsHandlers) AExit(e *Event) {
	t := e.Args["t"].(*testing.T)
	foo := e.Args["foo"].(string)
	assert.Equal(t, "bar", foo)
}

func (h *TestHandlerArgsHandlers) CState(e *Event) {
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
	err := m.BindHandlers(&TestHandlerArgsHandlers{})
	assert.NoError(t, err)

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

func (h *TestSelfHandlersCancellableHandlers) AA(e *Event) bool {
	AACounter := e.Args["AACounter"].(*int)
	*AACounter++
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
	err := m.BindHandlers(&TestSelfHandlersCancellableHandlers{})
	assert.NoError(t, err)
	// run the test
	// handlers will assert
	AACounter := 0
	m.Set(S{"A", "B"}, A{"AACounter": &AACounter})
	// assert
	assert.Equal(t, 1, AACounter, "AA call count")
	assert.Equal(t, 0, history.Counter["AA"], "AA call count")
	assert.Equal(t, 0, history.Counter["AnyB"], "AnyB call count")
}

func TestSelfHandlersOrder(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A"})
	defer m.Dispose()
	m.SetLogLevel(LogEverything)

	// bind history
	events := []string{"AA", "AnyB", "BEnter"}
	history := trackTransitions(m, events)

	// run the test
	m.Set(S{"A", "B"}, nil)

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

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

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert
	assert.Equal(t, 1, history.Counter["AA"], "AA call count")
	assert.Equal(t, 0, history.Counter["BB"], "BB call count")
}

func TestRegressionRemoveCrossBlockedByImplied(t *testing.T) {
	// init
	m := NewCustomStates(t, Struct{
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
	m := NewCustomStates(t, Struct{
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

func (h *TestWhenHandlers) AState(e *Event) {
	go func() {
		time.Sleep(10 * time.Millisecond)
		e.Machine.Add(S{"B"}, nil)
		time.Sleep(10 * time.Millisecond)
		e.Machine.Add(S{"C"}, nil)
	}()
}

// TODO
func TestWhen(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	defer m.Dispose()

	// bind handlers
	err := m.BindHandlers(&TestWhenHandlers{})
	assert.NoError(t, err)

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

func (h *TestWhenNotHandlers) AState(e *Event) {
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
	err := m.BindHandlers(&TestWhenNotHandlers{})
	assert.NoError(t, err)

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
	ExceptionHandler
}

func (h *TestPartialNegotiationPanicHandlers) BEnter(_ *Event) {
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
	err := m.BindHandlers(&TestPartialNegotiationPanicHandlers{})
	assert.NoError(t, err)

	// run the test
	assert.Equal(t, Canceled, m.Add(S{"B"}, nil))
	// assert
	assertStates(t, m, S{"A", "Exception"})
	assert.Regexp(t, `\[cancel] \(B A\) by recover`, log,
		"log contains the target states and handler")
}

// TestPartialFinalPanic
type TestPartialFinalPanicHandlers struct {
	ExceptionHandler
}

func (h *TestPartialFinalPanicHandlers) BState(_ *Event) {
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
	err := m.BindHandlers(&TestPartialFinalPanicHandlers{})
	assert.NoError(t, err)

	// run the test
	m.Add(S{"A", "B", "C"}, nil)
	// assert
	assertStates(t, m, S{"A", "Exception"})
	assert.Contains(t, log, "[error:add] A B C (BState",
		"log contains the target states and handler")
	assert.Contains(t, log, "[error:trace] goroutine",
		"log contains the stack trace")
	assert.Regexp(t, `\[cancel] \(A B C\) by recover`, log,
		"log contains the target states and handler")
}

// TestStateCtx
type TestStateCtxHandlers struct {
	ExceptionHandler
	callbackCh chan bool
}

func (h *TestStateCtxHandlers) AState(e *Event) {
	t := e.Args["t"].(*testing.T)
	stepCh := e.Args["stepCh"].(chan bool)
	stateCtx := e.Machine.NewStateCtx("A")
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
	err := m.BindHandlers(handlers)
	assert.NoError(t, err)
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
	ExceptionHandler
	assertsCount int
}

func (h *TestQueueCheckableHandlers) AState(e *Event) {
	t := e.Args["t"].(*testing.T)
	e.Machine.Add(S{"B"}, nil)
	e.Machine.Add(S{"C"}, nil)
	e.Machine.Add(S{"D"}, nil)
	assert.Len(t, e.Machine.queue, 3, "queue should have 3 mutations scheduled")
	h.assertsCount++
	assert.Equal(t, 1,
		e.Machine.IsQueued(MutationAdd, S{"C"}, false, false, 0),
		"C should be queued")
	h.assertsCount++
	assert.Equal(t, -1,
		e.Machine.IsQueued(MutationAdd, S{"A"}, false, false, 0),
		"A should NOT be queued")
	h.assertsCount++
}

func TestQueueCheckable(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	defer m.Dispose()
	// bind handlers
	handlers := &TestQueueCheckableHandlers{}
	err := m.BindHandlers(handlers)
	assert.NoError(t, err)

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
	m.states["C"] = State{
		Auto:    true,
		Require: S{"B"},
	}
	m.states["D"] = State{
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

func TestTime(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	// relations
	m.states["B"] = State{Multi: true}

	// test 1
	// ()[]
	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"A", "B"})
	assertTimes(t, m, S{"A", "B", "C", "D"}, T{1, 1, 0, 0})

	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"A", "B"})
	assertTimes(t, m, S{"A", "B", "C", "D"}, T{1, 3, 0, 0})

	m.Add(S{"A", "B", "C"}, nil)
	assertStates(t, m, S{"A", "B", "C"})
	assertTimes(t, m, S{"A", "B", "C", "D"}, T{1, 5, 1, 0})

	m.Set(S{"D"}, nil)
	assertStates(t, m, S{"D"})
	assertTimes(t, m, S{"A", "B", "C", "D"}, T{2, 6, 2, 1})

	m.Add(S{"D", "C"}, nil)
	assertStates(t, m, S{"D", "C"})
	assertTimes(t, m, S{"A", "B", "C", "D"}, T{2, 6, 3, 1})

	m.Remove(S{"B", "C"}, nil)
	assertStates(t, m, S{"D"})
	assertTimes(t, m, S{"A", "B", "C", "D"}, T{2, 6, 4, 1})

	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"D", "A", "B"})
	assertTimes(t, m, S{"A", "B", "C", "D"}, T{3, 7, 4, 1})

	// test 2
	order := S{"A", "B", "C", "D"}
	before := m.Time(order)
	m.Add(S{"C"}, nil)
	now := m.Time(order)

	// assert
	assertStates(t, m, S{"A", "B", "D", "C"})
	assertTimes(t, m, S{"A", "B", "C", "D"}, T{3, 7, 5, 1})
	assert.True(t, IsTimeAfter(now, before))
	assert.False(t, IsTimeAfter(before, now))
}

func TestWhenCtx(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()

	// wait on 2 Whens with a step context
	ctx, cancel := context.WithCancel(context.Background())
	whenTimeCh := m.WhenTime(S{"A", "B"}, T{3, 3}, ctx)
	whenArgsCh := m.WhenArgs("B", A{"foo": "bar"}, ctx)
	whenCh := m.When1("C", ctx)

	// assert
	assert.Greater(t, len(m.indexWhenTime), 0)
	assert.Greater(t, len(m.indexWhen), 0)
	assert.Greater(t, len(m.indexWhenArgs), 0)

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	select {
	case <-whenCh:
	case <-whenArgsCh:
	case <-whenTimeCh:
	case <-ctx.Done():
	}

	// wait for the context to be canceled and cleanups happen
	time.Sleep(time.Millisecond)

	// assert
	assert.Equal(t, len(m.indexWhenTime), 0)
	assert.Equal(t, len(m.indexWhen), 0)
	assert.Equal(t, len(m.indexWhenArgs), 0)
}

func TestWhenArgs(t *testing.T) {
	// init
	m := NewRels(t, nil)
	defer m.Dispose()

	// bind
	whenCh := m.WhenArgs("B", A{"foo": "bar"}, nil)

	// incorrect args
	m.Add1("B", A{"foo": "foo"})
	select {
	case <-whenCh:
		t.Fatal("when shouldnt be resolved")
	default:
		// pass
	}

	// correct args
	m.Add1("B", A{"foo": "bar"})
	select {
	default:
		t.Fatal("when should be resolved")
	case <-whenCh:
		// pass
	}
}

func TestWhenTime(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()

	// bind
	// (A:1 B:1)
	whenCh := m.WhenTime(S{"A", "B"}, T{5, 2}, nil)

	// tick some, but not enough
	m.Remove(S{"A", "B"}, nil)
	m.Add(S{"A", "B"}, nil)
	// (A:3 B:3) not yet
	select {
	case <-whenCh:
		t.Fatal("when shouldnt be resolved")
	default:
		// pass
	}

	m.Remove1("A", nil)
	m.Add1("A", nil)
	// (A:5 B:3) OK

	select {
	default:
		t.Fatal("when should be resolved")
	case <-whenCh:
		// pass
	}
}

func TestNewCommon(t *testing.T) {
	// TODO TestConfig
	t.Skip()
}

func TestTracers(t *testing.T) {
	// TODO TestConfig
	t.Skip()
}

func TestQueueLimit(t *testing.T) {
	// TODO TestConfig
	t.Skip()
}

func TestSubmachines(t *testing.T) {
	// TODO TestSubmachines
	t.Skip()
}

func TestEval(t *testing.T) {
	// TODO TestSubmachines
	t.Skip()
}

func TestSetStates(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A", "C"})
	defer m.Dispose()

	// add relations and states
	s := m.GetStruct()
	s["A"] = State{Multi: true}
	s["B"] = State{Remove: S{"C"}}
	s["D"] = State{Add: S{"E"}}
	s["E"] = State{}

	// update states
	err := m.SetStruct(s, S{"A", "B", "C", "D", "E"})
	if err != nil {
		t.Fatal(err)
	}

	// test
	m.Set(S{"A", "B", "D"}, nil)

	// assert
	assert.ElementsMatch(t, S{"A", "B", "D", "E"}, m.activeStates)
}

func TestIs(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()

	// test
	assert.True(t, m.Is(S{"A", "B"}), "A B should be active")
	assert.False(t, m.Is(S{"A", "B", "C"}), "A B C shouldnt be active")
}

func TestNot(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()

	// test
	assert.False(t, m.Not(S{"A", "B"}), "A B should be active")
	assert.False(t, m.Not(S{"A", "B", "C"}), "A B C is partially active")
	assert.True(t, m.Not1("D"), "D is inactive")
}

func TestAny(t *testing.T) {
	// init
	m := NewNoRels(t, S{"A", "B"})
	defer m.Dispose()

	// test
	assert.True(t, m.Any(S{"A", "B"}, S{"C"}), "A B should be active")
	assert.True(t, m.Any(S{"A", "B", "C"}, S{"A"}), "A B C is partially active")
}

func TestIsClock(t *testing.T) {
	// init
	m := NewNoRels(t, nil)
	// relations
	m.states["B"] = State{Multi: true}

	// test 1
	// ()[]
	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"A", "B"})
	assertClocks(t, m, S{"A", "B", "C", "D"}, T{1, 1, 0, 0})

	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"A", "B"})
	assertClocks(t, m, S{"A", "B", "C", "D"}, T{1, 3, 0, 0})

	m.Add(S{"A", "B", "C"}, nil)
	assertStates(t, m, S{"A", "B", "C"})
	assertClocks(t, m, S{"A", "B", "C", "D"}, T{1, 5, 1, 0})

	m.Set(S{"D"}, nil)
	assertStates(t, m, S{"D"})
	assertClocks(t, m, S{"A", "B", "C", "D"}, T{2, 6, 2, 1})

	m.Add(S{"D", "C"}, nil)
	assertStates(t, m, S{"D", "C"})
	assertClocks(t, m, S{"A", "B", "C", "D"}, T{2, 6, 3, 1})

	m.Remove(S{"B", "C"}, nil)
	assertStates(t, m, S{"D"})
	assertClocks(t, m, S{"A", "B", "C", "D"}, T{2, 6, 4, 1})

	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"D", "A", "B"})
	assertClocks(t, m, S{"A", "B", "C", "D"}, T{3, 7, 4, 1})
}

func TestInspect(t *testing.T) {
	m := NewRels(t, S{"A", "C"})
	// (A:1 C:1)[B:0 D:0 Exception:0]
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
	// (A:1 C:1)[B:0 D:0 Exception:0]
	m.Remove(S{"C"}, nil)
	// ()[A:2 B:0 C:2 D:0 Exception:0]
	m.Add(S{"B"}, nil)
	// (A:3 B:1 C:3)[D:0 Exception:0]
	m.Add(S{"D"}, nil)
	// (A:3 B:1 C:3 D:1)[Exception:0]
	expected = `
		A:
		  State:   true 3
		  Auto:    true
		  Require: C
		
		B:
		  State:   true 1
		  Multi:   true
		  Add:     C
		
		C:
		  State:   true 3
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
