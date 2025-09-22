package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// States map defines relations and properties of states.
var States = am.Schema{

	// Errors

	ErrDiagrams: {
		Multi:   true,
		Require: S{Exception},
		Remove:  S{DiagramsScheduled, DiagramsRendering},
	},
	ErrWeb: {
		Multi:   true,
		Require: S{Exception},
	},

	// ///// Input events

	ClientMsg:       {Multi: true},
	ConnectEvent:    {Multi: true},
	DisconnectEvent: {Multi: true},
	WebReq: {
		Multi:   true,
		Require: S{Start},
	},
	WebSocket: {
		Multi:   true,
		Require: S{Start},
	},

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
	TreeGroupsFocused:    {Remove: GroupFocused},
	LogFocused:           {Remove: GroupFocused},
	ClientListFocused:    {Remove: GroupFocused},
	TimelineTxsFocused:   {Remove: GroupFocused},
	TimelineStepsFocused: {Remove: GroupFocused},
	MatrixFocused:        {Remove: GroupFocused},
	DialogFocused:        {Remove: GroupFocused},
	Toolbar1Focused:      {Remove: GroupFocused},
	Toolbar2Focused:      {Remove: GroupFocused},
	Toolbar3Focused:      {Remove: GroupFocused},
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
	Ready: {
		Require: S{Start},
		Add:     S{UpdateFocus},
	},
	// TODO should activate FiltersFocused
	FilterAutoTx:     {},
	FilterCanceledTx: {},
	FilterEmptyTx:    {},
	FilterSummaries:  {},
	FilterTraces:     {},
	FilterHealth:     {},
	FilterChecks:     {},
	FilterOutGroup:   {},
	Redraw:           {},

	// ///// Actions

	Start: {Add: S{
		FilterSummaries, FilterHealth, FilterEmptyTx, ClientListFocused,
	}},
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
	ToggleTool:  {Remove: S{ToolToggled}},
	ToolToggled: {Remove: S{ToggleTool}},
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
	UpdateLogReader:  {Require: S{Ready, LogReaderEnabled}},
	UpdateFocus: {
		Multi:   true,
		Require: S{Ready},
	},
	AfterFocus: {
		Multi:   true,
		Require: S{Ready},
	},
	ToolRain: {
		Multi:   true,
		Require: S{Ready},
	},

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
	BuildingLog: {
		Require: S{ClientSelected},
		Remove:  S{LogBuilt},
		After:   S{ClientSelected},
	},
	LogBuilt: {
		Require: S{ClientSelected},
		Remove:  S{BuildingLog},
	},

	SetCursor: {
		Multi:   true,
		Require: S{Ready},
	},
	SetGroup: {
		Multi:   true,
		Require: S{ClientSelected},
	},
	DiagramsScheduled: {
		Multi:   true,
		Require: S{Ready},
	},
	DiagramsRendering: {
		Require: S{Ready},
		Remove:  S{DiagramsReady},
	},
	DiagramsReady: {Remove: S{DiagramsRendering}},

	InitClient: {Multi: true},
}

// Groups of states.

var (
	GroupFocused = S{
		AddressFocused, ClientListFocused, TreeGroupsFocused, TreeFocused,

		LogFocused, LogReaderFocused, MatrixFocused,

		TimelineTxsFocused, TimelineStepsFocused,

		Toolbar1Focused, Toolbar2Focused, Toolbar3Focused,

		DialogFocused,
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
		FilterAutoTx, FilterCanceledTx, FilterEmptyTx, FilterHealth, FilterOutGroup,
	}
)

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	// TODO rename to SchemaFocused
	TreeFocused          = "TreeFocused"
	TreeGroupsFocused    = "TreeGroupsFocused"
	LogFocused           = "LogFocused"
	TimelineTxsFocused   = "TimelineTxsFocused"
	TimelineStepsFocused = "TimelineStepsFocused"
	TimelineHidden       = "TimelineHidden"
	TimelineStepsHidden  = "TimelineStepsHidden"
	MatrixFocused        = "MatrixFocused"
	DialogFocused        = "DialogFocused"
	Toolbar1Focused      = "Toolbar1Focused"
	Toolbar2Focused      = "Toolbar2Focused"
	Toolbar3Focused      = "Toolbar3Focused"
	LogReaderFocused     = "LogReaderFocused"
	AddressFocused       = "AddressFocused"
	// ClientListFocused is client list focused.
	// TODO rename to ClientListFocused
	ClientListFocused = "ClientListFocused"

	// Redraw means the UI should be re-drawn.
	Redraw                = "Redraw'"
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
	BackStep           = "BackStep"
	ConnectEvent       = "ConnectEvent"
	DisconnectEvent    = "DisconnectEvent"
	WebReq             = "WebReq"
	WebSocket          = "WebSocket"
	RemoveClient       = "RemoveClient"
	ClientSelected     = "ClientSelected"
	BuildingLog        = "BuildingLog"
	LogBuilt           = "LogBuilt"
	SelectingClient    = "SelectingClient"
	HelpDialog         = "HelpDialog"
	ExportDialog       = "ExportDialog"
	MatrixView         = "MatrixView"
	TreeLogView        = "TreeLogView"
	TreeMatrixView     = "TreeMatrixView"
	LogReaderVisible   = "LogReaderVisible"
	LogReaderEnabled   = "LogReaderEnabled"
	UpdateLogScheduled = "UpdateLogScheduled"
	UpdatingLog        = "UpdatingLog"
	LogUpdated         = "LogUpdated"
	UpdateLogReader    = "UpdateLogReader"
	UpdateFocus        = "UpdateFocus"
	AfterFocus         = "AfterFocus"
	ToolRain           = "ToolRain"
	LogUserScrolled    = "LogUserScrolled"
	// ScrollToTx scrolls to a specific transition.
	ScrollToTx   = "ScrollToTx"
	ScrollToStep = "ScrollToStep"
	// Ready is an async result of start
	Ready            = "Ready"
	FilterCanceledTx = "FilterCanceledTx"
	FilterAutoTx     = "FilterAutoTx"
	FilterHealth     = "FilterHealth"
	FilterChecks     = "FilterChecks"
	// FilterOutGroup filters out txs for states outside the selected group.
	FilterOutGroup = "FilterOutGroup"
	// FilterEmptyTx is a filter for txes which didn't change state and didn't
	// run any self handler either
	FilterEmptyTx   = "FilterEmptyTx"
	FilterSummaries = "FilterSummaries"
	FilterTraces    = "FilterTraces"
	ToggleTool      = "ToggleTool"
	ToolToggled     = "ToolToggled"
	// SwitchingClientTx switches to the given client and scrolls to the given
	// transaction (1-based tx index). Accepts Client.id and Client.cursorTx.
	SwitchingClientTx = "SwitchingClientTx"
	// SwitchedClientTx is a completed SwitchingClientTx.
	SwitchedClientTx = "SwitchedClientTx"
	// ScrollToMutTx scrolls to a transition which mutated or called the
	// passed state,
	// If fwd is true, it scrolls forward, otherwise backwards.
	ScrollToMutTx = "ScrollToMutTx"
	MatrixRain    = "MatrixRain"
	SetCursor     = "SetCursor"
	// TODO merge with SetCursor (better name?)
	SetGroup          = "SetGroup"
	DiagramsScheduled = "DiagramsScheduled"
	DiagramsRendering = "DiagramsRendering"
	DiagramsReady     = "DiagramsReady"
	ErrDiagrams       = "ErrDiagrams"
	ErrWeb            = "ErrWeb"
	InitClient        = "InitClient"
)

// Names of all the states (pkg enum).

// Names is an ordered list of all the state names.
var Names = S{
	am.Exception,

	// /// Input events

	ClientMsg,
	ConnectEvent,
	DisconnectEvent,
	WebReq,
	WebSocket,

	// user scrolling
	UserFwd,
	UserBack,
	UserFwdStep,
	UserBackStep,

	// /// External state (eg UI)

	TreeFocused,
	TreeGroupsFocused,
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
	Toolbar3Focused,
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
	Redraw,

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
	FilterTraces,
	FilterHealth,
	FilterChecks,
	FilterOutGroup,
	ToggleTool,
	ToolToggled,
	SwitchingClientTx,
	SwitchedClientTx,
	ScrollToMutTx,
	MatrixRain,
	LogReaderVisible,
	LogReaderEnabled,
	UpdateLogScheduled,
	UpdatingLog,
	LogUpdated,
	UpdateLogReader,
	UpdateFocus,
	AfterFocus,
	ToolRain,

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
	BuildingLog,
	LogBuilt,

	SetCursor,
	SetGroup,
	DiagramsScheduled,
	DiagramsRendering,
	DiagramsReady,
	ErrDiagrams,
	ErrWeb,

	InitClient,
}

// #endregion
