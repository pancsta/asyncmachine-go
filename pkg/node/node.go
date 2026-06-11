// Package node provides distributed worker pools with supervisors.
package node

import (
	"encoding/gob"
	"errors"
	"fmt"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
)

const (
	// EnvAmNodeLogSupervisor enables extra logging for node supervisor.
	EnvAmNodeLogSupervisor = "AM_NODE_LOG_SUPERVISOR"
	// EnvAmNodeLogClient enables extra logging for node client.
	EnvAmNodeLogClient = "AM_NODE_LOG_CLIENT"
)

// states of a worker
type WorkerState string

const (
	StateIniting WorkerState = "initing"
	StateRpc     WorkerState = "rpc"
	StateIdle    WorkerState = "idle"
	StateBusy    WorkerState = "busy"
	StateReady   WorkerState = "ready"
)

var Pass = am.Pass

// ///// ///// /////

// ///// ERRORS

// ///// ///// /////

// sentinel errors

var (
	ErrWorker        = errors.New("worker error")
	ErrWorkerMissing = errors.New("worker missing")
	ErrWorkerHealth  = errors.New("worker failed healthcheck")
	ErrWorkerConn    = errors.New("error starting connection")
	ErrWorkerKill    = errors.New("error killing worker")
	ErrPool          = errors.New("pool error")
	ErrHeartbeat     = errors.New("heartbeat failed")
	ErrRpc           = errors.New("rpc error")
)

// error mutations

// AddErrWorker wraps an error in the ErrWorker sentinel and adds to a machine.
func AddErrWorker(event *am.Event, mach *am.Machine, err error, args am.A) am.Result {
	err = fmt.Errorf("%w: %w", ErrWorker, err)
	return mach.EvAddErrState(event, ssS.ErrWorker, err, args)
}

// AddErrWorkerStr wraps a msg in the ErrWorker sentinel and adds to a machine.
// TODO add event param
func AddErrWorkerStr(event *am.Event, mach *am.Machine, msg string, args am.A) am.Result {
	err := fmt.Errorf("%w: %s", ErrWorker, msg)
	return mach.EvAddErrState(event, ssS.ErrWorker, err, args)
}

// AddErrPool wraps an error in the ErrPool sentinel and adds to a machine.
// TODO add event param
func AddErrPool(event *am.Event, mach *am.Machine, err error, args am.A) am.Result {
	wrappedErr := fmt.Errorf("%w: %w", ErrPool, err)
	return mach.EvAddErrState(event, ssS.ErrPool, wrappedErr, args)
}

// AddErrPoolStr wraps a msg in the ErrPool sentinel and adds to a machine.
// TODO add event param
func AddErrPoolStr(event *am.Event, mach *am.Machine, msg string, args am.A) am.Result {
	err := fmt.Errorf("%w: %s", ErrPool, msg)
	return mach.EvAddErrState(event, ssS.ErrPool, err, args)
}

// AddErrRpc wraps an error in the ErrRpc sentinel and adds to a machine.
// TODO add event param
func AddErrRpc(event *am.Event, mach *am.Machine, err error, args am.A) am.Result {
	wrappedErr := fmt.Errorf("%w: %w", ErrRpc, err)
	return mach.EvAddErrState(event, ssS.ErrNetwork, wrappedErr, args)
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "am_node"

type Args struct {
	am.ArgsBase `json:"-"`
}

func (Args) ArgsPrefix() string {
	return APrefix
}

// ----- TODO per-state args

// A is a struct for node arguments. It's a typesafe alternative to [am.A].
type A struct {
	Args `json:"-"`

	// Id is a machine ID.
	Id string `log:"id"`
	// PublicAddr is the public address of a Supervisor or WorkerRpc.
	PublicAddr string `log:"public_addr"`
	// LocalAddr is the public address of a Supervisor or WorkerRpc.
	LocalAddr string `log:"local_addr"`
	// BootAddr is the local address of the Bootstrap machine.
	BootAddr string `log:"boot_addr"`
	// NodesList is a list of available nodes (supervisors' public RPC addresses).
	NodesList []string
	// WorkerRpcId is a machine ID of the worker RPC client.
	WorkerRpcId string `log:"id"`
	// SuperRpcId is a machine ID of the super RPC client.
	SuperRpcId string `log:"id"`

	// non-rpc fields

	// WorkerRpc is the RPC client connected to a WorkerRpc.
	WorkerRpc *rpc.Client
	// Bootstrap is the RPC machine used to connect WorkerRpc to the Supervisor.
	Bootstrap *bootstrap
	// Dispose the worker.
	Dispose bool
	// WorkerAddr is an index for WorkerInfo.
	WorkerAddr string
	// WorkerInfo describes a worker.
	WorkerInfo *workerInfo
	// WorkersCh returns a list of workers. This channel has to be buffered.
	WorkersCh chan<- []*workerInfo
	// WorkerState is a requested state of workers, eg for listings.
	WorkerState WorkerState
}

// ARpc is a subset of A, that can be passed over RPC.
type ARpc struct {
	Args `json:"-"`

	// Id is a machine ID.
	Id string `log:"id"`
	// PublicAddr is the public address of a Supervisor or Worker.
	PublicAddr string `log:"public_addr"`
	// LocalAddr is the public address of a Supervisor, Worker, or [bootstrap].
	LocalAddr string `log:"local_addr"`
	// BootAddr is the local address of the Bootstrap machine.
	BootAddr string `log:"boot_addr"`
	// NodesList is a list of available nodes (supervisors' public RPC addresses).
	NodesList []string
	// WorkerRpcId is a machine ID of the worker RPC client.
	WorkerRpcId string `log:"worker_rpc_id"`
	// SuperRpcId is a machine ID of the super RPC client.
	SuperRpcId string `log:"super_rpc_id"`
}

// ----- RPC boilerplate

// ArgsRpc will be available in the REPL.
var ArgsRpc = []am.ArgsApi{ARpc{}}

func init() {
	for _, arg := range ArgsRpc {
		gob.Register(arg)
	}
}
