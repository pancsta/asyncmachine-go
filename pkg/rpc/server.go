package rpc

import (
	"context"
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pancsta/rpc2"

	amh "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/rpcnames"
	ss "github.com/pancsta/asyncmachine-go/pkg/rpc/states/server"
)

type GetterFunc func(string) any

type Server struct {
	*ExceptionHandler

	Addr string
	Mach *am.Machine
	// ClockInterval is the interval for clock updates, effectively throttling
	// the number of updates sent to the client within the interval window.
	// 0 means pushes are disabled. Setting to a very small value will make
	// pushes instant.
	ClockInterval time.Duration
	// Listener can be set manually before starting the server.
	Listener   net.Listener
	LogEnabled bool
	CallCount  uint64

	// w is the worker machine
	w         *am.Machine
	rpcServer *rpc2.Server
	rpcClient *rpc2.Client
	// lastClockHTime is the last (human) time a clock update was sent to the
	// client.
	lastClockHTime time.Time
	lastClock      am.Time
	ticker         *time.Ticker
	clockMx        sync.Mutex
	// mutMx is a lock preventing mutation methods from racing each other.
	mutMx         sync.Mutex
	lastClockSum  uint64
	skipClockPush atomic.Bool
	lastClockMsg  ClockMsg
	getter        GetterFunc
}

// interfaces
var _ serverRpcMethods = &Server{}
var _ clientServerMethods = &Server{}

func NewServer(
	ctx context.Context, addr string, id string, worker *am.Machine,
	getter GetterFunc,
) (*Server, error) {
	if !worker.StatesVerified {
		return nil, fmt.Errorf("states not verified")
	}
	if id == "" {
		id = "rpc"
	}

	gob.Register(am.Relation(0))

	s := &Server{
		ExceptionHandler: &ExceptionHandler{},
		Addr:             addr,
		ClockInterval:    250 * time.Millisecond,
		LogEnabled:       os.Getenv("AM_RPC_LOG_SERVER") != "",
		w:                worker,
		getter:           getter,
	}

	// state machine
	mach, err := am.NewCommon(ctx, "s-"+id, ss.States, ss.Names,
		s, nil, nil)
	if err != nil {
		return nil, err
	}
	s.Mach = mach

	// bind to worker via Tracer API
	s.traceWorker()

	return s, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (s *Server) RpcStartingState(e *am.Event) {
	ctx := s.Mach.NewStateCtx(ss.RpcStarting)
	ctxStart := s.Mach.NewStateCtx(ss.Start)
	s.log("Connecting to %s", s.Addr)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		if s.Listener == nil {
			// use Start as the context for the listener
			cfg := net.ListenConfig{}
			lis, err := cfg.Listen(ctxStart, "tcp", s.Addr)
			if err != nil {
				// add err to mach
				errNetwork(s.Mach, err)
				// add outcome to mach
				s.Mach.Remove1(ss.RpcStarting, nil)

				return
			}

			s.Listener = lis
		} else {
			// update Addr if an external listener was provided
			s.Addr = s.Listener.Addr().String()
		}

		s.bindRpcHandlers()
		go s.rpcServer.Accept(s.Listener)
		if ctx.Err() != nil {
			return // expired
		}

		// bind to client events
		s.rpcServer.OnDisconnect(func(client *rpc2.Client) {
			if ctx.Err() != nil {
				return // expired
			}
			s.Mach.Add1(ss.ClientDisconn, am.A{"client": client})
		})
		// TODO flaky conn event
		s.rpcServer.OnConnect(func(client *rpc2.Client) {
			s.Mach.Add1(ss.ClientConn, am.A{"client": client})
		})

		s.Mach.Add1(ss.RpcReady, nil)
	}()
}

// RpcReadyState starts a ticker to compensate for clock push denounces.
func (s *Server) RpcReadyState(e *am.Event) {
	// no ticker for instant clocks
	if s.ClockInterval == 0 {
		return
	}

	ctx := s.Mach.NewStateCtx(ss.RpcReady)
	if s.ticker == nil {
		s.ticker = time.NewTicker(s.ClockInterval)
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
				s.ticker = nil
				return

			case <-t.C:
				s.pushClockUpdate()
			}
		}
	}()
}

func (s *Server) RpcReadyEnd(e *am.Event) {
	// TODO gracefully disconn from the client
	_ = s.Listener.Close()
	s.rpcServer = nil
}

func (s *Server) HandshakeDoneEnd(e *am.Event) {
	_ = s.rpcClient.Close()
	s.rpcClient = nil
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

// Start starts the server, optionally creates a listener if not provided and
// results in the Ready state.
func (s *Server) Start() am.Result {
	return s.Mach.Add1(ss.Start, nil)
}

// Stop stops the server, and optionally disposes resources.
func (s *Server) Stop(dispose bool) am.Result {
	res := s.Mach.Remove1(ss.Start, nil)
	if dispose {
		s.log("disposing")
		s.Mach.Dispose()
		s.Listener = nil
		s.rpcServer = nil
	}

	return res
}

// SendPayload sends a payload to the client.
func (s *Server) SendPayload(ctx context.Context, file *ArgsPayload) error {
	// TODO bind to an async state
	// TODO test SendPayload
	defer s.Mach.PanicToErr(nil)

	return s.rpcClient.CallWithContext(ctx, rpcnames.ClientSendPayload.Encode(),
		file, nil)
}

// GetKind returns a kind of RPC component (server / client).
func (s *Server) GetKind() Kind {
	return KindServer
}

// NewMirrorMach returns a new machine instance of the same kind as the
// the remote worker. It does suppose handlers, but only final ones, not
// negotiation, as all the write ops go through the remote worker. It does
// however support relation based negotiation, to locally reject mutations.
// TODO add Opts.NoNegHandlers to am
// TODO add full clock stram to make sure all final handlers are triggered
// func (s *Server) NewMirrorMach() *am.Machine {
//	return KindServer
// }

func (s *Server) log(msg string, args ...any) {
	if !s.LogEnabled {
		return
	}
	s.Mach.Log(msg, args...)
}

func (s *Server) traceWorker() bool {
	// reg a new tracer via an eval window (not mid-tx)
	ok := s.w.Eval("traceWorker", func() {
		s.w.Tracers = append(s.w.Tracers, &WorkerTracer{s: s})
	}, s.Mach.Ctx)

	// TODO handle dispose and close the connection

	return ok
}

func (s *Server) bindRpcHandlers() {
	// new RPC instance, release prev resources
	s.rpcServer = rpc2.NewServer()

	s.rpcServer.Handle(rpcnames.Handshake.Encode(), s.RemoteHandshake)
	s.rpcServer.Handle(rpcnames.HandshakeAck.Encode(), s.RemoteHandshakeAck)
	s.rpcServer.Handle(rpcnames.Add.Encode(), s.RemoteAdd)
	s.rpcServer.Handle(rpcnames.AddNS.Encode(), s.RemoteAddNS)
	s.rpcServer.Handle(rpcnames.Remove.Encode(), s.RemoteRemove)
	s.rpcServer.Handle(rpcnames.Set.Encode(), s.RemoteSet)
	s.rpcServer.Handle(rpcnames.Sync.Encode(), s.RemoteSync)
	s.rpcServer.Handle(rpcnames.Get.Encode(), s.RemoteGet)
	s.rpcServer.Handle(rpcnames.Bye.Encode(), s.RemoteBye)

	// TODO RemoteLog, RemoteWhenArgs, RemoteGetMany

	// s.rpcServer.Handle("RemoteLog", s.RemoteLog)
	// s.rpcServer.Handle("RemoteWhenArgs", s.RemoteWhenArgs)
}

func (s *Server) pushClockUpdate() {
	if s.skipClockPush.Load() || s.Mach.Not1(ss.ClientConn) ||
		s.Mach.Not1(ss.HandshakeDone) {
		s.log("force-skip clock push")
		return
	}

	// disabled
	if s.ClockInterval == 0 {
		return
	}

	clock := s.genClockUpdate(false)
	// debounce
	if clock == nil {
		return
	}

	// notif without a response
	defer s.Mach.PanicToErr(nil)
	s.log("pushClockUpdate")
	s.CallCount++
	err := s.rpcClient.Notify(rpcnames.ClientSetClock.Encode(), clock)
	if err != nil {
		errAuto(s.Mach, "pushClockUpdate", err)
	}
}

func (s *Server) genClockUpdate(skipTimeCheck bool) ClockMsg {
	// TODO cache based on time sum (track the history)
	s.clockMx.Lock()
	defer s.clockMx.Unlock()

	// exit if too often
	if !skipTimeCheck && (time.Since(s.lastClockHTime) < s.ClockInterval) {
		s.log("genClockUpdate: too soon")
		return nil
	}
	hTime := time.Now()
	mTime := s.w.Time(nil)

	// exit if no change since the last sync
	var sum uint64
	for _, v := range mTime {
		sum += v
	}
	if sum == s.lastClockSum && s.ClockInterval != 0 {
		// s.log("genClockUpdate: same sum")
		return nil
	}

	// proceed - valid clock update
	s.lastClockMsg = NewClockMsg(s.lastClock, mTime)
	s.lastClock = mTime
	s.lastClockHTime = hTime
	s.lastClockSum = sum

	return s.lastClockMsg
}

// ///// ///// /////

// ///// REMOTE METHODS

// ///// ///// /////

func (s *Server) RemoteHandshake(
	client *rpc2.Client, _ *Empty, resp *RespHandshake,
) error {
	// TODO GetStruct and Time inside Eval
	// TODO check if client here is the same as RespHandshakeAck

	mTime := s.w.Time(nil)
	*resp = RespHandshake{
		ID:         s.w.ID,
		StateNames: s.w.StateNames(),
		Time:       mTime,
	}

	s.Mach.Add1(ss.Handshaking, nil)
	var sum uint64
	for _, v := range mTime {
		sum += v
	}
	s.lastClock = mTime
	s.lastClockSum = sum
	s.lastClockHTime = time.Now()

	// TODO timeout for RemoteHandshakeAck

	return nil
}

func (s *Server) RemoteHandshakeAck(
	client *rpc2.Client, done *bool, _ *Empty,
) error {
	if done == nil || !*done {
		s.Mach.Remove1(ss.Handshaking, nil)
		errResponseStr(s.Mach, "handshake failed")
		return nil
	}

	// accept the client
	s.rpcClient = client
	// TODO pass as param
	s.Mach.Add1(ss.HandshakeDone, nil)

	return nil
}

func (s *Server) RemoteAdd(
	_ *rpc2.Client, args *ArgsMut, resp *RespResult,
) error {
	s.mutMx.Lock()
	defer s.mutMx.Unlock()

	// validate
	if args.States == nil {
		return fmt.Errorf("%w", ErrInvalidParams)
	}

	// execute
	s.skipClockPush.Store(true)
	val := s.w.Add(amh.IndexesToStates(s.w, args.States), args.Args)

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
	s.mutMx.Lock()
	defer s.mutMx.Unlock()

	// validate
	if args.States == nil {
		return fmt.Errorf("%w", ErrInvalidParams)
	}

	// execute
	s.skipClockPush.Store(true)
	_ = s.w.Add(amh.IndexesToStates(s.w, args.States), args.Args)
	s.skipClockPush.Store(false)

	return nil
}

func (s *Server) RemoteRemove(
	_ *rpc2.Client, args *ArgsMut, resp *RespResult,
) error {
	s.mutMx.Lock()
	defer s.mutMx.Unlock()

	// validate
	if args.States == nil {
		return fmt.Errorf("%w", ErrInvalidParams)
	}

	// execute
	s.skipClockPush.Store(true)
	val := s.w.Remove(amh.IndexesToStates(s.w, args.States), args.Args)
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
	s.mutMx.Lock()
	defer s.mutMx.Unlock()

	// validate
	if args.States == nil {
		return fmt.Errorf("%w", ErrInvalidParams)
	}

	// execute
	s.skipClockPush.Store(true)
	val := s.w.Set(amh.IndexesToStates(s.w, args.States), args.Args)
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
	s.log("RemoteSync")

	if s.w.TimeSum(nil) > sum {
		*resp = RespSync{
			Time: s.w.Time(nil),
		}
	} else {
		*resp = RespSync{}
	}

	s.log("RemoteSync: %v", resp.Time)

	return nil
}

func (s *Server) RemoteGet(
	_ *rpc2.Client, name string, resp *RespGet,
) error {
	s.log("RemoteGet: %s", rpcnames.Decode(name))

	if s.getter == nil {
		// TODO error
		*resp = RespGet{"no_getter"}
	} else {
		*resp = RespGet{s.getter(name)}
	}
	return nil
}

func (s *Server) RemoteBye(
	_ *rpc2.Client, _ *Empty, _ *Empty,
) error {
	s.log("RemoteBye")

	// TODO ClientBye to keep it in sync
	s.Mach.Add1(ss.ClientDisconn, nil)
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Mach.Remove1(ss.HandshakeDone, nil)
	}()

	return nil
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

type WorkerTracer struct {
	*am.NoOpTracer

	s *Server
}

func (t *WorkerTracer) TransitionEnd(_ *am.Transition) {
	go func() {
		t.s.mutMx.Lock()
		defer t.s.mutMx.Unlock()

		t.s.pushClockUpdate()
	}()
}

// TODO implement as an optimization
// func (t *WorkerTracer) QueueEnd(_ *am.Transition) {
//	t.s.pushClockUpdate()
// }
