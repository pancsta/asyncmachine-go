package machine

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"
)

// TODO bind via Tracer, not handlers
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

// TODO bind via Tracer, not handlers
func trackTransitions(mach *Machine) *History {
	history := &History{
		Order:   []string{},
		Counter: map[string]int{},
	}
	err := mach.BindHandlers(history)
	if err != nil {
		panic(err)
	}

	return history
}

func assertStates(t *testing.T, m *Machine, expected S,
	msgAndArgs ...interface{},
) {
	assert.ElementsMatch(t, expected, m.activeStates, msgAndArgs...)
}

func assertTime(t *testing.T, m *Machine, states S, times Time,
	msgAndArgs ...interface{},
) {
	assert.Equal(t, m.Time(states), times, msgAndArgs...)
}

func assertNoException(t *testing.T, m *Machine) {
	assert.False(t, m.Is1(Exception), "Exception state active")
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

		assert.Greater(t, nextIdx, idx, "expected %s before %s", name, next)
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
	m.SetLogLevel(LogEverything)
	m.SetLogger(func(i LogLevel, msg string, args ...any) {
		if m.IsDisposed() {
			return
		}
		if os.Getenv(EnvAmDebug) != "" {
			t.Logf(msg+"\n", args...)
		}
		mx.Lock()
		defer mx.Unlock()

		*log += fmt.Sprintf(msg+"\n", args...)
	})
}

func assertString(t *testing.T, m *Machine, expected string, states S) {
	assert.Equal(t,
		strings.Trim(dedent.Dedent(expected), "\n"),
		strings.Trim(m.Inspect(states), "\n"))
}
