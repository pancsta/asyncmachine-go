package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// States map defines relations and properties of states.
var States = am.Struct{

	// ///// Input events

	ClientMsg:       {Multi: true},
	ConnectEvent:    {Multi: true},
	DisconnectEvent: {Multi: true},

	// user scrolling tx / steps
	UserFwd: {
		Add:    S{Fwd},
		Remove: GroupPlaying,
	},
	UserBack: {
		Add:    S{Back},
		Remove: GroupPlaying,
	},
	UserFwdStep: {
		Add:     S{FwdStep},
		Require: S{ClientSelected},
		Remove:  SAdd(GroupPlaying, S{LogUserScrolled}),
	},
	UserBackStep: {
		Add:     S{BackStep},
		Require: S{ClientSelected},
		Remove:  SAdd(GroupPlaying, S{LogUserScrolled}),
	},

	// ///// Read-only states (e.g. UI)

	// focus group

	TreeFocused:          {Remove: GroupFocused},
	LogFocused:           {Remove: GroupFocused},
	ClientListFocused:    {Remove: GroupFocused},
	TimelineTxsFocused:   {Remove: GroupFocused},
	TimelineStepsFocused: {Remove: GroupFocused},
	MatrixFocused:        {Remove: GroupFocused},
	DialogFocused:        {Remove: GroupFocused},
	Toolbar1Focused:      {Remove: GroupFocused},
	Toolbar2Focused:      {Remove: GroupFocused},
	LogReaderFocused: {
		Require: S{LogReaderVisible},
		Remove:  GroupFocused,
	},
	AddressFocused: {Remove: GroupFocused},

	TimelineHidden:      {Require: S{TimelineStepsHidden}},
	TimelineStepsHidden: {},
	NarrowLayout: {
		Require: S{Ready},
		Remove:  S{ClientListVisible},
	},
	ClientListVisible: {
		Require: S{Ready},
		Auto:    true,
	},
	StateNameSelected:     {Require: S{ClientSelected}},
	TimelineStepsScrolled: {Require: S{ClientSelected}},
	HelpDialog:            {Remove: GroupDialog},
	ExportDialog: {
		Require: S{ClientSelected},
		Remove:  GroupDialog,
	},
	LogUserScrolled: {
		Remove: S{Playing, TailMode},
		// TODO remove the requirement once its possible to go back
		//  to timeline-scroll somehow
		Require: S{LogFocused},
	},
	Ready: {Require: S{Start}},
	// TODO should activate FiltersFocused
	FilterAutoTx:      {},
	FilterCanceledTx:  {},
	FilterEmptyTx:     {},
	FilterSummaries:   {},
	FilterHealthcheck: {},

	// ///// Actions

	Start: {Add: S{FilterSummaries, FilterHealthcheck, FilterEmptyTx}},
	Healthcheck: {
		Multi:   true,
		Require: S{Start},
	},
	GcMsgs: {Remove: S{SelectingClient, SwitchedClientTx, ScrollToTx,
		ScrollToMutTx}},
	TreeLogView: {
		Auto:    true,
		Require: S{Start},
		Remove:  SAdd(GroupViews, S{TreeMatrixView, MatrixView, MatrixRain}),
	},
	MatrixView:     {Remove: GroupViews},
	TreeMatrixView: {Remove: GroupViews},
	TailMode: {
		Require: S{ClientSelected},
		Remove:  SAdd(GroupPlaying, S{LogUserScrolled}),
	},
	Playing: {
		Require: S{ClientSelected},
		Remove:  SAdd(GroupPlaying, S{LogUserScrolled}),
	},
	Paused: {
		Auto:    true,
		Require: S{ClientSelected},
		Remove:  GroupPlaying,
	},
	ToggleTool: {},
	SwitchingClientTx: {
		Require: S{Ready},
		Remove:  GroupSwitchedClientTx,
	},
	SwitchedClientTx: {
		Require: S{Ready},
		Remove:  GroupSwitchedClientTx,
	},
	ScrollToMutTx: {Require: S{ClientSelected}},
	// TODO depend on a common Matrix view
	MatrixRain: {},
	LogReaderVisible: {
		Auto:    true,
		Require: S{TreeLogView, LogReaderEnabled},
	},
	LogReaderEnabled: {},
	UpdateLogReader:  {Require: S{LogReaderEnabled}},

	// tx / steps back / fwd

	Fwd: {
		Require: S{ClientSelected},
	},
	Back: {
		Require: S{ClientSelected},
	},
	FwdStep: {
		Require: S{ClientSelected},
	},
	BackStep: {
		Require: S{ClientSelected},
	},

	ScrollToTx: {
		Require: S{ClientSelected},
		Remove:  S{TailMode, Playing, TimelineStepsScrolled},
	},
	ScrollToStep: {
		Require: S{ClientSelected},
		Remove:  S{TailMode, Playing},
	},

	// client selection

	SelectingClient: {
		Require: S{Start},
		Remove:  S{ClientSelected},
	},
	ClientSelected: {
		Require: S{Start},
		Remove:  S{SelectingClient},
	},
	RemoveClient: {Require: S{ClientSelected}},

	SetCursor: {
		Multi:   true,
		Require: S{Ready},
	},
	GraphsScheduled: {
		Multi:   true,
		Require: S{Ready},
	},
	GraphsRendering: {
		Require: S{Ready},
	},

	InitClient: {Multi: true},
}

// Groups of states.

var (
	GroupFocused = S{
		TreeFocused, LogFocused, TimelineTxsFocused,
		TimelineStepsFocused, ClientListFocused, MatrixFocused, DialogFocused,
		Toolbar1Focused, Toolbar2Focused, LogReaderFocused, AddressFocused,
	}
	GroupPlaying = S{
		Playing, Paused, TailMode,
	}
	GroupDialog = S{
		HelpDialog, ExportDialog,
	}
	GroupViews = S{
		MatrixView, TreeLogView, TreeMatrixView,
	}
	GroupSwitchedClientTx = S{
		SwitchingClientTx, SwitchedClientTx,
	}
	GroupFilters = S{
		FilterAutoTx, FilterCanceledTx, FilterEmptyTx, FilterHealthcheck,
	}
)

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	// TODO rename to StructureFocused
	TreeFocused          = "TreeFocused"
	LogFocused           = "LogFocused"
	TimelineTxsFocused   = "TimelineTxsFocused"
	TimelineStepsFocused = "TimelineStepsFocused"
	TimelineHidden       = "TimelineHidden"
	TimelineStepsHidden  = "TimelineStepsHidden"
	MatrixFocused        = "MatrixFocused"
	DialogFocused        = "DialogFocused"
	Toolbar1Focused      = "Toolbar1Focused"
	Toolbar2Focused      = "Toolbar2Focused"
	LogReaderFocused     = "LogReaderFocused"
	AddressFocused       = "AddressFocused"
	// ClientListFocused is client list focused.
	// TODO rename to ClientListFocused
	ClientListFocused = "ClientListFocused"

	TimelineStepsScrolled = "TimelineStepsScrolled"
	ClientMsg             = "ClientMsg"
	// StateNameSelected states that a state name is selected somehwere in the
	// tree (and possibly other places).
	// TODO support a list of states
	StateNameSelected = "StateNameSelected"
	// NarrowLayout activates when resized or started in a narrow TUI viewport.
	NarrowLayout      = "NarrowLayout"
	ClientListVisible = "ClientListVisible"
	Start             = "Start"
	Healthcheck       = "Healthcheck"
	GcMsgs            = "GcMsgs"
	Playing           = "Playing"
	Paused            = "Paused"
	// TailMode always shows the latest transition
	TailMode = "TailMode"
	// UserFwd is a user generated event
	UserFwd = "UserFwd"
	// Fwd moves to the next transition
	Fwd = "Fwd"
	// UserBack is a user generated event
	UserBack = "UserBack"
	// Back moves to the previous transition
	Back = "Back"
	// UserFwdStep is a user generated event
	UserFwdStep = "UserFwdStep"
	// UserBackStep is a user generated event
	UserBackStep = "UserBackStep"
	// FwdStep moves to the next transition's steps
	FwdStep = "FwdStep"
	// BackStep moves to the previous transition's steps
	BackStep         = "BackStep"
	ConnectEvent     = "ConnectEvent"
	DisconnectEvent  = "DisconnectEvent"
	RemoveClient     = "RemoveClient"
	ClientSelected   = "ClientSelected"
	SelectingClient  = "SelectingClient"
	HelpDialog       = "HelpDialog"
	ExportDialog     = "ExportDialog"
	MatrixView       = "MatrixView"
	TreeLogView      = "TreeLogView"
	TreeMatrixView   = "TreeMatrixView"
	LogReaderVisible = "LogReaderVisible"
	LogReaderEnabled = "LogReaderEnabled"
	UpdateLogReader  = "UpdateLogReader"
	LogUserScrolled  = "LogUserScrolled"
	// ScrollToTx scrolls to a specific transition.
	ScrollToTx   = "ScrollToTx"
	ScrollToStep = "ScrollToStep"
	// Ready is an async result of start
	Ready             = "Ready"
	FilterCanceledTx  = "FilterCanceledTx"
	FilterAutoTx      = "FilterAutoTx"
	FilterHealthcheck = "FilterHealthcheck"
	// FilterEmptyTx is a filter for txes which didn't change state and didn't
	// run any self handler either
	FilterEmptyTx   = "FilterEmptyTx"
	FilterSummaries = "FilterSummaries"
	ToggleTool      = "ToggleTool"
	// SwitchingClientTx switches to the given client and scrolls to the given
	// transaction (1-based tx index). Accepts Client.id and Client.cursorTx.
	SwitchingClientTx = "SwitchingClientTx"
	// SwitchedClientTx is a completed SwitchingClientTx.
	SwitchedClientTx = "SwitchedClientTx"
	// ScrollToMutTx scrolls to a transition which mutated or called the
	// passed state,
	// If fwd is true, it scrolls forward, otherwise backwards.
	ScrollToMutTx   = "ScrollToMutTx"
	MatrixRain      = "MatrixRain"
	SetCursor       = "SetCursor"
	GraphsRendering = "GraphsRendering"
	GraphsScheduled = "GraphsScheduled"
	InitClient      = "InitClient"
)

// Names of all the states (pkg enum).

// Names is an ordered list of all the state names.
var Names = S{

	// /// Input events

	ClientMsg,
	ConnectEvent,
	DisconnectEvent,

	// user scrolling
	UserFwd,
	UserBack,
	UserFwdStep,
	UserBackStep,

	// /// External state (eg UI)

	TreeFocused,
	LogFocused,
	LogReaderFocused,
	AddressFocused,
	ClientListFocused,
	TimelineTxsFocused,
	TimelineStepsFocused,
	TimelineHidden,
	TimelineStepsHidden,
	Toolbar1Focused,
	Toolbar2Focused,
	MatrixFocused,
	DialogFocused,
	StateNameSelected,
	NarrowLayout,
	ClientListVisible,
	HelpDialog,
	ExportDialog,
	LogUserScrolled,
	Ready,
	TimelineStepsScrolled,

	// /// Actions

	Start,
	Healthcheck,
	GcMsgs,
	TreeLogView,
	MatrixView,
	TreeMatrixView,
	TailMode,
	Playing,
	Paused,
	FilterAutoTx,
	FilterCanceledTx,
	FilterEmptyTx,
	FilterSummaries,
	FilterHealthcheck,
	ToggleTool,
	SwitchingClientTx,
	SwitchedClientTx,
	ScrollToMutTx,
	MatrixRain,
	LogReaderVisible,
	LogReaderEnabled,
	UpdateLogReader,

	// tx / steps back / fwd
	Fwd,
	Back,
	FwdStep,
	BackStep,

	ScrollToTx,
	ScrollToStep,

	// client
	ClientSelected,
	SelectingClient,
	RemoveClient,

	SetCursor,
	GraphsRendering,
	GraphsScheduled,

	InitClient,

	am.Exception,
}

// #endregion
