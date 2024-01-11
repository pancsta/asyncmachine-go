package machine_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type History struct {
	Order   []string
	Counter map[string]int
}

func trackTransitions(m *am.Machine, events S) *History {
	history := &History{
		Order:   []string{},
		Counter: map[string]int{},
	}
	isBound := make(chan bool)

	ch := m.On(append(events, "queue-end"), nil)
	go func() {
		// guaranteed to run after listening starts
		go func() {
			// causes a return from trackTransitions
			close(isBound)
		}()
		for {
			e, ok := <-ch
			if !ok {
				break
			}
			if e.Name == "queue-end" {
				continue
			}
			history.Order = append(history.Order, e.Name)
			if _, ok := history.Counter[e.Name]; !ok {
				history.Counter[e.Name] = 0
			}
			history.Counter[e.Name]++
		}
	}()
	<-isBound
	return history
}

func assertStates(t *testing.T, m *am.Machine, expected S,
	msgAndArgs ...interface{},
) {
	assert.ElementsMatch(t, expected, m.ActiveStates, msgAndArgs...)
}

func assertNoException(t *testing.T, m *am.Machine) {
	assert.False(t, m.Is(S{"Exception"}), "Exception state active")
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

func captureLog(t *testing.T, m *am.Machine, log *string) {
	m.SetLogLevel(am.LogEverything)
	m.SetLogger(func(i am.LogLevel, msg string, args ...any) {
		if os.Getenv("AM_DEBUG") != "" {
			t.Logf(msg, args...)
		}
		*log += fmt.Sprintf(msg, args...)
	})
}

func assertString(t *testing.T, m *am.Machine, expected string, states S) {
	assert.Equal(t,
		strings.Trim(dedent.Dedent(expected), "\n"),
		strings.Trim(m.Inspect(states), "\n"))
}
