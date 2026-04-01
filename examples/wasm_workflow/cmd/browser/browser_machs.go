// See wasm+js files at:
// https://github.com/pancsta/asyncmachine-go/tree/main/examples/wasm/client

package main

import (
	"context"
	"log"
	"net"
	"os"
	"time"

	example "github.com/pancsta/asyncmachine-go/examples/wasm_workflow"
	"github.com/pancsta/asyncmachine-go/examples/wasm_workflow/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

var ssW = states.WorkerStates
var ssD = states.DispatcherStates
var Pass = example.Pass
var PassRpc = example.PassRpc
var ParseArgs = example.ParseArgs

type A = example.A
type ARpc = example.ARpc

func newDispatcherMach(
	ctx context.Context, handlers any, conns map[string]net.Conn,
) (*am.Machine, *arpc.Server, []*arpc.Client, error) {
	//

	id := "browser2"
	mach, err := am.NewCommon(ctx, id, states.DispatcherSchema, ssD.Names(), handlers, nil, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	mach.SemLogger().SetArgsMapper(example.LogArgs)
	mach.SetGroups(states.DispatcherGroups, ssD)
	amhelp.MachDebugEnv(mach)
	repl, err := arpc.MachReplWs(mach, example.EnvRelayHttpAddr, &arpc.ReplOpts{
		WebSocketTunnel: arpc.WsListenPath("repl-"+mach.Id(), example.EnvBrowser2ReplAddr),
		Args:            ARpc{},
		ParseRpc:        example.ParseRpc,
	})
	if err == nil {
		repl.Start(nil)
	}
	os.Setenv(amtele.EnvService, id)

	// RPC Server (network-facing)

	srv, err := arpc.NewServer(mach.Context(), example.EnvRelayHttpAddr, mach.Id(), mach, &arpc.ServerOpts{
		// eg localhost:8080/listen/bar/localhost:7070 opens 7070 for "bar"
		// TODO should be automatic in WASM
		WebSocketTunnel: arpc.WsListenPath(mach.Id(), example.EnvBrowser2TcpAddr),
		Parent:          mach,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
	// pipes
	err = ampipe.BindReady(srv.Mach, mach, ssW.RpcReady, "")
	if err != nil {
		return nil, nil, nil, err
	}
	// quick pushes
	interval := time.Millisecond
	srv.PushInterval.Store(&interval)
	// start
	srv.Start(nil)

	var clients []*arpc.Client
	// RPC Clients (WebWorker-facing)
	for leafId, conn := range conns {
		client, err := arpc.NewClient(mach.Context(), "", "bro-"+leafId, states.WorkerSchema, &arpc.ClientOpts{
			Parent:   mach,
			Consumer: mach,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		// inject the WebWorker port conn
		client.Conn.Store(&conn)
		// TODO save client in handlers
		// wait for NetMach init
		client.Mach.WhenQueue(
			client.Start(nil))

		// pipe everything into the Dispatcher (directly)
		err = ampipe.BindMany(client.NetMach, mach, ssW.Names(),
			am.StatesPrefix(am.Capitalize(leafId), ssW.Names()))
		if err != nil {
			return nil, nil, nil, err
		}

		// pipe out Start, Work, and Retry to leaf workers
		pipeSrc := am.S{ssD.Start}
		pipeDest := am.S{ssD.Start, ssW.Working, ssW.Retrying}
		switch leafId {
		case "browser3":
			pipeSrc = append(pipeSrc, ssD.Browser3Work, ssD.Browser3Retry)
		case "browser4":
			pipeSrc = append(pipeSrc, ssD.Browser4Work, ssD.Browser4Retry)
		}
		err = ampipe.BindMany(mach, client.NetMach, pipeSrc, pipeDest)
		if err != nil {
			return nil, nil, nil, err
		}

		clients = append(clients, client)
	}

	return mach, srv, clients, nil
}

// newWorkerMach
//
// conn: injected connection struct build from WebWorker MessageChannel. It prevents the RPC server
// to tunnel TCP over WS.
func newWorkerMach(
	ctx context.Context, id string, handlers any, conn *example.PortConn,
) (*am.Machine, *arpc.Server, error) {
	//

	var addrRpc string
	var addrRepl string
	// TODO read via a string convention
	switch id {
	case "browser1":
		addrRpc = example.EnvBrowser1TcpAddr
		addrRepl = example.EnvBrowser1ReplAddr
	case "browser3":
		addrRepl = example.EnvBrowser3ReplAddr
	case "browser4":
		addrRepl = example.EnvBrowser4ReplAddr
	}
	os.Setenv(amtele.EnvService, id)

	mach, err := am.NewCommon(ctx, id, states.WorkerSchema, ssW.Names(), handlers, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	mach.SemLogger().SetArgsMapper(example.LogArgs)
	amhelp.MachDebugEnv(mach)
	repl, err := arpc.MachReplWs(mach, example.EnvRelayHttpAddr, &arpc.ReplOpts{
		WebSocketTunnel: arpc.WsListenPath("repl-"+mach.Id(), addrRepl),
		Args:            ARpc{},
		ParseRpc:        example.ParseRpc,
	})
	if err != nil {
		return nil, nil, err
	}
	repl.Start(nil)

	// RPC Server (only the UI thread)

	opts := &arpc.ServerOpts{
		// eg localhost:8080/listen/bar/localhost:7070 opens 7070 for "bar"
		WebSocketTunnel: arpc.WsListenPath(mach.Id(), addrRpc),
		Parent:          mach,
	}
	if conn != nil {
		opts.WebSocketTunnel = ""
	}
	srv, err := arpc.NewServer(mach.Context(), example.EnvRelayHttpAddr, mach.Id(), mach, opts)
	if err != nil {
		return nil, nil, err
	}
	if conn != nil {
		srv.Conn = conn
	}
	// pipes
	err = ampipe.BindReady(srv.Mach, mach, ssW.RpcReady, "")
	if err != nil {
		return nil, nil, err
	}
	// quick pushes
	interval := time.Millisecond
	srv.PushInterval.Store(&interval)
	// start
	srv.Start(nil)

	return mach, srv, nil
}
