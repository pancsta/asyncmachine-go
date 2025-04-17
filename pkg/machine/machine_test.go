package machine

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/exp/maps"
)

func init() {
	// debug
	os.Setenv(EnvAmDebug, "1")
}

func ExampleNew() {
	ctx := context.TODO()
	mach := New(ctx, Schema{
		"Foo": {Require: S{"Bar"}},
		"Bar": {},
	}, nil)
	mach.Add1("Foo", nil)
	mach.Is1("Foo") // false
}

func ExampleNewCommon() {
	// define (tip: use am-gen instead)
	stateStruct := Schema{
		"Foo": {Require: S{"Bar"}},
		"Bar": {},
	}
	stateNames := []string{"Foo", "Bar"}
	type Handlers struct{}
	// func (h *Handlers) FooState(e *Event) {
	// 	args := e.Args
	// 	mach := e.Machine()
	// 	ctx := mach.NewStateCtx("Foo")
	// 	// unblock
	// 	go func() {
	// 		if ctx.Err() != nil {
	// 			return // expired
	// 		}
	// 		// blocking calls...
	// 		if ctx.Err() != nil {
	// 			return // expired
	// 		}
	// 		mach.Add1("Bar", nil)
	// 	}()
	// }

	// init
	ctx := context.TODO()
	mach, err := NewCommon(ctx, "mach-id",
		stateStruct, stateNames,
		&Handlers{}, nil, nil)
	if err != nil {
		panic(err)
	}
	mach.SetLogLevel(LogOps)

	// debug
	// import amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	// amhelp.EnableDebugging(false)
	// amhelp.MachDebugEnv(mach)
}

// NewNoRels creates a new machine with no relations between states.
func NewNoRels(t *testing.T, initialState S) *Machine {
	m := New(context.Background(), Schema{
		"A": {},
		"B": {},
		"C": {},
		"D": {},
	}, nil)

	m.SetLogger(func(i LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	if os.Getenv(EnvAmDebug) != "" && os.Getenv("AM_TEST_RUNNER") == "" {
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
	m := New(context.Background(), Schema{
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
	if os.Getenv(EnvAmDebug) != "" && os.Getenv("AM_TEST_RUNNER") == "" {
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
	if os.Getenv(EnvAmDebug) != "" && os.Getenv("AM_TEST_RUNNER") == "" {
		m.SetLogLevel(LogEverything)
		m.HandlerTimeout = 2 * time.Minute
	}
	return m
}

func TestSingleStateActive(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A"})
	assertStates(t, m, S{"A"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestMultipleStatesActive(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A"})
	m.Add(S{"B"}, nil)
	assertStates(t, m, S{"A", "B"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestExposeAllStateNames(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A"})
	assert.ElementsMatch(t, S{"A", "B", "C", "D", "Exception"}, m.StateNames())

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestStateSet(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A"})
	m.Set(S{"B"}, nil)
	assertStates(t, m, S{"B"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestStateAdd(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A"})
	m.Add(S{"B"}, nil)
	assertStates(t, m, S{"A", "B"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestStateRemove(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"B", "C"})
	m.Remove(S{"C"}, nil)
	assertStates(t, m, S{"B"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestPanicWhenStateIsUnknown(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A"})
	assert.Panics(t, func() {
		m.Set(S{"E"}, nil)
	})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestGetStateRelations(t *testing.T) {
	t.Parallel()
	m := New(context.Background(), Schema{
		"A": {
			Add:     S{"B"},
			Require: S{"B"},
			Auto:    true,
		},
		"B": {},
	}, nil)

	of, err := m.resolver.GetRelationsOf("A")

	assert.NoErrorf(t, err, "all states are known")
	assert.Nil(t, err)
	assert.Equal(t, []Relation{RelationAdd, RelationRequire},
		of)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestGetRelationsBetweenStates(t *testing.T) {
	t.Parallel()
	m := New(context.Background(), Schema{
		"A": {
			Add:     S{"B"},
			Require: S{"C"},
			Auto:    true,
		},
		"B": {},
		"C": {},
	}, nil)

	between, err := m.resolver.GetRelationsBetween("A", "B")

	assert.NoError(t, err)
	assert.Equal(t, []Relation{RelationAdd},
		between)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestSingleToSingleStateTransition(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A", "B"})

	// expectations
	events := []string{
		"AExit", "BExit", "CEnter", "AC", "BC", "CState",
	}
	history := trackTransitions(m)

	// transition
	m.Set(S{"C"}, nil)

	// assert the final state
	assertStates(t, m, S{"C"})

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert the order
	assertOrder(t, history, events)
	// assert event counts
	assertEventCounts(t, history, 1)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestSingleToMultiStateTransition(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A"})
	events := []string{
		"AExit", "BEnter", "CEnter", "AB", "AC", "BState", "CState",
	}
	history := trackTransitions(m)
	// transition
	m.Set(S{"B", "C"}, nil)
	// assert the final state
	assertStates(t, m, S{"B", "C"})

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert the order
	assertOrder(t, history, events)
	// assert event counts
	assertEventCounts(t, history, 1)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestMultiToMultiStateTransition(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A", "B"})
	events := []string{
		"AExit", "BExit", "DEnter", "CEnter", "AD", "AC", "BD", "BC", "CState",
	}
	history := trackTransitions(m)
	// transition
	m.Set(S{"D", "C"}, nil)
	// assert the final state
	assert.ElementsMatch(t, S{"D", "C"}, m.activeStates)

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert the order
	assertOrder(t, history, events)
	// assert event counts
	assertEventCounts(t, history, 1)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestMultiToSingleStateTransition(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A", "B"})
	events := []string{
		"AExit", "BExit", "CEnter", "AC", "BC", "AnyEnter", "AEnd", "BEnd",
		"CState", "AnyState",
	}
	history := trackTransitions(m)
	// transition
	m.Set(S{"C"}, nil)
	// assert the final state
	assertStates(t, m, S{"C"})

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert the order
	assertOrder(t, history, events)
	// assert event counts
	assertEventCounts(t, history, 1)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestTransitionToActiveState(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A", "B"})

	// history
	history := trackTransitions(m)

	// test
	m.Set(S{"A"}, nil)

	// assert the final state
	assertStates(t, m, S{"A"})
	// assert event counts (none should happen)
	assert.Equal(t, 0, history.Counter["AExit"])
	assert.Equal(t, 0, history.Counter["AnyA"])
	assert.Equal(t, 0, history.Counter["AnyA"])

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestAfterRelationWhenEntering(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A", "B"})

	// relations
	m.states["C"] = State{After: S{"D"}}
	m.states["A"] = State{After: S{"B"}}

	// history
	events := []string{"DEnter", "CEnter", "AD", "AC"}
	history := trackTransitions(m)

	// test
	m.Set(S{"C", "D"}, nil)

	// assert the final state
	assertStates(t, m, S{"C", "D"})
	assertOrder(t, history, events)
	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, 1, count)
	}

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestAfterRelationWhenExiting(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A", "B"})

	// relations
	m.states["C"] = State{After: S{"D"}}
	m.states["A"] = State{After: S{"B"}}

	// history
	events := []string{"BExit", "AExit", "AD", "AC", "BD", "BC"}
	history := trackTransitions(m)

	// test
	m.Set(S{"C", "D"}, nil)

	// assert the final state
	assertStates(t, m, S{"C", "D"})
	assertOrder(t, history, events)
	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, 1, count)
	}

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestRequireOrder(t *testing.T) {
	t.Parallel()
	m := NewCustomStates(t, Schema{
		"A": {
			Auto:    true,
			Require: S{"B", "C"},
		},
		"B": {
			Auto:    true,
			Require: S{"D"},
		},
		"C": {
			Auto: true,
			// After: S{"D", "B"},
		},
		"D":     {Auto: true},
		"Start": {},
	})

	// history
	history := trackTransitions(m)
	t.Logf("Order: %s", m.StateNames())

	// test
	m.Add1("Start", nil)

	// assert the final state
	assertOrder(t, history, []string{"DState", "BState", "AState"})
	assertOrder(t, history, []string{"CState", "AState"})
	assertStates(t, m, S{"A", "B", "C", "D", "Start"})
	// assert event counts
	for s, count := range history.Counter {
		if strings.HasPrefix(s, Any) {
			// global handelrs run for auto transitions
			assert.Equal(t, 2, count)
		} else {
			assert.Equal(t, 1, count)
		}
	}

	// test the resolver directly
	res := m.Resolver().(*DefaultRelationsResolver)
	res.Transition = &Transition{}

	order := S{"A", "B", "C", "D"}
	res.SortStates(order)
	assert.Equal(t, S{"D", "B", "C", "A"}, order)

	order = S{"C", "D", "A", "B"}
	res.SortStates(order)
	assert.Equal(t, S{"D", "B", "C", "A"}, order)

	order = S{"D", "A", "C", "B"}
	res.SortStates(order)
	assert.Equal(t, S{"D", "B", "C", "A"}, order)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestRemoveRelation(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"D"})

	// relations
	m.states["C"] = State{Remove: S{"D"}}

	// C deactivates D
	m.Add(S{"C"}, nil)
	assertStates(t, m, S{"C"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestRemoveRelationSimultaneous(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"D"})

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

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestRemoveRelationCrossBlocking(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fn   func(t *testing.T, m *Machine)
	}{
		{
			"using Set should de-activate the old one",
			func(t *testing.T, m *Machine) {
				// (D:1)[A:0 B:0 C:0]
				m.Set(S{"C"}, nil)
				assertStates(t, m, S{"C"})
			},
		},
		{
			"using Set should work both ways",
			func(t *testing.T, m *Machine) {
				// (D:1)[A:0 B:0 C:0]
				m.Set(S{"C"}, nil)
				assertStates(t, m, S{"C"})
				m.Set(S{"D"}, nil)
				assertStates(t, m, S{"D"})
			},
		},
		{
			"using Add should de-activate the old one",
			func(t *testing.T, m *Machine) {
				// (D:1)[A:0 B:0 C:0]
				m.Add(S{"C"}, nil)
				assertStates(t, m, S{"C"})
			},
		},
		{
			"using Add should work both ways",
			func(t *testing.T, m *Machine) {
				// (D:1)[A:0 B:0 C:0]
				m.Add(S{"C"}, nil)
				assertStates(t, m, S{"C"})
				m.Add(S{"D"}, nil)
				assertStates(t, m, S{"D"})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := NewNoRels(t, S{"D"})
			// C and D are cross blocking each other via Remove
			m.states["C"] = State{Remove: S{"D"}}
			m.states["D"] = State{Remove: S{"C"}}
			test.fn(t, m)
		})
	}
}

func TestAddRelation(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, nil)

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

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestRequireRelation(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// relations
	m.states["C"] = State{Require: S{"D"}}

	// test
	m.Set(S{"C", "D"}, nil)

	// assert
	assertStates(t, m, S{"C", "D"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestRequireRelationWhenRequiredIsntActive(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A"})

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

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestRemoveMultiImplied(t *testing.T) {
	t.Parallel()
	m := NewNoRels(t, S{"A", "B", "C"})

	// relations
	m.states["A"] = State{
		Multi:   true,
		Add:     S{"B"},
		Require: S{"C"},
	}
	m.states["B"] = State{Require: S{"C"}}

	// remove implied state
	m.Remove1("B", nil)
	assertStates(t, m, S{"A", "C"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestQueue
type TestQueueHandlers struct{}

func (h *TestQueueHandlers) BEnter(e *Event) bool {
	e.Machine().Add(S{"C"}, nil)
	return true
}

func TestQueue(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// history
	history := trackTransitions(m)

	// handlers
	err := m.BindHandlers(&TestQueueHandlers{})
	assert.NoError(t, err)

	// triggers Add(C) from BEnter
	m.Set(S{"B"}, nil)

	// assert event counts
	for s, count := range history.Counter {
		if strings.HasPrefix(s, Any) {
			// global handelrs run for auto transitions
			assert.Equal(t, 2, count)
		} else {
			assert.Equal(t, 1, count)
		}
	}
	assertStates(t, m, S{"C", "B"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestNegotiationCancel
type TestNegotiationCancelHandlers struct{}

func (h *TestNegotiationCancelHandlers) DEnter(_ *Event) bool {
	return false
}

func TestNegotiationCancel(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			// init
			m := NewNoRels(t, S{"A"})

			// bind handlers
			err := m.BindHandlers(&TestNegotiationCancelHandlers{})
			assert.NoError(t, err)

			// bind logger
			log := ""
			captureLog(t, m, &log)

			// test
			result := test.fn(t, m)

			// assert
			assert.Equal(t, Canceled, result, "transition should be canceled")
			assertStates(t, m, S{"A"}, "state shouldnt be changed")
			assert.Regexp(t, test.log, log,
				"log should explain the reason of cancelation")

			// dispose
			m.Dispose()
			<-m.WhenDisposed()
		})
	}
}

func TestAutoStates(t *testing.T) {
	t.Parallel()

	// init
	m := NewNoRels(t, nil)

	// relations
	m.states["B"] = State{
		Auto:    true,
		Require: S{"A"},
	}

	// bind logger
	log := ""
	captureLog(t, m, &log)

	// test
	result := m.Add(S{"A"}, nil)

	// assert
	assert.Equal(t, Executed, result, "transition should be executed")
	assertStates(t, m, S{"A", "B"}, "dependant auto state should be set")
	assert.Contains(t, log, "[auto] B", "log should mention the auto state")
}

func TestPartialAutoStates(t *testing.T) {
	t.Parallel()

	// init
	m := NewNoRels(t, nil)

	// relations
	m.states["A"] = State{
		Auto: true,
	}
	m.states["B"] = State{
		Auto:    true,
		After:   S{"A"},
		Require: S{"C"},
	}

	// bind logger
	log := ""
	captureLog(t, m, &log)

	// test
	result := m.Add(S{"D"}, nil)

	// assert
	assert.Equal(t, Executed, result, "transition should be executed")
	assertStates(t, m, S{"A", "D"}, "dependant auto state should be set")
	assert.Contains(t, log, "[state:auto] +A",
		"log should mention the auto state")
}

func TestPartialAutoStatesByStateState(t *testing.T) {
	t.Parallel()

	// init
	m := NewCustomStates(t, Schema{
		"A": {Auto: true},
		"B": {Auto: true},
		"C": {},
	})

	// CB cancels
	_ = m.BindHandlers(&struct {
		CB HandlerNegotiation
	}{
		CB: func(e *Event) bool { return false },
	})

	// trigger auto
	m.Add1("C", nil)
	assertStates(t, m, S{"C", "A"}, "only A auto state should be active")
}

func TestPartialAutoStatesByEnter(t *testing.T) {
	t.Parallel()

	// init
	m := NewCustomStates(t, Schema{
		"A": {Auto: true},
		"B": {Auto: true},
		"C": {},
	})

	// BEnter cancels
	_ = m.BindHandlers(&struct {
		BEnter HandlerNegotiation
	}{
		BEnter: func(e *Event) bool { return false },
	})

	// trigger auto
	m.Add1("C", nil)
	assertStates(t, m, S{"C", "A"}, "only A auto state should be active")
}

// TestNegotiationRemove
type TestNegotiationRemoveHandlers struct{}

func (h *TestNegotiationRemoveHandlers) AExit(_ *Event) bool {
	return false
}

func TestNegotiationRemove(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// bind handlers
	err := m.BindHandlers(&TestNegotiationRemoveHandlers{})
	assert.NoError(t, err)

	// bind logger
	log := ""
	captureLog(t, m, &log)

	// test
	// AExit will cancel the transition
	result := m.Remove(S{"A"}, nil)

	// assert
	assert.Equal(t, Canceled, result, "transition should be canceled")
	assertStates(t, m, S{"A"}, "state shouldnt be changed")
	assert.Regexp(t, `\[cancel] \(\) by AExit`, log,
		"log should explain the reason of cancelation")

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestHandlerStateInfo
type TestHandlerStateInfoHandlers struct{}

func (h *TestHandlerStateInfoHandlers) DEnter(e *Event) {
	t := e.Args["t"].(*testing.T)
	assert.ElementsMatch(t, S{"A"}, e.Machine().ActiveStates(),
		"provide the previous states of the transition")
	assert.ElementsMatch(t, S{"D"}, e.Transition().TargetStates(),
		"provide the target states of the transition")
	assert.True(t, e.Mutation().StateWasCalled("D"),
		"provide the called states of the transition")
	txStrExp := "D (requested)\nD (set)\nA (remove)\nA (handler)"
	txStr := e.Transition().String()
	assert.Equal(t, txStrExp, txStr,
		"provide a string version of the transition")
}

func TestHandlerStateInfo(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})
	err := m.VerifyStates(S{"A", "B", "C", "D", "Exception"})
	if err != nil {
		assert.NoError(t, err)
	}

	// bind history
	events := []string{"DEnter"}
	history := trackTransitions(m)

	// bind handlers
	err = m.BindHandlers(&TestHandlerStateInfoHandlers{})
	assert.NoError(t, err)

	// test
	// DEnter will assert
	m.Set(S{"D"}, A{"t": t})

	// assert
	assertOrder(t, history, events)
	assert.Equal(t, 1, history.Counter["DEnter"])

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestGetters(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})
	mapper := NewArgsMapper([]string{"arg", "arg2"}, 5)
	m.SetLogArgs(mapper)
	m.SetLogLevel(LogEverything)

	// assert
	assert.Equal(t, LogEverything, m.GetLogLevel())
	assert.Equal(t, 0, len(m.Queue()))

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestSwitch(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	caseA := false
	switch m.Switch(S{"A", "B"}) {
	case "A":
		caseA = true
	case "B":
	}
	assert.Equal(t, true, caseA)

	caseDef := false
	switch m.Switch(S{"C", "B"}) {
	case "B":
	default:
		caseDef = true
	}
	assert.Equal(t, true, caseDef)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestDispose(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})
	ran := false
	m.HandleDispose(func(id string, ctx context.Context) {
		ran = true
	})

	// test
	m.Dispose()
	<-m.WhenDisposed()
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, true, ran)
}

func TestDisposeForce(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})
	ran := false
	m.HandleDispose(func(id string, ctx context.Context) {
		ran = true
	})

	// test
	m.DisposeForce()
	assert.Equal(t, true, ran)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestLogArgs(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})
	mapper := NewArgsMapper([]string{"arg", "arg2"}, 5)
	m.SetLogArgs(mapper)

	// bind logger
	log := ""
	captureLog(t, m, &log)

	// run test
	args := A{"arg": "foofoofoo"}
	m.Add1("D", args)

	// assert
	assert.Contains(t, log, "(arg=fo...)", "Arg should be in the log")

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
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
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// bind history
	history := trackTransitions(m)

	// bind handlers
	err := m.BindHandlers(&TestHandlerArgsHandlers{})
	assert.NoError(t, err)

	// test
	// handlers will assert
	m.Add(S{"B"}, A{"t": t, "foo": "bar"})
	m.Remove(S{"A"}, A{"t": t, "foo": "bar"})
	m.Set(S{"C"}, A{"t": t, "foo": "bar"})

	// assert
	assertEventCountsMin(t, history, 1)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestEvMutations
type TestEvH struct{}

func (h *TestEvH) AState(e *Event) {
	e.Machine().EvAdd1(e, "B", e.Args)
	e.Machine().EvRemove1(e, "A", nil)
}

func (h *TestEvH) BState(e *Event) {
	t := e.Args["t"].(*testing.T)
	assert.Equal(t, e.MachineId, e.Mutation().Source.MachId)
}

func TestEvMutations(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// bind history
	history := trackTransitions(m)

	// bind handlers
	err := m.BindHandlers(&TestEvH{})
	assert.NoError(t, err)

	// test
	// handlers will assert
	m.Add1("A", A{"t": t})
	// m.When1("B", nil)

	// assert
	assertEventCountsMin(t, history, 1)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestEval(t *testing.T) {
	// init

	t.Setenv(EnvAmDetectEval, "1")
	m := NewNoRels(t, nil)
	_ = m.BindHandlers(&struct {
		AB HandlerNegotiation
		BA HandlerNegotiation
	}{
		AB: func(e *Event) bool {
			return true
		},
		BA: func(e *Event) bool {
			return true
		},
	})
	m.detectEval = true

	// test
	a := 0
	m.Eval(t.Name(), func() {
		a = 1
	}, nil)

	// assert
	assert.Equal(t, 1, a)

	// TOOD test timeout, mach ctx, eval ctx

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestBreakpoints(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// test
	m.LogStackTrace = true
	m.AddBreakpoint(S{"A"}, S{"B"})
	m.Add1("A", nil)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestHandlerStateState(t *testing.T) {
	t.Parallel()

	// spies
	ab := false
	ba := false

	// init
	m := NewNoRels(t, S{"A"})
	_ = m.BindHandlers(&struct {
		AB HandlerNegotiation
		BA HandlerNegotiation
	}{
		AB: func(e *Event) bool {
			ab = true
			return true
		},
		BA: func(e *Event) bool {
			ba = true
			return true
		},
	})

	// test
	m.Add1("B", nil)

	// assert
	assert.True(t, ab)
	assert.False(t, ba)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestHandlerNegotiationStateState(t *testing.T) {
	t.Parallel()

	// init
	m := NewNoRels(t, S{"A"})
	_ = m.BindHandlers(&struct {
		AB HandlerNegotiation
		BA HandlerNegotiation
	}{
		AB: func(e *Event) bool {
			return false
		},
		BA: func(e *Event) bool {
			return true
		},
	})

	// test
	res := m.Add1("B", nil)

	// assert
	assert.Equal(t, Canceled, res)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestAnonHandlers(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})
	ok := false

	// bind handlers
	err := m.BindHandlers(&struct {
		AExit func(e *Event) bool
		AEnd  func(e *Event)
	}{
		AExit: func(e *Event) bool {
			return true
		},
		AEnd: func(e *Event) {
			ok = true
		},
	})
	assert.NoError(t, err)

	// test
	m.Remove(S{"A"}, nil)

	// assert
	assert.True(t, m.Not1("A"))
	assert.True(t, ok)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestSelfHandlersCancellable
type TestSelfHandlersCancellableHandlers struct{}

func (h *TestSelfHandlersCancellableHandlers) AA(e *Event) bool {
	AACounter := e.Args["AACounter"].(*int)
	*AACounter++
	return false
}

func TestSelfHandlersCancellable(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// bind history
	history := trackTransitions(m)

	// bind handlers
	err := m.BindHandlers(&TestSelfHandlersCancellableHandlers{})
	assert.NoError(t, err)

	// test
	// handlers will assert
	AACounter := 0
	m.Set(S{"A", "B"}, A{"AACounter": &AACounter})

	// assert
	counter := history.Counter
	assert.Equal(t, 1, AACounter, "AA call count")
	assert.Equal(t, 1, counter["AA"], "History handler called")
	assert.Equal(t, 0, counter["AnyB"], "History handler NOT called")

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestSelfHandlersOrder(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// bind history
	events := []string{"BEnter", "AA", "AB", "AnyEnter", "BState", "AnyState"}
	history := trackTransitions(m)

	// test
	m.Set(S{"A", "B"}, nil)

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert
	assertOrder(t, history, events)
	assert.Equal(t, events, history.Order)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestDoubleHandlers(t *testing.T) {
	t.Parallel()
	// TODO bind 2 handlers and check for dups
	t.Skip("TODO")
}

func TestSelfHandlersForCalledOnly(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// bind history
	history := trackTransitions(m)

	// test
	m.Add(S{"B"}, nil)
	m.Add(S{"A"}, nil)

	// wait for history to drain the channel
	time.Sleep(10 * time.Millisecond)

	// assert
	counter := history.Counter
	assert.Equal(t, 1, counter["AA"], "AA call count")
	assert.Equal(t, 0, counter["BB"], "BB call count")

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestRegressionRemoveCrossBlockedByImplied(t *testing.T) {
	t.Parallel()
	// init
	m := NewCustomStates(t, Schema{
		"A": {Remove: S{"B"}},
		"B": {Remove: S{"A"}},
		"Z": {Add: S{"B"}},
	})

	// test
	m.Set(S{"Z"}, nil)

	// assert
	assertStates(t, m, S{"Z", "B"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestRegressionImpliedBlockByBeingRemoved(t *testing.T) {
	t.Parallel()
	// init
	m := NewCustomStates(t, Schema{
		"Wet":   {Require: S{"Water"}},
		"Dry":   {Remove: S{"Wet"}},
		"Water": {Add: S{"Wet"}, Remove: S{"Dry"}},
	})

	// test
	m.Set(S{"Dry"}, nil)
	m.Set(S{"Water"}, nil)

	// assert
	assertStates(t, m, S{"Water", "Wet"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestWhen
type TestWhenHandlers struct{}

func (h *TestWhenHandlers) AState(e *Event) {
	go func() {
		time.Sleep(10 * time.Millisecond)
		e.Machine().Add(S{"B"}, nil)
		time.Sleep(10 * time.Millisecond)
		e.Machine().Add(S{"C"}, nil)
	}()
}

func TestWhen(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// bind handlers
	err := m.BindHandlers(&TestWhenHandlers{})
	assert.NoError(t, err)

	// test
	m.Set(S{"A"}, nil)
	<-m.When(S{"B", "C"}, nil)

	// assert
	assertStates(t, m, S{"A", "B", "C"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestWhen2(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// test

	ready1 := make(chan struct{})
	pass1 := make(chan struct{})
	go func() {
		close(ready1)
		<-m.When(S{"A", "B"}, nil)
		close(pass1)
	}()

	ready2 := make(chan struct{})
	pass2 := make(chan struct{})
	go func() {
		close(ready2)
		<-m.When(S{"A", "B"}, nil)
		close(pass2)
	}()

	<-ready1
	<-ready2

	m.Add(S{"A", "B"}, nil)

	<-pass1
	<-pass2

	m.Dispose()
	<-m.WhenDisposed()
}

func TestWhenActive(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// test
	<-m.When(S{"A"}, nil)

	// assert
	assertStates(t, m, S{"A"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestWhenDups(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// test
	_ = m.When(S{"A"}, nil)
	_ = m.When(S{"A", "B"}, nil)
	_ = m.When(S{"A", "C"}, nil)

	m.Set(S{"A"}, nil)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestWhenNot
type TestWhenNotHandlers struct{}

func (h *TestWhenNotHandlers) AState(e *Event) {
	go func() {
		time.Sleep(10 * time.Millisecond)
		e.Machine().Remove1("B", nil)
		time.Sleep(10 * time.Millisecond)
		e.Machine().Remove1("C", nil)
	}()
}

func TestWhenNot(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"B", "C"})

	// bind handlers
	err := m.BindHandlers(&TestWhenNotHandlers{})
	assert.NoError(t, err)

	// test
	m.Add(S{"A"}, nil)
	<-m.WhenNot(S{"B", "C"}, nil)
	<-m.WhenNot1("B", nil)

	// assert
	assertStates(t, m, S{"A"})
	assertNoException(t, m)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestWhenNot2(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A", "B"})

	// test

	ready1 := make(chan struct{})
	pass1 := make(chan struct{})
	go func() {
		close(ready1)
		<-m.WhenNot(S{"A", "B"}, nil)
		close(pass1)
	}()

	ready2 := make(chan struct{})
	pass2 := make(chan struct{})
	go func() {
		close(ready2)
		<-m.WhenNot(S{"A", "B"}, nil)
		close(pass2)
	}()

	<-ready1
	<-ready2

	m.Remove(S{"A", "B"}, nil)

	<-pass1
	<-pass2

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestWhenNotActive(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// test
	<-m.WhenNot(S{"B"}, nil)

	// assert
	assertStates(t, m, S{"A"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestPartialNegotiationPanic
type TestPartialNegotiationPanicHandlers struct {
	*ExceptionHandler
}

func (h *TestPartialNegotiationPanicHandlers) BEnter(_ *Event) {
	panic("BEnter panic")
}

func TestPartialNegotiationPanic(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A"})

	// logger
	log := ""
	captureLog(t, m, &log)

	// bind handlers
	err := m.BindHandlers(&TestPartialNegotiationPanicHandlers{})
	assert.NoError(t, err)

	// test
	assert.Equal(t, Canceled, m.Add(S{"B"}, nil))
	// assert
	assertStates(t, m, S{"A", "Exception"})
	assert.Regexp(t, `\[cancel] \(B A\) by recover`, log,
		"log contains the target states and handler")

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestPartialFinalPanic
type TestPartialFinalPanicHandlers struct {
	*ExceptionHandler
}

func (h *TestPartialFinalPanicHandlers) BState(_ *Event) {
	panic("BState panic")
}

func TestPartialFinalPanic(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// logger
	log := ""
	captureLog(t, m, &log)

	// bind handlers
	err := m.BindHandlers(&TestPartialFinalPanicHandlers{})
	assert.NoError(t, err)

	// test
	m.Add(S{"A", "B", "C"}, nil)

	// assert
	assertStates(t, m, S{"A", "Exception"})
	assert.Contains(t, log, "[error:add] A B C (BState",
		"log contains the target states and handler")
	assert.Contains(t, log,
		"pkg/machine.(*TestPartialFinalPanicHandlers)",
		"log contains the stack trace")
	assert.Regexp(t, `\[cancel] \(A B C\) by recover`, log,
		"log contains the target states and handler")

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestStateCtx
type TestStateCtxHandlers struct {
	*ExceptionHandler
	callbackCh chan struct{}
}

func (h *TestStateCtxHandlers) AState(e *Event) {
	mach := e.Machine()
	t := e.Args["t"].(*testing.T)
	stepCh := e.Args["stepCh"].(chan bool)
	stateCtx := mach.NewStateCtx("A")

	h.callbackCh = make(chan struct{})
	go func() {
		<-stepCh
		assertStates(t, mach, S{})
		assert.Error(t, stateCtx.Err(), "state context should be canceled")
		close(h.callbackCh)
	}()

	index := mach.StateNames()
	assert.Equal(t, "A", e.step.GetFromState(index))
	assert.Equal(t, "A", e.step.GetToState(index))
}

func TestStateCtx(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// bind handlers
	handlers := &TestStateCtxHandlers{}
	err := m.BindHandlers(handlers)
	assert.NoError(t, err)

	// test
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

	// test early cancel
	ctx := m.NewStateCtx("B")
	select {
	case <-ctx.Done():
		// pass
	default:
		t.Fatal("expected cancel")
	}

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestQueueCheckable
type TestQueueCheckableHandlers struct {
	*ExceptionHandler
	assertsCount int
}

func (h *TestQueueCheckableHandlers) AState(e *Event) {
	t := e.Args["t"].(*testing.T)
	m := e.Machine()
	m.Add(S{"B"}, nil)
	m.Add(S{"C"}, nil)
	m.Add(S{"D"}, nil)
	assert.Len(t, m.queue, 3, "queue should have 3 mutations scheduled")
	h.assertsCount++

	assert.Equal(t, 1,
		m.IsQueued(MutationAdd, S{"C"}, false, false, 0),
		"C should be queued")
	assert.True(t, m.WillBe1("C"), "A should NOT be queued")
	h.assertsCount++

	assert.Equal(t, -1,
		m.IsQueued(MutationAdd, S{"A"}, false, false, 0),
		"A should NOT be queued")
	assert.False(t, m.WillBe1("A"), "A should NOT be queued")
	h.assertsCount++

	// coverage
	assert.False(t, m.WillBeRemoved1("A"), "No removal should be queued")
	assert.False(t, m.WillBeRemoved1("A"), "No removal should be queued")
}

func TestQueueCheckable(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// bind handlers
	handlers := &TestQueueCheckableHandlers{}
	m.MustBindHandlers(handlers)

	// test
	m.Add(S{"A"}, A{"t": t})

	// assert
	assert.Equal(t, 3, handlers.assertsCount, "asserts executed")
	assertNoException(t, m)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestPartialAuto(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

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

	// test
	m.Add(S{"A"}, nil)

	// assert
	assertStates(t, m, S{"A"})
	assert.Regexp(t, `\[cancel:reject\] [C D]{3}`, log)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestTime(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// relations
	m.states["B"] = State{Multi: true}

	// test 1
	// ()[]
	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"A", "B"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{1, 1, 0, 0})

	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"A", "B"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{1, 3, 0, 0})

	m.Add(S{"A", "B", "C"}, nil)
	assertStates(t, m, S{"A", "B", "C"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{1, 5, 1, 0})

	m.Set(S{"D"}, nil)
	assertStates(t, m, S{"D"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{2, 6, 2, 1})

	m.Add(S{"D", "C"}, nil)
	assertStates(t, m, S{"D", "C"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{2, 6, 3, 1})

	m.Remove(S{"B", "C"}, nil)
	assertStates(t, m, S{"D"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{2, 6, 4, 1})

	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"D", "A", "B"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{3, 7, 4, 1})

	// test 2
	order := S{"A", "B", "C", "D"}
	before := m.Time(order)
	m.Add(S{"C"}, nil)
	now := m.Time(order)

	// assert
	assertStates(t, m, S{"A", "B", "D", "C"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{3, 7, 5, 1})
	assert.True(t, IsTimeAfter(now, before))
	assert.False(t, IsTimeAfter(before, now))
	assert.Equal(t, 16, int(m.TimeSum(nil)))

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestWhenCtx(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A", "B"})

	// wait on "when" methods with a "step" context
	ctx, cancel := context.WithCancel(context.Background())
	whenTimeCh := m.WhenTime(S{"A", "B"}, Time{3, 3}, ctx)
	whenTicks1Ch := m.WhenTicks("A", 2, ctx)
	whenTicks2Ch := m.WhenTicksEq("B", 3, ctx)
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
		t.Fatal("when shouldnt be resolved")
	case <-whenArgsCh:
		t.Fatal("when shouldnt be resolved")
	case <-whenTimeCh:
		t.Fatal("when shouldnt be resolved")
	case <-whenTicks1Ch:
		t.Fatal("when shouldnt be resolved")
	case <-whenTicks2Ch:
		t.Fatal("when shouldnt be resolved")
	case <-ctx.Done():
		// resolved
	}

	// wait for the context to be canceled and cleanups happen
	// TODO bind to default value of the delay
	time.Sleep(2 * 100 * time.Millisecond)

	// assert
	// use internal locks to avoid races
	m.activeStatesLock.Lock()
	m.indexWhenArgsLock.Lock()
	assert.Equal(t, 0, len(m.indexWhenTime))
	assert.Equal(t, 0, len(m.indexWhen))
	assert.Equal(t, 0, len(m.indexWhenArgs))
	m.activeStatesLock.Unlock()
	m.indexWhenArgsLock.Unlock()

	// test validation
	whenTimeCh = m.WhenTime(S{"A", "B"}, Time{1}, nil)
	assert.True(t, m.IsErr())
	<-whenTimeCh // TODO timeout

	// test passed
	whenTimeCh = m.WhenTime(S{"A"}, Time{1}, nil)
	assert.True(t, m.IsErr())
	<-whenTimeCh // TODO timeout

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestWhenArgs(t *testing.T) {
	t.Parallel()
	// init
	m := NewRels(t, nil)

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
	case <-whenCh:
		// pass
	default:
		t.Fatal("when should be resolved")
	}

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestWhenTime(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A", "B"})

	// bind
	// (A:1 B:1)
	whenCh1 := m.WhenTime(S{"A", "B"}, Time{5, 2}, nil)
	_ = m.WhenTime(S{"A", "B"}, Time{7, 2}, nil)
	whenCh3 := m.WhenTime(S{"A", "B"}, Time{9, 2}, nil)

	// tick some, but not enough
	m.Remove(S{"A", "B"}, nil)
	m.Add(S{"A", "B"}, nil)
	// (A:3 B:3) not yet
	select {
	case <-whenCh1:
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
	case <-whenCh1:
		// pass
	}

	m.Remove1("A", nil)
	m.Add1("A", nil)
	m.Remove1("A", nil)
	m.Add1("A", nil)

	<-whenCh3

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestNewCommon
type TestNewCommonHandlers struct {
	*ExceptionHandler
}

func (h *TestNewCommonHandlers) AState(e *Event) {}

func TestNewCommon(t *testing.T) {
	t.Parallel()
	// init
	s := Schema{"A": {}, Exception: {}}
	m, err := NewCommon(context.TODO(), "foo", s, maps.Keys(s),
		&TestNewCommonHandlers{}, nil, nil)

	// assert
	assert.NoError(t, err)
	assert.Equal(t, 1, len(m.handlers))
}

// TestTracers
type TestTracersHandlers struct {
	*ExceptionHandler
}

func (h *TestTracersHandlers) AState(e *Event) {}

func TestTracers(t *testing.T) {
	t.Parallel()
	tNoop := &NoOpTracer{}
	m := New(context.TODO(), Schema{"A": {}}, &Opts{
		Tracers: []Tracer{tNoop},
	})
	_ = m.BindHandlers(&TestTracersHandlers{})
	assert.Equal(t, 1, len(m.tracers))
	assert.False(t, m.tracers[0].Inheritable())
	m.Add1("A", nil)

	// assert
	assert.Len(t, m.Tracers(), 1)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestQueueLimit(t *testing.T) {
	t.Parallel()
	// TODO TestQueueLimit
	t.Skip()
}

func TestSubmachines(t *testing.T) {
	t.Parallel()
	// TODO TestSubmachines
	t.Skip()
}

func TestSetStates(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A", "C"})

	// add relations and states
	s := m.GetStruct()
	s["A"] = State{Multi: true}
	s["B"] = State{Remove: S{"C"}}
	s["D"] = State{Add: S{"E"}}
	s["E"] = State{}

	// update states
	rev := m.SchemaVer()
	err := m.SetStruct(s, S{"A", "B", "C", "D", "E"})
	if err != nil {
		t.Fatal(err)
	}
	assert.NotEqual(t, rev, m.SchemaVer())

	// test
	m.Set(S{"A", "B", "D"}, nil)

	// assert
	assert.ElementsMatch(t, S{"A", "B", "D", "E"}, m.activeStates)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestIs(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A", "B"})

	// test
	assert.True(t, m.Is(S{"A", "B"}), "A B should be active")
	assert.False(t, m.Is(S{"A", "B", "C"}), "A B C shouldnt be active")

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestNot(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A", "B"})

	// test
	assert.False(t, m.Not(S{"A", "B"}), "A B should be active")
	assert.False(t, m.Not(S{"A", "B", "C"}), "A B C is partially active")
	assert.True(t, m.Not1("D"), "D is inactive")

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestAny(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A", "B"})

	// test
	assert.True(t, m.Any(S{"A", "B"}, S{"C"}), "A B should be active")
	assert.True(t, m.Any(S{"A", "B", "C"}, S{"A"}), "A B C is partially active")
	assert.True(t, m.Any1("A", "B"), "A B is fully active")

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestClock(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)
	_ = m.VerifyStates(S{"A", "B", "C", "D", "Exception"})
	// relations
	m.states["B"] = State{Multi: true}

	// test 1
	// ()[]
	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"A", "B"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{1, 1, 0, 0})

	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"A", "B"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{1, 3, 0, 0})

	m.Add(S{"A", "B", "C"}, nil)
	assertStates(t, m, S{"A", "B", "C"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{1, 5, 1, 0})

	m.Set(S{"D"}, nil)
	assertStates(t, m, S{"D"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{2, 6, 2, 1})

	m.Add(S{"D", "C"}, nil)
	assertStates(t, m, S{"D", "C"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{2, 6, 3, 1})

	m.Remove(S{"B", "C"}, nil)
	assertStates(t, m, S{"D"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{2, 6, 4, 1})

	m.Add(S{"A", "B"}, nil)
	assertStates(t, m, S{"D", "A", "B"})
	assertTime(t, m, S{"A", "B", "C", "D"}, Time{3, 7, 4, 1})

	assert.Equal(t, Clock{
		"A": 3, "B": 7, "C": 4, "D": 1, "Exception": 0,
	}, m.Clock(nil))

	assert.Equal(t, Clock{
		"A": 3, "B": 7,
	}, m.Clock(S{"A", "B"}))

	assert.Equal(t, uint64(3), m.Tick("A"))

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestInspect(t *testing.T) {
	t.Parallel()
	m := NewRels(t, S{"A", "C"})
	// (A:1 C:1)[B:0 D:0 Exception:0]
	names := S{"A", "B", "C", "D", "Exception"}
	expected := `
		1 A
		    |Time     1
		    |Auto     true
		    |Require  C
		0 B
		    |Time     0
		    |Multi    true
		    |Add      C
		1 C
		    |Time     1
		    |After    D
		0 D
		    |Time     0
		    |Add      C B
		0 Exception
		    |Time     0
		    |Multi    true
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
		0 Exception
		    |Time     0
		    |Multi    true
		1 A
		    |Time     3
		    |Auto     true
		    |Require  C
		1 B
		    |Time     1
		    |Multi    true
		    |Add      C
		1 C
		    |Time     3
		    |After    D
		1 D
		    |Time     1
		    |Add      C B
    `
	err := m.VerifyStates(S{"Exception", "A", "B", "C", "D"})
	assert.NoError(t, err)
	assertString(t, m, expected, nil)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestNilCtx(t *testing.T) {
	t.Parallel()
	m := New(nil, Schema{"A": {}}, nil) //nolint:all
	assert.Greater(t, len(m.id), 5)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestWhenQueueEnds
type TestWhenQueueEndsHandlers struct {
	*ExceptionHandler
}

func (h *TestWhenQueueEndsHandlers) AState(e *Event) {
	close(e.Args["readyMut"].(chan struct{}))
	<-e.Args["readyGo"].(chan struct{})
	e.Machine().Add1("B", nil)
}

func TestWhenQueueEnds(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// order
	err := m.VerifyStates(S{"A", "B", "C", "D", "Exception"})
	if err != nil {
		t.Fatal(err)
	}

	// bind handlers
	err = m.BindHandlers(&TestWhenQueueEndsHandlers{})
	assert.NoError(t, err)

	// test
	readyGo := make(chan struct{})
	readyMut := make(chan struct{})
	var queueEnds <-chan struct{}
	go func() {
		<-readyMut
		assert.NotNil(t, m.Transition(),
			"Machine should be during a transition")
		queueEnds = m.WhenQueueEnds(context.TODO())
		close(readyGo)
	}()
	m.Add1("A", A{"readyMut": readyMut, "readyGo": readyGo})
	// confirm the queue wait is closed
	<-queueEnds

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestGetRelationsBetween(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// relations
	m.states["A"] = State{
		Add:   S{"B", "C"},
		After: S{"B"},
	}
	m.states["B"] = State{
		Remove: S{"C"},
		Add:    S{"A"},
	}
	m.states["C"] = State{After: S{"D"}}
	m.states["D"] = State{Require: S{"A"}}

	getRels := func(from, to string) []Relation {
		relations, err := m.resolver.GetRelationsBetween(from, to)
		assert.NoError(t, err)
		return relations
	}

	// test and assert
	rels := getRels("A", "B")
	assert.Equal(t, RelationAdd, rels[0])
	assert.Equal(t, RelationAfter, rels[1])
	rels = getRels("B", "C")
	assert.Equal(t, RelationRemove, rels[0])
	rels = getRels("C", "D")
	assert.Equal(t, RelationAfter, rels[0])
	rels = getRels("D", "A")
	assert.Equal(t, RelationRequire, rels[0])

	_, err := m.resolver.GetRelationsBetween("Unknown1", "A")
	assert.Error(t, err)

	_, err = m.resolver.GetRelationsBetween("A", "Unknown1")
	assert.Error(t, err)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestString(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A", "B"})
	_ = m.VerifyStates(S{"A", "B", "C", "D", "Exception"})

	// test
	assert.Equal(t, "(A:1 B:1)", m.String())
	assert.Equal(t, "(A:1 B:1) [C:0 D:0 Exception:0]", m.StringAll())

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestNestedMutation
type TestNestedMutationHandlers struct {
	*ExceptionHandler
}

func (h *TestNestedMutationHandlers) AState(e *Event) {
	t := e.Args["t"].(*testing.T)

	e.Machine().Add1("B", nil)
	e.Machine().Add1("B", nil)
	e.Machine().Add1("B", nil)
	assert.Equal(t, 1, len(e.Machine().queue))

	e.Machine().Remove1("B", nil)
	assert.Equal(t, 2, len(e.Machine().queue))
	e.Machine().Remove1("B", nil)
	assert.Equal(t, 2, len(e.Machine().queue))
}

func TestNestedMutation(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"B"})

	// bind handlers
	err := m.BindHandlers(&TestNestedMutationHandlers{})
	assert.NoError(t, err)

	// test
	m.Add1("A", A{"t": t})

	// assert
	assertStates(t, m, S{"A"})

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestVerifyStates(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, S{"A", "B"})

	// test
	err := m.VerifyStates(S{"A", "A", "B", "Err"})
	assert.Error(t, err)
	err = m.VerifyStates(S{"A", "A"})
	assert.Error(t, err)
	err = m.VerifyStates(S{"A", "B"})
	assert.Error(t, err)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestHas(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// test err
	assert.False(t, m.Has(S{"A", "Err"}))

	// test ok
	assert.True(t, m.Has(S{"A", "A", "B"}))
	assert.True(t, m.Has(m.StateNames()))
	assert.True(t, m.Has(S{"A", "B"}))

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestLogger(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)

	// test
	m.SetLoggerSimple(t.Logf, LogEverything)
	assert.NotNil(t, m.GetLogger())
	assert.Panics(t, func() {
		m.SetLoggerSimple(nil, LogEverything)
	})
	// coverage
	m.Add1("A", nil)
	m.SetLogger(nil)
	m.Add1("A", nil)
	m.SetLogId(!m.GetLogId())
	m.Log("foo")
	m.LogLvl(LogChanges, "foo")
	m.SetLoggerEmpty(LogChanges)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestIsClock(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)
	cA := m.Clock(S{"A"})
	cAll := m.Clock(nil)

	m.Add(S{"A", "B"}, nil)

	// test
	assert.False(t, m.IsClock(cAll))
	assert.False(t, m.IsClock(cA))

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

func TestIsTime(t *testing.T) {
	t.Parallel()
	// init
	m := NewNoRels(t, nil)
	tA := m.Time(S{"A"})
	tAll := m.Time(nil)

	m.Add(S{"A", "B"}, nil)

	// test
	assert.False(t, m.IsTime(tA, S{"A"}))
	assert.False(t, m.IsTime(tAll, nil))

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TODO TestAnyEnterHandler
func TestAnyEnterHandler(t *testing.T) {
	t.Parallel()
	t.Skip()
}

func TestExportImport(t *testing.T) {
	t.Parallel()
	// init
	m1 := NewNoRels(t, S{"A"})
	defer m1.Dispose()
	err := m1.VerifyStates(m1.StateNames())
	if err != nil {
		t.Fatal(err)
	}

	// change clocks
	m1.Remove1("B", nil)
	m1.Add1("A", nil)
	m1.Add1("B", nil)
	m1.Add1("C", nil)
	m1Str := m1.String()

	// export
	serial := m1.Export()

	// import
	m2 := NewNoRels(t, nil)
	err = m2.Import(serial)
	if err != nil {
		t.Fatal(err)
	}

	// assert
	assert.True(t, m2.StatesVerified(),
		"imported states have deterministic order")
	assert.Equal(t, m1.id, m2.id, "imported machine ID should be the same")
	assert.Equal(t, m1Str, m2.String(),
		"imported machine clock should be the same")
}

// TestNestedMutation
type TestHandlerTimeoutHandlers struct {
	*ExceptionHandler
}

func (h *TestHandlerTimeoutHandlers) AState(e *Event) {
	// wait longer then the timeout
	time.Sleep(500 * time.Millisecond)
}

func TestHandlerTimeout(t *testing.T) {
	t.Parallel()

	// init
	m := NewNoRels(t, nil)
	m.HandlerTimeout = 450 * time.Millisecond

	// bind handlers
	err := m.BindHandlers(&TestHandlerTimeoutHandlers{})
	assert.NoError(t, err)

	// test
	res := m.Add1("A", nil)

	// assert TODO assert log
	assert.Equal(t, Canceled, res)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestBindTracer

type TestBindTracerTracer struct {
	*NoOpTracer
}

func TestBindTracer(t *testing.T) {
	t.Parallel()

	// init
	m := NewNoRels(t, nil)
	trace := &TestBindTracerTracer{}

	// test
	_ = m.BindTracer(trace)
	removed := m.DetachTracer(trace)

	// assert
	assert.Len(t, m.tracers, 0)
	assert.True(t, removed)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestUnbindHandlers

type TestUnbindHandlersHandlers struct {
	*ExceptionHandler
}

func TestUnbindHandlers(t *testing.T) {
	t.Parallel()

	// init
	m := NewNoRels(t, nil)
	h := &TestUnbindHandlersHandlers{}

	// test
	err := m.BindHandlers(h)
	if err != nil {
		t.Fatal(err)
	}
	err = m.DetachHandlers(h)
	if err != nil {
		t.Fatal(err)
	}

	// assert
	assert.Len(t, m.handlers, 0)

	// dispose
	m.Dispose()
	<-m.WhenDisposed()
}

// TestListHandlers

type TestListHandlersHandlers struct{}

func (h *TestListHandlersHandlers) AEnter(e *Event) bool {
	return false
}
func (h *TestListHandlersHandlers) AnyEnter(e *Event)  {}
func (h *TestListHandlersHandlers) AState(e *Event)    {}
func (h *TestListHandlersHandlers) AnyB(e *Event)      {}
func (h *TestListHandlersHandlers) Unrelated(e *Event) {}

func TestListHandlers(t *testing.T) {
	t.Parallel()

	// defined handlers
	h1 := &TestListHandlersHandlers{}
	names, err := ListHandlers(h1, S{"A", "B", "C"})
	if err != nil {
		t.Fatal(err)
	}

	// assert
	assert.Len(t, names, 4)
	assert.ElementsMatch(t, []string{"AEnter", "AState", "AnyB", HandlerGlobal},
		names)

	// anon handlers
	h2 := &struct {
		AEnter    HandlerNegotiation
		AState    HandlerFinal
		AnyEnter  HandlerFinal
		AnyB      HandlerFinal
		Unrelated func()
	}{
		AEnter:    func(e *Event) bool { return false },
		AState:    func(e *Event) {},
		AnyEnter:  func(e *Event) {},
		AnyB:      func(e *Event) {},
		Unrelated: func() {},
	}
	names, err = ListHandlers(h2, S{"A", "B", "C"})
	if err != nil {
		t.Fatal(err)
	}

	// assert
	assert.Len(t, names, 4)
	assert.ElementsMatch(t, []string{"AEnter", "AState", "AnyB", HandlerGlobal},
		names)

	// anon handlers for non-existing states
	h3 := &struct {
		ZEnter    HandlerNegotiation
		ZState    HandlerFinal
		AnyEnter  HandlerFinal
		AnyY      HandlerFinal
		Unrelated func()
	}{
		ZEnter:    func(e *Event) bool { return false },
		ZState:    func(e *Event) {},
		AnyEnter:  func(e *Event) {},
		AnyY:      func(e *Event) {},
		Unrelated: func() {},
	}
	_, err = ListHandlers(h3, S{"A", "B", "C"})
	assert.ErrorContains(t, err, "missing states: Z from handler ZEnter")
	assert.ErrorContains(t, err, "missing states: Z from handler ZState")
}

// TODO TestResolverDagSort

func TestCountActive(t *testing.T) {
	t.Parallel()

	// init
	m := NewNoRels(t, nil)
	m.Add1("A", nil)

	assert.Equal(t, m.CountActive(S{"A", "B"}), 1)
}

func TestAddErr(t *testing.T) {
	t.Parallel()

	// add err
	m := NewNoRels(t, nil)
	err := errors.New("test")
	m.AddErrState("B", err, nil)
	m.AddErr(err, nil)
	assert.True(t, m.Is1("B"))
	assert.True(t, m.IsErr())

	// panic to state
	m = NewNoRels(t, nil)
	go func() {
		defer m.PanicToErrState("B", nil)

		panic("test")
	}()
	<-m.WhenErr(nil)
	assert.True(t, m.Is1("B"))
	assert.True(t, m.IsErr())

	// panic to err
	m = NewNoRels(t, nil)
	go func() {
		defer m.PanicToErr(nil)

		panic("test")
	}()
	<-m.WhenErr(nil)
	assert.True(t, m.IsErr())
}

func TestToggle(t *testing.T) {
	t.Parallel()

	m := NewNoRels(t, nil)
	m.Toggle1("A", nil)
	assert.True(t, m.Is1("A"))
	m.Toggle1("A", nil)
	assert.False(t, m.Is1("A"))

	m = NewNoRels(t, nil)
	m.Toggle(S{"A"}, nil)
	assert.True(t, m.Is1("A"))
	m.Toggle(S{"A"}, nil)
	assert.False(t, m.Is1("A"))

	// TODO test params
}

func TestOpts(t *testing.T) {
	t.Parallel()

	// TODO test parent tracer

	m := NewNoRels(t, nil)

	tags := []string{"a", "b"}
	m2 := New(m.Ctx(), Schema{}, &Opts{
		Parent:            m,
		DontLogID:         true,
		DontLogStackTrace: true,
		LogLevel:          LogChanges,
		QueueLimit:        10,
		Tags:              tags,
	})
	assert.Equal(t, m.Id(), m2.ParentId())
	assert.Equal(t, false, m2.LogStackTrace)
	assert.Equal(t, tags, m2.Tags())
	assert.Equal(t, 10, m2.QueueLimit)
	assert.Equal(t, LogChanges, m2.GetLogLevel())

	tags2 := []string{"c"}
	m2.SetTags(tags2)
	assert.Equal(t, tags2, m2.Tags())
}
