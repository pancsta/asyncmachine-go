package machine

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/samber/lo"
)

// S (state names) is a string list of state names.
type S []string

// A (arguments) is a map of named method arguments.
type A map[string]any

// T is an ordered list of state clocks.
type T []uint64

// State defines a single state of a machine, its properties and relations.
type State struct {
	Auto    bool
	Multi   bool
	Require S
	Add     S
	Remove  S
	After   S
}

// States is a map of state names to state definitions.
type States = map[string]State

type Event struct {
	Name    string
	Machine *Machine
	Args    A
	// internal events lack a step
	step    *TransitionStep
}

type Opts struct {
	// Unique ID of this machine. Default: random UUID.
	ID string
	// Time for a handler to execute. Default: time.Second
	HandlerTimeout time.Duration
	// If true, the machine will NOT print all exceptions to stdout.
	DontPrintExceptions bool
	// If true, the machine will die on panics.
	DontPanicToException bool
	// If true, the machine will NOT prefix its logs with its ID.
	DontLogID bool
	// Custom relations resolver. Default: *DefaultRelationsResolver.
	Resolver RelationsResolver
	// Log level of the machine. Default: LogNothing.
	LogLevel LogLevel
}

// Result enum is the result of a state Transition
type Result int

const (
	Executed Result = 1 << iota
	Canceled
	Queued
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
	MutationTypeAdd MutationType = iota
	MutationTypeRemove
	MutationTypeSet
)

func (m MutationType) String() string {
	switch m {
	case MutationTypeAdd:
		return "add"
	case MutationTypeRemove:
		return "remove"
	case MutationTypeSet:
		return "set"
	}
	return ""
}

type Mutation struct {
	Type         MutationType
	CalledStates S
	Args         A
	// this mutation has been triggered by an auto state
	Auto bool
}

// TransitionStepType enum
type TransitionStepType int

const (
	TransitionStepTypeRelation TransitionStepType = 1 << iota
	TransitionStepTypeTransition
	TransitionStepTypeSet
	TransitionStepTypeRemove
	TransitionStepTypeNoSet
	TransitionStepTypeRequested
	TransitionStepTypeCancel
)

func (tt TransitionStepType) String() string {
	switch tt {
	case TransitionStepTypeRelation:
		return "rel"
	case TransitionStepTypeTransition:
		return "transition"
	case TransitionStepTypeSet:
		return "set"
	case TransitionStepTypeRemove:
		return "remove"
	case TransitionStepTypeNoSet:
		return "no-set"
	case TransitionStepTypeRequested:
		return "requested"
	case TransitionStepTypeCancel:
		return "cancel"
	}
	return ""
}

type TransitionStep struct {
	ID        string
	FromState string
	ToState   string
	Type      TransitionStepType
	// eg a transition method name, relation type
	Data any
	// marks a final handler (FooState, FooEnd)
	IsFinal bool
	// marks a self handler (FooFoo)
	IsSelf bool
	// marks an enter handler (FooEnter). Requires IsFinal.
	IsEnter bool
}

// Relation enum
type Relation int

const (
	RelationAfter Relation = iota
	RelationAdd
	RelationRequire
	RelationRemove
)

func (r Relation) String(relation Relation) string {
	switch relation {
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

type HandlerBinding struct {
	Ready chan struct{}
}

type Logger func(level LogLevel, msg string, args ...any)

// LogLevel enum
type LogLevel int

const (
	LogNothing LogLevel = iota
	LogChanges
	LogOps
	LogDecisions
	LogEverything
)

type (
	// map of (single) state names to a list of bindings
	indexWhen map[string][]*whenBinding
	// map of (single) state names to a list of bindings
	indexStateCtx map[string][]context.CancelFunc
	indexEventCh map[string][]chan *Event
)

type whenBinding struct {
	ch       chan struct{}
	negation bool
	states   stateIsActive
	matched  int
	total    int
}

type stateIsActive map[string]bool

// emitter represents a single event consumer, synchronized by channels.
type emitter struct {
	id       string
	disposed bool
	methods  *reflect.Value
}

// newEmitter creates a new emitter for Machine.
// Each emitter should be consumed by one receiver only to guarantee the
// delivery of all events.
func (m *Machine) newEmitter(name string, methods *reflect.Value) *emitter {
	e := &emitter{
		id:       name,
		methods:  methods,
	}
	// TODO emitter mutex
	m.emitters = append(m.emitters, e)
	return e
}

func (e *emitter) dispose() {
	e.disposed = true
	e.methods = nil
}

// DiffStates returns the states that are in states1 but not in states2.
func DiffStates(states1 S, states2 S) S {
	return lo.Filter(states1, func(name string, i int) bool {
		return !lo.Contains(states2, name)
	})
}

// IsTimeAfter checks if time1 is after time2. Requires ordered results from
// Machine.Time() (with specified states).
func IsTimeAfter(time1, time2 T) bool {
	after := false
	for i, t1 := range time1 {
		if t1 < time2[i] {
			return false
		}
		if t1 > time2[i] {
			after = true
		}
	}
	return after
}

// ExceptionHandler provide basic Exception state support.
type ExceptionHandler struct{}

// ExceptionArgsPanic is an optional argument for the Exception state which
// describes a panic within a transition handler.
type ExceptionArgsPanic struct {
	CalledStates S
	StatesBefore S
	Transition   *Transition
	LastStep     *TransitionStep
	StackTrace   []byte
}

// ExceptionState is a (final) handler for the Exception state.
// Args:
// - err error: The error that caused the Exception state.
// - panic *ExceptionArgsPanic: Optional details about the panic.
func (eh *ExceptionHandler) ExceptionState(e *Event) {
	if e.Machine.PrintExceptions {
		err := e.Args["err"].(error)
		details := e.Args["panic"].(*ExceptionArgsPanic)
		if details == nil {
			e.Machine.log(LogChanges, "[error:%s] %s (%s)", err)
			return
		}
		mutType := details.Transition.Mutation.Type
		e.Machine.log(LogChanges, "[error:%s] %s (%s)", mutType,
			j(details.CalledStates), err)
		if details.StackTrace != nil {
			e.Machine.log(LogEverything, "[error:trace] %s", details.StackTrace)
		}
	}
}

// utils

// j joins state names
func j(states []string) string {
	return strings.Join(states, " ")
}

// jw joins state names with `sep`.
func jw(states []string, sep string) string {
	return strings.Join(states, sep)
}

func closeSafe[T any](ch chan T) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func padString(str string, length int, pad string) string {
	for {
		str += pad
		if len(str) > length {
			return str[0:length]
		}
	}
}
