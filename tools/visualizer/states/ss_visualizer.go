// Package states contains a stateful schema-v2 for Visualizer.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

// VisualizerStatesDef contains all the states of the Visualizer state machine.
type VisualizerStatesDef struct {
	*am.StatesBase

	ConnectEvent    string
	ClientMsg       string
	DisconnectEvent string

	// DbInit string
	InitClient string

	GenMermaid string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from DisposedStatesDef
	*ss.DisposedStatesDef
}

// VisualizerGroupsDef contains all the state groups Visualizer state machine.
type VisualizerGroupsDef struct {
}

// VisualizerStruct represents all relations and properties of VisualizerStates.
var VisualizerStruct = StructMerge(
	// inherit from BasicStruct
	ss.BasicStruct,
	// inherit from DisposedStruct
	ss.DisposedStruct,
	am.Struct{
		// ssV.DbInit: {Require: S{ssV.Start}},
		// ssV.Start:  {Add: S{ssV.DbInit}},
		// ssV.Ready:  {Require: S{ssV.DbInit}},

		ssV.ConnectEvent: {
			Multi:   true,
			Require: S{ssV.Start}},
		ssV.ClientMsg: {
			Multi:   true,
			Require: S{ssV.Start}},
		ssV.DisconnectEvent: {
			Multi:   true,
			Require: S{ssV.Start}},
		ssV.InitClient: {
			Multi:   true,
			Require: S{ssV.Start}},

		ssV.GenMermaid: {Require: S{ssV.Ready}},
	})

// EXPORTS AND GROUPS

var (
	ssV = am.NewStates(VisualizerStatesDef{})
	sgV = am.NewStateGroups(VisualizerGroupsDef{})

	// VisualizerStates contains all the states for the Visualizer machine.
	VisualizerStates = ssV
	// VisualizerGroups contains all the state groups for the Visualizer machine.
	VisualizerGroups = sgV
)
