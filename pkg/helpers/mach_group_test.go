//go:build !tinygo

package helpers_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

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
		assert.Len(t, filter.Filter(machs), 3)
	})

	t.Run("limit", func(t *testing.T) {
		filter := &amhelp.MachGroupFilter{Limit: 2}
		assert.Len(t, filter.Filter(machs), 2)
	})

	t.Run("start idx", func(t *testing.T) {
		filter := &amhelp.MachGroupFilter{StartIdx: 1}
		got := filter.Filter(machs)
		assert.Len(t, got, 2)
		assert.Equal(t, "m2", got[0].Id())
	})

	t.Run("filter + pagination", func(t *testing.T) {
		filter := &amhelp.MachGroupFilter{IdSuffix: "2", Limit: 1}
		got := filter.Filter(machs)
		assert.Len(t, got, 1)
		assert.Equal(t, "m2", got[0].Id())
	})
}
