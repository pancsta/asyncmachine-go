package states

import (
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	. "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

// ///// ///// /////

// ///// SERVER

// ///// ///// /////

// ServerStatesDef contains all the states of the debugger server state machine,
// used to listen for RPC connections and msgD.
type ServerStatesDef struct {
	*am.StatesBase

	ErrGraph string

	ConnectEvent    string
	ClientMsg       string
	DisconnectEvent string
	InitClient      string
}

// ServerSchema represents all relations and properties of ServerStates.
var ServerSchema = am.Schema{
	ssV.ErrGraph: {Require: S{Exception}},
	ssV.ConnectEvent: {
		Multi:   true,
		Require: S{ssam.BasicStates.Start}},
	ssV.ClientMsg: {
		Multi:   true,
		Require: S{ssam.BasicStates.Start}},
	ssV.DisconnectEvent: {
		Multi:   true,
		Require: S{ssam.BasicStates.Start}},
	ssV.InitClient: {
		Multi:   true,
		Require: S{ssam.BasicStates.Start}},
}

// EXPORTS AND GROUPS

var (
	ssV = am.NewStates(ServerStatesDef{})

	// ServerStates contains all the states for the Server machine.
	ServerStates = ssV
)

// ///// ///// /////

// ///// ///// /////

// DebuggerStatesDef contains all the states of the debugger state machine.
type DebuggerStatesDef struct {
	*am.StatesBase

	// errors
	ErrDiagrams string
	ErrWeb      string

	// input events
	WebReq        string
	WebSocketDiag string
	WebSocketDbg  string

	// input events
	UserFwd      string
	UserBack     string
	UserFwdStep  string
	UserBackStep string

	// focus group
	TreeFocused          string
	TreeGroupsFocused    string
	LogFocused           string
	ClientListFocused    string
	TimelineTxsFocused   string
	TimelineStepsFocused string
	MatrixFocused        string
	DialogFocused        string
	Toolbar1Focused      string
	Toolbar2Focused      string
	Toolbar3Focused      string
	Toolbar4Focused      string
	LogReaderFocused     string
	AddressFocused       string
	Resized              string

	// view states
	TimelineTxHidden      string
	TimelineStepsHidden   string
	NarrowLayout          string
	UserNarrowLayout      string
	ClientListVisible     string
	StateNameSelected     string
	TimelineStepsScrolled string
	HelpDialog            string
	ExportDialog          string
	LogUserScrolled       string
	Ready                 string
	FilterAutoTx          string
	FilterAutoCanceledTx  string
	FilterCanceledTx      string
	FilterQueuedTx        string
	FilterEmptyTx         string
	FilterDisconn         string
	LogTimestamps         string
	FilterTraces          string
	FilterHealth          string
	FilterChecks          string
	FilterRpcMachs        string
	FilterOutGroup        string
	Redraw                string

	// actions
	Start             string
	Heartbeat         string
	GcMsgs            string
	TreeLogView       string
	MatrixView        string
	TreeMatrixView    string
	TailMode          string
	Playing           string
	Paused            string
	ToggleTool        string
	ToolToggled       string
	SwitchingClientTx string
	SwitchedClientTx  string
	// ScrollToMutTxState scrolls to a transition which mutated the passed state,
	// If fwd is true, it scrolls forward, otherwise backwards.
	ScrollToMutTx      string
	MatrixRain         string
	MatrixRainSelected string
	LogReaderVisible   string
	LogReaderEnabled   string
	UpdateLogScheduled string
	UpdatingLog        string
	LogUpdated         string
	UpdateLogReader    string
	UpdateFocus        string
	Overlay            string
	AfterFocus         string
	FocusPrev          string
	FocusNext          string
	ToolRain           string

	// tx / steps back / fwd
	Fwd          string
	Back         string
	FwdStep      string
	BackStep     string
	ScrollToTx   string
	ScrollToStep string

	// client selection
	SelectingClient string
	ClientSelected  string
	RemoveClient    string
	BuildingLog     string
	LogBuilt        string

	SetCursor  string
	SetGroup   string
	SshServer  string
	SshDisconn string

	DiagramsGraphRendering string
	DiagramsGraphReady     string
	DiagramsMachRendering  string
	DiagramsMachReady      string
	// pre-render all state-zoom diagrams for the selected machine
	DiagramsStatesRendering string
	DiagramsStatesReady     string

	*ssam.BasicStatesDef
	*ServerStatesDef
	*ssrpc.StateSourceStatesDef
	*ssam.DisposedStatesDef
}

// DebuggerSchema represents all relations and properties of DebuggerStates.
var DebuggerSchema = ssam.BasicSchema.Merge(
	ServerSchema,
	ssrpc.StateSourceSchema,
	ssam.DisposedSchema,
	am.Schema{

		// Errors

		ssD.ErrDiagrams: {
			Multi:   true,
			Require: S{Exception},
		},
		ssD.ErrWeb: {
			Multi:   true,
			Require: S{Exception},
		},

		// ///// Input events

		ssD.WebReqDiag: {
			Multi:   true,
			Require: S{Start},
		},
		ssD.WebSocketDiag: {
			Multi:   true,
			Require: S{Start},
		},

		// user scrolling tx / steps
		ssD.UserFwd: {
			Add:    S{ssD.Fwd},
			Remove: sgD.Playing,
		},
		ssD.UserBack: {
			Add:    S{ssD.Back},
			Remove: sgD.Playing,
		},
		ssD.UserFwdStep: {
			Add:     S{ssD.FwdStep},
			Require: S{ssD.ClientSelected},
			Remove:  sgD.Playing.Add1(ssD.LogUserScrolled),
		},
		ssD.UserBackStep: {
			Add:     S{ssD.BackStep},
			Require: S{ssD.ClientSelected},
			Remove:  sgD.Playing.Add1(ssD.LogUserScrolled),
		},

		// ///// Read-only states (e.g. UI)

		// focus group

		ssD.TreeFocused:          {Remove: sgD.Focused},
		ssD.TreeGroupsFocused:    {Remove: sgD.Focused},
		ssD.LogFocused:           {Remove: sgD.Focused},
		ssD.ClientListFocused:    {Remove: sgD.Focused},
		ssD.TimelineTxsFocused:   {Remove: sgD.Focused},
		ssD.TimelineStepsFocused: {Remove: sgD.Focused},
		ssD.MatrixFocused:        {Remove: sgD.Focused},
		ssD.DialogFocused:        {Remove: sgD.Focused},
		ssD.Toolbar1Focused:      {Remove: sgD.Focused},
		ssD.Toolbar2Focused:      {Remove: sgD.Focused},
		ssD.Toolbar3Focused:      {Remove: sgD.Focused},
		ssD.Toolbar4Focused:      {Remove: sgD.Focused},
		ssD.LogReaderFocused: {
			Require: S{ssD.LogReaderVisible},
			Remove:  sgD.Focused,
		},
		ssD.AddressFocused: {Remove: sgD.Focused},
		ssD.Resized:        {Multi: true},

		ssD.TimelineTxHidden:    {Require: S{ssD.TimelineStepsHidden}},
		ssD.TimelineStepsHidden: {},
		ssD.NarrowLayout: {
			Require: S{Ready},
			Remove:  S{ssD.ClientListVisible},
		},
		ssD.UserNarrowLayout:  {Add: S{ssD.NarrowLayout}},
		ssD.ClientListVisible: {},
		ssD.StateNameSelected: {
			Multi:   true,
			Require: S{ssD.ClientSelected},
		},
		ssD.TimelineStepsScrolled: {Require: S{ssD.ClientSelected}},
		ssD.HelpDialog: {
			Add:    S{ssD.DialogFocused},
			Remove: sgD.Dialog.Add(sgD.Focused),
		},
		ssD.ExportDialog: {
			Add:    S{ssD.DialogFocused},
			Remove: sgD.Dialog.Add(sgD.Focused),
		},
		ssD.LogUserScrolled: {
			Remove: S{ssD.Playing, ssD.TailMode},
			// TODO remove the requirement once its possible to go back
			//  to timeline-scroll somehow
			Require: S{ssD.LogFocused},
		},
		Ready: {
			Require: S{Start},
			Add:     S{ssD.UpdateFocus},
		},
		// TODO should activate FiltersFocused
		ssD.FilterAutoTx:         {Remove: S{ssD.FilterAutoCanceledTx}},
		ssD.FilterAutoCanceledTx: {Remove: S{ssD.FilterAutoTx}},
		ssD.FilterCanceledTx:     {},
		ssD.FilterQueuedTx:       {},
		ssD.FilterEmptyTx:        {},
		ssD.FilterDisconn:        {},
		ssD.LogTimestamps:        {},
		ssD.FilterTraces:         {},
		ssD.FilterHealth:         {},
		ssD.FilterChecks:         {},
		ssD.FilterRpcMachs:       {},
		ssD.FilterOutGroup:       {},
		ssD.Redraw:               {},

		// ///// Actions

		Start: {Add: S{
			ssD.LogTimestamps, ssD.FilterHealth, ssD.ClientListFocused,
			ssD.FilterAutoCanceledTx, ssD.ClientListVisible,
		}},
		ssD.Heartbeat: {
			Multi:   true,
			Require: S{Start},
		},
		ssD.GcMsgs: {Remove: S{
			ssD.SelectingClient, ssD.SwitchedClientTx, ssD.ScrollToTx,
			ssD.ScrollToMutTx}},
		ssD.TreeLogView: {
			Auto:    true,
			Require: S{Start},
			Remove:  sgD.Views.Add1(ssD.MatrixRain),
		},
		ssD.MatrixView:     {Remove: sgD.Views.Add1(ssD.LogReaderVisible)},
		ssD.TreeMatrixView: {Remove: sgD.Views.Add1(ssD.LogReaderVisible)},
		ssD.TailMode: {
			Require: S{ssD.ClientSelected},
			Remove:  sgD.Playing.Add1(ssD.LogUserScrolled),
		},
		ssD.Playing: {
			Require: S{ssD.ClientSelected},
			Remove:  sgD.Playing.Add1(ssD.LogUserScrolled),
		},
		ssD.Paused: {
			Auto:    true,
			Require: S{ssD.ClientSelected},
			Remove:  sgD.Playing,
		},
		ssD.ToggleTool:  {Remove: S{ssD.ToolToggled}},
		ssD.ToolToggled: {Remove: S{ssD.ToggleTool}},
		ssD.SwitchingClientTx: {
			Require: S{Ready},
			Remove:  S{ssD.SwitchedClientTx},
		},
		ssD.SwitchedClientTx: {
			Require: S{Ready},
			Remove:  S{ssD.SwitchingClientTx},
		},
		ssD.ScrollToMutTx: {Require: S{ssD.ClientSelected}},
		// TODO make it depend on a common MatrixVisible state
		ssD.MatrixRain: {},
		ssD.MatrixRainSelected: {
			Multi:   true,
			Require: S{ssD.MatrixRain, ssD.ClientSelected},
		},
		ssD.LogReaderVisible: {
			Auto:    true,
			Require: S{ssD.TreeLogView, ssD.LogReaderEnabled},
		},
		ssD.LogReaderEnabled:   {},
		ssD.UpdateLogScheduled: {Require: S{Ready}},
		ssD.UpdatingLog: {
			Require: S{Ready, ssD.ClientSelected},
			Remove:  S{ssD.LogUpdated},
		},
		ssD.LogUpdated: {
			Require: S{Ready, ssD.ClientSelected},
			Remove:  S{ssD.UpdatingLog},
		},
		ssD.UpdateLogReader: {Require: S{Ready, ssD.LogReaderEnabled}},
		ssD.UpdateFocus: {
			Multi:   true,
			Require: S{Ready},
		},
		ssD.Overlay: {Require: S{ssD.LogReaderFocused}},
		ssD.AfterFocus: {
			Multi:   true,
			Require: S{Ready},
		},
		ssD.FocusPrev: {
			Multi:   true,
			Require: S{Ready},
		},
		ssD.FocusNext: {
			Multi:   true,
			Require: S{Ready},
		},
		ssD.ToolRain: {
			Multi:   true,
			Require: S{Ready},
		},

		// tx / steps back / fwd

		ssD.Fwd: {
			Require: S{ssD.ClientSelected},
			Remove:  S{ssD.Overlay},
		},
		ssD.Back: {
			Require: S{ssD.ClientSelected},
			Remove:  S{ssD.Overlay},
		},
		ssD.FwdStep: {
			Require: S{ssD.ClientSelected},
		},
		ssD.BackStep: {
			Require: S{ssD.ClientSelected},
		},

		ssD.ScrollToTx: {
			Multi:   true,
			Require: S{ssD.ClientSelected},
			Remove:  S{ssD.TailMode, ssD.Playing, ssD.TimelineStepsScrolled},
		},
		ssD.ScrollToStep: {
			Require: S{ssD.ClientSelected},
			Remove:  S{ssD.TailMode, ssD.Playing},
		},

		// client selection

		ssD.SelectingClient: {
			Require: S{Start},
			Remove:  S{ssD.ClientSelected, ssD.Overlay},
		},
		ssD.ClientSelected: {
			Require: S{Start},
			Remove:  S{ssD.SelectingClient},
		},
		ssD.RemoveClient: {Require: S{ssD.ClientSelected}},
		ssD.BuildingLog: {
			Require: S{ssD.ClientSelected},
			Remove:  S{ssD.LogBuilt},
			After:   S{ssD.ClientSelected},
		},
		ssD.LogBuilt: {
			Require: S{ssD.ClientSelected},
			Remove:  S{ssD.BuildingLog},
		},

		ssD.SetCursor: {Require: S{Ready}},
		ssD.SetGroup: {
			Multi:   true,
			Require: S{ssD.ClientSelected},
		},
		ssD.SshServer: {Require: S{Start}},
		ssD.SshDisconn: {
			Multi:   true,
			Require: S{ssD.SshServer},
		},

		// diagrams

		ssD.DiagramsGraphRendering: {
			Multi:   true,
			Require: S{ssD.Ready},
			Add:     S{ssD.Loading},
			Remove:  S{ssD.DiagramsGraphReady},
		},
		ssD.DiagramsGraphReady: {
			Require: S{ssD.Ready},
			Remove:  S{ssD.DiagramsGraphRendering},
		},
		ssD.DiagramsMachRendering: {
			Multi:   true,
			Require: S{ssD.ClientSelected},
			Add:     S{ssD.Loading},
			Remove:  S{ssD.DiagramsMachReady},
		},
		ssD.DiagramsMachReady: {
			Require: S{ssD.ClientSelected},
			Remove:  S{ssD.DiagramsMachRendering},
		},
		ssD.DiagramsStatesRendering: {
			Multi:   true,
			Require: S{ssD.ClientSelected},
			Add:     S{ssD.Loading},
			Remove:  S{ssD.DiagramsStatesReady},
		},
		ssD.DiagramsStatesReady: {
			Require: S{ssD.ClientSelected},
			Remove:  S{ssD.DiagramsStatesRendering},
		},
	},
)

// DebuggerGroupsDef contains all the state groups of the debugger state
// machine.
type DebuggerGroupsDef struct {

	// Focused represents focus-related states, 1 active at a time.
	Focused S

	// Views represents view-related states, 1 active at a time.
	Views S

	// Playing represents playback-related states, 1 active at a time.
	Playing S

	// ClientTx represents client transaction states, 1 active at a time.
	ClientTx S

	// Filters represents filter states.
	Filters S

	// Dialog represents dialog-related states.
	Dialog S

	// Debug contains states useful when debugging the debugger in another
	// debugger.
	Debug S

	Mcp         S
	McpReadonly S
}

// EXPORTS AND GROUPS

var (
	groupFocus = am.S{
		ssD.AddressFocused, ssD.ClientListFocused,
		ssD.TreeGroupsFocused, ssD.TreeFocused,

		ssD.LogFocused, ssD.LogReaderFocused, ssD.MatrixFocused,

		ssD.TimelineTxsFocused, ssD.TimelineStepsFocused,

		ssD.Toolbar1Focused, ssD.Toolbar2Focused,
		ssD.Toolbar3Focused, ssD.Toolbar4Focused,

		ssD.DialogFocused,
	}
	groupFilters = S{
		ssD.FilterAutoTx, ssD.FilterCanceledTx, ssD.FilterEmptyTx,
		ssD.FilterHealth, ssD.FilterOutGroup, ssD.FilterQueuedTx,
		ssD.FilterAutoCanceledTx,
	}
	ssD = am.NewStates(DebuggerStatesDef{})
	sgD = am.NewStateGroups(DebuggerGroupsDef{
		// Focus
		Focused: groupFocus,

		// Views
		Views: S{ssD.MatrixView, ssD.TreeLogView, ssD.TreeMatrixView},

		// Playing
		Playing: S{ssD.Playing, ssD.Paused, ssD.TailMode},

		// ClientTx
		ClientTx: S{ssD.SwitchingClientTx, ssD.SwitchedClientTx},

		// Filters
		Filters: groupFilters,

		// Dialog
		Dialog: S{ssD.HelpDialog, ssD.ExportDialog},

		Debug: groupFocus.Add1(
			ssD.ExportDialog, ssD.UpdateFocus, ssD.LogReaderVisible,
			ssD.UpdateFocus, ssD.FocusNext, ssD.FocusPrev,
			ssD.Overlay, ssD.HelpDialog, ssD.ExportDialog,
		),

		Mcp: groupFocus.Add1(ssD.UserFwd, ssD.UserBack, ssD.UserFwdStep,
			ssD.UserBackStep, ssD.ScrollToMutTx, ssD.SwitchingClientTx,
			ssD.SelectingClient, ssD.RemoveClient, ssD.SetCursor, ssD.SetGroup,
			ssD.StateNameSelected, ssD.FocusNext, ssD.FocusPrev,
			ssD.LogReaderEnabled, ssD.UserNarrowLayout, ssD.TimelineTxHidden,
			ssD.TimelineStepsHidden),

		McpReadonly: groupFilters.Add1(ssD.SwitchedClientTx, ssD.ClientSelected),
	})

	// DebuggerStates contains all the states for the debugger machine.
	DebuggerStates = ssD
	// DebuggerGroups contains all the state groups for the debugger machine.
	DebuggerGroups = sgD
)
