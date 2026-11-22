package machine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// testSignalContext returns a cancellable context for tests. TinyGo doesn't
// provide signal.NotifyContext, so use the portable version for both builds.
func testSignalContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

type History struct {
	Order   []string
	Counter map[string]int
}

// Global

func (h *History) AnyEnter(e *Event) {
	h.event("AnyEnter")
}

func (h *History) AnyState(e *Event) {
	h.event("AnyState")
}

// A

func (h *History) AD(e *Event) {
	h.event("AD")
}

func (h *History) AC(e *Event) {
	h.event("AC")
}

func (h *History) AExit(e *Event) {
	h.event("AExit")
}

func (h *History) AEnd(e *Event) {
	h.event("AEnd")
}

func (h *History) AA(e *Event) {
	h.event("AA")
}

func (h *History) AState(e *Event) {
	h.event("AState")
}

// B

func (h *History) AB(e *Event) {
	h.event("AB")
}

func (h *History) BEnter(e *Event) {
	h.event("BEnter")
}

func (h *History) BEnd(e *Event) {
	h.event("BEnd")
}

func (h *History) BState(e *Event) {
	h.event("BState")
}

func (h *History) BB(e *Event) {
	h.event("BB")
}

func (h *History) BExit(e *Event) {
	h.event("BExit")
}

func (h *History) BD(e *Event) {
	h.event("BD")
}

// C

func (h *History) BAny(e *Event) {
	h.event("BAny")
}

func (h *History) BC(e *Event) {
	h.event("BC")
}

func (h *History) CEnter(e *Event) {
	h.event("CEnter")
}

func (h *History) CState(e *Event) {
	h.event("CState")
}

// D

func (h *History) DEnter(e *Event) {
	h.event("DEnter")
}

func (h *History) DState(e *Event) {
	h.event("DState")
}

func (h *History) event(name string) {
	h.Order = append(h.Order, name)

	if _, ok := h.Counter[name]; !ok {
		h.Counter[name] = 0
	}

	h.Counter[name]++
}

func trackTransitions(mach *Machine) *History {
	history := &History{
		Order:   []string{},
		Counter: map[string]int{},
	}
	_, err := bindHandlerMaps(mach, history)
	if err != nil {
		panic(err)
	}

	return history
}

func assertStates(t *testing.T, m *Machine, expected S,
	msgAndArgs ...interface{},
) {
	require.ElementsMatch(t, expected, m.activeStates, msgAndArgs...)
}

func assertTime(t *testing.T, m *Machine, states S, times Time,
	msgAndArgs ...interface{},
) {
	require.Equal(t, m.Time(states), times, msgAndArgs...)
}

func assertNoException(t *testing.T, m *Machine) {
	require.False(t, m.Is1(StateException), "Exception state active")
}

func assertEventCounts(t *testing.T, history *History, expected int) {
	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, expected, count)
	}
}

func assertOrder(t *testing.T, history *History, handlers []string) {
	for i, name := range handlers {
		if i == len(handlers)-1 {
			break
		}
		next := handlers[i+1]
		idx := slices.Index(history.Order, name)
		nextIdx := slices.Index(history.Order, next)

		require.Greater(t, nextIdx, idx, "expected %s before %s", name, next)
	}
}

func assertEventCountsMin(t *testing.T, history *History, expected int) {
	// assert event counts
	for event, count := range history.Counter {
		assert.GreaterOrEqual(t, count, expected, "event %s call count", event)
	}
}

func captureLog(t *testing.T, m *Machine, log *string) {
	mx := sync.Mutex{}
	m.SemLogger().SetLevel(LogEverything)
	m.SemLogger().SetLogger(func(i LogLevel, msg string, args ...any) {
		if m.IsDisposed() {
			return
		}
		if os.Getenv(EnvAmDebug) != "" {
			t.Logf(msg+"\n", args...)
		}
		mx.Lock()
		defer mx.Unlock()

		*log += sprintf(msg+"\n", args...)
	})
}

func assertString(t *testing.T, m *Machine, expected string, states S) {
	assert.Equal(t,
		strings.Trim(dedent(expected), "\n"),
		strings.Trim(m.Inspect(states), "\n"))
}

// dedent removes the common leading whitespace from all lines, and trims
// whitespace-only lines.
func dedent(s string) string {
	lines := strings.Split(s, "\n")

	// find the longest common leading whitespace
	var margin string
	first := true
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			lines[i] = ""

			continue
		}
		indent := line[:len(line)-len(trimmed)]
		if first {
			margin = indent
			first = false

			continue
		}
		for !strings.HasPrefix(indent, margin) {
			margin = margin[:len(margin)-1]
		}
	}
	if margin == "" {
		return strings.Join(lines, "\n")
	}
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, margin)
	}

	return strings.Join(lines, "\n")
}

func TestEnv(t *testing.T) {
	t.Setenv(EnvAmLog, "1")
	assert.Equal(t, LogExternal, EnvLogLevel(""))
}

func TestAMerge(t *testing.T) {
	if os.Getenv(EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	a1 := A{"a": 1}
	a2 := A{"b": 2}

	m := AMerge(a1, a2)
	assert.Equal(t, 1, m["a"])
	assert.Equal(t, 2, m["b"])
}

func TestStateUtils(t *testing.T) {
	if os.Getenv(EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	s := State{
		Auto:   true,
		Remove: S{"A"},
		Add:    S{"B"},
	}

	add := StateAdd(s, State{
		Multi:  true,
		Remove: S{"C"},
	})
	set := StateSet(s, false, true, State{
		Remove: S{"C"},
	})

	// assert
	assert.Equal(t, S{"A", "C"}, add.Remove)
	assert.Equal(t, S{"C"}, set.Remove)

	assert.True(t, add.Auto)
	assert.False(t, set.Auto)

	assert.False(t, StatesEqual(S{"A"}, S{"B"}))
	assert.False(t, StatesEqual(S{"A"}, S{"A", "B"}))
	assert.True(t, StatesEqual(S{"A"}, S{"A"}))
}

// ///// ///// /////

// ///// CUSTOM ASSERTS

// ///// ///// /////

// Custom assertions for pkg/machine tests, replacing testify. The API
// mimics testify's assert/require, so test bodies stay unchanged and the
// package remains free of external deps (also for tinygo builds).

// assert and require mimic github.com/stretchr/testify assertions.
var (
	assert  = asserts{}
	require = asserts{fatal: true}
)

// asserts implements a subset of testify's assert/require API. [require] uses
// fatal: true, which stops the test on failure.
type asserts struct {
	fatal bool
}

func (a asserts) fail(t *testing.T, ok bool, msg string) {
	t.Helper()

	if ok {
		return
	}
	if a.fatal {
		t.Fatalf("%s", msg)
	}
	t.Errorf("%s", msg)
}

// msgArgs formats optional message arguments the way testify does.
func msgArgs(msgAndArgs ...any) string {
	if len(msgAndArgs) == 0 {
		return ""
	}

	var m string
	if len(msgAndArgs) == 1 {
		if s, ok := msgAndArgs[0].(string); ok {
			m = s
		} else {
			m = fmt.Sprintf("%+v", msgAndArgs[0])
		}
	} else {
		m = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
	}
	if m == "" {
		return ""
	}

	return "\nmessages: " + m
}

// objectsEqual is a port of testify's ObjectsAreEqual.
func objectsEqual(expected, actual any) bool {
	if expected == nil || actual == nil {
		return expected == actual
	}

	exp, ok := expected.([]byte)
	if !ok {
		return reflect.DeepEqual(expected, actual)
	}

	act, ok := actual.([]byte)
	if !ok {
		return false
	}
	if exp == nil || act == nil {
		return exp == nil && act == nil
	}

	return bytes.Equal(exp, act)
}

func isNil(object any) bool {
	if object == nil {
		return true
	}

	value := reflect.ValueOf(object)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice, reflect.UnsafePointer:

		return value.IsNil()
	}

	return false
}

func isEmpty(object any) bool {
	if object == nil {
		return true
	}

	return isEmptyValue(reflect.ValueOf(object))
}

func isEmptyValue(v reflect.Value) bool {
	if v.IsZero() {
		return true
	}
	switch v.Kind() {
	case reflect.Chan, reflect.Map, reflect.Slice:
		return v.Len() == 0
	case reflect.Pointer:
		return isEmptyValue(v.Elem())
	}

	return false
}

// getLen returns the length of an object, or -1 if len() is not supported.
func getLen(object any) int {
	v := reflect.ValueOf(object)
	switch v.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice,
		reflect.String:

		return v.Len()
	}

	return -1
}

func containsElement(list, element any) bool {
	v := reflect.ValueOf(list)
	if !v.IsValid() {
		return false
	}

	switch v.Kind() {
	case reflect.String:
		return strings.Contains(v.String(), fmt.Sprint(element))
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if objectsEqual(k.Interface(), element) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if objectsEqual(v.Index(i).Interface(), element) {
				return true
			}
		}
	}

	return false
}

func matchRegexp(rx, str any) bool {
	var r *regexp.Regexp
	if rr, ok := rx.(*regexp.Regexp); ok {
		r = rr
	} else {
		r = regexp.MustCompile(fmt.Sprint(rx))
	}

	switch v := str.(type) {
	case []byte:
		return r.Match(v)
	case string:
		return r.MatchString(v)
	default:
		return r.MatchString(fmt.Sprint(v))
	}
}

// diffLists returns elements only in A and only in B, counting duplicates.
func diffLists(listA, listB any) (extraA, extraB []any) {
	aValue, bValue := reflect.ValueOf(listA), reflect.ValueOf(listB)
	aLen, bLen := aValue.Len(), bValue.Len()

	// mark indexes in bValue which were already used
	visited := make([]bool, bLen)
	for i := 0; i < aLen; i++ {
		element := aValue.Index(i).Interface()
		found := false
		for j := 0; j < bLen; j++ {
			if visited[j] {
				continue
			}
			if objectsEqual(bValue.Index(j).Interface(), element) {
				visited[j] = true
				found = true
				break
			}
		}
		if !found {
			extraA = append(extraA, element)
		}
	}
	for j := 0; j < bLen; j++ {
		if !visited[j] {
			extraB = append(extraB, bValue.Index(j).Interface())
		}
	}

	return extraA, extraB
}

// compare returns 1 if e1 > e2, -1 if e1 < e2, and 0 if equal.
func compare(e1, e2 any, kind reflect.Kind) (int, bool) {
	v1, v2 := reflect.ValueOf(e1), reflect.ValueOf(e2)
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64:

		return cmpOrdered(v1.Int(), v2.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64:

		return cmpOrdered(v1.Uint(), v2.Uint()), true
	case reflect.Float32, reflect.Float64:
		return cmpOrdered(v1.Float(), v2.Float()), true
	case reflect.String:
		return strings.Compare(v1.String(), v2.String()), true
	}

	return 0, false
}

func cmpOrdered[T int64 | uint64 | float64](a, b T) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	}

	return 0
}

func (a asserts) Equal(
	t *testing.T, expected, actual any, msgAndArgs ...any,
) {
	t.Helper()

	a.fail(t, objectsEqual(expected, actual), fmt.Sprintf(
		"not equal:\nexpected: %#v\nactual  : %#v%s",
		expected, actual, msgArgs(msgAndArgs...)))
}

func (a asserts) NotEqual(
	t *testing.T, expected, actual any, msgAndArgs ...any,
) {
	t.Helper()

	a.fail(t, !objectsEqual(expected, actual), fmt.Sprintf(
		"should not be equal to %#v%s", actual, msgArgs(msgAndArgs...)))
}

func (a asserts) True(t *testing.T, value bool, msgAndArgs ...any) {
	t.Helper()

	a.fail(t, value, "should be true"+msgArgs(msgAndArgs...))
}

func (a asserts) False(t *testing.T, value bool, msgAndArgs ...any) {
	t.Helper()

	a.fail(t, !value, "should be false"+msgArgs(msgAndArgs...))
}

func (a asserts) Nil(t *testing.T, object any, msgAndArgs ...any) {
	t.Helper()

	a.fail(t, isNil(object), fmt.Sprintf(
		"expected nil, got %#v%s", object, msgArgs(msgAndArgs...)))
}

func (a asserts) NotNil(t *testing.T, object any, msgAndArgs ...any) {
	t.Helper()

	a.fail(t, !isNil(object), "expected not nil"+msgArgs(msgAndArgs...))
}

func (a asserts) Len(
	t *testing.T, object any, length int, msgAndArgs ...any,
) {
	t.Helper()

	l := getLen(object)
	a.fail(t, l == length, fmt.Sprintf(
		"%#v should have %d item(s), but has %d%s",
		object, length, l, msgArgs(msgAndArgs...)))
}

func (a asserts) Empty(t *testing.T, object any, msgAndArgs ...any) {
	t.Helper()

	a.fail(t, isEmpty(object), fmt.Sprintf(
		"should be empty, but was %#v%s", object, msgArgs(msgAndArgs...)))
}

func (a asserts) Contains(
	t *testing.T, s, contains any, msgAndArgs ...any,
) {
	t.Helper()

	a.fail(t, containsElement(s, contains), fmt.Sprintf(
		"%#v does not contain %#v%s", s, contains, msgArgs(msgAndArgs...)))
}

func (a asserts) Regexp(t *testing.T, rx, str any, msgAndArgs ...any) {
	t.Helper()

	a.fail(t, matchRegexp(rx, str), fmt.Sprintf(
		"expect %q to match %q%s", str, rx, msgArgs(msgAndArgs...)))
}

func (a asserts) Greater(t *testing.T, e1, e2 any, msgAndArgs ...any) {
	t.Helper()

	a.compare(t, e1, e2, 1, false, msgAndArgs...)
}

func (a asserts) GreaterOrEqual(
	t *testing.T, e1, e2 any, msgAndArgs ...any,
) {
	t.Helper()

	a.compare(t, e1, e2, 0, true, msgAndArgs...)
}

func (a asserts) Less(t *testing.T, e1, e2 any, msgAndArgs ...any) {
	t.Helper()

	a.compare(t, e1, e2, -1, false, msgAndArgs...)
}

// compare checks if e1 is greater or equal (orGreater) to e2 (want: 1 or 0),
// or less than e2 (want: -1).
func (a asserts) compare(
	t *testing.T, e1, e2 any, want int, orGreater bool,
	msgAndArgs ...any,
) {
	t.Helper()

	kind := reflect.ValueOf(e1).Kind()
	if kind != reflect.ValueOf(e2).Kind() {
		a.fail(t, false,
			"elements should be the same type"+msgArgs(msgAndArgs...))

		return
	}
	got, ok := compare(e1, e2, kind)
	ok = ok && (got == want || (orGreater && got > want))
	a.fail(t, ok, fmt.Sprintf(
		"expected comparison result %d, got %#v vs %#v%s",
		want, e1, e2, msgArgs(msgAndArgs...)))
}

func (a asserts) ElementsMatch(
	t *testing.T, listA, listB any, msgAndArgs ...any,
) {
	t.Helper()

	if isEmpty(listA) && isEmpty(listB) {
		return
	}
	extraA, extraB := diffLists(listA, listB)
	a.fail(t, len(extraA) == 0 && len(extraB) == 0, fmt.Sprintf(
		"elements do not match:\nextra in A: %#v\nextra in B: %#v%s",
		extraA, extraB, msgArgs(msgAndArgs...)))
}

func (a asserts) Same(
	t *testing.T, expected, actual any, msgAndArgs ...any,
) {
	t.Helper()

	e, av := reflect.ValueOf(expected), reflect.ValueOf(actual)
	ok := e.IsValid() && av.IsValid() &&
		e.Kind() == reflect.Pointer && av.Kind() == reflect.Pointer &&
		e.Type() == av.Type() && e.Pointer() == av.Pointer()
	a.fail(t, ok, fmt.Sprintf("%#v is not the same pointer as %#v%s",
		expected, actual, msgArgs(msgAndArgs...)))
}

func (a asserts) Panics(t *testing.T, f func(), msgAndArgs ...any) {
	t.Helper()

	panicked := func() (panicked bool) {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		f()

		return
	}()
	a.fail(t, panicked, "func should panic"+msgArgs(msgAndArgs...))
}

func (a asserts) Fail(t *testing.T, msgAndArgs ...any) {
	t.Helper()

	a.fail(t, false, "failed"+msgArgs(msgAndArgs...))
}

func (a asserts) Error(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()

	a.fail(t, err != nil, fmt.Sprintf(
		"an error is expected, but got nil%s", msgArgs(msgAndArgs...)))
}

func (a asserts) NoError(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()

	a.fail(t, err == nil, fmt.Sprintf(
		"unexpected error: %v%s", err, msgArgs(msgAndArgs...)))
}

func (a asserts) NoErrorf(
	t *testing.T, err error, format string, args ...any,
) {
	t.Helper()

	a.fail(t, err == nil, fmt.Sprintf("unexpected error: %v\nmessages: %s",
		err, fmt.Sprintf(format, args...)))
}

func (a asserts) ErrorIs(
	t *testing.T, err, target error, msgAndArgs ...any,
) {
	t.Helper()

	a.fail(t, errors.Is(err, target), fmt.Sprintf(
		"error %v is not %v%s", err, target, msgArgs(msgAndArgs...)))
}

func (a asserts) ErrorContains(
	t *testing.T, err error, contains string, msgAndArgs ...any,
) {
	t.Helper()

	ok := err != nil && strings.Contains(err.Error(), contains)
	a.fail(t, ok, fmt.Sprintf(
		"error %v does not contain %q%s", err, contains,
		msgArgs(msgAndArgs...)))
}

func (a asserts) EqualError(
	t *testing.T, err error, errString string, msgAndArgs ...any,
) {
	t.Helper()

	ok := err != nil && err.Error() == errString
	a.fail(t, ok, fmt.Sprintf(
		"error message not equal:\nexpected: %q\nactual  : %v%s",
		errString, err, msgArgs(msgAndArgs...)))
}
