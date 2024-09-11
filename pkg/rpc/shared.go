package rpc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cenkalti/rpc2"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssCli "github.com/pancsta/asyncmachine-go/pkg/rpc/states/client"
)

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
	Data []byte
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

// clientServerMethods is a shared interface for RPC client/server.
type clientServerMethods interface {
	GetKind() Kind
}

type Kind string

const (
	KindClient Kind = "client"
	KindServer Kind = "server"
)

// // DEBUG for perf testing TODO tag
// type ClockMsg am.Time

// ///// ///// /////

// ///// RPC APIS

// ///// ///// /////

// serverRpcMethods is an RPC server for controlling RemoteMachine.
// TODO verify parity with RemoteMachine via reflection
type serverRpcMethods interface {
	// rpc

	RemoteHandshake(client *rpc2.Client, args *Empty, resp *RespHandshake) error

	// mutations

	RemoteAdd(client *rpc2.Client, args *ArgsMut, resp *RespResult) error
	RemoteRemove(client *rpc2.Client, args *ArgsMut, resp *RespResult) error
	RemoteSet(client *rpc2.Client, args *ArgsMut, reply *RespResult) error
}

// clientRpcMethods is the RPC server exposed by the RPC client for bi-di comm.
type clientRpcMethods interface {
	RemoteSetClock(worker *rpc2.Client, args ClockMsg, resp *Empty) error
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

	// ErrNetwork group

	ErrNetwork        = errors.New("network error")
	ErrNetworkTimeout = errors.New("network timeout")
)

// wrapping error setters

func errResponse(mach *am.Machine, err error) {
	mach.AddErr(fmt.Errorf("%w: %w", ErrInvalidResp, err), nil)
}

func errResponseStr(mach *am.Machine, msg string) {
	mach.AddErr(fmt.Errorf("%w: %s", ErrInvalidResp, msg), nil)
}

func errParams(mach *am.Machine, err error) {
	mach.AddErr(fmt.Errorf("%w: %w", ErrInvalidParams, err), nil)
}

func errNetwork(mach *am.Machine, err error) {
	mach.AddErr(fmt.Errorf("%w: %w", ErrNetwork, err), nil)
}

// errAuto detects sentinels from error msgs and wraps.
func errAuto(mach *am.Machine, msg string, err error) {

	// detect group from text
	var errGroup error
	if strings.HasPrefix(err.Error(), "gob: ") {
		errGroup = ErrInvalidResp
	} else if strings.Contains(err.Error(), "rpc2: can't find method") {
		errGroup = ErrRpc
	} else if strings.Contains(err.Error(), "connection is shut down") ||
		strings.Contains(err.Error(), "unexpected EOF") {
		errGroup = ErrNetwork
	}

	// wrap in a group
	if errGroup != nil {
		mach.AddErr(fmt.Errorf("%w: %s: %w", errGroup, msg, err), nil)
		return
	}

	// Exception state fallback
	if msg == "" {
		mach.AddErr(err, nil)
	} else {
		mach.AddErr(fmt.Errorf("%s: %w", msg, err), nil)
	}
}

// ExceptionHandler is a shared exception handler for RPC server and
// client.
type ExceptionHandler struct {
	*am.ExceptionHandler
}

func (h *ExceptionHandler) ExceptionEnter(e *am.Event) bool {
	err := e.Args["err"].(error)

	mach := e.Machine
	isClient := mach.Has(am.S{ssCli.Disconnecting, ssCli.Disconnected})
	if errors.Is(err, ErrNetwork) && isClient &&
		mach.Any1(ssCli.Disconnecting, ssCli.Disconnected) {

		// skip network errors on client disconnect
		return false
	}

	return true
}

func (h *ExceptionHandler) ExceptionState(e *am.Event) {
	// call super
	h.ExceptionHandler.ExceptionState(e)
	mach := e.Machine
	err := e.Args["err"].(error)

	// handle sentinel errors to states
	// TODO handle rpc2.ErrShutdown
	if errors.Is(err, am.ErrHandlerTimeout) {
		// TODO activate ErrSlowHandlers
	} else if errors.Is(err, ErrNetwork) || errors.Is(err, ErrNetworkTimeout) {
		mach.Add1(ss.ErrNetwork, nil)
	} else if errors.Is(err, ErrInvalidParams) {
		mach.Add1(ss.ErrRpc, nil)
	} else if errors.Is(err, ErrInvalidResp) {
		mach.Add1(ss.ErrRpc, nil)
	} else if errors.Is(err, ErrRpc) {
		mach.Add1(ss.ErrRpc, nil)
	}
}

// ///// ///// /////

// ///// CLOCK

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

	// call the destination
	destination, err := net.Dial("tcp", fwdTo)
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
