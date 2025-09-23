package machine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sync/atomic"
	"time"
)

const (
	// EnvAmDebug enables a simple debugging mode (eg long timeouts).
	// "2" logs to stdout (where applicable)
	// "1" | "2" | "" (default)
	EnvAmDebug = "AM_DEBUG"
	// EnvAmTestRunner indicates a CI test runner.
	EnvAmTestRunner = "AM_TEST_RUNNER"
	// EnvAmLog sets the log level.
	// "1" | "2" | "3" | "4" | "5" | "" (default)
	EnvAmLog = "AM_LOG"
	// EnvAmDetectEval detects evals directly in handlers (use in tests).
	EnvAmDetectEval = "AM_DETECT_EVAL"
	// EnvAmTraceFilter will remove its contents from stack traces, shortening
	// them .
	EnvAmTraceFilter = "AM_TRACE_FILTER"
	// EnvAmTestDebug activates debugging in tests.
	EnvAmTestDebug = "AM_TEST_DEBUG"
	// HandlerAnyEnter is the name of the global negotiation transition handler.
	HandlerAnyEnter = "AnyEnter"
	// HandlerAnyState is the name of the global final transition handler.
	HandlerAnyState = "AnyState"
	SuffixEnter     = "Enter"
	SuffixExit      = "Exit"
	SuffixState     = "State"
	SuffixEnd       = "End"
	PrefixErr       = "Err"
	// StateAny is a name of a meta-state used in catch-all handlers.
	StateAny = "Any"
	// StateException is the name of the predefined Exception state.
	StateException = "Exception"
	// StateHeartbeat is the name of the predefined Heartbeat state.
	StateHeartbeat = "Heartbeat"
	// StateHealthcheck is the name of the predefined Healthcheck state.
	StateHealthcheck = "Healthcheck"
	// StateStart is the name of the predefined Start state.
	StateStart = "Start"
	// StateReady is the name of the predefined Ready state.
	StateReady = "Ready"
)

type (
	// S (state names) is a string list of state names.
	S []string
	// Schema is a map of state names to state definitions.
	Schema = map[string]State
)

// State defines a single state of a machine, its properties and relations.
type State struct {
	Auto    bool     `json:"auto,omitempty"`
	Multi   bool     `json:"multi,omitempty"`
	Require S        `json:"require,omitempty"`
	Add     S        `json:"add,omitempty"`
	Remove  S        `json:"remove,omitempty"`
	After   S        `json:"after,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// A (arguments) is a map of named arguments for a Mutation.
type A map[string]any

// Clock is a map of state names to their tick values. It's like Time but
// indexed by string, instead of int.
type Clock map[string]uint64

// HandlerFinal is a final transition handler func signature.
type HandlerFinal func(e *Event)

// HandlerNegotiation is a negotiation transition handler func signature.
type HandlerNegotiation func(e *Event) bool

// HandlerDispose is a machine disposal handler func signature.
type HandlerDispose func(id string, ctx context.Context)

// Opts struct is used to configure a new Machine.
type Opts struct {
	// Unique ID of this machine. Default: random ID.
	Id string
	// Time for a handler to execute. Default: time.Second
	HandlerTimeout time.Duration
	// TODO docs
	HandlerDeadline time.Duration
	// If true, the machine will NOT print all exceptions to stdout.
	DontLogStackTrace bool
	// If true, the machine will die on panics.
	DontPanicToException bool
	// If true, the machine will NOT prefix its logs with its ID.
	// TODO refac to DontLogId
	DontLogId bool
	// Custom relations resolver. Default: *DefaultRelationsResolver.
	Resolver RelationsResolver
	// Log level of the machine. Default: LogNothing.
	LogLevel LogLevel
	// Tracer for the machine. Default: nil.
	Tracers []Tracer
	// LogArgs matching function for the machine. Default: nil.
	LogArgs LogArgsMapperFn
	// Parent machine, used to inherit certain properties, e.g. tracers.
	// Overrides ParentID. Default: nil.
	Parent Api
	// ParentID is the ID of the parent machine. Default: "".
	ParentId string
	// Tags is a list of tags for the machine. Default: nil.
	Tags []string
	// QueueLimit is the maximum number of mutations that can be queued.
	// Default: 1000.
	// TODO per-state QueueLimit
	QueueLimit int
	// DetectEval will detect Eval calls directly in handlers, which causes a
	// deadlock. It works in similar way as -race flag in Go and can also be
	// triggered by setting either env var: AM_DEBUG=1 or AM_DETECT_EVAL=1.
	// Default: false.
	DetectEval     bool
	HandlerBackoff time.Duration
}

// Serialized is a machine state serialized to a JSON/YAML/TOML compatible
// struct. One also needs the state Struct to re-create a state machine.
type Serialized struct {
	// ID is the ID of a state machine.
	ID string `json:"id" yaml:"id" toml:"id"`
	// Time is the [Machine.Time] value.
	Time Time `json:"time" yaml:"time" toml:"time"`
	// QueueTick is the [Machine.QueueTick] value.
	QueueTick uint64 `json:"queue_tick" yaml:"queue_tick" toml:"queue_tick"`
	// StateNames is an ordered list of state names.
	StateNames S `json:"state_names" yaml:"state_names" toml:"state_names"`
	// TODO schema hash
}

// Api is a subset of Machine for alternative implementations.
type Api interface {
	// ///// REMOTE

	// Mutations (remote)

	// Add1 is [Machine.Add1].
	Add1(state string, args A) Result
	// Add is [Machine.Add].
	Add(states S, args A) Result
	// Remove1 is [Machine.Remove1].
	Remove1(state string, args A) Result
	// Remove is [Machine.Remove].
	Remove(states S, args A) Result
	// AddErr is [Machine.AddErr].
	AddErr(err error, args A) Result
	// AddErrState is [Machine.AddErrState].
	AddErrState(state string, err error, args A) Result
	// Toggle is [Machine.Toggle].
	Toggle(states S, args A) Result
	// Toggle1 is [Machine.Toggle1].
	Toggle1(state string, args A) Result
	// Set is [Machine.Set].
	Set(states S, args A) Result

	// Traced mutations (remote)

	// EvAdd1 is [Machine.EvAdd1].
	EvAdd1(event *Event, state string, args A) Result
	// EvAdd is [Machine.EvAdd].
	EvAdd(event *Event, states S, args A) Result
	// EvRemove1 is [Machine.EvRemove1].
	EvRemove1(event *Event, state string, args A) Result
	// EvRemove is [Machine.EvRemove].
	EvRemove(event *Event, states S, args A) Result
	// EvAddErr is [Machine.EvAddErr].
	EvAddErr(event *Event, err error, args A) Result
	// EvAddErrState is [Machine.EvAddErrState].
	EvAddErrState(event *Event, state string, err error, args A) Result
	// EvToggle is [Machine.Toggle].
	EvToggle(event *Event, states S, args A) Result
	// EvToggle1 is [Machine.Toggle1].
	EvToggle1(event *Event, state string, args A) Result

	// Waiting (remote)

	// WhenArgs is [Machine.WhenArgs].
	WhenArgs(state string, args A, ctx context.Context) <-chan struct{}

	// Getters (remote)

	// Err is [Machine.Err].
	Err() error

	// ///// LOCAL

	// Checking (local)

	// IsErr is [Machine.IsErr].
	IsErr() bool
	// Is is [Machine.Is].
	Is(states S) bool
	// Is1 is [Machine.Is1].
	Is1(state string) bool
	// Any is [Machine.Any].
	Any(states ...S) bool
	// Any1 is [Machine.Any1].
	Any1(state ...string) bool
	// Not is [Machine.Not].
	Not(states S) bool
	// Not1 is [Machine.Not1].
	Not1(state string) bool
	// IsTime is [Machine.IsTime].
	IsTime(time Time, states S) bool
	// WasTime is [Machine.WasTime].
	WasTime(time Time, states S) bool
	// IsClock is [Machine.IsClock].
	IsClock(clock Clock) bool
	// WasClock is [Machine.WasClock].
	WasClock(clock Clock) bool
	// Has is [Machine.Has].
	Has(states S) bool
	// Has1 is [Machine.Has1].
	Has1(state string) bool
	// CanAdd is [Machine.CanAdd].
	CanAdd(states S, args A) Result
	// CanAdd1 is [Machine.CanAdd1].
	CanAdd1(state string, args A) Result
	// CanRemove is [Machine.CanRemove].
	CanRemove(states S, args A) Result
	// CanRemove1 is [Machine.CanRemove1].
	CanRemove1(state string, args A) Result
	// CountActive is [Machine.CountActive].
	CountActive(states S) int

	// Waiting (local)

	// When is [Machine.When].
	When(states S, ctx context.Context) <-chan struct{}
	// When1 is [Machine.When1].
	When1(state string, ctx context.Context) <-chan struct{}
	// WhenNot is [Machine.WhenNot].
	WhenNot(states S, ctx context.Context) <-chan struct{}
	// WhenNot1 is [Machine.WhenNot1].
	WhenNot1(state string, ctx context.Context) <-chan struct{}
	// WhenTime is [Machine.WhenTime].
	WhenTime(states S, times Time, ctx context.Context) <-chan struct{}
	// WhenTime1 is [Machine.WhenTime1].
	WhenTime1(state string, tick uint64, ctx context.Context) <-chan struct{}
	// WhenTicks is [Machine.WhenTicks].
	WhenTicks(state string, ticks int, ctx context.Context) <-chan struct{}
	// WhenErr is [Machine.WhenErr].
	WhenErr(ctx context.Context) <-chan struct{}
	// WhenQueue is [Machine.WhenQueue].
	WhenQueue(tick Result) <-chan struct{}

	// Getters (local)

	// StateNames is [Machine.StateNames].
	StateNames() S
	// StateNamesMatch is [Machine.StateNamesMatch].
	StateNamesMatch(re *regexp.Regexp) S
	// ActiveStates is [Machine.ActiveStates].
	ActiveStates() S
	// Tick is [Machine.Tick].
	Tick(state string) uint64
	// Clock is [Machine.Clock].
	Clock(states S) Clock
	// Time is [Machine.Time].
	Time(states S) Time
	// TimeSum is [Machine.TimeSum].
	TimeSum(states S) uint64
	// QueueTick is [Machine.QueueTick].
	QueueTick() uint64
	// NewStateCtx is [Machine.NewStateCtx].
	NewStateCtx(state string) context.Context
	// Export is [Machine.Export].
	Export() *Serialized
	// Schema is [Machine.Schema].
	Schema() Schema
	// Switch is [Machine.Switch].
	Switch(groups ...S) string
	// Groups is [Machine.Groups].
	Groups() (map[string][]int, []string)
	// Index is [Machine.Index].
	Index(states S) []int
	// Index1 is [Machine.Index1].
	Index1(state string) int

	// Misc (local)

	// Id is [Machine.Id].
	Id() string
	// ParentId is [Machine.ParentId].
	ParentId() string
	// Tags is [Machine.Tags].
	Tags() []string
	// Ctx is [Machine.Ctx].
	Ctx() context.Context
	// String is [Machine.String].
	String() string
	// StringAll is [Machine.StringAll].
	StringAll() string
	// Log is [Machine.Log].
	Log(msg string, args ...any)
	// SemLogger is [Machine.SemLogger].
	SemLogger() SemLogger
	// Inspect is [Machine.Inspect].
	Inspect(states S) string
	// BindHandlers is [Machine.BindHandlers].
	BindHandlers(handlers any) error
	// DetachHandlers is [Machine.DetachHandlers].
	DetachHandlers(handlers any) error
	// HasHandlers is [Machine.HasHandlers].
	HasHandlers() bool
	// StatesVerified is [Machine.StatesVerified].
	StatesVerified() bool
	// Tracers is [Machine.Tracers].
	Tracers() []Tracer
	// DetachTracer is [Machine.DetachTracer].
	DetachTracer(tracer Tracer) error
	// BindTracer is [Machine.BindTracer].
	BindTracer(tracer Tracer) error
	// AddBreakpoint is [Machine.AddBreakpoint].
	AddBreakpoint(added S, removed S, strict bool)
	// AddBreakpoint1 is [Machine.AddBreakpoint1].
	AddBreakpoint1(added string, removed string, strict bool)
	// Dispose is [Machine.Dispose].
	Dispose()
	// WhenDisposed is [Machine.WhenDisposed].
	WhenDisposed() <-chan struct{}
	// IsDisposed is [Machine.IsDisposed].
	IsDisposed() bool

	// TODO debug

	// QueueDump() []string
}

type breakpoint struct {
	Added   S
	Removed S
	Strict  bool
}

// ///// ///// /////

// ///// ENUMS

// ///// ///// /////

// Result enum is the result of a state Transition. [Queue] is a virtual value
// and everything >= Queue represents a queue tick on which the mutation will
// be processed. It's useful for queued negotiations.
type Result uint64

const (
	// Executed means that the transition was executed immediately and not
	// canceled.
	Executed Result = 0
	// Canceled means that the transition was canceled, by either relations or a
	// negotiation handler.
	Canceled Result = 1
	// Queued means that the transition was queued for later execution. Everything
	// above 3 also means Queued. The following methods can be used to wait for
	// the results:
	// - Machine.When
	// - Machine.WhenNot
	// - Machine.WhenArgs
	// - Machine.WhenTime
	// - Machine.WhenTime1
	// - Machine.WhenTicks
	// - Machine.WhenQueue
	// - Machine.WhenQueueEnds
	// See [Machine.queueTick].
	Queued Result = 2
)

var (
	// ErrStateUnknown indicates that the state is unknown.
	ErrStateUnknown = errors.New("state unknown")
	// ErrStateInactive indicates that a necessary state isn't active.
	ErrStateInactive = errors.New("state not active")
	// ErrCanceled indicates that a transition was canceled.
	ErrCanceled = errors.New("transition canceled")
	// ErrQueued indicates that a mutation was queued. Queuing is a feature, and
	// not an error, but it may be considered as such in certain cases.
	ErrQueued = errors.New("transition queued")
	// ErrInvalidArgs indicates that arguments are invalid.
	ErrInvalidArgs = errors.New("invalid arguments")
	// ErrHandlerTimeout indicates that a mutation timed out.
	ErrHandlerTimeout = errors.New("handler timeout")
	// ErrEvalTimeout indicates that an eval func timed out.
	ErrEvalTimeout = errors.New("eval timeout")
	// ErrTimeout indicates that a generic timeout occurred.
	ErrTimeout = errors.New("timeout")
	// ErrStateMissing indicates that states are missing.
	ErrStateMissing = errors.New("missing states")
	// ErrRelation indicates that a relation definition is invalid.
	ErrRelation = errors.New("relation error")
	// ErrDisposed indicates that the machine has been disposed.
	ErrDisposed = errors.New("machine disposed")
	// ErrSchema indicates an issue with the machine schema.
	ErrSchema = errors.New("schema error")
	// ErrInternal happens for internal errors in the machine. These should be
	// reported as bugs.
	ErrInternal = errors.New("internal error")
)

func (r Result) String() string {
	switch r {
	case Executed:
		return "executed"
	case Canceled:
		return "canceled"
	default:
		return "queued"
	}
}

type MutSource struct {
	MachId string
	TxId   string
	// Machine time of the source machine BEFORE the event.
	MachTime uint64
}

// MutationType enum
type MutationType int

const (
	MutationAdd MutationType = iota
	MutationRemove
	MutationSet
	mutationEval
)

func (m MutationType) String() string {
	switch m {
	case MutationAdd:
		return "add"
	case MutationRemove:
		return "remove"
	case MutationSet:
		return "set"
	case mutationEval:
		return "eval"
	}
	return ""
}

// Mutation represents an atomic change (or an attempt) of machine's active
// states. Mutation causes a Transition.
type Mutation struct {
	// add, set, remove
	Type MutationType
	// States explicitly passed to the mutation method, as their indexes. Use
	// Transition.CalledStates or IndexToStates to get the actual state names.
	Called []int
	// argument map passed to the mutation method (if any).
	Args A
	// this mutation has been triggered by an auto state
	Auto bool
	// Source is the source event for this mutation.
	Source *MutSource
	// IsCheck indicates that this mutation is a check, see [Machine.CanAdd].
	IsCheck bool

	// optional queue info

	// QueueTickNow is the queue tick during which this mutation was scheduled.
	QueueTickNow uint64
	// QueueLen is the length of the queue at the time when the mutation was
	// queued.
	QueueLen int32
	// QueueTokensLen is the amount of unexecuted queue tokens (priority queue).
	// TODO impl
	QueueTokensLen int32
	// QueueTick is the assigned queue tick at the time when the mutation was
	// queued. 0 for prepended mutations.
	QueueTick uint64
	// QueueToken is a unique ID, which is given to prepended mutations.
	// Tokens are assigned in a series but executed in random order.
	QueueToken uint64

	// internals

	// specific context for this mutation (optional)
	ctx context.Context
	// optional eval func, only for mutationEval
	eval        func()
	evalSource  string
	cacheCalled atomic.Pointer[S]
}

func (m *Mutation) String() string {
	return fmt.Sprintf("[%s] %v", m.Type, m.Called)
}

func (m *Mutation) IsCalled(idx int) bool {
	return slices.Contains(m.Called, idx)
}

func (m *Mutation) CalledIndex(index S) *TimeIndex {
	return NewTimeIndex(index, m.Called)
}

func (m *Mutation) StringFromIndex(index S) string {
	called := NewTimeIndex(index, m.Called)
	return "[" + m.Type.String() + "] " + j(called.ActiveStates())
}

// MapArgs returns arguments of this Mutation which match the passed [mapper].
func (m *Mutation) MapArgs(mapper LogArgsMapperFn) map[string]string {
	if mapper == nil {
		return map[string]string{}
	}

	return mapper(m.Args)
}

// LogArgs returns a text snippet with arguments which should be logged for this
// Mutation.
func (m *Mutation) LogArgs(mapper LogArgsMapperFn) string {
	return MutationFormatArgs(m.MapArgs(mapper))
}

// StepType enum
type StepType int8

const (
	StepRelation StepType = 1 << iota
	StepHandler
	// TODO rename to StepActivate
	StepSet
	// StepRemove indicates a step where a state goes active->inactive
	// TODO rename to StepDeactivate
	StepRemove
	// StepRemoveNotActive indicates a step where a state goes inactive->inactive
	// TODO rename to StepDeactivatePassive
	StepRemoveNotActive
	StepRequested
	StepCancel
)

func (tt StepType) String() string {
	switch tt {
	case StepRelation:
		return "rel"
	case StepHandler:
		return "handler"
	case StepSet:
		return "activate"
	case StepRemove:
		return "deactivate"
	case StepRemoveNotActive:
		return "deactivate-passive"
	case StepRequested:
		return "requested"
	case StepCancel:
		return "cancel"
	}
	return ""
}

// Step struct represents a single step within a Transition, either a relation
// resolving step or a handler call.
type Step struct {
	Type StepType
	// Only for Type == StepRelation.
	RelType Relation
	// marks a final handler (FooState, FooEnd)
	IsFinal bool
	// marks a self handler (FooFoo)
	IsSelf bool
	// marks an enter handler (FooState, but not FooEnd). Requires IsFinal.
	IsEnter bool
	// Deprecated, use GetFromState(). TODO remove
	FromState string
	// TODO implement
	FromStateIdx int
	// Deprecated, use GetToState(). TODO remove
	ToState string
	// TODO implement
	ToStateIdx int
	// Deprecated, use RelType. TODO remove
	Data any
	// TODO emitter name and num
}

// GetFromState returns the source state of a step. Optional, unless no
// GetToState().
func (s *Step) GetFromState(index S) string {
	// TODO rename to FromState
	if s.FromState != "" {
		return s.FromState
	}
	if s.FromStateIdx == -1 {
		return ""
	}
	if s.FromStateIdx < len(index) {
		return index[s.FromStateIdx]
	}

	return ""
}

// GetToState returns the target state of a step. Optional, unless no
// GetFromState().
func (s *Step) GetToState(index S) string {
	// TODO rename to ToState
	if s.ToState != "" {
		return s.ToState
	}
	if s.ToStateIdx == -1 {
		return ""
	}
	if s.ToStateIdx < len(index) {
		return index[s.ToStateIdx]
	}

	return ""
}

func (s *Step) StringFromIndex(idx S) string {
	var line string

	// collect
	from := s.GetFromState(idx)
	to := s.GetToState(idx)
	if from == "" && to == "" {
		to = StateAny
	}

	// format TODO markdown?
	if from != "" {
		from = "**" + from + "**"
	}
	if to != "" {
		to = "**" + to + "**"
	}

	// output
	if from != "" && to != "" {
		line += from + " " + s.RelType.String() + " " + to
	} else {
		line = from
	}
	if line == "" {
		line = to
	}

	if s.Type == StepRelation {
		return line
	}

	suffix := ""
	if s.Type == StepHandler {
		if s.IsSelf {
			suffix = line
		} else if s.IsFinal && s.IsEnter {
			suffix = SuffixState
		} else if s.IsFinal && !s.IsEnter {
			suffix = SuffixEnd
		} else if !s.IsFinal && s.IsEnter {
			suffix = SuffixEnter
		} else if !s.IsFinal && !s.IsEnter {
			suffix = SuffixExit
		}
	}

	// TODO infer handler names
	return s.Type.String() + " " + line + suffix
}

func newStep(from string, to string, stepType StepType,
	relType Relation,
) *Step {
	ret := &Step{
		// TODO refac with the new dbg protocol, use indexes only
		FromState: from,
		ToState:   to,
		Type:      stepType,
		RelType:   relType,
	}
	// default values TODO use real values
	if from == "" {
		ret.FromStateIdx = -1
	}
	if to == "" {
		ret.ToStateIdx = -1
	}

	return ret
}

func newSteps(from string, toStates S, stepType StepType,
	relType Relation,
) []*Step {
	// TODO optimize, only use during debug
	var ret []*Step
	for _, to := range toStates {
		ret = append(ret, newStep(from, to, stepType, relType))
	}

	return ret
}

// Relation enum
type Relation int8

const (
	// TODO refac, 0 should be RelationNone, start from 1, udpate with dbg proto
	RelationAfter Relation = iota
	RelationAdd
	RelationRequire
	RelationRemove
)

func (r Relation) String() string {
	switch r {
	case RelationAfter:
		return "after"
	case RelationAdd:
		return "add"
	case RelationRequire:
		return "require"
	case RelationRemove:
		return "remove"
	}

	return ""
}

// Position describes the item's position in a list. Used mostly for the queue
// in [Machine.IsQueued].
type Position uint8

const (
	PositionAny Position = iota
	PositionFirst
	PositionLast
)

type (
	HandlerError  func(mach *Machine, err error)
	HandlerChange func(mach *Machine, err error)
)
