package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	testutils "github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/node"
	"github.com/pancsta/asyncmachine-go/pkg/node/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
)

var ssW = states.WorkerStates

func main() {
	ctx := context.Background()
	fA := flag.String("a", "", "addr")
	flag.Parse()
	if fA == nil || *fA == "" {
		panic("addr is required")
	}
	addr := *fA

	slog.Info("fork worker", "addr", addr)

	// worker

	// machine init
	mach := am.New(context.Background(), testutils.RelsNodeWorkerStruct, &am.Opts{
		Id: "t-worker-" + addr})
	err := mach.VerifyStates(testutils.RelsNodeWorkerStates)
	if err != nil {
		panic(err)
	}

	if os.Getenv(am.EnvAmDebug) != "" {
		mach.SemLogger().SetLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}
	worker, err := node.NewWorker(ctx, "NTW", mach.Schema(),
		mach.StateNames(), nil)
	if err != nil {
		panic(err)
	}
	err = worker.Mach.BindHandlers(&workerHandlers{Mach: mach})
	if err != nil {
		panic(err)
	}

	// connect Worker to the bootstrap machine
	res := worker.Start(addr)
	if res != am.Executed {
		panic(worker.Mach.Err())
	}
	err = amhelp.WaitForAll(ctx, 1*time.Second,
		worker.Mach.When1(ssW.RpcReady, nil))
	if err != nil {
		panic(err)
	}

	// wait for connection
	_ = amhelp.WaitForAll(ctx, 3*time.Second,
		worker.Mach.When1(ssW.SuperConnected, nil))
	// block until disconnected
	<-worker.Mach.WhenNot1(ssW.SuperConnected, nil)
}

type workerHandlers struct {
	Mach *am.Machine
}

func (w *workerHandlers) WorkRequestedState(e *am.Event) {
	input := e.Args["input"].(int)

	payload := &rpc.ArgsPayload{
		Name:   w.Mach.Id(),
		Data:   input * input,
		Source: e.Machine().Id(),
	}

	e.Machine().Add1(ssW.ClientSendPayload, rpc.PassRpc(&rpc.A{
		Name:    w.Mach.Id(),
		Payload: payload,
	}))
}
