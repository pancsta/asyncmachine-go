package machine

import (
	"context"
	"errors"
	"slices"
	"time"
)

const (
	// EnvAmDebug enables a simple debugging mode (eg long timeouts).
	// "1" | "2" | "" (default)
	EnvAmDebug = "AM_DEBUG"
	// EnvAmLog sets the log level.
	// "1" | "2" | "3" | "4" | "" (default)
	EnvAmLog = "AM_LOG"
	// EnvAmLogFile enables file logging (use machine ID as name).
	// "1" | "" (default)
	EnvAmLogFile = "AM_LOG_FILE"
	// EnvAmDetectEval detects evals directly in handlers (use in tests).
	EnvAmDetectEval = "AM_DETECT_EVAL"
	// EnvAmTraceFilter will remove its contents from stack traces, making them
	// shorter.
	EnvAmTraceFilter = "AM_TRACE_FILTER"
	// EnvAmTestDebug activates debugging in tests.
	EnvAmTestDebug = "AM_TEST_DEBUG"
	// HandlerGlobal is the name of a global transition handler.
	HandlerGlobal = "AnyAny"
)

// S (state names) is a string list of state names.
type S []string

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

// Time is machine time, an ordered list of state ticks. It's like Clock, but
// indexed by int, instead of string.
// TODO use math/big?
type Time []uint64

// Clock is a map of state names to their clock tick values. It's like Time, but
// indexed by string, instead of int.
type Clock map[string]uint64

// HandlerFinal TODO support in BindHandlers
type HandlerFinal func(*Event)

// HandlerNegotiation TODO support in BindHandlers
type HandlerNegotiation func(*Event) bool

// Opts struct is used to configure a new Machine.
type Opts struct {
	// Unique ID of this machine. Default: random ID.
	ID string
	// Time for a handler to execute. Default: time.Second
	HandlerTimeout time.Duration
	// If true, the machine will NOT print all exceptions to stdout.
	DontLogStackTrace bool
	// If true, the machine will die on panics.
	DontPanicToException bool
	// If true, the machine will NOT prefix its logs with its ID.
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

	// Waiting (remote)

	WhenArgs(state string, args A, ctx context.Context) <-chan struct{}

	// Getters (remote)

	Err() error

	// ///// LOCAL

	// Checking (local)

	IsErr() bool
	Is(states S) bool
	Is1(state string) bool
	Not(states S) bool
	Not1(state string) bool
	Any(states ...S) bool
	Any1(state ...string) bool
	Has(states S) bool
	Has1(state string) bool
	IsTime(time Time, states S) bool
	IsClock(clock Clock) bool

	// Waiting (local)

	When(states S, ctx context.Context) <-chan struct{}
	When1(state string, ctx context.Context) <-chan struct{}
	WhenNot(states S, ctx context.Context) <-chan struct{}
	WhenNot1(state string, ctx context.Context) <-chan struct{}
	WhenTime(
		states S, times Time, ctx context.Context) <-chan struct{}
	WhenTicks(state string, ticks int, ctx context.Context) <-chan struct{}
	WhenTicksEq(state string, tick uint64, ctx context.Context) <-chan struct{}
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
	GetStruct() Struct
	Switch(groups ...S) string

	// Misc (local)

	Log(msg string, args ...any)
	Id() string
	ParentId() string
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
	StatesVerified() bool
	Tracers() []Tracer
	DetachTracer(tracer Tracer) bool
	BindTracer(tracer Tracer)
	Dispose()
	WhenDisposed() <-chan struct{}
	IsDisposed() bool
}

// ///// ///// /////

// ///// ENUMS

// ///// ///// /////

// Result enum is the result of a state Transition.
type Result int

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
	// ErrCanceled can be used to indicate a canceled Transition.
	ErrCanceled = errors.New("transition canceled")
	// ErrQueued can be used to indicate a queued Transition.
	ErrQueued = errors.New("transition queued")
	// ErrInvalidArgs can be used to indicate invalid arguments.
	ErrInvalidArgs = errors.New("invalid arguments")
	// ErrHandlerTimeout can be used to indicate timed out mutation.
	ErrHandlerTimeout = errors.New("handler timeout")
	// ErrTimeout can be used to indicate a timeout.
	ErrTimeout = errors.New("timeout")
	// ErrStateMissing is used to indicate missing states.
	ErrStateMissing = errors.New("missing states")
	// ErrRelation is used to indicate a relation definition error.
	ErrRelation = errors.New("relation error")
	// ErrDisposed is used to indicate that the machine has been disposed.
	ErrDisposed = errors.New("machine disposed")
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
	// states explicitly passed to the mutation method
	// TODO optimize as index-based CalledStates() S
	CalledStates S
	// argument map passed to the mutation method (if any).
	Args A
	// this mutation has been triggered by an auto state
	Auto bool
	// specific context for this mutation (optional)
	ctx context.Context
	// optional eval func, only for mutationEval
	eval func()
}

// StateWasCalled returns true if the Mutation was called (directly) with the
// passed state (in opposite to it coming from an `Add` relation).
// TODO change to CalledIs(), CalledIs1(), CalledAny(), CalledAny1()
func (m Mutation) StateWasCalled(state string) bool {
	return slices.Contains(m.CalledStates, state)
}

// StepType enum
type StepType int

const (
	StepRelation StepType = 1 << iota
	StepHandler
	StepSet
	// StepRemove indicates a step where a state goes active->inactive
	StepRemove
	// StepRemoveNotActive indicates a step where a state goes inactive->inactive
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
	// optional, unless no ToState
	// TODO optimize as index-based FromState() S
	FromState string
	// optional, unless no FromState
	// TODO optimize as index-based ToState() S
	ToState string
	// eg a transition method name, relation type
	Data any
	// marks a final handler (FooState, FooEnd)
	IsFinal bool
	// marks a self handler (FooFoo)
	IsSelf bool
	// marks an enter handler (FooEnter). Requires IsFinal.
	IsEnter bool
}

func newStep(from string, to string, stepType StepType,
	data any,
) *Step {
	// TODO optimize with state indexes
	return &Step{
		FromState: from,
		ToState:   to,
		Type:      stepType,
		// TODO refac to RelType
		Data: data,
	}
}

func newSteps(from string, toStates S, stepType StepType,
	data any,
) []*Step {
	var ret []*Step
	for _, to := range toStates {
		ret = append(ret, newStep(from, to, stepType, data))
	}
	return ret
}

// Relation enum
type Relation int

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
