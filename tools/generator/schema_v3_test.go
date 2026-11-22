package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type FixtureGroupsSuper struct {
	GroupB S
	GroupA S
}

type FixtureGroups struct {
	*FixtureGroupsSuper
	GroupC S
}

func TestExtractGroupsOrdered(t *testing.T) {
	groups := &FixtureGroups{
		FixtureGroupsSuper: &FixtureGroupsSuper{
			GroupB: S{"B"},
			GroupA: S{"A"},
		},
		GroupC: S{"C"},
	}

	// order must be stable across map iterations
	for range 20 {
		ordered := ExtractGroupsOrdered(groups)
		names := make([]string, 0, len(ordered))
		for _, group := range ordered {
			names = append(names, group.Name)
		}
		assert.Equal(t, []string{"GroupB", "GroupA", "GroupC"}, names)
	}

	// the unordered API keeps working
	assert.Equal(t, map[string]S{
		"GroupA": {"A"},
		"GroupB": {"B"},
		"GroupC": {"C"},
	}, ExtractGroups(groups))
}
