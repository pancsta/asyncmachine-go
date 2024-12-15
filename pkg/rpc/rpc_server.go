package rpc

import (
	"context"
	"errors"
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
	"github.com/pancsta/asyncmachine-go/pkg/rpc/rpcnames"
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

	// Addr is the address of the server on the network.
	Addr            string
	DeliveryTimeout time.Duration
	// PushInterval is the interval for clock updates, effectively throttling
	// the number of updates sent to the client within the interval window.
	// 0 means pushes are disabled. Setting to a very small value will make
	// pushes instant.
	PushInterval time.Duration
	// PushAllTicks will push all ticks to the client, enabling client-side final
	// handlers. TODO implement
	PushAllTicks bool
	// Listener can be set manually before starting the server.
	Listener net.Listener
	// Conn can be set manually before starting the server.
	Conn net.Conn
	// NoNewListener will prevent the server from creating a new listener if
	// one is not provided, or has been closed. Useful for cmux.
	NoNewListener bool
	LogEnabled    bool
	CallCount     uint64

	// AllowId will limit clients to a specific ID, if set.
	AllowId string

	// source is a state source, either a local or remote RPC worker.
	source    am.Api
	rpcServer *rpc2.Server
	rpcClient atomic.Pointer[rpc2.Client]
	// lastClockHTime is the last (human) time a clock update was sent to the
	// client.
	lastClockHTime time.Time
	lastClock      am.Time
	ticker         *time.Ticker
	clockMx        sync.Mutex
	// mutMx is a lock preventing mutation methods from racing each other.
	mutMx         sync.Mutex
	lastClockSum  atomic.Pointer[uint64]
	skipClockPush atomic.Bool
	lastClockMsg  ClockMsg
	tracer        *WorkerTracer
	// ID of the currently connected client.
	clientId         atomic.Pointer[string]
	deliveryHandlers any
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
	if !sourceMach.Has(ssW.Names()) {
		err := errors.New(
			"worker has to implement pkg/rpc/states/WorkerStatesDef")

		return nil, err
	}

	s := &Server{
		ExceptionHandler: &ExceptionHandler{},
		Addr:             addr,
		PushInterval:     250 * time.Millisecond,
		DeliveryTimeout:  5 * time.Second,
		LogEnabled:       os.Getenv(EnvAmRpcLogServer) != "",
		source:           sourceMach,
	}
	var sum uint64
	s.lastClockSum.Store(&sum)

	// state machine
	mach, err := am.NewCommon(ctx, "rs-"+name, states.ServerStruct, ssS.Names(),
		s, opts.Parent, &am.Opts{Tags: []string{"rpc-server"}})
	if err != nil {
		return nil, err
	}
	mach.SetLogArgs(LogArgs)
	s.Mach = mach

	// bind to worker via Tracer API
	s.tracer = &WorkerTracer{s: s}
	_ = sourceMach.BindTracer(s.tracer)

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
			SendPayloadState: getSendPayloadState(s),
		}
	} else {
		// dynamic handlers
		h = createSendPayloadHandlers(s, payloadState)
	}
	err = sourceMach.BindHandlers(h)
	if err != nil {
		return nil, err
	}
	mach.HandleDispose(func(id string, ctx context.Context) {
		_ = sourceMach.DetachHandlers(h)
	})

	return s, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (s *Server) StartEnd(e *am.Event) {
	args := ParseArgs(e.Args)

	if args.Dispose {
		s.log("disposing")
		s.Mach.Dispose()
		s.Listener = nil
		s.rpcServer = nil
		s.source.DetachTracer(s.tracer)
		_ = s.source.DetachHandlers(s.deliveryHandlers)
	}
}

func (s *Server) RpcStartingEnter(e *am.Event) bool {
	if s.Listener == nil && s.NoNewListener {
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
		} else if s.Listener != nil {
			// update Addr from listener (support for external and :0)
			s.Addr = s.Listener.Addr().String()
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

			s.Listener = lis
			// update Addr from listener (support for external and :0)
			s.Addr = s.Listener.Addr().String()
		}

		s.log("RPC started on %s", s.Addr)

		go func() {
			if ctxRpcStarting.Err() != nil {
				return // expired
			}
			s.Mach.EvAdd1(e, ssS.RpcReady, Pass(&A{Addr: s.Addr}))

			if s.Conn != nil {
				srv.ServeConn(s.Conn)
			} else {
				srv.Accept(s.Listener)
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

// RpcReadyState starts a ticker to compensate for clock push denounces.
func (s *Server) RpcReadyState(e *am.Event) {
	// no ticker for instant clocks
	if s.PushInterval == 0 {
		return
	}

	ctx := s.Mach.NewStateCtx(ssS.RpcReady)
	s.ticker = time.NewTicker(s.PushInterval)

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

func (s *Server) RpcReadyEnd(e *am.Event) {
	// TODO tell the client Bye to gracefully disconn
	if s.Listener != nil {
		_ = s.Listener.Close()
		s.Listener = nil
	}
	// s.rpcServer = nil
}

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
	if dispose {
		s.log("disposing")
	}
	res := s.Mach.Remove1(ssS.Start, Pass(&A{
		Dispose: dispose,
	}))

	return res
}

// SendPayload sends a payload to the client. It's usually called by a handler
// for SendPayload.
func (s *Server) SendPayload(
	event *am.Event, ctx context.Context, payload *ArgsPayload,
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

	payload.Token = utils.RandID(0)
	if event != nil {
		payload.Source = event.MachineId
		payload.SourceTx = event.TransitionId
	}
	s.log("sending payload %s", payload.Name)

	// TODO failsafe
	return s.rpcClient.Load().CallWithContext(ctx,
		rpcnames.ClientSendPayload.Encode(), payload, &Empty{})
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

	s.rpcServer.Handle(rpcnames.Hello.Encode(), s.RemoteHello)
	s.rpcServer.Handle(rpcnames.Handshake.Encode(), s.RemoteHandshake)
	s.rpcServer.Handle(rpcnames.Add.Encode(), s.RemoteAdd)
	s.rpcServer.Handle(rpcnames.AddNS.Encode(), s.RemoteAddNS)
	s.rpcServer.Handle(rpcnames.Remove.Encode(), s.RemoteRemove)
	s.rpcServer.Handle(rpcnames.Set.Encode(), s.RemoteSet)
	s.rpcServer.Handle(rpcnames.Sync.Encode(), s.RemoteSync)
	s.rpcServer.Handle(rpcnames.Bye.Encode(), s.RemoteBye)

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
	err := c.Notify(rpcnames.ClientSetClock.Encode(), clock)
	if err != nil {
		s.Mach.Remove1(ssS.ClientConnected, nil)
		AddErr(nil, s.Mach, "pushClockUpdate", err)
	}
}

func (s *Server) genClockUpdate(skipTimeCheck bool) ClockMsg {
	s.clockMx.Lock()
	defer s.clockMx.Unlock()

	// exit if too often
	if !skipTimeCheck && (time.Since(s.lastClockHTime) < s.PushInterval) {
		// s.log("genClockUpdate: too soon")
		return nil
	}
	hTime := time.Now()
	mTime := s.source.Time(nil)

	// exit if no change since the last sync
	var sum uint64
	for _, v := range mTime {
		sum += v
	}
	if sum == *s.lastClockSum.Load() {
		// s.log("genClockUpdate: same sum %d", sum)
		return nil
	}

	// proceed - valid clock update
	s.lastClockMsg = NewClockMsg(s.lastClock, mTime)
	s.lastClock = mTime
	s.lastClockHTime = hTime
	s.lastClockSum.Store(&sum)

	return s.lastClockMsg
}

// ///// ///// /////

// ///// REMOTE METHODS

// ///// ///// /////

func (s *Server) RemoteHello(
	client *rpc2.Client, _ *Empty, resp *RespHandshake,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	// TODO pass ID and key here
	// TODO GetStruct and Time inside Eval
	// TODO check if client here is the same as RespHandshakeAck

	mTime := s.source.Time(nil)
	*resp = RespHandshake{
		ID:         s.source.Id(),
		StateNames: s.source.StateNames(),
		Time:       mTime,
	}

	// TODO block
	var sum uint64
	for _, v := range mTime {
		sum += v
	}
	s.log("RemoteHello: t%v", sum)
	s.Mach.Add1(ssS.Handshaking, nil)
	s.lastClock = mTime
	s.lastClockSum.Store(&sum)
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

	sum := s.source.TimeSum(nil)
	s.log("RemoteHandshake: t%v", sum)

	// accept the client
	s.rpcClient.Store(client)
	s.clientId.Store(id)
	s.Mach.Add1(ssS.HandshakeDone, Pass(&A{Id: *id}))

	// state changed during the handshake, push manually
	if *s.lastClockSum.Load() != sum && s.PushInterval == 0 {
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
		val = s.source.EvAdd(args.Event, amhelp.IndexesToStates(s.source.StateNames(),
			args.States), args.Args)
	} else {
		// TODO eval
		val = s.source.Add(amhelp.IndexesToStates(s.source.StateNames(),
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
	_ = s.source.Add(amhelp.IndexesToStates(s.source.StateNames(), args.States), args.Args)
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
	val := s.source.Remove(amhelp.IndexesToStates(s.source.StateNames(), args.States),
		args.Args)
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
	val := s.source.Set(amhelp.IndexesToStates(s.source.StateNames(), args.States),
		args.Args)
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

	if s.source.TimeSum(nil) > sum {
		*resp = RespSync{
			Time: s.source.Time(nil),
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
func getSendPayloadState(s *Server) am.HandlerFinal {
	return func(e *am.Event) {
		e.Machine().EvRemove1(e, ssW.SendPayload, nil)
		ctx := s.Mach.NewStateCtx(ssW.Start)
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

			err := s.SendPayload(e, ctx, args.Payload)
			if err != nil {
				e.Machine().EvAddErrState(e, ssW.ErrSendPayload, err, Pass(argsOut))
			}
		}()
	}
}

// createSendPayloadHandlers creates SendPayload handlers for a custom state.
func createSendPayloadHandlers(s *Server, stateName string) any {
	fn := getSendPayloadState(s)

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
