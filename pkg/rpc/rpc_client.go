package rpc

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/rpc2"

	"github.com/pancsta/asyncmachine-go/internal/utils"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

var (
	ssC  = states.ClientStates
	ssCo = states.ConsumerStates
)

// Client is a type representing an RPC client that interacts with a remote
// am.Machine instance.
type Client struct {
	*ExceptionHandler

	Mach *am.Machine
	Name string

	// Addr is the address the Client will connect to.
	Addr string
	// NetMach is a remote am.Machine instance
	NetMach *NetworkMachine
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

	// sync settings (read-only)

	// Skip schema synchronization / fetching.
	SyncNoSchema bool
	// Synchronize machine times for every mutation (within a single sync msg).
	SyncAllMutations bool
	// Only sync selected states.
	SyncAllowedStates am.S
	// Skip syncing of these states.
	SyncSkippedStates am.S
	// Only activete/deactivate (0-1) clock values will be sent.
	SyncShallowClocks     bool
	SyncMutationFiltering bool

	// failsafe - connection (writable when stopped)

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

	// failsafe - calls (writable when stopped)

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
	DisconnTimeout   time.Duration

	// internal

	netMachInt *NetMachInternal
	// locks processing of the received clock updates (mutation queue)
	lockQueue sync.Mutex
	// locks calling the server
	callLock sync.Mutex
	rpc      atomic.Pointer[rpc2.Client]
	// schema of the network machine
	schema am.Schema
	conn   net.Conn
	// tmpTestErr is an error to return on the next call or notify, only for
	// testing.
	tmpTestErr error
	// permTestErr is an error to return on the next call or notify, only for
	// testing.
	permTestErr    error
	connRetryRound atomic.Int32
	trackedStates  am.S
	// tracked idx -> machine idx
	trackedStateIdxs []int
}

// interfaces
var (
	_ clientRpcMethods    = &Client{}
	_ clientServerMethods = &Client{}
)

// NewClient creates a new RPC client and exposes a remote state machine as
// a remote worker, with a subst of the API under Client.NetMach. Optionally
// takes a consumer, which is a state machine with a WorkerPayload state. See
// states.ConsumerStates.
func NewClient(
	ctx context.Context, netSrcAddr string, name string, netSrcSchema am.Schema,
	opts *ClientOpts,
) (*Client, error) {
	// defaults
	if name == "" {
		name = "rpc"
	}
	if opts == nil {
		opts = &ClientOpts{}
	}

	// validate
	if netSrcAddr == "" {
		return nil, errors.New("rpcc: workerAddr required")
	}
	if len(netSrcSchema) == 0 && !opts.NoSchema {
		return nil, errors.New("rpcc: schema or opts.NoSchema required")
	}

	c := &Client{
		Name:             name,
		ExceptionHandler: &ExceptionHandler{},
		LogEnabled:       os.Getenv(EnvAmRpcLogClient) != "",
		Addr:             netSrcAddr,
		CallTimeout:      3 * time.Second,
		ConnTimeout:      3 * time.Second,
		DisconnTimeout:   3 * time.Second,
		DisconnCooldown:  10 * time.Millisecond,
		ReconnectOn:      true,

		SyncNoSchema:          opts.NoSchema,
		SyncAllowedStates:     opts.AllowedStates,
		SyncSkippedStates:     opts.SkippedStates,
		SyncAllMutations:      opts.SyncMutations,
		SyncShallowClocks:     opts.SyncShallowClocks,
		SyncMutationFiltering: opts.MutationFiltering,

		ConnRetryTimeout: 1 * time.Minute,
		ConnRetries:      15,
		ConnRetryDelay:   100 * time.Millisecond,
		ConnRetryBackoff: 3 * time.Second,

		CallRetryTimeout: 1 * time.Minute,
		CallRetries:      15,
		CallRetryDelay:   100 * time.Millisecond,
		CallRetryBackoff: 3 * time.Second,

		schema: am.CloneSchema(netSrcSchema),
	}

	if amhelp.IsDebug() {
		c.CallTimeout = 100 * time.Second
	}

	// state machine
	mach, err := am.NewCommon(ctx, GetClientId(name), states.ClientSchema,
		ssC.Names(), c, opts.Parent, &am.Opts{Tags: []string{
			"rpc-client",
			"addr:" + netSrcAddr,
		}})
	if err != nil {
		return nil, err
	}
	mach.SemLogger().SetArgsMapper(LogArgs)
	c.Mach = mach
	// optional env debug
	if os.Getenv(EnvAmRpcDbg) != "" {
		amhelp.MachDebugEnv(mach)
	}

	// TODO debug
	// mach.AddBreakpoint(nil, am.S{ssC.Disconnected})

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
	ctx := c.Mach.NewStateCtx(ssC.Start)

	// init net mach
	nmConn := &clientNetMachConn{rpc: c}
	// tmp state names
	stateNames := slices.Collect(maps.Keys(c.schema))
	id := PrefixNetMach + utils.RandId(5)
	netMach, nmInternal, err := NewNetworkMachine(ctx, id, nmConn, c.schema,
		stateNames, c.Mach, nil, c.SyncMutationFiltering)
	if err != nil {
		c.Mach.AddErr(err, nil)
		return
	}
	c.NetMach = netMach
	c.netMachInt = nmInternal
}

func (c *Client) StartEnd(e *am.Event) {
	// gather state from before the transition
	before := e.Transition().TimeBefore
	idx := e.Machine().Index1

	// if never connected, stop here
	if before.Is([]int{idx(ssC.Connecting), idx(ssC.Exception)}) {
		return
	}

	// graceful disconnect
	wasConn := before.Is1(idx(ssC.Connecting)) || before.Is1(idx(ssC.Connected))
	if wasConn {
		c.Mach.EvAdd1(e, ssC.Disconnecting, nil)
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
			c.Mach.EvAdd1(e, ssC.Disconnected, nil)
			AddErrNetwork(e, c.Mach, err)
			return
		}
		c.conn = conn

		// rpc
		client := c.bindRpcHandlers(conn)
		go client.Run()

		c.Mach.EvAdd1(e, ssC.Connected, nil)
	}()
}

func (c *Client) DisconnectingEnter(e *am.Event) bool {
	return c.rpc.Load() != nil && c.conn != nil
}

func (c *Client) DisconnectingState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ssC.Disconnecting)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// notify the server and wait a bit
		c.notify(ctx, ServerBye.Value, &MsgEmpty{})
		if !amhelp.Wait(ctx, c.DisconnCooldown) {
			c.ensureGroupConnected(e)

			return // expired
		}

		// close with timeout
		if c.rpc.Load() != nil {
			select {
			case <-time.After(c.DisconnTimeout):
				c.log("rpc.Close timeout")
			case <-amhelp.ExecAndClose(func() {
				_ = c.rpc.Load().Close()
			}):
				c.log("rpc.Close")
			case <-ctx.Done():
				// expired
			}
		}
		if ctx.Err() != nil {
			c.ensureGroupConnected(e)

			return // expired
		}

		c.Mach.EvAdd1(e, ssC.Disconnected, nil)
	}()
}

func (c *Client) ConnectedState(e *am.Event) {
	ctx := c.Mach.NewStateCtx(ssC.Connected)
	disconnCh := c.rpc.Load().DisconnectNotify()
	// reset reconn counter
	c.connRetryRound.Store(0)

	// loop
	go func() {
		select {

		case <-ctx.Done():
			return // expired

		case <-disconnCh:
			c.log("rpc.DisconnectNotify")
			c.Mach.EvAdd1(e, ssC.Disconnected, nil)
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
	if wasAny(c.Mach.Index1(ssC.Connected), c.Mach.Index1(ssC.Connecting)) &&
		c.ReconnectOn {

		c.Mach.EvAdd1(e, ssC.RetryingConn, nil)
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
		if ctx.Err() != nil {
			return // expired
		}

		// send hello or retry conn
		resp := &MsgSrvHello{}
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
			msg := MsgCliHello{
				Id:            c.Mach.Id(),
				SyncSchema:    !c.SyncNoSchema,
				SyncMutations: c.SyncAllMutations,
				ShallowClocks: c.SyncShallowClocks,
				SkippedStates: c.SyncSkippedStates,
				AllowedStates: c.SyncAllowedStates,
			}
			if !c.SyncNoSchema {
				msg.SyncSchema = true
				msg.SchemaHash = amhelp.SchemaHash(c.NetMach.Schema())
			}

			if c.call(ctx, ServerHello.Value, msg, resp, timeout) {
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
			c.Mach.EvAdd1(e, ssC.RetryingConn, nil)
			return
		}

		// validate
		stateNames := resp.Serialized.StateNames
		if len(stateNames) == 0 {
			AddErrRpcStr(e, c.Mach, "states missing")
			return
		}
		if resp.Serialized.ID == "" {
			AddErrRpcStr(e, c.Mach, "ID missing")
			return
		}
		if c.NetMach.Schema() == nil && resp.Schema == nil && !c.SyncNoSchema {
			AddErrRpcStr(e, c.Mach, "schema missing")
			return
		}

		// set schema and states
		c.updateStatesSchema(resp)

		// optional env debug on 1st call
		if c.Mach.Tick(ssC.Handshaking) == 1 && os.Getenv(EnvAmRpcDbg) != "" {
			amhelp.MachDebugEnv(c.NetMach)
		}

		// ID as tag TODO find-n-replace the tag, not via index [1]
		c.NetMach.tags[1] = "src-id:" + resp.Serialized.ID
		// TODO setter
		c.NetMach.remoteId = resp.Serialized.ID

		// confirm the handshake or retry conn
		if !c.call(ctx, ServerHandshake.Value, &MsgEmpty{}, &MsgEmpty{}, 0) {
			c.Mach.EvAdd1(e, ssC.RetryingConn, nil)
			return
		}

		// finalize
		c.Mach.EvAdd1(e, ssC.HandshakeDone, Pass(&A{
			Id:        resp.Serialized.ID,
			MachTime:  resp.Serialized.Time,
			QueueTick: resp.Serialized.QueueTick,
		}))
	}()
}

func (c *Client) HandshakeDoneEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a.Id != "" && a.MachTime != nil && a.QueueTick > 0
}

func (c *Client) HandshakeDoneState(e *am.Event) {
	args := ParseArgs(e.Args)

	// finalize the worker init
	netMach := c.NetMach
	netMach.id = PrefixNetMach + c.Name
	c.clockSet(args.MachTime, args.QueueTick, args.MachTick)

	c.log("connected to %s", netMach.remoteId)
	c.log("time t%d q%d: %v",
		netMach.Time(nil).Sum(nil), netMach.QueueTick(), args.MachTime)
}

func (c *Client) CallRetryFailedState(e *am.Event) {
	c.Mach.EvRemove1(e, ssC.CallRetryFailed, nil)

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
	c.Mach.EvRemove1(e, am.StateException, nil)
	// TODO handle am.ErrSchema:
	//  "worker has to implement pkg/rpc/states/NetSourceStatesDef"
	//  only for nondeterministic machs
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
			amhelp.Add1Sync(ctx, c.Mach, ssC.Connecting, nil)
			if ctx.Err() != nil {
				return // expired
			}

			_ = amhelp.WaitForErrAny(ctx, c.ConnTimeout*2, c.Mach,
				c.Mach.WhenNot1(ssC.Connecting, ctx))
			if ctx.Err() != nil {
				return // expired
			}
			// remover err
			c.Mach.EvRemove1(e, ssC.Exception, nil)

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
		c.Mach.EvRemove1(e, ssC.RetryingConn, nil)
		c.Mach.EvAdd1(e, ssC.ConnRetryFailed, nil)
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

	c.Consumer.EvAdd1(e, ssCo.WorkerPayload, Pass(argsOut))
}

func (c *Client) HealthcheckState(e *am.Event) {
	c.Mach.EvRemove1(e, ssC.Healthcheck, nil)
	c.ensureGroupConnected(e)
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
		c.NetMach.Dispose()
	}

	return res
}

// IsPartial is true for NetMachs syncing only a subset of the Net Source's
// states.
func (c *Client) IsPartial() bool {
	return len(c.SyncSkippedStates) > 0 || len(c.SyncSkippedStates) > 0
}

// GetKind returns a kind of the RPC component (server / client).
func (c *Client) GetKind() Kind {
	return KindClient
}

// Sync requests non-diff clock values from the remote machine. Useful to call
// after a batch of no-sync methods, eg [NetworkMachine.AddNS]. Sync doesn't
// honor [ClientOpts.SyncMutations] and only returns clock values (so can be
// used to skip mutation syncing within a period).
func (c *Client) Sync() am.Time {
	c.Mach.Add1(ssC.MetricSync, nil)

	// call rpc
	resp := &MsgSrvSync{}
	ok := c.callFailsafe(c.Mach.Ctx(), ServerSync.Value, &MsgEmpty{}, resp)
	if !ok {
		return nil
	}

	// validate
	if len(resp.Time) > 0 && len(resp.Time) != len(c.NetMach.StateNames()) {
		AddErrRpcStr(nil, c.Mach, "wrong clock len")

		return nil
	}

	// process
	c.clockSet(resp.Time, resp.QueueTick, resp.MachTick)

	return c.NetMach.machTime
}

// ///// ///// /////

// ///// INTERNAL

// ///// ///// /////

func (c *Client) updateStatesSchema(resp *MsgSrvHello) {
	netMach := c.NetMach

	// locks
	netMach.schemaMx.Lock()
	defer netMach.schemaMx.Unlock()
	netMach.clockMx.Lock()
	defer netMach.clockMx.Unlock()

	// optional schema
	if resp.Schema != nil {
		c.schema = resp.Schema
		netMach.schema = resp.Schema
	}

	// update states and time
	netMach.stateNames = resp.Serialized.StateNames
	netMach.queueTick = resp.Serialized.QueueTick
	netMach.machTime = resp.Serialized.Time
	for idx, state := range netMach.stateNames {
		netMach.machClock[state] = netMach.machTime[idx]
	}
	netMach.subs.SetClock(netMach.machClock)
	c.log("registered %d states", len(netMach.stateNames))

	// calculate tracked states
	c.trackedStates = netMach.stateNames
	if c.SyncAllowedStates != nil {
		c.trackedStates = am.StatesShared(c.trackedStates, c.SyncAllowedStates)
	}
	c.trackedStates = am.StatesDiff(c.trackedStates, c.SyncSkippedStates)

	// indexes
	c.trackedStateIdxs = make([]int, len(c.trackedStates))
	for i, name := range c.trackedStates {
		c.trackedStateIdxs[i] = slices.Index(netMach.stateNames, name)
	}
}

// clockFromUpdate returns after-values of:
// - machine time
// - queue tick
// - machine tick
func (c *Client) clockFromUpdate(
	update *MsgSrvUpdate, timeBefore am.Time, qTickBefore uint64,
	machTickBefore uint32,
) (am.Time, uint64, uint32) {
	// TODO validate length indexes ticks

	// calculate
	timeAfter := slices.Clone(timeBefore)
	l := uint16(len(timeAfter))

	for i, idx := range update.Indexes {
		val := update.Ticks[i]
		if idx >= l {
			// TODO err states mising
			continue
		}
		timeAfter[idx] += uint64(val)
	}
	qTickAfter := qTickBefore + uint64(update.QueueTick)
	machTickAfter := machTickBefore + uint32(update.MachTick)

	return timeAfter, qTickAfter, machTickAfter
}

// ensureGroupConnected ensures that at least one state from  GroupConnected
// is active.
func (c *Client) ensureGroupConnected(e *am.Event) {
	groupConn := states.ClientGroups.Connected
	if !c.Mach.Any1(groupConn...) && !c.Mach.WillBe(groupConn) {
		c.Mach.EvAdd1(e, ssC.Disconnected, nil)
	}
}

func (c *Client) log(msg string, args ...any) {
	if !c.LogEnabled {
		return
	}
	c.Mach.Log(msg, args...)
}

func (c *Client) bindRpcHandlers(conn net.Conn) *rpc2.Client {
	c.log("new rpc2 client")

	client := rpc2.NewClient(conn)
	client.Handle(ClientUpdate.Value, c.RemoteUpdate)
	client.Handle(ClientUpdateMutations.Value, c.RemoteUpdateMutations)
	client.Handle(ClientSendPayload.Value, c.RemoteSendPayload)
	client.Handle(ClientBye.Value, c.RemoteBye)
	client.Handle(ClientSchemaChange.Value, c.RemoteSchemaChange)

	// wait for reply on each req
	client.SetBlocking(true)

	c.rpc.Store(client)
	return client
}

func (c *Client) clockSet(mTime am.Time, qTick uint64, machTick uint32) {
	if c.Mach.Not1(ssC.HandshakeDone) {
		return
	}

	c.lockQueue.Lock()
	defer c.lockQueue.Unlock()

	// err
	if mTime == nil {
		// TODO log?
		return
	}

	var sum uint64
	for _, v := range mTime {
		sum += v
	}

	c.log("clockUpdate full OK t%d q%d", sum, qTick)
	c.netMachInt.Lock()
	c.netMachInt.UpdateClock(mTime, qTick, machTick)
}

// clockUpdate tries to update the lock from a diff and returns false in case
// of a clock drift
func (c *Client) clockUpdate(update *MsgSrvUpdate, queueLocked bool) bool {
	if c.Mach.Not1(ssC.HandshakeDone) {
		return true
	}

	// may be locked by clockUpdateMutations
	if !queueLocked {
		c.lockQueue.Lock()
		defer c.lockQueue.Unlock()
	}

	// lock the netmach
	c.netMachInt.Lock()

	// diff clock update
	// fmt.Printf("[C] update %v\n", update)
	netMach := c.NetMach
	mTime, qTick, machTick := c.clockFromUpdate(update, netMach.machTime,
		netMach.queueTick, netMach.machTick)
	// fmt.Printf("[C] time %v\n", mTime)

	checksumTime := mTime
	if c.SyncShallowClocks {
		checksumTime = am.NewTime(checksumTime, c.trackedStateIdxs)
	}
	check := Checksum(checksumTime.Sum(nil), qTick, machTick)

	// verify
	// fmt.Printf("[C:before] %d %d %d\n", netMach.machTime.Sum(nil),
	//   netMach.queueTick, netMach.machTick)
	// fmt.Printf("[C:after] %d %d %d\n", mTime.Sum(nil), qTick, machTick)
	// fmt.Printf("[C:update] %v %d %d\n", update.Ticks, update.QueueTick,
	//   update.MachTick)
	if check != update.Checksum {
		// fmt.Printf("[C] check %d != %d\n", update.Checksum, check)
		c.Mach.Log("clockUpdate mismatch %d != %d", update.Checksum, check)
		c.log("msg q%d m%d ch%d %+v", update.QueueTick, update.MachTick,
			update.Checksum, update.Indexes)
		c.log("clock t%d q%d m%d ch%d (%+v)", mTime.Sum(nil), qTick,
			machTick, check, mTime)

		// request full sync
		netMach.clockMx.Unlock()
		return false
	}

	// err
	if mTime == nil {
		// request full sync
		netMach.clockMx.Unlock()
		return false
	}

	c.log("clockUpdate diff OK tt%d q%d", mTime.Sum(nil), qTick)
	// will unlock itself TODO pass mutType?
	c.netMachInt.UpdateClock(mTime, qTick, machTick)

	return true
}

// clockUpdateMutations is like clockUpdate, but uses granular mutations.
func (c *Client) clockUpdateMutations(msgs *MsgSrvUpdateMuts) bool {
	if c.Mach.Not1(ssC.HandshakeDone) {
		return true
	}

	c.lockQueue.Lock()
	defer c.lockQueue.Unlock()

	for i := range msgs.Updates {
		// TODO pass mutation data
		if !c.clockUpdate(&msgs.Updates[i], true) {
			return false
		}
	}

	return true
}

func (c *Client) callFailsafe(
	ctx context.Context, method string, args, resp any,
) bool {
	mName := ServerMethods.Parse(method).Value

	// validate
	if c.rpc.Load() == nil {
		AddErrNoConn(nil, c.Mach, errors.New(mName))
		return false
	}

	// locks
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
	mName := ServerMethods.Parse(method).Value

	// call
	c.CallCount++
	if timeout == 0 {
		timeout = c.CallTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := c.rpc.Load().CallWithContext(ctx, method, args, resp)
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
		AddErrNetwork(nil, c.Mach, fmt.Errorf("%w: %s", c.tmpTestErr, mName))
		c.tmpTestErr = nil
		return false
	}
	// err test
	if c.permTestErr != nil {
		AddErrNetwork(nil, c.Mach, fmt.Errorf("%w: %s", c.tmpTestErr, mName))
		return false
	}
	// err
	if err != nil {
		// TODO specific err?
		AddErr(nil, c.Mach, mName, err)
		return false
	}

	return true
}

func (c *Client) notifyFailsafe(
	ctx context.Context, method string, args any,
) bool {
	mName := ServerMethods.Parse(method).Value

	// validate
	if c.rpc.Load() == nil {
		AddErrNoConn(nil, c.Mach, errors.New(mName))
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
	mName := ServerMethods.Parse(method).Value

	// timeout
	err := c.conn.SetDeadline(time.Now().Add(c.CallTimeout))
	if err != nil {
		AddErr(nil, c.Mach, mName, err)
		return false
	}

	// call
	c.CallCount++
	err = c.rpc.Load().Notify(method, args)
	if ctx.Err() != nil {
		return false // expired
	}

	// err
	if err != nil {
		AddErr(nil, c.Mach, method, err)
		return false
	}

	// remove timeout
	err = c.conn.SetDeadline(time.Time{})
	if err != nil {
		AddErr(nil, c.Mach, mName, err)
		return false
	}

	return true
}

// ///// ///// /////

// ///// REMOTE METHODS

// ///// ///// /////

// RemoteUpdate updates the clock of NetMach from a cumulative diff. Only
// called by the server.
func (c *Client) RemoteUpdate(
	_ *rpc2.Client, update *MsgSrvUpdate, _ *MsgEmpty,
) error {
	// validate
	if update == nil {
		AddErrParams(nil, c.Mach, nil)
		return nil
	}

	// execute or fallback
	c.clockUpdate(update, false)

	return nil
}

// RemoteUpdateMutations updates the clock of NetMach from a list of mutations.
// Only called by the server.
func (c *Client) RemoteUpdateMutations(
	_ *rpc2.Client, updates *MsgSrvUpdateMuts, _ *MsgEmpty,
) error {
	// validate
	if updates == nil {
		AddErrParams(nil, c.Mach, nil)
		return nil
	}

	// execute or fallback
	if !c.clockUpdateMutations(updates) {
		c.Sync()
	}

	return nil
}

// RemoteSendingPayload triggers the WorkerDelivering state, which is an
// optional indication that the server has started a data transmission to the
// Client. This payload shouldn't contain the data itself, only the name and
// token.
func (c *Client) RemoteSendingPayload(
	_ *rpc2.Client, payload *MsgSrvPayload, _ *MsgEmpty,
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
// WorkerPayload. The Consumer should bind his handlers and handle this state to
// receive the data.
func (c *Client) RemoteSendPayload(
	_ *rpc2.Client, payload *MsgSrvPayload, _ *MsgEmpty,
) error {
	c.log("RemoteSendPayload %s:%s", payload.Name, payload.Token)
	c.Mach.Add1(ssC.WorkerPayload, Pass(&A{
		Payload: payload,
		Name:    payload.Name,
	}))

	return nil
}

// RemoteBye is called by the server on a planned disconnect.
// TODO take a reason / source event?
func (c *Client) RemoteBye(
	_ *rpc2.Client, _ *MsgEmpty, _ *MsgEmpty,
) error {
	// TODO check if this expected / covers all scenarios
	c.Mach.Remove1(ssC.Start, nil)
	return nil
}

// RemoteSchemaChange is called by the server on a source machine schema change.
func (c *Client) RemoteSchemaChange(
	_ *rpc2.Client, msg *MsgSrvHello, _ *MsgEmpty,
) error {
	c.log("new schema v" + strconv.Itoa(len(msg.Serialized.StateNames)))
	c.updateStatesSchema(msg)
	return nil
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

type ClientOpts struct {
	// Consumer is an optional target for the [states.SendPayload] state.
	Consumer *am.Machine
	// Parent is a parent state machine for a new Client state machine. See
	// [am.Opts].
	Parent am.Api
	// Make this client schema-less (infer an empty one for tracked states).
	NoSchema bool
	// Only sync selected states.
	AllowedStates am.S
	// Skip syncing of these states.
	SkippedStates am.S
	// Sync machine time for every mutation. Disables
	// [ClientOpts.SyncShallowClocks].
	SyncMutations bool
	// Only activete/deactivate (0-1) clock values will be sent.
	SyncShallowClocks bool
	// Enable client-side mutation filtering by performing relations resolution
	// based on locally active states. Doesn't work with [ClientOpts.NoSchema].
	// TODO not implemented yet
	MutationFiltering bool
}

// GetClientId returns an RPC Client machine ID from a name. This ID will be
// used to handshake the server.
func GetClientId(name string) string {
	return "rc-" + name
}

// clientNetMachConn connects exposes the RPC client conn to the composed
// NetworkMachine.
type clientNetMachConn struct {
	rpc *Client
}

func (c clientNetMachConn) Call(
	ctx context.Context, method ServerMethod, args any, resp any,
) bool {
	if !c.rpc.callFailsafe(ctx, method.Value, args, resp) {
		return false
	}

	switch method {
	case ServerAdd:
		fallthrough
	case ServerSet:
		fallthrough
	case ServerRemove:
		mutResp, ok := resp.(*MsgSrvMutation)
		if !ok {
			c.rpc.Mach.AddErr(errors.New("parsing resp.(*MsgSrvMutation)"), nil)
			return false
		}
		// support mutations
		synced := false
		if c.rpc.SyncAllMutations {
			synced = c.rpc.clockUpdateMutations(mutResp.Mutations)
		} else {
			synced = c.rpc.clockUpdate(mutResp.Update, false)
		}
		if !synced {
			c.rpc.Sync()
		}
	}

	return true
}

func (c clientNetMachConn) Notify(
	ctx context.Context, method ServerMethod, args any,
) bool {
	return c.rpc.notifyFailsafe(ctx, method.Value, args)
}

var _ NetMachConn = clientNetMachConn{}
