package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// NfaStatesDef contains all the states of the Nfa state machine.
type NfaStatesDef struct {
	*am.StatesBase

	Start string
	Ready string

	Input0     string
	Input0Done string
	Input1     string
	Input1Done string

	StepX string
	Step0 string
	Step1 string
	Step2 string
	Step3 string
}

// NfaGroupsDef contains all the state groups Nfa state machine.
type NfaGroupsDef struct {
	Steps S
}

// NfaStruct represents all relations and properties of NfaStates.
var NfaStruct = am.Struct{

	ssN.Start: {Add: S{ssN.StepX}},
	ssN.Ready: {Require: S{ssN.Start}},

	ssN.Input0: {
		Multi:   true,
		Require: S{ssN.Start},
	},
	ssN.Input0Done: {
		Multi:   true,
		Require: S{ssN.Start},
	},
	ssN.Input1: {
		Multi:   true,
		Require: S{ssN.Start},
	},
	ssN.Input1Done: {
		Multi:   true,
		Require: S{ssN.Start},
	},

	ssN.StepX: {Remove: sgN.Steps},
	ssN.Step0: {Remove: sgN.Steps},
	ssN.Step1: {Remove: sgN.Steps},
	ssN.Step2: {Remove: sgN.Steps},
	ssN.Step3: {Remove: sgN.Steps},
}

// EXPORTS AND GROUPS

var (
	ssN = am.NewStates(NfaStatesDef{})
	sgN = am.NewStateGroups(NfaGroupsDef{
		Steps: S{ssN.StepX, ssN.Step0, ssN.Step1, ssN.Step2, ssN.Step3},
	})

	// NfaStates contains all the states for the Nfa machine.
	NfaStates = ssN
	// NfaGroups contains all the state groups for the Nfa machine.
	NfaGroups = sgN
)
