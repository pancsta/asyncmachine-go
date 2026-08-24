package starter_kit

import (
	"context"
	"encoding/gob"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/tools/generator/testdata/starter/states"
)

// enable debug with `true` or AM_DBG_ADDR=1
var isDebug = false

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

var ss = states.StarterStates

// ///// ///// /////

// ///// MACHINE

// ///// ///// /////

func New(ctx context.Context) (*Handlers, error) {
	// handlers
	handlers := &Handlers{DisposedHandlers: &ssam.DisposedHandlers{}}
	mach, err := am.NewCommon(ctx, "starter", states.StarterSchema, ss.Names(), handlers, nil, nil)
	if err != nil {
		return nil, err
	}
	handlers.Mach = mach

	// telemetry and logging
	mach.SetGroups(states.StarterGroups, states.StarterStates)
	// mach.SemLogger().SetLevel(am.LogChanges)
	mach.SemLogger().SetArgsMapper(amhelp.LogArgsMapper)
	amhelp.MachDebugEnv(mach)
	err, _ = arpc.MachReplEnv(mach, &arpc.ReplOpts{
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

var _ = ss.Start

func (h *Handlers) StartEnter(e *am.Event) bool { return true }
func (h *Handlers) StartState(e *am.Event) {}
func (h *Handlers) StartExit(e *am.Event) bool { return true }
func (h *Handlers) StartEnd(e *am.Event) {}

var _ = ss.BaseDBReady

func (h *Handlers) BaseDBReadyEnter(e *am.Event) bool { return true }
func (h *Handlers) BaseDBReadyState(e *am.Event) {}
func (h *Handlers) BaseDBReadyExit(e *am.Event) bool { return true }
func (h *Handlers) BaseDBReadyEnd(e *am.Event) {}

var _ = ss.BaseDBSaving

func (h *Handlers) BaseDBSavingEnter(e *am.Event) bool { return true }
func (h *Handlers) BaseDBSavingState(e *am.Event) {}
func (h *Handlers) BaseDBSavingExit(e *am.Event) bool { return true }
func (h *Handlers) BaseDBSavingEnd(e *am.Event) {}

var _ = ss.BaseDBStarting

func (h *Handlers) BaseDBStartingEnter(e *am.Event) bool { return true }
func (h *Handlers) BaseDBStartingState(e *am.Event) {}
func (h *Handlers) BaseDBStartingExit(e *am.Event) bool { return true }
func (h *Handlers) BaseDBStartingEnd(e *am.Event) {}

var _ = ss.CharacterReady

func (h *Handlers) CharacterReadyEnter(e *am.Event) bool { return true }
func (h *Handlers) CharacterReadyState(e *am.Event) {}
func (h *Handlers) CharacterReadyExit(e *am.Event) bool { return true }
func (h *Handlers) CharacterReadyEnd(e *am.Event) {}

var _ = ss.CheckStories

func (h *Handlers) CheckStoriesEnter(e *am.Event) bool { return true }
func (h *Handlers) CheckStoriesState(e *am.Event) {}
func (h *Handlers) CheckStoriesExit(e *am.Event) bool { return true }
func (h *Handlers) CheckStoriesEnd(e *am.Event) {}

var _ = ss.CheckingMenuRefs

func (h *Handlers) CheckingMenuRefsEnter(e *am.Event) bool { return true }
func (h *Handlers) CheckingMenuRefsState(e *am.Event) {}
func (h *Handlers) CheckingMenuRefsExit(e *am.Event) bool { return true }
func (h *Handlers) CheckingMenuRefsEnd(e *am.Event) {}

var _ = ss.RestoreCharacter

func (h *Handlers) RestoreCharacterEnter(e *am.Event) bool { return true }
func (h *Handlers) RestoreCharacterState(e *am.Event) {}
func (h *Handlers) RestoreCharacterExit(e *am.Event) bool { return true }
func (h *Handlers) RestoreCharacterEnd(e *am.Event) {}

var _ = ss.GenCharacter

func (h *Handlers) GenCharacterEnter(e *am.Event) bool { return true }
func (h *Handlers) GenCharacterState(e *am.Event) {}
func (h *Handlers) GenCharacterExit(e *am.Event) bool { return true }
func (h *Handlers) GenCharacterEnd(e *am.Event) {}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "starter"

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

// ArgsRpc will be available in the REPL.
var ArgsRpc = []am.ArgsApi{}

// A is an args struct common for all state handlers.
type A struct {
	Args `json:"-"`

	// Return chan.
	ReturnCh chan<- []string
}

func (A) ArgsState() string {
	return am.StateAny
}

// ----- per-state typed args

type ACheckingMenuRefs struct {
	Args `json:"-"`

	// TODO fields for CheckingMenuRefs
}

func (ACheckingMenuRefs) ArgsState() string {
	return ss.CheckingMenuRefs
}
