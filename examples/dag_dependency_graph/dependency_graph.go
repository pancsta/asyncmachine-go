// Based on https://en.wikipedia.org/wiki/Dependency_graph
//
// This example shows how to construct both sync and async DAG dependency graphs using Require and Auto.
//
// It can be used to e.g. resolve blocking code dependencies in order.

package main

import (
	"time"

	"github.com/joho/godotenv"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func init() {
	// load .env
	_ = godotenv.Load()

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetLogLevel(am.LogChanges)
}

func main() {
	depGraph()
	asyncDepGraph()
}

// SYNC
func depGraph() {
	// init the state machine
	mach := am.New(nil, am.Struct{
		"A": {
			Auto:    true,
			Require: am.S{"B", "C"},
		},
		"B": {
			Auto:    true,
			Require: am.S{"D"},
		},
		"C":     {Auto: true},
		"D":     {Auto: true},
		"Start": {},
	}, &am.Opts{LogLevel: am.LogChanges, ID: "sync"})
	amhelp.MachDebugEnv(mach)
	_ = mach.BindHandlers(&handlers{})
	mach.Add1("Start", nil)
}

type handlers struct{}

func (h *handlers) AState(e *am.Event) {
	println("A ok")
}

func (h *handlers) BState(e *am.Event) {
	println("B ok")
}

func (h *handlers) CState(e *am.Event) {
	println("C ok")
}

func (h *handlers) DState(e *am.Event) {
	println("D ok")
}

// ASYNC

func asyncDepGraph() {
	// init the state machine
	mach := am.New(nil, am.Struct{
		"AInit": {
			Auto:    true,
			Require: am.S{"B", "C"},
		},
		"A": {
			Require: am.S{"B", "C"},
		},
		"BInit": {
			Auto:    true,
			Require: am.S{"D"},
		},
		"B": {
			Require: am.S{"D"},
		},
		"CInit": {Auto: true},
		"C":     {},
		"DInit": {Auto: true},
		"D":     {},

		"Start": {},
	}, &am.Opts{LogLevel: am.LogChanges, ID: "async"})
	amhelp.MachDebugEnv(mach)
	_ = mach.BindHandlers(&asyncHandlers{})
	mach.Add1("Start", nil)
	<-mach.When1("A", nil)
}

type asyncHandlers struct{}

func (h *asyncHandlers) AInitState(e *am.Event) {
	// unblock
	go func() {
		// block
		time.Sleep(100 * time.Millisecond)
		// next
		e.Machine().Add1("A", nil)
	}()
}

func (h *asyncHandlers) BInitState(e *am.Event) {
	// unblock
	go func() {
		// block
		time.Sleep(100 * time.Millisecond)
		// next
		e.Machine().Add1("B", nil)
	}()
}

func (h *asyncHandlers) CInitState(e *am.Event) {
	// unblock
	go func() {
		// block
		time.Sleep(100 * time.Millisecond)
		// next
		e.Machine().Add1("C", nil)
	}()
}

func (h *asyncHandlers) DInitState(e *am.Event) {
	// unblock
	go func() {
		// block
		time.Sleep(100 * time.Millisecond)
		// next
		e.Machine().Add1("D", nil)
	}()
}

func (h *asyncHandlers) AState(e *am.Event) {
	println("A ok")
}

func (h *asyncHandlers) BState(e *am.Event) {
	println("B ok")
}

func (h *asyncHandlers) CState(e *am.Event) {
	println("C ok")
}

func (h *asyncHandlers) DState(e *am.Event) {
	println("D ok")
}
