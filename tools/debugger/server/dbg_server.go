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
	"github.com/pancsta/asyncmachine-go/internal/utils"
	"github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
	"github.com/soheilhy/cmux"
)

// ///// ///// /////

// ///// CLIENT

// ///// ///// /////

type Exportable struct {
	// TODO refac to MsgSchema
	// TODO version schemas
	MsgStruct *dbg.DbgMsgStruct
	MsgTxs    []*dbg.DbgMsgTx
}

type Client struct {
	// bits which get saved into the go file
	*Exportable

	// current transition, 1-based, mirrors the slider (eg 1 means tx.ID == 0)
	// TODO atomic
	// current step, 1-based, mirrors the slider
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

func (c *Client) LastTxTill(hTime time.Time) int {
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

func (c *Client) TxByMachTime(sum uint64) int {
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

	if len(schema.GroupsOrder) == 0 {
		return
	}

	// schema groups
	pastSelf := false
	prev := am.S{}
	for _, g := range schema.GroupsOrder {
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

func StartRpc(
	mach *am.Machine, addr string, mux chan<- cmux.CMux, p types.Params,
) {
	// TODO dont listen in WASM

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
		log.Println(err)
		mach.AddErr(err, nil)
		fmt.Println(err)
		// TODO ret err
		os.Exit(1)
	}
	mach.Log("dbg server started at %s", addr)

	// connect to other instances
	fwdTo := make([]*rpc.Client, len(p.FwdData))
	for i, a := range p.FwdData {
		fwdTo[i], err = rpc.Dial("tcp", a)
		if err != nil {
			fmt.Printf("Cant fwd to %s: %s\n", a, err)
			os.Exit(1)
		}
	}

	// first cmux for tcp
	mux1 := cmux.New(lis)
	tcpL := mux1.Match(cmux.Any())
	go tcpAccept(tcpL, mach, fwdTo)

	// HTTP and WS on port+1
	if p.UiWeb {
		httpAddr := fmt.Sprintf("%s:%d",
			addr[:strings.LastIndex(addr, ":")], port+1)
		httpMux := http.ServeMux{}
		httpMux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
			httpHandlerIndex(w, r, mach, p)
		})
		httpMux.Handle("/", http.FileServer(http.Dir(p.OutputDir)))
		httpMux.HandleFunc("/diagrams/mach", func(
			w http.ResponseWriter, r *http.Request,
		) {

			httpHandlerDiagMach(w, r, mach)
		})
		httpMux.HandleFunc("/diagrams/mach.svg", func(
			w http.ResponseWriter, r *http.Request,
		) {

			httpHandlerDiagMach(w, r, mach)
		})
		httpMux.HandleFunc("/diagrams/mach.ws",
			func(w http.ResponseWriter, r *http.Request) {
				wsDiagHandler(w, r, mach)
			})
		httpMux.HandleFunc("/dbg.ws", func(w http.ResponseWriter, r *http.Request) {
			wsDbgHandler(w, r, mach, fwdTo)
		})
		httpSrv := &http.Server{
			Handler: &httpMux,
			Addr:    httpAddr,
		}

		// listen
		go func() {
			mach.AddErr(httpSrv.ListenAndServe(), nil)
		}()
	}

	// push out for IoC
	if mux != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			mux <- mux1
		}()
	}

	// start first cmux
	if err := mux1.Serve(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// TODO rename to RPCServer (compat break)
type RPCServer struct {
	Mach *am.Machine
	// TODO ConnId
	ConnID string
	FwdTo  []*rpc.Client
}

type DbgMsgSchemaFwd struct {
	MsgStruct *dbg.DbgMsgStruct
	ConnId    string
}

// TODO rename to RemoteMsgSchemaReceive (compat break)
func (r *RPCServer) DbgMsgSchemaFwd(
	msg *DbgMsgSchemaFwd, _ *string,
) error {

	r.Mach.Add1(ss.ConnectEvent, am.A{
		// TODO typed args
		"msg_struct": msg.MsgStruct,
		"conn_id":    msg.ConnId,
		"Client.id":  msg.MsgStruct.ID,
	})

	return nil
}

func (r *RPCServer) DbgMsgSchema(
	msgSchema *dbg.DbgMsgStruct, _ *string,
) error {

	r.Mach.Add1(ss.ConnectEvent, am.A{
		// TODO typed args
		"msg_struct": msgSchema,
		"conn_id":    r.ConnID,
		"Client.id":  msgSchema.ID,
	})

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

			r.Mach.Add1(ss.ClientMsg, am.A{
				"msgs_tx":  queue,
				"conn_ids": queueConnId,
			})
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

			r.Mach.Add1(ss.ClientMsg, am.A{
				"msgs_tx":  queue,
				"conn_ids": queueConnId,
			})
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

func tcpAccept(l net.Listener, mach *am.Machine, fwdTo []*rpc.Client) {
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
	conn io.ReadWriteCloser, mach *am.Machine, fwdTo []*rpc.Client,
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
		os.Exit(1)
	}
	server.ServeConn(conn)

	// TODO pass to range fwdTo (dedicated RPC method)
	rcvr.Mach.Add1(ss.DisconnectEvent, am.A{"conn_id": connId})
}

// ///// ///// /////

// ///// WEB

// ///// ///// /////

// httpHandlerIndex lists --dir
func httpHandlerIndex(
	w http.ResponseWriter, r *http.Request, mach *am.Machine, p types.Params,
) {
	middleware(w)

	// Read directory contents
	entries, err := os.ReadDir(p.OutputDir)
	if err != nil {
		mach.AddErr(err, nil)
		http.Error(w, "Failed to read directory", http.StatusInternalServerError)
		return
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
	// TODO read from state and hide
	fmt.Fprintf(w, `<li><a href="/diagrams/mach">/diagrams/mach</a></li>`)
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

// httpHandlerDiagMach serve the diagram website.
func httpHandlerDiagMach(
	w http.ResponseWriter, r *http.Request, mach *am.Machine,
) {
	done := make(chan struct{})

	middleware(w)

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// TODO WebDiagReq
	mach.Add1(ss.WebReq, am.A{
		// TODO typed params
		"uri":                 r.RequestURI,
		"*http.Request":       r,
		"http.ResponseWriter": w,
		"doneChan":            done,
		"addr":                r.RemoteAddr,
	})
	// TODO timeout
	<-done
}

// wsDiagHandler handles diagram updates.
func wsDiagHandler(w http.ResponseWriter, r *http.Request, mach *am.Machine) {
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
	mach.Add1(ss.WebSocketDiag, am.A{
		// TODO typed params
		"*websocket.Conn":     conn,
		"*http.Request":       r,
		"http.ResponseWriter": w,
		"doneChan":            done,
		"addr":                r.RemoteAddr,
	})
	// TODO timeout
	// TODO r.Context()?
	<-done
}

func middleware(w http.ResponseWriter) {
	// TODO add security
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "localhost")
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
	w http.ResponseWriter, r *http.Request, mach *am.Machine, fwdTo []*rpc.Client,
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
