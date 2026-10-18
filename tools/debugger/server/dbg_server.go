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
	fwdBackoffMin = 500 * time.Millisecond
	fwdBackoffMax = 30 * time.Second
)

// fwdConn is a lazily- and re-dialed RPC connection to a forwarding target.
// It survives target downtime (both at startup and mid-stream) by redialing
// in the background with an exponential backoff, instead of failing the
// whole dbg server or permanently giving up on a forward target.
type fwdConn struct {
	addr string
	mach *am.Machine

	mx       sync.Mutex
	cli      *rpc.Client
	backoff  time.Duration
	nextDial time.Time
}

// newFwdConn creates a fwdConn and kicks off the initial (non-blocking)
// dial.
func newFwdConn(mach *am.Machine, addr string) *fwdConn {
	f := &fwdConn{addr: addr, mach: mach, backoff: fwdBackoffMin}
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

// dial (re)connects, replays known client schemas as the first messages on
// the fresh connection, and resets the backoff on success. Safe to call
// concurrently.
func (f *fwdConn) dial() {
	f.mx.Lock()
	defer f.mx.Unlock()

	if f.cli != nil || time.Now().Before(f.nextDial) {
		return
	}

	cli, err := rpc.Dial("tcp", f.addr)
	if err != nil {
		f.retry(err)
		return
	}

	// replay known client schemas first, so a (re)connected target learns
	// about clients it missed while this fwd link was down
	// TODO collect last N msgs and push after the schema msg, memorize last msg
	//  ID sent per client
	if err := pushClientSchemas(cli); err != nil {
		_ = cli.Close()
		f.retry(fmt.Errorf("schema replay: %w", err))
		return
	}

	f.mach.Log("dbg fwd to %s connected", f.addr)
	f.cli = cli
	f.backoff = fwdBackoffMin
}

// Call forwards an RPC call over the current connection. If disconnected, or
// the call fails, it triggers a reconnect with a backoff and returns an
// error, leaving the caller to drop or retry the message.
func (f *fwdConn) Call(serviceMethod string, args, reply any) error {
	f.mx.Lock()
	cli := f.cli
	f.mx.Unlock()

	if cli == nil {
		go f.dial()
		return fmt.Errorf("dbg fwd to %s: not connected", f.addr)
	}

	err := cli.Call(serviceMethod, args, reply)
	if err != nil {
		f.mx.Lock()
		if f.cli == cli {
			_ = cli.Close()
			f.cli = nil
		}
		f.mx.Unlock()
		go f.dial()
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
	fwdTo := make([]*fwdConn, len(p.FwdData))
	for i, a := range p.FwdData {
		fwdTo[i] = newFwdConn(mach, a)
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
}

type DbgMsgSchemaFwd struct {
	MsgStruct *dbg.DbgMsgStruct
	ConnId    string
}

var (
	// clientSchemas holds the latest known schema of each currently
	// connected client (direct or forwarded), keyed by its origin ConnId.
	// Replayed to fwd targets on (re)connect, see [pushClientSchemas].
	clientSchemas   = make(map[string]*DbgMsgSchemaFwd)
	clientSchemasMx sync.Mutex
)

// pushClientSchemas replays the schema of every currently connected client
// over cli, as the first messages on a freshly (re)connected fwd link, so
// the target learns about clients it missed while disconnected.
func pushClientSchemas(cli *rpc.Client) error {
	clientSchemasMx.Lock()
	msgs := make([]*DbgMsgSchemaFwd, 0, len(clientSchemas))
	for _, msg := range clientSchemas {
		msgs = append(msgs, msg)
	}
	clientSchemasMx.Unlock()

	for _, msg := range msgs {
		if err := cli.Call("RPCServer.DbgMsgSchemaFwd", msg, nil); err != nil {
			return err
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

	clientSchemasMx.Lock()
	clientSchemas[msg.ConnId] = msg
	clientSchemasMx.Unlock()

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

	clientSchemasMx.Lock()
	clientSchemas[r.ConnID] = &DbgMsgSchemaFwd{
		MsgStruct: msgSchema,
		ConnId:    r.ConnID,
	}
	clientSchemasMx.Unlock()

	// fwd to other instances
	for _, host := range r.FwdTo {
		fwdMsg := DbgMsgSchemaFwd{
			MsgStruct: msgSchema,
			ConnId:    r.ConnID,
		}
		// TODO const name
		err := host.Call("RPCServer.DbgMsgSchemaFwd", &fwdMsg, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

var (
	queue       []*dbg.DbgMsgTx
	queueConnId []string
	queueMx     sync.Mutex
	scheduled   bool
)

func (r *RPCServer) DbgMsgTx(msgTx *dbg.DbgMsgTx, _ *string) error {
	queueMx.Lock()
	defer queueMx.Unlock()

	if !scheduled {
		scheduled = true
		go func() {
			// debounce
			time.Sleep(time.Second)

			queueMx.Lock()
			defer queueMx.Unlock()

			r.Mach.Add1(ss.ClientMsg, Pass(&A{
				MsgsTx:  queue,
				ConnIds: queueConnId,
			}))
			// DEBUG
			// println("sent", len(queue), "msgs")
			queue = nil
			queueConnId = nil
			scheduled = false
		}()
	}

	now := time.Now()
	msgTx.Time = &now
	queue = append(queue, msgTx)
	queueConnId = append(queueConnId, r.ConnID)

	// fwd to other instances
	for _, host := range r.FwdTo {
		fwdMsg := DbgMsgTx{
			MsgTx:  msgTx,
			ConnId: r.ConnID,
		}
		// TODO const name
		err := host.Call("RPCServer.DbgMsgTxFwd", fwdMsg, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

type DbgMsgTx struct {
	MsgTx  *dbg.DbgMsgTx
	ConnId string
}

// TODO rename to RemoteMsgTxReceive (compat break)
func (r *RPCServer) DbgMsgTxFwd(msg *DbgMsgTx, _ *string) error {
	queueMx.Lock()
	defer queueMx.Unlock()

	msgTx := msg.MsgTx
	connId := msg.ConnId

	if !scheduled {
		scheduled = true
		go func() {
			// debounce
			time.Sleep(time.Second)

			queueMx.Lock()
			defer queueMx.Unlock()

			r.Mach.Add1(ss.ClientMsg, Pass(&A{
				MsgsTx:  queue,
				ConnIds: queueConnId,
			}))
			// DEBUG
			// println("sent", len(queue), "msgs")
			queue = nil
			queueConnId = nil
			scheduled = false
		}()
	}

	now := time.Now()
	msgTx.Time = &now
	queue = append(queue, msgTx)
	queueConnId = append(queueConnId, connId)

	// fwd to other instances
	for _, host := range r.FwdTo {
		fwdMsg := DbgMsgTx{
			MsgTx:  msgTx,
			ConnId: connId,
		}
		err := host.Call("RPCServer.DbgMsgTxFwd", &fwdMsg, nil)
		if err != nil {
			return err
		}
	}

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

	err := server.Register(rcvr)
	if err != nil {
		rcvr.Mach.AddErr(err, nil)
		// TODO err msg
		fmt.Println(err)
		return
	}
	server.ServeConn(conn)

	clientSchemasMx.Lock()
	delete(clientSchemas, connId)
	clientSchemasMx.Unlock()

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
			listenHost, err := utils.GetGlobalUnicastIP()
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
