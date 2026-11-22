package helpers_test

import (
	"context"
	"testing"

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
