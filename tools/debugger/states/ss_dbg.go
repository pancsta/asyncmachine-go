package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// S is a type alias for a list of state names.
type S = am.S

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
		Remove:  am.SMerge(GroupPlaying, S{LogUserScrolled}),
	},
	UserBackStep: {
		Add:     S{BackStep},
		Require: S{ClientSelected},
		Remove:  am.SMerge(GroupPlaying, S{LogUserScrolled}),
	},

	// ///// Read-only states (eg UI)

	// focus group

	TreeFocused:          {Remove: GroupFocused},
	LogFocused:           {Remove: GroupFocused},
	SidebarFocused:       {Remove: GroupFocused},
	TimelineTxsFocused:   {Remove: GroupFocused},
	TimelineStepsFocused: {Remove: GroupFocused},
	MatrixFocused:        {Remove: GroupFocused},
	DialogFocused:        {Remove: GroupFocused},
	FiltersFocused:       {Remove: GroupFocused},

	StateNameSelected: {Require: S{ClientSelected}},
	HelpDialog:        {Remove: GroupDialog},
	ExportDialog: {
		Require: S{ClientSelected},
		Remove:  GroupDialog,
	},
	LogUserScrolled:  {Remove: S{Playing, TailMode}},
	Ready:            {Require: S{Start}},
	FilterAutoTx:     {},
	FilterCanceledTx: {},
	FilterEmptyTx:    {},

	// ///// Actions

	Start: {},
	TreeLogView: {
		Auto:   true,
		Remove: GroupViews,
	},
	MatrixView:     {Remove: GroupViews},
	TreeMatrixView: {Remove: GroupViews},
	TailMode: {
		Require: S{ClientSelected},
		Remove:  am.SMerge(GroupPlaying, S{LogUserScrolled}),
	},
	Playing: {
		Require: S{ClientSelected},
		Remove:  am.SMerge(GroupPlaying, S{LogUserScrolled}),
	},
	Paused: {
		Auto:    true,
		Require: S{ClientSelected},
		Remove:  GroupPlaying,
	},
	ToggleFilter: {},

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

	ScrollToTx: {Require: S{ClientSelected}},

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
}

// Groups of mutually exclusive states.

var (
	GroupFocused = S{
		TreeFocused, LogFocused, TimelineTxsFocused,
		TimelineStepsFocused, SidebarFocused, MatrixFocused, DialogFocused,
		FiltersFocused,
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
)

// #region boilerplate defs

// Names of all the states (pkg enum).

const (
	TreeFocused          = "TreeFocused"
	LogFocused           = "LogFocused"
	TimelineTxsFocused   = "TimelineTxsFocused"
	TimelineStepsFocused = "TimelineStepsFocused"
	MatrixFocused        = "MatrixFocused"
	DialogFocused        = "DialogFocused"
	FiltersFocused       = "FiltersFocused"

	ClientMsg         = "ClientMsg"
	StateNameSelected = "StateNameSelected"
	Start             = "Start"
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
	BackStep        = "BackStep"
	ConnectEvent    = "ConnectEvent"
	DisconnectEvent = "DisconnectEvent"
	SidebarFocused  = "SidebarFocused"
	RemoveClient    = "RemoveClient"
	ClientSelected  = "ClientSelected"
	SelectingClient = "SelectingClient"
	HelpDialog      = "HelpDialog"
	ExportDialog    = "ExportDialog"
	MatrixView      = "MatrixView"
	TreeLogView     = "TreeLogView"
	TreeMatrixView  = "TreeMatrixView"
	LogUserScrolled = "LogUserScrolled"
	ScrollToTx      = "ScrollToTx"
	// Ready is an async result of start
	Ready            = "Ready"
	FilterCanceledTx = "FilterCanceledTx"
	FilterAutoTx     = "FilterAutoTx"
	// FilterEmptyTx is a filter for txes which didn't change state and didn't
	// run any self handler either
	FilterEmptyTx = "FilterEmptyTx"
	ToggleFilter  = "ToggleFilter"
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
	SidebarFocused,
	TimelineTxsFocused,
	TimelineStepsFocused,
	FiltersFocused,
	MatrixFocused,
	DialogFocused,
	StateNameSelected,
	HelpDialog,
	ExportDialog,
	LogUserScrolled,
	Ready,

	// /// Actions

	Start,
	TreeLogView,
	MatrixView,
	TreeMatrixView,
	TailMode,
	Playing,
	Paused,
	FilterAutoTx,
	FilterCanceledTx,
	FilterEmptyTx,
	ToggleFilter,

	// tx / steps back / fwd
	Fwd,
	Back,
	FwdStep,
	BackStep,

	ScrollToTx,

	// client
	ClientSelected,
	SelectingClient,
	RemoveClient,

	am.Exception,
}

// #endregion
