package example

import (
	"crypto/sha512"
	_ "embed"
	"encoding/gob"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var (
	EnvReplDir              string
	EnvRelayHttpAddr        string
	EnvOrchestratorTcpAddr  string
	EnvOrchestratorReplAddr string
	EnvBrowser1TcpAddr      string
	EnvBrowser1ReplAddr     string
	EnvBrowser2TcpAddr      string
	EnvBrowser2ReplAddr     string
	EnvBrowser3ReplAddr     string
	EnvBrowser4ReplAddr     string
	EnvHashIterations       int
	EnvExitOnCompleted      bool
	EnvSuccessRate          int
)

//go:embed config.env
var env string

func init() {
	reader := strings.NewReader(env)
	envMap, err := godotenv.Parse(reader)
	if err == nil {
		for k, v := range envMap {
			os.Setenv(k, v)
		}
	}

	EnvRelayHttpAddr = os.Getenv("RELAY_ADDR")
	EnvReplDir = os.Getenv("REPL_DIR")
	EnvOrchestratorTcpAddr = os.Getenv("ORCHESTRATOR_TCP_ADDR")
	EnvOrchestratorReplAddr = os.Getenv("ORCHESTRATOR_REPL_ADDR")
	EnvBrowser1TcpAddr = os.Getenv("BROWSER_1_TCP_ADDR")
	EnvBrowser1ReplAddr = os.Getenv("BROWSER_1_REPL_ADDR")
	EnvBrowser2TcpAddr = os.Getenv("BROWSER_2_TCP_ADDR")
	EnvBrowser2ReplAddr = os.Getenv("BROWSER_2_REPL_ADDR")
	EnvBrowser3ReplAddr = os.Getenv("BROWSER_3_REPL_ADDR")
	EnvBrowser4ReplAddr = os.Getenv("BROWSER_4_REPL_ADDR")
	v, _ := strconv.Atoi(os.Getenv("HASH_ITERATIONS"))
	EnvHashIterations = v
	EnvExitOnCompleted = os.Getenv("EXIT_ON_COMPLETED") == "1"
	v, _ = strconv.Atoi(os.Getenv("SUCCESS_RATE"))
	EnvSuccessRate = v
}

// ///// ///// /////

// ///// WORK

// ///// ///// /////

// SimulateLoad hashes a payload thousands of times to burn CPU cycles.
func SimulateLoad(iterations int) {
	payload := []byte("example_wasm_workflow_data")
	for i := 0; i < iterations; i++ {
		hash := sha512.Sum512(payload)
		payload = hash[:] // Feed the hash back in as the next payload
	}
}

func ComputeHash(payloads ...[]byte) [64]byte {
	p := slices.Concat(payloads...)
	return sha512.Sum512(p)
}

type Boot struct {
	TraceId string
	SpanId  string
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
	Id      string `log:"id"`
	Boot    *Boot
	Payload []byte
	Failed  bool `log:"failed"`
	Retry   bool `log:"retry"`

	// non-rpc fields

	ReturnCh chan<- []string
}

// ARpc is a subset of A that can be passed over RPC.
type ARpc struct {
	Id      string `log:"id"`
	Boot    *Boot
	Payload []byte
	Failed  bool `log:"failed"`
	Retry   bool `log:"retry"`
}

// ParseArgs extracts A or ARpc from [am.Event.Args][APrefix].
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
func PassRpc(args *A) am.A {
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
