package temporal_fileprocessing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestFileProcessing(t *testing.T) {
	// start the flow and wait for the result
	machine, err := FileProcessingFlow(context.Background(), t.Logf, "foo.txt")
	assert.NoError(t, err)
	assert.True(t, machine.Is(am.S{"FileUploaded"}))

	// how it looks at the end
	t.Log("\n" + machine.String())
	t.Log("\n" + machine.StringAll())
	t.Log("\n" + machine.Inspect(nil))
}
