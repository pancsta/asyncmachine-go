package temporal_fileprocessing

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/joho/godotenv"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func init() {
	// load .env
	_ = godotenv.Load()

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetLogLevel(am.LogChanges)
}

func TestFileProcessing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// handle OS exit signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	// start the flow and wait for the result
	mach, err := FileProcessingFlow(ctx, t.Logf, "foo.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !mach.Is(am.S{"FileUploaded"}) {
		t.Fatal("not FileUploaded")
	}

	t.Log(mach.String())
	t.Log(mach.StringAll())
	t.Log(mach.Inspect(nil))
}
