package mymach

import (
	"context"
	"encoding/gob"
	"fmt"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"mymach/states"
)

var isDebug = true

func init() {
	if !isDebug {
		return
	}

	// manual logging
	// amhelp.SetEnvLogLevel(am.LogOps)
	// os.Setenv(amhelp.EnvAmLogPrint, "2")

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	amhelp.EnableDebugging(true)
}

var ss = states.MyMachStates
var ArgsRpc = []am.ArgsApi{}

// ///// ///// /////

// ///// MACHINE

// ///// ///// /////

func New(ctx context.Context) (*Handlers, error) {
	// handlers
	handlers := &Handlers{DisposedHandlers: &ssam.DisposedHandlers{}}
	mach, err := am.NewCommon(ctx, "my_mach", states.MyMachSchema, ss.Names(), handlers, nil, nil)
	if err != nil {
		return nil, err
	}
	handlers.Mach = mach

	// telemetry and logging
	mach.SetGroups(states.MyMachGroups, states.MyMachStates)
	// mach.SemLogger().SetLevel(am.LogChanges)
	mach.SemLogger().SetArgsMapper(amhelp.LogArgsMapper)
	amhelp.MachDebugEnv(mach)
	_ = arpc.MachRepl(mach, "", &arpc.ReplOpts{
		AddrDir: ".",
		Args:    ArgsRpc,
	})

	return handlers, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

// see handler.go for state-state handlers

type Handlers struct {
	*am.ExceptionHandler
	*ssam.DisposedHandlers

	Mach *am.Machine
}

var _ = ss.Wet

func (h *Handlers) WetEnter(e *am.Event) bool {
	return e.Transition().TimeIndexAfter().Is1(ss.Water)
}
func (h *Handlers) WetState(e *am.Event) {
	fmt.Println("it is wet now")
}
func (h *Handlers) WetExit(e *am.Event) bool { return true }
func (h *Handlers) WetEnd(e *am.Event)       {}

var _ = ss.Water

func (h *Handlers) WaterEnter(e *am.Event) bool { return true }
func (h *Handlers) WaterState(e *am.Event)      {}
func (h *Handlers) WaterExit(e *am.Event) bool  { return true }
func (h *Handlers) WaterEnd(e *am.Event)        {}

var _ = ss.Dry

func (h *Handlers) DryEnter(e *am.Event) bool { return true }
func (h *Handlers) DryState(e *am.Event) {
	fmt.Println("it is dry now")
}
func (h *Handlers) DryExit(e *am.Event) bool { return true }
func (h *Handlers) DryEnd(e *am.Event)       {}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "my_mach"

// Args is shared pkg args for Any state
type Args struct {
	am.ArgsBase `json:"-"`
}

func (Args) ArgsPrefix() string {
	return APrefix
}
func init() {
	for _, arg := range ArgsRpc {
		gob.Register(arg)
	}
}

// A is an args struct common for all state handlers.
type A struct {
	Args `json:"-"`

	// Return chan.
	ReturnCh chan<- []string
}

func (A) ArgsState() string {
	return am.StateAny
}
