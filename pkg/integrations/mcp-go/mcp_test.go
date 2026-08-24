package mcp

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestMCPServer(t *testing.T) {
	ctx := context.Background()
	mach := am.New(ctx, am.Schema{
		"StateA": {},
		"StateB": {Require: am.S{"StateA"}},
		"StateC": {},
	}, nil)

	opts := Opts{
		Name:    "test-mcp",
		Version: "1.0.0",
		Desc:    "Test MCP server",
		Args: []am.ArgsApi{
			am.ArgsBase{},
		},
		StatesInclude: am.S{"StateA", "StateB"},
		MutCallback: func(ctx context.Context) error {
			return nil
		},
		StateCalls: []am.CallSignature{
			{Name: "CustomCall", States: am.S{"StateC"}, Desc: "Custom call"},
		},
	}

	srv, err := New(mach, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Test stateNames
	names := srv.StateNames()
	assert.Contains(t, names, "StateA")
	assert.Contains(t, names, "StateB")
	assert.NotContains(t, names, "StateC") // Excluded by StatesInclude

	// Test mutAdd tool
	reqAdd := mcp.CallToolRequest{}
	reqAdd.Params.Name = "Add"
	reqAdd.Params.Arguments = map[string]any{
		"state": "StateA",
	}

	res, err := srv.mutAdd(ctx, reqAdd)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.False(t, res.IsError)
	assert.Contains(t, res.Content[0].(mcp.TextContent).Text, "StateA:")
	assert.True(t, mach.Is1("StateA"))

	// Test mutRemove tool
	reqRemove := mcp.CallToolRequest{}
	reqRemove.Params.Name = "Remove"
	reqRemove.Params.Arguments = map[string]any{
		"state": "StateA",
	}

	resRemove, err := srv.mutRemove(ctx, reqRemove)
	require.NoError(t, err)
	require.NotNil(t, resRemove)
	assert.False(t, resRemove.IsError, resRemove.Content)
	assert.False(t, mach.Is1("StateA"))
}

func TestMCPServer_StateCalls(t *testing.T) {
	ctx := context.Background()
	mach := am.New(ctx, am.Schema{
		"StateA": {},
		"StateB": {},
	}, nil)

	opts := Opts{
		Name:    "test-mcp-calls",
		Version: "1.0.0",
		Desc:    "Test MCP server calls",
		Args: []am.ArgsApi{
			am.ArgsBase{},
		},
		StatesInclude: am.S{"StateA", "StateB"},
		StateCalls: []am.CallSignature{
			{Name: "CallA", States: am.S{"StateA"}, Needed: []string{"Param1"}},
			{Name: "CallRemoveB", States: am.S{"StateB"}, IsRemove: true},
		},
	}

	srv, err := New(mach, opts)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// Test CallA (Add)
	sigA := opts.StateCalls[0]
	toolA, handlerA := srv.newCallSigHandler(&sigA)
	require.Equal(t, "CallA", toolA.Name)

	reqA := mcp.CallToolRequest{}
	reqA.Params.Name = "CallA"
	reqA.Params.Arguments = map[string]any{
		"Param1": "Value1",
	}
	resA, err := handlerA(ctx, reqA)
	require.NoError(t, err)
	require.NotNil(t, resA)
	assert.False(t, resA.IsError)
	assert.True(t, mach.Is1("StateA"))

	// Initialize StateB for removal
	mach.Add1("StateB", nil)
	assert.True(t, mach.Is1("StateB"))

	// Test CallRemoveB (Remove)
	sigB := opts.StateCalls[1]
	toolB, handlerB := srv.newCallSigHandler(&sigB)
	require.Equal(t, "CallRemoveB", toolB.Name)

	reqB := mcp.CallToolRequest{}
	reqB.Params.Name = "CallRemoveB"
	reqB.Params.Arguments = map[string]any{}
	resB, err := handlerB(ctx, reqB)
	require.NoError(t, err)
	require.NotNil(t, resB)
	assert.False(t, resB.IsError)
	assert.False(t, mach.Is1("StateB"))
}
