package temporal_fileprocessing

import (
	"context"
	"testing"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestFileProcessing(t *testing.T) {
	// start the flow and wait for the result
	machine, err := FileProcessingFlow(context.Background(), t.Logf, "foo.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !machine.Is(am.S{"FileUploaded"}) {
		t.Fatal("not FileUploaded")
	}

	// how it looks at the end
	t.Log("\n" + machine.String())
	t.Log("\n" + machine.StringAll())
	t.Log("\n" + machine.Inspect(nil))
}
