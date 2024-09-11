package machine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWithOpts(t *testing.T) {
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
	OptsWithParentTracers(opts, mach)
	assert.Equal(t, mach.Tracers[0], opts.Tracers[0])
	assert.NotNil(t, opts.LogArgs)
}

func TestResultString(t *testing.T) {
	assert.Equal(t, "executed", Executed.String())
	assert.Equal(t, "canceled", Canceled.String())
	assert.Equal(t, "queued", Queued.String())
}

func TestMutationTypeString(t *testing.T) {
	assert.Equal(t, "add", MutationAdd.String())
	assert.Equal(t, "remove", MutationRemove.String())
	assert.Equal(t, "set", MutationSet.String())
	assert.Equal(t, "eval", MutationEval.String())
}

func TestStepTypeString(t *testing.T) {
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
	assert.Equal(t, "after", RelationAfter.String())
	assert.Equal(t, "add", RelationAdd.String())
	assert.Equal(t, "require", RelationRequire.String())
	assert.Equal(t, "remove", RelationRemove.String())
}

func TestLogLevelString(t *testing.T) {
	assert.Equal(t, "nothing", LogNothing.String())
	assert.Equal(t, "nothing", LogLevel(0).String())
	assert.Equal(t, "changes", LogChanges.String())
	assert.Equal(t, "ops", LogOps.String())
	assert.Equal(t, "decisions", LogDecisions.String())
	assert.Equal(t, "everything", LogEverything.String())
}

func TestNewArgsMapper(t *testing.T) {
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
	s := S{"A", "B", "C"}
	s2 := S{"C", "D", "E"}
	ex := S{"A", "B", "C", "D", "E"}
	assert.Equal(t, ex, SAdd(s, s2))
	assert.Equal(t, S{}, SAdd())
}

func TestNormalizeID(t *testing.T) {
	assert.Equal(t, "foo_bar_baz", NormalizeID("Foo Bar-Baz"))
}

func TestIsActiveTick(t *testing.T) {
	assert.True(t, IsActiveTick(1))
	assert.False(t, IsActiveTick(0))
	assert.False(t, IsActiveTick(6548734))
	assert.True(t, IsActiveTick(6548735))
}
