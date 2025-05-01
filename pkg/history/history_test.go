package history

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func ExampleTrack() {
	// create a machine
	mach := am.New(context.Background(), am.Schema{
		"A": {},
		"B": {},
	}, nil)

	// create a history
	h := Track(mach, am.S{"A", "C"}, 0)

	// collect some info
	h.ActivatedRecently("A", time.Second) // true
	fmt.Printf("%s", h.LastActivated["A"])
	println(h.Entries)
}

func TestTrack(t *testing.T) {
	// create a machine
	mach := NewNoRels(t, nil)

	// create a history
	h := Track(mach, am.S{"A", "C"}, 0)

	// mutate
	mach.Add(am.S{"A", "B"}, nil)
	lastA := h.LastActivated["A"]
	mach.Add1("C", nil)
	mach.Remove1("A", nil)
	time.Sleep(time.Nanosecond)
	mach.Add(am.S{"A", "D"}, nil)

	// assert
	assert.Greater(t, h.LastActivated["A"].Nanosecond(), lastA.Nanosecond(),
		"Activation timestamp updated")
	assert.Len(t, h.Entries, 4, "4 mutations for tracked states")
	assert.True(t, h.ActivatedRecently("A", time.Second), "A was activated")
	assert.False(t, h.ActivatedRecently("B", time.Second), "B isn't tracked")
}

func TestTrackPreactivation(t *testing.T) {
	// create a machine
	mach := NewNoRels(t, am.S{"A", "C"})

	// create a history
	h := Track(mach, am.S{"A", "C"}, 0)

	// assert
	assert.True(t, h.ActivatedRecently("A", time.Second), "A was pre-activated")
}

// NewNoRels creates a new machine with no relations between states.
func NewNoRels(t *testing.T, initialState am.S) *am.Machine {
	m := am.New(context.Background(), am.Schema{
		"A": {},
		"B": {},
		"C": {},
		"D": {},
	}, nil)
	m.SetLogger(func(i am.LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	if amhelp.IsDebug() {
		m.SetLogLevel(am.LogEverything)
		m.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		m.Set(initialState, nil)
	}
	return m
}
