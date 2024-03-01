package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// enum of all the state names
const (
	TreeFocused          string = "TreeFocused"
	LogFocused           string = "LogFocused"
	TimelineTxsFocused   string = "TimelineTxsFocused"
	TimelineStepsFocused string = "TimelineStepsFocused"
	ClientMsg            string = "ClientMsg"
	KeystrokeInput       string = "KeystrokeInput"
	Paused               string = "Paused"
	StateNameSelected    string = "StateNameSelected"
	Init                 string = "Init"
	Playing              string = "Playing"
	Fwd                  string = "Fwd"
	Rewind               string = "Rewind"
	FwdStep              string = "FwdStep"
	RewindStep           string = "RewindStep"
	ClientConnected      string = "ClientConnected"
	LiveView             string = "LiveView"
	HelpScreen           string = "HelpScreen"
	Exception            string = "Exception"
)

var Names = am.S{
	ClientMsg, KeystrokeInput, TreeFocused, LogFocused, TimelineTxsFocused,
	TimelineStepsFocused, StateNameSelected, ClientConnected, Init, LiveView,
	Playing, Paused, Fwd, Rewind, FwdStep, RewindStep, HelpScreen, Exception,
}

var groupFocused = am.S{
	TreeFocused, LogFocused, TimelineTxsFocused,
	TimelineStepsFocused,
}

var GroupPlaying = am.S{
	Playing, Paused, LiveView,
}

var States = am.States{
	// Input events
	ClientMsg: {
		Multi: true,
	},
	KeystrokeInput: {
		Multi: true,
	},

	// State (external)
	TreeFocused: {
		Remove: groupFocused,
	},
	LogFocused: {
		Remove: groupFocused,
	},
	TimelineTxsFocused: {
		Remove: groupFocused,
	},
	TimelineStepsFocused: {
		Remove: groupFocused,
	},
	StateNameSelected: {
		Multi: true,
	},
	ClientConnected: {},

	// Actions
	Init: {
		Add: am.S{LiveView},
	},
	LiveView: {
		Remove: GroupPlaying,
	},
	Playing: {
		Remove: GroupPlaying,
	},
	Paused: {
		Auto:   true,
		Remove: GroupPlaying,
	},
	Fwd: {},
	Rewind: {
		Remove: am.S{LiveView},
	},
	FwdStep: {
		Remove: am.S{LiveView},
	},
	RewindStep: {},
	HelpScreen: {},
}
