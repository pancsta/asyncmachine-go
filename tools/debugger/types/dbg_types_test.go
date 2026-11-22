package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestParseMachUrl(t *testing.T) {
	t.Parallel()

	addr, err := ParseMachUrl(
		"mach://mach-1/tx-1/2?t=100&q=5&ht=2026-11-10T12:00:00Z" +
			"&state=Ready&group=grp",
	)
	require.NoError(t, err)
	assert.Equal(t, "mach-1", addr.MachId)
	assert.Equal(t, "tx-1", addr.TxId)
	assert.Equal(t, 2, addr.Step)
	assert.Equal(t, uint64(100), addr.MachTime)
	assert.Equal(t, uint64(5), addr.QueueTick)
	assert.Equal(t, time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC),
		addr.HumanTime)
	assert.Equal(t, "Ready", addr.State)
	assert.Equal(t, "grp", addr.Group)
	assert.False(t, addr.IsCtx)
}

func TestParseCtxUrl(t *testing.T) {
	t.Parallel()

	cv := &am.CtxValue{Id: "mach-1", State: "Ready", Tick: 7}
	addr, err := ParseMachUrl(cv.Url())
	require.NoError(t, err)
	assert.Equal(t, "mach-1", addr.MachId)
	assert.Equal(t, "Ready", addr.State)
	assert.Equal(t, uint64(7), addr.StateTick)
	assert.True(t, addr.IsCtx)
	assert.Equal(t, cv.Url(), addr.StringBase())
	assert.Equal(t, cv.Url(), addr.String())
}

func TestParseCtxUrlInvalid(t *testing.T) {
	t.Parallel()

	for _, u := range []string{
		"ctx://mach-1/Ready",
		"ctx://mach-1/Ready/",
		"ctx://mach-1//7",
		"ctx:///Ready/7",
		"ctx://mach-1/Ready/foo",
	} {
		_, err := ParseMachUrl(u)
		assert.Error(t, err, u)
	}
}
