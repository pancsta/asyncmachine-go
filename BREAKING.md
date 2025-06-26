# Breaking API changes

Only `pkg/machine` and `pkg/states` adhere to semver. Semver of other packages is not guaranteed at the moment.

## v0.12

- `Opts.ID` and `Opts.ParentID` are now `Opts.Id`, `Opts.ParentId`
- `RelationsResolver` had many renames
- `Mutation.StateWasCalled(string)` is now `Mutation.IsCalled(int)`
- `Mutation.CalledStates` has been removed
- `Machine.GetLogger() *Logger` is now `Machine.Logger() Logger`
- `Machine.GetLogLevel()` is now `Machine.LogLevel()`
- `Transition.IsAccepted` and `Transition.IsCompleted` are now `atomic.Bool`
- `Machine.DetachTracer` now returns `error`
- added `Api.HasHandlers()`
- `Machine.Index` is now `Machine.Index1`
- `Machine.IndexN` is now `Machine.Index`

## v0.11

- `am.Struct` is now `am.Schema`
- `Machine.GetStruct()` is now `Machine.Schema()`
- `am.StructMerge()` is now `am.SchemaMerge()`
- `Tracer.StructChange()` is now `Tracer.SchemaChange()`
- `Machine.WhenTicksEq()` is now `Machine.WhenTime1()`

## v0.10

- `FooBar()` handlers get executed later and more often
- `FooAny()`, `AnyFoo()` handlers have been removed
- `AnyAny()` is now `AnyEnter()`

## v0.9

- `Event.Machine` is now `Event.Machine()`
- `Machine.RegisterDisposalHandler(func())` is now `HandleDispose(func(id, ctx))`
- `Step.FromState` is now `Step.GetFromState()`
- `Step.ToState` is now `Step.GetToState()`
- `Step.Data` is now `Step.RelType`

## v0.8

- `Machine.ID` is now `Id()`
- `Machine.Tracers` is now `Tracers() Tracers`
- `Machine.LogID` is now `GetLogId() bool`
- `Machine.Switch(ss... string)` is now `Switch(states S)`
- `Machine.StatesVerified` is now `StatesVerified()`
- `Machine.ParentID` is now `ParentId()`
- `Transition.StatesBefore` is now `StatesBefore()`
- `Transition.TargetStates` is now `TargetStates()`
- `Tracer.TransitionInit` now returns an optional `Context`
- `Machine.Ctx` is now `Ctx()`

## v0.7

- `Machine.PrintExceptions` is now `Machine.LogStackTrace`
- `Machine.Resolver` is now `Machine.Resolver()`
- `Machine.StateNames` is now `Machine.StateNames()`
- `Machine.Transition` is now `Machine.Transition()`
- `Machine.Err` is now `Machine.Err()`
- `Machine.AddErrStr()` has been removed
- `Machine.AddErr(error)` is now `Machine.AddErr(error, Args)`
- `Machine.AddErrState(string, error)` is now `Machine.AddErrState(state, error, Args)`
- `Machine.WhenTicksEq()` now accepts `uint64`
- `Machine.IsClock()` and `Machine.Clock()` are now `Machine.Time()`
- `Machine.OnEvent()` has been removed
- `Machine.DuringTransition()` is now `Machine.Transition()`
- `Machine.SetTestLogger()` is now `Machine.SetLoggerSimple()`
- `Machine.HasStateChanged()` is now `Machine.IsClock()`
- `Machine.HasStateChangedSince()` is now `Machine.IsTime()`
- `Machine.Clocks()` is now `Machine.Clock()`
- `Machine.Export()` and `Machine.Import()` now use `am.Serialized`
- `Opts.DontPrintExceptions` is now`Opts.DontLogStackTrace`
- `Transition.ClocksBefore` is now `Transition.ClockBefore()`
- `Transition.ClocksAfter` is now `Transition.ClockAfter()`
- `Transition.TAfter` is now `Transition.TimeAfter`
- `Transition.IsCompleted` is now `Transition.IsCompleted()`
- `T` is now `Time`
- `Clocks` is now `Clock`
- `Event*` enum has been removed
- `SMerge` is now `SAdd`
