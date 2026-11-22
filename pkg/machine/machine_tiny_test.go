//go:build tinygo

package machine

import (
	"context"
	"errors"
	"testing"
	"time"
)

const isTinyGo = true

func newTinyGoMach(t *testing.T, schema Schema) *Machine {
	t.Helper()

	m := New(context.Background(), schema, nil)
	t.Cleanup(func() {
		m.Dispose()
		<-m.WhenDisposed()
	})

	return m
}

func TestTinyGoMutations(t *testing.T) {
	m := newTinyGoMach(t, Schema{
		"A": {},
		"B": {},
		"C": {},
	})

	if m.Add1("A", nil) != Executed {
		t.Fatal("Add1(A) should execute")
	}
	if !m.Is1("A") || m.Not1("A") {
		t.Fatal("A should be active")
	}

	m.Add1("B", nil)
	if m.Set(S{"C"}, nil) != Executed {
		t.Fatal("Set(C) should execute")
	}
	if !m.Is1("C") || !m.Not1("A") || !m.Not1("B") {
		t.Fatal("only C should be active")
	}

	m.Remove1("C", nil)
	if !m.Not1("C") {
		t.Fatal("C should be inactive")
	}
}

func TestTinyGoHandlersBindMaps(t *testing.T) {
	m := newTinyGoMach(t, Schema{
		"A": {},
		"B": {},
	})

	var neg, fin int
	_, err := m.HandlersBindMaps(
		map[string]HandlerNegotiation{
			"AEnter": func(e *Event) bool {
				neg++

				return true
			},
		},
		map[string]HandlerFinal{
			"AState": func(e *Event) {
				fin++
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if m.Add1("A", nil) != Executed {
		t.Fatal("Add1(A) should execute")
	}
	if neg != 1 {
		t.Fatalf("expected 1 negotiation call, got %d", neg)
	}
	if fin != 1 {
		t.Fatalf("expected 1 final call, got %d", fin)
	}
	if !m.Is1("A") {
		t.Fatal("A should be active")
	}
}

func TestTinyGoNegotiationCancel(t *testing.T) {
	m := newTinyGoMach(t, Schema{
		"A": {},
	})

	_, err := m.HandlersBindMaps(
		map[string]HandlerNegotiation{
			"AEnter": func(e *Event) bool {
				return false
			},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if m.Add1("A", nil) != Canceled {
		t.Fatal("Add1(A) should be canceled")
	}
	if !m.Not1("A") {
		t.Fatal("A should be inactive")
	}
}

func TestTinyGoWhen(t *testing.T) {
	m := newTinyGoMach(t, Schema{
		"A": {},
	})

	ch := m.When1("A", nil)
	go func() {
		time.Sleep(10 * time.Millisecond)
		m.Add1("A", nil)
	}()

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("When1 timed out")
	}

	ch = m.WhenNot1("A", nil)
	m.Remove1("A", nil)

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("WhenNot1 timed out")
	}
}

func TestTinyGoGroups(t *testing.T) {
	m := newTinyGoMach(t, Schema{
		"A": {},
		"B": {},
		"C": {},
	})

	m.SetGroupsString(map[string]S{
		"Group0": {"A", "B"},
		"Group1": {"C"},
	}, []string{"Group0", "Group1"})

	groups, order := m.Groups()
	if len(order) != 2 || order[0] != "Group0" || order[1] != "Group1" {
		t.Fatalf("unexpected group order: %v", order)
	}
	if got := groups["Group0"]; len(got) != 2 ||
		got[0] != m.Index1("A") || got[1] != m.Index1("B") {
		t.Fatalf("unexpected Group0: %v", got)
	}
	if got := groups["Group1"]; len(got) != 1 || got[0] != m.Index1("C") {
		t.Fatalf("unexpected Group1: %v", got)
	}
}

func TestTinyGoVerifyStates(t *testing.T) {
	m := newTinyGoMach(t, Schema{
		"A": {},
		"B": {},
	})

	names := m.StateNames()
	if err := m.VerifyStates(names); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyStates(S{names[0]}); err == nil {
		t.Fatal("expected a state count error")
	}
}

func TestTinyGoMulti(t *testing.T) {
	m := newTinyGoMach(t, Schema{
		"A": {Multi: true},
	})

	var fin int
	_, err := m.HandlersBindMaps(nil,
		map[string]HandlerFinal{
			"AState": func(e *Event) {
				fin++
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	m.Add1("A", nil)
	m.Add1("A", nil)

	if fin != 2 {
		t.Fatalf("expected 2 final calls, got %d", fin)
	}
	if tick := m.Tick("A"); tick != 3 {
		t.Fatalf("expected tick 3, got %d", tick)
	}
}

func TestTinyGoErrors(t *testing.T) {
	m := newTinyGoMach(t, Schema{
		"ErrTest": {Require: S{StateException}},
	})

	m.AddErrState("ErrTest", errors.New("test error"), nil)
	if !m.Is1(StateException) {
		t.Fatal("Exception should be active")
	}
	if m.Err() == nil {
		t.Fatal("Err() should not be nil")
	}
}

func TestTinyGoTracers(t *testing.T) {
	m := newTinyGoMach(t, Schema{
		"A": {},
	})

	tracer := &TracerNoOp{Id: "tinygo-tracer"}
	id, err := m.TracerBind(tracer)
	if err != nil {
		t.Fatal(err)
	}
	if id != "tinygo-tracer" {
		t.Fatalf("unexpected tracer id: %s", id)
	}
	if err := m.TracerDetach(id); err != nil {
		t.Fatal(err)
	}
}

func TestTinyGoSchemaParse(t *testing.T) {
	_, err := (Schema{
		"A": {
			Require: S{"B"},
			Remove:  S{"B"},
		},
		"B": {},
	}).Parse()
	if err == nil {
		t.Fatal("expected a require-remove conflict error")
	}

	_, err = (Schema{
		"A": {},
		"B": {Require: S{"A"}},
	}).Parse()
	if err != nil {
		t.Fatal(err)
	}
}

func TestTinyGoDispose(t *testing.T) {
	m := New(context.Background(), Schema{"A": {}}, nil)
	m.Dispose()

	select {
	case <-m.WhenDisposed():
	case <-time.After(2 * time.Second):
		t.Fatal("WhenDisposed timed out")
	}
}

func assertFuncName(t *testing.T, fn func()) {
	t.Helper()

	// funcName is a no-op in tinygo
	assert.Empty(t, funcName(fn))
}
