package helpers

import (
	"context"
	"os"
	"testing"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/stretchr/testify/assert"
)

type S = am.S
type T = am.T

func TestTimeMatrix(t *testing.T) {
	// m1 init
	m1 := NewNoRels(t, nil)
	statesStruct := m1.GetStruct()
	statesStruct["B"] = am.State{Multi: true}
	err := m1.SetStruct(statesStruct, S{"A", "B", "C", "D", am.Exception})
	assert.NoError(t, err)

	// mutate & assert
	m1.Add(S{"A", "B"}, nil)
	m1.Add(S{"A", "B"}, nil)
	assertClocks(t, m1, S{"A", "B", "C", "D"}, T{1, 3, 0, 0},
		"m1 clocks mismatch")

	// m2 init
	m2 := NewNoRels(t, nil)
	statesStruct = m1.GetStruct()
	statesStruct["B"] = am.State{Multi: true}
	err = m2.SetStruct(statesStruct, S{"A", "B", "C", "D", am.Exception})
	assert.NoError(t, err)

	// mutate & assert
	m2.Add(S{"A", "B"}, nil)
	m2.Add(S{"A", "B"}, nil)
	m2.Add(S{"A", "B", "C"}, nil)
	m2.Set(S{"D"}, nil)
	assertClocks(t, m2, S{"A", "B", "C", "D"}, T{2, 6, 2, 1},
		"m2 clocks mismatch")

	matrix, err := TimeMatrix([]*am.Machine{m1, m2})
	assert.NoError(t, err)
	assert.Equal(t, T{1, 3, 0, 0, 0}, matrix[0])
	assert.Equal(t, T{2, 6, 2, 1, 0}, matrix[1])
}

///// helpers
// TODO extract test helpers to internal/testing

// NewNoRels creates a new machine with no relations between states.
func NewNoRels(t *testing.T, initialState am.S) *am.Machine {
	m := am.New(context.Background(), am.Struct{
		"A": {},
		"B": {},
		"C": {},
		"D": {},
	}, nil)
	m.SetLogger(func(i am.LogLevel, msg string, args ...any) {
		t.Logf(msg, args...)
	})
	if os.Getenv("AM_DEBUG") != "" {
		m.SetLogLevel(am.LogEverything)
		m.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		m.Set(initialState, nil)
	}
	return m
}

func assertClocks(t *testing.T, m *am.Machine, states S, times T,
	msgAndArgs ...interface{},
) {
	for i, state := range states {
		assert.True(t, m.IsClock(state, times[i]), msgAndArgs...)
	}
}
