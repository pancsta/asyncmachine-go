//go:build !tinygo

package utils

import (
	"context"
	"os"
	"testing"
	"time"

	ss "github.com/pancsta/asyncmachine-go/internal/testing/states"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssnode "github.com/pancsta/asyncmachine-go/pkg/node/states"
)

// isTinyGo reports whether the current build targets TinyGo.
const isTinyGo = false

// inherit from RPC worker

var (
	RelsNodeWorkerSchema = ssnode.WorkerSchema.Merge(ss.States)
	RelsNodeWorkerStates = am.SAdd(ss.Names, ssnode.WorkerStates.Names())
)

// NewRelsNodeWorker creates a new Node worker machine with basic relations
// between ABCD states.
func NewRelsNodeWorker(t *testing.T, initialState am.S) *am.Machine {

	// machine init
	mach := am.New(context.Background(), RelsNodeWorkerSchema, &am.Opts{
		Id: "t-" + t.Name()})
	err := mach.VerifyStates(RelsNodeWorkerStates)
	if err != nil {
		t.Fatal(err)
	}

	mach.SemLogger().SetLevel(am.EnvLogLevel(os.Getenv(am.EnvAmLog)))
	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}
	mach.SemLogger().SetArgsMapper(am.NewLogArgsMapper(50, am.LogArgs))

	return mach
}
