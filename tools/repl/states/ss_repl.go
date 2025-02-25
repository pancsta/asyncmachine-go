// Package states contains a stateful schema-v2 for Repl.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

// ReplStatesDef contains all the states of the Repl state machine.
type ReplStatesDef struct {
	*am.StatesBase

	ErrSyntax string

	// CONNECTION

	Disconnected   string
	Connecting     string
	Connected      string
	ConnectedFully string
	Disconnecting  string

	// PIPES

	RpcConn    string
	RpcDisconn string

	// REPL CMDS

	CmdAdd         string
	CmdRemove      string
	CmdGroupAdd    string
	CmdGroupRemove string
	CmdList        string
	CmdScript      string
	CmdWhenTime    string
	CmdWhen        string
	CmdWhenNot     string
	CmdInspect     string
	CmdStatus      string

	// REST

	// REPL is running in a TUI mode
	ReplMode string
	// List fully connected machines, with filters.
	ListMachines string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from DisposedStatesDef
	*ss.DisposedStatesDef
}

// ReplGroupsDef contains all the state groups Repl state machine.
type ReplGroupsDef struct {
	*ss.ConnectedGroupsDef

	Cmds S
}

// ReplStruct represents all relations and properties of ReplStates.
var ReplStruct = StructMerge(
	// inherit from BasicStruct
	ss.BasicStruct,
	// inherit from DisposedStruct
	ss.DisposedStruct,
	am.Struct{

		ssC.ErrSyntax: {},

		// CONNECTION

		ssC.Disconnected: {
			Remove: S{ssC.Connecting, ssC.ConnectedFully, ssC.Disconnecting},
		},
		ssC.Connecting: {
			Require: S{ssC.Start},
			Remove:  S{ssC.Disconnecting},
		},
		ssC.Connected: {
			Require: S{ssC.Start},
			Remove:  S{ssC.Disconnected},
		},
		ssC.ConnectedFully: {
			Require: S{ssC.Connected},
			Remove:  S{ssC.Disconnected},
		},
		ssC.Disconnecting: {
			Remove: S{ssC.ConnectedFully, ssC.Connected, ssC.Connecting},
		},

		// PIPES

		ssC.RpcConn:    {Multi: true},
		ssC.RpcDisconn: {Multi: true},

		// CMDS

		ssC.CmdAdd: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdRemove: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdGroupAdd: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdGroupRemove: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdList: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdScript: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdWhenTime: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdWhen: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdWhenNot: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdInspect: {
			Multi:   true,
			Require: S{ssC.Connected},
		},
		ssC.CmdStatus: {
			Multi:   true,
			Require: S{ssC.Connected},
		},

		// STATUS

		ssC.ReplMode: {Require: S{ssC.Start}},

		// ACTIONS

		ssC.ListMachines: {
			Multi:   true,
			Require: S{ssC.Start},
		},
	})

// EXPORTS AND GROUPS

var (
	ssC = am.NewStates(ReplStatesDef{})
	sgC = am.NewStateGroups(ReplGroupsDef{
		Cmds: S{ssC.CmdAdd, ssC.CmdRemove, ssC.CmdList, ssC.CmdScript,
			ssC.CmdWhenTime, ssC.CmdWhen, ssC.CmdWhenNot, ssC.CmdInspect,
			ssC.CmdStatus},
	}, ss.ConnectedGroups)

	// ReplStates contains all the states for the Repl machine.
	ReplStates = ssC
	// ReplGroups contains all the state groups for the Repl machine.
	ReplGroups = sgC
)
