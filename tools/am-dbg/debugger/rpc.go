package debugger

import (
	"encoding/gob"
	"net"
	"net/rpc"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/am-dbg/states"
	"log"
)

type RPCServer struct {
	Mach *am.Machine
	URL  string
}

func (r *RPCServer) MsgStruct(msgStruct *telemetry.MsgStruct, _ *string) error {
	r.Mach.Add(am.S{ss.ClientMsg}, am.A{"msg_struct": msgStruct})
	return nil
}

func (r *RPCServer) MsgTx(msgTx *telemetry.MsgTx, _ *string) error {
	r.Mach.Add(am.S{ss.ClientMsg}, am.A{"msg_tx": msgTx})
	return nil
}

func StartRCP(rcvr *RPCServer) {
	var err error
	gob.Register(am.Relation(0))
	url := telemetry.RpcHost
	if rcvr.URL != "" {
		url = rcvr.URL
	}
	l, err := net.Listen("tcp", url)
	if err != nil {
		rcvr.Mach.AddErr(err)
		panic(err)
	}
	log.Println("RPC server started at", url)
	server := rpc.NewServer()
	err = server.Register(rcvr)
	if err != nil {
		rcvr.Mach.AddErr(err)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			rcvr.Mach.AddErr(err)
			continue
		}
		// only 1 client
		if rcvr.Mach.Is(am.S{ss.ClientConnected}) {
			continue
		}
		rcvr.Mach.Add(am.S{ss.ClientConnected}, nil)
		go func() {
			server.ServeConn(conn)
			rcvr.Mach.Remove(am.S{ss.ClientConnected}, nil)
		}()
	}
}
