package node

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
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
}

// bootstrap is a bootstrap machine for a worker to connect to the supervisor,
// after forking.
//
// Flow: Start to WorkerLocalAddr.
type bootstrap struct {
	*am.ExceptionHandler
	Mach *am.Machine

	// Name is the name of this bootstrap.
	Name       string
	LogEnabled bool
	// WorkerArgs contains connection info sent by the Worker.
	WorkerArgs atomic.Pointer[A]

	server *rpc.Server
}

func newBootstrap(
	ctx context.Context, superMach *am.Machine, supName string,
) (*bootstrap, error) {
	c := &bootstrap{
		Name:       supName + utils.RandID(6),
		LogEnabled: os.Getenv(EnvAmNodeLogSupervisor) != "",
	}
	mach, err := am.NewCommon(ctx, "nb-"+c.Name, states.BootstrapStruct,
		ssB.Names(), c, superMach, nil)
	if err != nil {
		return nil, err
	}

	// prefix the random ID
	c.Mach = mach
	amhelp.MachDebugEnv(mach)

	return c, nil
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
	b.log("worker addr %s: %s / %s", args.Id, args.PublicAddr, args.LocalAddr)
	cp := *args
	b.WorkerArgs.Store(&cp)
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

// workerInfo is supervisor's data for a worker.
type workerInfo struct {
	b          *bootstrap
	proc       *os.Process
	rpc        *rpc.Client
	publicAddr string
	localAddr  string
	errs       *cache.Cache
	errsRecent *cache.Cache
	mx         sync.RWMutex
}

func newWorkerInfo(
	s *Supervisor, boot *bootstrap, proc *os.Process,
) *workerInfo {
	return &workerInfo{
		errs:       cache.New(s.WorkerErrTtl, time.Minute),
		errsRecent: cache.New(s.WorkerErrRecent, time.Minute),
		b:          boot,
		proc:       proc,
	}
}

func (w *workerInfo) hasErrs() bool {
	return w.errsRecent.ItemCount() > 0
}
