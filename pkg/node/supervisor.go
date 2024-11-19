package node

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"slices"
	"sync"
	"time"

	cmap "github.com/orcaman/concurrent-map/v2"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/node/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

var (
	ssS = states.SupervisorStates
	ssB = states.BootstrapStates
)

type Supervisor struct {
	*am.ExceptionHandler
	Mach *am.Machine

	// WorkerKind is the kind of worker this supervisor is managing.
	WorkerKind string
	// WorkerBin is the path and args to the worker binary.
	WorkerBin []string
	// Name is the name of the supervisor.
	Name       string
	LogEnabled bool

	// worker pool

	// Max is the maximum number of workers. Default is 10.
	Max int
	// Min is the minimum number of workers. Default is 2.
	Min int
	// Warm is the number of warm (ready) workers. Default is 5.
	Warm int
	// MaxClientWorkers is the maximum number of workers per 1 client. Defaults to
	// Max.
	MaxClientWorkers int
	// WorkerErrTtl is the time to keep worker errors in memory. Default is 10m.
	WorkerErrTtl time.Duration
	// WorkerErrRecent is the time to consider recent errors. Default is 1m.
	WorkerErrRecent time.Duration
	// WorkerErrKill is the number of errors to kill a worker. Default is 3.
	WorkerErrKill int

	// network

	// ConnTimeout is the time to wait for an outbound connection to be
	// established. Default is 5s.
	ConnTimeout     time.Duration
	DeliveryTimeout time.Duration
	// PoolPause is the time to wait between normalizing the pool. Default is 5s.
	PoolPause time.Duration
	// HealthcheckPause is the time between trying to get a Healtcheck response
	// from a worker.
	HealthcheckPause time.Duration
	Heartbeat        time.Duration

	// PublicAddr is the address for the public RPC server to listen on. The
	// effective address is at [PublicMux.Addr].
	PublicAddr string
	// PublicMux is the public listener to create RPC servers for each client.
	PublicMux *rpc.Mux
	// PublicRpc are the public RPC servers of connected clients, indexed by
	// remote addresses.
	PublicRpcs map[string]*rpc.Server

	// LocalAddr is the address for the local RPC server to listen on. The
	// effective address is at [LocalRpc.Addr].
	LocalAddr string
	// LocalRpc is the local RPC server, used by other supervisors to connect.
	// TODO rpc/mux
	LocalRpc *rpc.Server

	// TODO healthcheck endpoint
	// HttpAddr string

	// workerPids is a map of local RPC addresses to workerInfo data.
	workers cmap.ConcurrentMap[string, *workerInfo]

	// workerSNames is a list of states for the worker.
	workerSNames am.S
	// workerStruct is the struct for the worker.
	workerStruct am.Struct

	// in-memory workers

	testFork func(string) error
	testKill func(string) error

	normalizeStart time.Time

	// self removing multi handlers

	WorkerReadyState       am.HandlerFinal
	WorkerGoneState        am.HandlerFinal
	ClientSendPayloadState am.HandlerFinal
	SuperSendPayloadState  am.HandlerFinal
}

// NewSupervisor initializes and returns a new Supervisor instance with
// specified context, worker attributes, and options.
func NewSupervisor(
	ctx context.Context, workerKind string, workerBin []string,
	workerStruct am.Struct, workerSNames am.S, opts *SupervisorOpts,
) (*Supervisor, error) {
	// validate
	if len(workerBin) == 0 || workerBin[0] == "" {
		return nil, errors.New("super: workerBin required")
	}
	if workerStruct == nil {
		return nil, errors.New("super: workerStruct required")
	}
	if workerSNames == nil {
		return nil, errors.New("super: workerSNames required")
	}
	if opts == nil {
		opts = &SupervisorOpts{}
	}

	err := amhelp.Implements(workerSNames, states.WorkerStates.Names())
	if err != nil {
		err := fmt.Errorf(
			"worker has to implement am/node/states/WorkerStates: %w", err)
		return nil, err
	}

	name := fmt.Sprintf("%s-%s-%s-%d", workerKind, utils.Hostname(),
		time.Now().Format("150405"), opts.InstanceNum)

	s := &Supervisor{
		WorkerKind: workerKind,
		WorkerBin:  workerBin,
		Name:       name,
		LogEnabled: os.Getenv(EnvAmNodeLogSupervisor) != "",

		// defaults

		Max:              10,
		Min:              2,
		Warm:             5,
		MaxClientWorkers: 10,
		ConnTimeout:      5 * time.Second,
		DeliveryTimeout:  5 * time.Second,
		Heartbeat:        1 * time.Minute,
		PoolPause:        5 * time.Second,
		HealthcheckPause: 500 * time.Millisecond,
		WorkerErrTtl:     10 * time.Minute,
		WorkerErrRecent:  1 * time.Minute,
		WorkerErrKill:    3,

		// rpc

		PublicRpcs: make(map[string]*rpc.Server),

		// internals

		workerSNames: workerSNames,
		workerStruct: workerStruct,
		workers:      cmap.New[*workerInfo](),
	}

	if amhelp.IsDebug() {
		// increase timeouts using context.WithTimeout directly
		s.DeliveryTimeout = 10 * s.DeliveryTimeout
	}

	mach, err := am.NewCommon(ctx, "ns-"+s.Name, states.SupervisorStruct,
		ssS.Names(), s, opts.Parent, nil)
	if err != nil {
		return nil, err
	}

	mach.SetLogArgs(LogArgs)
	s.Mach = mach
	amhelp.MachDebugEnv(mach)

	// self removing multi handlers
	s.WorkerReadyState = amhelp.RemoveMulti(mach, ssS.WorkerReady)
	s.WorkerGoneState = amhelp.RemoveMulti(mach, ssS.WorkerGone)
	s.ClientSendPayloadState = amhelp.RemoveMulti(mach, ssS.ClientSendPayload)
	s.SuperSendPayloadState = amhelp.RemoveMulti(mach, ssS.SuperSendPayload)

	// check base states
	err = amhelp.Implements(mach.StateNames(), ssS.Names())
	if err != nil {
		err := fmt.Errorf(
			"client has to implement am/node/states/SupervisorStates: %w", err)
		return nil, err
	}

	return s, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (s *Supervisor) ErrWorkerState(e *am.Event) {
	// remove errors as handled
	s.Mach.Remove(am.S{ssS.ErrWorker, ssS.Exception}, nil)
	args := ParseArgs(e.Args)
	w, _ := s.workers.Get(args.LocalAddr)
	// TODO am.ParseArgs
	err, _ := e.Args["err"].(error)

	// kill?
	if !errors.Is(err, ErrWorkerKill) && w != nil {
		err1 := w.errs.Add(utils.RandID(0), err, 0)
		err2 := w.errsRecent.Add(utils.RandID(0), err, 0)
		if err := errors.Join(err1, err2); err != nil {
			s.Mach.Log("failed to add error to worker %s: %v", args.LocalAddr, err)
		}

		// kill if too many errs
		if w.errs.ItemCount() > s.WorkerErrKill {
			s.Mach.Add1(ssS.KillingWorker, Pass(&A{
				LocalAddr: args.LocalAddr,
			}))
		}
	}

	// dispose bootstrap
	if args.Bootstrap != nil {
		args.Bootstrap.Dispose()
	}

	// re-check the pool status
	s.Mach.Remove1(ssS.PoolReady, nil)
}

// TODO ErrPool

func (s *Supervisor) StartEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.PublicAddr != "" && a.LocalAddr != ""
}

func (s *Supervisor) ClientConnectedState(e *am.Event) {
	s.Mach.Remove1(ssS.ClientConnected, nil)
}

func (s *Supervisor) ClientDisconnectedEnter(e *am.Event) bool {
	a := rpc.ParseArgs(e.Args)
	return a != nil && a.Addr != ""
}

func (s *Supervisor) ClientDisconnectedState(e *am.Event) {
	s.Mach.Remove1(ssS.ClientDisconnected, nil)

	addr := rpc.ParseArgs(e.Args).Addr
	srv, ok := s.PublicRpcs[addr]
	if !ok {
		s.log("client %s disconnected, but not found", addr)
	}
	srv.Stop(true)
	delete(s.PublicRpcs, addr)
}

func (s *Supervisor) StartState(e *am.Event) {
	var err error
	ctx := s.Mach.NewStateCtx(ssS.Start)
	args := ParseArgs(e.Args)
	s.LocalAddr = args.LocalAddr
	s.PublicAddr = args.PublicAddr

	// public rpc (muxed)
	s.PublicMux, err = rpc.NewMux(ctx, "ns-pub-"+s.Name, s.newClientConn,
		&rpc.MuxOpts{Parent: s.Mach})
	if err != nil {
		AddErrRpc(s.Mach, err, nil)
		return
	}
	amhelp.MachDebugEnv(s.PublicMux.Mach)

	// local rpc
	opts := &rpc.ServerOpts{
		Parent:       s.Mach,
		PayloadState: ssS.SuperSendPayload,
	}
	s.LocalRpc, err = rpc.NewServer(ctx, s.LocalAddr, "ns-loc-"+s.Name, s.Mach,
		opts)
	if err != nil {
		AddErrRpc(s.Mach, err, nil)
		return
	}
	amhelp.MachDebugEnv(s.LocalRpc.Mach)
	s.LocalRpc.DeliveryTimeout = s.DeliveryTimeout
	err = rpc.BindServerMulti(s.LocalRpc.Mach, s.Mach, ssS.LocalRpcReady,
		ssS.SuperConnected, ssS.SuperDisconnected)
	if err != nil {
		AddErrRpc(s.Mach, err, nil)
		return
	}

	// start
	s.PublicMux.Start()
	s.LocalRpc.Start()

	// unblock
	go func() {
		// wait for the RPC servers to become ready
		err := amhelp.WaitForAll(ctx, s.ConnTimeout,
			s.PublicMux.Mach.When1(ssrpc.MuxStates.Ready, nil),
			s.LocalRpc.Mach.When1(ssrpc.ServerStates.RpcReady, nil))
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			err := errors.Join(err, s.PublicMux.Mach.Err(), s.LocalRpc.Mach.Err())
			AddErrRpc(s.Mach, err, nil)
			return
		}

		// wait for the pool
		select {
		case <-ctx.Done():
			return // expired
		case <-s.Mach.When1(ssS.PoolReady, nil):
		}

		// start Heartbeat
		t := time.NewTicker(s.Heartbeat)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return // expired
			case <-t.C:
				s.Mach.Add1(ssS.Heartbeat, nil)
			}
		}
	}()
}

func (s *Supervisor) StartEnd(e *am.Event) {
	// TODO stop all rpc servers
	if s.PublicMux != nil {
		s.PublicMux.Stop(true)
	}
	if s.LocalRpc != nil {
		s.LocalRpc.Stop(true)
	}
}

func (s *Supervisor) ForkWorkerEnter(e *am.Event) bool {
	return s.workers.Count() < s.Max
}

func (s *Supervisor) ForkWorkerState(e *am.Event) {
	s.Mach.Remove1(ssS.ForkWorker, nil)
	ctx := s.Mach.NewStateCtx(ssS.Start)

	// init bootstrap machine
	boot, err := newBootstrap(ctx, s.Mach, s.Name)
	if err != nil {
		AddErrWorker(s.Mach, err, nil)
		return
	}
	argsOut := &A{Bootstrap: boot}

	// start connection-bootstrap machine
	res := boot.Mach.Add1(states.BootstrapStates.Start, nil)
	if res != am.Executed || boot.Mach.IsErr() {
		AddErrWorker(s.Mach, ErrWorkerConn, Pass(argsOut))
		return
	}

	// unblock
	go func() {
		// wait for bootstrap RPC to become ready
		err := amhelp.WaitForAll(ctx, s.ConnTimeout,
			boot.server.Mach.When1(ssrpc.ServerStates.RpcReady, nil))
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			AddErrWorker(s.Mach, err, Pass(argsOut))
			return
		}

		// next
		s.Mach.Add1(ssS.ForkingWorker, Pass(argsOut))
	}()
}

func (s *Supervisor) ForkingWorkerEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.Bootstrap != nil && a.Bootstrap.Addr() != "" &&
		s.workers.Count() < s.Max
}

func (s *Supervisor) ForkingWorkerState(e *am.Event) {
	s.Mach.Remove1(ssS.ForkingWorker, nil)
	ctx := s.Mach.NewStateCtx(ssS.Start)
	args := ParseArgs(e.Args)
	b := args.Bootstrap
	argsOut := &A{Bootstrap: b}

	// test forking, if provided
	if s.testFork != nil {
		// unblock
		go func() {
			if ctx.Err() != nil {
				return // expired
			}
			if err := s.testFork(args.Bootstrap.Addr()); err != nil {
				AddErrWorker(s.Mach, err, Pass(argsOut))
				return
			}
			// fake entry
			s.workers.Set(b.Addr(), newWorkerInfo(s, b, nil))

			// next
			s.Mach.Add1(ssS.AwaitingWorker, Pass(argsOut))
		}()

		// tests end here
		return
	}

	// prep for forking
	var cmdArgs []string
	if len(s.WorkerBin) > 1 {
		cmdArgs = s.WorkerBin[1:]
	}
	cmdArgs = slices.Concat(cmdArgs, []string{"-a", b.Addr()})
	s.log("forking worker %s %s", s.WorkerBin[0], cmdArgs)
	cmd := exec.CommandContext(ctx, s.WorkerBin[0], cmdArgs...)
	cmd.Env = os.Environ()
	s.workers.Set(b.Addr(), newWorkerInfo(s, b, cmd.Process))

	// read errors
	stderr, err := cmd.StderrPipe()
	if err != nil {
		AddErrWorker(s.Mach, err, Pass(argsOut))
		return
	}
	scanner := bufio.NewScanner(stderr)

	// fork the worker
	err = cmd.Start()
	if err != nil {
		AddErrWorker(s.Mach, err, Pass(argsOut))
		return
	}

	// monitor the fork
	go func() {
		var out string
		for scanner.Scan() {
			out += scanner.Text() + "\n"
		}

		// skip ctx expire, [cmd] already inherited it
		err := cmd.Wait()
		if err != nil {
			if out != "" {
				s.log("fork error: %s", out)
			}
			AddErrWorker(s.Mach, err, Pass(argsOut))

			return
		}
	}()

	// next
	s.Mach.Add1(ssS.AwaitingWorker, Pass(argsOut))
}

func (s *Supervisor) AwaitingWorkerEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.Bootstrap != nil && a.Bootstrap.Addr() != ""
}

func (s *Supervisor) AwaitingWorkerState(e *am.Event) {
	s.Mach.Remove1(ssS.AwaitingWorker, nil)
	ctx := s.Mach.NewStateCtx(ssS.Start)
	args := ParseArgs(e.Args)
	b := args.Bootstrap
	argsOut := &A{Bootstrap: b}

	// unblock
	go func() {
		// worker ok, dispose bootstrap
		defer b.Dispose()

		// wait for the worker to connect to bootstrap
		err := amhelp.WaitForAll(ctx, s.ConnTimeout,
			b.Mach.When1(ssB.WorkerAddr, nil))
		// bootstraps dispose themselves, which closes chans
		if ctx.Err() != nil || b.Mach.Disposed.Load() {
			return // expired
		}
		if err != nil {
			AddErrWorker(s.Mach, err, Pass(argsOut))
			return
		}

		// rpc to the worker
		// TODO panic timing err?
		workerAddr := b.WorkerArgs.Load().LocalAddr
		wrpc, err := rpc.NewClient(ctx, workerAddr, s.Name, s.workerStruct,
			s.workerSNames, &rpc.ClientOpts{Parent: s.Mach})
		if err != nil {
			AddErrWorker(s.Mach, err, Pass(argsOut))
			return
		}
		amhelp.MachDebugEnv(wrpc.Mach)

		// wait for client ready
		wrpc.Start()
		err = amhelp.WaitForErrAny(ctx, s.ConnTimeout, wrpc.Mach,
			wrpc.Mach.When1(ssC.Ready, ctx))
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			AddErrWorker(s.Mach, wrpc.Mach.Err(), Pass(argsOut))
			return
		}
		argsOut.Worker = wrpc

		// next
		s.Mach.Add1(ssS.WorkerForked, Pass(argsOut))
	}()
}

func (s *Supervisor) WorkerForkedEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.Bootstrap != nil && a.Bootstrap.Addr() != "" &&
		a.Worker != nil
}

func (s *Supervisor) WorkerForkedState(e *am.Event) {
	s.Mach.Remove1(ssS.WorkerForked, nil)
	args := ParseArgs(e.Args)
	b := args.Bootstrap
	addr := b.WorkerArgs.Load().LocalAddr
	wrpc := args.Worker
	argsOut := &A{
		LocalAddr: addr,
		Id:        wrpc.Mach.Id(),
	}

	// switch addresses and set
	info, ok := s.workers.Get(b.Addr())
	if !ok {
		AddErrWorker(s.Mach, ErrWorkerMissing, Pass(argsOut))
		return
	}
	info.mx.Lock()
	defer info.mx.Unlock()
	info.rpc = wrpc
	info.publicAddr = b.WorkerArgs.Load().PublicAddr
	info.localAddr = addr
	s.workers.Set(addr, info)
	s.workers.Remove(b.Addr())

	// custom pipe worker states
	err := errors.Join(
		ampipe.BindReady(wrpc.Mach, s.Mach, ssS.WorkerReady, ""),
		wrpc.Mach.BindHandlers(&struct {
			ExceptionState am.HandlerFinal
			ReadyEnd       am.HandlerFinal
		}{
			ExceptionState: func(e *am.Event) {
				AddErrWorker(s.Mach, wrpc.Mach.Err(), Pass(argsOut))
			},
			ReadyEnd: func(event *am.Event) {
				s.Mach.Add1(ssS.KillWorker, Pass(argsOut))
			},
		}),
	)
	if err != nil {
		AddErrWorker(s.Mach, err, Pass(argsOut))
		return
	}

	// ping and re-check the pool status
	wrpc.Worker.Add1(ssW.Healthcheck, nil)
	s.Mach.Add1(ssS.PoolReady, nil)
}

func (s *Supervisor) KillingWorkerEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.LocalAddr != ""
}

func (s *Supervisor) KillingWorkerState(e *am.Event) {
	s.Mach.Remove1(ssS.KillingWorker, nil)
	addr := ParseArgs(e.Args).LocalAddr
	argsOut := &A{LocalAddr: addr}

	// fake kill in tests
	if s.testKill != nil {
		if err := s.testKill(addr); err != nil {
			s.Mach.AddErr(err, Pass(argsOut))
			return
		}

		return
	}

	w, ok := s.workers.Get(addr)
	if !ok {
		s.log("worker %s not found", addr)
	}
	err := w.proc.Kill()
	if err != nil {
		s.Mach.AddErr(err, Pass(argsOut))
	}

	// TODO confirm port disconnect

	s.Mach.Add1(ssS.WorkerKilled, Pass(argsOut))
}

func (s *Supervisor) WorkerKilledEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.LocalAddr != ""
}

func (s *Supervisor) WorkerKilledState(e *am.Event) {
	s.Mach.Remove1(ssS.WorkerKilled, nil)

	args := ParseArgs(e.Args)
	s.workers.Remove(args.LocalAddr)

	// re-check the pool status
	s.Mach.Remove1(ssS.PoolReady, nil)
}

func (s *Supervisor) PoolReadyEnter(e *am.Event) bool {
	return len(s.ReadyWorkers()) >= s.Min
}

func (s *Supervisor) PoolReadyExit(e *am.Event) bool {
	return len(s.ReadyWorkers()) < s.Min
}

func (s *Supervisor) HeartbeatState(e *am.Event) {
	// TODO detect stuck NormalizingPool
	// TODO time limit heartbeat
	ctx := s.Mach.NewStateCtx(ssS.Heartbeat)

	// clear gone workers TODO check if binding is enough
	// for _, info := range s.StartedWorkers() {
	// 	w := info.rpc.Worker
	//
	// 	// was Ready, but not anymore
	// 	if w.Not1(ssW.Ready) && w.Tick(ssW.Ready) > 2 {
	// 		s.Mach.Add1(ssS.KillingWorker, Pass(&A{
	// 			LocalAddr: info.localAddr,
	// 		}))
	// 	}
	// }

	// ping ready workers
	ready := s.ReadyWorkers()

	// unblock
	go func() {
		defer s.Mach.Remove1(ssS.Heartbeat, nil)
		wg := sync.WaitGroup{}

		for _, info := range ready {
			w := info.rpc.Worker
			wg.Add(1)

			// parallel
			go func() {
				defer wg.Done()

				// 3 tries
				ok := false
				tick := info.rpc.Mach.Tick(ssW.Healthcheck)
				for i := 0; i < 3; i++ {

					// blocking rpc call
					info.rpc.Mach.Add1(ssW.Healthcheck, nil)
					if ctx.Err() != nil {
						return // expired
					}
					if tick < info.rpc.Mach.Tick(ssW.Healthcheck) {
						ok = true
						break
					}

					_ = amhelp.Wait(ctx, s.HealthcheckPause)
				}

				if !ok {
					AddErrWorker(s.Mach, ErrWorkerHealth, Pass(&A{
						LocalAddr: info.localAddr,
						Id:        w.ID,
					}))
				}
			}()
		}

		wg.Wait()

		// update states
		s.Mach.Add1(ssS.PoolReady, nil)
		s.Mach.Remove1(ssS.PoolReady, nil)
		if len(s.IdleWorkers()) > 0 {
			s.Mach.Add1(ssS.WorkersAvailable, nil)
		} else {
			s.Mach.Remove1(ssS.WorkersAvailable, nil)
		}
	}()
}

func (s *Supervisor) NormalizingPoolState(e *am.Event) {
	ctx := s.Mach.NewStateCtx(ssS.NormalizingPool)
	s.normalizeStart = time.Now()

	// unblock
	go func() {
		defer s.Mach.Add1(ssS.PoolNormalized, nil)
		ready := false

		// 5 rounds
		for i := 0; i < 5; i++ {

			// include warm workers, [i] is 0-based
			existing := s.workers.Count()
			for ii := existing; ii < s.Min+s.Warm && ii < s.Max; ii++ {
				s.Mach.Add1(ssS.ForkWorker, nil)
			}

			// wait and keep checking
			check := func() bool {
				// TODO dont flood the log
				ready = len(s.ReadyWorkers()) >= s.Min
				if ready {
					s.Mach.Add1(ssS.PoolReady, nil)
				}

				return !ready
			}
			// blocking call
			_ = amhelp.Interval(ctx, s.ConnTimeout, 50*time.Millisecond, check)
			if ready {
				break
			}

			// not ok, wait a bit
			if !amhelp.Wait(ctx, s.PoolPause) {
				return // expired
			}

			ready = s.Mach.Is1(ssS.PoolReady)
			if ready {
				break
			}

			s.log("failed to normalize pool, round %d", i)
		}

		if !ready {
			AddErrPoolStr(s.Mach, "failed to normalize pool", nil)
		}

		s.normalizeStart = time.Time{}
	}()
}

func (s *Supervisor) ProvideWorkerEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.Id != ""
}

func (s *Supervisor) ProvideWorkerState(e *am.Event) {
	s.Mach.Remove1(ssS.ProvideWorker, nil)
	args := ParseArgs(e.Args)
	ctx := s.Mach.NewStateCtx(ssS.Start)

	// unblock
	go func() {
		// find and Idle worker
		for _, info := range s.IdleWorkers() {

			// confirm with the worker
			res := amhelp.Add1Block(ctx, info.rpc.Worker, ssW.ServeClient, PassRpc(&A{
				Id: args.Id,
			}))
			if ctx.Err() != nil {
				return // expired
			}
			if res != am.Executed {
				s.log("worker %s rejected %s", info.rpc.Worker.ID, args.Id)
				continue
			}

			// send the addr to the client via RPC SendPayload
			s.Mach.Add1(ssS.ClientSendPayload, rpc.Pass(&rpc.A{
				Name: "worker_addr",
				Payload: &rpc.ArgsPayload{
					Name:   ssS.ProvideWorker,
					Source: s.Mach.Id(),
					Data:   info.publicAddr,
				},
			}))

			s.log("worker %s provided to %s", info.rpc.Worker.ID, args.Id)
			break
		}
	}()
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

func (s *Supervisor) Start(publicAddr string) {
	s.Mach.Add1(ssS.Start, Pass(&A{
		LocalAddr:  "localhost:0",
		PublicAddr: publicAddr,
	}))
}

func (s *Supervisor) Stop() {
	s.Mach.Remove1(ssS.Start, nil)
	s.Mach.Dispose()
}

// SetPool sets the pool parameters with defaults.
func (s *Supervisor) SetPool(min, max, warm, maxPerClient int) {
	if max < min {
		min = max
	}
	s.Min = min
	s.Max = max
	s.Warm = warm
	if maxPerClient == 0 {
		maxPerClient = s.Max
	}
	s.MaxClientWorkers = maxPerClient

	s.CheckPool()
}

// CheckPool tries to set pool as ready and normalizes it, if not.
func (s *Supervisor) CheckPool() bool {
	s.Mach.Add1(ssS.NormalizingPool, nil)
	s.Mach.Add1(ssS.PoolReady, nil)

	return s.Mach.Is1(ssS.PoolReady)
}

// AllWorkers returns workers (in any state).
func (s *Supervisor) AllWorkers() []*workerInfo {
	var ret []*workerInfo
	for item := range s.workers.IterBuffered() {
		ret = append(ret, item.Val)
	}

	return ret
}

// InitingWorkers returns workers being currently initialized.
func (s *Supervisor) InitingWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.AllWorkers() {
		info.mx.RLock()
		if info.rpc == nil {
			ret = append(ret, info)
		}
		info.mx.RUnlock()
	}

	return ret
}

// IdleWorkers returns Idle workers.
func (s *Supervisor) IdleWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.RpcWorkers() {
		info.mx.RLock()
		w := info.rpc.Worker
		if !info.hasErrs() && w.Is1(ssW.Idle) {
			ret = append(ret, info)
		}
		info.mx.RUnlock()
	}

	return ret
}

// RpcWorkers returns workers with an RPC connection.
func (s *Supervisor) RpcWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.AllWorkers() {
		info.mx.RLock()
		if info.rpc != nil && info.rpc.Worker != nil {
			ret = append(ret, info)
		}
		info.mx.RUnlock()
	}

	return ret
}

// BusyWorkers returns Busy workers.
func (s *Supervisor) BusyWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.RpcWorkers() {
		info.mx.RLock()
		w := info.rpc.Worker
		if !info.hasErrs() && info.rpc != nil &&
			w.Any1(sgW.WorkStatus...) && !w.Is1(ssW.Idle) {
			ret = append(ret, info)
		}
		info.mx.RUnlock()
	}

	return ret
}

// ReadyWorkers returns Ready workers.
func (s *Supervisor) ReadyWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.RpcWorkers() {
		info.mx.RLock()
		w := info.rpc.Worker
		if !info.hasErrs() && info.rpc != nil && w.Is1(ssW.Ready) {
			ret = append(ret, info)
		}
		info.mx.RUnlock()
	}

	return ret
}

func (s *Supervisor) Dispose() {
	s.Mach.Dispose()
}

func (s *Supervisor) log(msg string, args ...any) {
	if !s.LogEnabled {
		return
	}
	s.Mach.Log(msg, args...)
}

// newClientConn creates a new RPC server for a client.
// TODO keep one forked and bind immediately
func (s *Supervisor) newClientConn(
	num int, conn net.Conn,
) (*rpc.Server, error) {
	s.log("new client connection %d", num)
	ctx := s.Mach.NewStateCtx(ssS.Start)
	name := fmt.Sprintf("ns-pub-%d-%s", num, s.Name)

	opts := &rpc.ServerOpts{
		Parent:       s.Mach,
		PayloadState: ssS.ClientSendPayload,
	}
	rpcS, err := rpc.NewServer(ctx, s.PublicAddr, name, s.Mach, opts)
	if err != nil {
		return nil, err
	}
	amhelp.MachDebugEnv(rpcS.Mach)

	// set up
	rpcS.DeliveryTimeout = s.DeliveryTimeout
	err = rpc.BindServerMulti(rpcS.Mach, s.Mach, ssS.PublicRpcReady,
		ssS.ClientConnected, ssS.ClientDisconnected)
	if err != nil {
		return nil, err
	}

	// store
	ok := s.Mach.Eval("newClientConn", func() {
		// TODO check ctx
		s.PublicRpcs[conn.RemoteAddr().String()] = rpcS
	}, ctx)
	if !ok {
		return nil, am.ErrHandlerTimeout
	}

	s.log("new client connection %d ready", num)
	return rpcS, nil
}
