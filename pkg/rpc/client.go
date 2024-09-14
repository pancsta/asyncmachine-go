package rpc

import (
	"context"
	"maps"
	"net"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/pancsta/rpc2"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/rpcnames"
	ss "github.com/pancsta/asyncmachine-go/pkg/rpc/states/client"
)

type Client struct {
	*ExceptionHandler

	Mach *am.Machine
	// Worker is a remote Worker instance
	Worker      *Worker
	Payloads    map[string]*ArgsPayload
	CallCount   uint64
	LogEnabled  bool
	CallTimeout time.Duration

	clockMx     sync.Mutex
	workerAddr  string
	rpc         *rpc2.Client
	stateNames  am.S
	stateStruct am.Struct
	conn        net.Conn
}

// interfaces
var (
	_ clientRpcMethods    = &Client{}
	_ clientServerMethods = &Client{}
)

func NewClient(
	ctx context.Context, workerAddr string, id string, stateStruct am.Struct,
	stateNames am.S,
) (*Client, error) {
	if id == "" {
		id = "rpc"
	}

	c := &Client{
		ExceptionHandler: &ExceptionHandler{},
		Payloads:         map[string]*ArgsPayload{},
		LogEnabled:       os.Getenv("AM_RPC_LOG_CLIENT") != "",
		CallTimeout:      3 * time.Second,

		workerAddr:  workerAddr,
		stateNames:  slices.Clone(stateNames),
		stateStruct: maps.Clone(stateStruct),
	}

	if os.Getenv("AM_DEBUG") != "" {
		c.CallTimeout = 100 * time.Second
	}

	// state machine
	mach, err := am.NewCommon(ctx, "c-"+id, ss.States, ss.Names,
		c, nil, nil)
	if err != nil {
		return nil, err
	}
	c.Mach = mach

	return c, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (c *Client) StartEnd(e *am.Event) {
	// gather state from before the transition
	before := e.Transition().TimeBefore
	mach := e.Machine
	wasConn := before.Is1(mach.Index(ss.Connecting)) ||
		before.Is1(mach.Index(ss.Connected))

	// graceful disconnect
	if wasConn {
		c.Mach.Add1(ss.Disconnecting, nil)
	}
}

func (c *Client) ConnectingState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ss.Connecting)

	// async
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// net dial
		timeout := 1 * time.Second
		if os.Getenv("AM_DEBUG") != "" || os.Getenv("AM_TEST") != "" {
			timeout = 100 * time.Second
		}
		// TODO TLS
		d := net.Dialer{
			Timeout: timeout,
		}
		conn, err := d.DialContext(ctx, "tcp", c.workerAddr)
		if err != nil {
			errNetwork(c.Mach, err)
			return
		}
		c.conn = conn

		// rpc
		c.bindRpcHandlers(conn)
		go c.rpc.Run()

		c.Mach.Add1(ss.Connected, nil)
	}()
}

func (c *Client) DisconnectingEnter(e *am.Event) bool {
	return c.rpc != nil && c.conn != nil
}

func (c *Client) DisconnectingState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ss.Disconnecting)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		c.notify(ctx, rpcnames.Bye.Encode(), &Empty{})
		time.Sleep(1 * time.Second)

		c.Mach.Add1(ss.Disconnected, nil)
	}()
}

func (c *Client) ConnectedState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ss.Connected)

	go func() {
		select {

		case <-ctx.Done():
			return // expired

		case <-c.rpc.DisconnectNotify():
			c.Mach.Add1(ss.Disconnecting, nil)
		}
	}()
}

func (c *Client) DisconnectedEnter(e *am.Event) bool {
	// graceful disconnect
	willDisconn := e.Machine.IsQueued(am.MutationAdd, am.S{ss.Disconnecting},
		false, false, 0)

	return willDisconn <= -1
}

func (c *Client) DisconnectedState(e *am.Event) {
	// ignore the error when disconnecting
	if c.rpc != nil {
		_ = c.rpc.Close()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *Client) HandshakingState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ss.Connected)

	go func() {
		// call rpc
		resp := &RespHandshake{}
		if !c.call(ctx, rpcnames.Handshake.Encode(), Empty{}, resp) {
			return
		}

		// validate
		if len(resp.StateNames) == 0 {
			errResponseStr(c.Mach, "states missing")
			return
		}
		if resp.ID == "" {
			errResponseStr(c.Mach, "ID missing")
			return
		}

		// compare states
		diff := am.DiffStates(c.stateNames, resp.StateNames)
		if len(diff) > 0 || len(resp.StateNames) != len(c.stateNames) {
			errResponseStr(c.Mach, "States differ on client/server")
			return
		}

		// confirm the handshake
		if !c.notify(ctx, rpcnames.HandshakeAck.Encode(), true) {
			return
		}

		// finalize
		c.Mach.Add1(ss.HandshakeDone, am.A{
			"ID":      resp.ID,
			"am.Time": resp.Time,
		})
	}()
}

func (c *Client) HandshakeDoneState(e *am.Event) {
	// TODO validate on Enter
	// TODO enum names

	id := e.Args["ID"].(string)
	clock := e.Args["am.Time"].(am.Time)

	// handshake crates the worker
	// TODO extract to NewWorker
	c.Worker = &Worker{
		c:             c,
		ID:            id,
		Ctx:           c.Mach.Ctx,
		states:        c.stateStruct,
		stateNames:    c.stateNames,
		clockTime:     clock,
		indexWhen:     am.IndexWhen{},
		indexStateCtx: am.IndexStateCtx{},
		indexWhenTime: am.IndexWhenTime{},
		whenDisposed:  make(chan struct{}),
	}
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

// Start connects the client to the server and initializes the worker.
// Results in the Ready state.
func (c *Client) Start() am.Result {
	return c.Mach.Add1(ss.Start, nil)
}

// Stop disconnects the client from the server and disposes the worker.
// waitTillExit: if passed, waits for the client to disconnect using the
// context.
func (c *Client) Stop(waitTillExit context.Context, dispose bool) am.Result {
	res := c.Mach.Remove1(ss.Start, nil)
	// wait for the client to disconnect
	if res != am.Canceled && waitTillExit != nil {
		<-c.Mach.When1(ss.Disconnected, waitTillExit)
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
	// call rpc
	resp := RespGet{}
	if !c.call(ctx, rpcnames.Get.Encode(), name, &resp) {
		return nil, c.Mach.Err()
	}

	return &resp, nil
}

// GetKind returns a kind of RPC component (server / client).
func (c *Client) GetKind() Kind {
	return KindClient
}

func (c *Client) log(msg string, args ...any) {
	if !c.LogEnabled {
		return
	}
	c.Mach.Log(msg, args...)
}

func (c *Client) bindRpcHandlers(conn net.Conn) {
	c.rpc = rpc2.NewClient(conn)
	c.rpc.Handle(rpcnames.ClientSetClock.Encode(), c.RemoteSetClock)
	c.rpc.Handle(rpcnames.ClientSendPayload.Encode(), c.RemoteSendPayload)
	// TODO check if test suite passes without it
	c.rpc.SetBlocking(true)
}

func (c *Client) updateClock(msg ClockMsg, t am.Time) {
	if c.Mach.Not1(ss.Ready) {
		return
	}

	c.clockMx.Lock()
	var clock am.Time
	if msg != nil {
		// diff clock update
		clock = ClockFromMsg(c.Worker.clockTime, msg)
	} else {
		// full clock update
		clock = t
	}

	if clock == nil {
		c.clockMx.Unlock()
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

	timeBefore := c.Worker.clockTime
	activeBefore := c.Worker.activeStatesUnlocked()
	c.Worker.clockTime = clock
	c.clockMx.Unlock()

	// process clock-based indexes
	c.Worker.processWhenBindings(activeBefore)
	c.Worker.processWhenTimeBindings(timeBefore)
	c.Worker.processStateCtxBindings(activeBefore)
}

func (c *Client) call(
	ctx context.Context, method string, args, resp any,
) bool {
	defer c.Mach.PanicToErr(nil)

	callCtx, cancel := context.WithTimeout(ctx, c.CallTimeout)
	defer cancel()

	c.CallCount++
	err := c.rpc.CallWithContext(callCtx, method, args, resp)
	if ctx.Err() != nil {
		return false // expired
	}
	if callCtx.Err() != nil {
		errAuto(c.Mach, rpcnames.Decode(method).String(), ErrNetworkTimeout)
		return false
	}

	if err != nil {
		errAuto(c.Mach, rpcnames.Decode(method).String(), err)
		return false
	}

	return true
}

func (c *Client) notify(
	ctx context.Context, method string, args any,
) bool {
	defer c.Mach.PanicToErr(nil)

	// TODO timeout

	c.CallCount++
	err := c.rpc.Notify(method, args)
	if ctx.Err() != nil {
		return false
	}
	if err != nil {
		errAuto(c.Mach, method, err)
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
		errParams(c.Mach, nil)
		return nil
	}

	// execute
	c.updateClock(clock, nil)

	return nil
}

// RemoteSendPayload receives a payload from the server. Only called by the
// server.
func (c *Client) RemoteSendPayload(
	_ *rpc2.Client, file *ArgsPayload, _ *Empty,
) error {
	// TODO test
	c.log("RemoteSendPayload %s", file.Name)
	c.Payloads[file.Name] = file

	return nil
}
