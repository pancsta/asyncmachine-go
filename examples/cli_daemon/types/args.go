package types

import (
	"encoding/gob"
	"time"

	"github.com/pancsta/asyncmachine-go/examples/cli_daemon/states"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var ss = states.DaemonStates

// ///// ///// /////

// ///// ARGS

// ///// ///// /////
// TODO add RPC args example from pkg/node

const APrefix = "daemon"

type Args struct {
	am.ArgsBase `json:"-"`
}

func (Args) ArgsPrefix() string {
	return APrefix
}

// -----

type OpFoo1 struct {
	Args `json:"-"`

	Duration time.Duration `log:"duration"`
}

func (OpFoo1) ArgsState() string {
	return ss.OpFoo1
}

// -----

type OpBar2 OpFoo1

func (OpBar2) ArgsState() string {
	return ss.OpBar2
}

// ----- RPC boilerplate

func init() {
	for _, arg := range ArgsRpc {
		gob.Register(arg)
	}
}

// ArgsRpc will be available in the REPL.
var ArgsRpc = []am.ArgsApi{OpFoo1{}, OpBar2{}}
