package machine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Time is machine time, an ordered list of state ticks. It's like Clock, but
// indexed by int, instead of string.
// TODO use math/big?
type Time []uint64

// Time TODO Any, Any1, Not, Not1

// Get returns the tick at the given index, or 0 if out of bounds (for old
// schemas).
func (t Time) Get(idx int) uint64 {
	// out of bound falls back to 0
	if len(t) <= idx {
		return 0
	}

	return t[idx]
}

// Is1 checks if a state is active at a given time, via its index. See
// Machine.Index().
func (t Time) Is1(idx int) bool {
	if idx == -1 || idx >= len(t) {
		return false
	}
	return IsActiveTick(t[idx])
}

// Is checks if all the passed states were active at a given time, via indexes.
// See Machine.Index().
func (t Time) Is(idxs []int) bool {
	if len(idxs) == 0 {
		return false
	}

	for _, idx := range idxs {
		// -1 is not found or mach disposed
		if idx == -1 {
			return false
		}
		if !IsActiveTick(t[idx]) {
			return false
		}
	}

	return true
}

// TODO docs
func (t Time) Not(idxs []int) bool {
	if len(idxs) == 0 {
		return true
	}

	for _, idx := range idxs {
		// -1 is not found or mach disposed
		if idx != -1 && IsActiveTick(t[idx]) {
			return false
		}
	}

	return true
}

// TODO docs
func (t Time) Not1(idx int) bool {
	if idx == -1 || idx >= len(t) {
		return false
	}

	return !IsActiveTick(t[idx])
}

// TODO Any

// Any1 see Machine.Any1.
func (t Time) Any1(idxs ...int) bool {
	if len(idxs) == 0 {
		return false
	}

	for _, idx := range idxs {
		if IsActiveTick(t[idx]) {
			return true
		}
	}

	return false
}

func (t Time) String() string {
	ret := ""
	for _, tick := range t {
		if ret != "" {
			ret += " "
		}
		ret += strconv.Itoa(int(tick))
	}

	return ret
}

// ActiveStates returns a list of active state names in this machine time slice.
func (t Time) ActiveStates(index S) S {
	ret := S{}
	for i, tick := range t {
		if !IsActiveTick(tick) {
			continue
		}
		name := "unknown" + strconv.Itoa(i)
		if len(index) > i {
			name = index[i]
		}
		ret = append(ret, name)
	}

	return ret
}

// ActiveIndex returns a list of active state indexes in this machine time
// slice.
func (t Time) ActiveIndex() []int {
	ret := []int{}
	for i, tick := range t {
		if !IsActiveTick(tick) {
			continue
		}
		ret = append(ret, i)
	}

	return ret
}

// Sum returns the sum of all the ticks in Time.
func (t Time) Sum() uint64 {
	var sum uint64
	for _, idx := range t {
		sum += idx
	}

	return sum
}

// TODO Time(states) - part of [Api]

func (t Time) TimeSum(idxs []int) uint64 {
	if len(idxs) == 0 {
		return t.Sum()
	}

	var sum uint64
	for _, idx := range idxs {
		sum += t[idx]
	}

	return sum
}

// DiffSince returns the number of ticks for each state in Time since the
// passed machine time.
func (t Time) DiffSince(before Time) Time {
	ret := make(Time, len(t))
	if len(t) != len(before) {
		return ret
	}

	for i := range before {
		ret[i] = t[i] - before[i]
	}

	return ret
}

// Add sums 2 instances of Time and returns a new one.
func (t Time) Add(t2 Time) Time {
	ret := make(Time, len(t))
	if len(t) != len(t2) {
		return t
	}

	for i := range t2 {
		ret[i] = t[i] + t2[i]
	}

	return ret
}

// TimeIndex

// TimeIndex is Time with a bound state index (list of state names). It's not
// suitable for storage, use Time instead.
type TimeIndex struct {
	Time
	Index S
}

func NewTimeIndex(index S, activeStates []int) *TimeIndex {
	ret := &TimeIndex{
		Index: index,
		Time:  make(Time, len(index)),
	}
	for _, idx := range activeStates {
		ret.Time[idx] = 1
	}

	return ret
}

func (t TimeIndex) StateName(idx int) string {
	if idx >= len(t.Index) {
		return ""
	}

	return t.Index[idx]
}

// all methods from Time

func (t TimeIndex) Is(states S) bool {
	return t.Time.Is(StatesToIndex(t.Index, states))
}

func (t TimeIndex) Is1(state string) bool {
	return t.Time.Is(StatesToIndex(t.Index, S{state}))
}

func (t TimeIndex) Not(states S) bool {
	return t.Time.Not(StatesToIndex(t.Index, states))
}

func (t TimeIndex) Not1(state string) bool {
	return t.Time.Not(StatesToIndex(t.Index, S{state}))
}

func (t TimeIndex) Any1(states ...string) bool {
	return t.Time.Any1(StatesToIndex(t.Index, states)...)
}

func (t TimeIndex) String() string {
	ret := ""
	for i, tick := range t.Time {
		if ret != "" {
			ret += " "
		}
		name := t.StateName(int(tick))
		if name == "" {
			name = "unknown" + strconv.Itoa(i)
		}
		ret += name
	}

	return ret
}

func (t TimeIndex) ActiveStates() S {
	return t.Time.ActiveStates(t.Index)
}

func (t TimeIndex) TimeSum(states S) uint64 {
	return t.Time.TimeSum(StatesToIndex(t.Index, states))
}

// Context

type (
	CtxKeyName struct{}
	CtxValue   struct {
		Id    string
		State string
		Tick  uint64
	}
)

var CtxKey = &CtxKeyName{}

// Options

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

// ///// ///// /////

// ///// LOGGING

// ///// ///// /////

// Logger is a logging function for the machine.
type Logger func(level LogLevel, msg string, args ...any)

// LogArgsMapper is a function that maps arguments to be logged. Useful for
// debugging.
type LogArgsMapper func(args A) map[string]string

type LogEntry struct {
	Level LogLevel
	Text  string
}

// LogLevel enum
type LogLevel int

// TODO spread log level 0 - 10 - 20 - 30 - 40 - 50 - 60
const (
	// LogNothing means no logging, including external msgs.
	LogNothing LogLevel = iota
	// LogExternal will show ony external user msgs.
	LogExternal
	// LogSteps will show external user msgs and also create transition steps.
	LogSteps
	// LogChanges means logging state changes and external msgs.
	LogChanges
	// LogOps means LogChanges + logging all the operations.
	LogOps
	// LogDecisions means LogOps + logging all the decisions behind them.
	LogDecisions
	// LogEverything means LogDecisions + all event and handler names, and more.
	LogEverything
)

func (l LogLevel) String() string {
	switch l {
	case LogExternal:
		return "external"
	case LogSteps:
		return "steps"
	case LogChanges:
		return "changes"
	case LogOps:
		return "ops"
	case LogDecisions:
		return "decisions"
	case LogEverything:
		return "everything"
	default:
		return "nothing"
	}
}

// LogArgs is a list of common argument names to be logged. Useful for
// debugging.
var LogArgs = []string{"name", "id", "port", "addr", "err"}

// LogArgsMaxLen is the default maximum length of the arg's string
// representation.
var LogArgsMaxLen = 20

// NewArgsMapper returns a matcher function for LogArgs. Useful for debugging
// untyped argument maps.
//
// maxLen: maximum length of the arg's string representation). Default to
// LogArgsMaxLen,
func NewArgsMapper(names []string, maxLen int) func(args A) map[string]string {
	if maxLen == 0 {
		maxLen = LogArgsMaxLen
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
			ret[name] = TruncateStr(fmt.Sprintf("%v", args[name]), maxLen)
		}

		return ret
	}
}

// Tracer is an interface for logging machine transitions and events, used by
// Opts.Tracers and Machine.BindTracer.
type Tracer interface {
	TransitionInit(transition *Transition)
	TransitionStart(transition *Transition)
	TransitionEnd(transition *Transition)
	HandlerStart(transition *Transition, emitter string, handler string)
	HandlerEnd(transition *Transition, emitter string, handler string)
	// MachineInit is called only for machines with tracers added via
	// Opts.Tracers.
	MachineInit(machine Api) context.Context
	MachineDispose(machID string)
	NewSubmachine(parent, machine Api)
	Inheritable() bool
	QueueEnd(machine Api)
	SchemaChange(machine Api, old Schema)
	VerifyStates(machine Api)
}

// NoOpTracer is a no-op implementation of Tracer, used for embedding.
type NoOpTracer struct{}

func (t *NoOpTracer) TransitionInit(transition *Transition)  {}
func (t *NoOpTracer) TransitionStart(transition *Transition) {}
func (t *NoOpTracer) TransitionEnd(transition *Transition)   {}
func (t *NoOpTracer) HandlerStart(
	transition *Transition, emitter string, handler string) {
}

func (t *NoOpTracer) HandlerEnd(
	transition *Transition, emitter string, handler string) {
}

func (t *NoOpTracer) MachineInit(machine Api) context.Context {
	return nil
}
func (t *NoOpTracer) MachineDispose(machID string)      {}
func (t *NoOpTracer) NewSubmachine(parent, machine Api) {}
func (t *NoOpTracer) QueueEnd(machine Api)              {}

func (t *NoOpTracer) SchemaChange(machine Api, old Schema) {}
func (t *NoOpTracer) VerifyStates(machine Api)             {}
func (t *NoOpTracer) Inheritable() bool                    { return false }

var _ Tracer = &NoOpTracer{}

// ///// ///// /////

// ///// EVENTS, WHEN, EMITTERS

// ///// ///// /////

var emitterNameRe = regexp.MustCompile(`/\w+\.go:\d+`)

// Event struct represents a single event of a Mutation within a Transition.
// One event can have 0-n handlers.
type Event struct {
	// Name of the event / handler
	Name string
	// MachineId is the ID of the parent machine.
	MachineId string
	// TransitionId is the ID of the parent transition.
	TransitionId string
	// Args is a map of named arguments for a Mutation.
	Args A
	// IsCheck is true if this event is a check event, fired by one of Can*()
	// methods. Useful for avoiding flooding the log with errors.
	IsCheck bool
	// TODO add Source with MachID and TxID (useful for piping)

	// Machine is the machine that the event belongs to. It can be used to access
	// the current Transition and Mutation.
	machine *Machine
}

// Mutation returns the Mutation of an Event.
func (e *Event) Mutation() *Mutation {
	t := e.Machine().Transition()
	if t == nil {
		return nil
	}
	return t.Mutation
}

func (e *Event) Machine() *Machine {
	return e.machine
}

// Transition returns the Transition of an Event.
func (e *Event) Transition() *Transition {
	if e.machine == nil {
		return nil
	}
	return e.Machine().t.Load()
}

// IsValid confirm this event should still be processed. Useful for negotiation
// handlers, which can't use state context.
func (e *Event) IsValid() bool {
	tx := e.Transition()
	if tx == nil {
		return false
	}

	return e.TransitionId == tx.ID && !tx.IsCompleted.Load() &&
		tx.IsAccepted.Load()
}

// Clone clones only the essential data of the Event. Useful for tracing vs GC.
func (e *Event) Clone() *Event {
	id := e.MachineId
	if e.Machine() == nil {
		id = e.Machine().Id()
	}

	return &Event{
		Name:         e.Name,
		MachineId:    id,
		TransitionId: e.TransitionId,
	}
}

type (
	// IndexWhen is a map of (single) state names to a list of activation or
	// de-activation bindings
	IndexWhen map[string][]*WhenBinding
	// IndexWhenTime is a map of (single) state names to a list of time bindings
	IndexWhenTime map[string][]*WhenTimeBinding
	// IndexWhenArgs is a map of (single) state names to a list of args value
	// bindings
	IndexWhenArgs map[string][]*WhenArgsBinding
	// IndexStateCtx is a map of (single) state names to a context cancel function
	IndexStateCtx map[string]*CtxBinding
)

type CtxBinding struct {
	Ctx    context.Context
	Cancel context.CancelFunc
}

type WhenBinding struct {
	Ch chan struct{}
	// means states are required to NOT be active
	Negation bool
	States   StateIsActive
	Matched  int
	Total    int
}

type WhenTimeBinding struct {
	Ch chan struct{}
	// map of completed to their index positions
	// TODO optimize indexes
	Index map[string]int
	// number of matches so far
	Matched int
	// number of total matches needed
	Total int
	// optional Time to match for completed from Index
	Times     Time
	Completed StateIsActive
}

type WhenArgsBinding struct {
	ch   chan struct{}
	args A
}

type whenQueueBinding struct {
	ch chan struct{}
}

// TODO optimize indexes
type StateIsActive map[string]bool

// handler represents a single event consumer, synchronized by channels.
type handler struct {
	h    any
	name string
	mx   sync.Mutex
	// disposed     bool
	methods      *reflect.Value
	methodNames  []string
	methodCache  map[string]reflect.Value
	missingCache map[string]struct{}
}

func (e *handler) dispose() {
	// TODO check if this leaks
	// e.disposed = true
	// e.methods = nil
	// e.methodCache = nil
	// e.methodNames = nil
	// e.h = nil
}

// ///// ///// /////

// ///// ERROR HANDLING

// ///// ///// /////

const (
	// Exception is the name of the Exception state.
	Exception   = "Exception"
	Heartbeat   = "Heartbeat"
	Healthcheck = "Healthcheck"
)

// ExceptionArgsPanic is an optional argument ["panic"] for the Exception state
// which describes a panic within a Transition handler.
type ExceptionArgsPanic struct {
	CalledStates S
	StatesBefore S
	Transition   *Transition
	LastStep     *Step
	StackTrace   string
}

// ExceptionHandler provide a basic Exception state support, as should be
// embedded into handler structs in most of the cases. Because ExceptionState
// will be called after [Machine.HandlerDeadline], it should handle locks
// on its own (to not race with itself).
type ExceptionHandler struct{}

type recoveryData struct {
	err   any
	stack string
}

func captureStackTrace() string {
	buf := make([]byte, 4024)
	n := runtime.Stack(buf, false)
	stack := string(buf[:n])
	lines := strings.Split(stack, "\n")
	isPanic := strings.Contains(stack, "panic")
	slices.Reverse(lines)

	heads := []string{
		"AddErr", "AddErrState", "Remove", "Remove1", "Add", "Add1", "Set",
	}
	// TODO trim tails start at reflect.Value.Call({
	//  with asyncmachine 2 frames down

	// trim the head, remove junk
	stop := false
	for i, line := range lines {
		if isPanic && strings.HasPrefix(line, "panic(") {
			lines = lines[:i-1]
			break
		}

		for _, head := range heads {
			if strings.Contains("machine.(*Machine)."+line+"(", head) {
				lines = lines[:i-1]
				stop = true
				break
			}
		}
		if stop {
			break
		}
	}
	slices.Reverse(lines)
	join := strings.Join(lines, "\n")

	if filter := os.Getenv(EnvAmTraceFilter); filter != "" {
		join = strings.ReplaceAll(join, filter, "")
	}

	return join
}

// ExceptionState is a final entry handler for the Exception state.
// Args:
// - err error: The error that caused the Exception state.
// - panic *ExceptionArgsPanic: Optional details about the panic.
func (eh *ExceptionHandler) ExceptionState(e *Event) {
	// TODO handle ErrHandlerTimeout to a state, if set

	args := ParseArgs(e.Args)
	err := args.Err
	trace := args.ErrTrace
	mach := e.Machine()

	// err
	if err == nil {
		err = errors.New("missing error in args to ExceptionState")
	}
	if args.Panic == nil {
		errMsg := strings.TrimSpace(err.Error())

		// TODO more mutation info
		if mach.LogStackTrace && trace != "" {
			mach.log(LogChanges, "[error] %s\n%s", errMsg, trace)
		} else {
			mach.log(LogChanges, "[error] %s", errMsg)
		}

		return
	}

	// handler panic info
	mutType := args.Panic.Transition.Mutation.Type
	if mach.LogStackTrace && trace != "" {
		// stack trace
		mach.log(LogChanges, "[error:%s] %s (%s)\n%s", mutType,
			j(args.Panic.CalledStates), err, trace)
	} else {
		// no stack trace
		mach.log(LogChanges, "[error:%s] %s (%s)", mutType,
			j(args.Panic.CalledStates), err)
	}
}

// NewLastTxTracer returns a Tracer that logs the last transition.
func NewLastTxTracer() *LastTxTracer {
	return &LastTxTracer{}
}

// TODO add TTL, ctx
type LastTxTracer struct {
	*NoOpTracer
	lastTx atomic.Pointer[Transition]
}

func (t *LastTxTracer) TransitionEnd(transition *Transition) {
	t.lastTx.Store(transition)
}

// Load returns the last transition.
func (t *LastTxTracer) Load() *Transition {
	return t.lastTx.Load()
}

func (t *LastTxTracer) String() string {
	tx := t.lastTx.Load()
	if tx == nil {
		return ""
	}

	return tx.String()
}
