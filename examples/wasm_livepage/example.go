package example

import (
	_ "embed"
	"encoding/gob"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"github.com/pancsta/asyncmachine-go/examples/wasm_livepage/states"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var ssD = states.DemoStates

var (
	EnvHttpAddr string
	EnvReplDir  string
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

	EnvHttpAddr = os.Getenv("LIVEPAGE_HTTP_ADDR")
	EnvReplDir = os.Getenv("REPL_DIR")
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "lpdemo"

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
	return ssD.Msg
}

// ----- RPC

// ArgsRpc will be available in the REPL.
var ArgsRpc = []am.ArgsApi{AMsg{}}

func init() {
	for _, arg := range ArgsRpc {
		gob.Register(arg)
	}
}

// NewMsg is a typed Msg with the given text.
func NewMsg(msg string) *AMsg {
	return &AMsg{Msg: msg}
}
