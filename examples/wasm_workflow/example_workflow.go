package example

import (
	"crypto/sha512"
	_ "embed"
	"encoding/gob"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pancsta/asyncmachine-go/examples/wasm_workflow/states"

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
var ssO = states.OrchestratorStates
var ssW = states.WorkerStates

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

const APrefix = "wasm"

// Args is shared pkg args for Any state
type Args struct {
	am.ArgsBase `json:"-"`
}

func (Args) ArgsPrefix() string {
	return APrefix
}

// -----

type ABrowserConn struct {
	Args `json:"-"`

	// ID of the connected state machine
	Id string `log:"id"`
}

func (ABrowserConn) ArgsState() string {
	return ssO.BrowserConn
}

// -----

type AStart struct {
	Args `json:"-"`

	Boot *Boot
}

func (AStart) ArgsState() string {
	return ssO.Start
}

// -----

type AWorking struct {
	Args `json:"-"`

	Payload []byte
	Retry   bool `log:"retry"`
}

func (AWorking) ArgsState() string {
	return ssW.Working
}

// -----

type ACompleted struct {
	Args `json:"-"`

	Payload []byte
}

func (ACompleted) ArgsState() string {
	return ssW.Completed
}

// ----- RPC

func init() {
	for _, arg := range ArgsRpc {
		gob.Register(arg)
	}
	for _, arg := range ArgsWorkerRpc {
		gob.Register(arg)
	}
}

// ArgsRpc will be available in the REPL.
var ArgsRpc = []am.ArgsApi{ABrowserConn{}, AStart{}}

// ArgsWorkerRpc will be available in the REPL.
var ArgsWorkerRpc = []am.ArgsApi{AWorking{}}
