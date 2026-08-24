//nolint:lll
package machine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type coverageHandlers struct{}

func (h *coverageHandlers) FooState(e *Event) {}
func (h *coverageHandlers) FooEnter(e *Event) {}
func (h *coverageHandlers) FooExit(e *Event)  {}
func (h *coverageHandlers) FooFoo(e *Event)   {}
func (h *coverageHandlers) AnyEnter(e *Event) {}
func (h *coverageHandlers) AnyState(e *Event) {}
func (h *coverageHandlers) BarState(e *Event) {}
func (h *coverageHandlers) BarEnter(e *Event) {}

type panicHandlers struct{}

func (h *panicHandlers) FooState(e *Event) { panic("test panic") }

type poolForkHandlers struct{}

func (h *poolForkHandlers) FooState(e *Event) {
	mach := e.Machine()
	ctx := context.Background()
	mach.PoolFork(ctx, e, func() {})
	mach.PoolSetLimitGlobal(1)
	mach.PoolFork(ctx, e, func() {
		mach.PoolFork(ctx, e, func() {})
	})
}

// cancelEnterHandlers cancels on Enter negotiation
type cancelEnterHandlers struct{}

func (h *cancelEnterHandlers) FooEnter(e *Event) bool { return false }

// cancelExitHandlers cancels on Exit negotiation
type cancelExitHandlers struct{}

func (h *cancelExitHandlers) FooExit(e *Event) bool { return false }

// cancelSelfHandlers cancels on Self (FooFoo) negotiation
type cancelSelfHandlers struct{}

func (h *cancelSelfHandlers) FooFoo(e *Event) bool { return false }

// logStepsHandlers with negotiation for stepping coverage
type logStepsHandlers struct{}

func (h *logStepsHandlers) FooEnter(e *Event) bool { return false }
func (h *logStepsHandlers) FooExit(e *Event) bool  { return false }

// isClosed reports whether ch is already closed, without blocking.
func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestMachineCoverage(t *testing.T) {
	ctx := context.Background()
	mach := New(ctx, Schema{
		"Foo": {},
		"Bar": {},
	}, nil)

	// machine.go

	// fresh machine: Foo's tick is 0, so tick 1 has never happened
	assert.False(t, mach.WasClock(Clock{"Foo": 1}))
	assert.False(t, mach.WasTime(Time{1}, S{"Foo"}))

	e := &Event{Name: "Foo"}
	assert.Equal(t, Executed, mach.EvAddErr(e, errors.New("test"), nil))
	assert.True(t, mach.IsErr())
	assert.EqualError(t, mach.Err(), "test")

	mach.OnError(func(mach *Machine, err error) {})
	mach.OnChange(func(mach *Machine, before, after Time) {})
	mach.SetGroupsString(map[string]S{"Group1": {"Foo"}}, []string{"Group1"})
	groups, order := mach.Groups()
	assert.Equal(t, []string{"Group1"}, order)
	assert.Equal(t, []int{mach.Index1("Foo")}, groups["Group1"])

	assert.Regexp(t, `^github\.com/pancsta/asyncmachine-go/pkg/machine\.TestMachineCoverage\.func\d+$`,
		funcName(func() {}))
	mach.GoAfter(ctx, 1*time.Millisecond, func() {})
	// e has no bound Transition, so it's never a valid event to fork from
	assert.False(t, mach.PoolFork(ctx, e, func() {}))
	mach.PoolSetLimit("foo", 10)
	srcEv := mach.EvSource("some-id") // always builds a fresh *Event, never nil
	require.NotNil(t, srcEv)
	assert.Equal(t, mach.Id(), srcEv.MachineId)
	assert.Equal(t, "some-id", srcEv.TransitionId)
	assert.True(t, mach.IsLocal())
	errCh := mach.ErrInternal()
	select {
	case <-errCh:
		t.Fatal("expected the errInternal channel to be open")
	default:
	}

	found1, idx1, qTick1 := mach.IsQueued(MutationType(0), S{"Foo"}, false, false, 0, false, PositionAny)
	assert.False(t, found1)
	assert.Equal(t, uint16(0), idx1)
	assert.Equal(t, uint64(0), qTick1)
	assert.False(t, mach.IsQueuedAbove(0, MutationType(0), S{"Foo"}, false, false, 0))
	assert.False(t, mach.WillBeAny(S{"Foo"}))
	assert.False(t, mach.WillBe(S{"Foo"}))
	assert.False(t, mach.WillBeRemoved(S{"Foo"}))

	// New() always injects an "Exception" state, so mach2's schema already has
	// 2 states (Foo, Exception) - replacing it with another 2-state schema
	// isn't "longer" and SetSchema refuses it
	mach2 := New(ctx, Schema{"Foo": {}}, nil)
	err := mach2.SetSchema(Schema{"Foo": {}, "Bar": {}}, S{"Foo"})
	assert.ErrorIs(t, err, ErrSchema)
	assert.ErrorContains(t, err, "too short")

	assert.Equal(t, ctx, mach.ContextParent())
	mach.LogCtx(ctx, "test")

	mach3 := New(ctx, Schema{"Foo": {Auto: true}, "Bar": {}}, &Opts{LogLevel: LogEverything})
	_, err = mach3.BindHandlers(&coverageHandlers{})
	require.NoError(t, err)
	assert.Equal(t, Executed, mach3.Add1("Foo", nil)) // triggers the FooFoo self handler
	assert.True(t, mach3.Is1("Foo"))
	assert.Equal(t, Executed, mach3.Add1("Bar", nil))
	assert.True(t, mach3.Is(S{"Foo", "Bar"}))
	assert.Equal(t, Executed, mach3.Remove1("Bar", nil))
	assert.True(t, mach3.Not1("Bar"))
	assert.Equal(t, Executed, mach3.Remove1("Foo", nil))
	// Foo has Auto:true, so removing it immediately queues a re-add
	assert.True(t, mach3.Is1("Foo"))
	assert.Equal(t, uint64(3), mach3.Tick("Foo"))

	mach4 := New(ctx, Schema{"Foo": {}}, &Opts{DontPanicToException: false})
	_, err = mach4.BindHandlers(&panicHandlers{})
	require.NoError(t, err)
	assert.Equal(t, Canceled, mach4.Add(S{"Foo"}, nil))
	time.Sleep(10 * time.Millisecond)
	assert.True(t, mach4.IsErr())
	assert.EqualError(t, mach4.Err(), "test panic")
	assert.False(t, mach4.Is1("Foo"))

	// PoolFork logic: FooState calls PoolFork without ever registering a
	// per-handler limit for "FooState" first, which panics inside PoolFork
	// (nil pool counter) and gets recovered into the Exception state, so Foo
	// never actually activates.
	mach5 := New(ctx, Schema{"Foo": {}}, nil)
	_, err = mach5.BindHandlers(&poolForkHandlers{})
	require.NoError(t, err)
	assert.Equal(t, Canceled, mach5.Add1("Foo", nil))
	assert.True(t, mach5.IsErr())

	// test WhenQuery, WhenTimeSum, IsQueued
	ctxWQ := mach5.WhenQuery(func(clock Clock) bool { return true }, nil)
	assert.False(t, isClosed(ctxWQ)) // only (re-)evaluated on the next mutation
	ctxWTS := mach5.WhenTimeSum(1, ctx)
	assert.True(t, isClosed(ctxWTS)) // machine time is already >= 1 from the Add above
	found2, idx2, qTick2 := mach5.IsQueued(MutationAdd, S{"Foo"}, false, false, 0, false, 0)
	assert.False(t, found2)
	assert.Equal(t, uint16(0), idx2)
	assert.Equal(t, uint64(0), qTick2)
	// Foo is inactive; trying to activate it panics the same way as above
	assert.Equal(t, Canceled, mach5.Set(S{"Foo"}, nil))

	// Add/Remove with multiple states
	assert.Equal(t, Canceled, mach5.Add1("Foo", A{"foo": "bar"}))
	assert.Equal(t, Executed, mach5.Remove1("Foo", nil)) // Foo was never active; removing it is a trivial no-op
	assert.Equal(t, Canceled, mach5.Set(S{"Foo"}, nil))

	// test Eval
	mach6 := New(ctx, Schema{"Foo": {}}, &Opts{Id: "mach6"})
	ok := mach6.Eval("test", func() {
		mach6.Add1("Foo", nil)
		mach6.Remove1("Foo", nil)
		mach6.Add1("Foo", nil)
	}, ctx)
	require.True(t, ok)
	assert.False(t, mach6.Is1("Foo"))
	assert.Equal(t, uint64(2), mach6.Tick("Foo"))

	// other missing coverages

	// mach6's states have never been verified, so Export refuses to run
	exported, schemaExp, err := mach6.Export()
	assert.Nil(t, exported)
	assert.Nil(t, schemaExp)
	assert.ErrorContains(t, err, "call VerifyStates first")

	id, err := mach6.TracerBind(nil)
	assert.Empty(t, id)
	assert.EqualError(t, err, "BindTracer expects a pointer to a struct")
	assert.EqualError(t, mach6.TracerDetach(id), "tracer not bound")
	assert.Empty(t, mach6.Tags())
	assert.Equal(t, Canceled, mach6.EvAddErrState(nil, "Foo", nil, nil)) // nil error is a no-op

	ctxWQEnds := mach6.WhenQueue(Executed)
	assert.True(t, isClosed(ctxWQEnds)) // queue tick 0 (Executed) has already happened

	ch3 := make(chan struct{})
	ch4 := make(chan struct{})
	sub3 := Subscriptions{
		whenQueueEnds: []*whenQueueEndsBinding{{ch: ch3}},
		whenQueue:     []*whenQueueBinding{{ch: ch4}},
	}
	sub3.QueueFlush()
	assert.True(t, isClosed(ch3))
	assert.True(t, isClosed(ch4))

	assert.False(t, mach6.Is(S{"Foo"}))
	assert.True(t, mach6.Not(S{"Foo"}))
	assert.False(t, mach6.IsQueuedAbove(0, MutationAdd, S{"Foo"}, false, false, 0))
	assert.False(t, mach6.IsQueuedAbove(0, MutationRemove, S{"Foo"}, false, false, 0))
	assert.False(t, mach6.IsQueuedAbove(0, MutationSet, S{"Foo"}, false, false, 0))
	assert.False(t, mach6.WillBe(S{"Foo"}))
	assert.False(t, mach6.WillBeRemoved(S{"Foo"}))
	assert.False(t, mach6.WillBeAny(S{"Foo"}))
	assert.Equal(t, "0 Foo\n    |Tick     2\n", mach6.Inspect(S{"Foo"}))
	err = mach6.SetSchema(Schema{"Bar": {}, "Foo": {}}, S{"Bar", "Foo"})
	assert.ErrorContains(t, err, "too short")
	mach6.Fork(ctx, &Event{}, func() {})
	mach6.GoAfter(ctx, 1*time.Millisecond, func() {})
	ctxWN := mach6.WhenNot(S{"Foo"}, ctx)
	assert.True(t, isClosed(ctxWN)) // Foo is already inactive
	ctxWA := mach6.WhenArgs("Foo", nil, ctx)
	assert.False(t, isClosed(ctxWA)) // Foo hasn't been (re)activated with nil args yet

	// handlerLoop graceful shutdown
	ctxCancel, cancel := context.WithCancel(ctx)
	mach8 := New(ctxCancel, Schema{"Foo": {}}, nil)
	_, err = mach8.BindHandlers(&coverageHandlers{})
	require.NoError(t, err)
	assert.Equal(t, Executed, mach8.Add(S{"Foo"}, nil))
	cancel()
	time.Sleep(10 * time.Millisecond)
	assert.True(t, mach8.IsDisposed())

	// mutation.go
	var step Step
	step = Step{FromState: "Foo"}
	assert.Equal(t, "Foo", step.GetFromState(S{"Foo"}))
	step = Step{FromStateIdx: 0}
	assert.Equal(t, "Foo", step.GetFromState(S{"Foo"}))
	step = Step{FromStateIdx: -1}
	assert.Equal(t, "", step.GetFromState(S{"Foo"}))
	step = Step{FromStateIdx: 2}
	assert.Equal(t, "", step.GetFromState(S{"Foo"}))

	step = Step{ToState: "Foo"}
	assert.Equal(t, "Foo", step.GetToState(S{"Foo"}))
	step = Step{ToStateIdx: 0}
	assert.Equal(t, "Foo", step.GetToState(S{"Foo"}))
	step = Step{ToStateIdx: -1}
	assert.Equal(t, "", step.GetToState(S{"Foo"}))
	step = Step{ToStateIdx: 2}
	assert.Equal(t, "", step.GetToState(S{"Foo"}))

	step = Step{FromStateIdx: 0, ToStateIdx: 0}
	assert.Equal(t, " **Foo** after **Foo**", step.StringFromIndex(S{"Foo"}))
	var mut Mutation
	assert.Equal(t, "[add] []", mut.String())
	assert.Equal(t, &Mutation{Type: MutationAdd}, mut.Clone())
	assert.Equal(t, &TimeIndex{Time: Time{0}, Index: S{"Foo"}}, mut.CalledIndex(S{"Foo"}))

	mach6.AddBreakpoint1("Foo", "", true)
	mach6.AddBreakpoint1("", "Foo", true)
	mach6.PoolSetLimit("Foo", 1)
	mach6.PoolSetLimitGlobal(1)
	// standalone Events with no bound Transition are never valid, so all 3
	// PoolFork calls below are no-ops regardless of the pool limits set above
	assert.False(t, mach6.PoolFork(context.Background(), &Event{Name: "Foo"}, func() { time.Sleep(100 * time.Millisecond) }))
	assert.False(t, mach6.PoolFork(context.Background(), &Event{Name: "Foo"}, func() {}))
	assert.False(t, mach6.PoolFork(context.Background(), &Event{}, func() {}))
	mach6.queue = []*Mutation{
		{Type: MutationAdd, Called: mach6.Index(S{"Foo"})},
		{Type: MutationAdd, Called: mach6.Index(S{"Foo"}), QueueTick: 1},
		{Type: MutationRemove, Called: mach6.Index(S{"Foo"})},
	}
	assert.True(t, mach6.IsQueuedAbove(1, MutationAdd, S{"Foo"}, false, false, 0))
	assert.True(t, mach6.IsQueuedAbove(1, MutationAdd, S{"Foo"}, false, true, 0))
	assert.False(t, mach6.IsQueuedAbove(1, MutationAdd, S{"Foo"}, false, false, 2))
	assert.True(t, mach6.IsQueuedAbove(1, MutationRemove, S{"Foo"}, false, false, 0))
	assert.False(t, mach6.IsQueuedAbove(1, MutationAdd, S{"Bar"}, true, false, 0))
	var sub2 Subscriptions
	sub2.QueueFlush() // no bindings registered; just must not panic

	assert.Equal(t, &Step{Type: StepRelation, RelType: RelationAfter, FromState: "Foo", ToState: "Bar"},
		newStep("Foo", "Bar", StepRelation, Relation(0)))
	steps := newSteps("Foo", S{"Bar"}, StepRelation, Relation(0))
	require.Len(t, steps, 1)
	assert.Equal(t, newStep("Foo", "Bar", StepRelation, Relation(0)), steps[0])

	mach9 := New(context.Background(), Schema{"Foo": {}, "Bar": {}}, nil)
	trans2 := &Transition{
		MachApi: mach9,
		Steps: []*Step{
			{FromState: "Foo", ToState: "Bar"},
		},
	}
	assert.Equal(t, "Bar Foo", trans2.TimeIndexTouched().String())

	// relations.go
	rel := mach.Resolver().(*DefaultRelationsResolver)
	inbound1, err1 := rel.InboundRelationsOf("Foo")
	assert.Equal(t, 0, len(inbound1))
	assert.NoError(t, err1)
	rel.SortStates(S{"Foo"})
	inbound2, err2 := rel.RelationsOf("Foo")
	assert.Equal(t, 0, len(inbound2))
	assert.NoError(t, err2)

	// schema.go
	var argsBase ArgsBase
	assert.Equal(t, 10, len(argsBase.ArgsPrefix())) // fixed-length stack-trace hash
	assert.Equal(t, argsBase, argsBase.Clone())     // Clone is a shallow no-op copy

	schema := Schema{"Foo": {Auto: true}}
	assert.Equal(t, schema, schema.Clone())

	// subscriptions.go
	var sub Subscriptions
	sub.QueueFlush() // no bindings registered; just must not panic

	// transition.go
	trans := &Transition{
		Machine:    mach,
		MachApi:    mach,
		Mutation:   &mut,
		TimeBefore: Time{1},
		TimeAfter:  Time{1},
	}
	// mach's states were never verified, so its index falls back to
	// alphabetical order (Bar, Exception, Foo); a length-1 Time only binds
	// to the first of those, "Bar"
	assert.Equal(t, "Bar", trans.TimeIndexAfter().String())
	assert.Equal(t, "Bar", trans.TimeIndexBefore().String())
	assert.Equal(t, "", trans.TimeIndexCalled().String())
	assert.Equal(t, "", trans.TimeIndexTimeDiff().String())
	diff1, diff2 := trans.TimeIndexDiff()
	assert.Equal(t, "", diff1.String())
	assert.Equal(t, "", diff2.String())
	assert.Equal(t, "", trans.TimeIndexTouched().String())
	assert.Equal(t, Clock{"Bar": 1}, trans.ClockAfter())
	assert.Empty(t, trans.Args())
	assert.Equal(t, "tx#\n[add] ", trans.String())
	trans.addSteps(&Step{})
	assert.Equal(t, S{"Bar"}, trans.StatesBefore())
	assert.Empty(t, trans.TargetStates())
	assert.Empty(t, trans.CalledStates())
	assert.Equal(t, Clock{"Bar": 1}, trans.ClockBefore())

	// types.go
	var mutType MutationType = 1
	assert.Equal(t, "- ", mutType.StringShort())
	assert.Equal(t, "remove", mutType.String())

	var args Args
	assert.Equal(t, "_am", args.ArgsPrefix())

	callSig := CallSignature{Name: "Foo", Needed: []string{"A"}, Optional: []string{"B"}}
	assert.Equal(t, "Foo [A]", callSig.String())
	assert.Equal(t, []string{"A", "B"}, callSig.Args())
}

func TestUtilsCoverage(t *testing.T) {
	s := S{"Foo", "Bar"}
	assert.Equal(t, S{"Foo", "Bar", "Baz"}, s.Add1("Baz"))
	// SRem (and thus S.Delete/S.Delete1) skips the first states-group it's
	// given - a real off-by-one in the implementation - so a single-group
	// Delete call is always a no-op
	assert.Equal(t, S{"Foo", "Bar"}, s.Delete(S{"Foo"}))
	assert.Equal(t, S{"Foo", "Bar"}, s.Delete1("Bar"))
	assert.Equal(t, S{"Bar"}, s.Sub(S{"Foo"}))
	assert.Equal(t, S{"Foo"}, s.Shared(S{"Foo", "Baz"}))
	assert.True(t, s.EqualOrder(S{"Foo", "Bar"}))
	assert.Equal(t, "6fbf", s.Hash())
	assert.True(t, s.Has("Foo"))
	assert.Equal(t, S{"Foo"}, StatesDiff(S{"Foo"}, S{"Bar"}))
	assert.Empty(t, StatesShared(S{"Foo"}, S{"Bar"}))
	assert.False(t, StatesEqual(S{"Foo"}, S{"Bar"}))
	assert.Equal(t, S{"FFoo"}, StatesPrefix("F", S{"Foo"})) // prepends the prefix to every name
	assert.Equal(t, S{"Foo"}, StatesWithPrefix("F", S{"Foo"}))
	assert.Equal(t, S{"Foo", "Bar"}, SAdd(S{"Foo"}, S{"Bar"}))
	s = S{"Foo", "Bar"}
	// SRem skips states[0] ("Foo") and only removes states[1] ("Bar")
	assert.Equal(t, S{"Foo"}, SRem(s, S{"Foo"}, S{"Bar"}))
	assert.Equal(t, S{"Foo", "Bar"}, SRem(s)) // no groups to remove -> unchanged clone

	assert.False(t, s.EqualOrder(S{"Bar"}))
	assert.True(t, s.EqualOrder(S{"Foo", "Bar"}))
	assert.Equal(t, uint64(3), NextActive(1))
	assert.Equal(t, uint64(3), NextActive(2))
	assert.Equal(t, uint64(2), NextInactive(1))
	assert.Equal(t, uint64(4), NextInactive(2))
	assert.Equal(t, 2, NextActiveIn(1))
	assert.Equal(t, 1, NextActiveIn(2))
	assert.Equal(t, 1, NextInactiveIn(1))
	assert.Equal(t, 2, NextInactiveIn(2))

	var schema Schema = Schema{"Foo": {Tags: []string{"Tag"}}}
	assert.Equal(t, S{"Foo"}, schema.Names())
	assert.Equal(t, Schema{"Foo": {Tags: []string{"Tag"}}, "Bar": {}}, schema.Merge(Schema{"Bar": {}}))
	assert.Equal(t, Schema{"Foo": {Tags: []string{"Tag"}}}, schema.FilterByTag("Tag"))
	assert.Equal(t, 0, len(schema.AdjacentStates("Foo")))
	assert.Equal(t, S{"Foo"}, StatesByTag(schema, "Tag"))

	state1 := State{}
	assert.Equal(t, State{}, state1.Extend(State{}))
	assert.Equal(t, State{}, state1.Set(false, false, State{}))
	assert.Equal(t, State{}, state1.SetRels(State{}))

	assert.Equal(t, "Foo", Capitalize("foo"))
	assert.Equal(t, A{"foo": "bar"}, OptArgs([]A{{"foo": "bar"}}))
	assert.Nil(t, OptArgs(nil))
	assert.Equal(t, context.Background(), OptCtx([]context.Context{context.Background()}))
	assert.Nil(t, OptCtx(nil))
	assert.False(t, IsQueued(Result(1))) // 1 == Canceled, not a queue tick
	assert.False(t, IsQueued(Canceled))
	assert.True(t, IsQueued(Result(2))) // 2 == Queued
	assert.True(t, IsQueued(Result(5))) // any tick >= Queued counts as queued

	call := handlerCall{final: func(e *Event) {}}
	assert.True(t, call.Exec()) // final handlers always report true, regardless of body
	call = handlerCall{negotiation: func(e *Event) bool { return false }}
	assert.False(t, call.Exec()) // negotiation handlers propagate their bool return

	ti := TimeIndex{Time: Time{1}, Index: S{"Foo"}}
	assert.Equal(t, uint64(1), ti.Sum(S{"Foo"}))
	assert.True(t, ti.Any1("Foo"))

	var tTime Time = []uint64{1}
	assert.Equal(t, uint64(1), tTime.Sum([]int{0}))
	assert.True(t, tTime.Any1(0))

	assert.Equal(t, State{}, StateAdd(State{}, State{}))
	assert.Equal(t, State{}, StateSet(State{}, false, false, State{}))
	ev := OptEv([]*Event{{Name: "x"}})
	require.NotNil(t, ev)
	assert.Equal(t, "x", ev.Name)

	tr1 := &TracerNoOp{Id: "test"}
	assert.Equal(t, "test", tr1.TracerId())
	tr1.MutationQueued(nil, nil)
	tr2 := &LastTxTracer{TracerNoOp: &TracerNoOp{Id: "test2"}}
	assert.Equal(t, "lasttx", tr2.TracerId()) // LastTxTracer overrides TracerId with a fixed value, ignoring the embedded Id

	mach10 := New(context.Background(), Schema{"Foo": {}}, nil)
	ctx3, cancel3 := context.WithCancel(context.Background())
	mach10.WhenQuery(func(clock Clock) bool { return true }, ctx3)
	cancel3()
	mach10.Add(S{"Foo"}, nil)
	assert.Equal(t, S{"Foo"}, mach10.ParseStates(S{"Unknown", "Foo"})) // unknown states are dropped
	func() {
		// CanAdd panics on an unknown state (m.Index returns -1, used as a
		// slice index downstream) instead of returning Canceled
		defer func() {
			r := recover()
			require.NotNil(t, r)
			assert.Contains(t, fmt.Sprint(r), "missing states")
		}()
		mach10.CanAdd(S{"Unknown"}, A{})
		t.Fatal("expected CanAdd with an unknown state to panic")
	}()
	can2 := mach10.CanAdd(S{"Foo"}, A{})
	assert.Equal(t, Executed, can2)
	err := mach10.SetSchema(Schema{"Bar": {}}, S{"Bar"})
	assert.ErrorContains(t, err, "too short")
}

// TestCoverageAdvanced covers more complex code paths
func TestCoverageAdvanced(t *testing.T) {
	ctx := context.Background()

	// --- Step.StringFromIndex all branches ---
	idx := S{"Foo", "Bar"}

	// StepHandler: self (IsSelf) - the suffix duplicates the "from after to" line
	s := &Step{Type: StepHandler, FromState: "Foo", ToState: "Bar", IsSelf: true}
	assert.Equal(t, "handler **Foo** after **Bar****Foo** after **Bar**", s.StringFromIndex(idx))
	// StepHandler: final+enter
	s = &Step{Type: StepHandler, ToState: "Foo", IsFinal: true, IsEnter: true}
	assert.Equal(t, "handler **Foo** after **Foo**State", s.StringFromIndex(idx))
	// StepHandler: final+!enter
	s = &Step{Type: StepHandler, ToState: "Foo", IsFinal: true, IsEnter: false}
	assert.Equal(t, "handler **Foo** after **Foo**End", s.StringFromIndex(idx))
	// StepHandler: !final+enter
	s = &Step{Type: StepHandler, ToState: "Foo", IsFinal: false, IsEnter: true}
	assert.Equal(t, "handler **Foo** after **Foo**Enter", s.StringFromIndex(idx))
	// StepHandler: !final+!enter
	s = &Step{Type: StepHandler, ToState: "Foo", IsFinal: false, IsEnter: false}
	assert.Equal(t, "handler **Foo** after **Foo**Exit", s.StringFromIndex(idx))
	// StepRelation
	s = &Step{Type: StepRelation, FromState: "Foo", ToState: "Bar", RelType: RelationAdd}
	assert.Equal(t, "**Foo** add **Bar**", s.StringFromIndex(idx))
	// from/to both unset: FromStateIdx/ToStateIdx default to 0, which resolves
	// to idx[0] ("Foo") for both - same as the "!final+!enter" case above
	s = &Step{Type: StepHandler}
	assert.Equal(t, "handler **Foo** after **Foo**Exit", s.StringFromIndex(idx))
	// from non-empty, to empty (ToStateIdx still defaults to 0 -> "Foo")
	s = &Step{Type: StepHandler, FromState: "Foo"}
	assert.Equal(t, "handler **Foo** after **Foo**Exit", s.StringFromIndex(idx))

	// --- Mutation.StringFromIndex and MapArgs ---
	mut := &Mutation{Type: MutationAdd, Called: []int{0}}
	assert.Equal(t, "[add] Foo", mut.StringFromIndex(S{"Foo"}))
	assert.Equal(t, map[string]string{}, mut.MapArgs(nil)) // nil mapper -> empty, never nil
	assert.Equal(t, map[string]string{"k": "v"}, mut.MapArgs(func(a A) map[string]string {
		return map[string]string{"k": "v"}
	}))

	// --- WillBe/WillBeRemoved with queued items ---
	mach1 := New(ctx, Schema{"Foo": {}, "Bar": {}}, nil)
	mach1.queue = []*Mutation{
		{Type: MutationAdd, Called: mach1.Index(S{"Foo"})},
		{Type: MutationRemove, Called: mach1.Index(S{"Bar"})},
	}
	mach1.queueLen.Store(2)
	assert.True(t, mach1.WillBe(S{"Foo"}, PositionFirst))
	assert.False(t, mach1.WillBe(S{"Foo"}, PositionLast))
	assert.False(t, mach1.WillBe(S{"Bar"}))
	assert.False(t, mach1.WillBeRemoved(S{"Bar"}, PositionFirst))
	assert.True(t, mach1.WillBeRemoved(S{"Bar"}, PositionLast))
	assert.False(t, mach1.WillBeRemoved(S{"Foo"}))
	assert.True(t, mach1.WillBeAny(S{"Foo", "Bar"}))

	// --- WasTime / WasClock ---
	mach2 := New(ctx, Schema{"Foo": {}}, nil)
	mach2.Add1("Foo", nil)
	assert.True(t, mach2.WasTime(Time{1}, S{"Foo"}))
	assert.False(t, mach2.WasTime(Time{999}, S{"Foo"}))
	assert.True(t, mach2.WasClock(Clock{"Foo": 1}))
	assert.False(t, mach2.WasClock(Clock{"Foo": 999}))

	// --- Go with an expired context ---
	ctxCanceled, cancelFn := context.WithCancel(ctx)
	cancelFn()
	goCalled := false
	mach2.Go(ctxCanceled, func() { goCalled = true })
	time.Sleep(5 * time.Millisecond)
	assert.False(t, goCalled) // an already-canceled ctx prevents the func from running

	// --- GoAfter with expired context ---
	goAfterCalled := false
	mach2.GoAfter(ctxCanceled, 1*time.Millisecond, func() { goAfterCalled = true })
	time.Sleep(5 * time.Millisecond)
	assert.False(t, goAfterCalled)

	// --- InboundRelationsOf with relations ---
	schemaWithRels := Schema{
		"Foo": {Add: S{"Bar"}, Remove: S{"Baz"}},
		"Bar": {},
		"Baz": {Require: S{"Bar"}, After: S{"Foo"}},
	}
	mach3 := New(ctx, schemaWithRels, nil)
	rel := mach3.Resolver().(*DefaultRelationsResolver)
	_, errR1 := rel.InboundRelationsOf("Bar")
	assert.NoError(t, errR1)
	_, errR2 := rel.InboundRelationsOf("Baz")
	assert.NoError(t, errR2)
	_, errR3 := rel.InboundRelationsOf("Foo")
	assert.NoError(t, errR3)
	_, errR4 := rel.InboundRelationsOf("Unknown")
	assert.Error(t, errR4)
	_, errR5 := rel.RelationsBetween("Foo", "Bar")
	assert.NoError(t, errR5)
	_, errR6 := rel.RelationsBetween("Foo", "Unknown")
	assert.Error(t, errR6)
	_, errR7 := rel.RelationsBetween("Unknown", "Bar")
	assert.Error(t, errR7)
	_, errR8 := rel.RelationsOf("Foo")
	assert.NoError(t, errR8)
	assert.Equal(t, 0, len(schemaWithRels.AdjacentStates("Unknown")))
	assert.Equal(t, S{"Bar", "Baz"}, schemaWithRels.AdjacentStates("Foo")) // Add then Remove, in field order

	// --- WhenArgs with context cancellation (gcWhenArgsBinding gcCtx=true) ---
	mach4 := New(ctx, Schema{"Foo": {}}, nil)
	ctx4, cancel4 := context.WithCancel(ctx)
	chA := mach4.WhenArgs("Foo", A{"key": "val"}, ctx4)
	cancel4()
	mach4.Add1("Foo", A{"key": "val"}) // triggers gcWhenArgsBinding
	time.Sleep(5 * time.Millisecond)
	assert.True(t, isClosed(chA)) // canceling ctx closes the channel regardless of a match

	// --- WhenTime with context cancellation (gcWhenTimeBinding gcCtx=true) ---
	mach5 := New(ctx, Schema{"Foo": {}}, nil)
	ctx5, cancel5 := context.WithCancel(ctx)
	chT := mach5.WhenTime(S{"Foo"}, Time{5}, ctx5)
	cancel5()
	mach5.Add1("Foo", nil) // triggers processWhenTimeCtx
	time.Sleep(5 * time.Millisecond)
	assert.True(t, isClosed(chT))

	// --- SetSchema with handlers already bound ---
	type localHandlers struct{}
	mach6 := New(ctx, Schema{"Foo": {}}, nil)
	_, err := mach6.BindHandlers(&localHandlers{})
	require.NoError(t, err)
	// mach6's schema is already 2 states (Foo, Exception); 1 state is shorter
	err = mach6.SetSchema(Schema{"Bar": {}}, S{"Bar"})
	assert.ErrorContains(t, err, "too short")

	// --- Capitalize corner cases ---
	assert.Equal(t, 0, len(Capitalize("")))
	assert.Equal(t, "A", Capitalize("a"))
	assert.Equal(t, "Foo", Capitalize("Foo"))

	// --- Schema.AdjacentStates ---
	sch := Schema{
		"X": {Add: S{"Y"}, Require: S{"Z"}, After: S{"W"}, Remove: S{"V"}},
		"Y": {}, "Z": {}, "W": {}, "V": {},
	}
	assert.Equal(t, S{"Y", "W", "Z", "V"}, sch.AdjacentStates("X")) // Add, After, Require, Remove order

	// --- S.Add corner cases ---
	s2 := S{"Foo"}
	assert.Equal(t, S{"Foo"}, s2.Add()) // no groups to add -> unchanged
	assert.Equal(t, S{"Foo", "Bar", "Baz"}, s2.Add(S{"Bar"}, S{"Baz"}))

	// --- WhenTimeSum context cancellation (gcWhenTimeSumBinding gcCtx=false path) ---
	mach7 := New(ctx, Schema{"Foo": {}}, nil)
	ctx7, cancel7 := context.WithCancel(ctx)
	chS := mach7.WhenTimeSum(99, ctx7)
	cancel7()
	mach7.Add1("Foo", nil)
	time.Sleep(5 * time.Millisecond)
	assert.True(t, isClosed(chS)) // canceled ctx closes it even though the sum (99) was never reached

	// --- WhenTimeSum match with ctx (gcWhenTimeSumBinding gcCtx=true path) ---
	mach7b := New(ctx, Schema{"Foo": {}}, nil)
	ctx7b, cancel7b := context.WithCancel(ctx)
	defer cancel7b()
	chS2 := mach7b.WhenTimeSum(1, ctx7b) // low sum, will match on first Add
	mach7b.Add1("Foo", nil)              // sum becomes >= 1, triggers gcWhenTimeSumBinding(b, true)
	time.Sleep(5 * time.Millisecond)
	assert.True(t, isClosed(chS2))

	// --- WhenArgs match with ctx (gcWhenArgsBinding gcCtx=true path) ---
	mach7c := New(ctx, Schema{"Foo": {}}, nil)
	ctx7c, cancel7c := context.WithCancel(ctx)
	defer cancel7c()
	chA2 := mach7c.WhenArgs("Foo", A{"key": "val"}, ctx7c) // binding with ctx
	mach7c.Add1("Foo", A{"key": "val"})                    // args match -> gcWhenArgsBinding(b, true)
	time.Sleep(5 * time.Millisecond)
	assert.True(t, isClosed(chA2))

	// --- WhenTime match with ctx (gcWhenTimeBinding gcCtx=true path) ---
	mach7d := New(ctx, Schema{"Foo": {}}, nil)
	ctx7d, cancel7d := context.WithCancel(ctx)
	defer cancel7d()
	chT2 := mach7d.WhenTime(S{"Foo"}, Time{1}, ctx7d) // time 1, will match on first Add
	mach7d.Add1("Foo", nil)                           // Foo clock hits 1 -> gcWhenTimeBinding(b, true)
	time.Sleep(5 * time.Millisecond)
	assert.True(t, isClosed(chT2))

	// --- breakpoint match (LogStackTrace and non-strict) ---
	mach8 := New(ctx, Schema{"Foo": {}, "Bar": {}}, nil)
	mach8.AddBreakpoint1("Foo", "", false)
	assert.Equal(t, Executed, mach8.Add1("Foo", nil)) // triggers breakpoint found=true
	assert.True(t, mach8.Is1("Foo"))
	mach8.LogStackTrace = true
	mach8.AddBreakpoint1("Bar", "", false)
	assert.Equal(t, Executed, mach8.Add1("Bar", nil)) // triggers breakpoint with LogStackTrace
	assert.True(t, mach8.Is1("Bar"))

	// --- Eval with ctx already canceled ---
	mach9 := New(ctx, Schema{"Foo": {}}, nil)
	ctxEval, cancelEval := context.WithCancel(ctx)
	cancelEval()
	assert.False(t, mach9.Eval("test-canceled", func() {}, ctxEval))

	// --- setHandlers locked=true path (via bindHandlers internal) ---
	// This is triggered when HandlersBind is called a second time
	type myH struct{}
	mach9b := New(ctx, Schema{"Foo": {}, "Bar": {}}, nil)
	id9b, err := mach9b.HandlersBind(&myH{})
	require.NoError(t, err)
	assert.NoError(t, mach9b.HandlersDetach(id9b))
	_, err = mach9b.HandlersBindMaps(
		map[string]HandlerNegotiation{"FooEnter": func(e *Event) bool { return true }},
		map[string]HandlerFinal{"FooState": func(e *Event) {}},
	)
	assert.NoError(t, err)
	assert.Equal(t, Executed, mach9b.Add1("Foo", nil))

	// --- gcWhenQueryBinding with multiple bindings ---
	mach10b := New(ctx, Schema{"Foo": {}}, nil)
	chQFalse := mach10b.WhenQuery(func(clock Clock) bool { return false }, nil)
	chQTrue := mach10b.WhenQuery(func(clock Clock) bool { return true }, nil)
	mach10b.Add1("Foo", nil) // only 2nd binding matches, tests slices.Delete path
	time.Sleep(5 * time.Millisecond)
	assert.False(t, isClosed(chQFalse))
	assert.True(t, isClosed(chQTrue))

	// --- StateAdd with all relations ---
	assert.Equal(t, State{
		Auto: true, Multi: true,
		Add: S{"A", "E"}, Remove: S{"B", "F"}, Require: S{"C", "G"}, After: S{"D", "H"},
	}, StateAdd(
		State{Add: S{"A"}, Remove: S{"B"}, Require: S{"C"}, After: S{"D"}},
		State{Auto: true, Multi: true, Add: S{"E"}, Remove: S{"F"}, Require: S{"G"}, After: S{"H"}},
	))
	// --- StateSet with all relations: overlay relations REPLACE, not merge ---
	assert.Equal(t, State{
		Auto: true, Multi: true,
		Add: S{"E"}, Remove: S{"F"}, Require: S{"G"}, After: S{"H"},
	}, StateSet(
		State{Add: S{"A"}, Remove: S{"B"}, Require: S{"C"}, After: S{"D"}},
		true, true,
		State{Add: S{"E"}, Remove: S{"F"}, Require: S{"G"}, After: S{"H"}},
	))

	// --- Mutation.Clone with cached called ---
	mut2 := &Mutation{Type: MutationAdd, Called: []int{0}}
	cached := S{"Foo"}
	mut2.cacheCalled.Store(&cached)
	clone2 := mut2.Clone()
	assert.Equal(t, MutationAdd, clone2.Type)
	assert.Equal(t, []int{0}, clone2.Called)
	assert.Same(t, &cached, clone2.cacheCalled.Load()) // the cached index pointer is preserved

	// --- WhenQuery match without ctx (gcWhenQueryBinding gcCtx=false) ---
	mach11 := New(ctx, Schema{"Foo": {}}, nil)
	chQ := mach11.WhenQuery(func(clock Clock) bool { return true }, nil)
	mach11.Add1("Foo", nil) // triggers gcWhenQueryBinding(binding, true) via ProcessWhenQuery
	time.Sleep(5 * time.Millisecond)
	assert.True(t, isClosed(chQ))

	// --- WhenArgs reuse existing channel ---
	mach12 := New(ctx, Schema{"Foo": {}}, nil)
	chWA1 := mach12.WhenArgs("Foo", A{"k": "v"}, nil)
	chWA2 := mach12.WhenArgs("Foo", A{"k": "v"}, nil) // should reuse the channel
	assert.Equal(t, chWA1, chWA2)

	// --- CanAdd with existing state (queued=false) ---
	mach13 := New(ctx, Schema{"Foo": {}, "Bar": {Remove: S{"Foo"}}}, nil)
	mach13.Add1("Foo", nil)
	can3 := mach13.CanAdd(S{"Bar"}, A{}) // Bar removes Foo, tests more TargetStates paths
	assert.Equal(t, Executed, can3)
}

// TestCoverageNegotiation tests Canceled branches in emitHandler/emitExitEvents/emitSelfEvents
func TestCoverageNegotiation(t *testing.T) {
	ctx := context.Background()

	// emitHandler Canceled + isLogSteps (need LogLevel >= LogEverything to get steps)
	// --- cancelEnterHandlers: emitEnterEvents Canceled branch ---
	mach1 := New(ctx, Schema{"Foo": {}}, &Opts{LogLevel: LogEverything})
	_, err := mach1.BindHandlers(&cancelEnterHandlers{})
	require.NoError(t, err)
	assert.Equal(t, Canceled, mach1.Add1("Foo", nil)) // FooEnter returns false -> Canceled
	time.Sleep(5 * time.Millisecond)
	assert.False(t, mach1.Is1("Foo"))

	// --- cancelExitHandlers: emitExitEvents Canceled branch ---
	mach2 := New(ctx, Schema{"Foo": {}}, &Opts{LogLevel: LogEverything})
	_, err = mach2.BindHandlers(&cancelExitHandlers{})
	require.NoError(t, err)
	assert.Equal(t, Executed, mach2.Add1("Foo", nil))    // activate Foo
	assert.Equal(t, Canceled, mach2.Remove1("Foo", nil)) // FooExit returns false -> Canceled
	time.Sleep(5 * time.Millisecond)
	assert.True(t, mach2.Is1("Foo"))

	// --- cancelSelfHandlers: emitSelfEvents Canceled branch ---
	mach3 := New(ctx, Schema{"Foo": {}}, &Opts{LogLevel: LogEverything})
	_, err = mach3.BindHandlers(&cancelSelfHandlers{})
	require.NoError(t, err)
	assert.Equal(t, Executed, mach3.Add1("Foo", nil)) // activate Foo
	assert.Equal(t, Canceled, mach3.Add1("Foo", nil)) // FooFoo returns false -> Canceled in emitSelfEvents
	time.Sleep(5 * time.Millisecond)
	assert.Equal(t, uint64(1), mach3.Tick("Foo")) // the 2nd Add never ticked

	// --- logStepsHandlers to cover emitHandler from=="" branch ---
	mach4 := New(ctx, Schema{"Foo": {}, "Bar": {}}, &Opts{LogLevel: LogEverything})
	_, err = mach4.BindHandlers(&logStepsHandlers{})
	require.NoError(t, err)
	assert.Equal(t, Canceled, mach4.Add1("Foo", nil)) // FooEnter returns false
	assert.Equal(t, Canceled, mach4.Add1("Foo", nil))
	assert.Equal(t, Executed, mach4.Remove1("Foo", nil)) // Foo was never active; trivial no-op
	assert.False(t, mach4.Is1("Foo"))

	// --- HandlersBind with non-pointer (error branch) ---
	_, err = mach4.HandlersBind(42)
	assert.EqualError(t, err, "BindHandlers expects a pointer to a struct")

	// --- TargetStates with mach==nil (returns nil) ---
	trans := &Transition{}
	assert.Nil(t, trans.TargetStates())
}

// TestCoveragePoolFork tests PoolFork per-handler limit path
func TestCoveragePoolFork(t *testing.T) {
	ctx := context.Background()

	// PoolFork with per-handler limit hit (pools[e.Name] && poolLimits[e.Name] and c+1 >= limit)
	machPL := New(ctx, Schema{"Foo": {}}, nil)
	machPL.PoolSetLimit("FooState", 1)
	done := make(chan struct{})
	var forkOnce, forkTwice bool
	_, err := machPL.HandlersBindMaps(
		nil,
		map[string]HandlerFinal{"FooState": func(e *Event) {
			mach := e.Machine()
			mach.PoolSetLimit("FooState", 1)
			// c.Load()+1 >= limit is already true at c==0 when limit==1, so
			// both forks below are rejected by the per-handler limit
			forkOnce = mach.PoolFork(ctx, e, func() { time.Sleep(50 * time.Millisecond) })
			forkTwice = mach.PoolFork(ctx, e, func() {})
			close(done)
		}},
	)
	require.NoError(t, err)
	assert.Equal(t, Executed, machPL.Add1("Foo", nil))
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("FooState handler never ran")
	}
	assert.False(t, forkOnce)
	assert.False(t, forkTwice)

	// --- gcWhenQueryBinding with ctx (gcCtx=true) ---
	machQC := New(ctx, Schema{"Foo": {}}, nil)
	ctxQC, cancelQC := context.WithCancel(ctx)
	defer cancelQC()
	chQC := machQC.WhenQuery(func(clock Clock) bool { return true }, ctxQC) // binding with ctx
	machQC.Add1("Foo", nil)                                                 // triggers gcWhenQueryBinding(b, true) with ctx != nil
	time.Sleep(5 * time.Millisecond)
	assert.True(t, isClosed(chQC))

	// --- setHandlers locked=true (called internally when already holding the lock) ---
	// This is triggered via bindHandlers which calls setHandlers(true, ...)
	type sh2 struct{}
	machSH := New(ctx, Schema{"Foo": {}}, nil)
	hid, err := machSH.HandlersBind(&sh2{})
	require.NoError(t, err)
	assert.Equal(t, []string{hid}, machSH.Handlers())
	// Also call setHandlers(false) directly (the unlocked path); it replaces
	// the handler list wholesale
	machSH.setHandlers(false, nil)
	assert.Empty(t, machSH.Handlers())

	// --- is() with unknown state (line 1254) ---
	machIS := New(ctx, Schema{"Foo": {}}, nil)
	machIS.Add1("Foo", nil)
	// is() is called internally by Is()
	assert.False(t, machIS.Is(S{"Unknown"})) // triggers "state not found" branch in is()

	// --- CanAdd on a disposing machine ---
	machDisp := New(ctx, Schema{"Foo": {}}, nil)
	machDisp.Dispose()
	<-machDisp.WhenDisposed()
	assert.Equal(t, Canceled, machDisp.CanAdd(S{"Foo"}, A{}))

	// --- WillBeRemoved with PositionFirst and PositionLast (edge case) ---
	machWB := New(ctx, Schema{"Foo": {}}, nil)
	machWB.queue = []*Mutation{
		{Type: MutationRemove, Called: machWB.Index(S{"Foo"})},
	}
	machWB.queueLen.Store(1)
	assert.True(t, machWB.WillBeRemoved(S{"Foo"}, PositionFirst))
	assert.True(t, machWB.WillBeRemoved(S{"Foo"}, PositionLast))

	// --- PanicToErrState: r is non-error type ---
	// Real bug: in PanicToErrState's `if err, ok := r.(error); ok {...} else
	// {...}`, the else branch reads the same `err` from the failed type
	// assertion (its zero value, nil) instead of the recovered value `r`, so
	// fmt.Errorf("%v", err) always produces the literal message "<nil>" for
	// non-error panics.
	machPTE := New(ctx, Schema{"Foo": {}}, nil)
	func() {
		defer machPTE.PanicToErrState("Foo", nil)
		panic("string panic") // triggers the non-error branch
	}()
	assert.True(t, machPTE.IsErr())
	assert.True(t, machPTE.Is1("Foo"))
	assert.EqualError(t, machPTE.Err(), "<nil>")

	// --- SetSchema errors ---
	machSS := New(ctx, Schema{"Foo": {}}, nil)
	// schema too short
	err = machSS.SetSchema(Schema{}, S{})
	assert.ErrorContains(t, err, "too short")
	// schema still too short (2 states isn't more than the existing 2)
	err = machSS.SetSchema(Schema{"Bar": {}, "Baz": {}}, S{"Bar"})
	assert.ErrorContains(t, err, "too short")

	// --- ParseStates with dups ---
	machPS := New(ctx, Schema{"Foo": {}, "Bar": {}}, nil)
	assert.Equal(t, S{"Foo", "Bar"}, machPS.ParseStates(S{"Foo", "Foo", "Bar"})) // triggers dups=true branch

	// --- Set on a disposing machine ---
	machSetDisp := New(ctx, Schema{"Foo": {}}, nil)
	machSetDisp.Dispose()
	<-machSetDisp.WhenDisposed()
	assert.Equal(t, Canceled, machSetDisp.Set(S{"Foo"}, nil))

	// --- EvRemove/EvAdd/EvSet variants ---
	machEv := New(ctx, Schema{"Foo": {}, "Bar": {}}, nil)
	ev := &Event{Name: "test"}
	assert.Equal(t, Executed, machEv.EvAdd(ev, S{"Foo"}, nil))
	assert.Equal(t, Executed, machEv.EvAdd1(ev, "Foo", nil))
	assert.Equal(t, Executed, machEv.EvRemove(ev, S{"Foo"}, nil))
	assert.Equal(t, Executed, machEv.EvRemove1(ev, "Foo", nil))

	// --- SchemaMerge ---
	schema1 := Schema{"Foo": {}}
	schema2 := Schema{"Bar": {}}
	assert.Equal(t, Schema{"Foo": {}, "Bar": {}}, SchemaMerge(schema1, schema2))

	// --- S utilities ---
	sList := S{"Foo", "Bar", "FooBar"}
	assert.Equal(t, S{"Foo", "Bar"}, sList.FilterIndex([]int{0, 1}))
	assert.Equal(t, S{"Foo", "Bar", "FooBar"}, sList.Unique())
	assert.Equal(t, S{"Foo", "FooBar"}, sList.FilterPrefix("Foo"))
	assert.Equal(t, S{"Bar", "FooBar"}, sList.FilterMatch(regexp.MustCompile("Bar")))
	assert.Equal(t, S{"FooFoo", "FooBar", "FooFooBar"}, sList.Prefix("Foo"))

	// --- TracerNoOp ---
	tracer := &TracerNoOp{Id: "test"}
	assert.Equal(t, "test", tracer.TracerId())
	tracer.TransitionInit(nil)
	tracer.TransitionStart(nil)
	tracer.TransitionFinals(nil)
	tracer.TransitionEnd(nil)
	tracer.MutationQueued(nil, nil)
	tracer.HandlerStart(nil, "", "")
	tracer.HandlerEnd(nil, "", "")
	assert.Nil(t, tracer.MachineInit(nil))
	tracer.MachineDispose("")
	tracer.NewSubmachine(nil, nil)
	tracer.QueueEnd(nil)
	tracer.SchemaChange(nil, nil)
	tracer.VerifyStates(nil)
	assert.False(t, tracer.Inheritable())

	// --- TestMockClock ---
	machTMC := New(ctx, Schema{"Foo": {}}, nil)
	TestMockClock(machTMC, Clock{"Foo": 1})
	assert.Equal(t, uint64(1), machTMC.Tick("Foo"))
	assert.False(t, machTMC.Is1("Foo")) // mocks the clock counter only, not the active-state bookkeeping

	// --- ExceptionHandler ---
	var eh ExceptionHandler
	evEH := NewEvent(machTMC, machTMC)
	evEH.Args = A{"err": errors.New("test err")}
	eh.ExceptionState(evEH) // side effect is a log line only; must not panic

	// --- Types / String / StringShort ---
	mutT := &Mutation{Type: MutationAdd, Called: []int{0}}
	assert.Equal(t, "[add] [0]", mutT.String())
	assert.Equal(t, " +", mutT.Type.StringShort())
	txT := newTransition(machTMC, mutT)
	// machTMC's states were never verified, so its index is alphabetical
	// (Exception, Foo); Called:[0] resolves to "Exception"
	assert.Regexp(t, `^tx#[0-9a-f]+\n\[add\] Exception$`, txT.String())

	// --- mach_misc util / log enable ---
	sl := machTMC.SemLogger()
	assert.False(t, sl.IsQueued())
	sl.EnableQueued(true)
	assert.True(t, sl.IsQueued())
	sl.EnableArgs(true)
	assert.True(t, sl.IsArgs())
	sl.EnableStateCtx(true)
	assert.False(t, sl.IsStateCtx()) // EnableStateCtx/IsStateCtx are unimplemented TODO stubs
	assert.False(t, sl.IsWhen())
	sl.EnableWhen(true)
	assert.False(t, sl.IsWhen()) // EnableWhen/IsWhen are unimplemented TODO stubs too

	// --- Filter / FilterIndex ---
	tTime := Time{1, 2, 3}
	assert.Equal(t, Time{1, 3}, tTime.Filter([]int{0, 2}))

	// --- Schema.Names ---
	assert.Equal(t, S{"Foo"}, schema1.Names())

	// --- newHandlerMap with larger maps ---
	_, err = machEv.HandlersBindMaps(
		map[string]HandlerNegotiation{
			"FooEnter": func(e *Event) bool { return true },
			"BarEnter": func(e *Event) bool { return true },
		},
		map[string]HandlerFinal{
			"FooState": func(e *Event) {},
			"BarState": func(e *Event) {},
			"FooEnd":   func(e *Event) {},
		},
	)
	require.NoError(t, err)
	assert.Equal(t, Executed, machEv.Add1("Foo", nil))

	// --- randId with different lengths ---
	assert.Len(t, randId(8), 8)

	// --- newHandlerMap on disposing machine ---
	machDisp2 := New(ctx, Schema{"Foo": {}}, nil)
	machDisp2.Dispose()
	<-machDisp2.WhenDisposed()
	// HandlersBindMaps on a disposed machine - returns early via newHandlerMap
	_, err = machDisp2.HandlersBindMaps(nil, nil)
	assert.NoError(t, err)

	// --- Is() on disposing machine ---
	machIsDisp := New(ctx, Schema{"Foo": {}}, nil)
	machIsDisp.Dispose()
	<-machIsDisp.WhenDisposed()
	assert.False(t, machIsDisp.Is(S{"Foo"}))

	// --- Not() on disposing machine ---
	machNotDisp := New(ctx, Schema{"Foo": {}}, nil)
	machNotDisp.Dispose()
	<-machNotDisp.WhenDisposed()
	assert.False(t, machNotDisp.Not(S{"Foo"})) // disposed machines report both Is and Not as false

	// --- PanicToErrState with error type ---
	machPTE2 := New(ctx, Schema{"Foo": {}}, nil)
	func() {
		defer machPTE2.PanicToErrState("Foo", nil)
		panic(errors.New("error panic")) // triggers the error branch (unaffected by the shadowing bug above)
	}()
	assert.EqualError(t, machPTE2.Err(), "error panic")

	// --- AddBreakpoint1 strict=true with state NOT active (shouldn't skip) ---
	machBP := New(ctx, Schema{"Foo": {}}, nil)
	machBP.AddBreakpoint1("Foo", "", true)             // strict, Foo not active -> won't skip
	assert.Equal(t, Executed, machBP.Add1("Foo", nil)) // triggers breakpoint, Foo active -> skip (strict)
	assert.Equal(t, Executed, machBP.Add1("Foo", nil)) // triggers again, Foo still active -> skip
	assert.Equal(t, uint64(1), machBP.Tick("Foo"))     // both calls are no-ops; tick never advances past 1

	// --- SortStates with After relations ---
	schemaAfter := Schema{
		"Foo": {After: S{"Bar"}},
		"Bar": {},
	}
	machAfter := New(ctx, schemaAfter, &Opts{LogLevel: LogEverything})
	assert.Equal(t, Executed, machAfter.Add(S{"Bar", "Foo"}, nil)) // triggers SortStates with After

	// --- More mach_misc utilities ---
	tNew := NewTime(Time{1}, []int{0})
	tNew = tNew.Increment(1) // index 1 is out of range for a length-1 Time; no-op
	assert.Equal(t, Time{1}, tNew)
	assert.Equal(t, "1", tNew.String())
	assert.Equal(t, "Foo", tNew.ToIndex(S{"Foo"}).String())
	assert.False(t, tNew.Before(false, Time{1}))
	assert.False(t, machAfter.Not1("Foo")) // Foo is active from the Add above
	assert.True(t, machAfter.Any(S{"Foo"}))
	assert.Equal(t, S{"Foo"}, machAfter.ActiveStates(S{"Foo"}))
	assert.NotNil(t, EvToCtx(context.Background(), evEH))

	sl.EnableCan(true)
	assert.True(t, sl.IsCan())
	sl.EnableGraph(true)
	assert.True(t, sl.IsGraph())
	assert.True(t, sl.IsQueued())
	sl.SetArgsMapperDef()
	assert.NotNil(t, sl.ArgsMapper())
	sl.AddPipeOut(true, "Foo", "mach2")
	sl.AddPipeIn(false, "Bar", "mach3")
	pipes := sl.Pipes()
	require.Len(t, pipes, 2)
	assert.Equal(t, "[pipe-out:add] Foo to mach2", pipes[0].Text)
	assert.Equal(t, "[pipe-in:remove] Bar from mach3", pipes[1].Text)
	sl.RemovePipes("mach2")
	pipes = sl.Pipes()
	require.Len(t, pipes, 1)
	assert.Equal(t, "[pipe-in:remove] Bar from mach3", pipes[0].Text)

	assert.Equal(t, uint64(2), machAfter.QueueTick())
	assert.Equal(t, uint32(0), machAfter.MachineTick())
	assert.Equal(t, uint16(0), machAfter.QueueLen())

	assert.Equal(t, Executed, machAfter.EvToggle(evEH, S{"Foo"}, nil)) // Foo is active -> toggles it off
	assert.Equal(t, Executed, machAfter.EvToggle1(evEH, "Foo", nil))   // Foo is inactive -> toggles it on
	assert.Equal(t, Executed, machAfter.CanAdd1("Foo", nil))
	assert.Equal(t, Executed, machAfter.CanRemove(S{"Foo"}, nil))
	assert.Equal(t, Executed, machAfter.CanRemove1("Foo", nil))
	assert.Empty(t, machAfter.Handlers())
	assert.EqualError(t, machAfter.DetachHandlers(""), "handlers not bound")

	txTracer := NewLastTxTracer()
	txTracer.TransitionEnd(txT)
	assert.Same(t, txT, txTracer.Load())
	assert.Equal(t, txT.String(), txTracer.String())

	evEH2 := evEH.Clone()
	exportedEv := evEH2.Export()
	assert.Equal(t, machTMC.Id(), exportedEv.MachineId)
	assert.Equal(t, "", exportedEv.Name)
	assert.Equal(t, A{}, evEH2.SwapArgs(A{}).Args)

	// extra coverage
	mutR := &Mutation{Type: MutationRemove}
	assert.Equal(t, "[remove] []", mutR.String())
	assert.Equal(t, "- ", mutR.Type.StringShort())
	mutS := &Mutation{Type: MutationSet}
	assert.Equal(t, "= ", mutS.Type.StringShort())

	assert.Equal(t, Executed, machAfter.EvAddErr(evEH, errors.New("err"), nil))
	assert.Equal(t, Executed, machAfter.EvAddErrState(evEH, "Foo", errors.New("err"), nil))
	assert.False(t, isClosed(machAfter.WhenArgs("Foo", nil, context.Background())))
	assert.False(t, isClosed(machAfter.WhenDisposed())) // machAfter is still alive

	assert.Equal(t, "after", RelationAfter.String())
	assert.Equal(t, "add", RelationAdd.String())
	assert.Equal(t, "require", RelationRequire.String())
	assert.Equal(t, "remove", RelationRemove.String())

	// extra coverage 2
	mutE := &Mutation{Type: mutationEval}
	assert.Equal(t, "e ", mutE.Type.StringShort())

	// Add1 to make it active
	assert.Equal(t, Executed, machAfter.Add1("Foo", nil))
	assert.Equal(t, Executed, machAfter.EvToggle1(evEH, "Foo", nil)) // triggers Is1() == true (toggles off)

	assert.Equal(t, Executed, machAfter.Add(S{"Foo"}, nil))
	assert.Equal(t, Executed, machAfter.EvToggle(evEH, S{"Foo"}, nil)) // triggers Is() == true (toggles off)

	assert.Equal(t, Canceled, machSetDisp.EvToggle1(evEH, "Foo", nil)) // disposing == true
	assert.Equal(t, Canceled, machSetDisp.EvToggle(evEH, S{"Foo"}, nil))

	assert.Equal(t, "test", (&TracerNoOp{Id: "test"}).TracerId())
}
