package server

import (
	"encoding/gob"
	"log"
	"net"
	"net/rpc"
	"sync"
	"time"

	"github.com/soheilhy/cmux"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

type RPCServer struct {
	Mach   *am.Machine
	ConnID string
}

func (r *RPCServer) DbgMsgStruct(
	msgStruct *telemetry.DbgMsgStruct, _ *string,
) error {
	r.Mach.Add1(ss.ConnectEvent, am.A{
		"msg_struct": msgStruct,
		"conn_id":    r.ConnID,
	})

	return nil
}

var (
	queue     []*telemetry.DbgMsgTx
	queueMx   sync.Mutex
	scheduled bool
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

			r.Mach.Add1(ss.ClientMsg, am.A{"msgs_tx": queue})
			// DEBUG
			// println("sent", len(queue), "msgs")
			queue = nil
			scheduled = false
		}()
	}

	now := time.Now()
	msgTx.Time = &now
	queue = append(queue, msgTx)

	return nil
}

func StartRpc(mach *am.Machine, addr string, mux chan<- cmux.CMux) {
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
	mach.Log("Telemetry server started at %s", addr)
	// Create a new cmux instance.
	m := cmux.New(lis)
	// push out for IoC
	if mux != nil {
		mux <- m
	}

	// Match connections in order: first rpc, then everything else.
	rpcL := m.Match(cmux.Any())
	go rpcAccept(rpcL, mach)

	// Start cmux serving.
	if err := m.Serve(); err != nil {
		panic(err)
	}
}

func rpcAccept(l net.Listener, mach *am.Machine) {
	// TODO restart on error
	defer mach.PanicToErr(nil)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println(err)
			mach.AddErr(err, nil)
			continue
		}

		// handle the client
		go func() {
			server := rpc.NewServer()
			connID := conn.RemoteAddr().String()
			rcvr := &RPCServer{
				Mach:   mach,
				ConnID: connID,
			}
			err = server.Register(rcvr)
			if err != nil {
				log.Println(err)
				rcvr.Mach.AddErr(err, nil)
				// TODO err msg
				panic(err)
			}
			server.ServeConn(conn)
			rcvr.Mach.Add1(ss.DisconnectEvent, am.A{"conn_id": connID})
		}()
	}
}

// ///// ///// /////

// ///// REMOTE MACHINE RPC

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
