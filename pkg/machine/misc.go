package machine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// S (state names) is a string list of state names.
type S []string

// A (arguments) is a map of named arguments for a Mutation.
type A map[string]any

// T is an ordered list of state clocks.
// TODO use math/big?
type T []uint64

// Clocks is a map of state names to their clocks.
type Clocks map[string]uint64

// State defines a single state of a machine, its properties and relations.
type State struct {
	Auto    bool
	Multi   bool
	Require S
	Add     S
	Remove  S
	After   S
}

// Struct is a map of state names to state definitions.
type Struct = map[string]State

// ///// ///// /////
// ///// OPTIONS
// ///// ///// /////

// Opts struct is used to configure a new Machine.
type Opts struct {
	// Unique ID of this machine. Default: random ID.
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
	// Tracer for the machine. Default: nil.
	Tracers []Tracer
	// LogArgs matching function for the machine. Default: nil.
	LogArgs func(args A) map[string]string
	// Parent machine, used to inherit certain properties, e.g. tracers.
	// Overrides ParentID. Default: nil.
	Parent *Machine
	// ParentID is the ID of the parent machine. Default: "".
	ParentID string
	// Tags is a list of tags for the machine. Default: nil.
	Tags []string
	// QueueLimit is the maximum number of mutations that can be queued.
	// Default: 1000.
	// TODO per-state QueueLimit
	QueueLimit int
}

// OptsWithDebug returns Opts with debug settings (DontPanicToException,
// long HandlerTimeout).
func OptsWithDebug(opts *Opts) *Opts {
	opts.DontPanicToException = true
	opts.HandlerTimeout = 10 * time.Minute

	return opts
}

// OptsWithTracers returns Opts with the given tracers. Tracers are inherited
// by submachines (via Opts.Parent) when env.AM_DEBUG is set.
func OptsWithTracers(opts *Opts, tracers ...Tracer) *Opts {
	if tracers != nil {
		opts.Tracers = tracers
	}

	return opts
}

// OptsWithParentTracers returns Opts with the parent's Opts.Tracers and
// Opts.LogArgs.
func OptsWithParentTracers(opts *Opts, parent *Machine) *Opts {
	var tracers []Tracer

	tracers = append(tracers, parent.Tracers...)
	opts.Tracers = tracers
	opts.LogArgs = parent.GetLogArgs()

	return opts
}

// ///// ///// /////
// ///// ENUMS
// ///// ///// /////

// Result enum is the result of a state Transition
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
	Queued
	// ResultNoOp means that the transition was a no-op, i.e. the state was
	// already active. ResultNoOp is only used by helpers, and never returned by
	// the machine itself.
	ResultNoOp
)

var (
	// ErrStateUnknown indicates that the state is unknown.
	ErrStateUnknown = errors.New("state unknown")
	// ErrCanceled can be used to indicate a canceled Transition. Not used ATM.
	ErrCanceled = errors.New("transition canceled")
	// ErrQueued can be used to indicate a queued Transition. Not used ATM.
	ErrQueued = errors.New("transition queued")
	// ErrInvalidArgs can be used to indicate invalid arguments. Not used ATM.
	ErrInvalidArgs = errors.New("invalid arguments")
	// ErrTimeout can be used to indicate timed out mutation. Not used ATM.
	ErrTimeout = errors.New("timeout")
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

const (
	// EventTransitionEnd transition ended
	EventTransitionEnd = "transition-end"
	// EventTransitionInit transition being initialized
	EventTransitionInit = "transition-init"
	// EventTransitionStart transition started
	EventTransitionStart = "transition-start"
	// EventTransitionCancel transition canceled
	EventTransitionCancel = "transition-cancel"
	// EventQueueEnd queue fully drained
	EventQueueEnd = "queue-end"
	// EventQueueAdd new mutation queued
	EventQueueAdd = "queue-add"
	// EventStructChange machine's states structure have changed
	EventStructChange = "struct-change"
	// EventTick active states changed, at least one state clock increased
	EventTick = "tick"
	// EventException Exception state becomes active
	EventException = "exception"
)

// MutationType enum
type MutationType int

const (
	MutationAdd MutationType = iota
	MutationRemove
	MutationSet
	MutationEval
)

func (m MutationType) String() string {
	switch m {
	case MutationAdd:
		return "add"
	case MutationRemove:
		return "remove"
	case MutationSet:
		return "set"
	case MutationEval:
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
	CalledStates S
	// argument map passed to the mutation method (if any).
	Args A
	// this mutation has been triggered by an auto state
	Auto bool
	// specific context for this mutation (optional)
	Ctx context.Context
	// optional eval func, only for MutationEval
	Eval func()
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
	FromState string
	// optional, unless no FromState
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

// ///// ///// /////
// ///// LOGGING
// ///// ///// /////

// Logger is a logging function for the machine.
type Logger func(level LogLevel, msg string, args ...any)

// LogLevel enum
type LogLevel int

type LogEntry struct {
	Level LogLevel
	Text  string
}

const (
	// LogNothing means no logging, including external msgs.
	LogNothing LogLevel = iota
	// LogChanges means logging state changes and external msgs.
	LogChanges
	// LogOps means LogChanges + logging all the operations.
	LogOps
	// LogDecisions means LogOps + logging all the decisions behind them.
	LogDecisions
	// LogEverything means LogDecisions + all event and emitter names, and more.
	LogEverything
)

func (l LogLevel) String() string {
	switch l {
	case LogNothing:
		return "nothing"
	case LogChanges:
		return "changes"
	case LogOps:
		return "ops"
	case LogDecisions:
		return "decisions"
	case LogEverything:
		return "everything"
	}
	return "nothing"
}

// NewArgsMapper returns a matcher function for LogArgs. Useful for debugging
// untyped argument maps.
//
// maxlen: maximum length of the string representation of the argument
// (default=20).
func NewArgsMapper(names []string, maxlen int) func(args A) map[string]string {
	if maxlen == 0 {
		maxlen = 20
	}
	return func(args A) map[string]string {
		oks := make([]bool, len(names))
		found := 0
		for i, name := range names {
			_, ok := args[name]
			oks[i] = ok
			found++
		}
		if found == 0 {
			return nil
		}
		ret := make(map[string]string)
		for i, name := range names {
			if !oks[i] {
				continue
			}
			ret[name] = truncateStr(fmt.Sprintf("%v", args[name]), maxlen)
		}
		return ret
	}
}

// Tracer is an interface for logging machine transitions and events, used by
// Opts.Tracers.
type Tracer interface {
	TransitionInit(transition *Transition)
	TransitionEnd(transition *Transition)
	HandlerStart(transition *Transition, emitter string, handler string)
	HandlerEnd(transition *Transition, emitter string, handler string)
	End()
	MachineInit(machine *Machine)
	MachineDispose(machID string)
	NewSubmachine(parent, machine *Machine)
	Inheritable() bool
	// TODO NewStatesStruct
	// TODO VerifyStates
	QueueEnd(machine *Machine)
}

// NoOpTracer is a no-op implementation of Tracer, used for embedding.
type NoOpTracer struct{}

func (t *NoOpTracer) TransitionInit(transition *Transition) {}
func (t *NoOpTracer) TransitionEnd(transition *Transition)  {}
func (t *NoOpTracer) HandlerStart(
	transition *Transition, emitter string, handler string) {
}

func (t *NoOpTracer) HandlerEnd(
	transition *Transition, emitter string, handler string) {
}
func (t *NoOpTracer) End()                                {}
func (t *NoOpTracer) MachineInit(mach *Machine)           {}
func (t *NoOpTracer) MachineDispose(machID string)        {}
func (t *NoOpTracer) NewSubmachine(parent, mach *Machine) {}
func (t *NoOpTracer) QueueEnd(machine *Machine)           {}
func (t *NoOpTracer) Inheritable() bool                   { return false }

// ///// ///// /////
// ///// EVENTS, WHEN, EMITTERS
// ///// ///// /////

// Event struct represents a single event of a Mutation withing a Transition.
// One event can have 0-n handlers.
type Event struct {
	// Name of the event / handler
	Name string
	// Machine is the machine that the event belongs to, it can be used to access
	// the current Transition and Mutation.
	Machine *Machine
	// Args is a map of named arguments for a Mutation.
	Args A
	// internal events lack a step
	step *Step
}

// Mutation returns the Mutation of an Event.
func (e *Event) Mutation() *Mutation {
	return e.Machine.Transition.Mutation
}

// Transition returns the Transition of an Event.
func (e *Event) Transition() *Transition {
	return e.Machine.Transition
}

type (
	// map of (single) state names to a list of activation / de-activation
	// bindings
	indexWhen map[string][]*whenBinding
	// map of (single) state names to a list of time bindings
	indexWhenTime map[string][]*whenTimeBinding
	// map of (single) state names to a list of args value bindings
	indexWhenArgs map[string][]*whenArgsBinding
	// map of (single) state names to a context cancel function
	indexStateCtx map[string][]context.CancelFunc
	// map of (single) state names to an event channel
	indexEventCh map[string][]chan *Event
)

type whenBinding struct {
	ch chan struct{}
	// means states are required to NOT be active
	negation bool
	states   stateIsActive
	matched  int
	total    int
}

type whenTimeBinding struct {
	ch chan struct{}
	// map of completed to their index positions
	index map[string]int
	// number of matches so far
	matched int
	// number of total matches needed
	total int
	// optional times to match for completed from index
	times     T
	completed stateIsActive
}

type whenArgsBinding struct {
	ch   chan struct{}
	args A
}

type whenQueueBinding struct {
	ch chan struct{}
}

type stateIsActive map[string]bool

// emitter represents a single event consumer, synchronized by channels.
type emitter struct {
	id       string
	disposed bool
	methods  *reflect.Value
}

func (e *emitter) dispose() {
	e.disposed = true
	e.methods = nil
}

// ///// ///// /////
// ///// EXCEPTION SUPPORT
// ///// ///// /////

const (
	// Exception is a name the Exception state.
	Exception = "Exception"
	// Any is a name of a meta state used in catch-all handlers.
	Any = "Any"
)

// ExceptionArgsPanic is an optional argument ["panic"] for the Exception state
// which describes a panic within a Transition handler.
type ExceptionArgsPanic struct {
	CalledStates S
	StatesBefore S
	Transition   *Transition
	LastStep     *Step
	StackTrace   []byte
}

// ExceptionHandler provide a basic Exception state support, as should be
// embedded into handler structs in most of the cases.
type ExceptionHandler struct{}

// ExceptionState is a final entry handler for the Exception state.
// Args:
// - err error: The error that caused the Exception state.
// - panic *ExceptionArgsPanic: Optional details about the panic.
func (eh *ExceptionHandler) ExceptionState(e *Event) {
	if e.Machine.PrintExceptions {
		err := e.Args["err"].(error)
		_, ok := e.Args["panic"]
		if !ok {
			// TODO more mutation info
			e.Machine.log(LogChanges, "[error] %s", err)
			return
		}
		details := e.Args["panic"].(*ExceptionArgsPanic)
		mutType := details.Transition.Mutation.Type
		e.Machine.log(LogChanges, "[error:%s] %s (%s)", mutType,
			j(details.CalledStates), err)
		if details.StackTrace != nil {
			e.Machine.log(LogChanges, "[error:trace] %s", details.StackTrace)
		}
	}
}

// ///// ///// /////
// ///// PUB UTILS
// ///// ///// /////

// DiffStates returns the states that are in states1 but not in states2.
func DiffStates(states1 S, states2 S) S {
	return slicesFilter(states1, func(name string, i int) bool {
		return !slices.Contains(states2, name)
	})
}

// IsTimeAfter checks if time1 is after time2. Requires a deterministic states
// order, e.g. by using Machine.VerifyStates.
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

// CloneStates deep clones the states struct and returns a copy.
func CloneStates(states Struct) Struct {
	ret := make(Struct)

	for name, state := range states {

		stateCopy := State{
			Auto:  state.Auto,
			Multi: state.Multi,
		}

		// TODO move to Resolver

		if state.Require != nil {
			stateCopy.Require = slices.Clone(state.Require)
		}
		if state.Add != nil {
			stateCopy.Add = slices.Clone(state.Add)
		}
		if state.Remove != nil {
			stateCopy.Remove = slices.Clone(state.Remove)
		}
		if state.After != nil {
			stateCopy.After = slices.Clone(state.After)
		}

		ret[name] = stateCopy
	}

	return ret
}

// IsActiveTick returns true if the tick represents an active state
// (odd number).
func IsActiveTick(tick uint64) bool {
	return tick%2 == 1
}

// everything else than a-z and _
var invalidName = regexp.MustCompile("[^a-z_0-9]+")

func NormalizeID(id string) string {
	return invalidName.ReplaceAllString(strings.ToLower(id), "_")
}

// SMerge merges multiple state lists into one, removing duplicates.
// Especially useful for merging state group with single states.
func SMerge(states ...S) S {
	if len(states) == 0 {
		return S{}
	}

	s := slices.Clone(states[0])
	for i := 1; i < len(states); i++ {
		s = append(s, states[i]...)
	}

	return slicesUniq(s)
}

type Export struct {
	ID         string `json:"id"`
	Clocks     Clocks `json:"clocks"`
	StateNames S      `json:"state_names"`
}

// ///// ///// /////
// ///// UTILS
// ///// ///// /////

// j joins state names into a single string
func j(states []string) string {
	return strings.Join(states, " ")
}

// jw joins state names into a single string with a separator.
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

// TODO move to Resolver
func parseStruct(states Struct) Struct {
	// TODO capitalize states

	parsedStates := CloneStates(states)
	for name, state := range states {

		// avoid self removal
		if slices.Contains(state.Remove, name) {
			state.Remove = slicesWithout(state.Remove, name)
		}

		// don't Remove if in Add
		for _, add := range state.Add {
			if slices.Contains(state.Remove, add) {
				state.Remove = slicesWithout(state.Remove, add)
			}
		}

		// avoid being after itself
		if slices.Contains(state.After, name) {
			state.After = slicesWithout(state.After, name)
		}

		parsedStates[name] = state
	}

	return parsedStates
}

// compareArgs return true if args2 is a subset of args1.
func compareArgs(args1, args2 A) bool {
	match := true

	for k, v := range args2 {
		// TODO better comparisons
		if args1[k] != v {
			match = false
			break
		}
	}

	return match
}

func truncateStr(s string, maxLength int) string {
	if len(s) <= maxLength {
		return s
	}
	if maxLength < 5 {
		return s[:maxLength]
	} else {
		return s[:maxLength-3] + "..."
	}
}

type handlerCall struct {
	fn      reflect.Value
	event   *Event
	timeout bool
}

func randID() string {
	id := make([]byte, 16)
	_, err := rand.Read(id)
	if err != nil {
		return "error"
	}

	return hex.EncodeToString(id)
}

func slicesWithout[S ~[]E, E comparable](coll S, el E) S {
	idx := slices.Index(coll, el)
	ret := slices.Clone(coll)
	if idx == -1 {
		return ret
	}
	return slices.Delete(ret, idx, idx+1)
}

// slicesNone returns true if none of the elements of coll2 are in coll1.
func slicesNone[S1 ~[]E, S2 ~[]E, E comparable](col1 S1, col2 S2) bool {
	for _, el := range col2 {
		if slices.Contains(col1, el) {
			return false
		}
	}
	return true
}

// slicesEvery returns true if all elements of coll2 are in coll1.
func slicesEvery[S1 ~[]E, S2 ~[]E, E comparable](col1 S1, col2 S2) bool {
	for _, el := range col2 {
		if !slices.Contains(col1, el) {
			return false
		}
	}
	return true
}

func slicesFilter[S ~[]E, E any](coll S, fn func(item E, i int) bool) S {
	var ret S
	for i, el := range coll {
		if fn(el, i) {
			ret = append(ret, el)
		}
	}
	return ret
}

func slicesReverse[S ~[]E, E any](coll S) S {
	ret := make(S, len(coll))
	for i := range coll {
		ret[i] = coll[len(coll)-1-i]
	}
	return ret
}

func slicesUniq[S ~[]E, E comparable](coll S) S {
	var ret S
	for _, el := range coll {
		if !slices.Contains(ret, el) {
			ret = append(ret, el)
		}
	}
	return ret
}

// diposeWithCtx handles early binding disposal caused by a canceled context.
// It's used by most of "when" methods.
func diposeWithCtx[T comparable](
	mach *Machine, ctx context.Context, ch chan struct{}, states S, binding T,
	lock *sync.RWMutex, index map[string][]T,
) {
	if ctx == nil {
		return
	}
	go func() {
		select {
		case <-ch:
			return
		case <-mach.Ctx.Done():
			return
		case <-ctx.Done():
		}
		// GC only if needed
		if mach.Disposed.Load() {
			return
		}

		// TODO track
		closeSafe(ch)

		lock.Lock()
		defer lock.Unlock()

		for _, s := range states {
			if _, ok := index[s]; ok {
				if len(index[s]) == 1 {
					delete(index, s)
				} else {
					index[s] = slicesWithout(index[s], binding)
				}
			}
		}
	}()
}
