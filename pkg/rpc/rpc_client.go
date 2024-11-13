package rpc

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/rpc2"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/rpcnames"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

var (
	ssC  = states.ClientStates
	ssCo = states.ConsumerStates
)

type Client struct {
	*ExceptionHandler

	Mach *am.Machine
	Name string

	// Addr is the address the Client will connect to.
	Addr string
	// Worker is a remote am.Machine instance
	Worker *Worker
	// Consumer is the optional consumer for deliveries.
	Consumer   *am.Machine
	CallCount  uint64
	LogEnabled bool
	// DisconnCooldown is the time to wait after notifying the server about
	// disconnecting before actually disconnecting. Default 10ms.
	DisconnCooldown time.Duration
	// LastMsgAt is the last received msg from the worker TODO
	LastMsgAt time.Time
	// HelloDelay between Connected and Handshaking. Default 0, useful for
	// rpc/Mux.
	HelloDelay time.Duration
	// ReconnectOn decides if the client will try to [RetryingConn] after a
	// clean [Disconnect].
	ReconnectOn bool

	// failsafe - connection

	// ConnTimeout is the maximum time to wait for a connection to be established.
	// Default 3s.
	ConnTimeout time.Duration
	// ConnRetries is the number of retries for a connection. Default 15.
	ConnRetries int
	// ConnRetryTimeout is the maximum time to retry a connection. Default 1m.
	ConnRetryTimeout time.Duration
	// ConnRetryDelay is the time to wait between retries. Default 100ms. If
	// ConnRetryBackoff is set, this is the initial delay, and doubles on each
	// retry.
	ConnRetryDelay time.Duration
	// ConnRetryBackoff is the maximum time to wait between retries. Default 3s.
	ConnRetryBackoff time.Duration

	// failsafe - calls

	// CallTimeout is the maximum time to wait for a call to complete. Default 3s.
	CallTimeout time.Duration
	// CallRetries is the number of retries for a call. Default 15.
	CallRetries int
	// CallRetryTimeout is the maximum time to retry a call. Default 1m.
	CallRetryTimeout time.Duration
	// CallRetryDelay is the time to wait between retries. Default 100ms. If
	// CallRetryBackoff is set, this is the initial delay, and doubles on each
	// retry.
	CallRetryDelay time.Duration
	// CallRetryBackoff is the maximum time to wait between retries. Default 3s.
	CallRetryBackoff time.Duration

	// internal

	callLock    sync.Mutex
	rpc         *rpc2.Client
	stateNames  am.S
	stateStruct am.Struct
	conn        net.Conn
	// tmpTestErr is an error to return on the next call or notify, only for
	// testing.
	tmpTestErr error
	// permTestErr is an error to return on the next call or notify, only for
	// testing.
	permTestErr    error
	connRetryRound atomic.Int32
}

// interfaces
var (
	_ clientRpcMethods    = &Client{}
	_ clientServerMethods = &Client{}
)

// NewClient creates a new RPC client and exposes a remote state machine as
// a remote worker, with a subst of the API under Client.Worker. Optionally
// takes a consumer, which is a state machine with a WorkerPayload state. See
// states.ConsumerStates.
func NewClient(
	ctx context.Context, workerAddr string, name string, stateStruct am.Struct,
	stateNames am.S, opts *ClientOpts,
) (*Client, error) {
	// validate
	if workerAddr == "" {
		return nil, errors.New("rpcc: workerAddr required")
	}
	if stateStruct == nil {
		return nil, errors.New("rpcc: stateStruct required")
	}
	if stateNames == nil {
		return nil, errors.New("rpcc: stateNames required")
	}

	if name == "" {
		name = "rpc"
	}
	if opts == nil {
		opts = &ClientOpts{}
	}

	c := &Client{
		Name:             name,
		ExceptionHandler: &ExceptionHandler{},
		LogEnabled:       os.Getenv(EnvAmRpcLogClient) != "",
		Addr:             workerAddr,
		CallTimeout:      3 * time.Second,
		ConnTimeout:      3 * time.Second,
		DisconnCooldown:  10 * time.Millisecond,
		ReconnectOn:      true,

		ConnRetryTimeout: 1 * time.Minute,
		ConnRetries:      15,
		ConnRetryDelay:   100 * time.Millisecond,
		ConnRetryBackoff: 3 * time.Second,

		CallRetryTimeout: 1 * time.Minute,
		CallRetries:      15,
		CallRetryDelay:   100 * time.Millisecond,
		CallRetryBackoff: 3 * time.Second,

		stateNames:  slices.Clone(stateNames),
		stateStruct: maps.Clone(stateStruct),
	}

	if amhelp.IsDebug() {
		c.CallTimeout = 100 * time.Second
	}

	// state machine
	mach, err := am.NewCommon(ctx, GetClientId(name), states.ClientStruct,
		ssC.Names(), c, opts.Parent, nil)
	if err != nil {
		return nil, err
	}
	mach.SetLogArgs(LogArgs)
	c.Mach = mach

	if opts.Consumer != nil {

		err := amhelp.Implements(opts.Consumer.StateNames(), ssCo.Names())
		if err != nil {
			err := fmt.Errorf(
				"consumer has to implement pkg/rpc/states/ConsumerStatesDef: %w", err)

			return nil, err
		}
		c.Consumer = opts.Consumer
	}

	return c, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (c *Client) StartState(e *am.Event) {
	// TODO extract NewWorker
	c.Worker = &Worker{
		c:             c,
		ctx:           c.Mach.Ctx(),
		states:        c.stateStruct,
		stateNames:    c.stateNames,
		indexWhen:     am.IndexWhen{},
		indexStateCtx: am.IndexStateCtx{},
		indexWhenTime: am.IndexWhenTime{},
		whenDisposed:  make(chan struct{}),
		machTime:      make(am.Time, len(c.stateNames)),
		parentId:      c.Mach.Id(),
	}
	lvl := am.LogNothing
	c.Worker.logLevel.Store(&lvl)
	c.Worker.activeState.Store(&am.S{})
	c.Mach.RegisterDisposalHandler(func() {
		c.Worker.Dispose()
	})
}

func (c *Client) StartEnd(e *am.Event) {
	// gather state from before the transition
	before := e.Transition().TimeBefore
	idx := e.Machine.Index

	// if never connected, stop here
	if before.Is([]int{idx(ssC.Connecting), idx(ssC.Exception)}) {
		return
	}

	// graceful disconnect
	wasConn := before.Is1(idx(ssC.Connecting)) || before.Is1(idx(ssC.Connected))
	if wasConn {
		c.Mach.Add1(ssC.Disconnecting, nil)
	}
}

func (c *Client) ConnectingState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ssC.Connecting)

	// async
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// net dial
		timeout := c.ConnTimeout
		if amhelp.IsDebug() {
			timeout = 100 * time.Second
		}
		// TODO TLS
		d := net.Dialer{
			Timeout: timeout,
		}
		c.Mach.Log("dialing %s", c.Addr)
		conn, err := d.DialContext(ctx, "tcp4", c.Addr)
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			c.Mach.Add1(ssC.Disconnected, nil)
			AddErrNetwork(c.Mach, err)
			return
		}
		c.conn = conn

		// rpc
		c.bindRpcHandlers(conn)
		go c.rpc.Run()

		c.Mach.Add1(ssC.Connected, nil)
	}()
}

func (c *Client) DisconnectingEnter(e *am.Event) bool {
	return c.rpc != nil && c.conn != nil
}

func (c *Client) DisconnectingState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ssC.Disconnecting)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// notify the server and wait a bit
		c.notify(ctx, rpcnames.Bye.Encode(), &Empty{})
		if !amhelp.Wait(ctx, c.DisconnCooldown) {
			c.ensureGroupConnected()

			return // expired
		}

		// close with timeout
		if c.rpc != nil {
			select {
			case <-time.After(c.CallTimeout):
				c.log("rpc.Close timeout")
			case <-amhelp.ExecAndClose(func() {
				_ = c.rpc.Close()
			}):
				c.log("rpc.Close")
			}
		}
		if ctx.Err() != nil {
			c.ensureGroupConnected()

			return // expired
		}

		c.Mach.Add1(ssC.Disconnected, nil)
	}()
}

func (c *Client) ConnectedState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ssC.Connected)
	disconnCh := c.rpc.DisconnectNotify()
	// reset reconn counter
	c.connRetryRound.Store(0)

	go func() {
		select {

		case <-ctx.Done():
			return // expired

		case <-disconnCh:
			c.log("rpc.DisconnectNotify")
			c.Mach.Add1(ssC.Disconnected, nil)
		}
	}()
}

func (c *Client) DisconnectedEnter(e *am.Event) bool {
	// graceful disconnect
	return !c.Mach.WillBe1(ssC.Disconnecting)
}

func (c *Client) DisconnectedState(e *am.Event) {
	// try to reconnect
	wasAny := e.Transition().TimeBefore.Any1
	if wasAny(c.Mach.Index(ssC.Connected), c.Mach.Index(ssC.Connecting)) &&
		c.ReconnectOn {

		c.Mach.Add1(ssC.RetryingConn, nil)
		return
	}

	// ignore error when disconnecting
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *Client) HandshakingState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ssC.Connected)

	// unblock
	go func() {
		// send hello or retry conn
		resp := &RespHandshake{}
		if c.HelloDelay > 0 {
			if !amhelp.Wait(ctx, c.HelloDelay) {
				return // expired
			}
		}

		// retry to pass cmux
		ok := false
		delay := c.CallRetryDelay
		// shorten the timeout
		timeout := c.CallTimeout / 2
		for i := 0; i < c.ConnRetries; i++ {
			// TODO pass ID and key here
			if c.call(ctx, rpcnames.Hello.Encode(), Empty{}, resp, timeout) {
				ok = true
				c.log("hello ok on %d try", i+1)

				break
			}
			if !amhelp.Wait(ctx, delay) {
				return // expired
			}

			// double the delay when backoff set
			if c.CallRetryBackoff > 0 {
				delay *= 2
				if delay > c.CallRetryBackoff {
					delay = c.CallRetryBackoff
				}
			}
		}
		if !ok {
			c.Mach.Add1(ssC.RetryingConn, nil)
			return
		}

		// validate
		if len(resp.StateNames) == 0 {
			AddErrRpcStr(c.Mach, "states missing")
			return
		}
		if resp.ID == "" {
			AddErrRpcStr(c.Mach, "ID missing")
			return
		}

		// compare states
		diff := am.DiffStates(c.stateNames, resp.StateNames)
		if len(diff) > 0 || len(resp.StateNames) != len(c.stateNames) {
			AddErrRpcStr(c.Mach, "States differ on client/server")
			return
		}

		// confirm the handshake or retry conn
		// TODO pass ID and key here
		if !c.call(ctx, rpcnames.Handshake.Encode(), c.Mach.Id(), &Empty{}, 0) {
			c.Mach.Add1(ssC.RetryingConn, nil)
			return
		}

		// finalize
		c.Mach.Add1(ssC.HandshakeDone, Pass(&A{
			Id:       resp.ID,
			MachTime: resp.Time,
		}))
	}()
}

func (c *Client) HandshakeDoneEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a.Id != "" && a.MachTime != nil
}

func (c *Client) HandshakeDoneState(e *am.Event) {
	args := ParseArgs(e.Args)

	// finalize the worker init
	w := c.Worker
	w.ID = c.Name + "-" + args.Id
	w.machTime = args.MachTime

	c.updateClock(nil, args.MachTime)

	c.log("connected to %s", c.Worker.ID)
	c.log("time t%d: %v", c.Worker.TimeSum(nil), args.MachTime)
}

func (c *Client) CallRetryFailedState(e *am.Event) {
	c.Mach.Remove1(ssC.CallRetryFailed, nil)

	// TODO disconnect after N failed retries
	// TODO backoff and reconnect (retry the whole connection)
}

func (c *Client) RetryingCallEnter(e *am.Event) bool {
	return c.Mach.Any1(ssC.Connected, ssC.RetryingConn)
}

// ExceptionState handles network errors and retries the connection.
func (c *Client) ExceptionState(e *am.Event) {
	// call super
	c.ExceptionHandler.ExceptionState(e)
	c.Mach.Remove1(am.Exception, nil)
}

// RetryingConnState should be set without Connecting in the same tx
func (c *Client) RetryingConnState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ssC.RetryingConn)
	delay := c.ConnRetryDelay
	start := time.Now()

	// unblock
	go func() {
		// retry loop
		for ctx.Err() == nil && c.connRetryRound.Load() < int32(c.ConnRetries) {
			c.connRetryRound.Add(1)

			// wait for time or exit
			if !amhelp.Wait(ctx, delay) {
				return // expired
			}

			// try
			amhelp.Add1Block(ctx, c.Mach, ssC.Connecting, nil)
			if ctx.Err() != nil {
				return // expired
			}

			_ = amhelp.WaitForErrAny(ctx, c.ConnTimeout*2, c.Mach,
				c.Mach.WhenNot1(ssC.Connecting, ctx))
			if ctx.Err() != nil {
				return // expired
			}
			// remover err
			c.Mach.Remove1(ssC.Exception, nil)

			// double the delay when backoff set
			if c.ConnRetryBackoff > 0 {
				delay *= 2
				if delay > c.ConnRetryBackoff {
					delay = c.ConnRetryBackoff
				}
			}

			if c.ConnRetryTimeout > 0 && time.Since(start) > c.ConnRetryTimeout {
				break
			}
		}

		// next
		if ctx.Err() != nil {
			return // expired
		}
		c.Mach.Remove1(ssC.RetryingConn, nil)
		c.Mach.Add1(ssC.ConnRetryFailed, nil)
	}()
}

func (c *Client) WorkerPayloadEnter(e *am.Event) bool {
	if c.Consumer == nil {
		return false
	}
	args := ParseArgs(e.Args)
	argsOut := &A{Name: args.Name}

	if args.Payload == nil {
		err := errors.New("invalid payload")
		c.Mach.AddErrState(ssC.ErrDelivery, err, Pass(argsOut))

		return false
	}

	return true
}

func (c *Client) WorkerPayloadState(e *am.Event) {
	args := ParseArgs(e.Args)
	argsOut := &A{
		Name:    args.Name,
		Payload: args.Payload,
	}

	c.Consumer.Add1(ssCo.WorkerPayload, Pass(argsOut))
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

// Start connects the client to the server and initializes the worker.
// Results in the Ready state.
func (c *Client) Start() am.Result {
	return c.Mach.Add(am.S{ssC.Start, ssC.Connecting}, nil)
}

// Stop disconnects the client from the server and disposes the worker.
//
// waitTillExit: if passed, waits for the client to disconnect using the
// context.
func (c *Client) Stop(waitTillExit context.Context, dispose bool) am.Result {
	res := c.Mach.Remove1(ssC.Start, nil)
	// wait for the client to disconnect
	if res != am.Canceled && waitTillExit != nil {
		// TODO timeout config
		_ = amhelp.WaitForAll(waitTillExit, 2*time.Second,
			c.Mach.When1(ssC.Disconnected, nil))
	}

	if dispose {
		c.log("disposing")
		c.Mach.Dispose()
		c.Worker.Dispose()
	}

	return res
}

// Get requests predefined data from the server's getter function.
func (c *Client) Get(ctx context.Context, name string) (*RespGet, error) {
	// callFailsafe rpc
	resp := RespGet{}
	if !c.callFailsafe(ctx, rpcnames.Get.Encode(), name, &resp) {
		return nil, c.Mach.Err()
	}

	return &resp, nil
}

// GetKind returns a kind of RPC component (server / client).
func (c *Client) GetKind() Kind {
	return KindClient
}

// ensureGroupConnected ensures that at least one state from  GroupConnected
// is active.
func (c *Client) ensureGroupConnected() {
	groupConn := states.ClientGroups.Connected
	if !c.Mach.Any(groupConn) && !c.Mach.WillBe(groupConn) {
		c.Mach.Add1(ssC.Disconnected, nil)
	}
}

// ///// ///// /////

// ///// INTERNAL

// ///// ///// /////

func (c *Client) log(msg string, args ...any) {
	if !c.LogEnabled {
		return
	}
	c.Mach.Log(msg, args...)
}

func (c *Client) bindRpcHandlers(conn net.Conn) {
	c.log("new rpc2 client")

	c.rpc = rpc2.NewClient(conn)
	c.rpc.Handle(rpcnames.ClientSetClock.Encode(), c.RemoteSetClock)
	c.rpc.Handle(rpcnames.ClientPushAllTicks.Encode(), c.RemotePushAllTicks)
	c.rpc.Handle(rpcnames.ClientSendPayload.Encode(), c.RemoteSendPayload)

	// wait for reply on each req
	c.rpc.SetBlocking(true)
}

func (c *Client) updateClock(msg ClockMsg, t am.Time) {
	if c.Mach.Not1(ssC.HandshakeDone) {
		return
	}

	c.log("updateClock %v %v", msg, t)

	// lock the worker
	c.Worker.clockMx.Lock()
	var clock am.Time
	if msg != nil {
		// diff clock update
		clock = ClockFromMsg(c.Worker.machTime, msg)
	} else {
		// full clock update
		clock = t
	}

	// err
	if clock == nil {
		c.Worker.clockMx.Unlock()
		return
	}

	var sum uint64
	for _, v := range clock {
		sum += v
	}

	if msg != nil {
		c.log("updateClock from msg %dt: %v", sum, msg)
	} else {
		c.log("updateClock full %d: %v", sum, t)
	}

	c.Worker.updateClock(clock)
}

func (c *Client) callFailsafe(
	ctx context.Context, method string, args, resp any,
) bool {
	mName := rpcnames.Decode(method).String()

	// validate
	if c.rpc == nil {
		AddErrNoConn(c.Mach, errors.New(mName))
		return false
	}

	// concurrency
	c.callLock.Lock()
	defer c.callLock.Unlock()

	// success path
	if c.call(ctx, method, args, resp, 0) {
		return true
	}

	// failure, retry
	start := time.Now()
	worked := false
	delay := c.CallRetryDelay
	c.Mach.Add1(ssC.RetryingCall, Pass(&A{
		Method:    mName,
		StartedAt: start,
	}))

	// cleanup
	defer func() {
		if worked {
			c.Mach.Remove1(ssC.RetryingCall, nil)
		} else {
			c.Mach.Add1(ssC.CallRetryFailed, Pass(&A{Method: mName}))
		}
	}()

	// retry loop
	for i := 0; i < c.CallRetries; i++ {
		// wait for time or exit
		if !amhelp.Wait(ctx, delay) {
			return false
		}

		// wait for state or exit
		<-c.Mach.When1(ssC.Ready, ctx)
		if ctx.Err() != nil {
			return false // expired
		}

		// call, again
		if c.call(ctx, method, args, resp, 0) {
			worked = true
			return true
		}

		// double the delay when backoff set
		if c.CallRetryBackoff > 0 {
			delay *= 2
			if delay > c.CallRetryBackoff {
				delay = c.CallRetryBackoff
			}
		}

		if c.CallRetryTimeout > 0 && time.Since(start) > c.CallRetryTimeout {
			break
		}
	}

	// fail
	return false
}

func (c *Client) call(
	ctx context.Context, method string, args, resp any, timeout time.Duration,
) bool {
	defer c.Mach.PanicToErr(nil)
	mName := rpcnames.Decode(method).String()

	// call
	c.CallCount++
	if timeout == 0 {
		timeout = c.CallTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := c.rpc.CallWithContext(ctx, method, args, resp)
	if ctx.Err() != nil {
		return false // expired
	}

	// err timeout
	if callCtx.Err() != nil {
		c.Mach.AddErrState(ssC.ErrNetworkTimeout, callCtx.Err(), nil)
		return false
	}
	// err test
	if c.tmpTestErr != nil {
		AddErrNetwork(c.Mach, fmt.Errorf("%w: %s", c.tmpTestErr, mName))
		c.tmpTestErr = nil
		return false
	}
	// err test
	if c.permTestErr != nil {
		AddErrNetwork(c.Mach, fmt.Errorf("%w: %s", c.tmpTestErr, mName))
		return false
	}
	// err
	if err != nil {
		AddErr(c.Mach, mName, err)
		return false
	}

	return true
}

func (c *Client) notifyFailsafe(
	ctx context.Context, method string, args any,
) bool {
	mName := rpcnames.Decode(method).String()

	// validate
	if c.rpc == nil {
		AddErrNoConn(c.Mach, errors.New(mName))
		return false
	}

	// concurrency
	c.callLock.Lock()
	defer c.callLock.Unlock()

	// success path
	if c.notify(ctx, method, args) {
		return true
	}

	// failure, retry
	start := time.Now()
	worked := false
	delay := c.CallRetryDelay
	c.Mach.Add1(ssC.RetryingCall, Pass(&A{
		Method:    mName,
		StartedAt: start,
	}))

	// cleanup
	defer func() {
		if worked {
			c.Mach.Remove1(ssC.RetryingCall, nil)
		} else {
			c.Mach.Add1(ssC.CallRetryFailed, Pass(&A{Method: mName}))
		}
	}()

	// retry loop
	for i := 0; i < c.CallRetries; i++ {
		time.Sleep(delay)

		// call, again
		if c.notify(ctx, method, args) {
			return true
		}

		// double the delay when backoff set
		if c.CallRetryBackoff > 0 {
			delay *= 2
			if delay > c.CallRetryBackoff {
				delay = c.CallRetryBackoff
			}
		}

		if c.CallRetryTimeout > 0 && time.Since(start) > c.CallRetryTimeout {
			break
		}
	}

	// fail
	return false
}

func (c *Client) notify(
	ctx context.Context, method string, args any,
) bool {
	defer c.Mach.PanicToErr(nil)
	mName := rpcnames.Decode(method).String()

	// timeout
	err := c.conn.SetDeadline(time.Now().Add(c.CallTimeout))
	if err != nil {
		AddErr(c.Mach, mName, err)
		return false
	}

	// call
	c.CallCount++
	err = c.rpc.Notify(method, args)
	if ctx.Err() != nil {
		return false // expired
	}

	// err
	if err != nil {
		AddErr(c.Mach, method, err)
		return false
	}

	// remove timeout
	err = c.conn.SetDeadline(time.Time{})
	if err != nil {
		AddErr(c.Mach, mName, err)
		return false
	}

	return true
}

// ///// ///// /////

// ///// REMOTE METHODS

// ///// ///// /////

// RemoteSetClock updates the client's clock. Only called by the server.
func (c *Client) RemoteSetClock(
	_ *rpc2.Client, clock ClockMsg, _ *Empty,
) error {
	// validate
	if clock == nil {
		AddErrParams(c.Mach, nil)
		return nil
	}

	// execute
	c.updateClock(clock, nil)

	return nil
}

// RemotePushAllTicks log all the machine clock's ticks, so all final handlers
// can be executed in order. Only called by the server.
func (c *Client) RemotePushAllTicks(
	_ *rpc2.Client, clocks []PushAllTicks, _ *Empty,
) error {
	// TODO implement, test

	for _, push := range clocks {
		// validate
		if push.ClockMsg == nil || push.Mutation == nil {
			AddErrParams(c.Mach, nil)
			return nil
		}

		// execute TODO
		// c.updateClock(clock, nil)
	}

	return nil
}

// RemoteSendingPayload triggers the WorkerDelivering state, which is an
// optional indication that the server has started a data transmission to the
// Client. This payload shouldn't contain the data itself, only the name and
// token.
func (c *Client) RemoteSendingPayload(
	_ *rpc2.Client, payload *ArgsPayload, _ *Empty,
) error {
	// TODO test
	c.log("RemoteSendingPayload %s", payload.Name)
	c.Mach.Add1(ssC.WorkerDelivering, Pass(&A{
		Payload: payload,
		Name:    payload.Name,
	}))

	return nil
}

// RemoteSendPayload receives a payload from the server and triggers
// WorkDelivered. The Consumer should bind his handlers and handle this state to
// receive the data.
func (c *Client) RemoteSendPayload(
	_ *rpc2.Client, payload *ArgsPayload, _ *Empty,
) error {
	c.log("RemoteSendPayload %s:%s", payload.Name, payload.Token)
	c.Mach.Add1(ssC.WorkerPayload, Pass(&A{
		Payload: payload,
		Name:    payload.Name,
	}))

	return nil
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

type ClientOpts struct {
	// PayloadState is a state for the server to listen on, to deliver payloads
	// to the client. The client adds this state to request a payload from the
	// worker. Default: am/rpc/states/WorkerStates.SendPayload.
	Consumer *am.Machine
	// Parent is a parent state machine for a new Client state machine. See
	// [am.Opts].
	Parent am.Api
}

// GetClientId returns a machine ID from a name. This ID will be used to
// handshake the server.
func GetClientId(name string) string {
	return "rc-" + name
}