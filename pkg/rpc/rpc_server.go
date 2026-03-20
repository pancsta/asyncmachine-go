package rpc

// TODO call ClientBye on Disposing

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/rpc2"
	"github.com/coder/websocket"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

var (
	ssS  = states.ServerStates
	ssSS = states.StateSourceStates
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
	// DeliveryTimeout is a timeout to SendPayload to the client.
	DeliveryTimeout time.Duration
	// Listener can be set manually before starting the server.
	Listener atomic.Pointer[net.Listener]
	// Conn can be set manually before starting the server. TODO atomic?
	Conn       net.Conn
	LogEnabled bool
	CallCount  uint64
	// Typed arguments struct value with defaults
	Args any
	Opts ServerOpts

	// sync settings

	// PushInterval is the interval for clock updates, effectively throttling
	// the number of updates sent to the client within the interval window.
	// 0 means pushes are disabled. Setting to a very small value will make
	// pushes instant.
	PushInterval atomic.Pointer[time.Duration]
	// AllowId will limit clients to a specific ID, if set.
	AllowId string

	// failsafe - connection (writable when stopped)

	// WsTunReconn enables retrying the WebSocket tunnel.
	WsTunReconn bool
	// WsTunConnTimeout is the maximum time to wait for a WebSocket tunnel
	// connection to be established.
	WsTunConnTimeout time.Duration
	// WsTunConnRetries is the number of retries for a connection.
	WsTunConnRetries int
	// WsTunConnRetryTimeout is the maximum time to retry a connection.
	WsTunConnRetryTimeout time.Duration
	// WsTunConnRetryDelay is the time to wait between retries. If
	// WsTunConnRetryBackoff is set, this is the initial delay and doubles on each
	// retry.
	WsTunConnRetryDelay time.Duration
	// WsTunConnRetryBackoff is the maximum time to wait between retries.
	WsTunConnRetryBackoff time.Duration

	// syncMutations will push all clock changes for each mutation, enabling
	// client-side mutation filtering.
	syncMutations     bool
	syncAllowedStates am.S
	syncSkippedStates am.S
	syncSchema        bool
	syncShallowClocks bool

	// security

	// internal

	rpcServer *rpc2.Server
	// rpcClient is the internal rpc2 client.
	rpcClient atomic.Pointer[rpc2.Client]
	tracer    *sourceTracer
	// TODO bind the a machine ticker (if handlers bound)
	ticker *time.Ticker

	// lock data collection (RemoteHello and tracer data)
	lockCollection sync.Mutex
	// lock exporting updates (pushClient and responses to mutations)
	lockExport sync.Mutex
	// server is currently responding to a client, and pushing should be skipped
	// respInProgress atomic.Bool

	// ID of the currently connected client.
	clientId         atomic.Pointer[string]
	deliveryHandlers any
	lastPush         time.Time
	lastPushData     *tracerData
	httpSrv          *http.Server
	wsTunRetryRound  atomic.Int32
	wsConn           *websocket.Conn
}

// interfaces
var (
	_ serverRpcMethods    = &Server{}
	_ clientServerMethods = &Server{}
)

// NewServer creates a new RPC server, bound to a worker machine.
// The source machine has to implement [states.StateSourceStatesDef] interface.
//
// addr: can be empty if [Server.Listener] or [Server.Conn] is set later.
func NewServer(
	ctx context.Context, addr string, name string, stateSource am.Api,
	opts *ServerOpts,
) (*Server, error) {
	// TODO SPLIT

	if name == "" {
		name = "rpc"
	}
	if opts == nil {
		opts = &ServerOpts{}
	}
	// TODO WASM auto-tunnel
	if opts.WebSocketTunnel != "" && addr == "" {
		return nil, fmt.Errorf("addr required for WebSocketTunnel")
	}

	// check the source
	if stateSource == nil {
		return nil, fmt.Errorf("netSrcMach required")
	}
	if !stateSource.StatesVerified() {
		return nil, fmt.Errorf(
			"net source states not verified, call VerifyStates()")
	}
	hasHandlers := stateSource.HasHandlers()
	if hasHandlers && !stateSource.Has(ssSS.Names()) {
		// error only when some handlers bound, skip deterministic machines
		err := fmt.Errorf(
			"%w: NetSourceMach with handlers has to implement "+
				"pkg/rpc/states/StateSourceStatesDef",
			am.ErrSchema)

		return nil, err
	}

	// TODO validate state names

	s := &Server{
		ExceptionHandler: &ExceptionHandler{},
		Addr:             addr,
		DeliveryTimeout:  5 * time.Second,
		LogEnabled:       os.Getenv(EnvAmRpcLogServer) != "",
		Source:           stateSource,
		Args:             opts.Args,
		Opts:             *opts,

		WsTunReconn:           true,
		WsTunConnTimeout:      3 * time.Second,
		WsTunConnRetryTimeout: 5 * time.Minute,
		WsTunConnRetries:      50,
		WsTunConnRetryDelay:   time.Second,
		WsTunConnRetryBackoff: 10 * time.Second,

		lastPushData: &tracerData{},
	}
	interval := 250 * time.Millisecond
	s.PushInterval.Store(&interval)

	// state machine
	mach, err := am.NewCommon(ctx, "rs-"+name, states.ServerSchema, ssS.Names(),
		nil, opts.Parent, &am.Opts{
			Tags: []string{TagRpcServer},
		})
	if err != nil {
		return nil, err
	}
	if err = BindHandlersServer(s, mach); err != nil {
		return nil, err
	}
	mach.SemLogger().SetArgsMapper(LogArgs)
	mach.SetGroups(states.ServerGroups, ssS)
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
		_ = amhelp.MachDebugEnv(mach)
	}

	// states
	if opts.WebSocketTunnel != "" {
		mach.Add1(ssS.WebSocketTunnel, nil)
	}

	// bind to source via Tracer API (inactive until activated)
	s.tracer = &sourceTracer{
		s: s,
		dataLatest: &tracerData{
			// queue ticks start at 1
			queueTick: 1,
		},
	}
	// queue ticks start at 1
	s.tracer.dataLatest.queueTick = 1
	if err = stateSource.BindTracer(s.tracer); err != nil {
		return nil, err
	}

	// handle payload
	if hasHandlers {

		// payload state
		payloadState := ssSS.SendPayload
		if opts.PayloadState != "" {
			payloadState = opts.PayloadState
		}

		// payload handlers
		if payloadState == ssSS.SendPayload {
			// default handlers
			h := &SendPayloadHandlers{
				SendPayloadState: newSendPayloadState(s, ssSS.SendPayload),
			}
			if err = BindStateSourcePayload(h, stateSource); err != nil {
				return nil, err
			}
		} else if err = bindCustomSendPayload(s, stateSource, payloadState); err != nil {

			return nil, err
		}
	}

	// longer timeouts
	if amhelp.IsDebug() {
		s.log("debug, extending timeouts")
		s.WsTunConnRetryTimeout *= 10
	}

	// debug
	// mach.AddBreakpoint1("", ssS.RpcReady, true)
	// mach.AddBreakpoint1("", ssS.RpcReady, false)

	return s, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (s *Server) ExceptionState(e *am.Event) {
	// call super
	s.ExceptionHandler.ExceptionState(e)

	// WebsocketTunnel
	if s.Mach.Is1(ssS.WebSocketTunnel) {
		// clean up on errs
		if s.wsConn != nil {
			_ = s.wsConn.Close(websocket.StatusInternalError, "")
		}
		if s.Conn != nil {
			_ = s.Conn.Close()
		}

		// retry?
		add, _ := e.Transition().TimeIndexDiff()
		shouldRetry := s.wsTunRetryRound.Load() < int32(s.WsTunConnRetries) &&
			s.WsTunReconn
		if add.Is1(ssS.ErrNetwork) && shouldRetry {
			s.log("WebSocket exception retry")
			s.Mach.Remove1(ssS.Exception, nil)
			s.Mach.Add1(ssS.RpcStarting, nil)
		}
	}
}

func (s *Server) StartState(e *am.Event) {
	ctx := s.Mach.NewStateCtx(ssS.Start)

	// start websocket server early
	if s.Opts.WebSocket {
		s.httpSrv = &http.Server{
			Addr: s.Addr,
			Handler: &wsHandlerServer{
				s:     s,
				event: e.Export(),
			},
		}

		// fork2
		go func() {
			if ctx.Err() != nil {
				return // expired
			}

			err := s.httpSrv.ListenAndServe()
			if err != nil {
				// add err to mach
				AddErrNetwork(e, s.Mach, err)
				// add outcome to mach
				s.Mach.Remove1(ssS.RpcStarting, nil)

				return
			}
		}()
	}
}

func (s *Server) StartEnd(e *am.Event) {
	if s.Opts.WebSocket {
		go func() {
			_ = s.httpSrv.Shutdown(s.Mach.Context())
		}()
	} else if s.Mach.Is1(ssS.WebSocketTunnel) && s.wsConn != nil {
		_ = s.wsConn.Close(websocket.StatusNormalClosure, "")
	}
	if ParseArgs(e.Args).Dispose {
		s.Mach.Dispose()
	}
}

func (s *Server) RpcStartingEnter(e *am.Event) bool {
	// check listening
	if s.Addr == "" && s.Listener.Load() == nil && s.Conn == nil {
		return false
	}

	return true
}

func (s *Server) RpcStartingState(e *am.Event) {
	ctxStart := s.Mach.NewStateCtx(ssS.Start)
	s.log("Starting RPC on %s", s.Addr)
	s.bindRpcHandlers()

	// skip for websocket server
	if s.Opts.WebSocket {
		return
	}

	// fork1 TODO mach.Fork
	go func() {
		// has to be ctxStart, not ctxRpcStarting TODO why? reconns?
		if ctxStart.Err() != nil {
			return // expired
		}

		// websocket listener (HTTP)
		if s.Opts.WebSocketTunnel != "" {
			addr := "ws://" + s.Addr + s.Opts.WebSocketTunnel
			delay := s.WsTunConnRetryDelay
			start := time.Now()

			// TODO WsTunConnectingState
			// go func() {
			// retry loop
			s.Conn = nil
			for ctxStart.Err() == nil &&
				(s.WsTunReconn || s.wsTunRetryRound.Load() == 0) &&
				s.wsTunRetryRound.Load() < int32(s.WsTunConnRetries) {

				// wait for time or exit
				s.wsTunRetryRound.Add(1)
				if !amhelp.Wait(ctxStart, delay) {
					return // expired
				}

				// dial
				ctxWs, cancel := context.WithTimeout(ctxStart, s.WsTunConnTimeout)
				// ctxWs, _ := context.WithTimeout(ctxStart, s.WsTunConnTimeout)
				s.log("Dialing (round %d) %s", s.wsTunRetryRound.Load(), addr)
				ws, _, err := websocket.Dial(ctxWs, addr, nil)
				if err != nil {
					s.log("WebSocket err")
					if ctxStart.Err() == nil && ctxWs.Err() != nil {
						AddErrNetworkTimeout(e, s.Mach, err)
					} else {
						AddErrNetwork(e, s.Mach, err)
					}

				} else {
					s.log("WebSocket OK")
					s.wsConn = ws
					if err != nil {
						AddErrNetwork(e, s.Mach, err)
					} else {
						s.log("Tunnel OK")
						s.Conn = websocket.NetConn(ctxStart, ws, websocket.MessageBinary)
						// reset the retry counter
						s.wsTunRetryRound.Store(0)
					}
					cancel()
					break
				}
				cancel()

				// double the delay when backoff set
				if s.WsTunConnRetryBackoff > 0 {
					delay *= 2
					if delay > s.WsTunConnRetryBackoff {
						delay = s.WsTunConnRetryBackoff
					}
				}

				// last try?
				if t := s.WsTunConnRetryTimeout; t > 0 && time.Since(start) > t {
					s.log("WebSocket RetryTimeout")
					break
				}

				// try again
			}

			// failed?
			if s.Conn == nil {
				s.log("WebSocket tunnel failure")
				s.Mach.EvRemove1(e, ssS.RpcStarting, nil)
				return
			}

			// existing connection
		} else if s.Conn != nil {
			s.log("Using existing connection %s", s.Conn.LocalAddr())
			s.Addr = s.Conn.LocalAddr().String()

			// existing listener
		} else if l := s.Listener.Load(); l != nil {
			// update Addr from listener (support for external and :0)
			s.Addr = (*l).Addr().String()

			// new listener
		} else {
			// create a listener if not provided
			// use Start as the context
			cfg := net.ListenConfig{}
			lis, err := cfg.Listen(ctxStart, "tcp4", s.Addr)
			if err != nil {
				// add err to mach
				AddErrNetwork(e, s.Mach, err)
				// add outcome to mach
				s.Mach.EvRemove1(e, ssS.RpcStarting, nil)

				return
			}

			s.Listener.Store(&lis)
			// update Addr from listener (support for external and :0)
			s.Addr = lis.Addr().String()
		}

		// next
		s.Mach.EvAdd1(e, ssS.RpcAccepting, nil)
	}()
}

func (s *Server) RpcAcceptingEnter(e *am.Event) bool {
	return s.Listener.Load() != nil || s.Conn != nil
}

func (s *Server) RpcAcceptingState(e *am.Event) {
	ctxRpcAccepting := s.Mach.NewStateCtx(ssS.RpcAccepting)
	ctxStart := s.Mach.NewStateCtx(ssS.Start)
	srv := s.rpcServer

	s.log("RPC started on %s", s.Addr)

	// unblock
	go func() {
		if ctxRpcAccepting.Err() != nil {
			return // expired
		}

		// fork to accept TODO state?
		go func() {
			if ctxRpcAccepting.Err() != nil {
				return // expired
			}
			s.Mach.EvAdd1(e, ssS.RpcReady, Pass(&A{Addr: s.Addr}))

			// accept (block)
			lis := s.Listener.Load()
			if s.Conn != nil {
				srv.ServeConn(s.Conn)
			} else if lis != nil {
				srv.Accept(*lis)
			} else {
				AddErrNetwork(e, s.Mach, fmt.Errorf("no listener"))
				s.Mach.EvRemove1(e, ssS.RpcReady, nil)

				return
			}
			if ctxStart.Err() != nil {
				return // expired
			}

			// clean up
			if lis != nil {
				(*lis).Close()
				s.Listener.Store(nil)
			}
			if ctxStart.Err() != nil {
				return // expired
			}

			// restart on failed listener
			if s.Mach.Is1(ssS.Start) {
				s.log("restarting on failed listener")
				s.Mach.EvRemove1(e, ssS.RpcReady, nil)
				s.Mach.EvAdd1(e, ssS.RpcStarting, nil)
			}
		}()

		// bind to RPC server events (or override)
		srv.OnDisconnect(func(client *rpc2.Client) {
			s.Mach.EvRemove1(e, ssS.ClientConnected, Pass(&A{Client: client}))
		})
		srv.OnConnect(func(client *rpc2.Client) {
			s.Mach.EvAdd1(e, ssS.ClientConnected, Pass(&A{Client: client}))
		})
	}()
}

func (s *Server) RpcReadyEnter(e *am.Event) bool {
	// only from RpcAccepting
	return s.Mach.Is1(ssS.RpcAccepting)
}

// RpcReadyState starts a ticker to compensate for clock push debounces.
func (s *Server) RpcReadyState(e *am.Event) {
	if s.Opts.WebSocketTunnel != "" {
		s.log("ws tunnel ok: %s", s.Opts.WebSocketTunnel)
	}

	// no ticker for instant clocks
	if *s.PushInterval.Load() == 0 {
		return
	}

	ctx := s.Mach.NewStateCtx(ssS.RpcReady)
	if s.ticker == nil {
		s.ticker = time.NewTicker(*s.PushInterval.Load())
	}

	// avoid dispose
	t := s.ticker

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// push clock updates, debounced by getLatestUpdate
		for {
			select {
			case <-ctx.Done():
				s.ticker.Stop()
				return

			case <-t.C:
				s.pushClient()
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
func (s *Server) Start(e *am.Event) am.Result {
	return s.Mach.EvAdd1(e, ssS.Start, nil)
}

// Stop stops the server, and optionally disposes resources.
func (s *Server) Stop(e *am.Event, dispose bool) am.Result {
	// TODO waitTillCtx
	if s.Mach == nil {
		return am.Canceled
	}
	if dispose {
		s.log("disposing")
	}
	amhelp.DisposeEv(s.Mach, e)

	return am.Executed
}

// SendPayload sends a payload to the client.
//
// srcEvent: optional event for tracing.
func (s *Server) SendPayload(
	ctx context.Context, srcEvent *am.Event, payload *MsgSrvPayload,
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
	if srcEvent != nil {
		payload.Source = srcEvent.MachineId
		payload.SourceTx = srcEvent.TransitionId
	}
	s.log("sending payload %s from %s to %s", payload.Name, payload.Source,
		payload.Destination)

	// TODO failsafe
	return s.rpcClient.Load().CallWithContext(ctx,
		ClientSendPayload.Value, payload, &MsgEmpty{})
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
	if os.Getenv(EnvAmRpcLogServer) != "" {
		rpc2.DebugLog = true
	}

	s.rpcServer.Handle(ServerHello.Value, s.RemoteHello)
	s.rpcServer.Handle(ServerHandshake.Value, s.RemoteHandshake)
	s.rpcServer.Handle(ServerAdd.Value, s.RemoteAdd)
	s.rpcServer.Handle(ServerAddNS.Value, s.RemoteAddNS)
	s.rpcServer.Handle(ServerRemove.Value, s.RemoteRemove)
	s.rpcServer.Handle(ServerSet.Value, s.RemoteSet)
	s.rpcServer.Handle(ServerSync.Value, s.RemoteSync)
	s.rpcServer.Handle(ServerArgs.Value, s.RemoteArgs)
	s.rpcServer.Handle(ServerBye.Value, s.RemoteBye)

	// TODO RemoteWhenArgs

	// s.rpcServer.Handle("RemoteLog", s.RemoteLog)
	// s.rpcServer.Handle("RemoteWhenArgs", s.RemoteWhenArgs)
}

// pushClient pushes an update to the client. It can be either a single or a
// multi update. It can also be throttled and happen later.
func (s *Server) pushClient() {
	c := s.rpcClient.Load()
	if c == nil {
		return
	}

	// push disabled or not ready
	if *s.PushInterval.Load() == 0 || s.Mach.Not1(ssS.HandshakeDone) {
		return
	}

	// skip if currently exporting
	if !s.lockExport.TryLock() {
		s.log("skip parallel export")
		return
	}
	defer s.lockExport.Unlock()

	// collect
	data := s.tracer.DataLatest()
	if data == nil {
		return
	}

	// no change
	if s.lastPushData.mTrackedTimeSum == data.mTrackedTimeSum &&
		s.lastPushData.queueTick == data.queueTick {

		// s.log("skip no diff")
		return
	}

	// too often
	if time.Since(s.lastPush) < *s.PushInterval.Load() {
		// s.log("update too soon")
		return
	}

	// sync
	s.log("pushClient:try t%d", data.mTrackedTimeSum)
	var err error
	if s.syncMutations {
		err = s.pushUpdateMutations(s.tracer.DataQueue())
	} else {
		err = s.pushUpdateLatest(data)
	}
	if err != nil {
		s.Mach.Remove1(ssS.ClientConnected, nil)
		AddErr(nil, s.Mach, "pushClient", err)

		return
	}
	s.log("pushClient:ok t%d", data.mTrackedTimeSum)

	s.storeLastPush(data)
}

// call via by pushClient
func (s *Server) pushUpdateMutations(muts []tracerMutation) error {
	c := s.rpcClient.Load()
	if c == nil {
		return nil
	}
	defer s.Mach.PanicToErr(nil)

	// calculate diffs
	updateMuts := calcUpdateMutations(s.syncSchema, muts, s.lastPushData)

	// nothing to push
	if len(updateMuts.MutationType) == 0 {
		return nil
	}

	// notify without a response
	s.CallCount++

	// TODO failsafe retry (stateful)
	return c.Notify(ClientUpdateMutations.Value, updateMuts)
}

// call via by pushClient
func (s *Server) pushUpdateLatest(data *tracerData) error {
	c := s.rpcClient.Load()
	if c == nil {
		return nil
	}
	defer s.Mach.PanicToErr(nil)

	// calculate diff
	update := calcUpdate(s.syncSchema, data, s.lastPushData, s.syncShallowClocks)

	// nothing to push
	if len(update.Indexes) == 0 {
		return nil
	}

	// notify without a response
	s.CallCount++
	// fmt.Printf("[S] update %v\n", update)
	// fmt.Printf("[S] time %v\n", data.mTime)

	// TODO failsafe retry (stateful)
	return c.Notify(ClientUpdate.Value, update)
}

// newMsgMutation creates a new response to a mutation call from the client.
// Requires [s.lockExport]
func (s *Server) newMsgMutation(
	mut am.Result, data *tracerData,
) *MsgSrvMutation {
	r := MsgSrvMutation{Result: mut}
	// calculate diff
	if s.syncMutations {
		r.Mutations = calcUpdateMutations(s.syncSchema, s.tracer.DataQueue(),
			s.lastPushData)
	} else {
		r.Update = calcUpdate(s.syncSchema, data, s.lastPushData,
			s.syncShallowClocks)
	}

	// fmt.Printf("[S] QueueTick: %d - %d\n", data.queueTick, s.lastPushQTick)
	// fmt.Printf("[S] MachTick: %d - %d\n", data.machTick, s.lastPushMachTick)

	s.storeLastPush(data)

	return &r
}

func (s *Server) storeLastPush(data *tracerData) {
	s.lastPush = time.Now()
	s.lastPushData = data
}

// ///// ///// /////

// ///// REMOTE METHODS

// ///// ///// /////
// TODO add local errs

func (s *Server) RemoteHello(
	client *rpc2.Client, req *MsgCliHello, resp *MsgSrvHello,
) error {
	// validate
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	if req == nil || req.Id == "" {
		s.Mach.Remove1(ssS.Handshaking, nil)
		AddErrRpcStr(nil, s.Mach, "handshake failed: ID missing")

		return ErrInvalidParams
	}

	// locks
	s.lockCollection.Lock()
	defer s.lockCollection.Unlock()

	// check access TODO test
	if s.AllowId != "" && req.Id != s.AllowId {
		s.Mach.Remove1(ssS.Handshaking, nil)

		return fmt.Errorf("%w: %s != %s", ErrNoAccess, req.Id, s.AllowId)
	}

	// set up sync
	s.syncAllowedStates = req.AllowedStates
	s.syncSkippedStates = req.SkippedStates
	s.syncShallowClocks = req.ShallowClocks
	s.syncMutations = req.SyncMutations

	// prep the mach msg
	export, schema, _ := s.Source.Export()
	s.tracer.calcTrackedStates(export.StateNames)
	s.tracer.active = true
	statesCount := len(export.StateNames)
	tTrackedSum := export.Time.Filter(s.tracer.trackedStateIdxs).Sum(nil)

	// client-bound indexes when no schema synced
	if !req.SyncSchema {
		export.StateNames = am.StatesShared(export.StateNames,
			s.tracer.trackedStates)
		export.Time = export.Time.Filter(s.tracer.trackedStateIdxs)

		// zero non-tracked for consistent checksums
	} else {
		for i := range export.StateNames {
			if slices.Contains(s.tracer.trackedStateIdxs, i) {
				continue
			}
			export.Time[i] = 0
		}
	}
	*resp = MsgSrvHello{
		Serialized:  export,
		StatesCount: uint32(statesCount),
	}

	// return the schema if requested
	if req.SyncSchema {
		s.syncSchema = true
		if req.SchemaHash == "" ||
			req.SchemaHash != amhelp.SchemaHash(schema) {

			resp.Schema = schema
		}
	}

	// memorize
	s.lastPushData.mTime = export.Time
	s.lastPushData.queueTick = export.QueueTick
	s.lastPushData.mTrackedTimeSum = tTrackedSum
	s.lastPush = time.Now()
	s.clientId.Store(&req.Id)

	s.log("RemoteHello: t%v q%d", tTrackedSum, export.QueueTick)
	s.Mach.Add1(ssS.Handshaking, nil)

	// TODO timeout for RemoteHandshake via push loop

	return nil
}

func (s *Server) RemoteHandshake(
	client *rpc2.Client, _ *MsgEmpty, _ *MsgEmpty,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	id := s.clientId.Load()
	if id == nil {
		return ErrNoConn
	}
	sum := s.Source.Time(nil).Sum(nil)
	qTick := s.Source.QueueTick()
	s.log("RemoteHandshake: t%v q%d", sum, qTick)

	// accept the client
	s.rpcClient.Store(client)
	s.Mach.Add1(ssS.HandshakeDone, Pass(&A{
		Id: *id,
	}))

	return nil
}

func (s *Server) RemoteAdd(
	_ *rpc2.Client, req *MsgCliMutation, resp *MsgSrvMutation,
) error {
	// TODO DEBUG
	if s.Source.Id() == "browser-agentui-cook" {
		print()
	}
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.lockExport.Lock()
	defer s.lockExport.Unlock()

	// validate
	if req.States == nil {
		return ErrInvalidParams
	}

	// typed args
	args := req.Args
	if s.Opts.ParseRpc != nil {
		args = s.Opts.ParseRpc(args)
	}

	// execute
	var val am.Result
	if req.Event != nil {
		val = s.Source.EvAdd(req.Event, amhelp.IndexesToStates(
			s.Source.StateNames(), req.States), args)
	} else {
		// TODO eval
		val = s.Source.Add(amhelp.IndexesToStates(s.Source.StateNames(),
			req.States), args)
	}

	// return
	data := s.tracer.DataLatest()
	*resp = *s.newMsgMutation(val, data)

	return nil
}

func (s *Server) RemoteAddNS(
	_ *rpc2.Client, req *MsgCliMutation, _ *MsgEmpty,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.lockExport.Lock()
	defer s.lockExport.Unlock()

	// validate
	if req.States == nil {
		return ErrInvalidParams
	}

	// typed args
	args := req.Args
	if s.Opts.ParseRpc != nil {
		args = s.Opts.ParseRpc(args)
	}

	// execute TODO event trace
	_ = s.Source.Add(amhelp.IndexesToStates(s.Source.StateNames(), req.States),
		args)

	return nil
}

func (s *Server) RemoteRemove(
	_ *rpc2.Client, req *MsgCliMutation, resp *MsgSrvMutation,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.lockExport.Lock()
	defer s.lockExport.Unlock()

	// validate
	if req.States == nil {
		return ErrInvalidParams
	}

	// typed args
	args := req.Args
	if s.Opts.ParseRpc != nil {
		args = s.Opts.ParseRpc(args)
	}

	// execute TODO event trace
	val := s.Source.Remove(amhelp.IndexesToStates(s.Source.StateNames(),
		req.States), args)

	// return
	data := s.tracer.DataLatest()
	*resp = *s.newMsgMutation(val, data)

	return nil
}

func (s *Server) RemoteSet(
	_ *rpc2.Client, req *MsgCliMutation, resp *MsgSrvMutation,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.lockExport.Lock()
	defer s.lockExport.Unlock()

	// validate
	if req.States == nil {
		return ErrInvalidParams
	}

	// typed args
	args := req.Args
	if s.Opts.ParseRpc != nil {
		args = s.Opts.ParseRpc(args)
	}

	// execute TODO event trace
	val := s.Source.Set(amhelp.IndexesToStates(s.Source.StateNames(),
		req.States), args)

	// return
	data := s.tracer.DataLatest()
	*resp = *s.newMsgMutation(val, data)

	return nil
}

func (s *Server) RemoteSync(
	_ *rpc2.Client, _ *MsgEmpty, resp *MsgSrvSync,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.Mach.Add1(ssS.MetricSync, nil)

	*resp = MsgSrvSync{
		Time:      s.Source.Time(nil),
		QueueTick: s.Source.QueueTick(),
	}
	s.log("RemoteSync: [%v]", resp.Time)

	return nil
}

// RemoteBye means the client says goodbye and will disconnect shortly.
func (s *Server) RemoteBye(
	_ *rpc2.Client, _ *MsgEmpty, _ *MsgEmpty,
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

// ///// ///// /////

// ///// MISC

// ///// ///// /////

// BindServer binds RpcReady and HandshakeDone with Add/Remove, to custom
// states.
func BindServer(
	source, target *am.Machine, rpcReady, clientReady string,
) error {
	// TODO use ampipe.BindMany
	if rpcReady == "" || clientReady == "" {
		return fmt.Errorf("rpcReady and clientConn must be set")
	}

	h := &struct {
		RpcReadyState am.HandlerFinal
		RpcReadyEnd   am.HandlerFinal

		HandshakeDoneState am.HandlerFinal
		HandshakeDoneEnd   am.HandlerFinal
	}{
		RpcReadyState: ampipe.Add(source, target, ssS.RpcReady, rpcReady),
		RpcReadyEnd:   ampipe.Remove(source, target, ssS.RpcReady, rpcReady),

		HandshakeDoneState: ampipe.Add(source, target, ssS.ClientConnected,
			clientReady),
		HandshakeDoneEnd: ampipe.Remove(source, target, ssS.ClientConnected,
			clientReady),
	}

	return source.BindHandlers(h)
}

// BindServerMulti binds RpcReady, ClientConnected, and ClientDisconnected.
// RpcReady is Add/Remove, the other two are Add-only to passed multi states.
func BindServerMulti(
	source, target *am.Machine, rpcReady, clientConn, clientDisconn string,
) error {
	if rpcReady == "" || clientConn == "" || clientDisconn == "" {
		return fmt.Errorf("rpcReady, clientConn, and clientDisconn must be set")
	}
	// TODO use ampipe.BindMany

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
	// TODO use ampipe.Bind
	h := &struct {
		RpcReadyState am.HandlerFinal
	}{
		RpcReadyState: ampipe.Add(source, target, ssS.RpcReady, rpcReady),
	}

	return source.BindHandlers(h)
}

type ServerOpts struct {
	// PayloadState is a state for the server to listen on, to deliver payloads
	// to the client. The client activates this state to request a payload from
	// the worker. Default: am/rpc/states/WorkerStates.SendPayload.
	PayloadState string
	// Parent is a parent state machine for a new Server state machine.
	Parent am.Api // Typed arguments struct pointer
	// optional typed args struct value
	Args any
	// optional RPC args parser
	ParseRpc func(args am.A) am.A
	// Listen on a WebSocket connection instead of TCP.
	WebSocket bool
	// HTTP URL without proto to tunnel the TCP listen over a WebSocket conn.
	// See WsListenPath.
	WebSocketTunnel string
}

type SendPayloadHandlers struct {
	SendPayloadState am.HandlerFinal
}

// newSendPayloadState returns a handler (usually SendPayloadState) that will
// deliver a payload to the RPC client. The resulting function can be bound in
// anon handlers.
func newSendPayloadState(s *Server, stateName string) am.HandlerFinal {
	return func(e *am.Event) {
		// self-remove
		e.Machine().EvRemove1(e, stateName, nil)

		ctx := s.Mach.NewStateCtx(ssS.Start)
		args := ParseArgs(e.Args)
		argsOut := &A{Name: args.Name}

		// side-effect error handling
		if args.Payload == nil || args.Name == "" {
			err := fmt.Errorf("invalid payload args [name, payload]")
			e.Machine().EvAddErrState(e, ssSS.ErrSendPayload, err, Pass(argsOut))

			return
		}

		// unblock and forward to the client
		go func() {
			// timeout context
			ctx, cancel := context.WithTimeout(ctx, s.DeliveryTimeout)
			defer cancel()

			err := s.SendPayload(ctx, e, args.Payload)
			if err != nil {
				e.Machine().EvAddErrState(e, ssSS.ErrSendPayload, err, Pass(argsOut))
			}
		}()
	}
}

// calcUpdate calculates a new update based on previously pushed data.
func calcUpdate(
	syncSchema bool, data, lastPush *tracerData, shallowClocks bool,
) *MsgSrvUpdate {
	var idxs []uint16
	var ticks []uint32
	if shallowClocks {
		idxs, ticks = genShallowUpdate(syncSchema, data, lastPush)
	} else {
		idxs, ticks = genDeepUpdate(syncSchema, data, lastPush)
	}

	return &MsgSrvUpdate{
		QueueTick: uint16(data.queueTick - lastPush.queueTick),
		MachTick:  uint8(data.machTick - lastPush.machTick),
		Indexes:   idxs,
		Ticks:     ticks,
		Checksum:  data.checksum,
	}
}

// calcUpdate calculates a new mutation update based on the queue and previously
// pushed data.
func calcUpdateMutations(
	syncSchema bool, muts []tracerMutation, prev *tracerData,
) *MsgSrvUpdateMuts {
	ret := &MsgSrvUpdateMuts{}
	for i := range muts {
		mut := &muts[i]
		ret.MutationType = append(ret.MutationType, mut.mutType)
		called := make([]uint16, len(mut.calledIdxs))
		for ii := range mut.calledIdxs {
			called[ii] = uint16(mut.calledIdxs[ii])
		}
		ret.CalledStates = append(ret.CalledStates, called)
		ret.Updates = append(ret.Updates, *calcUpdate(syncSchema, &mut.data, prev,
			false))

		prev = &mut.data
	}

	return ret
}

func genDeepUpdate(
	syncSchema bool, data, lastPush *tracerData,
) (indexes []uint16, ticks []uint32) {
	indexes = make([]uint16, 0, len(data.tracked))
	ticks = make([]uint32, 0, len(data.tracked))

	for trackedIdx := range data.tracked {
		stateIdx := data.trackedIdxs[trackedIdx]
		prev := lastPush.mTime
		now := data.mTime

		// client has shorter state names
		pushedIdx := uint16(stateIdx)
		if !syncSchema {
			pushedIdx = uint16(trackedIdx)
		}

		// first call or new schema
		if prev == nil || trackedIdx >= len(prev) {
			if now[trackedIdx] == 0 {
				continue
			}
			indexes = append(indexes, pushedIdx)
			ticks = append(ticks, uint32(now[pushedIdx]))

			// regular update
		} else if prev[pushedIdx] != now[pushedIdx] {
			indexes = append(indexes, pushedIdx)
			ticks = append(ticks, uint32(now[pushedIdx]-prev[pushedIdx]))
		}
	}

	return indexes, ticks
}

func genShallowUpdate(
	syncSchema bool, data, lastPush *tracerData,
) (indexes []uint16, ticks []uint32) {
	indexes = make([]uint16, 0, len(data.tracked))
	ticks = make([]uint32, 0, len(data.tracked))

	for trackedIdx := range data.tracked {
		stateIdx := data.trackedIdxs[trackedIdx]
		prev := lastPush.mTime
		now := data.mTime

		// client has shorter state names
		pushedIdx := uint16(stateIdx)
		if !syncSchema {
			pushedIdx = uint16(trackedIdx)
		}

		// first call or new schema
		if prev == nil || int(pushedIdx) >= len(prev) {
			if now[pushedIdx] == 0 {
				continue
			}
			indexes = append(indexes, pushedIdx)
			tick := 0
			if am.IsActiveTick(now[pushedIdx]) {
				tick = 1
			}
			ticks = append(ticks, uint32(tick))

			// regular update
		} else if prev[pushedIdx]%2 != now[pushedIdx]%2 {
			indexes = append(indexes, pushedIdx)
			ticks = append(ticks, uint32(1))
		}
	}

	return indexes, ticks
}

// WEBSOCKET SERVER (not tunnel)

type wsHandlerServer struct {
	s     *Server
	event *am.Event
}

// ServeHTTP continues [Server.RpcStartingState].
func (h *wsHandlerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mach := h.s.Mach

	connWs, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// TODO security
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	conn := websocket.NetConn(mach.Context(), connWs, websocket.MessageBinary)
	h.s.Conn = conn

	// next and stay alive
	mach.EvAdd1(h.event, ssS.RpcAccepting, nil)
	select {
	case <-mach.WhenNot1(ss.Start, nil):
	case <-r.Context().Done():
	}
}
