//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"syscall/js"
	"time"

	"github.com/gookit/goutil/dump"
	example "github.com/pancsta/asyncmachine-go/examples/wasm_workflow"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

// ConnEvent is a connection event between WebWorkers.
type ConnEvent struct {
	Port js.Value
	Id   js.Value
}

func main() {
	ctx := context.Background()
	dump.Println(os.Args)

	switch os.Args[1] {
	case "ui":
		mainUiThread(ctx)
	case "worker":
		mainWebWorker(ctx)
	}

	// wait
	select {}
}

// mainUiThread is the main executed for the main UI thread. It created a WORKER instance, as the main thread
// does not dispatch (proxy) any traffic.
func mainUiThread(ctx context.Context) {

	handlers := NewWorker()
	mach, srv, err := newWorkerMach(ctx, "browser1", handlers, nil)
	if err != nil {
		dump.Println(err)
		return
	}

	handlers.mach = mach
	handlers.srv = srv

	// init the dispatcher
	defer forkWorker("browser2").Close()

	<-ctx.Done()
}

// mainWebWorker is the main executed for each WebWorker. I can create either a WORKER instance (for leaf threads)
// or a DISPATCHER instance, which faces the network and fwd traffic to the leaf threads.
func mainWebWorker(ctx context.Context) {
	// Channel to signal when we have the connection
	ready := make(chan *ConnEvent)

	// set up the port
	var setupFunc js.Func
	setupFunc = js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		// dump.Println(args)
		event := args[0]
		data := event.Get("data")

		if data.Get("type").String() == "CONNECT" {
			// We received the port!
			port := data.Get("port")
			id := data.Get("id")
			ready <- &ConnEvent{
				Port: port,
				Id:   id,
			}
			// dispose this listener
			setupFunc.Release()
		}
		return nil
	})
	js.Global().Set("onmessage", setupFunc)
	js.Global().Call("postMessage", map[string]interface{}{"type": "READY"})
	connEvent := <-ready
	fmt.Printf("WORKER: Port received (ID: %s), upgrading to net.Conn\n", connEvent.Id.String())

	// connect
	conn := example.NewPortConn(connEvent.Port)
	defer conn.Close()
	id := connEvent.Id.String()

	switch id {

	// dispatcher mach
	case "browser2":
		ports, err := newDispatcher(ctx, conn)
		if err != nil {
			dump.Println(err)
			return
		}
		defer func() {
			for _, port := range ports {
				port.Close()
			}
		}()

	// worker mach
	default:
		handlers := NewWorker()
		mach, srv, err := newWorkerMach(ctx, id, handlers, conn)
		if err != nil {
			dump.Println(err)
			return
		}

		handlers.mach = mach
		handlers.srv = srv
	}

	<-ctx.Done()
}

func newDispatcher(ctx context.Context, conn *example.PortConn) ([]net.Conn, error) {

	// init leaf workers
	conns := []net.Conn{
		forkWorker("browser3"),
		forkWorker("browser4"),
	}

	// init the dispatcher and inject ports
	handlers := NewDispatcher()
	mach, srv, clients, err := newDispatcherMach(ctx, handlers, map[string]net.Conn{
		"browser3": conns[0],
		"browser4": conns[1],
	})
	if err != nil {
		return nil, err
	}

	handlers.mach = mach
	handlers.srv = srv
	handlers.clients = clients

	return conns, nil
}

func forkWorker(id string) net.Conn {
	// init
	workerObj := js.Global().Get("Worker").New("worker.js")
	msgChannel := js.Global().Get("MessageChannel").New()
	port1 := msgChannel.Get("port1")
	port2 := msgChannel.Get("port2")
	// dump.Println(port1, port2)

	// wait for READY
	workerReady := make(chan struct{})
	var workerOnMsg js.Func
	workerOnMsg = js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		event := args[0]
		if event.Get("data").Get("type").String() == "READY" {
			workerOnMsg.Release()
			close(workerReady)
		}
		return nil
	})
	workerObj.Set("onmessage", workerOnMsg)
	<-workerReady

	// transfer port2
	transferList := js.Global().Get("Array").New()
	transferList.Call("push", port2)
	msg := js.Global().Get("Object").New()
	msg.Set("type", "CONNECT")
	msg.Set("port", port2)
	msg.Set("id", id)
	workerObj.Call("postMessage", msg, transferList)

	// return net.Conn
	return example.NewPortConn(port1)
}

// ///// ///// /////

// ///// WORKER HANDLERS

// ///// ///// /////

// Worker is either:
// - leaf worker, running inside a WebWorker (talking to the Orchestrator via the Dispatcher)
// - main thread (talking to the Orchestrator directly over WebSocket)
type Worker struct {
	*ssam.DisposedHandlers
	*am.ExceptionHandler

	mach    *am.Machine
	srv     *arpc.Server
	payload []byte
}

func NewWorker() *Worker {
	return &Worker{
		DisposedHandlers: &ssam.DisposedHandlers{},
	}
}

func (h *Worker) StartEnter(e *am.Event) bool {
	// dump.Println(e.Args)
	args := example.ParseArgs(e.Args)
	return args.Boot != nil
}

func (h *Worker) StartState(e *am.Event) {
	ctx := h.mach.NewStateCtx(ssW.Start)
	args := example.ParseArgs(e.Args)

	// set the root span
	os.Setenv(amtele.EnvOtelTraceId, args.Boot.TraceId)
	os.Setenv(amtele.EnvOtelSpanId, args.Boot.SpanId)

	// start Otel
	h.mach.Fork(ctx, e, func() {
		h.mach.Go(ctx, func() {
			err := amtele.MachBindOtelEnv(h.mach)
			if err != nil {
				h.mach.AddErr(err, nil)
			}
		})

		// become ready 3sec after starting
		h.mach.EvAdd1(e, ssW.Ready, nil)
	})
}

func (h *Worker) StartEnd(e *am.Event) {
	// unbind otel tracer
	for _, t := range h.mach.Tracers() {
		if otel, ok := t.(*amtele.OtelMachTracer); ok {
			err := h.mach.DetachTracer(otel)
			if err != nil {
				h.mach.AddErr(err, nil)
			}
		}
	}
}

func (h *Worker) WorkingEnter(e *am.Event) bool {
	return len(example.ParseArgs(e.Args).Payload) > 0
}

func (h *Worker) WorkingState(e *am.Event) {
	ctx := h.mach.NewStateCtx(ssW.Working)
	payload := example.ParseArgs(e.Args).Payload
	h.payload = payload

	h.mach.Fork(ctx, e, h.doWork(e, payload))
}

func (h *Worker) RetryingEnter(e *am.Event) bool {
	return h.payload != nil
}

func (h *Worker) RetryingState(e *am.Event) {
	ctx := h.mach.NewStateCtx(ssW.Retrying)
	h.mach.Fork(ctx, e, h.doWork(e, h.payload))
}

func (h *Worker) CompletedEnter(e *am.Event) bool {
	return len(example.ParseArgs(e.Args).Payload) > 0
}

func (h *Worker) CompletedState(e *am.Event) {
	ctx := h.mach.NewStateCtx(ssW.Completed)
	payload := example.ParseArgs(e.Args).Payload

	h.mach.Fork(ctx, e, func() {
		h.mach.Log("sending the results back...")
		// send it back via untyped server payload
		err := h.srv.SendPayload(ctx, e, &arpc.MsgSrvPayload{
			Name: "result",
			Data: payload,
		})
		h.mach.EvAddErr(e, err, nil)
	})
}

func (h *Worker) FailedState(e *am.Event) {
	ctx := h.mach.NewStateCtx(ssW.Failed)

	h.mach.Fork(ctx, e, func() {
		h.mach.Log("sending empty results back...")
		// send it back via untyped server payload
		err := h.srv.SendPayload(ctx, e, &arpc.MsgSrvPayload{
			Name: "result",
			Data: []byte{},
		})
		h.mach.EvAddErr(e, err, nil)
	})
}

// methods

func (h *Worker) doWork(e *am.Event, payload []byte) func() {
	return func() {
		start := time.Now()
		// fake delay
		example.SimulateLoad(example.EnvHashIterations)
		// deterministic result
		hash := example.ComputeHash(payload, []byte(h.mach.Id()))
		result := hash[:]

		// random errs
		switch rand.Intn(example.EnvSuccessRate) {

		// maybe corruption?
		case 0:
			h.mach.Log("completed work in %dms (CORRUPTED)", time.Since(start).Milliseconds())
			result = []byte("corrupted")

		// maybe failure?
		case 1:
			h.mach.EvAdd1(e, ssW.Failed, nil)
			return

		default:
			h.mach.Log("completed work in %dms", time.Since(start).Milliseconds())
		}

		// next
		h.mach.EvAdd1(e, ssW.Completed, Pass(&A{
			Payload: result,
		}))

	}
}

// ///// ///// /////

// ///// DISPATCHER HANDLERS

// ///// ///// /////

// Dispatcher is running inside a WebWorker (talking to the Orchestrator directly over WebSocket)
type Dispatcher struct {
	*Worker
	clients []*arpc.Client

	// self-unsetting multi handlers
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		Worker: NewWorker(),
	}
}

func (h *Dispatcher) ServerPayloadState(e *am.Event) {
	h.mach.EvRemove1(e, ssD.ServerPayload, nil)
	// fwd payloads from leaf workers
	ctx := h.mach.NewStateCtx(ssW.Ready)
	// dump.Println(e.Args)
	h.srv.SendPayload(ctx, nil, arpc.ParseArgs(e.Args).Payload)
}

func (h *Dispatcher) DisposingState(e *am.Event) {
	for _, client := range h.clients {
		client.NetMach.Add1(ssam.DisposedStates.Disposing, nil)
	}
	// let it sync
	time.Sleep(100 * time.Millisecond)
	// call super
	h.DisposedHandlers.DisposingState(e)
}

// readability handlers

func (h *Dispatcher) Browser3WorkState(e *am.Event) {
	h.mach.EvRemove1(e, ssD.Browser3Work, nil)
}

func (h *Dispatcher) Browser4WorkState(e *am.Event) {
	h.mach.EvRemove1(e, ssD.Browser4Work, nil)
}

func (h *Dispatcher) Browser3RetryState(e *am.Event) {
	h.mach.EvRemove1(e, ssD.Browser3Retry, nil)
}

func (h *Dispatcher) Browser4RetryState(e *am.Event) {
	h.mach.EvRemove1(e, ssD.Browser4Retry, nil)
}
