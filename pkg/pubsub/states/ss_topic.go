// Package states contains a stateful schema-v2 for Topic.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
)

// TopicStatesDef contains all the states of the Topic state machine.
type TopicStatesDef struct {
	*am.StatesBase

	ErrJoining   string
	ErrListening string

	// status

	Joining string
	Joined  string
	Started string
	// Updates for unknown peers
	MissPeersByUpdates string
	// Heard about peers, but doesnt know them
	MissPeersByGossip string
	// Other peers have later clock values for some known peers
	MissUpdatesByGossip string

	// events

	PeerJoined string
	PeerLeft string
	MsgInfo  string
	MsgBye   string
	// MsgUpdates received
	MsgUpdates string
	// MsgReqInfo received
	MsgReqInfo string
	// MsgReqUpdates received
	MsgReqUpdates string
	// MsgReceived measn a general msg was sent to the channel.
	MsgReceived string

	// actions

	// ListMachines is a request to return the filtered list of connected machines
	// via chan, as [rpc.Worker].
	ListMachines string
	SendMsg      string
	// Sends MsgInfo to specific peers or the channel.
	SendInfo string

	// inherit from BasicStatesDef
	*ss.BasicStatesDef
	// inherit from ConnectedStatesDef
	*ss.ConnectedStatesDef
	// inherit from DisposedStatesDef
	*ss.DisposedStatesDef
}

// TopicGroupsDef contains all the state groups Topic state machine.
type TopicGroupsDef struct {
	*ss.ConnectedGroupsDef
}

// TopicStruct represents all relations and properties of TopicStates.
var TopicStruct = StructMerge(
	// inherit from BasicStruct
	ss.BasicStruct,
	// inherit from ConnectedStruct
	ss.ConnectedStruct,
	// inherit from DisposedStruct
	ss.DisposedStruct,
	am.Struct{

		// errors

		ssT.ErrJoining:   {Require: S{Exception}},
		ssT.ErrListening: {Require: S{Exception}},

		// inherited

		ssT.Started: {Require: S{ssT.Start}},
		ssT.Joining: {
			Require: S{ssT.Connected},
			Remove:  S{ssT.Joined},
		},
		ssT.Joined: {
			Require: S{ssT.Connected},
			Remove:  S{ssT.Joining},
		},
		ssT.Ready: {
			Auto:    true,
			Require: S{ssT.Joined},
		},

		// external events

		ssT.PeerJoined: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.PeerLeft: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.MsgInfo: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.MsgBye: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.MsgUpdates: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.MsgReqInfo: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.MsgReqUpdates: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.MsgReceived: {
			Multi:   true,
			Require: S{ssT.Joined},
		},

		// status events

		ssT.MissPeersByUpdates: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.MissPeersByGossip: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.MissUpdatesByGossip: {
			Multi:   true,
			Require: S{ssT.Joined},
		},

		// actions

		ssT.ListMachines: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.SendMsg: {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.SendInfo:  {
			Multi:   true,
			Require: S{ssT.Joined},
		},
		ssT.Heartbeat: {Require: S{ssT.Joined}},
	})

// EXPORTS AND GROUPS

var (
	ssT = am.NewStates(TopicStatesDef{})
	sgT = am.NewStateGroups(TopicGroupsDef{}, ss.ConnectedGroups)

	// TopicStates contains all the states for the Topic machine.
	TopicStates = ssT
	// TopicGroups contains all the state groups for the Topic machine.
	TopicGroups = sgT
)

// Hnadlers

type TopicExternalHandlers interface {
}