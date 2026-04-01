//go:build !wasm

package main

import (
	"context"
	"os/signal"
	"syscall"
	"testing"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

func TestWasmClient(t *testing.T) {
	if amhelp.IsTestRunner() {
		t.Skip("skipping WASM mock test")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// schemas

	_, _, _, err := newDispatcherMach(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = newWorkerMach(ctx, t.Name(), NewWorker(), nil)
	if err != nil {
		t.Fatal(err)
	}
}

type Worker struct {
	*ssam.DisposedHandlers
	*am.ExceptionHandler

	mach *am.Machine
	srv  *arpc.Server
}

func NewWorker() *Worker {
	return &Worker{
		DisposedHandlers: &ssam.DisposedHandlers{},
	}
}

// //
//
// // Server machine handlers
//
// //
//
// type FooHandlersMock struct {
// 	t           *testing.T
// 	rpcFoo      *arpc.Client
// 	machBar     *am.Machine
// 	machFooHand *am.Machine
// }
//
// func (h *FooHandlersMock) BoredState(e *am.Event) {
// 	h.t.Log("foo is bored...")
// }
//
// //
//
// // Browser machine handlers
//
// //
//
// type BarHandlersMock struct {
// 	t           *testing.T
// 	rpcFoo      *arpc.Client
// 	machBar     *am.Machine
// 	machFooHand *am.Machine
// }
//
// func (h *BarHandlersMock) StartState(e *am.Event) {
// 	h.t.Log("[bar] StartState")
//
// 	go func() {
// 		time.Sleep(3 * time.Second)
// 		h.machBar.EvAdd1(e, ssB.SubmitMsg, nil)
// 	}()
// }
//
// func (h *BarHandlersMock) SubmitMsgState(e *am.Event) {
// 	txt := "mock msg"
// 	args := PassRpc(&ARpc{
// 		Msg: txt,
// 	})
//
// 	// push to self
// 	h.machBar.EvAdd1(e, ssB.Msg, args)
//
// 	// push to server
// 	h.rpcFoo.NetMach.EvAdd1(e, ssF.Msg, args)
// }
//
// func (h *BarHandlersMock) MsgState(e *am.Event) {
// 	args := example.ParseArgs(e.Args)
//
// 	// both foo and bar mutate this state
// 	author := "bar"
// 	if e.Mutation().Source != nil {
// 		author = e.Mutation().Source.MachId
// 	}
//
// 	msg := fmt.Sprintf("[%s] %s", author, args.Msg)
// 	h.t.Log(msg)
// }
