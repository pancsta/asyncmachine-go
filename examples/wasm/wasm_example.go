package example

import (
	_ "embed"
	"encoding/gob"
	"encoding/json"
	"os"
	"strings"

	"github.com/joho/godotenv"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var (
	EnvFooTcpAddr  string
	EnvFooReplAddr string
)

var (
	EnvBarTcpAddr  string
	EnvBarReplAddr string
)

var (
	EnvRelayHttpAddr string
	EnvReplDir       string
)

//go:embed .env
var env string

func init() {
	reader := strings.NewReader(env)
	envMap, err := godotenv.Parse(reader)
	if err == nil {
		for k, v := range envMap {
			os.Setenv(k, v)
		}
	}

	EnvFooTcpAddr = os.Getenv("FOO_TCP_ADDR")
	EnvFooReplAddr = os.Getenv("FOO_REPL_ADDR")
	EnvBarTcpAddr = os.Getenv("BAR_TCP_ADDR")
	EnvBarReplAddr = os.Getenv("BAR_REPL_ADDR")
	EnvRelayHttpAddr = os.Getenv("RELAY_HTTP_ADDR")
	EnvReplDir = os.Getenv("REPL_DIR")
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

func init() {
	gob.Register(ARpc{})
}

const APrefix = "wasm"

// A is a struct for node arguments. It's a typesafe alternative to [am.A].
type A struct {
	Msg string `log:"msg"`

	// non-rpc fields

	ReturnCh chan<- []string
}

// ARpc is a subset of A, that can be passed over RPC.
type ARpc struct {
	Msg string `log:"msg"`
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

// ParseRpc parses am.A to *ARpc wrapped in am.A. Useful for REPLs.
func ParseRpc(args am.A) am.A {
	ret := am.A{APrefix: &ARpc{}}
	jsonArgs, err := json.Marshal(args)
	if err == nil {
		json.Unmarshal(jsonArgs, ret[APrefix])
	}

	return ret
}
