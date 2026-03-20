//go:build !tinygo

// Package states provides reusable state definitions.
//
// - basic
// - connected
// - connection pool
// - disposed
//
//go:generate go run ./gen
package states

import (
	_ "embed"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

//go:embed global/states_utils.go
var StatesUtilsFile string

// BasicStatesDef contains all the basic states.
type BasicStatesDef struct {
	*am.StatesBase

	// ErrNetwork indicates a generic network error.
	ErrNetwork string
	// ErrHandlerTimeout indicates one of the state machine handlers has timed
	// out.
	ErrHandlerTimeout string

	// Start indicates the machine should be working. Removing start can force
	// stop the machine.
	Start string
	// Ready indicates the machine meets criteria to perform work.
	Ready string
	// Healthcheck is a periodic request making sure that the machine is still
	// alive.
	Healthcheck string
	// Heartbeat is a periodic state that ensures the integrity of the machine.
	Heartbeat string
}

var BasicSchema = am.Schema{
	// Errors

	ssB.Exception: {Multi: true},
	ssB.ErrNetwork: {
		Multi:   true,
		Add:     S{Exception},
		Require: S{Exception},
	},
	ssB.ErrHandlerTimeout: {
		Multi:   true,
		Add:     S{Exception},
		Require: S{Exception},
	},

	// Basics

	ssB.Start:       {},
	ssB.Ready:       {Require: S{ssB.Start}},
	ssB.Healthcheck: {Multi: true},
	ssB.Heartbeat:   {},
}

// EXPORTS AND GROUPS

var (
	ssB = am.NewStates(BasicStatesDef{})

	// BasicStates contains all the states for the Basic machine.
	BasicStates = ssB
)
