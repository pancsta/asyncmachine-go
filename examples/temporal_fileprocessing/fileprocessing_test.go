package temporal_fileprocessing

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestFileProcessing(t *testing.T) { // Create a channel to receive OS signals
	ctx, cancel := context.WithCancel(context.Background())

	// handle OS exit signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	// start the flow and wait for the result
	machine, err := FileProcessingFlow(ctx, t.Logf, "foo.txt")
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
