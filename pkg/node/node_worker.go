package node

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/node/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

var (
	ssW = states.WorkerStates
	sgW = states.WorkerGroups
)

type Worker struct {
	*am.ExceptionHandler
	Mach *am.Machine

	Name string
	Kind string
	// AcceptClient is the ID of a client, passed by the supervisor. Worker should
	// only accept connections from this client.
	AcceptClient string

	// ConnTimeout is the time to wait for an outbound connection to be
	// established.
	ConnTimeout     time.Duration
	DeliveryTimeout time.Duration

	// BootAddr is the address of the bootstrap machine.
	BootAddr string
	// BootRpc is the RPC client connection to bootstrap machine, which passes
	// connection info to the Supervisor.
	BootRpc *rpc.Client

	// LocalAddr is the address of the local RPC server.
	LocalAddr string
	// LocalRpc is the local RPC server, used by the Supervisor to connect.
	LocalRpc *rpc.Server

	// PublicAddr is the address of the public RPC server.
	PublicAddr string
	// PublicRpc is the public RPC server, used by the Client to connect.
	PublicRpc *rpc.Server
}

// NewWorker initializes a new Worker instance and returns it, or an error if
// validation fails.
func NewWorker(ctx context.Context, kind string, workerStruct am.Struct,
	stateNames am.S, opts *WorkerOpts,
) (*Worker, error) {
	// validate
	if kind == "" {
		return nil, errors.New("worker: workerSNames required")
	}
	if stateNames == nil {
		return nil, errors.New("worker: workerSNames required")
	}
	if workerStruct == nil {
		return nil, errors.New("worker: workerStruct required")
	}
	if opts == nil {
		opts = &WorkerOpts{}
	}

	name := fmt.Sprintf("%s-%s-%s", kind, utils.Hostname(), utils.RandID(6))

	w := &Worker{
		ConnTimeout:     5 * time.Second,
		DeliveryTimeout: 5 * time.Second,
		Name:            name,
		Kind:            kind,
	}

	if amhelp.IsDebug() {
		// increase timeouts using context.WithTimeout directly
		w.DeliveryTimeout = 10 * w.DeliveryTimeout
	}

	mach, err := am.NewCommon(ctx, "nw-"+w.Name, workerStruct, stateNames, w,
		opts.Parent, nil)
	if err != nil {
		return nil, err
	}

	mach.SetLogArgs(LogArgs)
	w.Mach = mach
	amhelp.MachDebugEnv(mach)

	// check base states
	err = amhelp.Implements(mach.StateNames(), ssW.Names())
	if err != nil {
		err := fmt.Errorf(
			"client has to implement am/node/states/WorkerStates: %w", err)
		return nil, err
	}

	return w, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (w *Worker) ErrNetworkState(e *am.Event) {
	// TODO handle RPC errors
}

func (w *Worker) StartEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a.LocalAddr != ""
}

func (w *Worker) StartState(e *am.Event) {
	var err error
	ctx := w.Mach.NewStateCtx(ssW.Start)
	args := ParseArgs(e.Args)
	w.BootAddr = args.LocalAddr

	// local RPC
	opts := &rpc.ServerOpts{
		PayloadState: ssW.SuperSendPayload,
		Parent:       w.Mach,
	}
	w.LocalRpc, err = rpc.NewServer(ctx, "localhost:0", "nw-loc-"+w.Name, w.Mach,
		opts)
	if err != nil {
		AddErrRpc(w.Mach, err, nil)
		return
	}
	amhelp.MachDebugEnv(w.LocalRpc.Mach)
	w.LocalRpc.DeliveryTimeout = w.DeliveryTimeout
	err = errors.Join(
		rpc.BindServer(w.LocalRpc.Mach, w.Mach, ssW.LocalRpcReady,
			ssW.SuperConnected),
		ampipe.BindErr(w.LocalRpc.Mach, w.Mach, ssW.ErrSupervisor))
	if err != nil {
		w.Mach.AddErr(err, nil)
		return
	}

	// public RPC
	opts = &rpc.ServerOpts{
		PayloadState: ssW.ClientSendPayload,
		Parent:       w.Mach,
	}
	w.PublicRpc, err = rpc.NewServer(ctx, ":0", "nw-pub-"+w.Name, w.Mach, opts)
	if err != nil {
		AddErrRpc(w.Mach, err, nil)
		return
	}
	amhelp.MachDebugEnv(w.PublicRpc.Mach)
	w.PublicRpc.DeliveryTimeout = w.DeliveryTimeout
	err = errors.Join(
		rpc.BindServer(w.PublicRpc.Mach, w.Mach, ssW.PublicRpcReady,
			ssW.ClientConnected),
		ampipe.BindErr(w.PublicRpc.Mach, w.Mach, ssW.ErrClient))
	if err != nil {
		w.Mach.AddErr(err, nil)
		return
	}

	// start
	if w.LocalRpc.Start() != am.Executed {
		AddErrRpc(w.Mach, nil, nil)
		return
	}
	if w.PublicRpc.Start() != am.Executed {
		AddErrRpc(w.Mach, nil, nil)
		return
	}
}

func (w *Worker) StartEnd(e *am.Event) {
	args := ParseArgs(e.Args)

	if w.PublicRpc != nil {
		w.PublicRpc.Stop(true)
	}
	w.PublicRpc = nil

	if w.LocalRpc != nil {
		w.LocalRpc.Stop(true)
	}
	w.LocalRpc = nil

	if w.BootRpc != nil {
		w.BootRpc.Stop(context.TODO(), true)
	}
	w.BootRpc = nil

	if args.Dispose {
		w.Mach.Dispose()
	}
}

func (w *Worker) LocalRpcReadyState(e *am.Event) {
	// get the local addr
	w.LocalAddr = w.LocalRpc.Addr
}

func (w *Worker) PublicRpcReadyState(e *am.Event) {
	// get the local addr
	w.PublicAddr = w.PublicRpc.Addr
}

func (w *Worker) RpcReadyState(e *am.Event) {
	var err error
	ctx := w.Mach.NewStateCtx(ssW.LocalRpcReady)
	w.Mach.Add1(ssW.Ready, nil)

	// connect to the bootstrap machine
	opts := &rpc.ClientOpts{Parent: w.Mach}
	w.BootRpc, err = rpc.NewClient(ctx, w.BootAddr, "nw-"+w.Name,
		states.BootstrapStruct, states.BootstrapStates.Names(), opts)
	if err != nil {
		AddErrRpc(w.Mach, err, nil)
		return
	}
	err = ampipe.BindErr(w.BootRpc.Mach, w.Mach, "")
	if err != nil {
		AddErrRpc(w.Mach, err, nil)
		return
	}
	amhelp.MachDebugEnv(w.BootRpc.Mach)
	w.BootRpc.Start()

	// unblock
	go func() {
		// wait for the bootstrap client to be ready
		err := amhelp.WaitForAll(ctx, w.ConnTimeout,
			w.BootRpc.Mach.When1(ssrpc.ClientStates.Ready, nil))
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			AddErrRpc(w.Mach, err, nil)
			return
		}

		// pass the local port to [bootstrap.WorkerAddState] via RPC
		w.BootRpc.Worker.Add1(ssB.WorkerAddr, PassRpc(&A{
			LocalAddr:  w.LocalAddr,
			PublicAddr: w.PublicAddr,
			Id:         w.Mach.Id(),
		}))
		// dispose
		w.BootRpc.Stop(context.TODO(), true)
		w.BootRpc = nil
	}()
}

func (w *Worker) HealthcheckState(e *am.Event) {
	w.Mach.Remove1(ssW.Healthcheck, nil)
}

func (w *Worker) ServeClientEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.Id != ""
}

func (w *Worker) ServeClientState(e *am.Event) {
	args := ParseArgs(e.Args)
	w.AcceptClient = args.Id
	w.PublicRpc.AllowId = w.AcceptClient
}

func (w *Worker) SendPayloadEnter(e *am.Event) bool {
	// use SuperSendPayload and ClientSendPayload instead
	return false
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

// Start initiates the worker state machine with the given local address.
func (w *Worker) Start(localAddr string) am.Result {
	return w.Mach.Add1(ssW.Start, Pass(&A{LocalAddr: localAddr}))
}

// Stop halts the worker's state machine and optionally disposes of its
// resources based on the dispose flag.
func (w *Worker) Stop(dispose bool) {
	w.Mach.Remove1(ssW.Start, Pass(&A{Dispose: dispose}))
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

type WorkerOpts struct {
	// Parent is a parent state machine for a new Worker state machine. See
	// [am.Opts].
	Parent am.Api
}
