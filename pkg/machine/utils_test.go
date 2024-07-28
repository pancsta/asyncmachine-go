package machine

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"
)

type History struct {
	Order     []string
	Counter   map[string]int
	CounterMx sync.Mutex
}

// TODO not thread safe
func trackTransitions(m *Machine, events S) *History {
	history := &History{
		Order:   []string{},
		Counter: map[string]int{},
	}
	isBound := make(chan bool)

	ch := m.OnEvent(append(events, EventQueueEnd), nil)
	go func() {
		// guaranteed to run after listening starts
		go func() {
			// causes a return from trackTransitions
			close(isBound)
		}()

		for e := range ch {
			if e.Name == EventQueueEnd {
				continue
			}
			history.Order = append(history.Order, e.Name)

			history.CounterMx.Lock()
			if _, ok := history.Counter[e.Name]; !ok {
				history.Counter[e.Name] = 0
			}
			history.Counter[e.Name]++

			history.CounterMx.Unlock()
		}
	}()

	<-isBound
	return history
}

func assertStates(t *testing.T, m *Machine, expected S,
	msgAndArgs ...interface{},
) {
	assert.ElementsMatch(t, expected, m.activeStates, msgAndArgs...)
}

func assertTimes(t *testing.T, m *Machine, states S, times T,
	msgAndArgs ...interface{},
) {
	assert.Equal(t, m.Time(states), times, msgAndArgs...)
}

func assertClocks(t *testing.T, m *Machine, states S, times T,
	msgAndArgs ...interface{},
) {
	for i, state := range states {
		assert.True(t, m.IsClock(state, times[i]), msgAndArgs...)
	}
}

func assertNoException(t *testing.T, m *Machine) {
	assert.False(t, m.Is1(Exception), "Exception state active")
}

func assertEventCounts(t *testing.T, history *History, expected int) {
	history.CounterMx.Lock()
	defer history.CounterMx.Unlock()

	// assert event counts
	for _, count := range history.Counter {
		assert.Equal(t, expected, count)
	}
}

func assertEventCountsMin(t *testing.T, history *History, expected int) {
	history.CounterMx.Lock()
	defer history.CounterMx.Unlock()

	// assert event counts
	for event, count := range history.Counter {
		assert.GreaterOrEqual(t, expected, count, "event %s call count", event)
	}
}

func captureLog(t *testing.T, m *Machine, log *string) {
	m.SetLogLevel(LogEverything)
	m.SetLogger(func(i LogLevel, msg string, args ...any) {
		if os.Getenv("AM_DEBUG") != "" {
			t.Logf(msg, args...)
		}
		*log += fmt.Sprintf(msg, args...)
	})
}

func assertString(t *testing.T, m *Machine, expected string, states S) {
	assert.Equal(t,
		strings.Trim(dedent.Dedent(expected), "\n"),
		strings.Trim(m.Inspect(states), "\n"))
}
