//go:build js && wasm

package main

import (
	"context"
	"strings"
	"sync"
	"syscall/js"

	"honnef.co/go/js/dom/v2"

	example "github.com/pancsta/asyncmachine-go/examples/wasm_livepage"
	"github.com/pancsta/asyncmachine-go/examples/wasm_livepage/deps"
	"github.com/pancsta/asyncmachine-go/examples/wasm_livepage/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
	lpwasm "github.com/pancsta/livepage/wasm"
)

var ssD = states.DemoStates

// log is rendered into the server-provided #rpc-log node and re-applied after
// every livepage morph.
var (
	logMu    sync.Mutex
	logLines []string
	morphFn  js.Func
)

func main() {
	ctx := context.Background()

	// the server morphs the DOM, so re-render the wasm log afterwards
	morphFn = js.FuncOf(func(js.Value, []js.Value) any {
		renderLog()
		return nil
	})
	js.Global().Call("addEventListener", "livepage:morph", morphFn)

	logLine("wasm: booting")

	if !lpwasm.Available() {
		deps.Error("livepage JS client is not loaded")
		return
	}
	mux, err := lpwasm.Dial(ctx)
	if err != nil {
		deps.Error(err)
		return
	}

	// handlers machine, synced to the remote demo machine
	h := &Handlers{}
	handlerMach, err := am.NewCommon(ctx, "browser-handlers",
		states.DemoSchema, ssD.Names(), nil, nil, nil)
	if err != nil {
		deps.Error(err)
		return
	}
	if _, err := handlerMach.HandlersBindMaps(nil, map[string]am.HandlerFinal{
		"StartState": h.StartState,
		"IncrState":  h.IncrState,
		"MsgState":   h.MsgState,
	}); err != nil {
		deps.Error(err)
		return
	}
	amhelp.MachDebugEnv(handlerMach)

	// stream 1: the server is the aRPC client, the browser accepts
	srv, err := arpc.NewServer(ctx, "", "browser-handlers",
		handlerMach, &arpc.ServerOpts{Parent: handlerMach})
	if err != nil {
		deps.Error(err)
		return
	}
	connServer := mux.Open(1)
	srv.Conn = connServer
	srv.Start(nil)

	// stream 0: the browser is the aRPC client
	client, err := arpc.NewClient(ctx, "", "browser-livepage",
		states.DemoSchema, &arpc.ClientOpts{Parent: handlerMach})
	if err != nil {
		deps.Error(err)
		return
	}
	connClient := mux.Open(0)
	client.Conn.Store(&connClient)
	client.Start(nil)

	<-client.Mach.When1(ssrpc.ClientStates.Ready, nil)
	logLine("wasm: aRPC connected")

	if _, err := ampipe.BindAny(client.NetMach, handlerMach); err != nil {
		deps.Error(err)
		return
	}
	handlerMach.Set(client.NetMach.ActiveStates(nil), nil)

	// wasm -> server over stream 0, with args
	client.NetMach.Add1(ssD.Msg, am.Pass(example.NewMsg("hello from wasm")))
	// wasm -> server over stream 0, state only
	client.NetMach.Add1(ssD.Incr, nil)

	select {}
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

type Handlers struct {
	*am.ExceptionHandler
}

var _ = ssD.Start

func (h *Handlers) StartState(_ *am.Event) {
	logLine("wasm: remote machine ready")
}

var _ = ssD.Incr

func (h *Handlers) IncrState(_ *am.Event) {
	logLine("wasm: remote incr ack")
}

var _ = ssD.Msg

func (h *Handlers) MsgState(e *am.Event) {
	// server -> wasm over aRPC
	msg := am.ParseArgs[example.AMsg](e.Args).Msg
	if msg == "" {
		// clock-only sync without args
		return
	}
	logLine("wasm received " + msg)
}

// ///// ///// /////

// ///// DOM

// ///// ///// /////

func logLine(line string) {
	deps.Logf("[wasm] %s", line)

	logMu.Lock()
	logLines = append(logLines, line)
	logMu.Unlock()

	renderLog()
}

// renderLog renders the wasm log into the server-provided container. The
// container is wiped by livepage morphs, so it's re-rendered on
// "livepage:morph". Log lines also go to the browser console via [deps].
func renderLog() {
	logMu.Lock()
	text := strings.Join(logLines, "\n")
	logMu.Unlock()

	el := dom.GetWindow().Document().QuerySelector("#rpc-log")
	if el == nil {
		return
	}
	el.SetTextContent(text)
}
