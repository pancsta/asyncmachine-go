package machine

import (
	"context"
)

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
