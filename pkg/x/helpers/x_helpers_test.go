package helpers

import (
	"context"
	"testing"
	"time"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/stretchr/testify/assert"
)

type S = am.S
type T = am.Time

func TestTimeMatrix(t *testing.T) {
	// m1 init
	m1 := NewNoRels(t, nil)
	statesStruct := m1.Schema()
	statesStruct["B"] = am.State{Multi: true}
	statesStruct["Bump1"] = am.State{}
	err := m1.SetSchema(statesStruct,
		S{"A", "B", "C", "D", "Bump1", am.Exception})
	assert.NoError(t, err)

	// mutate & assert
	m1.Add(S{"A", "B"}, nil)
	m1.Add(S{"A", "B"}, nil)
	assertTime(t, m1, S{"A", "B", "C", "D"}, T{1, 3, 0, 0},
		"m1 clocks mismatch")

	// m2 init
	m2 := NewNoRels(t, nil)
	statesStruct = m1.Schema()
	statesStruct["B"] = am.State{Multi: true}
	statesStruct["Bump1"] = am.State{}
	err = m2.SetSchema(statesStruct, S{"A", "B", "C", "D", "Bump1", am.Exception})
	assert.NoError(t, err)

	// mutate & assert
	m2.Add(S{"A", "B"}, nil)
	m2.Add(S{"A", "B"}, nil)
	m2.Add(S{"A", "B", "C"}, nil)
	m2.Set(S{"D"}, nil)
	assertTime(t, m2, S{"A", "B", "C", "D"}, T{2, 6, 2, 1},
		"m2 clocks mismatch")

	matrix, err := TimeMatrix([]*am.Machine{m1, m2})
	assert.NoError(t, err)
	assert.Equal(t, T{1, 3, 0, 0, 0, 0}, matrix[0])
	assert.Equal(t, T{2, 6, 2, 1, 0, 0}, matrix[1])
}

// /// helpers
// TODO extract test helpers to internal/testing

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

func assertTime(t *testing.T, m *am.Machine, states S, time T,
	msgAndArgs ...interface{},
) {
	assert.Equal(t, m.Time(states), time, msgAndArgs...)
}
