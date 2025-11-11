package rpc

// TODO call ClientBye on Disposing

import (
	"context"
	"fmt"
	"net"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/rpc2"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

var (
	ssS = states.ServerStates
	ssW = states.WorkerStates
)

// Server is an RPC server that can be bound to a worker machine and provide
// remote access to its states and methods.
type Server struct {
	*ExceptionHandler
	Mach *am.Machine

	// Source is a state Source, either a local or remote RPC worker.
	Source am.Api
	// Addr is the address of the server on the network.
	Addr string
	// DeliveryTimeout is a timeout for SendPayload to the client.
	DeliveryTimeout time.Duration
	// PushInterval is the interval for clock updates, effectively throttling
	// the number of updates sent to the client within the interval window.
	// 0 means pushes are disabled. Setting to a very small value will make
	// pushes instant.
	PushInterval time.Duration
	// PushAllTicks will push all ticks to the client, enabling client-side
	// final handlers. TODO more info, implement via a queue
	PushAllTicks bool
	// Listener can be set manually before starting the server.
	Listener atomic.Pointer[net.Listener]
	// Conn can be set manually before starting the server.
	Conn net.Conn
	// NoNewListener will prevent the server from creating a new listener if
	// one is not provided, or has been closed. Useful for cmux.
	NoNewListener bool
	LogEnabled    bool
	CallCount     uint64

	// AllowId will limit clients to a specific ID, if set.
	AllowId string

	rpcServer *rpc2.Server
	// rpcClient is the internal rpc2 client.
	rpcClient atomic.Pointer[rpc2.Client]
	clockMx   sync.Mutex
	ticker    *time.Ticker
	// mutMx is a lock preventing mutation methods from racing each other.
	mutMx         sync.Mutex
	skipClockPush atomic.Bool
	tracer        *WorkerTracer
	// ID of the currently connected client.
	clientId         atomic.Pointer[string]
	deliveryHandlers any

	// lastClockHTime is the last (human) time a clock update was sent to the
	// client.
	lastClockHTime time.Time
	lastClock      am.Time
	lastClockSum   atomic.Uint64
	lastClockMsg   *ClockMsg
	lastQueueTick  uint64
}

// interfaces
var (
	_ serverRpcMethods    = &Server{}
	_ clientServerMethods = &Server{}
)

// NewServer creates a new RPC server, bound to a worker machine.
// The source machine has to implement am/rpc/states/WorkerStatesDef interface.
func NewServer(
	ctx context.Context, addr string, name string, sourceMach am.Api,
	opts *ServerOpts,
) (*Server, error) {
	if name == "" {
		name = "rpc"
	}
	if opts == nil {
		opts = &ServerOpts{}
	}

	// check the worker
	if !sourceMach.StatesVerified() {
		return nil, fmt.Errorf("worker states not verified, call VerifyStates()")
	}
	hasHandlers := sourceMach.HasHandlers()
	if hasHandlers && !sourceMach.Has(ssW.Names()) {
		// error only when some handlers bound, skip deterministic machines
		err := fmt.Errorf(
			"%w: RPC worker with handlers has to implement "+
				"pkg/rpc/states/WorkerStatesDef",
			am.ErrSchema)

		return nil, err
	}

	s := &Server{
		ExceptionHandler: &ExceptionHandler{},
		Addr:             addr,
		PushInterval:     250 * time.Millisecond,
		DeliveryTimeout:  5 * time.Second,
		LogEnabled:       os.Getenv(EnvAmRpcLogServer) != "",
		Source:           sourceMach,

		// queue ticks start at 1
		lastQueueTick: 1,
	}
	var sum uint64
	s.lastClockSum.Store(sum)

	// state machine
	mach, err := am.NewCommon(ctx, "rs-"+name, states.ServerSchema, ssS.Names(),
		s, opts.Parent, &am.Opts{Tags: []string{"rpc-server"}})
	if err != nil {
		return nil, err
	}
	mach.SemLogger().SetArgsMapper(LogArgs)
	mach.OnDispose(func(id string, ctx context.Context) {
		if l := s.Listener.Load(); l != nil {
			_ = (*l).Close()
			s.Listener.Store(nil)
		}
		s.rpcServer = nil
		_ = s.Source.DetachTracer(s.tracer)
		_ = s.Source.DetachHandlers(s.deliveryHandlers)
	})
	s.Mach = mach
	// optional env debug
	if os.Getenv(EnvAmRpcDbg) != "" {
		amhelp.MachDebugEnv(mach)
	}

	// bind to worker via Tracer API
	s.tracer = &WorkerTracer{s: s}
	_ = sourceMach.BindTracer(s.tracer)

	// handle payload
	if hasHandlers {

		// payload state
		payloadState := ssW.SendPayload
		if opts.PayloadState != "" {
			payloadState = opts.PayloadState
		}

		// payload handlers
		var h any
		if payloadState == ssW.SendPayload {
			// default handlers
			h = &SendPayloadHandlers{
				SendPayloadState: getSendPayloadState(s, ssW.SendPayload),
			}
		} else {
			// dynamic handlers
			h = createSendPayloadHandlers(s, payloadState)
		}
		err = sourceMach.BindHandlers(h)
		if err != nil {
			return nil, err
		}
		mach.OnDispose(func(id string, ctx context.Context) {
			_ = sourceMach.DetachHandlers(h)
		})
	}

	return s, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (s *Server) StartEnd(e *am.Event) {
	if ParseArgs(e.Args).Dispose {
		s.Mach.Dispose()
	}
}

func (s *Server) RpcStartingEnter(e *am.Event) bool {
	if s.Listener.Load() == nil && s.NoNewListener {
		return false
	}
	if s.Addr == "" {
		return false
	}

	return true
}

func (s *Server) RpcStartingState(e *am.Event) {
	ctxRpcStarting := s.Mach.NewStateCtx(ssS.RpcStarting)
	ctxStart := s.Mach.NewStateCtx(ssS.Start)
	s.log("Starting RPC on %s", s.Addr)
	s.bindRpcHandlers()
	srv := s.rpcServer

	// unblock
	go func() {
		// has to be ctxStart, not ctxRpcStarting TODO why?
		if ctxStart.Err() != nil {
			return // expired
		}

		if s.Conn != nil {
			s.Addr = s.Conn.LocalAddr().String()
		} else if l := s.Listener.Load(); l != nil {
			// update Addr from listener (support for external and :0)
			s.Addr = (*l).Addr().String()
		} else {
			// create a listener if not provided
			// use Start as the context
			cfg := net.ListenConfig{}
			lis, err := cfg.Listen(ctxStart, "tcp4", s.Addr)
			if err != nil {
				// add err to mach
				AddErrNetwork(e, s.Mach, err)
				// add outcome to mach
				s.Mach.Remove1(ssS.RpcStarting, nil)

				return
			}

			s.Listener.Store(&lis)
			// update Addr from listener (support for external and :0)
			s.Addr = lis.Addr().String()
		}

		s.log("RPC started on %s", s.Addr)

		// fork to accept
		go func() {
			if ctxRpcStarting.Err() != nil {
				return // expired
			}
			s.Mach.EvAdd1(e, ssS.RpcReady, Pass(&A{Addr: s.Addr}))

			// accept (block)
			lisP := s.Listener.Load()
			if s.Conn != nil {
				srv.ServeConn(s.Conn)
			} else {
				srv.Accept(*lisP)
			}
			if ctxStart.Err() != nil {
				return // expired
			}

			// clean up
			if lisP != nil {
				(*lisP).Close()
				s.Listener.Store(nil)
			}
			if ctxStart.Err() != nil {
				return // expired
			}

			// restart on failed listener
			if s.Mach.Is1(ssS.Start) {
				s.Mach.EvRemove1(e, ssS.RpcReady, nil)
				s.Mach.EvAdd1(e, ssS.RpcStarting, nil)
			}
		}()

		// bind to client events
		srv.OnDisconnect(func(client *rpc2.Client) {
			s.Mach.EvRemove1(e, ssS.ClientConnected, Pass(&A{Client: client}))
		})
		srv.OnConnect(func(client *rpc2.Client) {
			s.Mach.EvAdd1(e, ssS.ClientConnected, Pass(&A{Client: client}))
		})
	}()
}

func (s *Server) RpcReadyEnter(e *am.Event) bool {
	// only from RpcStarting
	return s.Mach.Is1(ssS.RpcStarting)
}

// RpcReadyState starts a ticker to compensate for clock push debounces.
func (s *Server) RpcReadyState(e *am.Event) {
	// no ticker for instant clocks
	if s.PushInterval == 0 {
		return
	}

	ctx := s.Mach.NewStateCtx(ssS.RpcReady)
	if s.ticker == nil {
		s.ticker = time.NewTicker(s.PushInterval)
	}

	// avoid dispose
	t := s.ticker

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// push clock updates, debounced by genClockUpdate
		for {
			select {
			case <-ctx.Done():
				s.ticker.Stop()
				return

			case <-t.C:
				s.pushClockUpdate(false)
			}
		}
	}()
}

// TODO tell the client Bye to gracefully disconn

func (s *Server) HandshakeDoneEnd(e *am.Event) {
	if c := s.rpcClient.Load(); c != nil {
		_ = c.Close()
		// s.rpcClient = nil
	}
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

// Start starts the server, optionally creating a Listener (if Addr provided).
// Results in either RpcReady or Exception.
func (s *Server) Start() am.Result {
	return s.Mach.Add1(ssS.Start, nil)
}

// Stop stops the server, and optionally disposes resources.
func (s *Server) Stop(dispose bool) am.Result {
	if s.Mach == nil {
		return am.Canceled
	}
	if dispose {
		s.log("disposing")
	}
	// TODO use Disposing
	res := s.Mach.Remove1(ssS.Start, Pass(&A{
		Dispose: dispose,
	}))

	return res
}

// SendPayload sends a payload to the client. It's usually called by a handler
// for SendPayload.
func (s *Server) SendPayload(
	ctx context.Context, event *am.Event, payload *ArgsPayload,
) error {
	// TODO add SendPayloadAsync calling RemoteSendingPayload first
	// TODO bind to an async state

	if s.Mach.Not1(ssS.ClientConnected) || s.Mach.Not1(ssS.HandshakeDone) {
		return ErrNoConn
	}

	// check destination
	id := s.ClientId()
	if payload.Destination != "" && id != payload.Destination {
		return fmt.Errorf("%w: %s != %s", ErrDestination, payload.Destination, id)
	}

	defer s.Mach.PanicToErr(nil)

	payload.Token = utils.RandId(0)
	if event != nil {
		payload.Source = event.MachineId
		payload.SourceTx = event.TransitionId
	}
	s.log("sending payload %s from %s to %s", payload.Name, payload.Source,
		payload.Destination)

	// TODO failsafe
	return s.rpcClient.Load().CallWithContext(ctx,
		ClientSendPayload.Value, payload, &Empty{})
}

func (s *Server) ClientId() string {
	id := s.clientId.Load()
	if id == nil {
		return ""
	}

	return *id
}

// GetKind returns a kind of RPC component (server / client).
func (s *Server) GetKind() Kind {
	return KindServer
}

func (s *Server) log(msg string, args ...any) {
	if !s.LogEnabled {
		return
	}
	s.Mach.Log(msg, args...)
}

func (s *Server) bindRpcHandlers() {
	// new RPC instance, release prev resources
	s.rpcServer = rpc2.NewServer()

	s.rpcServer.Handle(ServerHello.Value, s.RemoteHello)
	s.rpcServer.Handle(ServerHandshake.Value, s.RemoteHandshake)
	s.rpcServer.Handle(ServerAdd.Value, s.RemoteAdd)
	s.rpcServer.Handle(ServerAddNS.Value, s.RemoteAddNS)
	s.rpcServer.Handle(ServerRemove.Value, s.RemoteRemove)
	s.rpcServer.Handle(ServerSet.Value, s.RemoteSet)
	s.rpcServer.Handle(ServerSync.Value, s.RemoteSync)
	s.rpcServer.Handle(ServerBye.Value, s.RemoteBye)

	// TODO RemoteLog, RemoteWhenArgs, RemoteGetMany

	// s.rpcServer.Handle("RemoteLog", s.RemoteLog)
	// s.rpcServer.Handle("RemoteWhenArgs", s.RemoteWhenArgs)
}

func (s *Server) pushClockUpdate(force bool) {
	c := s.rpcClient.Load()
	if c == nil {
		return
	}
	if s.skipClockPush.Load() && !force {
		// TODO log lvl 2
		// s.log("force-skip clock push")
		return
	}

	if s.Mach.Not1(ssS.ClientConnected) ||
		s.Mach.Not1(ssS.HandshakeDone) {
		// TODO log lvl 2
		// s.log("skip clock push")
		return
	}

	// disabled
	if s.PushInterval == 0 && !force {
		return
	}

	// push all ticks
	// TODO PushAllTicks
	// if s.PushAllTicks {
	// }

	// push the latest clock only
	clock := s.genClockUpdate(false)
	// debounce
	if clock == nil {
		return
	}

	// notify without a response
	defer s.Mach.PanicToErr(nil)
	s.log("pushClockUpdate %d", s.lastClockSum.Load())
	s.CallCount++

	// TODO failsafe retry
	err := c.Notify(ClientSetClock.Value, clock)
	if err != nil {
		s.Mach.Remove1(ssS.ClientConnected, nil)
		AddErr(nil, s.Mach, "pushClockUpdate", err)
	}
}

func (s *Server) genClockUpdate(skipTimeCheck bool) *ClockMsg {
	s.clockMx.Lock()
	defer s.clockMx.Unlock()

	// exit if too often
	if !skipTimeCheck && (time.Since(s.lastClockHTime) < s.PushInterval) {
		// s.log("genClockUpdate: too soon")
		return nil
	}
	hTime := time.Now()
	qTick := s.Source.QueueTick()
	mTime := s.Source.Time(nil)

	// exit if no change since the last sync
	var tSum uint64
	for _, v := range mTime {
		tSum += v
	}
	// update on a state change and queue tick change
	if tSum == s.lastClockSum.Load() && qTick == s.lastQueueTick {
		// flooooood
		// s.log("genClockUpdate: no change t%d q%d", tSum, qTick)
		return nil
	}

	// proceed - valid clock update
	s.lastClockMsg = NewClockMsg(tSum, s.lastClock, mTime, s.lastQueueTick, qTick)
	s.lastClock = mTime
	s.lastClockHTime = hTime
	s.lastQueueTick = qTick
	s.lastClockSum.Store(tSum)
	s.log("genClockUpdate: t%d q%d ch%d (%s)", tSum, qTick,
		s.lastClockMsg.Checksum, s.Source.ActiveStates(nil))

	return s.lastClockMsg
}

// ///// ///// /////

// ///// REMOTE METHODS

// ///// ///// /////

func (s *Server) RemoteHello(
	client *rpc2.Client, req *ArgsHello, resp *RespHandshake,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.clockMx.Lock()
	defer s.clockMx.Unlock()
	// TODO pass ID and key here
	// TODO check if client here is the same as RespHandshakeAck

	export := s.Source.Export()
	*resp = RespHandshake{
		Serialized: export,
	}

	// return the schema if requested
	if req.ReqSchema {
		// TODO get via Api.Export to avoid races
		schema := s.Source.Schema()
		resp.Schema = schema
	}

	sum := export.Time.Sum(nil)
	s.log("RemoteHello: t%v q%d", sum, export.QueueTick)
	s.Mach.Add1(ssS.Handshaking, nil)
	s.lastClock = export.Time
	s.lastQueueTick = export.QueueTick
	s.lastClockSum.Store(sum)
	s.lastClockHTime = time.Now()

	// TODO timeout for RemoteHandshake

	return nil
}

func (s *Server) RemoteHandshake(
	client *rpc2.Client, id *string, _ *Empty,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	// TODO pass ID and key here
	if id == nil || *id == "" {
		s.Mach.Remove1(ssS.Handshaking, nil)
		AddErrRpcStr(nil, s.Mach, "handshake failed: ID missing")

		return ErrInvalidParams
	}

	// check access TODO test
	if s.AllowId != "" && *id != s.AllowId {
		s.Mach.Remove1(ssS.Handshaking, nil)

		return fmt.Errorf("%w: %s != %s", ErrNoAccess, *id, s.AllowId)
	}

	sum := s.Source.Time(nil).Sum(nil)
	qTick := s.Source.QueueTick()
	s.log("RemoteHandshake: t%v q%d", sum, qTick)

	// accept the client
	s.rpcClient.Store(client)
	s.clientId.Store(id)
	s.Mach.Add1(ssS.HandshakeDone, Pass(&A{Id: *id}))

	// state changed during the handshake, push manually
	if s.lastClockSum.Load() != sum || s.lastQueueTick != qTick &&
		s.PushInterval == 0 {
		s.pushClockUpdate(true)
	}

	return nil
}

func (s *Server) RemoteAdd(
	_ *rpc2.Client, args *ArgsMut, resp *RespResult,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.mutMx.Lock()
	defer s.mutMx.Unlock()

	// validate
	if args.States == nil {
		return ErrInvalidParams
	}

	// execute
	var val am.Result
	s.skipClockPush.Store(true)
	if args.Event != nil {
		val = s.Source.EvAdd(args.Event, amhelp.IndexesToStates(
			s.Source.StateNames(), args.States), args.Args)
	} else {
		// TODO eval
		val = s.Source.Add(amhelp.IndexesToStates(s.Source.StateNames(),
			args.States), args.Args)
	}

	// return
	*resp = RespResult{
		Result: val,
		Clock:  s.genClockUpdate(true),
	}
	s.skipClockPush.Store(false)

	return nil
}

func (s *Server) RemoteAddNS(
	_ *rpc2.Client, args *ArgsMut, _ *Empty,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.mutMx.Lock()
	defer s.mutMx.Unlock()

	// validate
	if args.States == nil {
		return ErrInvalidParams
	}

	// execute
	s.skipClockPush.Store(true)
	_ = s.Source.Add(amhelp.IndexesToStates(s.Source.StateNames(), args.States),
		args.Args)
	s.skipClockPush.Store(false)

	return nil
}

func (s *Server) RemoteRemove(
	_ *rpc2.Client, args *ArgsMut, resp *RespResult,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.mutMx.Lock()
	defer s.mutMx.Unlock()

	// validate
	if args.States == nil {
		return ErrInvalidParams
	}

	// execute
	s.skipClockPush.Store(true)
	val := s.Source.Remove(amhelp.IndexesToStates(s.Source.StateNames(),
		args.States), args.Args)
	s.skipClockPush.Store(false)

	// return
	*resp = RespResult{
		Result: val,
		Clock:  s.genClockUpdate(true),
	}
	return nil
}

func (s *Server) RemoteSet(
	_ *rpc2.Client, args *ArgsMut, resp *RespResult,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.mutMx.Lock()
	defer s.mutMx.Unlock()

	// validate
	if args.States == nil {
		return ErrInvalidParams
	}

	// execute
	s.skipClockPush.Store(true)
	val := s.Source.Set(amhelp.IndexesToStates(s.Source.StateNames(),
		args.States), args.Args)
	s.skipClockPush.Store(false)

	// return
	*resp = RespResult{
		Result: val,
		Clock:  s.genClockUpdate(true),
	}
	return nil
}

func (s *Server) RemoteSync(
	_ *rpc2.Client, sum uint64, resp *RespSync,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.log("RemoteSync")

	if s.Source.Time(nil).Sum(nil) > sum {
		*resp = RespSync{
			Time:      s.Source.Time(nil),
			QueueTick: s.Source.QueueTick(),
		}
	} else {
		*resp = RespSync{}
	}

	s.log("RemoteSync: %v", resp.Time)

	return nil
}

// RemoteBye means the client says goodbye and will disconnect shortly.
func (s *Server) RemoteBye(
	_ *rpc2.Client, _ *Empty, _ *Empty,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.log("RemoteBye")

	s.Mach.Remove1(ssS.ClientConnected, Pass(&A{
		Addr: s.Addr,
	}))
	go func() {
		select {
		case <-time.After(100 * time.Millisecond):
			s.log("rpc.Close timeout")
		case <-amhelp.ExecAndClose(func() {
			if c := s.rpcClient.Load(); c != nil {
				_ = c.Close()
			}
		}):
			s.log("rpc.Close")
		}

		time.Sleep(100 * time.Millisecond)
		s.Mach.Remove1(ssS.HandshakeDone, nil)
	}()

	// forget client
	s.rpcClient.Store(nil)
	s.clientId.Store(nil)

	return nil
}

func (s *Server) RemoteSetPushAllTicks(
	_ *rpc2.Client, val bool, _ *Empty,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.log("RemoteSetPushAllTicks")

	s.PushAllTicks = val

	return nil
}

// ///// ///// /////

// ///// BINDINGS

// ///// ///// /////

// BindServer binds RpcReady and ClientConnected with Add/Remove, to custom
// states.
func BindServer(source, target *am.Machine, rpcReady, clientConn string) error {
	h := &struct {
		RpcReadyState am.HandlerFinal
		RpcReadyEnd   am.HandlerFinal

		HandshakeDoneState am.HandlerFinal
		HandshakeDoneEnd   am.HandlerFinal
	}{
		RpcReadyState: ampipe.Add(source, target, ssS.RpcReady, rpcReady),
		RpcReadyEnd:   ampipe.Remove(source, target, ssS.RpcReady, rpcReady),

		HandshakeDoneState: ampipe.Add(source, target, ssS.ClientConnected,
			clientConn),
		HandshakeDoneEnd: ampipe.Remove(source, target, ssS.ClientConnected,
			clientConn),
	}

	return source.BindHandlers(h)
}

// BindServerMulti binds RpcReady, ClientConnected, and ClientDisconnected.
// RpcReady is Add/Remove, other two are Add-only to passed multi states.
func BindServerMulti(
	source, target *am.Machine, rpcReady, clientConn, clientDisconn string,
) error {
	h := &struct {
		RpcReadyState am.HandlerFinal
		RpcReadyEnd   am.HandlerFinal

		HandshakeDoneState am.HandlerFinal
		HandshakeDoneEnd   am.HandlerFinal
	}{
		RpcReadyState: ampipe.Add(source, target, ssS.RpcReady, rpcReady),
		RpcReadyEnd:   ampipe.Remove(source, target, ssS.RpcReady, rpcReady),

		HandshakeDoneState: ampipe.Add(source, target,
			ssS.ClientConnected, clientConn),
		HandshakeDoneEnd: ampipe.Add(source, target,
			ssS.ClientConnected, clientDisconn),
	}

	return source.BindHandlers(h)
}

// BindServerRpcReady bind RpcReady using Add to a custom multi state.
func BindServerRpcReady(source, target *am.Machine, rpcReady string) error {
	h := &struct {
		RpcReadyState am.HandlerFinal
	}{
		RpcReadyState: ampipe.Add(source, target, ssS.RpcReady, rpcReady),
	}

	return source.BindHandlers(h)
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

type ServerOpts struct {
	// PayloadState is a state for the server to listen on, to deliver payloads
	// to the client. The client activates this state to request a payload from
	// the worker. Default: am/rpc/states/WorkerStates.SendPayload.
	PayloadState string
	// Parent is a parent state machine for a new Server state machine. See
	// [am.Opts].
	Parent am.Api
}

type SendPayloadHandlers struct {
	SendPayloadState am.HandlerFinal
}

// getSendPayloadState returns a handler (usually SendPayloadState), that will
// deliver a payload to the RPC client. The resulting function can be bound in
// anon handlers.
func getSendPayloadState(s *Server, stateName string) am.HandlerFinal {
	return func(e *am.Event) {
		// self-remove
		e.Machine().EvRemove1(e, stateName, nil)

		ctx := s.Mach.NewStateCtx(ssS.Start)
		args := ParseArgs(e.Args)
		argsOut := &A{Name: args.Name}

		// side-effect error handling
		if args.Payload == nil || args.Name == "" {
			err := fmt.Errorf("invalid payload args [name, payload]")
			e.Machine().EvAddErrState(e, ssW.ErrSendPayload, err, Pass(argsOut))

			return
		}

		// unblock and forward to the client
		go func() {
			// timeout context
			ctx, cancel := context.WithTimeout(ctx, s.DeliveryTimeout)
			defer cancel()

			err := s.SendPayload(ctx, e, args.Payload)
			if err != nil {
				e.Machine().EvAddErrState(e, ssW.ErrSendPayload, err, Pass(argsOut))
			}
		}()
	}
}

// createSendPayloadHandlers creates SendPayload handlers for a custom (dynamic)
// state name. Useful when binding >1 RPC server into the same state source.
func createSendPayloadHandlers(s *Server, stateName string) any {
	fn := getSendPayloadState(s, stateName)

	// define a struct with the handler
	structType := reflect.StructOf([]reflect.StructField{
		{
			Name: stateName + "State",
			Type: reflect.TypeOf(fn),
		},
	})

	// new instance and set handler
	val := reflect.New(structType).Elem()
	val.Field(0).Set(reflect.ValueOf(fn))
	ret := val.Addr().Interface()

	return ret
}
