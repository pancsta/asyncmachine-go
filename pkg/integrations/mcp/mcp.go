package mcp

import (
	"context"
	"fmt"
	"slices"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type Opts struct {
	StatesInclude  am.S
	StatesExclude  am.S
	StatesReadonly am.S
	// list of typed args
	Args []am.ArgsApi
	// RPC args parser
	ArgsUnmarshaller amhelp.ArgsUnmarshallerFn
	CallSignatures   []am.CallSignature
	// optional func to call after a mutation
	MutCallback func(ctx context.Context) error
	Version     string
	Name        string
	Desc        string
	StateCalls  []am.CallSignature
}

type Server struct {
	Mach am.Api
	Mcp  *server.MCPServer
	Http *server.StreamableHTTPServer
	Opts Opts

	// all the names of type safe args
	argNames []string
}

func New(mach am.Api, opts Opts) (*Server, error) {
	var err error

	// validate
	if opts.Args == nil {
		return nil, fmt.Errorf("field ArgsBase required")
	}
	if opts.Name == "" {
		return nil, fmt.Errorf("field Name required")
	}
	if opts.Version == "" {
		return nil, fmt.Errorf("field Version required")
	}
	if opts.ArgsUnmarshaller == nil {
		opts.ArgsUnmarshaller = amhelp.NewArgsUnmarshaller(opts.Args)
	}

	// Create a new MCP server
	s := server.NewMCPServer(opts.Name, opts.Version,
		server.WithToolCapabilities(false),
		server.WithDescription(opts.Desc),
	)

	m := &Server{
		Mach: mach,
		Mcp:  s,
		Opts: opts,
	}

	// MACHINE

	// collect arg names
	m.argNames, err = amhelp.ArgsNames(opts.Args)
	if err != nil {
		return nil, err
	}
	var toolOptsArgs []mcp.ToolOption
	for _, arg := range m.argNames {
		toolOptsArgs = append(toolOptsArgs, mcp.WithString(arg))
	}

	// Add
	toolOpts := []mcp.ToolOption{
		mcp.WithDescription("Add a state with optional params"),
		mcp.WithString("state",
			mcp.Required(),
			mcp.Description("Name of the state to add"),
			mcp.Enum(m.StateNames()...),
		),
	}
	toolOpts = append(toolOpts, toolOptsArgs...)
	add := mcp.NewTool("Add", toolOpts...)

	// Remove
	toolOpts = []mcp.ToolOption{
		mcp.WithDescription("Remove a state with optional params"),
		mcp.WithString("state",
			mcp.Required(),
			mcp.Description("Name of the state to remove"),
			// TODO allowlist
			mcp.Enum(m.StateNames()...),
		),
	}
	toolOpts = append(toolOpts, toolOptsArgs...)
	remove := mcp.NewTool("Remove", toolOpts...)

	// specified call signatures
CALLS:
	for _, sig := range opts.StateCalls {
		// perms
		for _, state := range sig.States {
			if !m.stateMutable(state) {
				continue CALLS
			}
		}

		tool, handler := m.newCallSigHandler(&sig)
		s.AddTool(tool, handler)
	}

	// TODO Serialize, Inspect, Times (sum, queueTick, machTick)

	// bind handlers
	s.AddTool(add, m.mutAdd)
	s.AddTool(remove, m.mutRemove)

	m.Http = server.NewStreamableHTTPServer(s)

	return m, nil
}

func (m *Server) mutAdd(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	state, err := req.RequireString("state")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// perms
	if !m.stateMutable(state) {
		return mcp.NewToolResultError(fmt.Sprintf(
			"state %s not allowed", state)), nil
	}

	// parse args
	args := am.A{"FromMCP": true}
	for _, name := range m.argNames {
		if v := req.GetString(name, ""); v != "" {
			args[name] = v
		}
	}

	// mutate
	res := m.Mach.Add1(state, m.Opts.ArgsUnmarshaller(args))

	// wait until processed
	if res == am.Canceled {
		return mcp.NewToolResultError("canceled"), nil
	}
	select {
	case <-m.Mach.WhenQueue(res):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// callback
	if m.Opts.MutCallback != nil {
		err := m.Opts.MutCallback(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf(
				"callback error: %s", err)), nil
		}
	}

	return m.response()
}

func (m *Server) mutRemove(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	state, err := req.RequireString("state")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// perms
	if !m.stateMutable(state) {
		return mcp.NewToolResultError(fmt.Sprintf(
			"state %s not allowed", state)), nil
	}

	// parse args
	args := am.A{"FromMCP": true}
	for _, name := range m.argNames {
		args[name] = req.GetString(name, "")
	}

	// mutate
	res := m.Mach.Add1(state, nil)

	// wait until processed
	if res == am.Canceled {
		return mcp.NewToolResultError("canceled"), nil
	}
	select {
	case <-m.Mach.WhenQueue(res):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// callback
	if m.Opts.MutCallback != nil {
		err := m.Opts.MutCallback(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf(
				"callback error: %s", err)), nil
		}
	}

	return m.response()
}

func (m *Server) newCallSigHandler(
	sig *am.CallSignature,
) (mcp.Tool, server.ToolHandlerFunc) {
	name := sig.Name
	if name == "" {
		name = sig.String()
	}

	// collect opts
	var opts []mcp.ToolOption
	if sig.Desc != "" {
		opts = append(opts, mcp.WithDescription(sig.Desc))
	}

	// collect args
	for _, arg := range sig.Args() {
		var argOpts []mcp.PropertyOption
		if vals, ok := sig.Values[arg]; ok {
			argOpts = append(argOpts, mcp.Enum(vals...))
		}
		if slices.Contains(sig.Needed, arg) {
			argOpts = append(argOpts, mcp.Required())
		}
		opts = append(opts, mcp.WithString(arg, argOpts...))
	}

	// tool & handler
	tool := mcp.NewTool(name, opts...)
	return tool, func(
		ctx context.Context, req mcp.CallToolRequest,
	) (*mcp.CallToolResult, error) {
		// parse args
		args := am.A{"FromMCP": true}
		for _, name := range sig.Args() {
			args[name] = req.GetString(name, "")
		}

		// mutate
		var res am.Result
		if sig.IsRemove {
			res = m.Mach.Remove(sig.States, m.Opts.ArgsUnmarshaller(args))
		} else {
			res = m.Mach.Add(sig.States, m.Opts.ArgsUnmarshaller(args))
		}

		// wait until processed
		if res == am.Canceled {
			return mcp.NewToolResultError("canceled"), nil
		}
		select {
		case <-m.Mach.WhenQueue(res):
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		// callback
		if m.Opts.MutCallback != nil {
			err := m.Opts.MutCallback(ctx)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf(
					"callback error: %s", err)), nil
			}
		}

		return m.response()
	}
}

// response is a generic response with clocks of active states
func (m *Server) response() (*mcp.CallToolResult, error) {
	ret := "("
	for state, tick := range m.Mach.Clock(nil) {
		if !m.stateMutable(state) &&
			!slices.Contains(m.Opts.StatesReadonly, state) {

			continue
		}
		if !am.IsActiveTick(tick) {
			continue
		}
		if ret != "(" {
			ret += " "
		}
		ret += fmt.Sprintf("%s:%d", state, tick)
	}

	return mcp.NewToolResultText(ret + ")"), nil
}

func (m *Server) stateMutable(name string) bool {
	if len(m.Opts.StatesInclude) > 0 {
		return slices.Contains(m.Opts.StatesInclude, name)
	}

	return !slices.Contains(m.Opts.StatesExclude, name)
}

func (m *Server) StateNames() am.S {
	return slices.DeleteFunc(slices.Clone(m.Mach.StateNames()),
		func(name string) bool {
			return !m.stateMutable(name)
		})
}
