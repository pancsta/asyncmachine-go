// Package node provides distributed worker pools with supervisors.
package node

import (
	"encoding/gob"
	"errors"
	"fmt"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	"github.com/pancsta/asyncmachine-go/pkg/states"
)

func init() {
	gob.Register(&ARpc{})
}

const (
	// EnvAmNodeLogSupervisor enables machine logging for node supervisor.
	EnvAmNodeLogSupervisor = "AM_NODE_LOG_SUPERVISOR"
	// EnvAmNodeLogClient enables machine logging for node client.
	EnvAmNodeLogClient = "AM_NODE_LOG_CLIENT"
)

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
	ErrRpc           = errors.New("rpc error")
)

// error mutations

// AddErrWorker wraps an error in the ErrWorker sentinel and adds to a machine.
func AddErrWorker(mach *am.Machine, err error, args am.A) {
	mach.AddErrState(ssS.ErrWorker, fmt.Errorf("%w: %w", ErrWorker, err), args)
}

// AddErrWorkerStr wraps a msg in the ErrWorker sentinel and adds to a machine.
func AddErrWorkerStr(mach *am.Machine, msg string, args am.A) {
	mach.AddErrState(ssS.ErrWorker, fmt.Errorf("%w: %s", ErrWorker, msg), args)
}

// AddErrPool wraps an error in the ErrPool sentinel and adds to a machine.
func AddErrPool(mach *am.Machine, err error, args am.A) {
	mach.AddErrState(ssS.ErrPool, fmt.Errorf("%w: %w", ErrPool, err), args)
}

// AddErrPoolStr wraps a msg in the ErrPool sentinel and adds to a machine.
func AddErrPoolStr(mach *am.Machine, msg string, args am.A) {
	mach.AddErrState(ssS.ErrPool, fmt.Errorf("%w: %s", ErrPool, msg), args)
}

// AddErrRpc wraps an error in the ErrRpc sentinel and adds to a machine.
func AddErrRpc(mach *am.Machine, err error, args am.A) {
	err = fmt.Errorf("%w: %w", ErrRpc, err)
	mach.AddErrState(states.BasicStates.ErrNetwork, err, args)
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

// A is a struct for node arguments. It's a typesafe alternative to am.A.
type A struct {
	// Id is a machine ID.
	Id string `log:"id"`
	// PublicAddr is the public address of a Supervisor or Worker.
	PublicAddr string `log:"public_addr"`
	// LocalAddr is the public address of a Supervisor or Worker.
	LocalAddr string `log:"local_addr"`
	// NodesList is a list of available nodes (supervisors' public RPC addresses).
	NodesList  []string
	ClientAddr string

	// non-rpc fields

	// Worker is the RPC client connected to a Worker.
	Worker *rpc.Client
	// Bootstrap is the RPC machine used to connect Worker to the Supervisor.
	Bootstrap *bootstrap
	// Dispose the worker
	Dispose bool
}

// ARpc is a subset of A, that can be passed over RPC.
type ARpc struct {
	// Id is a machine ID.
	Id string `log:"id"`
	// PublicAddr is the public address of a Supervisor or Worker.
	PublicAddr string `log:"public_addr"`
	// LocalAddr is the public address of a Supervisor, Worker, or [bootstrap].
	LocalAddr string `log:"local_addr"`
	// NodesList is a list of available nodes (supervisors' public RPC addresses).
	NodesList  []string
	ClientAddr string
}

// ParseArgs extracts A from [am.Event.Args]["am_node"].
func ParseArgs(args am.A) *A {
	if r, _ := args["am_node"].(*ARpc); r != nil {
		return amhelp.ArgsToArgs(r, &A{})
	}
	a, _ := args["am_node"].(*A)
	return a
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	return am.A{"am_node": args}
}

// PassRpc prepares [am.A] from A to pass over RPC.
func PassRpc(args *A) am.A {
	return am.A{"am_node": amhelp.ArgsToArgs(args, &ARpc{})}
}

// LogArgs is an args logger for A and rpc.A.
func LogArgs(args am.A) map[string]string {
	a1 := rpc.ParseArgs(args)
	a2 := ParseArgs(args)
	if a1 == nil && a2 == nil {
		return nil
	}

	return am.AMerge(amhelp.ArgsToLogMap(a1), amhelp.ArgsToLogMap(a2))
}
