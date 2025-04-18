// Package history provides mutation history tracking and traversal.
package history

import (
	"slices"
	"sync"
	"time"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type History struct {
	*am.NoOpTracer

	Entries []Entry
	// LastActivated is a map of state names to the last time they were activated
	LastActivated map[string]time.Time
	// tracked states
	States am.S

	mx         sync.Mutex
	maxEntries int
}

func (h *History) TransitionEnd(tx *am.Transition) {
	if !tx.Accepted {
		return
	}

	mut := tx.Mutation
	match := false
	for _, name := range h.States {
		if mut.StateWasCalled(name) {
			match = true
			break
		}
	}
	if !match {
		return
	}

	// rotate TODO optimize rotation
	if len(h.Entries) >= h.maxEntries {
		cutFrom := len(h.Entries) - h.maxEntries
		h.Entries = h.Entries[cutFrom:]
	}
	// remember this mutation, remove Args
	h.Entries = append(h.Entries, Entry{
		CalledStates: amhelp.StatesToIndexes(tx.Machine.StateNames(),
			tx.Mutation.CalledStates),
		// TODO add to Transition
		MTimeDiff: tx.TimeAfter.DiffSince(tx.TimeBefore),
		Type:      tx.Mutation.Type,
		Auto:      tx.Mutation.Auto,
	})
	h.mx.Lock()
	// update last seen time
	for _, name := range tx.TargetStates() {
		if !slices.Contains(h.States, name) {
			continue
		}
		h.LastActivated[name] = time.Now()
	}
	h.mx.Unlock()
}

type Entry struct {
	// Mutation type
	Type am.MutationType
	// Indexes of called states
	CalledStates []int
	// Auto is true if the mutation was triggered by an Auto state
	Auto bool
	// MTime is machine time.
	MTime am.Time
	// HTime is human time.
	HTime time.Time
	// MTimeDiff is a machine time diff since the last entry.
	MTimeDiff am.Time
}

type MatcherStates struct {
	// Called is a set of states that were called in the mutation
	Called am.S
	// Active is a set of states that were active AFTER the mutation
	Active am.S
	// Inactive is a set of states that were inactive AFTER the mutation
	Inactive am.S
}

type MatcherTimes struct {
	// MinT is the minimum machine time the mutation could have occurred
	MinT am.Time
	// MaxT is the maximum machine time the mutation could have occurred
	MaxT am.Time
	// MinReal is the minimum real time the mutation could have occurred
	MinReal time.Time
	// MaxReal is the maximum real time the mutation could have occurred
	MaxReal time.Time
}

type MatcherIndexes struct {
	// Start is the minimum index of the mutation in the history
	Start int
	// End is the maximum index of the mutation in the history
	End int
}

// ActivatedRecently returns true if the state was activated within the last
// duration.
func (h *History) ActivatedRecently(state string, duration time.Duration) bool {
	h.mx.Lock()
	defer h.mx.Unlock()
	last, ok := h.LastActivated[state]
	if !ok {
		return false
	}
	return time.Since(last) < duration
}

// MatchEntries returns all entries that match the given criteria.
// TODO MatchEntries
func (h *History) MatchEntries(
	states MatcherStates, times MatcherTimes, indexes MatcherIndexes,
) []Entry {
	panic("not implemented")
}

// StatesActiveDuring returns true if all the given states were active during
// the whole given time range.
// TODO StatesActiveDuring
func (h *History) StatesActiveDuring(
	states am.S, timeRange MatcherTimes,
) bool {
	panic("not implemented")
}

// StatesInactiveDuring returns true if all the given states were inactive
// during the whole given time range.
// TODO StatesInactiveDuring
func (h *History) StatesInactiveDuring(
	states am.S, timeRange MatcherTimes,
) bool {
	panic("not implemented")
}

// Track creates a new history tracer for the given machine and states. Bind the
// tracker isnt thread safe and can't be unbound afterward.
// maxEntries: the maximum number of entries to keep in the history, 0 for
// default.
// TODO add MaxLimits{Entries int, Age time.Duration, MachineAge: uint64}
func Track(mach *am.Machine, states am.S, maxEntries int) *History {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	history := &History{
		Entries:       []Entry{},
		LastActivated: map[string]time.Time{},
		States:        states,
		maxEntries:    maxEntries,
	}

	// mark active states as activated to reflect the current state
	for _, name := range states {
		if mach.Is1(name) {
			history.LastActivated[name] = time.Now()
		}
	}
	_ = mach.BindTracer(history)

	return history
}
