package example

import (
	_ "embed"
	"encoding/gob"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pancsta/asyncmachine-go/examples/wasm/states"
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

var ss = states.SharedStates

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

const APrefix = "wasm"

type Args struct {
	am.ArgsBase `json:"-"`
}

func (Args) ArgsPrefix() string {
	return APrefix
}

// -----

type AMsg struct {
	Args `json:"-"`

	Msg string `log:"msg"`
}

func (AMsg) ArgsState() string {
	return ss.Msg
}

// ----- RPC

// ArgsRpc will be available in the REPL.
var ArgsRpc = []am.ArgsApi{AMsg{}}

func init() {
	for _, arg := range ArgsRpc {
		gob.Register(arg)
	}
}
