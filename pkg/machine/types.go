package machine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
)

const (
	// EnvAmDebug enables a simple debugging mode (eg long timeouts).
	// "2" logs to stdout (where applicable)
	// "1" | "2" | "" (default)
	EnvAmDebug = "AM_DEBUG"
	// EnvAmTestRunner indicates a CI test runner.
	EnvAmTestRunner = "AM_TEST_RUNNER"
	// EnvAmLog sets the log level respected by [NewCommon].
	// "1" | "2" | "3" | "4" | "5" | "" (default)
	EnvAmLog = "AM_LOG"
	// EnvAmDetectEval detects evals directly in handlers (use in tests) and also
	// logs panicked funcs.
	// TODO rename EnvAmDiagnose, log names of mach.Go sources
	EnvAmDetectEval = "AM_DETECT_EVAL"
	// EnvAmTraceFilter will remove its contents from stack traces, shortening
	// them.
	EnvAmTraceFilter = "AM_TRACE_FILTER"
	// EnvAmTestDebug activates debugging in tests.
	EnvAmTestDebug = "AM_TEST_DEBUG"
	// EnvAmTestDbgAddr enables test loop integration (no parallel and dbg relay)
	EnvAmTestDbgAddr = "AM_TEST_DBG_ADDR"
	SuffixEnter      = "Enter"
	SuffixExit       = "Exit"
	SuffixState      = "State"
	SuffixEnd        = "End"
	PrefixErr        = "Err"
	PrefixWarn       = "Warn"
	// StateAny is a name of a meta-state used in catch-all global handlers.
	StateAny = "Any"
	// StateException is the name of the predefined Exception state.
	StateException = "Exception"
	// StateHeartbeat is the name of the predefined Heartbeat state.
	StateHeartbeat = "Heartbeat"
	// StateHealthcheck is the name of the predefined Healthcheck state.
	StateHealthcheck = "Healthcheck"
	// StateStart is the name of the predefined Start state.
	StateStart = "Start"
	// StateDisposing is the name of the predefined Disposing state.
	StateDisposing = "Disposing"
	// StateReady is the name of the predefined Ready state.
	StateReady = "Ready"
	// StateMachineRestored is the name of the predefined MachineRestored state.
	StateMachineRestored = "MachineRestored"
	// TagArgsReq marks a state which can only be added with relevant args.
	TagArgsReq = "ArgsReq"
)

// A (arguments) is a map of named arguments for a [Mutation].
type A map[string]any

// Clock is a map of state names to their tick values. It's like [Time] but
// indexed by string, instead of int. See [TimeIndex] for a more advanced data
// structure.
type Clock map[string]uint64

func (c Clock) Sum(states S) uint64 {
	var sum uint64
	for name, v := range c {
		if len(states) > 0 && !states.Has(name) {
			continue
		}

		sum += v
	}

	return sum
}

// TODO Clock.Time
// func (c Clock) Time(index, filter S) TimeIndex {...}

// HandlerFinal is a final transition handler func signature.
type HandlerFinal func(e *Event)

// HandlerNegotiation is a negotiation transition handler func signature.
type HandlerNegotiation func(e *Event) bool

// HandlerNegotiationPartial is like [HandlerNegotiation], but with support for
// partial acceptance.
type HandlerNegotiationPartial func(e *Event) S

// HandlerDispose is a machine disposal handler func signature.
type HandlerDispose func(id string, ctx context.Context)

// Opts struct is used to configure a new [Machine].
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
	DontLogId bool
	// Custom relations resolver. Default: *[DefaultRelationsResolver].
	Resolver RelationsResolver
	// Log level of the machine. Default: [LogNothing].
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
	QueueLimit uint16
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
	// TODO refac to Id with the new dbg telemetry protocol
	ID string `json:"id" yaml:"id" toml:"id"`
	// StateNames is an ordered list of state names.
	StateNames S `json:"state_names" yaml:"state_names" toml:"state_names"`
	// Time is the [Machine.Time] value.
	Time Time `json:"time" yaml:"time" toml:"time"`
	// QueueTick is a value of [Machine.QueueTick].
	QueueTick uint64 `json:"queue_tick" yaml:"queue_tick" toml:"queue_tick"`
	// MachineTick is a value of [Machine.MachineTick].
	// nolint:lll
	MachineTick uint32 `json:"machine_tick" yaml:"machine_tick" toml:"machine_tick"`
	// Tags is a list of short strings attached to the machine.
	Tags []string `json:"tags" yaml:"tags" toml:"tags"`
}

// Api is a subset of Machine for alternative implementations.
type Api interface {
	// ///// REMOTE

	// Mutations (remote)

	// Add1 is a shorthand method to add a single state with the passed args.
	// See Add().
	//
	// # Example
	//
	// 	mach.Add1("Foo", nil)
	// 	// (Foo:1)
	Add1(state string, args A) Result
	// Add activates a list of states in the machine, returning the result of the
	// transition (Executed, Queued, Canceled).
	// Like every mutation method, it will resolve relations and trigger handlers.
	//
	// # Example
	//
	// 	mach.Add(am.S{"Foo", "Bar"}, nil)
	// 	// (Foo:1 Bar:1)
	Add(states S, args A) Result
	// Remove1 is [Machine.Remove] for a single state.
	//
	// # Example
	//
	// 	mach.Remove1("Foo", nil)
	// 	// ()
	Remove1(state string, args A) Result
	// Remove deactivates a list of states in the machine, returning the result of
	// the transition (Executed, Queued, Canceled).
	// Like every mutation method, it will resolve relations and trigger handlers.
	//
	// # Example
	//
	// 	mach.Remove(am.S{"Foo"}, nil)
	// 	// ()
	Remove(states S, args A) Result
	// AddErr is a dedicated method to add the StateException state with the
	// passed error and optional arguments.
	// Like every mutation method, it will resolve relations and trigger handlers.
	// AddErr produces a stack trace of the error, if LogStackTrace is enabled.
	AddErr(err error, args A) Result
	// AddErrState adds a dedicated error state, along with the build in
	// StateException state. Like every mutation method, it will resolve relations
	// and trigger handlers. AddErrState produces a stack trace of the error, if
	// LogStackTrace is enabled.
	AddErrState(state string, err error, args A) Result
	// Toggle deactivates a list of states in case all are active, or activates
	// them otherwise. Returns the result of the transition (Executed, Queued,
	// Canceled).
	Toggle(states S, args A) Result
	// Toggle1 activates or deactivates a single state, depending on its current
	// state. Returns the result of the transition (Executed, Queued, Canceled).
	Toggle1(state string, args A) Result
	// Set deactivates a list of states in the machine, returning the result of
	// the transition (Executed, Canceled, Queued).
	// Like every mutation method, it will resolve relations and trigger handlers.
	//
	// # Example
	//
	// 	mach.Set(am.S{"Foo"}, nil)
	// 	// (Foo:1)
	Set(states S, args A) Result

	// Traced mutations (remote)

	// EvAdd1 is like Add1 but passed the source event as the 1st param, which
	// results in traceable transitions.
	EvAdd1(event *Event, state string, args A) Result
	// EvAdd is like Add, but passed the source event as the 1st param, which
	// results in traceable transitions.
	EvAdd(event *Event, states S, args A) Result
	// EvRemove1 is like Remove1, but passed the source event as the 1st param,
	// which results in traceable transitions.
	EvRemove1(event *Event, state string, args A) Result
	// EvRemove is like Remove but passed the source event as the 1st param, which
	// results in traceable transitions.
	EvRemove(event *Event, states S, args A) Result
	// EvAddErr is like AddErr, but passed the source event as the 1st param,
	// which results in traceable transitions.
	EvAddErr(event *Event, err error, args A) Result
	// EvAddErrState is like AddErrState, but passed the source event as the 1st
	// param, which results in traceable transitions.
	EvAddErrState(event *Event, state string, err error, args A) Result
	// EvToggle is a traced version of [Machine.Toggle].
	EvToggle(event *Event, states S, args A) Result
	// EvToggle1 is a traced version of [Machine.Toggle1].
	EvToggle1(event *Event, state string, args A) Result

	// Waiting (remote)

	// WhenArgs returns a channel that will be closed when the passed state
	// becomes active with all the passed args. ArgsBase are compared using
	// the native
	// '=='. It's meant to be used with async Multi states, to filter out
	// a specific call.
	//
	// ctx: optional context that will close the channel when handler loop ends.
	//
	// # Example
	//
	// 	<-mach.WhenArgs("Foo", am.A{"ID": 123}, nil)
	WhenArgs(state string, args A, ctx context.Context) <-chan struct{}

	// ///// LOCAL

	// Checking (local)

	// Err returns the last error.
	Err() error
	// IsErr checks if the machine has the StateException state currently active.
	IsErr() bool
	// Is checks if all the passed states are currently active.
	//
	// 	machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
	// 	machine.Add(S{"Foo"}, nil)
	// 	machine.Is(S{"Foo"}) // true
	// 	machine.Is(S{"Foo", "Bar"}) // false
	Is(states S) bool
	// Is1 is [Machine.Is] for a single state.
	Is1(state string) bool
	// Any is a group call to Is, returns true if any of the params return true
	// from Is.
	//
	// 	machine.StringAll() // ()[Foo:0 Bar:0 Baz:0]
	// 	machine.Add(S{"Foo"}, nil)
	// 	// is(Foo, Bar) or is(Bar)
	// 	machine.Any(S{"Foo", "Bar"}, S{"Bar"}) // false
	// 	// is(Foo) or is(Bar)
	// 	machine.Any(S{"Foo"}, S{"Bar"}) // true
	Any(states ...S) bool
	// Any1 is group call to [Machine.Is1], returns true if any of the params
	// return true from [Machine.Is1].
	Any1(state ...string) bool
	// Not checks if **none** of the passed states are currently active.
	//
	// 	machine.StringAll()
	// 	// -> ()[A:0 B:0 C:0 D:0]
	// 	machine.Add(S{"A", "B"}, nil)
	//
	// 	// not(A) and not(C)
	// 	machine.Not(S{"A", "C"})
	// 	// -> false
	//
	// 	// not(C) and not(D)
	// 	machine.Not(S{"C", "D"})
	// 	// -> true
	Not(states S) bool
	// Not1 is [Machine.Not] for a single state.
	Not1(state string) bool
	// IsTime checks if the machine has changed since the passed
	// time (list of ticks). Returns true if at least one state has changed. The
	// states param is optional and can be used to check only a subset of states.
	IsTime(time Time, states S) bool
	// WasTime checks if the passed time has happened (or happening right now).
	// Returns false if at least one state is too early. The
	// states param is optional and can be used to check only a subset of states.
	WasTime(time Time, states S) bool
	// IsClock checks if the machine has changed since the passed
	// clock. Returns true if at least one state has changed.
	IsClock(clock Clock) bool
	// WasClock checks if the passed time has happened (or happening right now).
	// Returns false if at least one state is too early.
	WasClock(clock Clock) bool
	// Has return true is passed states are registered in the machine. Useful for
	// checking if a machine implements a specific state set.
	Has(states S) bool
	// Has1 is shorthand for Has. It returns true if the passed state is
	// registered in the machine.
	Has1(state string) bool
	// CanAdd checks if [states] can be added and returns Executed or
	// [AT.CheckDone] if a dry run mutation passes. Useful for reducing failed
	// negotiations.
	CanAdd(states S, args A) Result
	// CanAdd1 is [Machine.CanAdd] for a single state.
	CanAdd1(state string, args A) Result
	// CanRemove checks if [states] can be removed and returns Executed or
	// [AT.CheckDone] if a dry run mutation passes. Useful for reducing failed
	// negotiations.
	CanRemove(states S, args A) Result
	// CanRemove1 is [Machine.CanRemove] for a single state.
	CanRemove1(state string, args A) Result
	// Transition returns the current transition, if any.
	Transition() *Transition
	// IsLocal returns true for *am.Machine and false for *arpc.NetworkMachine.
	IsLocal() bool
	// ErrInternal returns a channel which receives handler and eval timeout
	// errors, so they can be handled accordingly without passing through
	// mutations.
	// This method always returns a closed channel for [Api.IsLocal] == false.
	ErrInternal() <-chan error
	// TODO ActivatedAt

	// Waiting (local)

	// When returns a channel that will be closed when all the passed states
	// become active or the machine gets disposed.
	//
	// ctx: optional context that will close the channel early.
	//
	// # Example
	//
	// 	<-mach.When(am.S{"Foo", "Bar"}, nil)
	When(states S, ctx context.Context) <-chan struct{}
	// When1 is an alias to When() for a single state.
	// See When.
	//
	// # Example
	//
	// 	<-mach.When1("Foo", nil)
	When1(state string, ctx context.Context) <-chan struct{}
	// WhenNot returns a channel that will be closed when all the passed states
	// become inactive or the machine gets disposed.
	//
	// ctx: optional context that will close the channel early.
	//
	// # Example
	//
	// 	<-mach.WhenNot(am.S{"Foo"}, nil)
	WhenNot(states S, ctx context.Context) <-chan struct{}
	// WhenNot1 is an alias to WhenNot() for a single state.
	// See WhenNot.
	//
	// # Example
	//
	// 	<-mach.WhenNot1("Foo", nil)
	WhenNot1(state string, ctx context.Context) <-chan struct{}
	// WhenTime returns a channel that will be closed when all the passed states
	// have passed the specified time. The time is a logical clock of the state.
	// Machine time can be sourced from [Machine.Time](), or [Machine.Clock]().
	//
	// ctx: optional context that will close the channel early.
	//
	// # Example
	//
	// 	<-mach.WhenTime(am.S{"Foo", "Bar"}, am.Time{6, 10}, nil)
	WhenTime(states S, times Time, ctx context.Context) <-chan struct{}
	// WhenTime1 waits till ticks for a single state equal the given value (or
	// higher).
	//
	// ctx: optional context that will close the channel early.
	//
	// # Example
	//
	// 	<-mach.WhenTime1("Foo", 6, nil)
	WhenTime1(state string, tick uint64, ctx context.Context) <-chan struct{}
	// WhenTicks waits N ticks of a single state (relative to now). Uses WhenTime
	// underneath.
	//
	// ctx: optional context that will close the channel early.
	//
	// # Example
	//
	// 	<-mach.WhenTicks("Foo", 2, nil)
	WhenTicks(state string, ticks int, ctx context.Context) <-chan struct{}
	// WhenNextActive waits until the state becomes active, excluding the current
	// activity.
	//
	// ctx: optional context that will close the channel early.
	//
	// # Example
	//
	// 	<-mach.WhenNextActive("Foo", nil)
	WhenNextActive(state string, ctx context.Context) <-chan struct{}
	// WhenQuery returns a channel that will be closed when the passed
	// [clockCheck] function returns true. [clockCheck] should be a pure
	// function and non-blocking.
	//
	// ctx: optional context that will close the channel early.
	//
	// # Example
	//
	// 	<-mach.WhenQuery(func(c am.Clock) bool {
	// 		// Foo activated >5 times and Bar activated twice as much
	// 		return c["Foo"] >= 10 && c["Bar"] >= 2*c["Foo"]
	// 	}, nil)
	WhenQuery(query func(clock Clock) bool, ctx context.Context) <-chan struct{}
	// WhenErr returns a channel that will be closed when the machine is in the
	// StateException state.
	//
	// ctx: optional context that will close the channel early.
	//
	// # Example
	//
	// 	<-mach.WhenErr(nil)
	WhenErr(ctx context.Context) <-chan struct{}
	// WhenQueue waits until the passed queueTick gets processed.
	//
	// # Example
	//
	// 	<-mach.WhenQueue(mach.Add1("Foo", nil))
	WhenQueue(tick Result) <-chan struct{}
	// TODO WhenTimeSum

	// Getters (local)

	// StateNames returns a SHARED copy of all the state names.
	StateNames() S
	// ActiveStates returns a copy of the currently active states when states is
	// nil, optionally limiting the results to a subset of states.
	ActiveStates(states S) S
	// Tick return the current tick for a given state.
	Tick(state string) uint64
	// Clock returns current machine's clock, a state-keyed map of ticks. If
	// states are passed, only the ticks of the passed states are returned.
	Clock(states S) Clock
	// Time returns machine's time, a list of ticks per state. Returned value
	// includes the specified states, or all the states if nil.
	Time(states S) Time
	// QueueTick is the number of times the queue has processed a mutation. Starts
	// from [1], for easy comparison with [Result].
	QueueTick() uint64
	// MachineTick is the number of times the machine has been started. Each start
	// means an empty queue. Only set by [Machine.Import].
	MachineTick() uint32
	// QueueLen returns the number of mutations currently in the queue.
	QueueLen() uint16
	// NewStateCtx returns a new sub-context, bound to the current clock's tick of
	// the passed state.
	//
	// Context cancels when the state has been deactivated, or right away,
	// if it isn't currently active.
	//
	// State contexts are used to check state expirations and should be checked
	// often inside goroutines.
	//
	// [e]: optional [Event] to bind to this context.
	NewStateCtx(state string, e ...*Event) context.Context
	// Export exports the machine state as Serialized: ID, machine time, and
	// state names.
	Export() (*Serialized, Schema, error)
	// Schema returns a copy of machine's schema.
	Schema() Schema
	// Switch returns the first state from the passed list that is currently
	// active, making it handy for switch statements. Useful for state groups.
	//
	// 	switch mach.Switch(ss.GroupPlaying) {
	// 	case "Playing":
	// 	case "Paused":
	// 	case "Stopped":
	// 	}
	Switch(groups ...S) string
	// Groups returns the machine's state groups as indexes, along with the
	// group order. See [Machine.SetGroups].
	Groups() (map[string][]int, []string)
	// Index returns a list of state indexes in the machine's StateNames() list,
	// with -1s for missing ones.
	Index(states S) []int
	// Index1 returns the index of a state in the machine's StateNames() list, or
	// -1 when not found or machine has been disposed.
	Index1(state string) int

	// Misc (local)

	// Id returns the machine's id.
	Id() string
	// ParentId returns the ID of the parent machine (if any).
	ParentId() string
	// ParseStates parses a list of states, removing unknown ones and duplicates.
	// Use [Machine.Has] and [Machine.Has1] to check if a state is defined for the
	// machine.
	ParseStates(states S) S
	// Tags returns machine's tags, a list of unstructured strings without spaces.
	Tags() []string
	// SetTags updates the machine's tags with the provided slice of strings.
	SetTags(tags []string)
	// Context returns the machine's context. This context lives longer than
	// the parent context and allows for graceful shutdown. This is the context
	// which should be used to create submachines. See also
	// [Machine.DisposeTimeout].
	Context() context.Context
	// ContextParent returns the original context passed to the constructor.
	ContextParent() context.Context
	// String returns a one line representation of the currently active states,
	// with their clock values. Inactive states are omitted.
	// Eg: (Foo:1 Bar:3)
	String() string
	// StringAll returns a one line representation of all the states, with their
	// clock values. Inactive states are in square brackets.
	//
	// 	(Foo:1 Bar:3) [Baz:2]
	StringAll() string
	// Log logs an [extern] message unless LogNothing is set.
	// Optionally redirects to a custom logger from [SemLogger.SetLogger].
	Log(msg string, args ...any)
	// SemLogger returns the semantic logger of the machine
	SemLogger() SemLogger
	// Inspect returns a multi-line string representation of the machine (states,
	// relations, clocks).
	// states: param for ordered or partial results.
	Inspect(states S) string
	// HandlersBind binds a struct of handler methods to machine's states, based
	// on the naming convention, eg `FooState(e *Event)`. Negotiation handlers can
	// optionally return bool.
	HandlersBind(handlers any, opts ...BindOpts) (string, error)
	// HandlersBindMaps is like [Machine.HandlersBind] but accepts maps of
	// handlers to bypass reflecting structs. It may lead to higher performance.
	// Returns a binding ID.
	//
	// # TODO
	//
	// v0.20 add HandlerNegotiationPartial
	HandlersBindMaps(negotiations map[string]HandlerNegotiation,
		finals map[string]HandlerFinal, opts ...BindOpts) (string, error)
	// HandlersDetach detaches previously bound machine handlers.
	HandlersDetach(bindingId string) error
	// Handlers returns the IDs of bound handlers.
	Handlers() []string
	// StatesVerified returns true if the state names have been ordered
	// using VerifyStates.
	StatesVerified() bool
	// Tracers return a copy of currently attached tracers.
	Tracers() []Tracer
	// TracerDetach tries to remove a tracer from the machine. Returns an error in
	// case the tracer wasn't bound.
	TracerDetach(id string) error
	// TracerBind binds a Tracer to the machine. Tracers can cause StateException
	// in submachines, before any handlers are bound. Use the Err() getter to
	// examine such errors.
	TracerBind(tracer Tracer) (string, error)
	// AddBreakpoint adds a breakpoint for an outcome of mutation (added and
	// removed states) checked by mutation equality. Once such a mutation happens,
	// a log message will be printed out. We can set an IDE's breakpoint on this
	// line and see the mutation's sync stack trace. If [Machine.LogStackTrace]
	// is set, the stack trace will be printed out as well. Many breakpoints can
	// be added, but none removed.
	//
	// Breakpoints are useful to find the caller of a mutation, but don't work
	// with [Machine.Set].
	//
	// strict: strict skips already active / inactive (for strict of diff
	// equality).
	AddBreakpoint(added S, removed S, strict bool)
	// AddBreakpoint1 is a single state version of [Machine.AddBreakpoint].
	AddBreakpoint1(added string, removed string, strict bool)
	// Dispose disposes the machine and all its emitters. You can wait for the
	// completion of the disposal with `<-mach.WhenDisposed()`. It's advised to
	// use Dispose() func from pkg/helpers instead, which handles Disposed state
	// mixing.
	Dispose()
	// WhenDisposed returns a channel that will be closed when the machine is
	// disposed.
	WhenDisposed() <-chan struct{}
	// IsDisposed returns true if the machine has been disposed.
	IsDisposed() bool
	// OnDispose adds a function to be called after the machine gets disposed.
	// These functions will run synchronously just before WhenDisposed() channel
	// gets closed. Considering it's a low-level feature, it's advised to handle
	// disposal via dedicated states.
	OnDispose(fn HandlerDispose)

	// Deprecated, to be removed soon.

	// BindHandlers is deprecated, use [Api.HandlersBind].
	// Deprecated.
	BindHandlers(handlers any, opts ...BindOpts) (string, error)
	// DetachHandlers is deprecated, use [Api.HandlersDetach].
	// Deprecated.
	DetachHandlers(bindingId string) error
	// DetachTracer is deprecated, use [Api.TracerDetach].
	// Deprecated.
	DetachTracer(id string) error
	// BindTracer is deprecated, use [Api.TracerBind].
	// Deprecated.
	BindTracer(tracer Tracer) (string, error)

	// Debug

	// QueueDump() []string
}

type breakpoint struct {
	Added   S
	Removed S
	Strict  bool
}

// ///// ///// /////

// ///// SUBSCRIPTION

// ///// ///// /////

type (
	// IndexWhen is a map of (single) state names to a list of activation or
	// deactivation bindings
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

	// TODO debug
	Debug string
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

// WhenTimeSumBinding is a slice element of machine time to a binding chan
type WhenTimeSumBinding struct {
	Ch    chan struct{}
	MTime uint64
	Ctx   context.Context
}

type WhenArgsBinding struct {
	ch      chan struct{}
	handler string
	args    A
	ctx     context.Context
}

type whenQueueEndsBinding struct {
	ch chan struct{}
}

type whenQueueBinding struct {
	ch   chan struct{}
	tick Result
}

type whenQueryBinding struct {
	ch  chan struct{}
	ctx context.Context
	fn  func(clock Clock) bool
}

// TODO optimize with indexes
type StateIsActive map[string]bool

// ///// ///// /////

// ///// ENUMS

// ///// ///// /////

// Result enum is the result of a state [Transition]. [Queued] is a virtual
// value and everything >= Queued (2) represents a queue tick on which the
// mutation will be processed. It's useful for queued negotiations.
type Result uint64

const (
	// Executed means that the transition was executed immediately and not
	// canceled.
	Executed Result = 0
	// Canceled means that the transition was canceled, by either relations or a
	// negotiation handler.
	Canceled Result = 1
	// Queued means that the transition was queued for later execution. Everything
	// above 2 also means Queued. The following methods can be used to wait for
	// the results:
	// - Machine.WhenQueue
	// - Machine.When
	// - Machine.WhenNot
	// - Machine.WhenArgs
	// - Machine.WhenTime
	// - Machine.WhenTime1
	// - Machine.WhenTicks
	// - Machine.WhenQueueEnds
	// See [Machine.QueueTick].
	Queued Result = 2
)

var (
	// ErrStateUnknown indicates that the state is unknown.
	ErrStateUnknown = errors.New("state unknown")
	// ErrStateInactive indicates that a necessary state isn't active.
	ErrStateInactive = errors.New("state not active")
	// ErrCanceled indicates that a transition was canceled.
	ErrCanceled = errors.New("transition canceled")
	// ErrQueued indicates that a mutation was queued. Queuing is a feature, not
	// an error, but it may be considered as such in certain cases.
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
	// ErrNestedEval indicated an eval called in another eval or directly inside a
	// transition handler's body.
	ErrNestedEval = errors.New("eval nested in handler or eval")
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
	// TODO add MutationInternal for SemLogger data-only/event tx
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

func (m MutationType) StringShort() string {
	switch m {
	case MutationAdd:
		return " +"
	case MutationRemove:
		return "- "
	case MutationSet:
		return "= "
	case mutationEval:
		return "e "
	}
	return ""
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
	HandlerChange func(mach *Machine, before, after Time)
	// HandlersBind is a curried version of [Api.HandlersBind] for DI.
	HandlersBind func(mach *Machine, opts ...BindOpts) (string, error)
)

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

// CallSignature binds states to args and optionally to enum values.
type CallSignature struct {
	// states to mutate (required)
	States S

	// true if not an Add mutation (optional)
	IsRemove bool
	// call name (optional)
	Name string
	// call description (optional)
	Desc string
	// required args (optional)
	Needed []string
	// optional args (optional)
	Optional []string
	// value enums per arg name (optional)
	Values map[string][]string
	// JSON schemas per arg name (optional)
	Schemas map[string][]string
}

func (c *CallSignature) String() string {
	label := c.Name
	mut := "Add"
	if c.IsRemove {
		mut = "Remove"
	}
	if label == "" {
		label = mut + ": " + strings.Join(c.States, " ")
	}
	if len(c.Needed) > 0 {
		label += " [" + strings.Join(c.Needed, " ") + "]"
	}

	return label
}

func (c *CallSignature) Args() []string {
	return slices.Concat(c.Needed, c.Optional)
}

// NextMut represent a scheduled mutation.
type NextMut struct {
	Mach   Api
	States S
	Args   A
}
