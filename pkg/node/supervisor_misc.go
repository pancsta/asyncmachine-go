package node

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/patrickmn/go-cache"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/node/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

type SupervisorOpts struct {
	// InstanceNum is the number of this instance in a failsafe config, used for
	// the ID.
	InstanceNum int
	// Parent is a parent state machine for a new Supervisor state machine. See
	// [am.Opts].
	Parent am.Api
	// TODO
	Tags []string
}

// bootstrap is a bootstrap machine for a worker to connect to the supervisor,
// after forking.
//
// Flow: Start to WorkerLocalAddr.
type bootstrap struct {
	*am.ExceptionHandler
	Mach  *am.Machine
	Super *Supervisor

	// Name is the name of this bootstrap.
	Name       string
	LogEnabled bool

	server *rpc.Server
}

func newBootstrap(ctx context.Context, super *Supervisor) (*bootstrap, error) {
	b := &bootstrap{
		Super:      super,
		Name:       super.Name + utils.RandID(6),
		LogEnabled: os.Getenv(EnvAmNodeLogSupervisor) != "",
	}
	mach, err := am.NewCommon(ctx, "nb-"+b.Name, states.BootstrapStruct,
		ssB.Names(), b, super.Mach, &am.Opts{Tags: []string{"node-bootstrap"}})
	if err != nil {
		return nil, err
	}

	// prefix the random ID
	b.Mach = mach
	amhelp.MachDebugEnv(mach)

	return b, nil
}

func (b *bootstrap) StartState(e *am.Event) {
	var err error
	ctx := b.Mach.NewStateCtx(ssB.Start)

	// init rpc
	b.server, err = rpc.NewServer(ctx, "localhost:0", "nb-"+b.Name, b.Mach,
		&rpc.ServerOpts{Parent: b.Mach})
	if err != nil {
		b.Mach.AddErrState(ssB.ErrNetwork, err, nil)
		return
	}
	amhelp.MachDebugEnv(b.server.Mach)
	err = ampipe.BindErr(b.server.Mach, b.Mach, ssB.ErrNetwork)
	if err != nil {
		b.Mach.AddErr(err, nil)
		return
	}

	// start
	b.server.Start()

	// timeout
	go func() {
		amhelp.Wait(ctx, b.Super.ConnTimeout)
		if !b.Mach.IsDisposed() && b.Mach.Not1(ssB.WorkerAddr) {
			err := fmt.Errorf("worker bootstrap: %w", am.ErrTimeout)
			b.Super.Mach.AddErrState(ssS.ErrWorker, err, Pass(&A{
				Bootstrap: b,
				Id:        b.Mach.Id(),
			}))
		}
	}()
}

func (b *bootstrap) StartEnd(e *am.Event) {
	if b.server != nil {
		b.server.Stop(true)
	}
	b.Mach.Dispose()
}

func (b *bootstrap) WorkerAddrEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.LocalAddr != "" && a.PublicAddr != "" && a.Id != ""
}

func (b *bootstrap) WorkerAddrState(e *am.Event) {
	args := ParseArgs(e.Args)
	// copy
	argsOut := *args
	argsOut.BootAddr = b.Addr()
	b.log("worker addr %s: %s / %s", args.Id, args.PublicAddr, args.LocalAddr)
	// pass the conn info to the supervisor, and self destruct
	b.Super.Mach.Add1(ssS.WorkerConnected, Pass(&argsOut))

	// dispose after a while
	go func() {
		time.Sleep(1 * time.Second)
		b.Mach.Remove1(ssB.Start, nil)
	}()
}

func (b *bootstrap) Dispose() {
	// TODO send bye to rpc-c
	b.log("disposing bootstrap")
	b.Mach.Remove1(ssB.Start, nil)
}

// Addr returns the address of the bootstrap server.
func (b *bootstrap) Addr() string {
	if b.server == nil {
		return ""
	}

	return b.server.Addr
}

func (b *bootstrap) log(msg string, args ...any) {
	if !b.LogEnabled {
		return
	}
	b.Mach.Log(msg, args...)
}

// workerInfo is supervisor's data for a remote node worker.
type workerInfo struct {
	// proc is the OS process of this node worker
	proc *os.Process
	// rpc is the RPC client for this node worker
	rpc *rpc.Client
	// w is the RPC worker of this node worker. It locally represents the remote
	// node worker.
	w          *rpc.Worker
	publicAddr string
	localAddr  string
	errs       *cache.Cache
	errsRecent *cache.Cache
}

func newWorkerInfo(s *Supervisor, proc *os.Process) *workerInfo {
	return &workerInfo{
		errs:       cache.New(s.WorkerErrTtl, time.Minute),
		errsRecent: cache.New(s.WorkerErrRecent, time.Minute),
		proc:       proc,
	}
}

func (w *workerInfo) hasErrs() bool {
	return w.errsRecent.ItemCount() > 0
}
