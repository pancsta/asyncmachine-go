//go:build !tinygo

//go:generate go run ./gen

package machine

import (
	"os"
	"testing"
)

const isTinyGo = false

const funcNameRe = `^github\.com/pancsta/asyncmachine-go/pkg/machine\.` +
	`TestMachineCoverage\.func\d+$`

func assertFuncName(t *testing.T, fn func()) {
	t.Helper()

	assert.Regexp(t, funcNameRe, funcName(fn))
}

// SCHEMA GROUPS

type TestGroups0Def struct {
	// shadow duplicated StatesBase
	*StatesBase

	State00 string
	State01 string
}
type TestGroups1Def struct {
	// shadow duplicated StatesBase
	*TestGroups0Def

	State10 string
	State11 string
}
type TestGroups2Def struct {
	// shadow duplicated StatesBase
	*TestGroups1Def

	State20 string
	State21 string
}

type TestGroupsBaseGroupsDef struct {
	BaseGroup0 S
	BaseGroup1 S
}

type TestGroups2GroupsDef struct {
	*TestGroupsBaseGroupsDef
	Group0           S
	GroupTestNameBar S
}

var testGroups0Schema = Schema{
	testGroups2S.State00: {},
	testGroups2S.State01: {},
}

var testGroups1Schema = testGroups0Schema.Merge(Schema{
	testGroups2S.State10: {},
	testGroups2S.State11: {},
})

var testGroups2Schema = testGroups1Schema.Merge(Schema{
	testGroups2S.State20: {},
	testGroups2S.State21: {},
})

// EXPORTS AND GROUPS

var (
	testGroups2S   = NewStates(TestGroups2Def{})
	testGroupsBase = NewStateGroups(TestGroupsBaseGroupsDef{
		BaseGroup0: S{"State10", "State11"},
		BaseGroup1: S{"State21", "State10"},
	})
	testGroupsG = NewStateGroups(TestGroups2GroupsDef{
		Group0:           S{"State20", "State21"},
		GroupTestNameBar: S{"State11", "State20"},
	}, testGroupsBase)
)

func TestGroups(t *testing.T) {
	if os.Getenv(EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	m := NewCustomStates(t, testGroups2Schema)
	m.SetGroups(testGroupsG, testGroups2S)
	groups, order := m.Groups()

	assert.Equal(t, groups, map[string][]int{
		"BaseGroup0":       {3, 4},
		"BaseGroup1":       {6, 3},
		"Group0":           {5, 6},
		"GroupTestNameBar": {4, 5},

		// schema
		"self":           {5, 6},
		"TestGroups1Def": {3, 4},
		"TestGroups0Def": {1, 2},
	})
	assert.Equal(t, order, []string{
		"Group0", "GroupTestNameBar", "BaseGroup0", "BaseGroup1",
		"self", "TestGroups1Def", "TestGroups0Def",
	})
}

type NestedStatesDef struct {
	*StatesBase

	Nested1 string
	Nested2 string
	Nested3 string
}

var NestedSchema = Schema{
	ssN.Nested1: {Remove: S{ssN.Nested2, ssB.Heartbeat}},
	ssN.Nested2: {Multi: true},
	ssN.Nested3: {Multi: true},
}

type BasicStatesDef struct {
	*StatesBase

	Heartbeat string
	Parent1   *NestedStatesDef
}

var BasicSchema = Schema{}.Merge(
	// prefix with "Parent1"
	NestedSchema.Nest(par1, ssN.Names()),
	Schema{
		ssB.Heartbeat: {},
		// override a nested state
		par1 + ssN.Nested3: {},
	})

var (
	par1 = ssB.Parent1.Prefix()
	ssB  = NewStates(BasicStatesDef{})
	ssN  = NewStates(NestedStatesDef{})
)

// TODO move to _all
func TestNestedSchema(t *testing.T) {
	assert.Equal(t, "Parent1", ssB.Parent1.Prefix())
	assert.Equal(t, "Parent1Nested1", ssB.Parent1.Nested1)
	assert.Equal(t, "Parent1Nested2", BasicSchema[ssB.Parent1.Nested1].Remove[0])
	assert.Equal(t, "Heartbeat", BasicSchema[ssB.Parent1.Nested1].Remove[1])
	assert.True(t, BasicSchema[ssB.Parent1.Nested2].Multi)
	assert.False(t, BasicSchema[ssB.Parent1.Nested3].Multi)
}

// TestPartialImpliedHandlers rejects itself when implied by relations.
type TestPartialImpliedHandlers struct{}

func (h *TestPartialImpliedHandlers) BEnter(e *Event) S {
	if e.StateImplied() {
		return e.AcceptWithoutThis()
	}

	return e.Accept()
}

// TestPartialImpliedStruct tests struct-based handler binding, which is not
// available in tinygo (see [TestPartialImplied] for the map-based variant).
func TestPartialImpliedStruct(t *testing.T) {
	if os.Getenv(EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	m := NewCustomStates(t, Schema{
		"A": {Add: S{"B"}},
		"B": {},
	})
	_, err := m.HandlersBind(&TestPartialImpliedHandlers{})
	require.NoError(t, err)

	assert.Equal(t, Executed, m.Add1("A", nil))
	assertStates(t, m, S{"A"})

	m.Dispose()
	<-m.WhenDisposed()
}
