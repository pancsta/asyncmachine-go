package node

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

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
	// TODO group under Supervisor.Times struct

	// ConnTimeout is the time to wait for an outbound connection to be
	// established. Default is 5s.
	ConnTimeout time.Duration
	// DeliveryTimeout is a timeout for RPC delivery.
	DeliveryTimeout time.Duration
	// OpTimeout is the default timeout for operations (eg getters).
	OpTimeout time.Duration
	// PoolPause is the time to wait between normalizing the pool. Default is 5s.
	PoolPause time.Duration
	// HealthcheckPause is the time between trying to get a Healtcheck response
	// from a worker.
	HealthcheckPause time.Duration
	// Heartbeat is the frequency of the Heartbeat state, which normalized the
	// pool and checks workers. Default 1m.
	Heartbeat time.Duration
	// WorkerCheckInterval defines how often to pull a worker's state. Default 1s.
	WorkerCheckInterval time.Duration

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

	// workers is a map of local RPC addresses to workerInfo data.
	workers map[string]*workerInfo

	// workerSNames is a list of states for the worker.
	workerSNames am.S
	// workerStruct is the struct for the worker.
	workerStruct am.Schema

	// in-memory workers

	TestFork func(string) error
	TestKill func(string) error

	normalizeStart time.Time

	// self removing multi handlers

	WorkerReadyState       am.HandlerFinal
	WorkerGoneState        am.HandlerFinal
	KillWorkerState        am.HandlerFinal
	ClientSendPayloadState am.HandlerFinal
	SuperSendPayloadState  am.HandlerFinal
	HealthcheckState       am.HandlerFinal
}

// NewSupervisor initializes and returns a new Supervisor instance with
// specified context, worker attributes, and options.
func NewSupervisor(
	ctx context.Context, workerKind string, workerBin []string,
	workerStruct am.Schema, workerSNames am.S, opts *SupervisorOpts,
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

	hostname := utils.Hostname()
	if len(hostname) > 15 {
		hostname = hostname[:15]
	}
	name := fmt.Sprintf("%s-%s-%s-%d", workerKind, hostname,
		time.Now().Format("150405"), opts.InstanceNum)

	s := &Supervisor{
		WorkerKind: workerKind,
		WorkerBin:  workerBin,
		Name:       name,
		LogEnabled: os.Getenv(EnvAmNodeLogSupervisor) != "",

		// defaults

		Max:                 10,
		Min:                 2,
		Warm:                5,
		MaxClientWorkers:    10,
		ConnTimeout:         5 * time.Second,
		DeliveryTimeout:     5 * time.Second,
		OpTimeout:           time.Second,
		Heartbeat:           1 * time.Minute,
		WorkerCheckInterval: 1 * time.Second,
		PoolPause:           5 * time.Second,
		HealthcheckPause:    500 * time.Millisecond,
		WorkerErrTtl:        10 * time.Minute,
		WorkerErrRecent:     1 * time.Minute,
		WorkerErrKill:       3,

		// rpc

		PublicRpcs: make(map[string]*rpc.Server),

		// internals

		workerSNames: workerSNames,
		workerStruct: workerStruct,
		workers:      map[string]*workerInfo{},
	}

	if amhelp.IsDebug() {
		// increase only the timeouts used by [context.WithTimeout] or [time.Sleep]
		// directly
		s.DeliveryTimeout = 10 * s.DeliveryTimeout
		s.OpTimeout = 10 * s.OpTimeout
	}

	mach, err := am.NewCommon(ctx, "ns-"+s.Name, states.SupervisorSchema,
		ssS.Names(), s, opts.Parent, &am.Opts{Tags: []string{
			"node-supervisor", "kind:" + workerKind,
			"instance:" + strconv.Itoa(opts.InstanceNum),
			"host:" + utils.Hostname(),
		}})
	if err != nil {
		return nil, err
	}

	mach.SetLogArgs(LogArgs)
	s.Mach = mach
	amhelp.MachDebugEnv(mach)
	mach.AddBreakpoint(am.S{ssS.ErrWorker}, nil)

	// self removing multi handlers
	s.WorkerReadyState = amhelp.RemoveMulti(mach, ssS.WorkerReady)
	s.WorkerGoneState = amhelp.RemoveMulti(mach, ssS.WorkerGone)
	s.KillWorkerState = amhelp.RemoveMulti(mach, ssS.KillWorker)
	s.ClientSendPayloadState = amhelp.RemoveMulti(mach, ssS.ClientSendPayload)
	s.SuperSendPayloadState = amhelp.RemoveMulti(mach, ssS.SuperSendPayload)
	s.HealthcheckState = amhelp.RemoveMulti(mach, ssS.Healthcheck)

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
	// remove err as handled, if the last err
	if !s.Mach.WillBe1(ssS.Exception) {
		s.Mach.Remove(am.S{ssS.ErrWorker, ssS.Exception}, nil)
	}
	err := am.ParseArgs(e.Args).Err
	args := ParseArgs(e.Args)
	w := s.workers[args.LocalAddr]

	// possibly kill the worker
	if !errors.Is(err, ErrWorkerKill) && w != nil {
		err1 := w.errs.Add(utils.RandId(0), err, 0)
		err2 := w.errsRecent.Add(utils.RandId(0), err, 0)
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
	s.PublicMux.Addr = s.PublicAddr
	if err != nil {
		_ = AddErrRpc(s.Mach, err, nil)
		return
	}

	// local rpc TODO mux
	opts := &rpc.ServerOpts{
		Parent:       s.Mach,
		PayloadState: ssS.SuperSendPayload,
	}
	s.LocalRpc, err = rpc.NewServer(ctx, s.LocalAddr, "ns-loc-"+s.Name, s.Mach,
		opts)
	if err != nil {
		_ = AddErrRpc(s.Mach, err, nil)
		return
	}
	s.LocalRpc.DeliveryTimeout = s.DeliveryTimeout
	err = rpc.BindServerMulti(s.LocalRpc.Mach, s.Mach, ssS.LocalRpcReady,
		ssS.SuperConnected, ssS.SuperDisconnected)
	if err != nil {
		_ = AddErrRpc(s.Mach, err, nil)
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
			_ = AddErrRpc(s.Mach, err, nil)
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
	return len(s.workers) < s.Max
}

func (s *Supervisor) ForkWorkerState(e *am.Event) {
	s.Mach.Remove1(ssS.ForkWorker, nil)
	ctx := s.Mach.NewStateCtx(ssS.Start)

	// init bootstrap machine
	boot, err := newBootstrap(ctx, s)
	if err != nil {
		_ = AddErrWorker(nil, s.Mach, err, nil)
		return
	}
	argsOut := &A{Bootstrap: boot}

	// start connection-bootstrap machine
	res := boot.Mach.Add1(states.BootstrapStates.Start, nil)
	if res != am.Executed || boot.Mach.IsErr() {
		_ = AddErrWorker(e, s.Mach, ErrWorkerConn, Pass(argsOut))
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
			_ = AddErrWorker(e, s.Mach, err, Pass(argsOut))
			return
		}

		// next
		s.Mach.Add1(ssS.ForkingWorker, Pass(argsOut))
	}()
}

func (s *Supervisor) ForkingWorkerEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a.Bootstrap != nil && a.Bootstrap.Addr() != "" &&
		len(s.workers) < s.Max
}

func (s *Supervisor) ForkingWorkerState(e *am.Event) {
	s.Mach.Remove1(ssS.ForkingWorker, nil)
	ctx := s.Mach.NewStateCtx(ssS.Start)
	args := ParseArgs(e.Args)
	boot := args.Bootstrap
	bootAddr := boot.Addr()
	argsOut := &A{Bootstrap: boot}

	// test forking, if provided
	if s.TestFork != nil {
		// unblock
		go func() {
			if ctx.Err() != nil {
				return // expired
			}
			if err := s.TestFork(bootAddr); err != nil {
				_ = AddErrWorker(e, s.Mach, err, Pass(argsOut))
				return
			}
			// fake entry
			s.Mach.Add1(ssS.SetWorker, Pass(&A{
				WorkerAddr: bootAddr,
				WorkerInfo: newWorkerInfo(s, nil),
			}))
		}()

		// tests end here
		return
	}

	// prep for forking
	var cmdArgs []string
	if len(s.WorkerBin) > 1 {
		cmdArgs = s.WorkerBin[1:]
	}
	// TODO custom param name -a
	// TODO store PIDs of workers, clean up old PIDs
	cmdArgs = slices.Concat(cmdArgs, []string{"-a", bootAddr})
	s.log("forking worker %s %s", s.WorkerBin[0], cmdArgs)
	cmd := exec.CommandContext(ctx, s.WorkerBin[0], cmdArgs...)
	cmd.Env = os.Environ()
	s.Mach.Add1(ssS.SetWorker, Pass(&A{
		WorkerAddr: bootAddr,
		WorkerInfo: newWorkerInfo(s, cmd.Process),
	}))

	// read errors
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = AddErrWorker(e, s.Mach, err, Pass(argsOut))
		return
	}
	scanner := bufio.NewScanner(stderr)

	// fork the worker
	err = cmd.Start()
	if err != nil {
		_ = AddErrWorker(e, s.Mach, err, Pass(argsOut))
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
			_ = AddErrWorker(e, s.Mach, err, Pass(argsOut))

			return
		}
	}()
}

func (s *Supervisor) WorkerConnectedEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.LocalAddr != ""
}

func (s *Supervisor) WorkerConnectedState(e *am.Event) {
	s.Mach.Remove1(ssS.WorkerConnected, nil)

	ctx := s.Mach.NewStateCtx(ssS.Start)
	args := ParseArgs(e.Args)
	// copy args
	argsOut := *args

	// unblock
	go func() {
		// bootstraps dispose themselves, which closes chans
		if ctx.Err() != nil {
			return // expired
		}

		// rpc to the worker
		workerAddr := args.LocalAddr
		_, port, err := net.SplitHostPort(workerAddr)
		if err != nil {
			_ = AddErrWorker(e, s.Mach, err, Pass(&argsOut))
			return
		}
		wrpc, err := rpc.NewClient(ctx, workerAddr, s.Name+"-"+port,
			s.workerStruct, s.workerSNames, &rpc.ClientOpts{Parent: s.Mach})
		if err != nil {
			_ = AddErrWorker(e, s.Mach, err, Pass(&argsOut))
			return
		}

		// wait for client ready
		wrpc.Start()
		err = amhelp.WaitForErrAny(ctx, s.ConnTimeout, wrpc.Mach,
			wrpc.Mach.When1(ssC.Ready, ctx))
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			_ = AddErrWorker(e, s.Mach, wrpc.Mach.Err(), Pass(&argsOut))
			return
		}

		// next
		argsOut.WorkerRpc = wrpc
		s.Mach.Add1(ssS.WorkerForked, Pass(&argsOut))
	}()
}

func (s *Supervisor) WorkerForkedEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.LocalAddr != "" && a.WorkerRpc != nil
}

func (s *Supervisor) WorkerForkedState(e *am.Event) {
	s.Mach.Remove1(ssS.WorkerForked, nil)

	args := ParseArgs(e.Args)
	addr := args.LocalAddr
	bootAddr := args.BootAddr
	wrpc := args.WorkerRpc
	argsOut := &A{
		LocalAddr: addr,
		Id:        wrpc.Mach.Id(),
	}

	// switch addresses (boot -> local) and update the worker map
	info, ok := s.workers[bootAddr]
	if !ok {
		_ = AddErrWorker(e, s.Mach, ErrWorkerMissing, Pass(argsOut))
		return
	}
	delete(s.workers, bootAddr)

	// update worker info
	info.rpc = wrpc
	info.w = wrpc.Worker
	info.publicAddr = args.PublicAddr
	info.localAddr = addr
	s.workers[addr] = info

	// custom pipe worker states
	err := errors.Join(
		ampipe.BindReady(wrpc.Mach, s.Mach, ssS.WorkerReady, ""),
		wrpc.Mach.BindHandlers(&struct {
			ExceptionState am.HandlerFinal
			ReadyEnd       am.HandlerFinal
		}{
			ExceptionState: func(e *am.Event) {
				_ = AddErrWorker(e, s.Mach, wrpc.Mach.Err(), Pass(argsOut))
			},
			ReadyEnd: func(e *am.Event) {
				// TODO why this kills the workers? which Ready ends?
				s.Mach.EvAdd1(e, ssS.KillWorker, Pass(argsOut))
			},
		}),
	)
	if err != nil {
		_ = AddErrWorker(e, s.Mach, err, Pass(argsOut))
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
	if s.TestKill != nil {
		if err := s.TestKill(addr); err != nil {
			s.Mach.AddErr(err, Pass(argsOut))
			return
		}

		return
	}

	w, ok := s.workers[addr]
	if !ok {
		_ = AddErrWorker(e, s.Mach, ErrWorkerMissing, Pass(argsOut))
		return
	}
	err := w.proc.Kill()
	if err != nil {
		_ = AddErrWorker(e, s.Mach, err, Pass(argsOut))
		return
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
	delete(s.workers, args.LocalAddr)

	// re-check the pool status
	s.Mach.Remove1(ssS.PoolReady, nil)
}

func (s *Supervisor) PoolReadyEnter(e *am.Event) bool {
	// TODO timeouts in tests
	return len(s.readyWorkers()) >= s.min()
}

func (s *Supervisor) PoolReadyExit(e *am.Event) bool {
	return len(s.readyWorkers()) < s.min()
}

func (s *Supervisor) HeartbeatState(e *am.Event) {
	// TODO detect stuck NormalizingPool
	// TODO time limit heartbeat
	// TODO test
	ctx := s.Mach.NewStateCtx(ssS.Heartbeat)

	// clear gone workers TODO check if binding is enough
	// for _, info := range s.StartedWorkers() {
	// 	w := info.rpc.Worker
	//
	// 	// was Ready, but not anymore
	// 	if w.Not1(ssW.Ready) && w.Tick(ssW.Ready) > 2 {
	// 		s.Mach.Add(ssS.KillingWorker, Pass(&A{
	// 			LocalAddr: info.localAddr,
	// 		}))
	// 	}
	// }

	// get ready workers, or abort
	ready := s.readyWorkers()
	if ready == nil {
		s.Mach.Remove1(ssS.Heartbeat, nil)
		return
	}

	// unblock
	go func() {
		defer s.Mach.Remove1(ssS.Heartbeat, nil)

		// parallel group
		eg, parCtx := errgroup.WithContext(ctx)
		for _, info := range ready {
			eg.Go(func() error {
				// 3 tries per worker
				ok := false
				tick := info.w.Tick(ssW.Healthcheck)
				for i := 0; i < 3; i++ {

					// blocking RPC call
					info.w.Add1(ssW.Healthcheck, nil)
					if ctx.Err() != nil {
						return ctx.Err() // expired
					}
					if tick < info.w.Tick(ssW.Healthcheck) {
						ok = true
						break
					}
					_ = amhelp.Wait(ctx, s.HealthcheckPause)
				}

				if !ok {
					return AddErrWorker(e, s.Mach, ErrWorkerHealth, Pass(&A{
						LocalAddr: info.localAddr,
						Id:        info.w.Id(),
					}))
				}

				return nil
			})
		}

		// dont wait here...
		go eg.Wait() //nolint:errcheck
		// ...wait with a timeout instead
		err := amhelp.WaitForAll(ctx, s.ConnTimeout, parCtx.Done())
		if err != nil {
			_ = AddErrPool(s.Mach, fmt.Errorf("%w: %w", ErrHeartbeat, err), nil)
			return
		}

		// update states PoolReady (negotiation lets the right one in)
		s.Mach.Add1(ssS.PoolReady, nil)
		s.Mach.Remove1(ssS.PoolReady, nil)

		// update WorkersAvailable
		idle, err := s.Workers(ctx, StateIdle)
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			_ = AddErrWorker(e, s.Mach, err, nil)
			return
		}
		if len(idle) > 0 {
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
		if ctx.Err() != nil {
			return // expired
		}

		defer s.Mach.Add1(ssS.PoolNormalized, nil)
		ready := false

		// 5 rounds
		for i := 0; i < 5; i++ {

			// list workers
			existing, err := s.Workers(ctx, "")
			if ctx.Err() != nil {
				return // expired
			}
			if err != nil {
				continue
			}
			// fork missing ones (include warm workers)
			for ii := len(existing); ii < s.min()+s.Warm && ii < s.Max; ii++ {
				s.Mach.Add1(ssS.ForkWorker, nil)
			}

			// wait and keep checking
			check := func() bool {
				if ctx.Err() != nil {
					return false // expired
				}

				ready, _ := s.Workers(ctx, StateReady)
				if ctx.Err() != nil {
					return false // expired
				}
				if len(ready) >= s.min() {
					s.Mach.Add1(ssS.PoolReady, nil)
					return false
				}

				// continue
				return true
			}
			// blocking call
			_ = amhelp.Interval(ctx, s.ConnTimeout, s.WorkerCheckInterval, check)
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
			_ = AddErrPoolStr(s.Mach, "failed to normalize pool", nil)
		}

		s.normalizeStart = time.Time{}
	}()
}

func (s *Supervisor) ProvideWorkerEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a.WorkerRpcId != "" && a.SuperRpcId != ""
}

func (s *Supervisor) ProvideWorkerState(e *am.Event) {
	s.Mach.Remove1(ssS.ProvideWorker, nil)
	args := ParseArgs(e.Args)
	ctx := s.Mach.NewStateCtx(ssS.Start)
	idle := s.idleWorkers()

	// unblock
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// find an idle worker
		for _, info := range idle {

			// confirm with the worker
			res := amhelp.Add1Block(ctx, info.rpc.Worker, ssW.ServeClient, PassRpc(&A{
				Id: args.WorkerRpcId,
			}))
			if ctx.Err() != nil {
				return // expired
			}
			if res != am.Executed {
				s.log("worker %s rejected %s", info.rpc.Worker.Id(), args.WorkerRpcId)
				continue
			}

			// send the addr to the client via RPC SendPayload
			s.Mach.Add1(ssS.ClientSendPayload, rpc.PassRpc(&rpc.A{
				Name: "worker_addr",
				Payload: &rpc.ArgsPayload{
					Name:        ssS.ProvideWorker,
					Source:      s.Mach.Id(),
					Destination: args.SuperRpcId,
					Data:        info.publicAddr,
				},
			}))

			s.log("worker %s provided to %s", info.rpc.Worker.Id(), args.SuperRpcId)
			break
		}
	}()
}

func (s *Supervisor) ListWorkersEnter(e *am.Event) bool {
	ch := ParseArgs(e.Args).WorkersCh
	// require a buffered channel
	return ch != nil && cap(ch) > 0
}

func (s *Supervisor) ListWorkersState(e *am.Event) {
	s.Mach.Remove1(ssS.ListWorkers, nil)
	args := ParseArgs(e.Args)

	switch args.WorkerState {
	case StateReady:
		args.WorkersCh <- s.readyWorkers()
	case StateIniting:
		args.WorkersCh <- s.initingWorkers()
	case StateBusy:
		args.WorkersCh <- s.busyWorkers()
	case StateIdle:
		args.WorkersCh <- s.idleWorkers()
	default:
		args.WorkersCh <- slices.Collect(maps.Values(s.workers))
	}
}

func (s *Supervisor) SetWorkerEnter(e *am.Event) bool {
	return ParseArgs(e.Args).WorkerAddr != ""
}

func (s *Supervisor) SetWorkerState(e *am.Event) {
	s.Mach.Remove1(ssS.SetWorker, nil)
	args := ParseArgs(e.Args)
	addr := args.WorkerAddr

	if args.WorkerInfo != nil {
		s.workers[addr] = args.WorkerInfo
	} else {
		delete(s.workers, addr)
	}
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

// Workers returns a list of workers in a desired state. If [ctx] expires, it
// will reutrn nil, nil.
func (s *Supervisor) Workers(
	ctx context.Context, state WorkerState,
) ([]*workerInfo, error) {
	wCh := make(chan []*workerInfo, 1)
	res := s.Mach.Add1(ssS.ListWorkers, Pass(&A{
		WorkersCh:   wCh,
		WorkerState: state,
	}))
	if res == am.Canceled {
		return nil, fmt.Errorf("listing workers: %w", am.ErrCanceled)
	}

	select {
	case <-ctx.Done():
		return nil, nil // expired
	case <-time.After(s.OpTimeout):
		return nil, fmt.Errorf("listing workers: %w", am.ErrTimeout)
	case workers := <-wCh:
		return workers, nil
	}
}

func (s *Supervisor) Dispose() {
	s.Mach.Dispose()
}

// InitingWorkers returns workers being currently initialized. Only call in
// handlers or use the [ListWorkers] state.
func (s *Supervisor) initingWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.workers {
		if info.rpc == nil {
			ret = append(ret, info)
		}
	}

	return ret
}

// RpcWorkers returns workers with an RPC connection. Only call in handlers or
// use the [ListWorkers] state.
func (s *Supervisor) rpcWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.workers {
		if info.rpc != nil && info.rpc.Worker != nil {
			ret = append(ret, info)
		}
	}

	return ret
}

// IdleWorkers returns Idle workers. Only call in handlers or use the
// [ListWorkers] state.
func (s *Supervisor) idleWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.rpcWorkers() {
		w := info.rpc.Worker
		if !info.hasErrs() && w.Is1(ssW.Idle) {
			ret = append(ret, info)
		}
	}

	return ret
}

// BusyWorkers returns Busy workers. Only call in handlers or use the
// [ListWorkers] state.
func (s *Supervisor) busyWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.rpcWorkers() {
		w := info.rpc.Worker
		if !info.hasErrs() && info.rpc != nil &&
			w.Any1(sgW.WorkStatus...) && !w.Is1(ssW.Idle) {
			ret = append(ret, info)
		}
	}

	return ret
}

// ReadyWorkers returns Ready workers. Only call in handlers or use the
// [ListWorkers] state.
func (s *Supervisor) readyWorkers() []*workerInfo {
	var ret []*workerInfo
	for _, info := range s.rpcWorkers() {
		w := info.rpc.Worker
		if !info.hasErrs() && info.rpc != nil && w.Is1(ssW.Ready) {
			ret = append(ret, info)
		}
	}

	return ret
}

func (s *Supervisor) log(msg string, args ...any) {
	if !s.LogEnabled {
		return
	}
	s.Mach.Log(msg, args...)
}

func (s *Supervisor) min() int {
	if s.Min > s.Max {
		return s.Max
	}
	return s.Min
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
		Parent:       s.PublicMux.Mach,
		PayloadState: ssS.ClientSendPayload,
	}
	rpcS, err := rpc.NewServer(ctx, s.PublicAddr, name, s.Mach, opts)
	if err != nil {
		return nil, err
	}

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
