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
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/pancsta/asyncmachine-go/internal/utils"
	"github.com/soheilhy/cmux"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

type RPCServer struct {
	Mach   *am.Machine
	ConnID string
	FwdTo  []*rpc.Client
}

type DbgMsgSchemaFwd struct {
	MsgStruct *telemetry.DbgMsgStruct
	ConnId    string
}

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

func StartRpc(
	mach *am.Machine, addr string, mux chan<- cmux.CMux, fwdAdds []string,
) {
	var err error
	gob.Register(am.Relation(0))
	if addr == "" {
		addr = telemetry.DbgAddr
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Println(err)
		mach.AddErr(err, nil)
		// TODO nice err msg
		panic(err)
	}
	mach.Log("dbg server started at %s", addr)

	// conenct to other instances
	fwdTo := make([]*rpc.Client, len(fwdAdds))
	for i, a := range fwdAdds {
		fwdTo[i], err = rpc.Dial("tcp", a)
		if err != nil {
			fmt.Printf("Cant fwd to %s: %s\n", a, err)
			os.Exit(1)
		}
	}

	// cmux
	m := cmux.New(lis)
	// push out for IoC
	if mux != nil {
		mux <- m
	}

	httpL1 := m.Match(cmux.HTTP1Fast())
	httpL2 := m.Match(cmux.HTTP1())
	tcpL := m.Match(cmux.Any())
	httpS := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			if r.RequestURI == "/diagrams/mach.ws" {
				wsHandler(w, r, mach)
				return
			}
			httpHandler(w, r, mach)
		})}

	// listen
	go func() {
		err := httpS.Serve(httpL1)
		// TODO ErrNetwork
		mach.AddErr(err, nil)
	}()
	go func() {
		err := httpS.Serve(httpL2)
		// TODO ErrNetwork
		mach.AddErr(err, nil)
	}()
	go tcpAccept(tcpL, mach, fwdTo)

	// start cmux
	if err := m.Serve(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
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

// ///// REMOTE MACHINE RPC

// TODO remove
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

func Decode(s string) GetField {
	return GetField(s[0])
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
