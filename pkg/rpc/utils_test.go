package rpc

import (
	"strings"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// TOOD use /internal

func assertStates(t *testing.T, m am.Api, expected am.S,
	msgAndArgs ...interface{},
) {
	assert.ElementsMatch(t, expected, m.ActiveStates(), msgAndArgs...)
}

func assertTime(t *testing.T, m am.Api, states am.S, time am.Time,
	msgAndArgs ...interface{},
) {
	assert.Subset(t, m.Time(states), time, msgAndArgs...)
}

func assertString(
	t *testing.T, m am.Api, expected string, states am.S,
) {
	assert.Equal(t,
		strings.Trim(dedent.Dedent(expected), "\n"),
		strings.Trim(m.Inspect(states), "\n"))
}
