package rpc

// TODO call ClientBye on Disposing

import (
	"context"
	"fmt"
	"net"
	"os"
	"reflect"
	"slices"
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
	ssW = states.NetSourceStates
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
	// Listener can be set manually before starting the server.
	Listener atomic.Pointer[net.Listener]
	// Conn can be set manually before starting the server.
	Conn net.Conn
	// NoNewListener will prevent the server from creating a new listener if
	// one is not provided or has been closed. Useful for cmux.
	NoNewListener bool
	LogEnabled    bool
	CallCount     uint64
	// Typed arguments struct value with defaults
	Args any
	// Typed arguments prefix in a resulting [am.A] map.
	ArgsPrefix string

	// sync settings

	// PushInterval is the interval for clock updates, effectively throttling
	// the number of updates sent to the client within the interval window.
	// 0 means pushes are disabled. Setting to a very small value will make
	// pushes instant.
	PushInterval atomic.Pointer[time.Duration]
	// syncMutations will push all clock changes for each mutation, enabling
	// client-side mutation filtering.
	syncMutations     bool
	syncAllowedStates am.S
	syncSkippedStates am.S
	syncSchema        bool
	syncShallowClocks bool

	// security

	// AllowId will limit clients to a specific ID, if set.
	AllowId string

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
}

// interfaces
var (
	_ serverRpcMethods    = &Server{}
	_ clientServerMethods = &Server{}
)

// NewServer creates a new RPC server, bound to a worker machine.
// The source machine has to implement [states.NetSourceStatesDef] interface.
func NewServer(
	ctx context.Context, addr string, name string, netSrcMach am.Api,
	opts *ServerOpts,
) (*Server, error) {
	if name == "" {
		name = "rpc"
	}
	if opts == nil {
		opts = &ServerOpts{}
	}

	// check the source
	if !netSrcMach.StatesVerified() {
		return nil, fmt.Errorf(
			"net source states not verified, call VerifyStates()")
	}
	hasHandlers := netSrcMach.HasHandlers()
	if hasHandlers && !netSrcMach.Has(ssW.Names()) {
		// error only when some handlers bound, skip deterministic machines
		err := fmt.Errorf(
			"%w: NetSourceMach with handlers has to implement "+
				"pkg/rpc/states/NetSourceStatesDef",
			am.ErrSchema)

		return nil, err
	}

	// TODO validate state names

	s := &Server{
		ExceptionHandler: &ExceptionHandler{},
		Addr:             addr,
		DeliveryTimeout:  5 * time.Second,
		LogEnabled:       os.Getenv(EnvAmRpcLogServer) != "",
		Source:           netSrcMach,
		Args:             opts.Args,
		ArgsPrefix:       opts.ArgsPrefix,

		lastPushData: &tracerData{},
	}
	interval := 250 * time.Millisecond
	s.PushInterval.Store(&interval)

	// state machine
	mach, err := am.NewCommon(ctx, "rs-"+name, states.ServerSchema, ssS.Names(),
		s, opts.Parent, &am.Opts{Tags: []string{"rpc-server"}})
	if err != nil {
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
	if err = netSrcMach.BindTracer(s.tracer); err != nil {
		return nil, err
	}
	netSrcMach.OnDispose(func(id string, ctx context.Context) {
		_ = netSrcMach.DetachTracer(s.tracer)
	})

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
			// dynamic handlers TODO use ampipe.Bind
			h = createSendPayloadHandlers(s, payloadState)
		}
		err = netSrcMach.BindHandlers(h)
		if err != nil {
			return nil, err
		}
		mach.OnDispose(func(id string, ctx context.Context) {
			_ = netSrcMach.DetachHandlers(h)
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
// for SendPayload. [event] is optional.
func (s *Server) SendPayload(
	ctx context.Context, event *am.Event, payload *MsgSrvPayload,
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
	if s.ArgsPrefix != "" && s.Args != nil {
		args = am.A{
			s.ArgsPrefix: amhelp.ArgsFromMap(req.Args, s.Args),
		}
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
	if s.ArgsPrefix != "" && s.Args != nil {
		args = am.A{
			s.ArgsPrefix: amhelp.ArgsFromMap(req.Args, s.Args),
		}
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
	if s.ArgsPrefix != "" && s.Args != nil {
		args = am.A{
			s.ArgsPrefix: amhelp.ArgsFromMap(req.Args, s.Args),
		}
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
	if s.ArgsPrefix != "" && s.Args != nil {
		args = am.A{
			s.ArgsPrefix: amhelp.ArgsFromMap(req.Args, s.Args),
		}
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

func (s *Server) RemoteArgs(
	_ *rpc2.Client, _ *MsgEmpty, resp *MsgSrvArgs,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}
	s.Mach.Add1(ssS.MetricSync, nil)

	// args TODO cache
	if s.Args != nil {
		args, err := utils.StructFields(s.Args)
		if err != nil {
			return err
		}
		(*resp).Args = args
	}

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

// ///// BINDINGS

// ///// ///// /////

// BindServer binds RpcReady and ClientConnected with Add/Remove, to custom
// states.
func BindServer(source, target *am.Machine, rpcReady, clientConn string) error {
	if rpcReady == "" || clientConn == "" {
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
			clientConn),
		HandshakeDoneEnd: ampipe.Remove(source, target, ssS.ClientConnected,
			clientConn),
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
	// Typed arguments struct pointer
	Args       any
	ArgsPrefix string
}

type SendPayloadHandlers struct {
	SendPayloadState am.HandlerFinal
}

// getSendPayloadState returns a handler (usually SendPayloadState) that will
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
	// TODO migrate to ampipe.Bind
	fn := getSendPayloadState(s, stateName)

	// define a struct with the handler
	structType := reflect.StructOf([]reflect.StructField{
		{
			Name: stateName + am.SuffixState,
			Type: reflect.TypeOf(fn),
		},
	})

	// new instance and set handler
	val := reflect.New(structType).Elem()
	val.Field(0).Set(reflect.ValueOf(fn))
	ret := val.Addr().Interface()

	return ret
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
