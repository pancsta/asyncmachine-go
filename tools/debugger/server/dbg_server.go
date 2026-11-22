package server

// TODO rewrite to a better protocol with
//  - everything from the reader
//  - graphs

import (
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/soheilhy/cmux"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	"github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

var ss = states.DebuggerStates
var Pass = am.Pass

type A = types.A

// ///// ///// /////

// ///// CLIENT

// ///// ///// /////

type Exportable struct {
	// TODO refac to MsgSchema
	// TODO version schemas
	MsgStruct *dbg.DbgMsgStruct
	MsgTxs    []*dbg.DbgMsgTx
	// TODO clean up on GC
	Markers map[string]struct{}
	Version string
}

type Client struct {
	// bits which get saved into the go file
	*Exportable

	// current transition, 1-based, mirrors the slider (eg 1 means tx.ID == 0)
	// TODO atomic
	ReaderCollapsed bool
	Id              string
	MTimeSum        uint64
	SelectedGroup   string
	Connected       atomic.Bool
	ConnId          string
	SchemaHash      string
	// processed list of filtered tx _indexes_
	MsgTxsFiltered []int
	// cache of processed log entries
	LogMsgs [][]*am.LogEntry
	// indexes of txs with errors, desc order for bisects
	// TOOD refresh on GC
	Errors []int
	// processed
	MsgTxsParsed    []*types.MsgTxParsed
	MsgSchemaParsed *types.MsgSchemaParsed

	txCache   map[string]int
	txCacheMx sync.Mutex
}

func NewClient(connId string, msgSchema *dbg.DbgMsgStruct) *Client {
	c := &Client{
		Id:         msgSchema.ID,
		ConnId:     connId,
		SchemaHash: amhelp.SchemaHash(msgSchema.States),
		Exportable: &Exportable{
			MsgStruct: msgSchema,
			Markers:   make(map[string]struct{}),
		},
	}
	c.ParseSchema()

	return c
}

func (c *Client) TxIsMarked(txId string) bool {
	_, ok := c.Markers[txId]
	return ok
}

func (c *Client) TxMark(txId string, mark bool) {
	if mark {
		c.Markers[txId] = struct{}{}
	} else {
		delete(c.Markers, txId)
	}
}

func (c *Client) ClearCache() {
	c.txCache = nil
}

func (c *Client) LastActive() time.Time {
	if len(c.MsgTxs) == 0 {
		return time.Time{}
	}

	return *c.MsgTxs[len(c.MsgTxs)-1].Time
}

func (c *Client) HadErrSinceTx(tx, distance int) bool {
	if slices.Contains(c.Errors, tx) {
		return true
	}

	// see TestHadErrSince
	index := sort.Search(len(c.Errors), func(i int) bool {
		return c.Errors[i] < tx
	})

	if index >= len(c.Errors) {
		return false
	}

	return tx-c.Errors[index] < distance
}

func (c *Client) TxAtQueueTick(qTick uint64) int {
	l := len(c.MsgTxs)
	if l == 0 {
		return -1
	}

	i := sort.Search(l, func(i int) bool {
		return c.MsgTxs[i].QueueTick >= qTick
	})

	if i == l {
		i = l - 1
	}

	return i
}

// TxAtStateTick returns the index of the first transition after which [state]
// reached the [tick] of its clock, or the closest later transition.
func (c *Client) TxAtStateTick(state string, tick uint64) int {
	states := c.MsgStruct.StatesIndex
	if slices.Index(states, state) == -1 {
		return -1
	}

	l := len(c.MsgTxs)
	if l == 0 {
		return -1
	}

	i := sort.Search(l, func(i int) bool {
		return c.MsgTxs[i].Clock(states, state) >= tick
	})

	if i == l {
		i = l - 1
	}

	return i
}

func (c *Client) TxAtHTime(hTime time.Time) int {
	l := len(c.MsgTxs)
	if l == 0 {
		return -1
	}

	i := sort.Search(l, func(i int) bool {
		t := c.MsgTxs[i].Time
		return t.After(hTime) || t.Equal(hTime)
	})

	if i == l {
		i = l - 1
	}
	tx := c.MsgTxs[i]

	// pick the closer one
	if i > 0 && tx.Time.Sub(hTime) > hTime.Sub(*c.MsgTxs[i-1].Time) {
		return i
	} else {
		return i
	}
}

func (c *Client) StatesToIndexes(states am.S) []int {
	return helpers.StatesToIndexes(c.MsgStruct.StatesIndex, states)
}

func (c *Client) IndexesToStates(indexes []int) am.S {
	return helpers.IndexesToStates(c.MsgStruct.StatesIndex, indexes)
}

func (c *Client) StatesStringToIndex(states string) []int {
	return c.MsgStruct.StatesIndex.Index(strings.Split(states, " "))
}

// TxIndex returns the index of transition ID [id] or -1 if not found.
func (c *Client) TxIndex(id string) int {
	c.txCacheMx.Lock()
	defer c.txCacheMx.Unlock()

	if c.txCache == nil {
		c.txCache = make(map[string]int)
	}
	if idx, ok := c.txCache[id]; ok {
		return idx
	}

	for i, tx := range c.MsgTxs {
		if tx.ID == id {
			c.txCache[id] = i
			return i
		}
	}

	return -1
}

func (c *Client) Tx(idx int) *dbg.DbgMsgTx {
	if idx < 0 || idx >= len(c.MsgTxs) {
		return nil
	}

	return c.MsgTxs[idx]
}

func (c *Client) TxParsed(idx int) *types.MsgTxParsed {
	if idx < 0 || idx >= len(c.MsgTxsParsed) {
		return nil
	}

	return c.MsgTxsParsed[idx]
}

// TxExecutedBy returns the execution transition for a queued transition
func (c *Client) TxExecutedBy(idx int) *dbg.DbgMsgTx {
	tx := c.MsgTxs[idx]
	if tx == nil || !tx.IsQueued {
		return nil
	}
	var executed *dbg.DbgMsgTx

	// DEBUG
	// // look into the future TODO links to the wrong one
	// for iii := c.CursorTx1; iii < len(c.MsgTxs); iii++ {
	// 	check := c.MsgTxs[iii]
	// 	txCalled := tx.CalledStateNames(statesIndex)
	// 	checkCalled := check.CalledStateNames(statesIndex)
	// 	if !check.IsQueued && am.StatesEqual(txCalled, checkCalled) {
	// 		d.Mach.Log("exec tx.MQT: %d, check.QT: %d",
	// 			tx.MutQueueTick, check.QueueTick)
	// 		d.Mach.Log("exec tx.Called: %s, check.Called: %s",
	// 			txCalled,
	// 			checkCalled)
	// 	}
	// }

	// look into the future
	for iii := idx + 1; iii < len(c.MsgTxs); iii++ {
		check := c.MsgTxs[iii]

		if check.IsQueued {
			continue
		}

		if check.QueueTick == tx.MutQueueTick ||
			(check.MutQueueToken > 0 && check.MutQueueToken == tx.MutQueueToken) {

			executed = check
			break
		}
	}

	return executed
}

func (c *Client) TxAtMachTime(sum uint64) int {
	idx, ok := slices.BinarySearchFunc(c.MsgTxsParsed,
		&types.MsgTxParsed{TimeSum: sum}, func(i, j *types.MsgTxParsed) int {
			if i.TimeSum < j.TimeSum {
				return -1
			} else if i.TimeSum > j.TimeSum {
				return 1
			}

			return 0
		})

	if !ok {
		return 0
	}

	return idx
}

func (c *Client) FilterIndexByCursor1(cursor1 int) int {
	if cursor1 == 0 {
		return 0
	}

	return slices.Index(c.MsgTxsFiltered, cursor1-1)
}

func (c *Client) ParseSchema() {
	// defaults
	schema := c.MsgStruct
	sp := &types.MsgSchemaParsed{
		GroupsOrder: []string{"all"},
		Groups: map[string]am.S{
			"all": schema.StatesIndex,
		},
	}
	c.MsgSchemaParsed = sp
	sp.Hash = amhelp.SchemaHash(schema.States)

	if len(schema.GroupsOrder) == 0 {
		return
	}

	// schema groups
	pastSelf := false
	prev := am.S{}
	for _, g := range schema.GroupsOrder {
		// TODO terrible
		name := strings.TrimSuffix(g, "StatesDef")
		if g == "self" {
			name = "- self"
		} else if pastSelf {
			name = "- " + name
		}
		sp.GroupsOrder = append(sp.GroupsOrder, name)
		sp.Groups[name] = c.IndexesToStates(schema.Groups[g])

		if pastSelf {
			// merge with prev groups TODO why? breaks inheriting from 2 sources
			sp.Groups[g] = slices.Concat(sp.Groups[name], prev)
			prev = sp.Groups[name]
		}

		if g == "self" {
			pastSelf = true
		}
	}
}

// TODO enable when needed
// func (c *Client) txByQueueTick(qTick uint64) int {
// 	idx, ok := slices.BinarySearchFunc(c.MsgTxs,
// 		&telemetry.DbgMsgTx{QueueTick: qTick},
// 		func(i, j *telemetry.DbgMsgTx) int {
// 			if i.QueueTick < j.QueueTick {
// 				return -1
// 			} else if i.QueueTick > j.QueueTick {
// 				return 1
// 			}
//
// 			return 0
// 		})
//
// 	if !ok {
// 		return 0
// 	}
//
// 	return idx
// }
//
// func (c *Client) txByMutQueueTick(qTick uint64) int {
// 	idx, ok := slices.BinarySearchFunc(c.MsgTxs,
// 		&telemetry.DbgMsgTx{MutQueueTick: qTick},
// 		func(i, j *telemetry.DbgMsgTx) int {
// 			if i.MutQueueTick < j.MutQueueTick {
// 				return -1
// 			} else if i.MutQueueTick > j.MutQueueTick {
// 				return 1
// 			}
//
// 			return 0
// 		})
//
// 	if !ok {
// 		return 0
// 	}
//
// 	return idx
// }

// TODO
// func NewClient()

// ///// ///// /////

// ///// SERVER

// ///// ///// /////

const (
	fwdBackoffMin  = 500 * time.Millisecond
	fwdBackoffMax  = 5 * time.Second
	fwdDialTimeout = 5 * time.Second
	fwdCallTimeout = 5 * time.Second
	// fwdDialWait is how long [fwdConn.Call] waits for an in-flight dial (and
	// its replays) before dropping the msg. Kept short, so a black-holed dial
	// can't stall the other fwd targets.
	fwdDialWait = 200 * time.Millisecond
	// fwdReplayMaxDefault is the default number of recent tx msgs kept in
	// memory for replaying to (re)connected fwd targets. Overridable via the
	// --fwd-replay-max CLI param.
	fwdReplayMaxDefault = 100
)

// fwdConn is a lazily- and re-dialed RPC connection to a forwarding target.
// It survives target downtime (both at startup and mid-stream) by redialing
// in the background with an exponential backoff, instead of failing the
// whole dbg server or permanently giving up on a forward target.
//
// Failing targets are isolated: callers treat fwd errors as non-fatal, and
// both dialing and calls are bounded with timeouts, so a missing or hung
// target cannot stall the other fwd targets nor the sending client.
type fwdConn struct {
	addr string
	mach *am.Machine
	// replayMax is the number of recent tx msgs kept per stream, see
	// [fwdReplayMaxDefault].
	replayMax int

	mx       sync.Mutex
	cli      *rpc.Client
	dialing  bool
	backoff  time.Duration
	nextDial time.Time
	// dialDone is closed when the in-flight dial attempt finishes, so callers
	// can wait for it (and its replays) instead of dropping msgs.
	// See [fwdConn.Call].
	dialDone chan struct{}
	// streams are the replay sources of this dbg server, one per accepted
	// conn, injected on accept and removed on disconnect.
	// See [fwdConn.addStream].
	streams []*RPCServer
	// detached keeps the streams of disconnected conns, so their buffered
	// msgs stay replayable until evicted by the [fwdConn.replayMax] budget.
	// See [fwdConn.removeStream].
	detached []*RPCServer
}

// newFwdConn creates a fwdConn and kicks off the initial (non-blocking)
// dial.
func newFwdConn(mach *am.Machine, addr string, replayMax int) *fwdConn {
	f := &fwdConn{
		addr:      addr,
		mach:      mach,
		replayMax: replayMax,
		backoff:   fwdBackoffMin,
	}
	go f.dial()

	return f
}

// retry schedules another dial attempt, doubling the backoff (capped at
// fwdBackoffMax). Requires f.mx to be held by the caller.
func (f *fwdConn) retry(reason error) {
	delay := f.backoff
	f.nextDial = time.Now().Add(delay)
	f.backoff *= 2
	if f.backoff > fwdBackoffMax {
		f.backoff = fwdBackoffMax
	}
	f.mach.Log("dbg fwd to %s failed, retrying in %s: %s",
		f.addr, delay, reason)
	go func() {
		time.Sleep(delay)
		f.dial()
	}()
}

// dial (re)connects, replays known client schemas and recent msgs as the first
// messages on the fresh connection, and resets the backoff on success. Safe to
// call concurrently. f.mx is not held while dialing, so a hanging target cannot
// block calls to the other targets.
func (f *fwdConn) dial() {
	f.mx.Lock()
	if f.cli != nil || f.dialing || time.Now().Before(f.nextDial) {
		f.mx.Unlock()
		return
	}
	f.dialing = true
	done := make(chan struct{})
	f.dialDone = done
	f.mx.Unlock()

	// waiters in [fwdConn.Call] get released on every path, after f.cli is set
	defer close(done)

	cli, err := dialFwd(f.addr)
	if err == nil {
		// replay known client schemas first, so a (re)connected target learns
		// about clients it missed while this fwd link was down
		err = f.pushClientSchemas(cli)
		if err == nil {
			// then the recent tx msgs it missed
			err = f.pushClientMsgs(cli)
		}
		if err != nil {
			err = fmt.Errorf("replay: %w", err)
		}
	}

	f.mx.Lock()
	f.dialing = false
	if err == nil {
		f.mach.Log("dbg fwd to %s connected", f.addr)
		f.cli = cli
		f.backoff = fwdBackoffMin
		f.mx.Unlock()
		return
	}
	if cli != nil {
		_ = cli.Close()
	}
	f.retry(err)
	f.mx.Unlock()
}

// dialFwd connects to addr with a timeout, so a missing or black-holed target
// cannot hang the dialer.
func dialFwd(addr string) (*rpc.Client, error) {
	conn, err := net.DialTimeout("tcp", addr, fwdDialTimeout)
	if err != nil {
		return nil, err
	}

	return rpc.NewClient(conn), nil
}

// call performs an RPC call with a timeout, so a half-open (accepted but
// unresponsive) target cannot hang the caller. f.mx is not required.
func (f *fwdConn) call(
	cli *rpc.Client, serviceMethod string, args, reply any,
) error {
	// client.Call has no timeout, so use Go + a timer to survive half-open
	// (accepted but unresponsive) targets
	call := cli.Go(serviceMethod, args, reply, make(chan *rpc.Call, 1))
	select {
	case res := <-call.Done:
		return res.Error
	case <-time.After(fwdCallTimeout):
		return fmt.Errorf("dbg fwd to %s: call timeout after %s",
			f.addr, fwdCallTimeout)
	}
}

// Call forwards an RPC call over the current connection. If a dial (and its
// replays) is in flight, it waits for it first. If disconnected, or the call
// fails or times out, it triggers a reconnect with a backoff and returns an
// error, leaving the caller to drop the message. Callers must treat errors as
// non-fatal for the other fwd targets.
func (f *fwdConn) Call(serviceMethod string, args, reply any) error {
	f.mx.Lock()
	cli := f.cli
	done := f.dialDone
	f.mx.Unlock()

	// wait for an in-flight (re)connect, incl. its schema and msg replays,
	// instead of dropping the msg - otherwise anything sent during a replay
	// (eg a new client's schema) would be lost until the next reconnect
	if cli == nil && done != nil {
		select {
		case <-done:
		case <-time.After(fwdDialWait):
		}
		f.mx.Lock()
		cli = f.cli
		f.mx.Unlock()
	}

	if cli == nil {
		go f.dial()
		return fmt.Errorf("dbg fwd to %s: not connected", f.addr)
	}

	err := f.call(cli, serviceMethod, args, reply)

	if err != nil {
		f.mx.Lock()
		if f.cli == cli {
			_ = cli.Close()
			f.cli = nil
		}
		f.retry(err)
		f.mx.Unlock()
	}

	return err
}

// New initializes listeners, fwd conns, and returns unstated servers.
func New(
	mach *am.Machine, addr string, params func() *types.Params,
) (cmux.CMux, *http.Server, error) {
	// TODO extract conns, write a sibling impl on AM
	// TODO dont listen in WASM

	p := params()
	var err error
	gob.Register(am.Relation(0))
	gob.Register(Exportable{})
	if addr == "" {
		addr = dbg.DbgAddr
	}
	_, addrPort, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(addrPort)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	mach.Log("dbg server started at %s", addr)

	// connect to other instances (non-blocking, reconnects with a backoff)
	// TODO add mutable FwdData setter
	replayMax := p.FwdReplayMax
	if replayMax <= 0 {
		replayMax = fwdReplayMaxDefault
	}
	fwdTo := make([]*fwdConn, len(p.FwdData))
	for i, a := range p.FwdData {
		fwdTo[i] = newFwdConn(mach, a, replayMax)
	}

	// first cmux for tcp
	mux := cmux.New(lis)
	tcpL := mux.Match(cmux.Any())
	go tcpAccept(tcpL, mach, fwdTo)

	// HTTP and WS on port+1
	var httpSrv *http.Server
	if p.UiWeb {
		httpAddr := fmt.Sprintf("%s:%d",
			addr[:strings.LastIndex(addr, ":")], port+1)
		httpMux := http.ServeMux{}
		httpMux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
			httpHandlerIndex(w, r, mach, params())
		})
		httpMux.Handle("/", http.FileServer(http.Dir(p.OutputDir)))

		// diagrams

		httpMux.HandleFunc("/viewer/mach",
			newDiagHandler(mach, types.DiagramTypeMach))
		httpMux.HandleFunc("/viewer/mach.svg",
			newDiagHandler(mach, types.DiagramTypeMach))
		httpMux.HandleFunc("/viewer/mach.ws",
			newWsDiagHandler(mach, types.DiagramTypeMach))

		httpMux.HandleFunc("/viewer/state",
			newDiagHandler(mach, types.DiagramTypeState))
		httpMux.HandleFunc("/viewer/state.svg",
			newDiagHandler(mach, types.DiagramTypeState))
		httpMux.HandleFunc("/viewer/state.ws",
			newWsDiagHandler(mach, types.DiagramTypeState))

		httpMux.HandleFunc("/viewer/steps",
			newDiagHandler(mach, types.DiagramTypeSteps))
		httpMux.HandleFunc("/viewer/steps.svg",
			newDiagHandler(mach, types.DiagramTypeSteps))
		httpMux.HandleFunc("/viewer/steps.ws",
			newWsDiagHandler(mach, types.DiagramTypeSteps))

		httpMux.HandleFunc("/viewer/graph",
			newDiagHandler(mach, types.DiagramTypeGraph))
		httpMux.HandleFunc("/viewer/graph.svg",
			newDiagHandler(mach, types.DiagramTypeGraph))
		httpMux.HandleFunc("/viewer/graph.ws",
			newWsDiagHandler(mach, types.DiagramTypeGraph))

		httpMux.HandleFunc("/dbg.ws", func(w http.ResponseWriter, r *http.Request) {
			wsDbgHandler(w, r, mach, fwdTo)
		})

		httpSrv = &http.Server{
			Handler: &httpMux,
			Addr:    httpAddr,
		}
	}

	return mux, httpSrv, nil
}

// TODO rename to RPCServer (compat break)
type RPCServer struct {
	Mach *am.Machine
	// TODO ConnId
	ConnID string
	FwdTo  []*fwdConn
	// replayMax is the max number of recent tx msgs to buffer for replay,
	// copied from the fwd targets. See [fwdReplayMaxDefault].
	replayMax int

	// txBatch batches incoming tx msgs into [ss.ClientMsg] mutations, see
	// [RPCServer.flushTxs].
	txBatch *amhelp.Batch[txBatchItem]

	// clientMsgs holds the last [RPCServer.replayMax] tx msgs of this conn's
	// stream (a direct client, or all clients behind a fwd link), for replaying
	// to (re)connected fwd targets. Copy-on-write, see [RPCServer.appendMsg].
	clientMsgs atomic.Pointer[[]*DbgMsgTx]

	// clientSchemas holds the latest schema of each client of this conn's
	// stream, keyed by machine ID. A new schema always replaces the old one.
	// Copy-on-write, see [RPCServer.setSchema].
	clientSchemas atomic.Pointer[map[string]*DbgMsgSchemaFwd]
}

type DbgMsgSchemaFwd struct {
	MsgStruct *dbg.DbgMsgStruct
	ConnId    string
}

// appendMsg buffers msg as one of the last [RPCServer.replayMax] msgs of this
// conn's stream, which get replayed to (re)connected fwd targets. Older msgs
// are dropped. Published snapshots are copy-on-write, so replaying needs no
// locks, and the CAS loop keeps concurrent appends (one goroutine per RPC call)
// safe.
func (r *RPCServer) appendMsg(msg *DbgMsgTx) {
	for {
		old := r.clientMsgs.Load()
		var msgs []*DbgMsgTx
		if old != nil {
			msgs = slices.Clone(*old)
		}

		// drop the oldest ones over the limit
		if r.replayMax > 0 && len(msgs) >= r.replayMax {
			msgs = msgs[len(msgs)-r.replayMax+1:]
		}
		msgs = append(msgs, msg)

		if r.clientMsgs.CompareAndSwap(old, &msgs) {
			return
		}
	}
}

// msgCount returns the number of buffered msgs of this conn.
func (r *RPCServer) msgCount() int {
	if msgs := r.clientMsgs.Load(); msgs != nil {
		return len(*msgs)
	}

	return 0
}

// setSchema caches the latest schema of a client of this conn's stream, keyed
// by machine ID, replacing any older schema of that client. Published
// snapshots are copy-on-write, so replaying needs no locks, and the CAS loop
// keeps concurrent updates (one goroutine per RPC call) safe.
func (r *RPCServer) setSchema(msg *DbgMsgSchemaFwd) {
	for {
		old := r.clientSchemas.Load()
		schemas := make(map[string]*DbgMsgSchemaFwd)
		if old != nil {
			for id, schema := range *old {
				schemas[id] = schema
			}
		}
		schemas[msg.MsgStruct.ID] = msg

		if r.clientSchemas.CompareAndSwap(old, &schemas) {
			return
		}
	}
}

// clearSchemas drops all cached schemas of this conn, so a disconnected client
// is never resent to fwd targets. Buffered msgs are kept, see
// [fwdConn.removeStream].
func (r *RPCServer) clearSchemas() {
	r.clientSchemas.Store(nil)
}

// addStream injects conn as a replay source for this target, so its schemas
// and buffered msgs get replayed on (re)connect. See [fwdConn.dial].
func (f *fwdConn) addStream(r *RPCServer) {
	f.mx.Lock()
	defer f.mx.Unlock()

	f.streams = append(f.streams, r)
}

// removeStream drops conn from the replay sources, but keeps it in [detached]
// so its buffered msgs stay replayable until evicted by the
// [fwdConn.replayMax] budget. Schemas are cleared by the caller, see
// [RPCServer.clearSchemas].
func (f *fwdConn) removeStream(r *RPCServer) {
	f.mx.Lock()
	defer f.mx.Unlock()

	for i, s := range f.streams {
		if s == r {
			f.streams = append(f.streams[:i], f.streams[i+1:]...)
			break
		}
	}

	f.detached = append(f.detached, r)

	// evict the oldest detached streams over the msgs budget
	total := 0
	for _, s := range f.detached {
		total += s.msgCount()
	}
	for total > f.replayMax && len(f.detached) > 0 {
		total -= f.detached[0].msgCount()
		f.detached = f.detached[1:]
	}
}

// pushClientSchemas replays the schema of every currently connected client
// over cli, as the first messages on a freshly (re)connected fwd link, so
// the target learns about clients it missed while disconnected.
func (f *fwdConn) pushClientSchemas(cli *rpc.Client) error {
	f.mx.Lock()
	streams := slices.Clone(f.streams)
	f.mx.Unlock()

	for _, stream := range streams {
		schemas := stream.clientSchemas.Load()
		if schemas == nil {
			continue
		}

		// deterministic order, so replays are stable
		ids := make([]string, 0, len(*schemas))
		for id := range *schemas {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		for _, id := range ids {
			msg := (*schemas)[id]
			if err := f.call(cli, "RPCServer.DbgMsgSchemaFwd", msg, nil); err != nil {
				return err
			}
		}
	}

	return nil
}

// pushClientMsgs replays the buffered tx msgs right after
// [fwdConn.pushClientSchemas] on a freshly (re)connected fwd link. It pushes
// everything, so the target is always fully caught up. Msgs of disconnected
// conns are replayed last, from [fwdConn.detached].
func (f *fwdConn) pushClientMsgs(cli *rpc.Client) error {
	f.mx.Lock()
	streams := slices.Clone(f.streams)
	streams = append(streams, f.detached...)
	f.mx.Unlock()

	for _, stream := range streams {
		msgs := stream.clientMsgs.Load()
		if msgs == nil {
			continue
		}

		for _, msg := range *msgs {
			if err := f.call(cli, "RPCServer.DbgMsgTxFwd", msg, nil); err != nil {
				return err
			}
		}
	}

	return nil
}

// TODO rename to RemoteMsgSchemaReceive (compat break)
func (r *RPCServer) DbgMsgSchemaFwd(
	msg *DbgMsgSchemaFwd, _ *string,
) error {

	r.Mach.Add1(ss.ConnectEvent, Pass(&A{
		MsgStruct: msg.MsgStruct,
		ConnId:    msg.ConnId,
		ClientId:  msg.MsgStruct.ID,
	}))

	// cache for fwd replay, a new schema replaces the old one of that client
	if len(r.FwdTo) > 0 {
		r.setSchema(msg)
	}

	return nil
}

func (r *RPCServer) DbgMsgSchema(
	msgSchema *dbg.DbgMsgStruct, _ *string,
) error {

	r.Mach.Add1(ss.ConnectEvent, Pass(&A{
		MsgStruct: msgSchema,
		ConnId:    r.ConnID,
		ClientId:  msgSchema.ID,
	}))

	// cache for fwd replay, a new schema replaces the old one of that client
	if len(r.FwdTo) > 0 {
		r.setSchema(&DbgMsgSchemaFwd{
			MsgStruct: msgSchema,
			ConnId:    r.ConnID,
		})
	}

	// fwd to other instances (best effort), a missing target must not affect
	// the sender nor the other targets
	for _, host := range r.FwdTo {
		fwdMsg := DbgMsgSchemaFwd{
			MsgStruct: msgSchema,
			ConnId:    r.ConnID,
		}
		// TODO const name
		// errors are logged and retried by the fwd conn itself
		_ = host.Call("RPCServer.DbgMsgSchemaFwd", &fwdMsg, nil)
	}

	return nil
}

func (r *RPCServer) DbgMsgTx(msgTx *dbg.DbgMsgTx, _ *string) error {
	r.enqueueTx(msgTx, r.ConnID)

	return nil
}

// txBatchItem is a tx msg with its origin conn ID, batched before being
// pushed as [ss.ClientMsg].
type txBatchItem struct {
	MsgTx  *dbg.DbgMsgTx
	ConnId string
}

// enqueueTx batches a tx msg for the [ss.ClientMsg] mutation and forwards it
// to other instances (best effort).
func (r *RPCServer) enqueueTx(msgTx *dbg.DbgMsgTx, connId string) {
	now := time.Now()
	msgTx.Time = &now
	r.txBatch.Add(txBatchItem{MsgTx: msgTx, ConnId: connId})

	// fwd to other instances, outside the batch lock, so a slow fwd target
	// cannot stall the batch nor the other targets
	r.fwdTx(msgTx, connId)
}

// flushTxs pushes a batch of tx msgs as a single [ss.ClientMsg] mutation.
func (r *RPCServer) flushTxs(items []txBatchItem) {
	msgs := make([]*dbg.DbgMsgTx, len(items))
	connIds := make([]string, len(items))
	for i, item := range items {
		msgs[i] = item.MsgTx
		connIds[i] = item.ConnId
	}

	r.Mach.Add1(ss.ClientMsg, Pass(&A{
		MsgsTx:  msgs,
		ConnIds: connIds,
	}))
}

// fwdTx forwards a message to all fwd targets, best effort. Errors are logged
// and retried by [fwdConn] itself; one failing target must not affect the
// others nor the sending client.
func (r *RPCServer) fwdTx(msgTx *dbg.DbgMsgTx, connId string) {
	fwdMsg := &DbgMsgTx{
		MsgTx:  msgTx,
		ConnId: connId,
	}

	// buffer recent msgs, so (re)connected targets can be caught up after
	// their schemas, see [fwdConn.pushClientMsgs]. Instances without fwd
	// targets pay nothing.
	if len(r.FwdTo) > 0 {
		r.appendMsg(fwdMsg)
	}

	for _, host := range r.FwdTo {
		// TODO const name
		_ = host.Call("RPCServer.DbgMsgTxFwd", fwdMsg, nil)
	}
}

type DbgMsgTx struct {
	MsgTx  *dbg.DbgMsgTx
	ConnId string
}

// TODO rename to RemoteMsgTxReceive (compat break)
func (r *RPCServer) DbgMsgTxFwd(msg *DbgMsgTx, _ *string) error {
	r.enqueueTx(msg.MsgTx, msg.ConnId)

	return nil
}

func tcpAccept(l net.Listener, mach *am.Machine, fwdTo []*fwdConn) {
	// TODO restart on error
	// defer mach.PanicToErr(nil)

	for {
		conn, err := l.Accept()
		if err != nil {
			// log.Println(err)
			mach.AddErr(err, nil)
			continue
		}

		// handle the client
		go AcceptConn(conn, mach, fwdTo)
	}
}

// AcceptConn accepts a std rpc connection, or alike.
func AcceptConn(
	conn io.ReadWriteCloser, mach *am.Machine, fwdTo []*fwdConn,
) {
	server := rpc.NewServer()
	connId := utils.RandId(8)
	rcvr := &RPCServer{
		Mach:   mach,
		ConnID: connId,
		FwdTo:  fwdTo,
	}
	rcvr.txBatch = &amhelp.Batch[txBatchItem]{
		Window: time.Second,
		Flush:  rcvr.flushTxs,
	}
	// all fwd targets share the same replay budget, see [newFwdConn]
	if len(fwdTo) > 0 {
		rcvr.replayMax = fwdTo[0].replayMax
	}

	err := server.Register(rcvr)
	if err != nil {
		rcvr.Mach.AddErr(err, nil)
		// TODO err msg
		fmt.Println(err)
		return
	}

	// inject the conn as a replay source, so fwd targets get its schemas and
	// buffered msgs on (re)connect. This happens before ServeConn, so no msg
	// of this conn can be missed by a replay.
	for _, f := range fwdTo {
		f.addStream(rcvr)
	}

	server.ServeConn(conn)

	// keep the buffered msgs replayable, but stop resending dead schemas
	rcvr.clearSchemas()
	for _, f := range fwdTo {
		f.removeStream(rcvr)
	}

	// TODO pass to range fwdTo (dedicated RPC method)
	rcvr.Mach.Add1(ss.DisconnectEvent, Pass(&A{
		ConnId: connId,
	}))
}

// ///// ///// /////

// ///// WEB

// ///// ///// /////

// httpHandlerIndex lists --dir
func httpHandlerIndex(
	w http.ResponseWriter, r *http.Request, mach *am.Machine, p *types.Params,
) {
	middleware(w)

	// Read directory contents
	entries, err := os.ReadDir(p.OutputDir)
	if err != nil {
		mach.AddErr(err, nil)
		http.Error(w, "Failed to read directory", http.StatusInternalServerError)
		return
	}

	// compute addr TODO take from dbg instance
	httpAddr := ""
	if p.ListenAddr != "-1" && p.ListenAddr != "" {
		host, port, err := net.SplitHostPort(p.ListenAddr)
		httpAddr = host

		// global listen
		if host == "0.0.0.0" {
			listenHost, err := utils.GetGlobalUnicastIp()
			if err == nil {
				httpAddr = listenHost
			}
		}

		if err == nil {
			dbgPort, _ := strconv.Atoi(port)
			httpAddr += fmt.Sprintf(":%d", dbgPort+1)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	// Build HTML response
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<html><head><title>am-dbg --dir %s</title></head><body>",
		p.OutputDir)
	fmt.Fprintf(w, "<h1>am-dbg</h1><hr>")

	fmt.Fprintf(w, "<h2>Pages</h2><hr><ul>")

	// diagrams
	if p.OutputDiagrams != types.ParamsOutputDiagramsNone {
		fmt.Fprintf(w, "<li><a href=\"/viewer/graph\">"+
			"Graph Diagram</a></li>")
		fmt.Fprintf(w, "<li><a href=\"/viewer/mach\">"+
			"Machine Diagram</a></li>")
		fmt.Fprintf(w, "<li><a href=\"/viewer/state\">"+
			"State Diagram</a></li>")
		fmt.Fprintf(w, "<li><a href=\"/viewer/steps\">"+
			"Step Diagram</a></li>")
	}
	fmt.Fprintf(w, `<li><a href="/traces">Traces View</a></li>`)
	if p.UiLivepage {
		fmt.Fprintf(w, `<li><a href="/livepage">Livepage UI</a></li>`)
	}

	if p.UiMcp {
		fmt.Fprintf(w, `<li>MCP: http://%s/mcp</li>`, httpAddr)
	}
	fmt.Fprintf(w, `</ul>`)

	fmt.Fprintf(w, "<h2>--dir %s</h2><hr><ul>", p.OutputDir)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		fmt.Fprintf(w, `<li><a href="%s">%s</a></li>`, name, name)
	}

	fmt.Fprintf(w, "</ul><hr></body></html>")
}

// newDiagHandler creates a new func serving HTML and SVGs of diagrams.
func newDiagHandler(
	mach *am.Machine, diagType types.DiagramType,
) func(w http.ResponseWriter, r *http.Request) {
	//

	return func(w http.ResponseWriter, r *http.Request) {
		done := make(chan struct{})

		middleware(w)

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// TODO WebDiagReq
		mach.Add1(ss.WebReqDiag, Pass(&A{
			DiagType:           diagType,
			Uri:                r.RequestURI,
			HttpRequest:        r,
			HttpResponseWriter: w,
			DoneChan:           done,
			Addr:               r.RemoteAddr,
		}))
		// TODO timeout
		<-done
	}
}

// newWsDiagHandler creates a new func handling diagram updates over WS.
func newWsDiagHandler(
	mach *am.Machine, diagType types.DiagramType,
) func(w http.ResponseWriter, r *http.Request) {
	//

	return func(w http.ResponseWriter, r *http.Request) {
		middleware(w)

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			mach.AddErrState(ss.ErrWeb, err, nil)
			return
		}
		defer conn.Close(websocket.StatusInternalError, "internal error")
		// TODO remove?
		done := make(chan struct{})
		mach.Add1(ss.WebSocketDiag, Pass(&A{
			DiagType:           diagType,
			WebSocketConn:      conn,
			HttpRequest:        r,
			HttpResponseWriter: w,
			DoneChan:           done,
			Addr:               r.RemoteAddr,
		}))
		// TODO timeout
		// TODO r.Context()?
		<-done
	}
}

func middleware(w http.ResponseWriter) {
	// TODO add security
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods",
		"POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers",
		"Accept, Content-Type, Content-Length, Accept-Encoding, "+
			"X-CSRF-Token, Authorization")

	// cache
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// wsDbgHandler handles dbg protocol over WS.
func wsDbgHandler(
	w http.ResponseWriter, r *http.Request, mach *am.Machine, fwdTo []*fwdConn,
) {

	connWs, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	conn := websocket.NetConn(mach.Context(), connWs, websocket.MessageBinary)
	AcceptConn(conn, mach, fwdTo)
}
