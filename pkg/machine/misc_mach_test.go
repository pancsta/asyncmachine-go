package machine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWithOpts(t *testing.T) {
	t.Parallel()

	// OptsWithDebug
	opts := &Opts{
		DontPanicToException: false,
		HandlerTimeout:       0,
	}
	OptsWithDebug(opts)
	assert.True(t, opts.DontPanicToException)
	assert.Greater(t, opts.HandlerTimeout, time.Duration(0))

	// OptsWithTracers
	tracer := &NoOpTracer{}
	OptsWithTracers(opts, tracer)

	assert.Equal(t, tracer, opts.Tracers[0])

	// OptsWithParentTracers
	mach := New(context.TODO(), nil, opts)
	mach.SetLogArgs(NewArgsMapper([]string{"arg"}, 10))
}

func TestResultString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "executed", Executed.String())
	assert.Equal(t, "canceled", Canceled.String())
	assert.Equal(t, "queued", Queued.String())
}

func TestMutationTypeString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "add", MutationAdd.String())
	assert.Equal(t, "remove", MutationRemove.String())
	assert.Equal(t, "set", MutationSet.String())
	assert.Equal(t, "eval", mutationEval.String())
}

func TestStepTypeString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "rel", StepRelation.String())
	assert.Equal(t, "handler", StepHandler.String())
	assert.Equal(t, "set", StepSet.String())
	assert.Equal(t, "remove", StepRemove.String())
	assert.Equal(t, "removenotactive", StepRemoveNotActive.String())
	assert.Equal(t, "requested", StepRequested.String())
	assert.Equal(t, "cancel", StepCancel.String())
	assert.Equal(t, "", StepType(0).String())
}

func TestRelationString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "after", RelationAfter.String())
	assert.Equal(t, "add", RelationAdd.String())
	assert.Equal(t, "require", RelationRequire.String())
	assert.Equal(t, "remove", RelationRemove.String())
}

func TestLogLevelString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "nothing", LogNothing.String())
	assert.Equal(t, "nothing", LogLevel(0).String())
	assert.Equal(t, "changes", LogChanges.String())
	assert.Equal(t, "ops", LogOps.String())
	assert.Equal(t, "decisions", LogDecisions.String())
	assert.Equal(t, "everything", LogEverything.String())
}

func TestNewArgsMapper(t *testing.T) {
	t.Parallel()

	// short
	mapper := NewArgsMapper([]string{"arg", "arg2"}, 2)
	res := mapper(A{"arg": "foo"})
	assert.Equal(t, "fo", res["arg"])
	res = mapper(A{"arg": "foo", "arg2": "bar"})
	assert.Equal(t, "fo", res["arg"])
	assert.Equal(t, "ba", res["arg2"])

	// long
	mapper = NewArgsMapper([]string{"arg", "arg2"}, 5)
	args := A{"arg": "foofoofoo"}
	res = mapper(args)
	assert.Equal(t, "fo...", res["arg"])
}

func TestParseStruct(t *testing.T) {
	t.Parallel()

	s := Struct{
		"A": {
			Remove: S{"A", "B", "C"},
			Add:    S{"C"},
			After:  S{"A"},
		},
		"B": {},
		"C": {},
	}
	ex := Struct{
		"A": {
			Remove: S{"B"},
			Add:    S{"C"},
			After:  S{},
		},
		"B": {},
		"C": {},
	}
	assert.Equal(t, ex, parseStruct(s))
}

func TestSMerge(t *testing.T) {
	t.Parallel()

	s := S{"A", "B", "C"}
	s2 := S{"C", "D", "E"}
	ex := S{"A", "B", "C", "D", "E"}
	assert.Equal(t, ex, SAdd(s, s2))
	assert.Equal(t, S{}, SAdd())
}

func TestIsActiveTick(t *testing.T) {
	t.Parallel()

	assert.True(t, IsActiveTick(1))
	assert.False(t, IsActiveTick(0))
	assert.False(t, IsActiveTick(6548734))
	assert.True(t, IsActiveTick(6548735))
}

// TestStatesFile

// super states
type TestStatesFileStatesSuperDef struct {
	*StatesBase

	Foo string
	Baz string
}

type TestStatesFileGroupsSuperDef struct {
	FooBaz S
}

var TestStatesFileSuperStruct = Struct{
	testSuperStates.Foo: {},
	testSuperStates.Baz: {},
}

// exports and groups
var (
	testSuperStates = NewStates(TestStatesFileStatesSuperDef{})
	testSuperGroups = NewStateGroups(TestStatesFileGroupsSuperDef{
		FooBaz: S{testSuperStates.Foo, testSuperStates.Baz},
	})
)

// child states
type TestStatesFileStatesDef struct {
	Bar string

	// inherit from TestStatesFileStatesSuperDef
	*TestStatesFileStatesSuperDef
}

type TestStatesFileGroupsDef struct {
	*TestStatesFileGroupsSuperDef

	FooBar S
}

var TestStatesFileStruct = StructMerge(
	TestStatesFileSuperStruct,
	Struct{
		testStates.Bar: {},
	})

// exports and groups
var (
	testStates = NewStates(TestStatesFileStatesDef{})
	testGroups = NewStateGroups(TestStatesFileGroupsDef{
		FooBar: S{testStates.Foo, testStates.Bar},
	}, testSuperGroups)
)

func TestStatesFile(t *testing.T) {
	t.Parallel()

	assert.Equal(t, testStates.Foo, "Foo")
	assert.Equal(t, testStates.Bar, "Bar")
	assert.Equal(t, testGroups.FooBar, S{"Foo", "Bar"})
	assert.Equal(t, testGroups.FooBaz, S{"Foo", "Baz"})
	assert.NotNil(t, TestStatesFileStruct["Foo"])
}

func TestTimeMethods(t *testing.T) {
	t.Parallel()

	mt := Time{2, 1, 3}
	assert.Equal(t, mt.Is1(1), true)
	assert.Equal(t, mt.Is1(-1), false)
	assert.Equal(t, mt.Is([]int{1}), true)
	assert.Equal(t, mt.Is([]int{-1}), false)
	assert.Equal(t, mt.Any1(1), true)
	assert.Equal(t, mt.Any1(2), true)

	mt2 := mt.Add(mt)
	assert.Equal(t, mt2.DiffSince(mt), mt)
}
