// Package rpc is a transparent RPC for state machines.
package rpc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/rpc2"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

const (
	// EnvAmRpcLogServer enables machine logging for RPC server.
	EnvAmRpcLogServer = "AM_RPC_LOG_SERVER"
	// EnvAmRpcLogClient enables machine logging for RPC client.
	EnvAmRpcLogClient = "AM_RPC_LOG_CLIENT"
)

var ss = states.SharedStates

// ///// ///// /////

// ///// TYPES

// ///// ///// /////

// ArgsMut is args for mutation methods.
type ArgsMut struct {
	States []int
	Args   am.A
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
	// Data is the payload data. The Consumer has to know the type.
	Data any
	// Token is a unique random ID for the payload. Autofilled by the server.
	Token string
}

type RespHandshake = am.Serialized

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

// A represents typed arguments of the RPC package.
type A struct {
	Id        string `log:"id"`
	Name      string `log:"name"`
	MachTime  am.Time
	Payload   *ArgsPayload
	Addr      string `log:"addr"`
	Err       error
	Method    string `log:"addr"`
	StartedAt time.Time
	Client    *rpc2.Client
	Dispose   bool
}

// ParseArgs extracts A from [am.Event.Args].
func ParseArgs(args am.A) *A {
	ret := &A{}

	// TODO needed?
	if machTime, ok := args["mach_time"].(am.Time); ok {
		ret.MachTime = machTime
	}
	if payload, ok := args["payload"].(*ArgsPayload); ok {
		ret.Payload = payload
	}
	if name, ok := args["name"].(string); ok {
		ret.Name = name
	}
	if id, ok := args["id"].(string); ok {
		ret.Id = id
	}
	if addr, ok := args["addr"].(string); ok {
		ret.Addr = addr
	}
	if err, ok := args["err"].(error); ok {
		ret.Err = err
	}
	if method, ok := args["method"].(string); ok {
		ret.Method = method
	}
	if startedAt, ok := args["started_at"].(time.Time); ok {
		ret.StartedAt = startedAt
	}
	if client, ok := args["client"].(*rpc2.Client); ok {
		ret.Client = client
	}

	return ret
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	a := am.A{}

	if args.Payload != nil {
		a["payload"] = args.Payload
	}
	if args.Name != "" {
		a["name"] = args.Name
	}
	if args.Id != "" {
		a["id"] = args.Id
	}
	if args.MachTime != nil {
		a["mach_time"] = args.MachTime
	}
	if args.Addr != "" {
		a["addr"] = args.Addr
	}
	if args.Err != nil {
		a["err"] = args.Err
	}
	if args.Method != "" {
		a["method"] = args.Method
	}
	if !args.StartedAt.IsZero() {
		a["started_at"] = args.StartedAt
	}
	if args.Client != nil {
		a["client"] = args.Client
	}

	return a
}

// LogArgs is an args logger for A.
func LogArgs(args am.A) map[string]string {
	a := ParseArgs(args)
	if a == nil {
		return nil
	}

	return amhelp.ArgsToLogMap(a)
}

// // DEBUG for perf testing TODO tag
// type ClockMsg am.Time

// ///// ///// /////

// ///// RPC APIS

// ///// ///// /////

// serverRpcMethods is the main RPC server's exposed methods.
type serverRpcMethods interface {
	// rpc

	RemoteHello(client *rpc2.Client, args *Empty, resp *RespHandshake) error

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

	// ErrNetwork group

	ErrNetwork        = errors.New("network error")
	ErrNetworkTimeout = errors.New("network timeout")

	// TODO ErrDelivery
)

// wrapping error setters

func AddErrRpcStr(mach *am.Machine, msg string) {
	err := fmt.Errorf("%w: %s", ErrRpc, msg)
	mach.AddErrState(ss.ErrRpc, err, nil)
}

func AddErrParams(mach *am.Machine, err error) {
	err = fmt.Errorf("%w: %w", ErrInvalidParams, err)
	mach.AddErrState(ss.ErrRpc, err, nil)
}

func AddErrResp(mach *am.Machine, err error) {
	err = fmt.Errorf("%w: %w", ErrInvalidResp, err)
	mach.AddErrState(ss.ErrRpc, err, nil)
}

func AddErrNetwork(mach *am.Machine, err error) {
	mach.AddErrState(ss.ErrNetwork, err, nil)
}

func AddErrNoConn(mach *am.Machine, err error) {
	err = fmt.Errorf("%w: %w", ErrNoConn, err)
	mach.AddErrState(ss.ErrNetwork, err, nil)
}

// AddErr detects sentinels from error msgs and calls the proper error setter.
func AddErr(mach *am.Machine, msg string, err error) {
	if msg == "" {
		err = fmt.Errorf("%w: %s", err, msg)
	}

	if strings.HasPrefix(err.Error(), "gob: ") {
		AddErrResp(mach, err)
	} else if strings.Contains(err.Error(), "rpc2: can't find method") {
		AddErrRpcStr(mach, err.Error())
	} else if strings.Contains(err.Error(), "connection is shut down") ||
		strings.Contains(err.Error(), "unexpected EOF") {

		// TODO bind to sentinels io.ErrUnexpectedEOF, rpc2.ErrShutdown
		mach.AddErrState(ss.ErrRpc, err, nil)
	} else if strings.Contains(err.Error(), "timeout") {
		AddErrNetwork(mach, errors.Join(err, ErrNetworkTimeout))
	} else if _, ok := err.(*net.OpError); ok {
		AddErrNetwork(mach, err)
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
	mach := e.Machine

	isRpcClient := mach.Has(am.S{ssC.Disconnecting, ssC.Disconnected})
	if errors.Is(args.Err, ErrNetwork) && isRpcClient &&
		mach.Any1(ssC.Disconnecting, ssC.Disconnected) {

		// skip network errors on client disconnect
		e.Machine.Log("ignoring ErrNetwork on Disconnecting/Disconnected")
		return false
	}

	return true
}

// ///// ///// /////

// ///// REMOTE HANDLERS

// ///// ///// /////

// Event struct represents a single event of a Mutation within a Transition.
// One event can have 0-n handlers.
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
) remoteHandler {
	return remoteHandler{
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

// TODO implement as an optimization
// func (t *WorkerTracer) QueueEnd(_ *am.Transition) {
//	t.s.pushClockUpdate()
// }

// ///// ///// /////

// ///// MISC

// ///// ///// /////

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

	for k := range after {
		if before == nil {
			// TODO test this path
			val = append(val, [2]int{k, int(after[k])})
		} else if before[k] != after[k] {
			val = append(val, [2]int{k, int(after[k] - before[k])})
		}
	}

	return val
}

func ClockFromMsg(before am.Time, msg ClockMsg) am.Time {
	after := slices.Clone(before)

	for _, v := range msg {
		key := v[0]
		val := v[1]
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
