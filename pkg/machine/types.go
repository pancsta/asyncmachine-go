package machine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	// EnvAmDebug enables a simple debugging mode (eg long timeouts).
	// "2" logs to stdout (where applicable)
	// "1" | "2" | "" (default)
	EnvAmDebug = "AM_DEBUG"
	// EnvAmLog sets the log level.
	// "1" | "2" | "3" | "4" | "" (default)
	EnvAmLog = "AM_LOG"
	// EnvAmLogFile enables file logging (use machine ID as a name).
	// "1" | "" (default)
	EnvAmLogFile = "AM_LOG_FILE"
	// EnvAmDetectEval detects evals directly in handlers (use in tests).
	EnvAmDetectEval = "AM_DETECT_EVAL"
	// EnvAmTraceFilter will remove its contents from stack traces, shortening
	// them .
	EnvAmTraceFilter = "AM_TRACE_FILTER"
	// EnvAmTestDebug activates debugging in tests.
	EnvAmTestDebug = "AM_TEST_DEBUG"
	// HandlerGlobal is the name of a global transition handler.
	HandlerGlobal = "AnyEnter"
	// Any is a name of a meta-state used in catch-all handlers.
	Any         = "Any"
	SuffixEnter = "Enter"
	SuffixExit  = "Exit"
	SuffixState = "State"
	SuffixEnd   = "End"
)

type (
	// S (state names) is a string list of state names.
	S []string
	// Schema is a map of state names to state definitions.
	Schema = map[string]State
)

// State defines a single state of a machine, its properties and relations.
type State struct {
	Auto    bool
	Multi   bool
	Require S
	Add     S
	Remove  S
	After   S
	Tags    []string
}

// A (arguments) is a map of named arguments for a Mutation.
type A map[string]any

// Clock is a map of state names to their tick values. It's like Time but
// indexed by string, instead of int.
type Clock map[string]uint64

// HandlerFinal is a final transition handler func signature.
type HandlerFinal func(*Event)

// HandlerNegotiation is a negotiation transition handler func signature.
type HandlerNegotiation func(*Event) bool

// HandlerDispose is a machine disposal handler func signature.
type HandlerDispose func(id string, ctx context.Context)

// Opts struct is used to configure a new Machine.
type Opts struct {
	// Unique ID of this machine. Default: random ID.
	// TODO refac to Id
	ID string
	// Time for a handler to execute. Default: time.Second
	HandlerTimeout time.Duration
	// If true, the machine will NOT print all exceptions to stdout.
	DontLogStackTrace bool
	// If true, the machine will die on panics.
	DontPanicToException bool
	// If true, the machine will NOT prefix its logs with its ID.
	// TODO refac to DontLogId
	DontLogID bool
	// Custom relations resolver. Default: *DefaultRelationsResolver.
	Resolver RelationsResolver
	// Log level of the machine. Default: LogNothing.
	LogLevel LogLevel
	// Tracer for the machine. Default: nil.
	Tracers []Tracer
	// LogArgs matching function for the machine. Default: nil.
	LogArgs func(args A) map[string]string
	// Parent machine, used to inherit certain properties, e.g. tracers.
	// Overrides ParentID. Default: nil.
	Parent Api
	// ParentID is the ID of the parent machine. Default: "".
	// TODO refac to ParentId
	ParentID string
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
	DetectEval bool
}

// Api is a subset of Machine for alternative implementations.
type Api interface {
	// ///// REMOTE

	// Mutations (remote)

	Add1(state string, args A) Result
	Add(states S, args A) Result
	Remove1(state string, args A) Result
	Remove(states S, args A) Result
	Set(states S, args A) Result
	AddErr(err error, args A) Result
	AddErrState(state string, err error, args A) Result

	EvAdd1(event *Event, state string, args A) Result
	EvAdd(event *Event, states S, args A) Result
	EvRemove1(event *Event, state string, args A) Result
	EvRemove(event *Event, states S, args A) Result
	EvAddErr(event *Event, err error, args A) Result
	EvAddErrState(event *Event, state string, err error, args A) Result

	// Waiting (remote)

	WhenArgs(state string, args A, ctx context.Context) <-chan struct{}

	// Getters (remote)

	Err() error

	// ///// LOCAL

	// Checking (local)

	IsErr() bool
	Is(states S) bool
	Is1(state string) bool
	Any(states ...S) bool
	Any1(state ...string) bool
	Not(states S) bool
	Not1(state string) bool
	IsTime(time Time, states S) bool
	WasTime(time Time, states S) bool
	IsClock(clock Clock) bool
	WasClock(clock Clock) bool
	Has(states S) bool
	Has1(state string) bool

	// Waiting (local)

	When(states S, ctx context.Context) <-chan struct{}
	When1(state string, ctx context.Context) <-chan struct{}
	WhenNot(states S, ctx context.Context) <-chan struct{}
	WhenNot1(state string, ctx context.Context) <-chan struct{}
	WhenTime(
		states S, times Time, ctx context.Context) <-chan struct{}
	WhenTime1(state string, tick uint64, ctx context.Context) <-chan struct{}
	WhenTicks(state string, ticks int, ctx context.Context) <-chan struct{}
	WhenErr(ctx context.Context) <-chan struct{}

	// Getters (local)

	StateNames() S
	ActiveStates() S
	Tick(state string) uint64
	Clock(states S) Clock
	Time(states S) Time
	TimeSum(states S) uint64
	NewStateCtx(state string) context.Context
	Export() *Serialized
	Schema() Schema
	Switch(groups ...S) string

	// Misc (local)

	Log(msg string, args ...any)
	Id() string
	ParentId() string
	Tags() []string
	SetLogId(val bool)
	GetLogId() bool
	SetLogger(logger Logger)
	SetLogLevel(lvl LogLevel)
	SetLoggerEmpty(lvl LogLevel)
	SetLoggerSimple(logf func(format string, args ...any), level LogLevel)
	Ctx() context.Context
	String() string
	StringAll() string
	Inspect(states S) string
	Index(state string) int
	BindHandlers(handlers any) error
	DetachHandlers(handlers any) error
	StatesVerified() bool
	Tracers() []Tracer
	DetachTracer(tracer Tracer) bool
	BindTracer(tracer Tracer) error
	Dispose()
	WhenDisposed() <-chan struct{}
	IsDisposed() bool
}

// ///// ///// /////

// ///// ENUMS

// ///// ///// /////

// Result enum is the result of a state Transition.
type Result int8

const (
	// Executed means that the transition was executed immediately and not
	// canceled.
	Executed Result = 1 << iota
	// Canceled means that the transition was canceled, by either relations or a
	// handler.
	Canceled
	// Queued means that the transition was queued for later execution. The
	// following methods can be used to wait for the results:
	// - Machine.When
	// - Machine.WhenNot
	// - Machine.WhenArgs
	// - Machine.WhenTime
	// - Machine.WhenTicks
	// - Machine.WhenTicksEq
	Queued
	// ResultNoOp means that the transition was a no-op, i.e. the state was
	// already active. ResultNoOp is only used by helpers, and never returned by
	// the machine itself.
	ResultNoOp
)

var (
	// ErrStateUnknown indicates that the state is unknown.
	ErrStateUnknown = errors.New("state unknown")
	// ErrStateInactive indicates that a necessary state isn't active.
	ErrStateInactive = errors.New("state not active")
	// ErrCanceled indicates that a transition was canceled.
	ErrCanceled = errors.New("transition canceled")
	// ErrQueued indicates that a transition was queued.
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
	// ErrSchema indicates an issue with the state schema.
	ErrSchema = errors.New("machine disposed")
)

func (r Result) String() string {
	switch r {
	case Executed:
		return "executed"
	case Canceled:
		return "canceled"
	case Queued:
		return "queued"
	}
	return ""
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

	// specific context for this mutation (optional)
	ctx context.Context
	// optional eval func, only for mutationEval
	eval func()
}

func (m Mutation) String() string {
	return fmt.Sprintf("[%s] %v", m.Type, m.Called)
}

func (m Mutation) IsCalled(idx int) bool {
	return slices.Contains(m.Called, idx)
}

func (m Mutation) CalledIndex(index S) *TimeIndex {
	return NewTimeIndex(index, m.Called)
}

func (m Mutation) StringFromIndex(index S) string {
	called := NewTimeIndex(index, m.Called)
	return "[" + m.Type.String() + "] " + j(called.ActiveStates())
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
		return "set"
	case StepRemove:
		return "remove"
	case StepRemoveNotActive:
		return "removenotactive"
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
	// Deprecated, use GetToState(). TODO remove
	FromState    string
	FromStateIdx int
	// Deprecated, use GetFromState(). TODO remove
	ToState    string
	ToStateIdx int
	// Deprecated, use RelType. TODO remove
	Data any
	// TODO emitter
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

func (s *Step) StringFromIdx(idx S) string {
	var line string
	from := s.GetFromState(idx)
	to := s.GetToState(idx)

	if from != "" && to != "" {
		line += from + " -> " + to
	} else {
		line = from
		if line == "" {
			line = to
		}
	}

	// TODO infer handler names
	return line + " (" + s.Type.String() + ")"
}

func newStep(from string, to string, stepType StepType,
	relType Relation,
) *Step {
	// TODO optimize with state indexes
	return &Step{
		// TODO refac v0.11
		FromState: from,
		ToState:   to,
		Type:      stepType,
		RelType:   relType,
	}
}

func newSteps(from string, toStates S, stepType StepType,
	relType Relation,
) []*Step {
	var ret []*Step
	for _, to := range toStates {
		ret = append(ret, newStep(from, to, stepType, relType))
	}
	return ret
}

// Relation enum
type Relation int8

const (
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
