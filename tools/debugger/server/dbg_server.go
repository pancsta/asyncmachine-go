package server

// TODO rewrite to a better protocol with
//  - everything from the reader
//  - graphs

import (
	"encoding/gob"
	"fmt"
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
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

// ///// ///// /////

// ///// CLIENT

// ///// ///// /////

type Exportable struct {
	// TODO refac to MsgSchema
	// TODO version schemas
	MsgStruct *telemetry.DbgMsgStruct
	MsgTxs    []*telemetry.DbgMsgTx
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

func (c *Client) Tx(idx int) *telemetry.DbgMsgTx {
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
			// merge with prev groups
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
	mach *am.Machine, addr string, mux chan<- cmux.CMux, fwdAdds []string,
	enableHttp bool,
) {
	// TODO dont listen in WASM

	var err error
	gob.Register(am.Relation(0))
	gob.Register(Exportable{})
	if addr == "" {
		addr = telemetry.DbgAddr
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

	// connect to another instances
	fwdTo := make([]*rpc.Client, len(fwdAdds))
	for i, a := range fwdAdds {
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

	// second cmux for http/ws on port+1
	if enableHttp {
		httpAddr := fmt.Sprintf("%s:%d",
			addr[:strings.LastIndex(addr, ":")], port+1)
		httpLis, err := net.Listen("tcp", httpAddr)
		if err != nil {
			log.Println(err)
			mach.AddErr(err, nil)
			panic(err)
		}
		mux2 := cmux.New(httpLis)
		httpL := mux2.Match(cmux.HTTP1())
		httpS := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

				if r.RequestURI == "/diagrams/mach.ws" {
					wsHandler(w, r, mach)
					return
				}
				httpHandler(w, r, mach)
			})}

		go func() {
			mach.AddErr(httpS.Serve(httpL), nil)
		}()
		go func() {
			if err := mux2.Serve(); err != nil {
				fmt.Println(err)
				mach.AddErr(err, nil)
				os.Exit(1)
			}
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

// TODO rename to RpcServer (compat break)
type RPCServer struct {
	Mach   *am.Machine
	ConnID string
	FwdTo  []*rpc.Client
}

type DbgMsgSchemaFwd struct {
	MsgStruct *telemetry.DbgMsgStruct
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
	msgSchema *telemetry.DbgMsgStruct, _ *string,
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
	queue       []*telemetry.DbgMsgTx
	queueConnId []string
	queueMx     sync.Mutex
	scheduled   bool
)

func (r *RPCServer) DbgMsgTx(msgTx *telemetry.DbgMsgTx, _ *string) error {
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
	MsgTx  *telemetry.DbgMsgTx
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
		go func() {
			server := rpc.NewServer()
			connID := utils.RandId(8)
			rcvr := &RPCServer{
				Mach:   mach,
				ConnID: connID,
				FwdTo:  fwdTo,
			}

			err = server.Register(rcvr)
			if err != nil {
				rcvr.Mach.AddErr(err, nil)
				// TODO err msg
				fmt.Println(err)
				os.Exit(1)
			}
			server.ServeConn(conn)

			// TODO pass to range fwdTo (dedicated RPC method)
			rcvr.Mach.Add1(ss.DisconnectEvent, am.A{"conn_id": connID})
		}()
	}
}

// ///// ///// /////

// ///// WEB

// ///// ///// /////

// httpHandler serves plain HTTP requests.
func httpHandler(w http.ResponseWriter, r *http.Request, mach *am.Machine) {
	done := make(chan struct{})

	middleware(w)

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

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

// wsHandler handles WebSocket upgrade requests and echoes messages.
func wsHandler(w http.ResponseWriter, r *http.Request, mach *am.Machine) {
	middleware(w)

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		mach.AddErrState(ss.ErrWeb, err, nil)
		return
	}
	defer c.Close(websocket.StatusInternalError, "internal error")
	done := make(chan struct{})
	mach.Add1(ss.WebSocket, am.A{
		// TODO typed params
		"*websocket.Conn":     c,
		"*http.Request":       r,
		"http.ResponseWriter": w,
		"doneChan":            done,
		"addr":                r.RemoteAddr,
	})
	// TODO timeout
	<-done
}

func middleware(w http.ResponseWriter) {
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

// ///// ///// /////

// ///// REMOTE MACHINE RPC

// TODO remove (used in dbg integration tests)
// ///// ///// /////

type GetField int

const (
	GetCursorTx GetField = iota + 1
	GetCursorStep
	GetClientCount
	GetMsgCount
	GetOpts
	GetSelectedState
)

func Decode(s string) GetField {
	return GetField(s[0])
}

func (n GetField) String() string {
	switch n {
	case GetCursorTx:
		return "GetCursorTx"
	case GetCursorStep:
		return "GetCursorStep"
	case GetClientCount:
		return "GetClientCount"
	case GetMsgCount:
		return "GetMsgCount"
	case GetOpts:
		return "GetOpts"
	case GetSelectedState:
		return "GetSelectedState"
	}

	return "!UNKNOWN!"
}

func (n GetField) Encode() string {
	return string(rune(n))
}
