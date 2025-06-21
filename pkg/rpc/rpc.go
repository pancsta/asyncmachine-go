// Package rpc is a transparent RPC for state machines.
package rpc

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/rpc2"

	"github.com/pancsta/asyncmachine-go/pkg/rpc/rpcnames"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

func init() {
	gob.Register(&ARpc{})
	gob.Register(am.Relation(0))
}

const (
	// EnvAmRpcLogServer enables machine logging for RPC server.
	EnvAmRpcLogServer = "AM_RPC_LOG_SERVER"
	// EnvAmRpcLogClient enables machine logging for RPC client.
	EnvAmRpcLogClient = "AM_RPC_LOG_CLIENT"
	// EnvAmRpcLogMux enables machine logging for RPC multiplexers.
	EnvAmRpcLogMux = "AM_RPC_LOG_MUX"
	// EnvAmRpcDbg enables env-based debugging for RPC components.
	EnvAmRpcDbg = "AM_RPC_DBG"
	// EnvAmReplAddr is a REPL address to listen on. "1" expands to 127.0.0.1:0.
	EnvAmReplAddr = "AM_REPL_ADDR"
	// EnvAmReplDir is a dir path to save the address file as
	// $AM_REPL_DIR/mach-id.addr. Optional.
	EnvAmReplDir = "AM_REPL_DIR"
)

var ss = states.SharedStates

// ///// ///// /////

// ///// TYPES

// ///// ///// /////

type ArgsHello struct {
	ReqSchema bool
}

// ArgsMut is args for mutation methods.
type ArgsMut struct {
	States []int
	Args   am.A
	Event  *am.Event
}

type ArgsGet struct {
	Name string
}

type ArgsLog struct {
	Msg  string
	Args []any
}

type ArgsPayload struct {
	Name string
	// Source is the machine ID that sent the payload.
	Source string
	// SourceTx is transition ID.
	SourceTx string
	// Destination is an optional machine ID that is supposed to receive the
	// payload. Useful when using rpc.Mux.
	Destination string
	// Data is the payload data. The Consumer has to know the type.
	Data any

	// Token is a unique random ID for the payload. Autofilled by the server.
	Token string
}

type RespHandshake struct {
	Schema     am.Schema
	Serialized *am.Serialized
}

type RespResult struct {
	Clock  ClockMsg
	Result am.Result
}

type RespSync struct {
	Time am.Time
}

type RespGet struct {
	Value any
}

type Empty struct{}

type ClockMsg [][2]int

type PushAllTicks struct {
	// Mutation is 0:[am.MutationType] 1-n: called state index
	Mutation []int
	ClockMsg ClockMsg
}

// clientServerMethods is a shared interface for RPC client/server.
type clientServerMethods interface {
	GetKind() Kind
}

type Kind string

const (
	KindClient Kind = "client"
	KindServer Kind = "server"
)

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "am_rpc"

// A represents typed arguments of the RPC package. It's a typesafe alternative
// to [am.A].
type A struct {
	Id        string `log:"id"`
	Name      string `log:"name"`
	MachTime  am.Time
	Payload   *ArgsPayload
	Addr      string `log:"addr"`
	Err       error
	Method    string `log:"addr"`
	StartedAt time.Time
	Dispose   bool

	// non-rpc fields

	Client *rpc2.Client
}

// ARpc is a subset of A, that can be passed over RPC.
type ARpc struct {
	Id        string `log:"id"`
	Name      string `log:"name"`
	MachTime  am.Time
	Payload   *ArgsPayload
	Addr      string `log:"addr"`
	Err       error
	Method    string `log:"addr"`
	StartedAt time.Time
	Dispose   bool
}

// ParseArgs extracts A from [am.Event.Args][APrefix].
func ParseArgs(args am.A) *A {
	if r, _ := args[APrefix].(*ARpc); r != nil {
		return amhelp.ArgsToArgs(r, &A{})
	}
	if a, _ := args[APrefix].(*A); a != nil {
		return a
	}
	return &A{}
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	return am.A{APrefix: args}
}

// PassRpc prepares [am.A] from A to pass over RPC.
func PassRpc(args *A) am.A {
	return am.A{APrefix: amhelp.ArgsToArgs(args, &ARpc{})}
}

// LogArgs is an args logger for A.
func LogArgs(args am.A) map[string]string {
	a := ParseArgs(args)
	if a == nil {
		return nil
	}

	return amhelp.ArgsToLogMap(a, 0)
}

// // DEBUG for perf testing TODO tag
// type ClockMsg am.Time

// ///// ///// /////

// ///// RPC APIS

// ///// ///// /////

// serverRpcMethods is the main RPC server's exposed methods.
type serverRpcMethods interface {
	// rpc

	RemoteHello(client *rpc2.Client, args *ArgsHello, resp *RespHandshake) error

	// mutations

	RemoteAdd(client *rpc2.Client, args *ArgsMut, resp *RespResult) error
	RemoteRemove(client *rpc2.Client, args *ArgsMut, resp *RespResult) error
	RemoteSet(client *rpc2.Client, args *ArgsMut, reply *RespResult) error
}

// clientRpcMethods is the RPC server exposed by the RPC client for bi-di comm.
type clientRpcMethods interface {
	RemoteSetClock(worker *rpc2.Client, args ClockMsg, resp *Empty) error
	RemoteSendingPayload(
		worker *rpc2.Client, file *ArgsPayload, resp *Empty,
	) error
	RemoteSendPayload(worker *rpc2.Client, file *ArgsPayload, resp *Empty) error
}

// ///// ///// /////

// ///// ERRORS

// ///// ///// /////

// sentinel errors

var (
	// ErrClient group

	ErrInvalidParams = errors.New("invalid params")
	ErrInvalidResp   = errors.New("invalid response")
	ErrRpc           = errors.New("rpc")
	ErrNoAccess      = errors.New("no access")
	ErrNoConn        = errors.New("not connected")
	ErrDestination   = errors.New("wrong destination")

	// ErrNetwork group

	ErrNetwork        = errors.New("network error")
	ErrNetworkTimeout = errors.New("network timeout")

	// TODO ErrDelivery
)

// wrapping error setters

func AddErrRpcStr(e *am.Event, mach *am.Machine, msg string) {
	err := fmt.Errorf("%w: %s", ErrRpc, msg)
	mach.EvAddErrState(e, ss.ErrRpc, err, nil)
}

func AddErrParams(e *am.Event, mach *am.Machine, err error) {
	err = fmt.Errorf("%w: %w", ErrInvalidParams, err)
	mach.AddErrState(ss.ErrRpc, err, nil)
}

func AddErrResp(e *am.Event, mach *am.Machine, err error) {
	err = fmt.Errorf("%w: %w", ErrInvalidResp, err)
	mach.AddErrState(ss.ErrRpc, err, nil)
}

func AddErrNetwork(e *am.Event, mach *am.Machine, err error) {
	mach.AddErrState(ss.ErrNetwork, err, nil)
}

func AddErrNoConn(e *am.Event, mach *am.Machine, err error) {
	err = fmt.Errorf("%w: %w", ErrNoConn, err)
	mach.AddErrState(ss.ErrNetwork, err, nil)
}

// AddErr detects sentinels from error msgs and calls the proper error setter.
// TODO also return error for compat
func AddErr(e *am.Event, mach *am.Machine, msg string, err error) {
	if msg != "" {
		err = fmt.Errorf("%w: %s", err, msg)
	}

	if strings.HasPrefix(err.Error(), "gob: ") {
		AddErrResp(e, mach, err)
	} else if strings.Contains(err.Error(), "rpc2: can't find method") {
		AddErrRpcStr(e, mach, err.Error())
	} else if strings.Contains(err.Error(), "connection is shut down") ||
		strings.Contains(err.Error(), "unexpected EOF") {

		// TODO bind to sentinels io.ErrUnexpectedEOF, rpc2.ErrShutdown
		mach.AddErrState(ss.ErrRpc, err, nil)
	} else if strings.Contains(err.Error(), "timeout") {
		AddErrNetwork(e, mach, errors.Join(err, ErrNetworkTimeout))
	} else if _, ok := err.(*net.OpError); ok {
		AddErrNetwork(e, mach, err)
	} else {
		mach.AddErr(err, nil)
	}
}

// ExceptionHandler is a shared exception handler for RPC server and
// client.
type ExceptionHandler struct {
	*am.ExceptionHandler
}

func (h *ExceptionHandler) ExceptionEnter(e *am.Event) bool {
	args := ParseArgs(e.Args)
	mach := e.Machine()

	isRpcClient := mach.Has(am.S{ssC.Disconnecting, ssC.Disconnected})
	if errors.Is(args.Err, ErrNetwork) && isRpcClient &&
		mach.Any1(ssC.Disconnecting, ssC.Disconnected) {

		// skip network errors on client disconnect
		e.Machine().Log("ignoring ErrNetwork on Disconnecting/Disconnected")
		return false
	}

	return true
}

// ///// ///// /////

// ///// REMOTE HANDLERS

// ///// ///// /////

// Event struct represents a single event of a Mutation within a Transition.
// One event can have 0-n handlers. TODO remove?
type Event struct {
	// Name of the event / handler
	Name string
	// Machine is the machine that the event belongs to.
	Machine am.Api
}

// Transition represents processing of a single mutation within a machine.
type Transition struct {
	// Machine is the parent machine of this transition.
	Machine am.Api
	// TimeBefore is the machine time from before the transition.
	TimeBefore am.Time
	// TimeAfter is the machine time from after the transition. If the transition
	// has been canceled, this will be the same as TimeBefore.
	TimeAfter am.Time
}

type HandlerFinal func(e *Event)

type remoteHandler struct {
	h            any
	funcNames    []string
	funcCache    map[string]reflect.Value
	missingCache map[string]struct{}
}

func newRemoteHandler(
	h any,
	funcNames []string,
) *remoteHandler {
	return &remoteHandler{
		h:            h,
		funcNames:    funcNames,
		funcCache:    make(map[string]reflect.Value),
		missingCache: make(map[string]struct{}),
	}
}

// ///// ///// /////

// ///// TRACERS

// ///// ///// /////

// WorkerTracer is a tracer for local worker machines (event source).
type WorkerTracer struct {
	*am.NoOpTracer

	s *Server
}

func (t *WorkerTracer) TransitionEnd(_ *am.Transition) {
	// TODO channel and value in atomic, skip dups (smaller tick values)
	go func() {
		t.s.mutMx.Lock()
		defer t.s.mutMx.Unlock()

		t.s.pushClockUpdate(false)
	}()
}

func (t *WorkerTracer) SchemaChange(mach am.Api, oldSchema am.Schema) {
	go func() {
		t.s.mutMx.Lock()
		defer t.s.mutMx.Unlock()

		if c := t.s.rpcClient.Load(); c != nil {
			msg := &RespHandshake{
				Schema:     mach.Schema(),
				Serialized: mach.Export(),
			}
			err := c.CallWithContext(mach.Ctx(),
				rpcnames.ClientSchemaChange.Encode(), msg, &Empty{})
			mach.AddErr(err, nil)
		}
	}()
}

// TODO implement as an optimization
// func (t *WorkerTracer) QueueEnd(_ *am.Transition) {
//	t.s.pushClockUpdate()
// }

// ///// ///// /////

// ///// MISC

// ///// ///// /////

// MachReplEnv sets up a machine for a REPL connection in case AM_REPL_ADDR env
// var is set. See MachRepl.
func MachReplEnv(mach am.Api) <-chan error {
	addr := os.Getenv(EnvAmReplAddr)
	dir := os.Getenv(EnvAmReplDir)

	err := make(chan error)
	if addr == "" {
		return err
	} else if addr == "1" {
		// expand 1 to default
		addr = ""
	}

	MachRepl(mach, addr, dir, nil, nil)

	return err
}

// MachRepl sets up a machine for a REPL connection, which allows for
// mutations, like any other RPC connection. See [/tools/cmd/arpc] for usage.
// This function is considered a debugging helper and can panic.
//
// addr: address to listen on, default to 127.0.0.1:0
// addrDir: optional dir path to save the address file as addrDir/mach-id.addr.
// addrCh: optional channel to send the address to, once ready
// errCh: optional channel to send err to, once ready
func MachRepl(
	mach am.Api, addr, addrDir string, addrCh chan<- string, errCh chan<- error,
) {
	if amhelp.IsTestRunner() {
		return
	}

	if addr == "" {
		addr = "127.0.0.1:0"
	}

	if mach.HasHandlers() && !mach.Has(ssW.Names()) {
		err := fmt.Errorf(
			"%w: REPL source has to implement pkg/rpc/states/WorkerStatesDef",
			am.ErrSchema)

		// panic only early
		panic(err)
	}

	mux, err := NewMux(mach.Ctx(), "repl-"+mach.Id(), nil, &MuxOpts{
		Parent: mach,
	})
	// panic only early
	if err != nil {
		panic(err)
	}
	mux.Addr = addr
	mux.Source = mach
	mux.Start()

	if addrCh == nil && addrDir == "" {
		if errCh != nil {
			close(errCh)
		}
		return
	}

	go func() {
		// dispose ret channels
		defer func() {
			if errCh != nil {
				close(errCh)
			}
			if addrCh != nil {
				close(addrCh)
			}
		}()

		// prep the dir
		dirOk := false
		if addrDir != "" {
			if _, err := os.Stat(addrDir); os.IsNotExist(err) {
				err := os.MkdirAll(addrDir, 0o755)
				if err == nil {
					dirOk = true
				} else if errCh != nil {
					errCh <- err
				}
			} else {
				dirOk = true
			}
		}

		// wait for an addr
		<-mux.Mach.When1(ssM.Ready, nil)
		if addrCh != nil {
			addrCh <- mux.Addr
		}

		// save to dir
		if dirOk && addrDir != "" {
			err = os.WriteFile(
				filepath.Join(addrDir, mach.Id()+".addr"),
				[]byte(mux.Addr), 0o644,
			)
			if errCh != nil {
				errCh <- err
			}
		}
	}()
}

// // DEBUG for perf testing
// func NewClockMsg(before, after am.Time) ClockMsg {
//	return ClockMsg(after)
// }
//
// // DEBUG for perf testing
// func ClockFromMsg(before am.Time, msg ClockMsg) am.Time {
//	return am.Time(msg)
// }

func NewClockMsg(before, after am.Time) ClockMsg {
	var val [][2]int

	for i := range after {
		if before == nil || i >= len(before) {
			// TODO test this path
			val = append(val, [2]int{i, int(after[i])})
		} else if before[i] != after[i] {
			val = append(val, [2]int{i, int(after[i] - before[i])})
		}
	}

	return val
}

func ClockFromMsg(before am.Time, msg ClockMsg) am.Time {
	after := slices.Clone(before)
	l := len(after)

	for _, v := range msg {
		if len(v) < 2 {
			continue
		}
		key := v[0]
		val := v[1]
		if key >= l {
			// TODO
			continue
		}
		after[key] += uint64(val)
	}

	return after
}

func TrafficMeter(
	listener net.Listener, fwdTo string, counter chan<- int64,
	end <-chan struct{},
) {
	defer listener.Close()
	// fmt.Println("Listening on " + listenOn)

	// callFailsafe the destination
	destination, err := net.Dial("tcp4", fwdTo)
	if err != nil {
		fmt.Println("Error connecting to destination:", err.Error())
		return
	}
	defer destination.Close()

	// wait for the connection
	conn, err := listener.Accept()
	if err != nil {
		fmt.Println("Error accepting connection:", err.Error())
		return
	}
	defer conn.Close()

	// forward data bidirectionally
	wg := sync.WaitGroup{}
	wg.Add(2)
	bytes := atomic.Int64{}
	go func() {
		c, _ := io.Copy(destination, conn)
		bytes.Add(c)
		wg.Done()
	}()
	go func() {
		c, _ := io.Copy(conn, destination)
		bytes.Add(c)
		wg.Done()
	}()

	// wait for the test and forwarding to finish
	<-end
	// fmt.Printf("Closing counter...\n")
	_ = listener.Close()
	_ = destination.Close()
	_ = conn.Close()
	wg.Wait()

	c := bytes.Load()
	// fmt.Printf("Forwarded %d bytes\n", c)
	counter <- c
}

// DisposeWithCtx handles early binding disposal caused by a canceled context.
// It's used by most of "when" methods.
// TODO GC in the handler loop instead
// TODO mixin from am.Subscription
func DisposeWithCtx[T comparable](
	mach *Worker, ctx context.Context, ch chan struct{}, states am.S, binding T,
	lock *sync.RWMutex, index map[string][]T, logMsg string,
) {
	if ctx == nil {
		return
	}
	go func() {
		select {
		case <-ch:
			return
		case <-mach.Ctx().Done():
			return
		case <-ctx.Done():
		}

		// TODO track
		utils.CloseSafe(ch)

		// GC only if needed
		if mach.Disposed.Load() {
			return
		}
		lock.Lock()
		defer lock.Unlock()

		for _, s := range states {
			if _, ok := index[s]; ok {
				if len(index[s]) == 1 {
					delete(index, s)
				} else {
					index[s] = utils.SlicesWithout(index[s], binding)
				}

				if logMsg != "" {
					mach.LogLvl(am.LogOps, logMsg) //nolint:govet
				}
			}
		}
	}()
}
