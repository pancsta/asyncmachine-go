// See wasm+js files at:
// https://github.com/pancsta/asyncmachine-go/tree/main/examples/wasm/client

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	example "github.com/pancsta/asyncmachine-go/examples/wasm"
	"github.com/pancsta/asyncmachine-go/examples/wasm/states"
	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	amhistb "github.com/pancsta/asyncmachine-go/pkg/history/badger"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

var ssF = states.FooStates
var ssB = states.BarStates

func initMachines(
	ctx context.Context, barHandlers any, fooHandlers any,
) (*am.Machine, *arpc.Client, *am.Machine, error) {

	//

	// Browser machine (local)

	//

	sessId := utils.RandId(3)
	barMach, err := am.NewCommon(ctx, "browser-bar-"+sessId, states.BarSchema, ssB.Names(), barHandlers, nil, nil)
	if err != nil {
		log.Fatal(err.Error())
	}
	barMach.SemLogger().SetArgsMapper(amhelp.LogArgsMapper)
	amhelp.MachDebugEnv(barMach)
	repl, err := arpc.MachReplWs(barMach, example.EnvRelayHttpAddr, &arpc.ReplOpts{
		// TODO should be automatic in WASM
		WebSocketTunnel: arpc.WsListenPath("repl-"+barMach.Id(), example.EnvBarReplAddr),
		Args:            example.ArgsRpc,
	})
	if err == nil {
		repl.Start(nil)
	}

	// RPC Server

	srv, err := arpc.NewServer(ctx, example.EnvRelayHttpAddr, barMach.Id(), barMach, &arpc.ServerOpts{
		// eg localhost:8080/listen/bar/localhost:7070 opens 7070 for "bar"
		// TODO should be automatic in WASM
		WebSocketTunnel: arpc.WsListenPath(barMach.Id(), example.EnvBarTcpAddr),
		Parent:          barMach,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
	// auto reconnects
	srv.WsTunReconn = true
	// quick pushes
	interval := time.Millisecond
	srv.PushInterval.Store(&interval)
	// start
	srv.Start(nil)

	// HISTORY

	// injected err handler
	onErr := func(err error) {
		log.Print("err: ", err.Error())
	}
	// backend and base configs
	cfg := amhistb.Config{
		BaseConfig: amhist.Config{
			MaxRecords:    10 ^ 6,
			TrackedStates: ssB.Names(),
		},
		EncJson: true,
	}
	db, err := amhistb.NewDb("")
	if err != nil {
		return nil, nil, nil, err
	}
	barMach.OnDispose(func(id string, ctx context.Context) {
		db.Close()
	})
	mem, err := amhistb.NewMemory(ctx, db, barMach, cfg, onErr)
	if err != nil {
		return nil, nil, nil, err
	}

	// mutate and query
	barMach.Add1(ssB.Heartbeat, nil)
	now := time.Now().UTC()
	err = mem.Sync()
	time.Sleep(100 * time.Millisecond)
	if err != nil {
		return nil, nil, nil, err
	}
	ok := mem.ActivatedBetween(ctx, ssB.Heartbeat, now.Add(-time.Second), now)
	fmt.Printf("Badger history: %v\n", ok)

	//

	// Server machine (remote)

	//

	// RPC Handlers Machine

	fooHandlerMach, err := am.NewCommon(ctx, "browser-foo-"+sessId, states.FooSchema, ssF.Names(), fooHandlers, barMach, &am.Opts{
		Tags: []string{"rpc-handler"},
	})
	if err != nil {
		log.Fatal(err.Error())
	}
	amhelp.MachDebugEnv(fooHandlerMach)
	fooHandlerMach.SemLogger().SetArgsMapper(amhelp.LogArgsMapper)

	// RPC Client (Net Machine)

	// TODO enable
	foo, err := arpc.NewClient(ctx, example.EnvRelayHttpAddr, fooHandlerMach.Id(), states.FooSchema, &arpc.ClientOpts{
		Parent: fooHandlerMach,
		// TODO should be the default for WASM
		WebSocket: arpc.WsDialPath(fooHandlerMach.Id(), example.EnvFooTcpAddr),
	})
	if err != nil {
		log.Fatal(err.Error())
	}

	// wait for RPC
	foo.Start(nil)
	<-foo.Mach.When1(ssrpc.ClientStates.Ready, nil)

	// bind and sync the handler mach to net mach
	if _, err := ampipe.BindAny(foo.NetMach, fooHandlerMach); err != nil {
		log.Fatal(err.Error())
	}
	fooHandlerMach.Set(foo.NetMach.ActiveStates(nil), nil)

	// start and wait
	barMach.Add1(ssB.Start, nil)

	return barMach, foo, fooHandlerMach, nil
}
