package main

import (
	"context"
	"fmt"
	"net"
	_ "net/http/pprof"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/joho/godotenv"

	example "github.com/pancsta/asyncmachine-go/examples/wasm"
	"github.com/pancsta/asyncmachine-go/examples/wasm/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	amrelay "github.com/pancsta/asyncmachine-go/tools/relay"
	amrelayt "github.com/pancsta/asyncmachine-go/tools/relay/types"
)

var ssF = states.FooStates
var ssB = states.BarStates
var PassRpc = example.PassRpc

type ARpc = example.ARpc

func init() {
	_ = godotenv.Load()
}

func main() {
	ctx := context.Background()

	// pprof debug
	// go func() {
	// 	http.ListenAndServe("localhost:6060", nil)
	// }()

	// net source machine

	handlers := &HandlersFoo{
		lastMsg: time.Now(),
	}
	fooMach, err := am.NewCommon(ctx, "server-foo", states.FooSchema, ssF.Names(), handlers, nil, nil)
	if err != nil {
		panic(err)
	}
	amhelp.MachDebugEnv(fooMach)
	fooMach.SemLogger().SetArgsMapper(example.LogArgs)
	handlers.machFoo = fooMach
	arpc.MachRepl(fooMach, example.EnvFooReplAddr, &arpc.ReplOpts{
		AddrDir:  example.EnvReplDir,
		Args:     ARpc{},
		ParseRpc: example.ParseRpc,
	})
	fooMach.Add1(ssF.Start, nil)

	// RPC Muxer

	mux, err := arpc.NewMux(ctx, example.EnvFooTcpAddr, "server-foo", fooMach, &arpc.MuxOpts{
		Parent: fooMach,
	})
	if err != nil {
		panic(err)
	}
	mux.Start(nil)
	handlers.mux = mux

	// websocket relay

	newClient := func(ctx context.Context, id string, conn net.Conn) (*arpc.Client, error) {
		// RPC Client (Net Machine)
		bar, err := arpc.NewClient(ctx, "", "server-bar", states.BarSchema, &arpc.ClientOpts{
			Parent: fooMach,
		})
		if err != nil {
			panic(err)
		}

		// inject the connection, store and start TODO atomic
		bar.Conn.Store(&conn)
		handlers.rpcBar.Store(bar)
		bar.Start(nil)
		go func() {
			<-bar.Mach.When1(ssrpc.ClientStates.Ready, nil)
			fmt.Printf("aRPC connected via WebSocket\n")
		}()

		return bar, nil
	}
	relay, err := amrelay.New(ctx, amrelayt.Args{
		Name:   "wasm-demo",
		Debug:  true,
		Parent: fooMach,
		Wasm: &amrelayt.ArgsWasm{
			ListenAddr:  example.EnvRelayHttpAddr,
			StaticDir:   "./client",
			ReplAddrDir: example.EnvReplDir,
			TunnelMatchers: []amrelayt.TunnelMatcher{{
				Id:        regexp.MustCompile("^browser-bar-"),
				NewClient: newClient,
			}},
			DialMatchers: []amrelayt.DialMatcher{{
				Id: regexp.MustCompile("^browser-foo-"),
				NewServer: func(ctx context.Context, id string, conn net.Conn) (*arpc.Server, error) {
					// TODO ctx to Event
					return mux.NewServer(nil, id, conn)
				},
			}},
		},
	})
	if err != nil {
		panic(err)
	}
	relay.Start(nil)

	// wait
	select {}
}

type HandlersFoo struct {
	*am.ExceptionHandler

	machFoo *am.Machine
	// last connected browser machine
	rpcBar atomic.Pointer[arpc.Client]
	mux    *arpc.Mux

	lastMsg   time.Time
	lastHello time.Time
}

func (h *HandlersFoo) StartState(e *am.Event) {
	ctx := h.machFoo.NewStateCtx(ssF.Start)

	h.machFoo.Fork(ctx, e, func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				// bored? 15sec after the last msg
				if time.Since(h.lastMsg) > 15*time.Second && h.machFoo.Not1(ssF.Bored) {
					// periodic sync without args
					h.machFoo.Add1(ssF.Bored, nil)
				}

				// hello? try 10 times, only if browser-bar connected
				bar := h.rpcBar.Load()
				if bar != nil && time.Since(h.lastHello) > 10*time.Second &&
					bar.Mach.Is1(ssam.BasicStates.Ready) &&
					bar.NetMach.Tick(ssB.Msg) < 20 {

					// instant sync with args
					args := PassRpc(&ARpc{
						Msg: "hello",
					})
					bar.NetMach.EvAdd1(e, ssF.Msg, args)
					h.machFoo.EvAdd1(e, ssF.Msg, args)
					h.lastHello = time.Now()
				}
			}
		}
	})
}

func (h *HandlersFoo) MsgEnter(e *am.Event) bool {
	return example.ParseArgs(e.Args).Msg != ""
}

func (h *HandlersFoo) MsgState(e *am.Event) {
	h.machFoo.Remove1(ssF.Msg, nil)
	args := example.ParseArgs(e.Args)

	// both foo and bar mutate this state
	author := "bar"
	if e.Mutation().Source != nil {
		author = e.Mutation().Source.MachId
	}

	msg := fmt.Sprintf("[%s] %s\n", author, args.Msg)
	fmt.Print(msg)
	h.lastMsg = time.Now()
}

func (h *HandlersFoo) BoredState(e *am.Event) {
	fmt.Println("foo is bored...")
}
