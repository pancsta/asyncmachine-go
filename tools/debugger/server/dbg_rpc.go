package server

import (
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
	"sync"
	"time"

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

func (r *RPCServer) DbgMsgStruct(
	msgStruct *telemetry.DbgMsgStruct, _ *string,
) error {
	r.Mach.Add1(ss.ConnectEvent, am.A{
		// TODO typed args
		"msg_struct": msgStruct,
		"conn_id":    r.ConnID,
		"Client.id":  msgStruct.ID,
	})

	// fwd to other instances
	for _, fwd := range r.FwdTo {
		err := fwd.Call("RPCServer.DbgMsgStruct", msgStruct, nil)
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
	// TODO remove
	msgTx.Time = &now
	queue = append(queue, msgTx)
	queueConnId = append(queueConnId, r.ConnID)

	// fwd to other instances
	for _, fwd := range r.FwdTo {
		err := fwd.Call("RPCServer.DbgMsgTx", msgTx, nil)
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

	rpcL := m.Match(cmux.Any())
	go rpcAccept(rpcL, mach, fwdTo)

	// start cmux
	if err := m.Serve(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func rpcAccept(l net.Listener, mach *am.Machine, fwdTo []*rpc.Client) {
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
			connID := utils.RandId(8)
			rcvr := &RPCServer{
				Mach:   mach,
				ConnID: connID,
				FwdTo:  fwdTo,
			}

			err = server.Register(rcvr)
			if err != nil {
				log.Println(err)
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
