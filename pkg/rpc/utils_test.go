package rpc

import (
	"strings"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/types"
)

func assertStates(t *testing.T, m types.MachineApi, expected am.S,
	msgAndArgs ...interface{},
) {
	assert.ElementsMatch(t, expected, m.ActiveStates(), msgAndArgs...)
}

func assertTime(t *testing.T, m types.MachineApi, states am.S, time am.Time,
	msgAndArgs ...interface{},
) {
	assert.Equal(t, m.Time(states), time, msgAndArgs...)
}

func assertNoException(t *testing.T, m types.MachineApi) {
	assert.False(t, m.Is1(am.Exception), "Exception state active")
}

func assertString(
	t *testing.T, m types.MachineApi, expected string, states am.S,
) {
	assert.Equal(t,
		strings.Trim(dedent.Dedent(expected), "\n"),
		strings.Trim(m.Inspect(states), "\n"))
}
