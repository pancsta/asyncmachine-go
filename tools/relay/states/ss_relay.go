// Package states contains a stateful schema-v2 for Relay.
// Bootstrapped with am-gen. Edit manually or re-gen & merge.
package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

//

// RELAY

//

// RelayStatesDef contains all the states of the Relay state machine.
type RelayStatesDef struct {
	*am.StatesBase

	HttpStarting string
	HttpReady    string
	// New WebSocket to TCP-listen tunnel req
	WsTunListenConn    string
	WsTunListenDisconn string
	WsDialConn         string
	WsDialDisconn      string

	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
	// inherit from DisposedStatesDef
	*ssam.DisposedStatesDef
	// inherit from [ssdbg.ServerStatesDef]
	*ssdbg.ServerStatesDef
}

// RelayGroupsDef contains all the state groups Relay state machine.
type RelayGroupsDef struct {
}

// RelaySchema represents all relations and properties of RelayStates.
var RelaySchema = SchemaMerge(
	// inherit from BasicStruct
	ssam.BasicSchema,
	// inherit from DisposedStruct
	ssam.DisposedSchema,
	// inherit from ServerSchema
	ssdbg.ServerSchema,
	am.Schema{
		ss.HttpStarting: {
			Require: S{ss.Start},
			Remove:  S{ss.HttpReady},
		},
		ss.HttpReady: {
			Require: S{ss.Start},
			Remove:  S{ss.HttpStarting},
		},
		ss.WsTunListenConn: {
			Multi:   true,
			Require: S{ss.HttpReady},
		},
		ss.WsTunListenDisconn: {
			Multi:   true,
			Require: S{ss.HttpReady},
		},
		ss.WsDialConn: {
			Multi:   true,
			Require: S{ss.HttpReady},
		},
		ss.WsDialDisconn: {
			Multi:   true,
			Require: S{ss.HttpReady},
		},
	})

// EXPORTS AND GROUPS

var (
	ss = am.NewStates(RelayStatesDef{})
	sg = am.NewStateGroups(RelayGroupsDef{})

	// RelayStates contains all the states for the Relay machine.
	RelayStates = ss
	// RelayGroups contains all the state groups for the Relay machine.
	RelayGroups = sg
)

//

// WEBSOCKET TCP TUNNEL

//

// WsTcpTunStatesDef contains all the states of the WsTcpTun state machine.
// This is a one-way flow which ends in TcpAccepted, then disposes.
type WsTcpTunStatesDef struct {
	*am.StatesBase

	// err caused by TCP client
	ErrClient string
	// err caused by WebSocket / TCP server
	ErrServer string

	// WebSocket connection OK
	WebSocket string
	// start listening on TCP port
	TcpListen string
	// TCP port listening
	TcpListening string
	// TCP client accepted
	TcpAccepted string

	// inherit from DisposedStatesDef
	*ssam.DisposedStatesDef
	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
}

// WsTcpTunGroupsDef contains all the state groups WsTcpTun state machine.
type WsTcpTunGroupsDef struct {
}

// WsTcpTunSchema represents all relations and properties of WsTcpTunStates.
var WsTcpTunSchema = SchemaMerge(
	// inherit from DisposedStruct
	ssam.DisposedSchema,
	// inherit from BasicSchema
	ssam.BasicSchema,
	am.Schema{
		ssW.ErrClient: {
			Add:     S{ssW.Disposing},
			Require: S{Exception},
		},
		ssW.ErrServer: {
			Add:     S{ssW.Disposing},
			Require: S{Exception},
		},

		ssW.WebSocket: {},
		ssW.Start:     {Add: S{ssW.TcpListen, ssW.WebSocket}},
		ssW.TcpListen: {
			Require: S{ssW.WebSocket},
			Remove:  S{ssW.TcpListening},
		},
		ssW.TcpListening: {
			Require: S{ssW.Start, ssW.WebSocket},
			Remove:  S{ssW.TcpListen},
		},
		ssW.TcpAccepted: {
			Add:     S{ssW.Ready},
			Require: S{ssW.TcpListening},
		},

		// inherited

		ssW.Ready: {Require: S{ssW.TcpAccepted}},
	})

// EXPORTS AND GROUPS

var (
	ssW = am.NewStates(WsTcpTunStatesDef{})
	sgW = am.NewStateGroups(WsTcpTunGroupsDef{})

	// WsTcpTunStates contains all the states for the WsTcpTun machine.
	WsTcpTunStates = ssW
	// WsTcpTunGroups contains all the state groups for the WsTcpTun machine.
	WsTcpTunGroups = sgW
)
