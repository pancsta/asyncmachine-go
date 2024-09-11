package rpc

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	ss "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/types"
)

type S = am.S

// type A = am.A
type (
	State  = am.State
	Struct = am.Struct
)

func init() {
	if os.Getenv("AM_TEST_DEBUG") != "" {
		utils.EnableTestDebug()
	}
}

func TestSingleStateActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRels(t, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// test
	w.Add1("A", nil)

	// assert
	assertStates(t, c.Worker, S{"A"})

	disposeTest(c, s)
}

func TestMultipleStatesActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRels(t, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// test
	w.Add(S{"A"}, nil)
	w.Add(S{"B"}, nil)

	// assert
	assertStates(t, c.Worker, S{"A", "B"})

	disposeTest(c, s)
}

func TestExposeAllStateNames(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRels(t, S{"A"})
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// assert
	assert.ElementsMatch(t, S{"A", "B", "C", "D", "Exception"},
		w.StateNames())

	disposeTest(c, s)
}

func TestStateSet(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRels(t, S{"A"})
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// test
	w.Set(S{"B"}, nil)

	// assert
	assertStates(t, w, S{"B"})

	disposeTest(c, s)
}

func TestStateAdd(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRels(t, S{"A"})
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// test
	w.Add(S{"B"}, nil)

	// assert
	assertStates(t, w, S{"A", "B"})

	disposeTest(c, s)
}

func TestStateRemove(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRels(t, S{"B", "C"})
	_, _, s, c := NewTest(t, ctx, m, nil, nil)

	// test
	c.Worker.Remove(S{"C"}, nil)
	w := c.Worker

	// assert
	assertStates(t, w, S{"B"})

	disposeTest(c, s)
}

func TestRemoveRelation(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// relations
	m := utils.NewNoRels(t, S{"D"})
	rels := m.GetStruct()
	rels["C"] = State{Remove: S{"D"}}
	_ = m.SetStruct(rels, ss.Names)
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// C deactivates D
	w.Add(S{"C"}, nil)
	assertStates(t, w, S{"C"})

	disposeTest(c, s)
}

func TestRemoveRelationSimultaneous(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRels(t, S{"D"})

	// relations
	rels := m.GetStruct()
	rels["C"] = State{Remove: S{"D"}}
	_ = m.SetStruct(rels, ss.Names)
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// test
	r := w.Set(S{"C", "D"}, nil)

	// assert
	assert.Equal(t, am.Canceled, r)
	assertStates(t, w, S{"D"})

	disposeTest(c, s)
}

func TestRemoveRelationCrossBlocking(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, w *Worker)
	}{
		{
			"using Set should de-activate the old one",
			func(t *testing.T, w *Worker) {
				// (D:1)[A:0 B:0 C:0]
				w.Set(S{"C"}, nil)
				assertStates(t, w, S{"C"})
			},
		},
		{
			"using Set should work both ways",
			func(t *testing.T, w *Worker) {
				// (D:1)[A:0 B:0 C:0]
				w.Set(S{"C"}, nil)
				assertStates(t, w, S{"C"})
				w.Set(S{"D"}, nil)
				assertStates(t, w, S{"D"})
			},
		},
		{
			"using Add should de-activate the old one",
			func(t *testing.T, w *Worker) {
				// (D:1)[A:0 B:0 C:0]
				w.Add(S{"C"}, nil)
				assertStates(t, w, S{"C"})
			},
		},
		{
			"using Add should work both ways",
			func(t *testing.T, w *Worker) {
				// (D:1)[A:0 B:0 C:0]
				w.Add(S{"C"}, nil)
				assertStates(t, w, S{"C"})
				w.Add(S{"D"}, nil)
				assertStates(t, w, S{"D"})
			},
		},
	}

	for i := range tests {
		test := tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// init
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			m := utils.NewNoRels(t, S{"D"})
			rels := m.GetStruct()
			// C and D are cross blocking each other via Remove
			rels["C"] = State{Remove: S{"D"}}
			rels["D"] = State{Remove: S{"C"}}
			_ = m.SetStruct(rels, ss.Names)
			_, _, s, c := NewTest(t, ctx, m, nil, nil)
			w := c.Worker

			// test
			test.fn(t, w)

			// dispose
			disposeTest(c, s)
		})
	}
}

func disposeTest(c *Client, s *Server) {
	c.Stop(context.TODO(), true)
	<-c.Mach.WhenDisposed()
	s.Stop(true)
}

func TestAddRelation(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	m := utils.NewNoRels(t, nil)
	rels := m.GetStruct()
	rels["A"] = State{Remove: S{"D"}}
	rels["C"] = State{Add: S{"D"}}
	_ = m.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// test
	w.Set(S{"C"}, nil)

	// assert
	assertStates(t, w, S{"C", "D"}, "state should be activated")
	w.Set(S{"A", "C"}, nil)
	assertStates(t, w, S{"A", "C"}, "state should be skipped if "+
		"blocked at the same time")

	disposeTest(c, s)
}

func TestRequireRelation(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, nil)
	rels := mach.GetStruct()
	rels["A"] = State{Require: S{"D"}}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	w.Set(S{"C", "D"}, nil)

	// assert
	assertStates(t, w, S{"C", "D"})

	disposeTest(c, s)
}

func TestRequireRelationWhenRequiredIsntActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	rels := mach.GetStruct()
	rels["C"] = State{Require: S{"D"}}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	w.Set(S{"C", "A"}, nil)

	// assert
	assertStates(t, w, S{"A"}, "target state shouldnt be activated")

	disposeTest(c, s)
}

// TestQueue
type TestQueueHandlers struct{}

func (h *TestQueueHandlers) BEnter(e *am.Event) bool {
	e.Machine.Add(S{"C"}, nil)
	return true
}

// TestNegotiationCancel
type TestNegotiationCancelHandlers struct{}

func (h *TestNegotiationCancelHandlers) DEnter(_ *am.Event) bool {
	return false
}

func TestNegotiationCancel(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T, m types.MachineApi) am.Result
		log  *regexp.Regexp
	}{
		{
			"using set",
			func(t *testing.T, m types.MachineApi) am.Result {
				// m = (A)
				// DEnter will cancel the transition
				return m.Set(S{"D"}, nil)
			},
			regexp.MustCompile(`\[cancel] \(D\) by DEnter`),
		},
		{
			"using add",
			func(t *testing.T, m types.MachineApi) am.Result {
				// m = (A)
				// DEnter will cancel the transition
				return m.Add(S{"D"}, nil)
			},
			regexp.MustCompile(`\[cancel] \(D A\) by DEnter`),
		},
	}

	for i := range tests {
		test := tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// init
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// machine
			mach := utils.NewNoRels(t, S{"A"})
			rels := mach.GetStruct()
			rels["C"] = State{Require: S{"D"}}
			_ = mach.SetStruct(rels, ss.Names)

			// bind handlers
			err := mach.BindHandlers(&TestNegotiationCancelHandlers{})
			assert.NoError(t, err)

			// worker
			_, _, s, c := NewTest(t, ctx, mach, nil, nil)
			w := c.Worker

			// test
			result := test.fn(t, w)

			// assert
			assert.Equal(t, am.Canceled, result, "transition should be canceled")
			assertStates(t, w, S{"A"}, "state shouldnt be changed")

			// dispose
			disposeTest(c, s)
		})
	}
}

func TestAutoStates(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, nil)
	rels := mach.GetStruct()
	rels["B"] = State{
		Auto:    true,
		Require: S{"A"},
	}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	result := w.Add(S{"A"}, nil)

	// assert
	assert.Equal(t, am.Executed, result, "transition should be executed")
	assertStates(t, w, S{"A", "B"}, "dependant auto state should be set")

	disposeTest(c, s)
}

// TestNegotiationRemove
type TestNegotiationRemoveHandlers struct{}

func (h *TestNegotiationRemoveHandlers) AExit(_ *am.Event) bool {
	return false
}

func TestNegotiationRemove(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	err := mach.BindHandlers(&TestNegotiationRemoveHandlers{})
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	// AExit will cancel the transition
	result := w.Remove(S{"A"}, nil)

	// assert
	assert.Equal(t, am.Canceled, result, "transition should be canceled")
	assertStates(t, w, S{"A"}, "state shouldnt be changed")

	disposeTest(c, s)
}

// TestHandlerStateInfo
type TestHandlerStateInfoHandlers struct{}

func (h *TestHandlerStateInfoHandlers) DEnter(e *am.Event) {
	t := e.Args["t"].(*testing.T)
	assert.ElementsMatch(t, S{"A"}, e.Machine.ActiveStates(),
		"provide the previous states of the transition")
	assert.ElementsMatch(t, S{"D"}, e.Transition().TargetStates,
		"provide the target states of the transition")
	assert.True(t, e.Mutation().StateWasCalled("D"),
		"provide the called states of the transition")
	tStr := "D -> requested\nD -> set\nA -> remove"
	assert.Equal(t, tStr, e.Transition().String(),
		"provide a string version of the transition")
}

func TestSwitch(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	rels := mach.GetStruct()
	rels["C"] = State{Require: S{"D"}}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	caseA := false
	switch w.Switch("A", "B") {
	case "A":
		caseA = true
	case "B":
	}
	assert.Equal(t, true, caseA)

	caseDef := false
	switch w.Switch("C", "B") {
	case "B":
	default:
		caseDef = true
	}
	assert.Equal(t, true, caseDef)

	disposeTest(c, s)
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
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	err := mach.BindHandlers(&TestHandlerArgsHandlers{})
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	// handlers will assert
	w.Add(S{"B"}, am.A{"t": t, "foo": "bar"})
	w.Remove(S{"A"}, am.A{"t": t, "foo": "bar"})
	w.Set(S{"C"}, am.A{"t": t, "foo": "bar"})

	disposeTest(c, s)
}

// TestSelfHandlersCancellable
type TestSelfHandlersCancellableHandlers struct {
	result int
}

func (h *TestSelfHandlersCancellableHandlers) AA(e *am.Event) bool {
	AACounter := e.Args["AACounter"].(int)
	h.result = AACounter + 1
	return false
}

func TestSelfHandlersCancellable(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	handlers := &TestSelfHandlersCancellableHandlers{}
	err := mach.BindHandlers(handlers)
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// bind handlers

	// test
	// handlers will assert
	AACounter := 1
	w.Set(S{"A", "B"}, am.A{"AACounter": &AACounter})

	// assert
	assert.Equal(t, 2, handlers.result, "AA call count")

	disposeTest(c, s)
}

func TestRegressionRemoveCrossBlockedByImplied(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomStates(t, Struct{
		"A": {Remove: S{"B"}},
		"B": {Remove: S{"A"}},
		"Z": {Add: S{"B"}},
	})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	w.Set(S{"Z"}, nil)

	// assert
	assertStates(t, w, S{"Z", "B"})

	disposeTest(c, s)
}

func TestRegressionImpliedBlockByBeingRemoved(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomStates(t, Struct{
		"Wet":   {Require: S{"Water"}},
		"Dry":   {Remove: S{"Wet"}},
		"Water": {Add: S{"Wet"}, Remove: S{"Dry"}},
	})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	w.Set(S{"Dry"}, nil)
	w.Set(S{"Water"}, nil)

	// assert
	assertStates(t, w, S{"Water", "Wet"})

	disposeTest(c, s)
}

// TestWhen
type TestWhenHandlers struct{}

func (h *TestWhenHandlers) AState(e *am.Event) {
	go func() {
		time.Sleep(10 * time.Millisecond)
		e.Machine.Add(S{"B"}, nil)
		time.Sleep(10 * time.Millisecond)
		// this triggers When
		e.Machine.Add(S{"C"}, nil)
	}()
}

func TestWhen(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, nil)
	// bind handlers
	err := mach.BindHandlers(&TestWhenHandlers{})
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	w.Set(S{"A"}, nil)
	<-w.When(S{"B", "C"}, nil)

	// assert
	assertStates(t, w, S{"A", "B", "C"})

	disposeTest(c, s)
}

func TestWhen2(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, nil)
	// bind handlers
	err := mach.BindHandlers(&TestWhenHandlers{})
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test

	ready1 := make(chan struct{})
	pass1 := make(chan struct{})
	go func() {
		close(ready1)
		<-w.When(S{"A", "B"}, nil)
		close(pass1)
	}()

	ready2 := make(chan struct{})
	pass2 := make(chan struct{})
	go func() {
		close(ready2)
		<-w.When(S{"A", "B"}, nil)
		close(pass2)
	}()

	<-ready1
	<-ready2

	w.Add(S{"A", "B"}, nil)

	<-pass1
	<-pass2

	disposeTest(c, s)
}

func TestWhenActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	<-w.When(S{"A"}, nil)

	// assert
	assertStates(t, w, S{"A"})

	disposeTest(c, s)
}

// TestWhenNot
type TestWhenNotHandlers struct{}

func (h *TestWhenNotHandlers) AState(e *am.Event) {
	go func() {
		time.Sleep(10 * time.Millisecond)
		e.Machine.Remove1("B", nil)
		time.Sleep(10 * time.Millisecond)
		e.Machine.Remove1("C", nil)
	}()
}

func TestWhenNot(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, nil)
	// bind handlers
	err := mach.BindHandlers(&TestWhenNotHandlers{})
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	w.Add(S{"A"}, nil)
	<-w.WhenNot(S{"B", "C"}, nil)

	// assert
	assertStates(t, w, S{"A"})
	assertNoException(t, w)

	disposeTest(c, s)
}

func TestWhenNot2(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A", "B"})
	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test

	ready1 := make(chan struct{})
	pass1 := make(chan struct{})
	go func() {
		close(ready1)
		<-w.WhenNot(S{"A", "B"}, nil)
		close(pass1)
	}()

	ready2 := make(chan struct{})
	pass2 := make(chan struct{})
	go func() {
		close(ready2)
		<-w.WhenNot(S{"A", "B"}, nil)
		close(pass2)
	}()

	<-ready1
	<-ready2

	w.Remove(S{"A", "B"}, nil)

	<-pass1
	<-pass2

	disposeTest(c, s)
}

func TestWhenNotActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	<-w.WhenNot(S{"B"}, nil)

	// assert
	assertStates(t, w, S{"A"})

	disposeTest(c, s)
}

// TestPartialNegotiationPanic
type TestPartialNegotiationPanicHandlers struct {
	*ExceptionHandler
}

func (h *TestPartialNegotiationPanicHandlers) BEnter(_ *am.Event) {
	panic("BEnter panic")
}

func TestPartialNegotiationPanic(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	err := mach.BindHandlers(&TestPartialNegotiationPanicHandlers{})
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	assert.Equal(t, am.Canceled, w.Add(S{"B"}, nil))
	// assert
	assertStates(t, w, S{"A", "Exception"})

	disposeTest(c, s)
}

// TestPartialFinalPanic
type TestPartialFinalPanicHandlers struct {
	*ExceptionHandler
}

func (h *TestPartialFinalPanicHandlers) BState(_ *am.Event) {
	panic("BState panic")
}

func TestPartialFinalPanic(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	err := mach.BindHandlers(&TestPartialFinalPanicHandlers{})
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	w.Add(S{"A", "B", "C"}, nil)

	// assert
	assertStates(t, w, S{"A", "Exception"})

	disposeTest(c, s)
}

// TestStateCtx
type TestStateCtxHandlers struct {
	*ExceptionHandler
	t          *testing.T
	stepCh     chan struct{}
	callbackCh chan struct{}
}

func (h *TestStateCtxHandlers) AState(e *am.Event) {
	stateCtx := e.Machine.NewStateCtx("A")
	h.callbackCh = make(chan struct{})
	go func() {
		<-h.stepCh
		assertStates(h.t, e.Machine, S{})
		assert.Error(h.t, stateCtx.Err(), "state context should be canceled")
		close(h.callbackCh)
	}()
}

func TestStateCtx(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, nil)
	handlers := &TestStateCtxHandlers{
		stepCh: make(chan struct{}),
		t:      t,
	}
	err := mach.BindHandlers(handlers)
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	// BState will assert
	w.Add(S{"A"}, nil)
	w.Remove(S{"A"}, nil)
	close(handlers.stepCh)
	<-handlers.callbackCh

	// assert
	assertStates(t, w, S{})

	disposeTest(c, s)
}

func TestPartialAuto(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	rels := mach.GetStruct()
	// relations
	rels["C"] = State{
		Auto:    true,
		Require: S{"B"},
	}
	rels["D"] = State{
		Auto:    true,
		Require: S{"B"},
	}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	w.Add(S{"A"}, nil)

	// assert
	assertStates(t, w, S{"A"})

	disposeTest(c, s)
}

func TestTime(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A"})
	rels := mach.GetStruct()
	rels["B"] = State{Multi: true}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test 1
	// ()[]
	w.Add(S{"A", "B"}, nil)
	assertStates(t, w, S{"A", "B"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{1, 1, 0, 0})

	w.Add(S{"A", "B"}, nil)
	assertStates(t, w, S{"A", "B"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{1, 3, 0, 0})

	w.Add(S{"A", "B", "C"}, nil)
	assertStates(t, w, S{"A", "B", "C"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{1, 5, 1, 0})

	w.Set(S{"D"}, nil)
	assertStates(t, w, S{"D"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{2, 6, 2, 1})

	w.Add(S{"D", "C"}, nil)
	assertStates(t, w, S{"D", "C"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{2, 6, 3, 1})

	w.Remove(S{"B", "C"}, nil)
	assertStates(t, w, S{"D"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{2, 6, 4, 1})

	w.Add(S{"A", "B"}, nil)
	assertStates(t, w, S{"D", "A", "B"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{3, 7, 4, 1})

	// test 2
	order := S{"A", "B", "C", "D"}
	before := w.Time(order)
	w.Add(S{"C"}, nil)
	now := w.Time(order)

	// assert
	assertStates(t, w, S{"A", "B", "D", "C"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{3, 7, 5, 1})
	assert.True(t, am.IsTimeAfter(now, before))
	assert.False(t, am.IsTimeAfter(before, now))

	disposeTest(c, s)
}

// TODO WhenArgs
// func TestWhenCtx(t *testing.T) {
//	t.Parallel()

// init
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	//
//	// machine
//	mach := amtest.NewNoRels(t, S{"A", "B"})
//	// worker
//	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
//	w := c.Worker
//	disposeTest(c, s)
//
//	// wait on 2 Whens with a step context
//	ctxWhen, cancelWhen := context.WithCancel(context.Background())
//	whenTimeCh := w.WhenTime(S{"A", "B"}, am.Time{3, 3}, ctxWhen)
//	whenArgsCh := w.WhenArgs("B", am.A{"foo": "bar"}, ctxWhen)
//	whenCh := w.When1("C", ctxWhen)
//
//	// assert
//	assert.Greater(t, len(w.indexWhenTime), 0)
//	assert.Greater(t, len(w.indexWhen), 0)
//	assert.Greater(t, len(w.indexWhenArgs), 0)
//
//	go func() {
//		time.Sleep(10 * time.Millisecond)
//		cancelWhen()
//	}()
//
//	select {
//	case <-whenCh:
//	case <-whenArgsCh:
//	case <-whenTimeCh:
//	case <-ctxWhen.Done():
//	}
//
//	// wait for the context to be canceled and cleanups happen
//	time.Sleep(time.Millisecond)
//
//	// assert
//	assert.Equal(t, len(w.indexWhenTime), 0)
//	assert.Equal(t, len(w.indexWhen), 0)
//	assert.Equal(t, len(w.indexWhenArgs), 0)
//
//	// dispose
//	disposeTest(c, s)
// }
//
// func TestWhenArgs(t *testing.T) {
//	t.Parallel()

// init
//	m := NewRels(t, nil)
//	defer w.Dispose()
//
//	// bind
//	whenCh := m.WhenArgs("B", am.A{"foo": "bar"}, nil)
//
//	// incorrect args
//	w.Add1("B", am.A{"foo": "foo"})
//	select {
//	case <-whenCh:
//		t.Fatal("when shouldnt be resolved")
//	default:
//		// pass
//	}
//
//	// correct args
//	w.Add1("B", am.A{"foo": "bar"})
//	select {
//	default:
//		t.Fatal("when should be resolved")
//	case <-whenCh:
//		// pass
//	}
//
//	// dispose
//	w.Dispose()
//	<-w.WhenDisposed()
// }

func TestWhenTime(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A", "B"})
	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// bind
	// (A:1 B:1)
	whenCh := w.WhenTime(S{"A", "B"}, am.Time{5, 2}, nil)

	// tick some, but not enough
	w.Remove(S{"A", "B"}, nil)
	w.Add(S{"A", "B"}, nil)
	// (A:3 B:3) not yet
	select {
	case <-whenCh:
		t.Fatal("when shouldnt be resolved")
	default:
		// pass
	}

	w.Remove1("A", nil)
	w.Add1("A", nil)
	// (A:5 B:3) OK

	select {
	default:
		t.Fatal("when should be resolved")
	case <-whenCh:
		// pass
	}

	disposeTest(c, s)
}

func TestIs(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A", "B"})
	rels := mach.GetStruct()
	rels["B"] = State{
		Auto:    true,
		Require: S{"A"},
	}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	assert.True(t, w.Is(S{"A", "B"}), "A B should be active")
	assert.False(t, w.Is(S{"A", "B", "C"}), "A B C shouldnt be active")

	disposeTest(c, s)
}

func TestNot(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A", "B"})
	rels := mach.GetStruct()
	rels["B"] = State{
		Auto:    true,
		Require: S{"A"},
	}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	assert.False(t, w.Not(S{"A", "B"}), "A B should be active")
	assert.False(t, w.Not(S{"A", "B", "C"}), "A B C is partially active")
	assert.True(t, w.Not1("D"), "D is inactive")

	disposeTest(c, s)
}

func TestAny(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A", "B"})
	rels := mach.GetStruct()
	rels["B"] = State{
		Auto:    true,
		Require: S{"A"},
	}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	assert.True(t, w.Any(S{"A", "B"}, S{"C"}), "A B should be active")
	assert.True(t, w.Any(S{"A", "B", "C"}, S{"A"}),
		"A B C is partially active")

	disposeTest(c, s)
}

func TestClock(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, nil)
	rels := mach.GetStruct()
	// relations
	rels["B"] = State{Multi: true}
	_ = mach.SetStruct(rels, ss.Names)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test 1
	// ()[]
	w.Add(S{"A", "B"}, nil)
	assertStates(t, w, S{"A", "B"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{1, 1, 0, 0})

	w.Add(S{"A", "B"}, nil)
	assertStates(t, w, S{"A", "B"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{1, 3, 0, 0})

	w.Add(S{"A", "B", "C"}, nil)
	assertStates(t, w, S{"A", "B", "C"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{1, 5, 1, 0})

	w.Set(S{"D"}, nil)
	assertStates(t, w, S{"D"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{2, 6, 2, 1})

	w.Add(S{"D", "C"}, nil)
	assertStates(t, w, S{"D", "C"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{2, 6, 3, 1})

	w.Remove(S{"B", "C"}, nil)
	assertStates(t, w, S{"D"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{2, 6, 4, 1})

	w.Add(S{"A", "B"}, nil)
	assertStates(t, w, S{"D", "A", "B"})
	assertTime(t, w, S{"A", "B", "C", "D"}, am.Time{3, 7, 4, 1})

	assert.Equal(t, am.Clock{
		"A": 3, "B": 7, "C": 4, "D": 1, "Exception": 0,
	}, w.Clock(nil))

	assert.Equal(t, am.Clock{
		"A": 3, "B": 7,
	}, w.Clock(S{"A", "B"}))

	assert.Equal(t, uint64(3), w.Tick("A"))

	disposeTest(c, s)
}

func TestInspect(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewRels(t, S{"A", "C"})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

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
	assertString(t, w, expected, names)
	// (A:1 C:1)[B:0 D:0 Exception:0]
	w.Remove(S{"C"}, nil)
	// ()[A:2 B:0 C:2 D:0 Exception:0]
	w.Add(S{"B"}, nil)
	// (A:3 B:1 C:3)[D:0 Exception:0]
	w.Add(S{"D"}, nil)
	// (A:3 B:1 C:3 D:1)[Exception:0]
	expected = `
		Exception:
		  State:   false 0
		  Multi:   true
	
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
		`
	assertString(t, w, expected, nil)

	disposeTest(c, s)
}

func TestString(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, S{"A", "B"})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	assert.Equal(t, "(A:1 B:1)", w.String())
	assert.Equal(t, "(A:1 B:1)[Exception:0 C:0 D:0]", w.StringAll())

	disposeTest(c, s)
}

// TestNestedMutation
type TestNestedMutationHandlers struct {
	*ExceptionHandler
}

func (h *TestNestedMutationHandlers) AState(e *am.Event) {
	e.Machine.Add1("B", nil)
	e.Machine.Add1("B", nil)
	e.Machine.Add1("B", nil)

	e.Machine.Remove1("B", nil)
	e.Machine.Remove1("B", nil)
}

func TestNestedMutation(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRels(t, nil)
	// bind handlers
	err := mach.BindHandlers(&TestNestedMutationHandlers{})
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil)
	w := c.Worker

	// test
	w.Add1("A", nil)

	// assert
	assertStates(t, w, S{"A"})

	disposeTest(c, s)
}

func TestIsClock(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRels(t, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// test
	cA := w.Clock(S{"A"})
	cAll := w.Clock(nil)
	w.Add(S{"A", "B"}, nil)

	// assert
	assert.False(t, w.IsClock(cAll))
	assert.False(t, w.IsClock(cA))

	disposeTest(c, s)
}

func TestIsTime(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRels(t, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil)
	w := c.Worker

	// test
	tA := w.Time(S{"A"})
	tAll := w.Time(nil)
	w.Add(S{"A", "B"}, nil)

	// assert
	assert.False(t, w.IsTime(tA, S{"A"}))
	assert.False(t, w.IsTime(tAll, nil))

	disposeTest(c, s)
}

// TODO
// func TestExport(t *testing.T) {
//	t.Parallel()

// init
//	m1 := NewNoRels(t, S{"A"})
//	defer m1.Dispose()
//
//	// change clocks
//	m1.Remove1("B", nil)
//	m1.Add1("A", nil)
//	m1.Add1("B", nil)
//	m1.Add1("C", nil)
//	m1Str := m1.Encode()
//
//	// export
//	jsonPath := path.Join(os.TempDir(), "am-TestExportImport.json")
//	err := m1.Export()
// }
