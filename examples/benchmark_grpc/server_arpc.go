package benchmark_grpc

import (
	"context"
	"errors"
	"os"

	ss "github.com/pancsta/asyncmachine-go/examples/benchmark_grpc/worker_states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssSrv "github.com/pancsta/asyncmachine-go/pkg/rpc/states/server"
)

type WorkerArpcServer struct {
	Worker *Worker
	Mach   *am.Machine

	RPC *arpc.Server
}

func NewWorkerArpcServer(
	ctx context.Context, addr string, worker *Worker,
) (*WorkerArpcServer, error) {
	// validate
	if worker == nil {
		return nil, errors.New("worker is nil")
	}

	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

	// init
	w := &WorkerArpcServer{
		Worker: worker,
		Mach:   am.New(ctx, ss.States, &am.Opts{ID: "worker"}),
	}

	// verify states and bind to methods
	err := w.Mach.VerifyStates(ss.Names)
	if err != nil {
		return nil, err
	}
	err = w.Mach.BindHandlers(w)
	if err != nil {
		return nil, err
	}

	// bind to worker
	worker.Subscribe(func() {
		w.Mach.Add1(ss.Event, nil)
	})

	// server init
	s, err := arpc.NewServer(ctx, addr, "worker", w.Mach, nil)
	if err != nil {
		return nil, err
	}
	w.RPC = s

	// logging
	w.RPC.Mach.SetLoggerSimple(w.log, logLvl)
	w.Mach.SetLoggerSimple(w.log, logLvl)

	// telemetry debug
	utils.MachDebug(w.RPC.Mach, amDbgAddr, logLvl, false)
	utils.MachDebug(w.Mach, amDbgAddr, logLvl, false)

	// server start
	w.RPC.Start()
	<-w.RPC.Mach.When1(ssSrv.RpcReady, nil)

	return w, nil
}

// methods

func (w *WorkerArpcServer) log(msg string, args ...any) {
	l("arpc-server", msg, args...)
}

// handlers

func (w *WorkerArpcServer) CallOpEnter(e *am.Event) bool {
	_, ok := e.Args["Op"].(Op)
	return ok
}

func (w *WorkerArpcServer) CallOpState(e *am.Event) {
	w.Mach.Remove1(ss.CallOp, nil)

	op := e.Args["Op"].(Op)
	w.Worker.CallOp(op)
}

func (w *WorkerArpcServer) EventState(_ *am.Event) {
	w.Mach.Remove1(ss.Event, nil)

	switch w.Worker.GetValue() {
	case Value1:
		w.Mach.Add1(ss.Value1, nil)
	case Value2:
		w.Mach.Add1(ss.Value2, nil)
	case Value3:
		w.Mach.Add1(ss.Value3, nil)
	}
}

func (w *WorkerArpcServer) StartState(_ *am.Event) {
	w.Worker.Start()
}
