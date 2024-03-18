// Based on https://en.wikipedia.org/wiki/Nondeterministic_finite_automaton
//
//=== RUN   TestRegexp
//=== RUN   TestRegexp/test_101010_(OK)
//[state] +Start +StepX
//[state] +Input
//[extern:InputState] input: 1
//[state] +Input
//[extern:InputState] input: 0
//[state] +Input
//[extern:InputState] input: 1
//[state] +Step0 -StepX
//[state] +Input
//[extern:InputState] input: 0
//[state] +Step1 -Step0
//[state] +Input
//[extern:InputState] input: 1
//[state] +Step2 -Step1
//[state] +Input
//[extern:InputState] input: 0
//[state] +Step3 -Step2
//[state] -Start
//--- PASS: TestRegexp (0.00s)
//    --- PASS: TestRegexp/test_101010_(OK) (0.00s)
//PASS

package nfa

import (
	"context"
	"testing"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// state enum pkg

var (
	states = am.Struct{
		// input states
		Input: {Multi: true},

		// action states
		Start: {Add: am.S{StepX}},

		// "state" states
		StepX: {Remove: groupSteps},
		Step0: {Remove: groupSteps},
		Step1: {Remove: groupSteps},
		Step2: {Remove: groupSteps},
		Step3: {Remove: groupSteps},
	}

	groupSteps = am.S{StepX, Step0, Step1, Step2, Step3}

	Start = "Start"
	Input = "Input"
	StepX = "StepX"
	Step0 = "Step0"
	Step1 = "Step1"
	Step2 = "Step2"
	Step3 = "Step3"

	Names = am.S{Start, Input, StepX, Step0, Step1, Step2, Step3, am.Exception}
)

// handlers

type Regexp struct {
	input  string
	cursor int
}

func (t *Regexp) StartState(e *am.Event) {
	mach := e.Machine

	// reset
	t.input = e.Args["input"].(string)
	t.cursor = 0
	mach.Add1(StepX, nil)

	// jump out of the queue
	go func() {
		// TODO use mach.WhenQueueEnds()
		<-mach.When1(StepX, nil)
		// needed for the queue lock to release
		time.Sleep(1 * time.Millisecond)

		for _, c := range t.input {
			switch c {

			case '0':
				mach.Add1(Input, am.A{"input": "0"})

			case '1':
				mach.Add1(Input, am.A{"input": "1"})
			}

			t.cursor++
		}

		mach.Remove1(Start, nil)
	}()
}

func (t *Regexp) InputState(e *am.Event) {
	mach := e.Machine
	input := e.Args["input"].(string)
	mach.Log("input: %s", input)

	if input == "0" {
		t.input0(mach)
	} else {
		t.input1(mach)
	}
}

func (t *Regexp) input0(mach *am.Machine) {
	switch mach.Switch(groupSteps...) {
	case StepX:
		mach.Add1(StepX, nil)

	case Step0:
		mach.Add1(Step1, nil)

	case Step1:
		mach.Add1(Step2, nil)

	case Step2:
		mach.Add1(Step3, nil)
	}
}

func (t *Regexp) input1(mach *am.Machine) {
	switch mach.Switch(groupSteps...) {
	case StepX:
		// TODO should use CanAdd and State0Enter
		if t.cursor+3 == len(t.input)-1 {
			mach.Add1(Step0, nil)
		} else {
			mach.Add1(StepX, nil)
		}

	case Step0:
		mach.Add1(Step1, nil)

	case Step1:
		mach.Add1(Step2, nil)

	case Step2:
		mach.Add1(Step3, nil)
	}
}

// example

func TestRegexp(t *testing.T) {
	var err error
	mach := am.New(context.Background(), states, &am.Opts{
		ID:                   "regexp",
		DontPanicToException: true,
		DontLogID:            true,
		LogLevel:             am.LogChanges,
		// LogLevel:       am.LogOps,
		// LogLevel:       am.LogDecisions,
		HandlerTimeout: time.Minute,
	})

	_ = mach.BindHandlers(&Regexp{})
	err = mach.VerifyStates(Names)
	if err != nil {
		t.Fatal(err)
	}

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
			mach.Add1(Start, am.A{
				"input": tt.input,
			})

			// wait for Start to finish
			<-mach.WhenNot1(Start, nil)

			// assert
			if tt.expect && mach.Not1(Step3) {
				t.Fatal("Expected Step3")
			} else if !tt.expect && mach.Is1(Step3) {
				t.Fatal("Didn't expect Step3")
			}
		})
	}
}
