package helpers_test

import (
	"context"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestTagSetOnMach(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mach := am.New(ctx, am.Schema{"Foo": {}}, &am.Opts{Id: "tag-set-on-mach"})
	if err := mach.VerifyStates(am.S{"Foo", am.StateException}); err != nil {
		t.Fatal(err)
	}

	// add a new tag
	assert.True(t, amhelp.TagSetOnMach(mach, "addr", "foo:1"))
	assert.Equal(t, "foo:1", amhelp.TagValue(mach.Tags(), "addr"))

	// replace an existing value
	assert.True(t, amhelp.TagSetOnMach(mach, "addr", "bar:2"))
	assert.Equal(t, "bar:2", amhelp.TagValue(mach.Tags(), "addr"))

	// no change
	assert.False(t, amhelp.TagSetOnMach(mach, "addr", "bar:2"))
}

func TestBatch(t *testing.T) {
	var mx sync.Mutex
	var batches [][]int
	b := &amhelp.Batch[int]{
		Window: 20 * time.Millisecond,
		Flush: func(items []int) {
			mx.Lock()
			defer mx.Unlock()
			batches = append(batches, items)
		},
	}

	b.Add(1)
	b.Add(2)
	b.Add(3)

	// all items are flushed together after the window
	assert.Eventually(t, func() bool {
		mx.Lock()
		defer mx.Unlock()
		return len(batches) == 1 && len(batches[0]) == 3
	}, time.Second, 10*time.Millisecond)
}

func TestBatchMaxSize(t *testing.T) {
	flushed := make(chan []int, 1)
	b := &amhelp.Batch[int]{
		Window:  time.Second,
		MaxSize: 2,
		Flush: func(items []int) {
			flushed <- items
		},
	}

	b.Add(1)
	b.Add(2)

	// flushed before the window elapses
	select {
	case items := <-flushed:
		assert.Equal(t, []int{1, 2}, items)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("batch not flushed on max size")
	}
}

func TestCondMatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mach := am.New(ctx, am.Schema{"Foo": {}, "Bar": {}, "Baz": {}},
		&am.Opts{Id: "cond-match"})
	mach.Add(am.S{"Foo", "Bar"}, nil)

	tests := []struct {
		name string
		cond amhelp.Cond
		want bool
	}{
		{"all match", amhelp.Cond{IsMatch: am.S{"^Fo", "^Ba"}}, true},
		{"all match fail", amhelp.Cond{IsMatch: am.S{"^Fo", "^Baz"}}, false},
		{"any match", amhelp.Cond{AnyMatch: am.S{"^Nope", "^Ba"}}, true},
		{"any match fail", amhelp.Cond{AnyMatch: am.S{"^Nope"}}, false},
		{"not match", amhelp.Cond{NotMatch: am.S{"^Nope"}}, true},
		{"not match fail", amhelp.Cond{NotMatch: am.S{"^Fo"}}, false},
		{"invalid regexp", amhelp.Cond{AnyMatch: am.S{"("}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cond.Check(mach))
		})
	}
}

func TestCondQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mach := am.New(ctx, am.Schema{"Foo": {}, "Bar": {}},
		&am.Opts{Id: "cond-query"})
	mach.Add(am.S{"Foo", "Bar"}, nil)

	both := func(clock am.Clock) bool {
		return clock["Foo"] == 1 && clock["Bar"] == 1
	}
	fooOnly := func(clock am.Clock) bool {
		return clock["Foo"] == 1 && clock["Bar"] == 0
	}
	none := func(am.Clock) bool { return false }
	var nilQuery func(am.Clock) bool

	tests := []struct {
		name string
		cond amhelp.Cond
		want bool
	}{
		{"all match", amhelp.Cond{Query: []func(am.Clock) bool{both, both}}, true},
		{"all match fail", amhelp.Cond{Query: []func(am.Clock) bool{both, fooOnly}}, false},
		{"any match", amhelp.Cond{AnyQuery: []func(am.Clock) bool{fooOnly, both}}, true},
		{"any match fail", amhelp.Cond{AnyQuery: []func(am.Clock) bool{fooOnly, none}}, false},
		{"not match", amhelp.Cond{NotQuery: []func(am.Clock) bool{fooOnly, none}}, true},
		{"not match fail", amhelp.Cond{NotQuery: []func(am.Clock) bool{none, both}}, false},
		{"nil query", amhelp.Cond{Query: []func(am.Clock) bool{nilQuery}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cond.Check(mach))
		})
	}
}

func newTestMach(t *testing.T, id, parentId string, states ...string) *am.Machine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	schema := am.Schema{}
	for _, state := range states {
		schema[state] = am.State{}
	}
	mach := am.New(ctx, schema, &am.Opts{Id: id, ParentId: parentId})

	return mach
}

func TestMachGroupFilter(t *testing.T) {
	parent := newTestMach(t, "parent", "", "Foo")
	child := newTestMach(t, "child", parent.Id(), "Foo", "Bar")
	other := newTestMach(t, "other", "", "Baz")

	child.Add1("Bar", nil)
	other.Add1("Baz", nil)

	tests := []struct {
		name   string
		filter amhelp.MachGroupFilter
		mach   am.Api
		want   bool
	}{
		{"empty", amhelp.MachGroupFilter{}, child, true},
		{"id exact match", amhelp.MachGroupFilter{IdExact: "child"}, child, true},
		{"id exact mismatch", amhelp.MachGroupFilter{IdExact: "other"}, child, false},
		{"id substr", amhelp.MachGroupFilter{IdSubstr: "hil"}, child, true},
		{"id prefix", amhelp.MachGroupFilter{IdPrefix: "chi"}, child, true},
		{"id suffix", amhelp.MachGroupFilter{IdSuffix: "ild"}, child, true},
		{"id regexp", amhelp.MachGroupFilter{IdRegexp: regexp.MustCompile("^chi")}, child, true},
		{"parent", amhelp.MachGroupFilter{Parent: "parent"}, child, true},
		{"parent mismatch", amhelp.MachGroupFilter{Parent: "nope"}, child, false},
		{"active states", amhelp.MachGroupFilter{StatesActive: am.S{"Bar"}}, child, true},
		{"missing active state", amhelp.MachGroupFilter{StatesActive: am.S{"Baz"}}, child, false},
		{"inactive states", amhelp.MachGroupFilter{StatesInactive: am.S{"Foo"}}, child, true},
		{"bis inactive state", amhelp.MachGroupFilter{StatesInactive: am.S{"Bar"}}, child, false},
		{"mtime min", amhelp.MachGroupFilter{MtimeMin: 1}, child, true},
		{"mtime min mismatch", amhelp.MachGroupFilter{MtimeMin: 2}, child, false},
		{"mtime max", amhelp.MachGroupFilter{MtimeMax: 1}, child, true},
		{"mtime max disabled", amhelp.MachGroupFilter{MtimeMax: 0}, child, true},
		{
			"mtime states",
			amhelp.MachGroupFilter{MtimeMin: 1, MtimeStates: am.S{"Bar"}},
			child, true,
		},
		{
			"mtime states mismatch",
			amhelp.MachGroupFilter{MtimeMin: 1, MtimeStates: am.S{"Foo"}},
			child, false,
		},
		{"other mach", amhelp.MachGroupFilter{StatesActive: am.S{"Baz"}}, other, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.filter.Matches(tt.mach))
		})
	}
}

func TestMachGroupFilterFilter(t *testing.T) {
	machs := []am.Api{
		newTestMach(t, "m1", "", "Foo"),
		newTestMach(t, "m2", "", "Foo"),
		newTestMach(t, "m3", "", "Foo"),
	}

	t.Run("no pagination", func(t *testing.T) {
		filter := &amhelp.MachGroupFilter{}
		assert.Len(t, amhelp.MachGroup(machs).Filter(filter), 3)
	})

	t.Run("limit", func(t *testing.T) {
		filter := &amhelp.MachGroupFilter{Limit: 2}
		assert.Len(t, amhelp.MachGroup(machs).Filter(filter), 2)
	})

	t.Run("start idx", func(t *testing.T) {
		filter := &amhelp.MachGroupFilter{StartIdx: 1}
		got := amhelp.MachGroup(machs).Filter(filter)
		assert.Len(t, got, 2)
		assert.Equal(t, "m2", got[0].Id())
	})

	t.Run("filter + pagination", func(t *testing.T) {
		filter := &amhelp.MachGroupFilter{IdSuffix: "2", Limit: 1}
		got := amhelp.MachGroup(machs).Filter(filter)
		assert.Len(t, got, 1)
		assert.Equal(t, "m2", got[0].Id())
	})
}
