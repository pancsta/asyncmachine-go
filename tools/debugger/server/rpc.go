// TODO move to debugger/server
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
			time.Sleep(time.Second)

			queueMx.Lock()
			defer queueMx.Unlock()

			r.Mach.Add1(ss.ClientMsg, am.A{"msgs_tx": queue})
			queue = nil
			scheduled = false
		}()
	}

	now := time.Now()
	msgTx.Time = &now
	queue = append(queue, msgTx)
	return nil
}

func StartRCP(mach *am.Machine, url string) {
	var err error
	gob.Register(am.Relation(0))
	if url == "" {
		url = telemetry.DbgHost
	}
	lis, err := net.Listen("tcp", url)
	if err != nil {
		log.Println(err)
		mach.AddErr(err)
		// TODO nice err msg
		panic(err)
	}
	log.Println("RPC server started at", url)
	// Create a new cmux instance.
	m := cmux.New(lis)

	// Match connections in order: first rpc, then everything else.
	rpcL := m.Match(cmux.Any())
	go rpcAccept(rpcL, mach)

	// Start cmux serving.
	if err := m.Serve(); err != nil {
		panic(err)
	}
}

func rpcAccept(l net.Listener, mach *am.Machine) {
	// TODO test if this fixes cmux
	defer func() {
		if r := recover(); r != nil {
			log.Println("recovered:", r)
		}
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println(err)
			mach.AddErr(err)
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
				rcvr.Mach.AddErr(err)
				// TODO err msg
				panic(err)
			}
			server.ServeConn(conn)
			rcvr.Mach.Add1(ss.DisconnectEvent, am.A{"conn_id": connID})
		}()
	}
}
