package machine

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
	// TODO: optimize, add index-based diffs
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
	return j(t.ActiveStates())
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

// ///// LOGGING, TRACING

// ///// ///// /////

// LoggerFn is a logging function for the machine.
type LoggerFn func(level LogLevel, msg string, args ...any)

// LogArgsMapperFn is a function that maps arguments to be logged. Useful for
// debugging. See NewArgsMapper.
type LogArgsMapperFn func(args A) map[string]string

type LogEntry struct {
	Level LogLevel
	Text  string
}

// LogLevel enum
type LogLevel int

const (
	// LogNothing means no logging, including external msgs.
	LogNothing LogLevel = iota
	// LogExternal will show ony external user msgs.
	LogExternal
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
	case LogNothing:
		fallthrough
	default:
		return "nothing"
	case LogExternal:
		return "external"
	case LogChanges:
		return "changes"
	case LogOps:
		return "ops"
	case LogDecisions:
		return "decisions"
	case LogEverything:
		return "everything"
	}
}

// SemLogger is a semantic logger for structured events. It's useful for graph
// info and configuring the text logger.
type SemLogger interface {
	// TODO implement empty methods
	// TODO add SetTag, RemoveTag, JoinTopic, LeaveTopic, custom graph
	//  links / edges

	// graph

	// AddPipeOut informs that [sourceState] has been piped out into [targetMach].
	// The name of the target state is unknown.
	AddPipeOut(addMut bool, sourceState, targetMach string)
	// AddPipeIn informs that [targetState] has been piped into this machine from
	// [sourceMach]. The name of the source state is unknown.
	AddPipeIn(addMut bool, targetState, sourceMach string)
	// RemovePipes removes all pipes for the passed machine ID.
	RemovePipes(machId string)

	// details

	// IsCan return true when the machine is logging Can* methods.
	IsCan() bool
	// EnableCan enables / disables logging of Can* methods.
	EnableCan(enable bool)
	// IsSteps return true when the machine is logging transition steps.
	IsSteps() bool
	// EnableSteps enables / disables logging of transition steps.
	EnableSteps(enable bool)
	// IsGraph returns true when the machine is logging graph structures.
	IsGraph() bool
	// EnableGraph enables / disables logging of graph structures.
	EnableGraph(enable bool)
	// EnableId enables or disables the logging of the machine's ID in log
	// messages.
	EnableId(val bool)
	// IsId returns true when the machine is logging the machine's ID in log
	// messages.
	IsId() bool
	// EnableQueued enables or disables the logging of queued mutations.
	EnableQueued(val bool)
	// IsQueued returns true when the machine is logging queued mutations.
	IsQueued() bool
	// EnableStateCtx enables or disables the logging of active state contexts.
	EnableStateCtx(val bool)
	// IsStateCtx returns true when the machine is logging active state contexts.
	IsStateCtx() bool
	// EnableWhen enables or disables the logging of "when" methods.
	EnableWhen(val bool)
	// IsWhen returns true when the machine is logging "when" methods.
	IsWhen() bool
	// EnableArgs enables or disables the logging known args.
	EnableArgs(val bool)
	// IsArgs returns true when the machine is logging known args.
	IsArgs() bool

	// logger

	// SetLogger sets a custom logger function.
	SetLogger(logger LoggerFn)
	// Logger returns the current custom logger function or nil.
	Logger() LoggerFn
	// SetLevel sets the log level of the machine.
	SetLevel(lvl LogLevel)
	// Level returns the log level of the machine.
	Level() LogLevel
	// SetEmpty creates an empty logger that does nothing and sets the log
	// level in one call. Useful when combined with am-dbg. Requires LogChanges
	// log level to produce any output.
	SetEmpty(lvl LogLevel)
	// SetSimple takes log.Printf and sets the log level in one
	// call. Useful for testing. Requires LogChanges log level to produce any
	// output.
	SetSimple(logf func(format string, args ...any), level LogLevel)
	// SetArgsMapper accepts a function which decides which mutation arguments
	// to log. See NewArgsMapper or create your own manually.
	SetArgsMapper(mapper LogArgsMapperFn)
	// ArgsMapper returns the current log args mapper function.
	ArgsMapper() LogArgsMapperFn
}

// SemConfig defines a config for SemLogger.
type SemConfig struct {
	// TODO
	Full     bool
	Steps    bool
	Graph    bool
	Can      bool
	Queued   bool
	Args     bool
	When     bool
	StateCtx bool
}

type semLogger struct {
	mach   *Machine
	steps  atomic.Bool
	graph  atomic.Bool
	queued atomic.Bool
	args   atomic.Bool
	can    atomic.Bool
}

// implement [SemLogger]
var _ SemLogger = &semLogger{}

func (s *semLogger) SetArgsMapper(mapper LogArgsMapperFn) {
	s.mach.logArgs.Store(&mapper)
}

func (s *semLogger) ArgsMapper() LogArgsMapperFn {
	fn := s.mach.logArgs.Load()
	if fn == nil {
		return nil
	}
	return *fn
}

func (s *semLogger) EnableId(val bool) {
	s.mach.logId.Store(val)
}

func (s *semLogger) IsId() bool {
	return s.mach.logId.Load()
}

func (s *semLogger) SetLogger(fn LoggerFn) {
	if fn == nil {
		s.mach.logger.Store(nil)

		return
	}
	s.mach.logger.Store(&fn)
}

func (s *semLogger) Logger() LoggerFn {
	if l := s.mach.logger.Load(); l != nil {
		return *l
	}

	return nil
}

func (s *semLogger) SetLevel(lvl LogLevel) {
	s.mach.logLevel.Store(&lvl)
}

func (s *semLogger) Level() LogLevel {
	return *s.mach.logLevel.Load()
}

func (s *semLogger) SetEmpty(lvl LogLevel) {
	var logger LoggerFn = func(_ LogLevel, msg string, args ...any) {
		// no-op
	}
	s.mach.logger.Store(&logger)
	s.mach.logLevel.Store(&lvl)
}

func (s *semLogger) SetSimple(
	logf func(format string, args ...any), level LogLevel,
) {
	if logf == nil {
		panic("logf cannot be nil")
	}

	var logger LoggerFn = func(_ LogLevel, msg string, args ...any) {
		logf(msg, args...)
	}
	s.mach.logger.Store(&logger)
	s.mach.logLevel.Store(&level)
}

func (s *semLogger) AddPipeOut(addMut bool, sourceState, targetMach string) {
	kind := "remove"
	if addMut {
		kind = "add"
	}
	s.mach.log(LogOps, "[pipe-out:%s] %s to %s", kind, sourceState,
		targetMach)
}

func (s *semLogger) AddPipeIn(addMut bool, targetState, sourceMach string) {
	kind := "remove"
	if addMut {
		kind = "add"
	}
	s.mach.log(LogOps, "[pipe-in:%s] %s from %s", kind, targetState,
		sourceMach)
}

func (s *semLogger) RemovePipes(machId string) {
	s.mach.log(LogOps, "[pipe:gc] %s", machId)
}

func (s *semLogger) IsSteps() bool {
	return s.steps.Load()
}

func (s *semLogger) EnableSteps(enable bool) {
	s.steps.Store(enable)
}

func (s *semLogger) IsCan() bool {
	return s.can.Load()
}

func (s *semLogger) EnableCan(enable bool) {
	s.can.Store(enable)
}

func (s *semLogger) IsGraph() bool {
	return s.graph.Load()
}

func (s *semLogger) EnableGraph(enable bool) {
	s.graph.Store(enable)
}

func (s *semLogger) IsQueued() bool {
	return s.queued.Load()
}

func (s *semLogger) EnableQueued(enable bool) {
	s.queued.Store(enable)
}

func (s *semLogger) IsArgs() bool {
	return s.args.Load()
}

func (s *semLogger) EnableArgs(enable bool) {
	s.args.Store(enable)
}

// TODO more data types

func (s *semLogger) EnableStateCtx(enable bool) {
	// TODO
}

func (s *semLogger) IsStateCtx() bool {
	return false
}

func (s *semLogger) IsWhen() bool {
	return false
}

func (s *semLogger) EnableWhen(enable bool) {
	// TODO
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

func MutationFormatArgs(matched map[string]string) string {
	if len(matched) == 0 {
		return ""
	}

	// sort by name
	var ret []string
	names := slices.Collect(maps.Keys(matched))
	slices.Sort(names)
	for _, k := range names {
		v := matched[k]
		ret = append(ret, k+"="+v)
	}

	return " (" + strings.Join(ret, " ") + ")"
}

// Tracer is an interface for logging machine transitions and events, used by
// Opts.Tracers and Machine.BindTracer.
type Tracer interface {
	TransitionInit(transition *Transition)
	TransitionStart(transition *Transition)
	TransitionEnd(transition *Transition)
	MutationQueued(machine Api, mutation *Mutation)
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
// TODO rename to TracerNoOp
type NoOpTracer struct{}

func (t *NoOpTracer) TransitionInit(transition *Transition)          {}
func (t *NoOpTracer) TransitionStart(transition *Transition)         {}
func (t *NoOpTracer) TransitionEnd(transition *Transition)           {}
func (t *NoOpTracer) MutationQueued(machine Api, mutation *Mutation) {}
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
	// Ctx is an optional context this event is constrained by.
	Ctx context.Context
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
	// optional ctx
	if e.Ctx != nil && e.Ctx.Err() != nil {
		return false
	}

	return e.TransitionId == tx.Id && !tx.IsCompleted.Load() &&
		tx.IsAccepted.Load()
}

// Export clones only the essential data of the Event. Useful for tracing vs GC.
func (e *Event) Export() *Event {
	id := e.MachineId
	if e.Machine() == nil {
		id = e.Machine().Id()
	}

	return &Event{
		MachineId:    id,
		Name:         e.Name,
		TransitionId: e.TransitionId,
		IsCheck:      e.IsCheck,
	}
}

// Clone clones the event struct, making it writable.
func (e *Event) Clone() *Event {
	e2 := e.Export()

	// non-exportable fields
	e2.Args = e.Args
	e2.Ctx = e.Ctx

	return e2
}

// SwapArgs clone the event and assign new args.
func (e *Event) SwapArgs(args A) *Event {
	e2 := e.Clone()
	e2.Args = args

	return e2
}

func (e *Event) String() string {
	mach := e.Machine()
	if mach == nil {
		return e.Mutation().String()
	}

	return e.Mutation().StringFromIndex(mach.StateNames())
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
	Ctx      context.Context
}

type WhenTimeBinding struct {
	Ch chan struct{}
	// map of matched to their index positions
	// TODO optimize indexes
	Index map[string]int
	// number of matches so far TODO len(Index) ?
	Matched int
	// number of total matches needed
	Total int // TODO len(Times) ?
	// optional Time to match for completed from Index
	Times     Time
	Completed StateIsActive
	Ctx       context.Context
}

type WhenArgsBinding struct {
	ch      chan struct{}
	handler string
	args    A
	ctx     context.Context
}

type whenQueueEndsBinding struct {
	ch  chan struct{}
	ctx context.Context
}

type whenQueueBinding struct {
	ch   chan struct{}
	tick Result
}

// TODO optimize with indexes
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

// ExceptionArgsPanic is an optional argument ["panic"] for the StateException
// state which describes a panic within a Transition handler.
type ExceptionArgsPanic struct {
	CalledStates S
	StatesBefore S
	Transition   *Transition
	LastStep     *Step
	StackTrace   string
}

// ExceptionHandler provide a basic StateException state support, as should be
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

// ExceptionState is a final entry handler for the StateException state.
// Args:
// - err error: The error that caused the StateException state.
// - panic *ExceptionArgsPanic: Optional details about the panic.
func (eh *ExceptionHandler) ExceptionState(e *Event) {
	// TODO handle ErrHandlerTimeout to ErrHandlerTimeoutState (if present)

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

func newClosedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
