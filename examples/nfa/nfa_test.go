// Based on https://en.wikipedia.org/wiki/Nondeterministic_finite_automaton
//
// === RUN   TestNfa
// === RUN   TestNfa/test_101010_(OK)
// [state] +StepX +Start
// [state] +Input1
// [state] +Input1Done
// [state] +Input0
// [state] +Input0Done
// [state] +Input1
// [state] +Step0 -StepX
// [state] +Input1Done
// [state] +Input0
// [state] +Step1 -Step0
// [state] +Input0Done
// [state] +Input1
// [state] +Step2 -Step1
// [state] +Input1Done
// [state] +Input0
// [state] +Step3 -Step2
// [state] +Input0Done
// [state] +Ready
// [state] -Start -Input0 -Input0Done -Input1 -Input1Done -Ready
// --- PASS: TestNfa/test_101010_(OK) (0.00s)
// --- PASS: TestNfa (1.01s)
// PASS

package nfa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pancsta/asyncmachine-go/examples/nfa/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var (
	ss = states.NfaStates
	sg = states.NfaGroups
)

func init() {
	// DEBUG
	//
	// - am-dbg is required for TUI debugging, you can go run it
	// - go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// - enable stdout logging
	//
	// amhelp.EnableDebugging(true)
	// amhelp.SemLogger().SetLevel(am.LogOps)
}

// handlers

type Regexp struct {
	*am.ExceptionHandler

	input  string
	cursor int
}

func (r *Regexp) StartState(e *am.Event) {
	r.input = e.Args["input"].(string)
	mach := e.Machine()

	go func() {
		for _, c := range strings.Split(r.input, "") {
			if c == "0" {
				mach.Add1(ss.Input0, nil)
				<-mach.When1(ss.Input0Done, nil)
			} else if c == "1" {
				mach.Add1(ss.Input1, nil)
				<-mach.When1(ss.Input1Done, nil)
			}
		}

		mach.Add1(ss.Ready, nil)
	}()
}

func (r *Regexp) Input0State(e *am.Event) {
	mach := e.Machine()
	defer func() { r.cursor++ }()
	defer mach.Add1(ss.Input0Done, nil)

	switch mach.Switch(sg.Steps) {
	case ss.StepX:
		mach.Add1(ss.StepX, nil)

	case ss.Step0:
		mach.Add1(ss.Step1, nil)

	case ss.Step1:
		mach.Add1(ss.Step2, nil)

	case ss.Step2:
		mach.Add1(ss.Step3, nil)
	}
}

func (r *Regexp) Input1State(e *am.Event) {
	mach := e.Machine()
	defer func() { r.cursor++ }()
	defer mach.Add1(ss.Input1Done, nil)

	switch mach.Switch(sg.Steps) {
	case ss.StepX:
		// TODO should use CanAdd and Step0Enter
		if r.cursor+3 == len(r.input)-1 {
			mach.Add1(ss.Step0, nil)
		} else {
			mach.Add1(ss.StepX, nil)
		}

	case ss.Step0:
		mach.Add1(ss.Step1, nil)

	case ss.Step1:
		mach.Add1(ss.Step2, nil)

	case ss.Step2:
		mach.Add1(ss.Step3, nil)
	}
}

// example

func TestNfa(t *testing.T) {
	ctx := context.Background()

	// tests
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{
			name:   "test 101010 (OK)",
			input:  "101010",
			expect: true,
		},
		{
			name:   "test 1010 (OK)",
			input:  "1010",
			expect: true,
		},
		{
			name:   "test 100010 (NOT OK)",
			input:  "100010",
			expect: false,
		},
	}

	// code
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mach, err := am.NewCommon(ctx, "nfa-"+tt.name, states.NfaSchema, ss.Names(), &Regexp{}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			mach.SemLogger().EnableId(false)
			amhelp.MachDebugEnv(mach)
			mach.Add1(ss.Start, am.A{"input": tt.input})
			<-mach.When1(ss.Ready, nil)

			// assert
			if tt.expect && mach.Not1(ss.Step3) {
				t.Fatal("Expected Step3")
			} else if !tt.expect && mach.Is1(ss.Step3) {
				t.Fatal("Didn't expect Step3")
			}

			mach.Remove1(ss.Start, nil)
		})
	}

	// am-dbg
	time.Sleep(time.Second)
}
