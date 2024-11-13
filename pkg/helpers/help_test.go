package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type A struct {
	Local struct{}
	Log   string `log:"log1"`
	Plain string
}

type ARpc struct {
	Log   string `log:"log1"`
	Plain string
}

func TestArgsRpc(t *testing.T) {
	a := &A{
		Local: struct{}{},
		Log:   "log",
		Plain: "plain",
	}
	expected := &ARpc{
		Log:   "log",
		Plain: "plain",
	}

	rpc := ArgsToArgs(a, &ARpc{})

	assert.IsType(t, &ARpc{}, rpc)
	assert.Equal(t, expected.Log, rpc.Log)
	assert.Equal(t, expected.Plain, rpc.Plain)
}

func TestArgsToLogMap(t *testing.T) {
	a := &A{
		Local: struct{}{},
		Log:   "log",
		Plain: "plain",
	}

	expected := map[string]string{
		"log1": "log",
	}

	logMap := ArgsToLogMap(a)

	assert.Equal(t, expected["log1"], logMap["log1"])
	assert.Len(t, logMap, 1)
}
