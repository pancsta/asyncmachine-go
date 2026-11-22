package nofmt

import (
	"errors"
	"fmt"
	"testing"
)

type testStringer struct{ v string }

func (s testStringer) String() string { return s.v }

type testStruct struct {
	A int
	B string
}

type testPrivateStruct struct {
	a int
	b string
}

type testTypedError struct{ code int }

func (e *testTypedError) Error() string { return "typed" }

func TestSprintfParity(t *testing.T) {
	cases := []struct {
		format string
		args   []any
	}{
		{"plain", nil},
		{"%s", []any{"hello"}},
		{"%s:%d", []any{"a", 5}},
		{"%d", []any{int64(-3)}},
		{"%d", []any{uint64(7)}},
		{"%v", []any{nil}},
		{"%v", []any{true}},
		{"%v", []any{float64(1.5)}},
		{"%v", []any{float32(1.5)}},
		{"%v", []any{complex(1.0, 2.0)}},
		{"%v", []any{[]int{1, 2}}},
		{"%v", []any{[]bool{true, false}}},
		{"%v", []any{[]uint64{1, 2}}},
		{"%v", []any{[]byte("hi")}},
		{"%s", []any{[]byte("hi")}},
		{"%v", []any{[]rune("hi")}},
		{"%s", []any{[]string{"a", "b"}}},
		{"%s", []any{[]string(nil)}},
		{"%d", []any{[]int{1, 2}}},
		{"%d", []any{[]int64{1, 2}}},
		{"%d", []any{[]uint64{1, 2}}},
		{"%d", []any{[]int{}}},
		{"%v", []any{map[string]uint64{"b": 2, "a": 1}}},
		{"%v", []any{map[int]string{2: "b", 10: "j", 1: "a"}}},
		{"%v", []any{map[string]uint64{}}},
		{"%v", []any{testStruct{1, "x"}}},
		{"%v", []any{testPrivateStruct{1, "x"}}},
		{"%+v", []any{testPrivateStruct{1, "x"}}},
		{"%v", []any{&[]int{1, 2}}},
		{"%v", []any{&map[string]uint64{"a": 1}}},
		{"%+v", []any{testStruct{1, "x"}}},
		{"%v", []any{&testStruct{1, "x"}}},
		{"%+v", []any{&testStruct{1, "x"}}},
		{"%v", []any{testStringer{"s"}}},
		{"%s", []any{testStringer{"s"}}},
		{"%v", []any{errors.New("e")}},
		{"%s", []any{errors.New("e")}},
		{"%-15s", []any{"x"}},
		{"%%", nil},
		{"%s %s\n", []any{"a", "b"}},
	}

	for _, c := range cases {
		want := fmt.Sprintf(c.format, c.args...)
		got := Sprintf(c.format, c.args...)
		if got != want {
			t.Errorf("Sprintf(%q, %v) = %q, want %q",
				c.format, c.args, got, want)
		}
	}
}

func TestErrorfParity(t *testing.T) {
	errA := errors.New("a")
	errB := errors.New("b")
	errTyped := &testTypedError{code: 42}

	cases := []struct {
		format string
		args   []any
	}{
		{"plain", nil},
		{"%s", []any{"x"}},
		{"%d", []any{42}},
		{"%v", []any{nil}},
		{"%v", []any{errA}},
		{"%w", []any{errA}},
		{"%w", []any{nil}},
		{"%w", []any{"not-an-error"}},
		{"%w: %s", []any{errA, "x"}},
		{"%s: %w", []any{"x", errA}},
		{"%w: %w: %s", []any{errA, errB, "x"}},
		{"%w", []any{errTyped}},
	}

	for _, c := range cases {
		want := fmt.Errorf(c.format, c.args...)
		got := Errorf(c.format, c.args...)
		if got.Error() != want.Error() {
			t.Errorf("Errorf(%q, %v) = %q, want %q",
				c.format, c.args, got.Error(), want.Error())
		}
		for _, arg := range c.args {
			err, ok := arg.(error)
			if !ok || err == nil {
				continue
			}
			if errors.Is(got, err) != errors.Is(want, err) {
				t.Errorf("errors.Is mismatch for %q: got %v, want %v",
					c.format, errors.Is(got, err), errors.Is(want, err))
			}
		}
		var gotTyped, wantTyped *testTypedError
		if errors.As(got, &gotTyped) != errors.As(want, &wantTyped) {
			t.Errorf("errors.As mismatch for %q", c.format)
		}
	}
}

func TestErrorfUnwrap(t *testing.T) {
	errA := errors.New("a")
	errB := errors.New("b")

	singleWant := fmt.Errorf("%w", errA)
	singleGot := Errorf("%w", errA)
	if errors.Unwrap(singleGot) != errors.Unwrap(singleWant) {
		t.Errorf("single Unwrap = %v, want %v",
			errors.Unwrap(singleGot), errors.Unwrap(singleWant))
	}

	multiWant := fmt.Errorf("%w: %w", errA, errB)
	multiGot := Errorf("%w: %w", errA, errB)
	if errors.Unwrap(multiGot) != errors.Unwrap(multiWant) {
		t.Errorf("multi Unwrap = %v, want %v",
			errors.Unwrap(multiGot), errors.Unwrap(multiWant))
	}
	if !errors.Is(multiGot, errA) || !errors.Is(multiGot, errB) {
		t.Error("multi-wrap errors.Is did not find both errors")
	}
}

func TestErrorfNil(t *testing.T) {
	if got := Errorf("%v", nil).Error(); got != "<nil>" {
		t.Errorf("Errorf(\"%%v\", nil) = %q, want %q", got, "<nil>")
	}
}

func TestSprintParity(t *testing.T) {
	cases := [][]any{
		{"a", 1, true, nil},
		{testStringer{"s"}},
		{[]int{1, 2}},
	}
	for _, args := range cases {
		want := fmt.Sprint(args...)
		got := Sprint(args...)
		if got != want {
			t.Errorf("Sprint(%v) = %q, want %q", args, got, want)
		}
	}
}
