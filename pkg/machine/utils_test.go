package machine

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"
)

type History struct {
	NoOpTracer

	Order   []string
	Counter map[string]int
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

func (h *History) AA(e *Event) {
	h.event("AA")
}

func (h *History) AAny(e *Event) {
	h.event("AAny")
}

// B

func (h *History) AB(e *Event) {
	h.event("AB")
}

func (h *History) BEnter(e *Event) {
	h.event("BEnter")
}

func (h *History) BState(e *Event) {
	h.event("BState")
}

func (h *History) BB(e *Event) {
	h.event("BB")
}

func (h *History) AnyB(e *Event) {
	h.event("AnyB")
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

func (h *History) AnyC(e *Event) {
	h.event("AnyC")
}

func (h *History) CEnter(e *Event) {
	h.event("CEnter")
}

func (h *History) CState(e *Event) {
	h.event("CState")
}

// D

func (h *History) AnyD(e *Event) {
	h.event("AnyD")
}

func (h *History) DEnter(e *Event) {
	h.event("DEnter")
}

func (h *History) event(name string) {
	h.Order = append(h.Order, name)

	if _, ok := h.Counter[name]; !ok {
		h.Counter[name] = 0
	}

	h.Counter[name]++
}

func trackTransitions(mach *Machine, events S) *History {
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

func assertEventCountsMin(t *testing.T, history *History, expected int) {
	// assert event counts
	for event, count := range history.Counter {
		assert.GreaterOrEqual(t, expected, count, "event %s call count", event)
	}
}

func captureLog(t *testing.T, m *Machine, log *string) {
	m.SetLogLevel(LogEverything)
	m.SetLogger(func(i LogLevel, msg string, args ...any) {
		if os.Getenv("AM_DEBUG") != "" {
			t.Logf(msg+"\n", args...)
		}
		*log += fmt.Sprintf(msg+"\n", args...)
	})
}

func assertString(t *testing.T, m *Machine, expected string, states S) {
	assert.Equal(t,
		strings.Trim(dedent.Dedent(expected), "\n"),
		strings.Trim(m.Inspect(states), "\n"))
}
