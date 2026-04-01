package main

import (
	"context"
	"fmt"
	"net"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	example "github.com/pancsta/asyncmachine-go/examples/wasm_workflow"
	"github.com/pancsta/asyncmachine-go/examples/wasm_workflow/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
	amrelay "github.com/pancsta/asyncmachine-go/tools/relay"
	amrelayt "github.com/pancsta/asyncmachine-go/tools/relay/types"
)

var ssO = states.OrchestratorStates
var ssD = states.DispatcherStates
var ssW = states.WorkerStates
var PassRpc = example.PassRpc
var Pass = example.Pass
var ParseArgs = example.ParseArgs

type ARpc = example.ARpc
type A = example.A

func init() {
	// pprof debug
	// go func() {
	// 	http.ListenAndServe("localhost:6060", nil)
	// }()
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// net source machine

	orch := &Orchestrator{workers: make(map[string]*arpc.Client)}
	mach, err := am.NewCommon(ctx, "orchestrator", states.OrchestratorSchema, ssO.Names(), orch, nil, nil)
	if err != nil {
		panic(err)
	}
	amhelp.MachDebugEnv(mach)
	mach.SemLogger().SetArgsMapper(example.LogArgs)
	mach.SetGroups(states.OrchestratorGroups, ssO)
	orch.mach = mach
	// open telemetry traces
	err = amtele.MachBindOtelEnv(mach)
	if err != nil {
		mach.AddErr(err, nil)
	}
	err = arpc.MachRepl(mach, example.EnvOrchestratorReplAddr, &arpc.ReplOpts{
		AddrDir:  example.EnvReplDir,
		Args:     ARpc{},
		ParseRpc: example.ParseRpc,
	})
	if err != nil {
		return
	}
	mach.Add1(ssO.Start, nil)
	mach.OnDispose(func(id string, ctx context.Context) {
		// fmt.Println("OnDispose")
		var chans []<-chan struct{}
		for _, client := range orch.workers {
			if client.NetMach == nil {
				continue
			}
			chans = append(chans, client.NetMach.When1(ssW.Disposed, nil))
			client.NetMach.Add1(ssW.Disposing, nil)
		}
		// let it sync
		amhelp.WaitForAll(mach.Context(), time.Second, chans...)
	})

	// RPC Muxer (not needed)

	// mux, err := arpc.NewMux(ctx, example.EnvOrchestratorTcpAddr, mach.Id(), mach, &arpc.MuxOpts{
	// 	Parent: mach,
	// })
	// if err != nil {
	// 	panic(err)
	// }
	// mux.Start(nil)
	// orch.mux = mux

	// websocket relay

	relay, err := amrelay.New(mach.Context(), amrelayt.Args{
		Name:   "wasm-workflow",
		Debug:  true,
		Parent: mach,
		Wasm: &amrelayt.ArgsWasm{
			ListenAddr:  example.EnvRelayHttpAddr,
			StaticDir:   "./web",
			ReplAddrDir: example.EnvReplDir,
			TunnelMatchers: []amrelayt.TunnelMatcher{{
				Id:        regexp.MustCompile("^browser\\d$"),
				NewClient: orch.newBrowserClient,
			}},
			// dialer (not needed)
			// DialMatchers: []amrelayt.DialMatcher{{
			// 	Id: regexp.MustCompile("^browser-foo-"),
			// 	NewServer: func(ctx context.Context, id string, conn net.Conn) (*arpc.Server, error) {
			// 		// TODO ctx to Event
			// 		return mux.NewServer(nil, id, conn)
			// 	},
			// }},
		},
	})
	if err != nil {
		panic(err)
	}
	relay.Start(nil)

	// wait for signal
	select {
	case <-ctx.Done():
		// or self-exit
	case <-mach.Context().Done():
	}
	// gracefully on signal...
	<-mach.Context().Done()
	fmt.Println("Shutting down...")
}

type Orchestrator struct {
	*am.ExceptionHandler

	mach    *am.Machine
	workers map[string]*arpc.Client
	mux     *arpc.Mux

	lastMsg   time.Time
	lastHello time.Time
	payload1  []byte
	payload2  []byte
	payload3  []byte
	payload4  []byte
	lastStart am.Time
}

func (o *Orchestrator) newBrowserClient(ctx context.Context, id string, conn net.Conn) (*arpc.Client, error) {
	// RPC Client (Net Machine)
	var client *arpc.Client
	var err error
	switch id {
	case "browser2":
		client, err = arpc.NewClient(ctx, "", "srv-"+id, states.DispatcherSchema, &arpc.ClientOpts{
			Parent:   o.mach,
			Consumer: o.mach,
		})
	case "browser1":
		client, err = arpc.NewClient(ctx, "", "srv-"+id, states.WorkerSchema, &arpc.ClientOpts{
			Parent:   o.mach,
			Consumer: o.mach,
		})
	default:
		return nil, fmt.Errorf("unknown client ID %s", id)
	}
	if err != nil {
		panic(err)
	}
	// inject the connection, store and start
	client.Conn.Store(&conn)
	o.workers[id] = client
	<-client.Mach.WhenQueue(client.Start(nil))
	prefix := am.Capitalize(id)

	// pipe from RPC client
	err = ampipe.BindMany(client.Mach, o.mach, am.S{
		ssrpc.ClientStates.Exception,
		ssrpc.ClientStates.Ready,
	}, am.S{
		ssO.ErrRpc,
		prefix + "Conn",
	})
	if err != nil {
		return nil, err
	}

	// pipe from NetMach
	pipeSrc := am.S{
		ssW.Exception,
		ssW.Ready,
		ssW.Completed,
		ssW.Working,
		ssW.Failed,
	}
	pipeDest := am.S{
		am.PrefixErr + prefix,
		prefix + ssW.Ready,
		prefix + ssW.Completed,
		prefix + ssW.Working,
		prefix + ssW.Failed,
	}

	// dispatcher states TODO shorten
	if id == "browser2" {
		pipeSrc = append(pipeSrc,
			"Browser3"+ssW.Exception,
			"Browser3"+ssW.Ready,
			"Browser3"+ssW.Completed,
			"Browser3"+ssW.Working,
			"Browser3"+ssW.Failed,

			"Browser4"+ssW.Exception,
			"Browser4"+ssW.Ready,
			"Browser4"+ssW.Completed,
			"Browser4"+ssW.Working,
			"Browser4"+ssW.Failed,
		)
		pipeDest = append(pipeDest,
			am.PrefixErr+"Browser3",
			"Browser3"+ssW.Ready,
			"Browser3"+ssW.Completed,
			"Browser3"+ssW.Working,
			"Browser3"+ssW.Failed,

			am.PrefixErr+"Browser4",
			"Browser4"+ssW.Ready,
			"Browser4"+ssW.Completed,
			"Browser4"+ssW.Working,
			"Browser4"+ssW.Failed,
		)
	}

	ampipe.Sync(client.NetMach, o.mach, pipeSrc, pipeDest)

	// TODO add already active states (from the piped ones)

	// bind pipes
	err = ampipe.BindMany(client.NetMach, o.mach, pipeSrc, pipeDest)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (o *Orchestrator) StartState(e *am.Event) {
	// become ready after 3 sec
	o.mach.EvAdd1(e, ssO.Ready, nil)
}

func (o *Orchestrator) StartWorkState(e *am.Event) {
	// unset self
	o.mach.EvRemove1(e, ssO.StartWork, nil)
	// trigger initial nodes
	o.mach.EvAdd(e, am.S{ssO.Browser1Work, ssO.Browser2Work}, nil)
	// memorize when this round started (for no-data retries)
	// o.lastStart = o.mach.Time(am.S{ssO.Browser1Work, ssO.Browser2Work, ssO.Browser3Work, ssO.Browser4Work})
}

func (o *Orchestrator) Browser1WorkState(e *am.Event) {
	ctx := o.mach.NewStateCtx(ssO.NetworkReady)
	args := ParseArgs(e.Args)
	o.mach.EvRemove1(e, ssO.Browser1Work, nil)
	o.mach.Fork(ctx, e, func() {
		// dont send data when retrying
		if args.Retry {
			o.workers["browser1"].NetMach.EvAdd1(e, ssW.Retrying, nil)
			return
		}

		// send initial payload
		o.workers["browser1"].NetMach.EvAdd1(e, ssW.Working, PassRpc(&A{
			// seed payload for browser1, results in o.payload1 when completed
			Payload: []byte("wasm-workflow-1"),
		}))
	})
}

func (o *Orchestrator) Browser2WorkState(e *am.Event) {
	ctx := o.mach.NewStateCtx(ssO.NetworkReady)
	args := ParseArgs(e.Args)
	o.mach.EvRemove1(e, ssO.Browser2Work, nil)
	o.mach.Fork(ctx, e, func() {
		// dont send data when retrying
		if args.Retry {
			o.workers["browser2"].NetMach.EvAdd1(e, ssW.Retrying, nil)
			return
		}

		// send initial payload
		o.workers["browser2"].NetMach.EvAdd1(e, ssW.Working, PassRpc(&A{
			// seed payload for browser1, results in o.payload1 when completed
			Payload: []byte("wasm-workflow-2"),
		}))
	})
}

func (o *Orchestrator) Browser3WorkState(e *am.Event) {
	ctx := o.mach.NewStateCtx(ssO.NetworkReady)
	args := ParseArgs(e.Args)
	o.mach.EvRemove1(e, ssO.Browser3Work, nil)
	o.mach.Fork(ctx, e, func() {
		// dont send data when retrying
		if args.Retry {
			o.workers["browser2"].NetMach.EvAdd1(e, ssD.Browser3Retry, nil)
			return
		}

		// send initial payload, proxy via browser2
		o.workers["browser2"].NetMach.EvAdd1(e, ssD.Browser3Work, PassRpc(&A{
			// received payload for browser3 from browser1, results in o.payload3 when completed
			Payload: o.payload1,
		}))
	})
}

func (o *Orchestrator) Browser4WorkState(e *am.Event) {
	ctx := o.mach.NewStateCtx(ssO.NetworkReady)
	args := ParseArgs(e.Args)
	o.mach.EvRemove1(e, ssO.Browser4Work, nil)
	o.mach.Fork(ctx, e, func() {
		// dont send data when retrying
		if args.Retry {
			o.workers["browser2"].NetMach.EvAdd1(e, ssD.Browser4Retry, nil)
			return
		}

		// send initial payload, proxy via browser2
		// proxy via browser2
		o.workers["browser2"].NetMach.EvAdd1(e, ssD.Browser4Work, PassRpc(&A{
			// received payload for browser4 from browser2, results in o.payload4 when completed
			Payload: o.payload2,
		}))
	})
}

func (o *Orchestrator) ServerPayloadEnter(e *am.Event) bool {
	payload := arpc.ParseArgs(e.Args).Payload
	_, ok := payload.Data.([]byte)
	return ok
}

func (o *Orchestrator) ServerPayloadState(e *am.Event) {
	// split Otel trace
	// o.mach.EvRemove1(e, ssO.ServerPayload, nil)
	payload := arpc.ParseArgs(e.Args).Payload
	// dump.Println(payload)
	retry := Pass(&A{Retry: true})
	switch payload.Source {
	case "browser1":
		o.payload1 = payload.Data.([]byte)
		// failure?
		if len(o.payload1) == 0 {
			o.mach.EvAdd1(e, ssO.Browser1Work, retry)
		} else {
			o.mach.EvAdd1(e, ssO.Browser1Delivered, nil)
		}
	case "browser2":
		o.payload2 = payload.Data.([]byte)
		// failure?
		if len(o.payload2) == 0 {
			o.mach.EvAdd1(e, ssO.Browser2Work, retry)
		} else {
			o.mach.EvAdd1(e, ssO.Browser2Delivered, nil)
		}
	case "browser3":
		o.payload3 = payload.Data.([]byte)
		// failure?
		if len(o.payload3) == 0 {
			o.mach.EvAdd1(e, ssO.Browser3Work, retry)
		} else {
			o.mach.EvAdd1(e, ssO.Browser3Delivered, nil)
		}
	case "browser4":
		o.payload4 = payload.Data.([]byte)
		// failure?
		if len(o.payload4) == 0 {
			o.mach.EvAdd1(e, ssO.Browser4Work, retry)
		} else {
			o.mach.EvAdd1(e, ssO.Browser4Delivered, nil)
		}
	}
}

func (o *Orchestrator) WorkDeliveredState(e *am.Event) {
	// dump.Println(fmt.Sprintf("%x", o.payload3))
	// dump.Println(fmt.Sprintf("%x", o.payload4))
	expected3 := "798084a3599272353ed165b1ef2717344a89246ca3756680059b7b1340053007b7af049916f67388bea5cdc007dbf7368522d808bd5a6ad68b57612b5fc804b2"
	expected4 := "7f8db768c3ff3bd492c68f48006f6e5d77981501df604eb77b9577e87acd5ee86772cb00b6f621ce9209d1e74c770d95e2b78d658c0e0f399989bb98421f6e95"
	hex3 := fmt.Sprintf("%x", o.payload3)
	hex4 := fmt.Sprintf("%x", o.payload4)

	// corrupted, restart
	if expected3 != hex3 || expected4 != hex4 {
		o.mach.EvAdd1(e, ssO.StartWork, nil)
		return
	}

	// OK
	if example.EnvExitOnCompleted {
		o.mach.Log("completed, exiting...")
		go amhelp.Dispose(o.mach)
	}
}

func (o *Orchestrator) ErrRpcExit(e *am.Event) bool {
	// TODO check all RPCs
	return true
}

func (o *Orchestrator) Browser1ConnState(e *am.Event) {
	// fwd to multi
	o.mach.EvAdd1(e, ssO.BrowserConn, Pass(&A{
		Id: e.Mutation().Source.MachId,
	}))
}

func (o *Orchestrator) Browser2ConnState(e *am.Event) {
	// fwd to multi
	o.mach.EvAdd1(e, ssO.BrowserConn, Pass(&A{
		Id: e.Mutation().Source.MachId,
	}))
}

func (o *Orchestrator) BrowserConnEnter(e *am.Event) bool {
	return ParseArgs(e.Args).Id != ""
}

func (o *Orchestrator) BrowserConnState(e *am.Event) {
	id := ParseArgs(e.Args).Id
	machId := strings.TrimPrefix(id, "rc-srv-")
	o.mach.Log("Connected to %s", machId)

	o.mach.Fork(o.mach.NewStateCtx(ssO.Start), e, func() {
		// TODO via a request for redos
		o.workers[machId].NetMach.EvAdd1(e, ssW.Start, PassRpc(&A{
			Boot: &example.Boot{
				TraceId: os.Getenv(amtele.EnvOtelTraceId),
				SpanId:  os.Getenv(amtele.EnvOtelSpanId),
			},
		}))
	})
}
