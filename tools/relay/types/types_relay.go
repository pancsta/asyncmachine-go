package types

import (
	"context"
	"net"
	"regexp"
	"time"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

const CmdRotateDbg = "rotate-dbg"

type OutputFunc func(msg string, args ...any)

// nolint:lll
type Args struct {
	RotateDbg *ArgsRotateDbg `arg:"subcommand:rotate-dbg" help:"Rotate dbg protocol with fragmented dump files"`
	Wasm      *ArgsWasm      `arg:"subcommand:wasm" help:"WebSockets to local TCP listeners for WASM"`
	Debug     bool           `arg:"--debug" help:"Enable debugging for asyncmachine"`
	Name      string         `arg:"-n,--name" help:"Name of this relay"`
	Output    OutputFunc     `arg:"-"`
	Parent    am.Api         `arg:"-"`
	Version   bool           `arg:"-v,--version" help:"Print version and exit"`
}

// ArgsRotateDbg converts dbg dumps to other formats / versions.
// am-relay convert-dbg am-dbg-dump.gob.br -o am-vis
// nolint:lll
type ArgsRotateDbg struct {
	ListenAddr   string        `arg:"-l,--listen-addr" default:"localhost:12732" help:"Listen address for the RPC server"`
	FwdAddr      []string      `arg:"-f,--fwd-addr,separate" help:"Address of an RPC server to forward data to (repeatable)"`
	IntervalTx   int           `arg:"--interval-tx" default:"10000" help:"Amount of transitions to create a dump file"`
	IntervalTime time.Duration `arg:"--interval-duration" default:"24h" help:"Amount of human time to create a dump file"`
	Filename     string        `arg:"-o,--output" default:"am-dbg-dump" help:"Output file base name"`
	Dir          string        `arg:"-d,--dir" default:"." help:"Output directory"`
}

// ArgsWasm
// nolint:lll
type ArgsWasm struct {
	ListenAddr  string `arg:"-l,--listen-addr" default:"localhost:12733" help:"Listen address for HTTP server"`
	StaticDir   string `arg:"-s,--static-dir" help:"Directory with static files to serve (optional)"`
	ReplAddrDir string `arg:"-r,--repl-addr-dir" help:"Directory for creating REPL addr files (optional)"`
	// Match incoming tunnels by mach IDs and pass directly to new RPC clients
	TunnelMatchers []TunnelMatcher `arg:"-"`
	// Match incoming TCP dials by mach IDs and pass directly to new RPC servers
	DialMatchers []DialMatcher `arg:"-"`
}

type DialMatcher struct {
	Id        *regexp.Regexp
	NewServer NewServerFunc
}

type NewServerFunc func(
	ctx context.Context, id string, conn net.Conn,
) (*arpc.Server, error)

type TunnelMatcher struct {
	Id        *regexp.Regexp
	NewClient NewClientFunc
}

type NewClientFunc func(
	ctx context.Context, id string, conn net.Conn,
) (*arpc.Client, error)

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "relay"

// A is a struct for node arguments. It's a typesafe alternative to [am.A].
type A struct {
	Id         string `log:"id"`
	Addr       string `log:"addr"`
	RemoteAddr string `log:"remote_addr"`

	// non-rpc fields

	Conn net.Conn
}

// ARpc is a subset of A, that can be passed over RPC.
type ARpc struct {
	Id   string `log:"id"`
	Addr string `log:"addr"`
}

// ParseArgs extracts A from [am.Event.Args][APrefix].
func ParseArgs(args am.A) *A {
	if r, ok := args[APrefix].(*ARpc); ok {
		return amhelp.ArgsToArgs(r, &A{})
	} else if r, ok := args[APrefix].(ARpc); ok {
		return amhelp.ArgsToArgs(&r, &A{})
	}
	if a, _ := args[APrefix].(*A); a != nil {
		return a
	}
	return &A{}
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	return am.A{APrefix: args}
}

// PassRpc prepares [am.A] from A to pass over RPC.
func PassRpc(args *ARpc) am.A {
	return am.A{APrefix: amhelp.ArgsToArgs(args, &ARpc{})}
}

// LogArgs is an args logger for A.
func LogArgs(args am.A) map[string]string {
	a := ParseArgs(args)
	if a == nil {
		return nil
	}

	return amhelp.ArgsToLogMap(a, 0)
}
