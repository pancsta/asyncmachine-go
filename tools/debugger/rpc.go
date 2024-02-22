package main

import (
	"encoding/gob"
	"net"
	"net/rpc"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

type RPCServer struct {
	mach *am.Machine
	url  string
}

func (r *RPCServer) MsgStruct(msgStruct *telemetry.MsgStruct, _ *string) error {
	r.mach.Add(am.S{ss.ClientMsg}, am.A{"msg_struct": msgStruct})
	return nil
}

func (r *RPCServer) MsgTx(msgTx *telemetry.MsgTx, _ *string) error {
	r.mach.Add(am.S{ss.ClientMsg}, am.A{"msg_tx": msgTx})
	return nil
}

func startRCP(rcvr *RPCServer) {
	var err error
	gob.Register(am.Relation(0))
	url := telemetry.RpcHost
	if rcvr.url != "" {
		url = rcvr.url
	}
	l, err := net.Listen("tcp", url)
	if err != nil {
		rcvr.mach.AddErr(err)
		panic(err)
	}
	server := rpc.NewServer()
	err = server.Register(rcvr)
	if err != nil {
		rcvr.mach.AddErr(err)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			rcvr.mach.AddErr(err)
			continue
		}
		// only 1 client
		if rcvr.mach.Is(am.S{ss.ClientConnected}) {
			continue
		}
		rcvr.mach.Add(am.S{ss.ClientConnected}, nil)
		go func() {
			server.ServeConn(conn)
			rcvr.mach.Remove(am.S{ss.ClientConnected}, nil)
		}()
	}
}
