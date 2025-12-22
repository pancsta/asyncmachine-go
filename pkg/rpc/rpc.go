// Package rpc is a transparent RPC for state machines.
package rpc

import (
	"bufio"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/rpc2"
	"github.com/orsinium-labs/enum"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

func init() {
	gob.Register(&ARpc{})
	gob.Register(am.Relation(0))
}

const (
	// EnvAmRpcLogServer enables machine logging for RPC server.
	EnvAmRpcLogServer = "AM_RPC_LOG_SERVER"
	// EnvAmRpcLogClient enables machine logging for RPC client.
	EnvAmRpcLogClient = "AM_RPC_LOG_CLIENT"
	// EnvAmRpcLogMux enables machine logging for RPC multiplexers.
	EnvAmRpcLogMux = "AM_RPC_LOG_MUX"
	// EnvAmRpcDbg enables env-based debugging for RPC components.
	EnvAmRpcDbg = "AM_RPC_DBG"
	// EnvAmReplAddr is a REPL address to listen on. "1" expands to 127.0.0.1:0.
	EnvAmReplAddr = "AM_REPL_ADDR"
	// EnvAmReplDir is a dir path to save the address file as
	// $AM_REPL_DIR/mach-id.addr. Optional.
	EnvAmReplDir = "AM_REPL_DIR"

	PrefixNetMach = "rnm-"
)

var ss = states.SharedStates

// ///// ///// /////

// ///// TYPES

// ///// ///// /////

// RPC methods

type (
	ServerMethod enum.Member[string]
	ClientMethod enum.Member[string]
)

var (
	// methods define on the server

	ServerAdd       = ServerMethod{"Add"}
	ServerAddNS     = ServerMethod{"AddNS"}
	ServerRemove    = ServerMethod{"Remove"}
	ServerSet       = ServerMethod{"Set"}
	ServerHello     = ServerMethod{"Hello"}
	ServerHandshake = ServerMethod{"Handshake"}
	ServerLog       = ServerMethod{"Log"}
	ServerSync      = ServerMethod{"Sync"}
	ServerBye       = ServerMethod{"Close"}

	ServerMethods = enum.New(ServerAdd, ServerAddNS, ServerRemove, ServerSet,
		ServerHello, ServerHandshake, ServerLog, ServerSync, ServerBye)

	// methods define on the client

	ClientUpdate          = ClientMethod{"ClientSetClock"}
	ClientUpdateMutations = ClientMethod{"ClientSetClockMany"}
	ClientPushAllTicks    = ClientMethod{"ClientPushAllTicks"}
	ClientSendPayload     = ClientMethod{"ClientSendPayload"}
	ClientBye             = ClientMethod{"ClientBye"}
	ClientSchemaChange    = ClientMethod{"SchemaChange"}

	ClientMethods = enum.New(ClientUpdate, ClientPushAllTicks,
		ClientSendPayload, ClientBye, ClientSchemaChange)
)

// MSGS TODO migrate to msgpack, shorten names

// MsgCliHello is the client saying hello to the server.
type MsgCliHello struct {
	// ID of the client saying Hello.
	Id string
	// Client wants to synchronize the schema.
	SyncSchema bool
	// Hash of the current schema, or "". Schema is always full and not affected
	// by [MsgCliHello.AllowedStates] or [MsgCliHello.SkippedStates].
	SchemaHash    string
	SyncMutations bool
	AllowedStates am.S
	SkippedStates am.S
	ShallowClocks bool
	// TODO WhenArgs: []{ map[string]string: token }
}

// MsgSrvHello is the server saying hello to the client.
type MsgSrvHello struct {
	Schema     am.Schema
	Serialized *am.Serialized
	// total source states count
	StatesCount uint32
}

// MsgCliMutation is the client requesting a mutation from the server.
type MsgCliMutation struct {
	States []int
	Args   am.A
	Event  *am.Event
}

// MsgSrvMutation is the server replying to a mutation request for the client.
type MsgSrvMutation struct {
	Update    *MsgSrvUpdate
	Mutations *MsgSrvUpdateMuts
	Result    am.Result
}

// MsgSrvPayload is the server sending a payload to the client.
type MsgSrvPayload struct {
	// Name is used to distinguish different payload types at the destination.
	Name string
	// Source is the machine ID that sent the payload.
	Source string
	// SourceTx is transition ID.
	SourceTx string
	// Destination is an optional machine ID that is supposed to receive the
	// payload. Useful when using rpc.Mux.
	Destination string
	// Data is the payload data. The Consumer has to know the type.
	Data any

	// internal

	// Token is a unique random ID for the payload. Autofilled by the server.
	Token string
}

// MsgSrvSync is the server replying to a full sync request from the client.
type MsgSrvSync struct {
	Time      am.Time
	QueueTick uint64
	MachTick  uint32
}

// TODO type MsgCliWhenArgs struct {}

// MsgEmpty is an empty message of either the server or client.
type MsgEmpty struct{}

// MsgSrvUpdate is the server telling the client about a net source's update.
type MsgSrvUpdate struct {
	// Indexes of incremented states.
	Indexes []uint16
	// Clock diffs of incremented states.
	// TODO optimize: []uint16 and send 2 updates when needed
	Ticks []uint32
	// TODO optimize: for shallow clocks
	// Active []bool
	// QueueTick is an incremental diff for the queue tick.
	QueueTick uint16
	// MachTick is an incremental diff for the machine tick.
	MachTick uint8
	// Checksum is the last digit of (TimeSum + QueueTick + MachTick)
	Checksum uint8
	// DBGSum       uint64
	// DBGLastSum   uint64
	// DBGQTick     uint64
	// DBGLastQTick uint64
}

// MsgSrvUpdateMuts is like [MsgSrvUpdate] but contains several clock updates
// (one for each mutation), as well as extra mutation info.
type MsgSrvUpdateMuts struct {
	// TODO mind partially accepted auto states (fake called states).
	// Auto         bool
	MutationType []am.MutationType
	CalledStates [][]uint16
	Updates      []MsgSrvUpdate
}

// clientServerMethods is a shared interface for RPC client/server.
type clientServerMethods interface {
	GetKind() Kind
}

// Kind of the RCP component.
type Kind string

const (
	KindClient Kind = "client"
	KindServer Kind = "server"
)

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "am_rpc"

// A represents typed arguments of the RPC package. It's a typesafe alternative
// to [am.A].
type A struct {
	Id        string `log:"id"`
	Name      string `log:"name"`
	MachTime  am.Time
	QueueTick uint64
	MachTick  uint32
	Payload   *MsgSrvPayload
	Addr      string `log:"addr"`
	Err       error
	Method    string `log:"addr"`
	StartedAt time.Time
	Dispose   bool

	// non-rpc fields

	Client *rpc2.Client
}

// ARpc is a subset of A, that can be passed over RPC.
type ARpc struct {
	Id        string `log:"id"`
	Name      string `log:"name"`
	MachTime  am.Time
	QueueTick uint64
	MachTick  uint32
	Payload   *MsgSrvPayload
	Addr      string `log:"addr"`
	Err       error
	Method    string `log:"addr"`
	StartedAt time.Time
	Dispose   bool
}

// ParseArgs extracts A from [am.Event.Args][APrefix].
func ParseArgs(args am.A) *A {
	if r, _ := args[APrefix].(*ARpc); r != nil {
		return amhelp.ArgsToArgs(r, &A{})
	}
	if a, _ := args[APrefix].(*A); a != nil {
		return a
	}
	return &A{}
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	return am.A{APrefix: args}
}

// PassRpc prepares [am.A] from A to pass over RPC.
func PassRpc(args *A) am.A {
	return am.A{APrefix: amhelp.ArgsToArgs(args, &ARpc{})}
}

// LogArgs is an args logger for A.
func LogArgs(args am.A) map[string]string {
	a := ParseArgs(args)
	if a == nil {
		return nil
	}

	return amhelp.ArgsToLogMap(a, 0)
}

// // DEBUG for perf testing TODO tag
// type MsgSrvUpdate am.Time

// ///// ///// /////

// ///// RPC APIS

// ///// ///// /////

// serverRpcMethods is the main RPC server's exposed methods.
type serverRpcMethods interface {
	// rpc

	RemoteHello(client *rpc2.Client, args *MsgCliHello, resp *MsgSrvHello) error

	// mutations

	RemoteAdd(
		client *rpc2.Client, args *MsgCliMutation, resp *MsgSrvMutation) error
	RemoteRemove(
		client *rpc2.Client, args *MsgCliMutation, resp *MsgSrvMutation) error
	RemoteSet(
		client *rpc2.Client, args *MsgCliMutation, reply *MsgSrvMutation) error
}

// clientRpcMethods is the RPC server exposed by the RPC client for bi-di comm.
type clientRpcMethods interface {
	RemoteUpdate(worker *rpc2.Client, args *MsgSrvUpdate, resp *MsgEmpty) error
	RemoteUpdateMutations(
		worker *rpc2.Client, args *MsgSrvUpdateMuts, resp *MsgEmpty) error
	RemoteSendingPayload(
		worker *rpc2.Client, file *MsgSrvPayload, resp *MsgEmpty) error
	RemoteSendPayload(
		worker *rpc2.Client, file *MsgSrvPayload, resp *MsgEmpty) error
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
	ErrDestination   = errors.New("wrong destination")

	// ErrNetwork group

	ErrNetwork        = errors.New("network error")
	ErrNetworkTimeout = errors.New("network timeout")

	// TODO ErrDelivery
)

// wrapping error setters

func AddErrRpcStr(e *am.Event, mach *am.Machine, msg string) {
	err := fmt.Errorf("%w: %s", ErrRpc, msg)
	mach.EvAddErrState(e, ss.ErrRpc, err, nil)
}

func AddErrParams(e *am.Event, mach *am.Machine, err error) {
	err = fmt.Errorf("%w: %w", ErrInvalidParams, err)
	mach.AddErrState(ss.ErrRpc, err, nil)
}

func AddErrResp(e *am.Event, mach *am.Machine, err error) {
	err = fmt.Errorf("%w: %w", ErrInvalidResp, err)
	mach.AddErrState(ss.ErrRpc, err, nil)
}

func AddErrNetwork(e *am.Event, mach *am.Machine, err error) {
	mach.AddErrState(ss.ErrNetwork, err, nil)
}

func AddErrNoConn(e *am.Event, mach *am.Machine, err error) {
	err = fmt.Errorf("%w: %w", ErrNoConn, err)
	mach.AddErrState(ss.ErrNetwork, err, nil)
}

// AddErr detects sentinels from error msgs and calls the proper error setter.
// TODO also return error for compat
func AddErr(e *am.Event, mach *am.Machine, msg string, err error) {
	if msg != "" {
		err = fmt.Errorf("%w: %s", err, msg)
	}

	if strings.HasPrefix(err.Error(), "gob: ") {
		AddErrResp(e, mach, err)
	} else if strings.Contains(err.Error(), "rpc2: can't find method") {
		AddErrRpcStr(e, mach, err.Error())
	} else if strings.Contains(err.Error(), "connection is shut down") ||
		strings.Contains(err.Error(), "unexpected EOF") {

		// TODO bind to sentinels io.ErrUnexpectedEOF, rpc2.ErrShutdown
		mach.AddErrState(ss.ErrRpc, err, nil)
	} else if strings.Contains(err.Error(), "timeout") {
		AddErrNetwork(e, mach, errors.Join(err, ErrNetworkTimeout))
	} else if _, ok := err.(*net.OpError); ok {
		AddErrNetwork(e, mach, err)
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
	mach := e.Machine()

	isRpcClient := mach.Has(am.S{ssC.Disconnecting, ssC.Disconnected})
	if errors.Is(args.Err, ErrNetwork) && isRpcClient &&
		mach.Any1(ssC.Disconnecting, ssC.Disconnected) {

		// skip network errors on client disconnect
		e.Machine().Log("ignoring ErrNetwork on Disconnecting/Disconnected")
		return false
	}

	return true
}

// ///// ///// /////

// ///// LOGGER

// ///// ///// /////

type semLogger struct {
	mach  *NetworkMachine
	steps atomic.Bool
	graph atomic.Bool
}

// implement [SemLogger]
var _ am.SemLogger = &semLogger{}

func (s *semLogger) SetArgsMapper(mapper am.LogArgsMapperFn) {
	// TODO
}

func (s *semLogger) ArgsMapper() am.LogArgsMapperFn {
	// TODO
	return nil
}

func (s *semLogger) EnableId(val bool) {
	// TODO
}

func (s *semLogger) IsId() bool {
	return false
}

func (s *semLogger) SetLogger(fn am.LoggerFn) {
	if fn == nil {
		s.mach.logger.Store(nil)

		return
	}
	s.mach.logger.Store(&fn)
}

func (s *semLogger) Logger() am.LoggerFn {
	if l := s.mach.logger.Load(); l != nil {
		return *l
	}

	return nil
}

func (s *semLogger) SetLevel(lvl am.LogLevel) {
	s.mach.logLevel.Store(&lvl)
}

func (s *semLogger) Level() am.LogLevel {
	return *s.mach.logLevel.Load()
}

func (s *semLogger) SetEmpty(lvl am.LogLevel) {
	var logger am.LoggerFn = func(_ am.LogLevel, msg string, args ...any) {
		// no-op
	}
	s.mach.logger.Store(&logger)
	s.mach.logLevel.Store(&lvl)
}

func (s *semLogger) SetSimple(
	logf func(format string, args ...any), level am.LogLevel,
) {
	var logger am.LoggerFn = func(_ am.LogLevel, msg string, args ...any) {
		logf(msg, args...)
	}
	s.mach.logger.Store(&logger)
	s.mach.logLevel.Store(&level)
}

func (s *semLogger) AddPipeOut(addMut bool, sourceState, targetMach string) {
	kind := "remove"
	if addMut {
		kind = "add"
	}
	s.mach.log(am.LogOps, "[pipe-out:%s] %s to %s", kind, sourceState,
		targetMach)
}

func (s *semLogger) AddPipeIn(addMut bool, targetState, sourceMach string) {
	kind := "remove"
	if addMut {
		kind = "add"
	}
	s.mach.log(am.LogOps, "[pipe-in:%s] %s from %s", kind, targetState,
		sourceMach)
}

func (s *semLogger) RemovePipes(machId string) {
	s.mach.log(am.LogOps, "[pipe:gc] %s", machId)
}

func (s *semLogger) IsSteps() bool {
	return s.steps.Load()
}

func (s *semLogger) EnableSteps(enable bool) {
	s.steps.Store(enable)
}

func (s *semLogger) IsGraph() bool {
	return s.graph.Load()
}

func (s *semLogger) EnableGraph(enable bool) {
	s.graph.Store(enable)
}

// TODO more data types

func (s *semLogger) EnableStateCtx(val bool) {
	// TODO
}

func (s *semLogger) IsStateCtx() bool {
	return true
}

func (s *semLogger) EnableWhen(val bool) {
	// TODO
}

func (s *semLogger) IsWhen() bool {
	return true
}

func (s *semLogger) EnableArgs(val bool) {
	// TODO params for synthetic log
}

func (s *semLogger) IsArgs() bool {
	return true
}

func (s *semLogger) EnableQueued(val bool) {
	// TODO
}

func (s *semLogger) IsQueued() bool {
	return true
}

func (s *semLogger) EnableCan(enable bool) {
	// TODO
}

func (s *semLogger) IsCan() bool {
	return true
}

// ///// ///// /////

// ///// REMOTE HANDLERS

// ///// ///// /////
// handler represents a single event consumer, synchronized by channels.
type handler struct {
	h            any
	name         string
	mx           sync.Mutex
	methods      *reflect.Value
	methodCache  map[string]reflect.Value
	missingCache map[string]struct{}
}

func newHandler(
	handlers any, name string, methods *reflect.Value,
) *handler {
	return &handler{
		name:         name,
		h:            handlers,
		methods:      methods,
		methodCache:  make(map[string]reflect.Value),
		missingCache: make(map[string]struct{}),
	}
}

// ///// ///// /////

// ///// TRACERS

// ///// ///// /////

type tracerData struct {
	mTrackedTimeSum uint64
	// time is either source-bound or client-bound (if no schema)
	mTime     am.Time
	queueTick uint64
	machTick  uint32
	// tracked-states-only checksum
	checksum uint8
	tracked  am.S
	// tracked idx -> machine idx
	trackedIdxs []int
	// mach time on the client according to tracked states
	// mTimeSumClient uint64
}

type tracerMutation struct {
	mutType    am.MutationType
	calledIdxs []int
	data       tracerData
}

// sourceTracer is a tracer for source state-machines, used by the RPC server
// to produce updates for the RPC client.
type sourceTracer struct {
	*am.TracerNoOp

	s *Server
	// tracer needs explicit activation
	active bool
	// latest data, possibly already sent
	dataLatest *tracerData
	// unsent data generated for each mutations
	dataQueue []tracerMutation

	// list of states this tracer is syncing
	trackedStates am.S
	// tracked idx -> machine idx
	trackedStateIdxs []int
}

// getters

func (t *sourceTracer) DataLatest() *tracerData {
	// lock
	t.s.lockCollection.Lock()
	defer t.s.lockCollection.Unlock()

	// copy
	data := t.dataLatest
	if data == nil {
		return nil
	}
	ret := *data

	return &ret
}

func (t *sourceTracer) DataQueue() []tracerMutation {
	// lock
	t.s.lockCollection.Lock()
	defer t.s.lockCollection.Unlock()

	// copy and flush
	ret := t.dataQueue
	t.dataLatest = nil

	return ret
}

// tracing

func (t *sourceTracer) TransitionEnd(tx *am.Transition) {
	s := t.s
	srcMach := s.Source

	// lock
	s.lockCollection.Lock()
	defer s.lockCollection.Unlock()

	if !t.active {
		return
	}

	// init cache
	allStates := tx.Machine.StateNames()
	if t.trackedStates == nil {
		t.calcTrackedStates(allStates)
	}

	qTick := srcMach.QueueTick()
	machTick := srcMach.MachineTick()
	mTime := srcMach.Time(nil)
	trackedTSum := mTime.Filter(t.trackedStateIdxs).Sum(nil)

	// filter the time slice
	if !s.syncSchema {
		mTime = mTime.Filter(t.trackedStateIdxs)
	}
	if s.syncShallowClocks {
		mTime = am.NewTime(mTime, mTime.ActiveStates(nil))
		trackedTSum = mTime.Sum(nil)
	}

	// update
	d := &tracerData{
		mTime:           mTime,
		mTrackedTimeSum: trackedTSum,
		// mTimeSumClient: mTimeClient.Sum(nil),
		queueTick:   qTick,
		machTick:    machTick,
		checksum:    Checksum(trackedTSum, qTick, machTick),
		tracked:     t.trackedStates,
		trackedIdxs: t.trackedStateIdxs,
	}
	t.dataLatest = d

	// DEBUG
	// if srcMach.Id() == "ns-TestPartial" {
	// 	fmt.Printf("[T] [%+v] %d %d\n", d.mTime, qTick, machTick)
	// 	fmt.Printf("[T] check %d\n", d.checksum)
	// }

	// mutations
	if s.syncMutations {
		mut := tx.Mutation
		// skip non-tracked called states
		called := slices.DeleteFunc(mut.Called, func(idx int) bool {
			return !slices.Contains(t.trackedStateIdxs, idx)
		})
		t.dataQueue = append(t.dataQueue, tracerMutation{
			mutType:    mut.Type,
			calledIdxs: called,
			data:       *d,
		})
	}

	calledTracked := am.StatesShared(tx.TargetStates(), t.trackedStates)

	// TODO optimize: fork max 1?
	go func() {
		s.log("tracer push: tt%d q%d (check:%d) %s", trackedTSum, qTick,
			d.checksum, calledTracked)

		// try to push this tx to the client
		t.s.pushClient()
	}()
}

func (t *sourceTracer) SchemaChange(mach am.Api, oldSchema am.Schema) {
	s := t.s

	// lock
	s.lockCollection.Lock()
	defer s.lockCollection.Unlock()

	if !t.active {
		return
	}

	msg := &MsgSrvHello{}
	msg.Serialized, msg.Schema, _ = mach.Export()
	msg.StatesCount = uint32(len(msg.Serialized.StateNames))
	allStates := msg.Serialized.StateNames

	// client-bound indexes when no schema synced
	export := msg.Serialized
	if !s.syncSchema {
		export.StateNames = am.StatesShared(export.StateNames,
			s.tracer.trackedStates)
		export.Time = export.Time.Filter(s.tracer.trackedStateIdxs)

		// zero non-tracked for consistent checksums
	} else {
		for i := range allStates {
			if slices.Contains(s.tracer.trackedStateIdxs, i) {
				continue
			}
			export.Time[i] = 0
		}
	}

	// init cache
	if t.trackedStates == nil {
		t.calcTrackedStates(allStates)
	}

	// memorize
	d := s.lastPushData
	d.mTime = export.Time
	d.queueTick = export.QueueTick
	d.mTrackedTimeSum = export.Time.Sum(nil)
	d.checksum = Checksum(d.mTrackedTimeSum, d.queueTick, d.machTick)

	// fork and push
	go func() {
		client := t.s.rpcClient.Load()
		if client == nil {
			return
		}

		s.lockExport.Lock()
		defer s.lockExport.Unlock()

		// send
		err := client.CallWithContext(mach.Ctx(), ClientSchemaChange.Value, msg,
			&MsgEmpty{})
		mach.AddErr(err, nil)
	}()
}

// internal

func (t *sourceTracer) calcTrackedStates(states am.S) {
	s := t.s

	t.trackedStates = states
	if s.syncAllowedStates != nil {
		t.trackedStates = am.StatesShared(t.trackedStates, s.syncAllowedStates)
	}
	t.trackedStates = am.StatesDiff(t.trackedStates, s.syncSkippedStates)
	t.trackedStateIdxs = make([]int, len(t.trackedStates))
	for i, name := range t.trackedStates {
		t.trackedStateIdxs[i] = slices.Index(states, name)
	}
}

// ///// ///// /////

// ///// MSGPACK

// ///// ///// /////
// TODO

type msgpackCoded struct {
	rwc io.ReadWriteCloser
	// TODO
	dec    *gob.Decoder
	enc    *gob.Encoder
	encBuf *bufio.Writer
	mutex  sync.Mutex
}

type msgpackMsg struct {
	Seq    uint64
	Method string
	Error  string
}

// TODO optimize with msgpack
func NewMsgpackCodec(conn io.ReadWriteCloser) rpc2.Codec {
	buf := bufio.NewWriter(conn)
	return &msgpackCoded{
		rwc: conn,
		// TODO
		dec:    gob.NewDecoder(conn),
		enc:    gob.NewEncoder(buf),
		encBuf: buf,
	}
}

func (c *msgpackCoded) ReadHeader(
	req *rpc2.Request, resp *rpc2.Response,
) error {
	var msg msgpackMsg
	if err := c.dec.Decode(&msg); err != nil {
		return err
	}

	if msg.Method != "" {
		req.Seq = msg.Seq
		req.Method = msg.Method
	} else {
		resp.Seq = msg.Seq
		resp.Error = msg.Error
	}
	return nil
}

func (c *msgpackCoded) ReadRequestBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *msgpackCoded) ReadResponseBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *msgpackCoded) WriteRequest(
	r *rpc2.Request, body interface{},
) (err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if err = c.enc.Encode(r); err != nil {
		return
	}
	if err = c.enc.Encode(body); err != nil {
		return
	}

	return c.encBuf.Flush()
}

func (c *msgpackCoded) WriteResponse(
	r *rpc2.Response, body interface{},
) (err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if err = c.enc.Encode(r); err != nil {
		return
	}
	if err = c.enc.Encode(body); err != nil {
		return
	}
	return c.encBuf.Flush()
}

func (c *msgpackCoded) Close() error {
	return c.rwc.Close()
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

// MachReplEnv sets up a machine for a REPL connection in case AM_REPL_ADDR env
// var is set. See MachRepl.
func MachReplEnv(mach am.Api) <-chan error {
	addr := os.Getenv(EnvAmReplAddr)
	dir := os.Getenv(EnvAmReplDir)

	err := make(chan error)
	switch addr {
	case "":
		return err
	case "1":
		// expand 1 to default
		addr = ""
	}

	MachRepl(mach, addr, dir, nil, nil)

	return err
}

// MachRepl sets up a machine for a REPL connection, which allows for
// mutations, like any other RPC connection. See [/tools/cmd/arpc] for usage.
// This function is considered a debugging helper and can panic.
//
// addr: address to listen on, default to 127.0.0.1:0
// addrDir: optional dir path to save the address file as addrDir/mach-id.addr.
// addrCh: optional channel to send the address to, once ready
// errCh: optional channel to send err to, once ready
func MachRepl(
	mach am.Api, addr, addrDir string, addrCh chan<- string, errCh chan<- error,
) {
	if amhelp.IsTestRunner() {
		return
	}

	if addr == "" {
		addr = "127.0.0.1:0"
	}

	if mach.HasHandlers() && !mach.Has(ssW.Names()) {
		err := fmt.Errorf(
			"%w: REPL source has to implement pkg/rpc/states/NetSourceStatesDef",
			am.ErrSchema)

		// panic only early
		panic(err)
	}

	mux, err := NewMux(mach.Ctx(), "repl-"+mach.Id(), nil, &MuxOpts{
		Parent: mach,
	})
	// panic only early
	if err != nil {
		panic(err)
	}
	mux.Addr = addr
	mux.Source = mach
	mux.Start()

	if addrCh == nil && addrDir == "" {
		if errCh != nil {
			close(errCh)
		}
		return
	}

	go func() {
		// dispose ret channels
		defer func() {
			if errCh != nil {
				close(errCh)
			}
			if addrCh != nil {
				close(addrCh)
			}
		}()

		// prep the dir
		dirOk := false
		if addrDir != "" {
			if _, err := os.Stat(addrDir); os.IsNotExist(err) {
				err := os.MkdirAll(addrDir, 0o755)
				if err == nil {
					dirOk = true
				} else if errCh != nil {
					errCh <- err
				}
			} else {
				dirOk = true
			}
		}

		// wait for an addr
		<-mux.Mach.When1(ssM.Ready, nil)
		if addrCh != nil {
			addrCh <- mux.Addr
		}

		// save to dir
		if dirOk && addrDir != "" {
			err = os.WriteFile(
				filepath.Join(addrDir, mach.Id()+".addr"),
				[]byte(mux.Addr), 0o644,
			)
			if errCh != nil {
				errCh <- err
			}
		}
	}()
}

// // DEBUG for perf testing
// func NewClockMsg(before, after am.Time) MsgSrvUpdate {
//	return MsgSrvUpdate(after)
// }
//
// // DEBUG for perf testing
// func clockFromUpdate(before am.Time, msg MsgSrvUpdate) am.Time {
//	return am.Time(msg)
// }

// Checksum calculates a short checksum of current machine time and ticks.
func Checksum(mTime uint64, qTick uint64, machTick uint32) uint8 {
	return uint8(mTime + qTick + uint64(machTick))
}

// TrafficMeter measures the traffic of a listener and forwards it to a
// destination. Results are sent to the [counter] channel. Useful for testing
// and benchmarking.
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

func newClosedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
