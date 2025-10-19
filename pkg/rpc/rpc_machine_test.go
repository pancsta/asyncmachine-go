// TODO handle-bound tests

package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sst "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type S = am.S

type (
	Schema = am.Schema
)

func TestSingleStateActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRelsRpcWorker(t, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Add1("A", nil)

	// assert
	assertStates(t, c.Worker, S{"A"})

	disposeTest(t, c, s, true)
}

func TestMultipleStatesActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRelsRpcWorker(t, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Add(S{"A"}, nil)
	w.Add(S{"B"}, nil)

	// assert
	assertStates(t, c.Worker, S{"A", "B"})

	disposeTest(t, c, s, true)
}

func TestExposeAllStateNames(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRelsRpcWorker(t, S{"A"})
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// assert
	assert.Subset(t, w.StateNames(), S{"A", "B", "C", "D"})

	disposeTest(t, c, s, true)
}

func TestStateSet(t *testing.T) {
	t.Parallel()
	// amhelp.EnableDebugging(true)

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRelsRpcWorker(t, S{"A"})
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Set(S{"B"}, nil)

	// assert
	assertStates(t, w, S{"B"})

	disposeTest(t, c, s, true)
}

func TestStateAdd(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRelsRpcWorker(t, S{"A"})
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Add(S{"B"}, nil)

	// assert
	assertStates(t, w, S{"A", "B"})

	disposeTest(t, c, s, true)
}

func TestStateRemove(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRelsRpcWorker(t, S{"B", "C"})
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)

	// test
	c.Worker.Remove(S{"C"}, nil)
	w := c.Worker

	// assert
	assertStates(t, w, S{"B"})

	disposeTest(t, c, s, true)
}

func TestRemoveRelation(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// relations
	m := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {},
		sst.C: {Remove: S{sst.D}},
		sst.D: {},
	})
	m.Add1(sst.D, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// C deactivates D
	w.Add(S{"C"}, nil)
	assertStates(t, w, S{"C"})

	disposeTest(t, c, s, true)
}

func TestRemoveRelationSimultaneous(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {},
		sst.C: {Remove: S{sst.D}},
		sst.D: {},
	})
	m.Add1(sst.D, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// test
	r := w.Set(S{"C", "D"}, nil)

	// assert
	assert.Equal(t, am.Canceled, r)
	assertStates(t, w, S{"D"})

	disposeTest(t, c, s, true)
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

			m := utils.NewCustomRpcWorker(t, am.Schema{
				sst.A: {},
				sst.B: {},
				sst.C: {Remove: S{sst.D}},
				sst.D: {Remove: S{sst.C}},
			})
			m.Add1(sst.D, nil)
			_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
			w := c.Worker

			// test
			test.fn(t, w)

			// dispose
			disposeTest(t, c, s, true)
		})
	}
}

func disposeTest(t *testing.T, c *Client, s *Server, checkErrs bool) {
	if checkErrs {
		amhelpt.AssertNoErrEver(t, c.Mach)
		amhelpt.AssertNoErrEver(t, s.Mach)
	}
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
	m := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {Remove: S{sst.D}},
		sst.B: {},
		sst.C: {Add: S{sst.D}},
		sst.D: {},
	})

	// worker
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Set(S{"C"}, nil)

	// assert
	assertStates(t, w, S{"C", "D"}, "state should be activated")
	w.Set(S{"A", "C"}, nil)
	assertStates(t, w, S{"A", "C"}, "state D should be skipped if "+
		"blocked at the same time")

	disposeTest(t, c, s, true)
}

func TestRequireRelation(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {Require: S{sst.D}},
		sst.B: {},
		sst.C: {},
		sst.D: {},
	})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Set(S{"C", "D"}, nil)

	// assert
	assertStates(t, w, S{"C", "D"})

	disposeTest(t, c, s, true)
}

func TestRequireRelationWhenRequiredIsntActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {},
		sst.C: {Require: S{sst.D}},
		sst.D: {},
	})
	mach.Add1(sst.A, nil)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Set(S{"C", "A"}, nil)

	// assert
	assertStates(t, w, S{"A"}, "target state shouldnt be activated")

	disposeTest(t, c, s, true)
}

func TestAutoStates(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {
			Auto:    true,
			Require: S{sst.A},
		},
		sst.C: {Require: S{sst.D}},
		sst.D: {},
	})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	result := w.Add(S{"A"}, nil)

	// assert
	assert.Equal(t, am.Executed, result, "transition should be executed")
	assertStates(t, w, S{"A", "B"}, "dependant auto state should be set")

	disposeTest(t, c, s, true)
}

func TestSwitch(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {},
		sst.C: {Require: S{sst.D}},
		sst.D: {},
	})
	mach.Add1(sst.A, nil)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	caseA := false
	switch w.Switch(S{"A", "B"}) {
	case "A":
		caseA = true
	case "B":
	}
	assert.Equal(t, true, caseA)

	caseDef := false
	switch w.Switch(S{"C", "B"}) {
	case "B":
	default:
		caseDef = true
	}
	assert.Equal(t, true, caseDef)

	disposeTest(t, c, s, true)
}

func TestRegressionRemoveCrossBlockedByImplied(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, Schema{
		"A": {Remove: S{"B"}},
		"B": {Remove: S{"A"}},
		"Z": {Add: S{"B"}},
	})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Set(S{"Z"}, nil)

	// assert
	assertStates(t, w, S{"Z", "B"})

	disposeTest(t, c, s, true)
}

func TestRegressionImpliedBlockByBeingRemoved(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, Schema{
		"Wet":   {Require: S{"Water"}},
		"Dry":   {Remove: S{"Wet"}},
		"Water": {Add: S{"Wet"}, Remove: S{"Dry"}},
	})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Set(S{"Dry"}, nil)
	w.Set(S{"Water"}, nil)

	// assert
	assertStates(t, w, S{"Water", "Wet"})

	disposeTest(t, c, s, true)
}

func TestWhen2(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRelsRpcWorker(t, nil)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
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

	disposeTest(t, c, s, true)
}

func TestWhenActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRelsRpcWorker(t, S{"A"})
	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	<-w.When(S{"A"}, nil)

	// assert
	assertStates(t, w, S{"A"})

	disposeTest(t, c, s, true)
}

func TestWhenNot2(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRelsRpcWorker(t, S{"A", "B"})
	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
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

	disposeTest(t, c, s, true)
}

func TestWhenNotActive(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRelsRpcWorker(t, S{"A"})
	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	<-w.WhenNot(S{"B"}, nil)

	// assert
	assertStates(t, w, S{"A"})

	disposeTest(t, c, s, true)
}

func TestPartialAuto(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {},
		sst.C: {
			Auto:    true,
			Require: S{sst.B},
		},
		sst.D: {
			Auto:    true,
			Require: S{sst.B},
		},
	})
	mach.Add1(sst.A, nil)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Add(S{"A"}, nil)

	// assert
	assertStates(t, w, S{"A"})

	disposeTest(t, c, s, true)
}

func TestTime(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {Multi: true},
		sst.C: {},
		sst.D: {},
	})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
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

	disposeTest(t, c, s, true)
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
//	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
//	w := c.Worker
//	disposeTest(t, c, s, true)
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
//	disposeTest(t, c, s, true)
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
	mach := utils.NewNoRelsRpcWorker(t, S{"A", "B"})
	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
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

	disposeTest(t, c, s, true)
}

func TestIs(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {
			Auto:    true,
			Require: S{sst.A},
		},
		sst.C: {},
		sst.D: {},
	})
	mach.Add(S{sst.A, sst.B}, nil)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	assert.True(t, w.Is(S{"A", "B"}), "A B should be active")
	assert.False(t, w.Is(S{"A", "B", "C"}), "A B C shouldnt be active")

	disposeTest(t, c, s, true)
}

func TestNot(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {
			Auto:    true,
			Require: S{sst.A},
		},
		sst.C: {},
		sst.D: {},
	})
	mach.Add(S{sst.A, sst.B}, nil)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	assert.False(t, w.Not(S{"A", "B"}), "A B should be active")
	assert.False(t, w.Not(S{"A", "B", "C"}), "A B C is partially active")
	assert.True(t, w.Not1("D"), "D is inactive")

	disposeTest(t, c, s, true)
}

func TestAny(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {
			Auto:    true,
			Require: S{sst.A},
		},
		sst.C: {},
		sst.D: {},
	})
	mach.Add(S{sst.A, sst.B}, nil)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	assert.True(t, w.Any(S{"A", "B"}, S{"C"}), "A B should be active")
	assert.True(t, w.Any(S{"A", "B", "C"}, S{"A"}),
		"A B C is partially active")

	disposeTest(t, c, s, true)
}

func TestClock(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewCustomRpcWorker(t, am.Schema{
		sst.A: {},
		sst.B: {Multi: true},
		sst.C: {},
		sst.D: {},
	})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
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

	assert.Subset(t, w.Clock(nil), am.Clock{
		"A": 3, "B": 7, "C": 4, "D": 1, "Exception": 0,
	})

	assert.Equal(t, am.Clock{
		"A": 3, "B": 7,
	}, w.Clock(S{"A", "B"}))

	assert.Equal(t, uint64(3), w.Tick("A"))

	disposeTest(t, c, s, true)
}

func TestInspect(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewRelsRpcWorker(t, S{"A", "C"})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

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
	assertString(t, w, expected, names)
	// (A:1 C:1)[B:0 D:0 Exception:0]
	w.Remove(S{"C"}, nil)
	// ()[A:2 B:0 C:2 D:0 Exception:0]
	w.Add(S{"B"}, nil)
	// (A:3 B:1 C:3)[D:0 Exception:0]
	w.Add(S{"D"}, nil)
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
		0 ErrProviding
		    |Time     0
		    |Require  Exception
		0 ErrSendPayload
		    |Time     0
		    |Require  Exception
		0 SendPayload
		    |Time     0
		    |Multi    true
	`
	assertString(t, w, expected, nil)

	disposeTest(t, c, s, true)
}

func TestString(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRelsRpcWorker(t, S{"A", "B"})

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	assert.Equal(t, "(A:1 B:1)", w.String())
	assert.Equal(t, "(A:1 B:1) [Exception:0 C:0 D:0 ErrProviding:0"+
		" ErrSendPayload:0 SendPayload:0]",
		w.StringAll())

	disposeTest(t, c, s, true)
}

// TestNestedMutation
type TestNestedMutationHandlers struct {
	*ExceptionHandler
}

func (h *TestNestedMutationHandlers) AState(e *am.Event) {
	e.Machine().Add1("B", nil)
	e.Machine().Add1("B", nil)
	e.Machine().Add1("B", nil)

	e.Machine().Remove1("B", nil)
	e.Machine().Remove1("B", nil)
}

func TestNestedMutation(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRelsRpcWorker(t, nil)
	// bind handlers
	err := mach.BindHandlers(&TestNestedMutationHandlers{})
	assert.NoError(t, err)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	w.Add1("A", nil)

	// assert
	assertStates(t, w, S{"A"})

	disposeTest(t, c, s, true)
}

func TestIsClock(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRelsRpcWorker(t, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// test
	cA := w.Clock(S{"A"})
	cAll := w.Clock(nil)
	w.Add(S{"A", "B"}, nil)

	// assert
	assert.False(t, w.IsClock(cAll))
	assert.False(t, w.IsClock(cA))

	disposeTest(t, c, s, true)
}

func TestIsTime(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := utils.NewNoRelsRpcWorker(t, nil)
	_, _, s, c := NewTest(t, ctx, m, nil, nil, nil, false)
	w := c.Worker

	// test
	tA := w.Time(S{"A"})
	tAll := w.Time(nil)
	w.Add(S{"A", "B"}, nil)

	// assert
	assert.False(t, w.IsTime(tA, S{"A"}))
	assert.False(t, w.IsTime(tAll, nil))

	disposeTest(t, c, s, true)
}

// TODO
// func TestExport(t *testing.T) {
//	t.Parallel()

// init
//	m1 := NewNoRels(t, S{"A"})
//	defer m1.Dispose()
//
//	// change clocks
//	m1.Add1("A", nil)
//	m1.Add1("B", nil)
//	m1.Add1("C", nil)
//	m1Str := m1.Encode()
//
//	// export
//	jsonPath := path.Join(os.Te1mpDir(), "am-TestExportImport.json")
//	err := m1.Export()
// }

// TestWhenQueue
type TestWhenQueueTracer struct {
	*am.NoOpTracer
	done chan struct{}
	t    *testing.T
}

func (tr *TestWhenQueueTracer) TransitionEnd(tx *am.Transition) {
	m := tx.Api
	// only when setting A
	if tx.TimeBefore.Is1(m.Index1("A")) {
		return
	}

	m.Add1("B", nil)
	m.Add1("C", nil)
	// TODO pause queue in the source machine's handler
	// res := m.Add1("D", nil)
	res := am.Result(5)
	m.Add1("D", nil)
	tr.done = make(chan struct{})

	go func() {
		<-m.WhenQueue(res)
		assertStates(tr.t, m, S{"A", "B", "C", "D"})
		close(tr.done)
	}()
}

// TODO bind via handlers
func TestWhenQueue(t *testing.T) {
	t.Parallel()

	// init
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// machine
	mach := utils.NewNoRelsRpcWorker(t, nil)

	// worker
	_, _, s, c := NewTest(t, ctx, mach, nil, nil, nil, false)
	w := c.Worker

	// test
	tr := &TestWhenQueueTracer{
		t: t,
	}
	require.NoError(t, w.BindTracer(tr))

	w.Add1("A", nil)
	// TODO weird close-doesnt-unblock bug in go1.25
	select {
	case <-tr.done:
	case <-time.After(time.Second):
		<-tr.done
	}

	// dispose
	disposeTest(t, c, s, true)
}
