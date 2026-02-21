//go:build js && wasm

package main

import (
	"context"
	"fmt"
	"log"

	"honnef.co/go/js/dom/v2"

	example "github.com/pancsta/asyncmachine-go/examples/wasm"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

func main() {
	ctx := context.Background()

	// DOM

	uiList := dom.GetWindow().Document().QuerySelector("#list")
	uiDoc := dom.GetWindow().Document()
	uiInput := dom.GetWindow().Document().QuerySelector("#input")

	// handlers

	barHandlers := &BarHandlers{
		uiDoc:   uiDoc,
		uiList:  uiList,
		uiInput: uiInput.(*dom.HTMLInputElement),
	}
	fooHandlers := &FooHandlers{
		uiDoc:  uiDoc,
		uiList: uiList,
	}

	// machines

	barMach, fooClient, fooHandMach := initMachines(ctx, barHandlers, fooHandlers)
	barHandlers.rpcFoo = fooClient
	barHandlers.machBar = barMach
	barHandlers.machFooHand = fooHandMach
	fooHandlers.rpcFoo = fooClient
	fooHandlers.machBar = barMach
	fooHandlers.machFooHand = fooHandMach

	// wait
	select {}
}

//

// Server machine handlers

//

type FooHandlers struct {
	uiDoc  dom.Document
	uiList dom.Element

	rpcFoo      *arpc.Client
	machBar     *am.Machine
	machFooHand *am.Machine
}

func (h *FooHandlers) BoredState(e *am.Event) {
	log.Printf("foo is bored...\n")
	addLi(h.uiDoc, h.uiList, "foo is bored...")
}

//

// Browser machine handlers

//

type BarHandlers struct {
	uiDoc   dom.Document
	uiList  dom.Element
	uiInput *dom.HTMLInputElement

	rpcFoo      *arpc.Client
	machBar     *am.Machine
	machFooHand *am.Machine
}

func (h *BarHandlers) StartState(e *am.Event) {
	log.Println("[bar] StartState")
	addLi(h.uiDoc, h.uiList, "[bar] StartState\n")

	h.uiInput.AddEventListener("keydown", false, func(event dom.Event) {
		keyEvent := event.(*dom.KeyboardEvent)
		if keyEvent.Key() != "Enter" {
			return
		}

		// submit
		event.PreventDefault()
		h.machBar.EvAdd1(e, ssB.SubmitMsg, nil)
	})
}

func (h *BarHandlers) SubmitMsgState(e *am.Event) {
	txt := h.uiInput.Value()
	h.uiInput.SetValue("")
	args := PassRpc(&ARpc{
		Msg: txt,
	})

	// push to self
	h.machBar.EvAdd1(e, ssB.Msg, args)

	// unblock
	go func() {
		// push to server (blocking)
		h.rpcFoo.NetMach.EvAdd1(e, ssF.Msg, args)
	}()
}

func (h *BarHandlers) MsgEnter(e *am.Event) bool {
	return example.ParseArgs(e.Args).Msg != ""
}

func (h *BarHandlers) MsgState(e *am.Event) {
	args := example.ParseArgs(e.Args)

	// both foo and bar mutate this state
	author := "bar"
	if e.Mutation().Source != nil {
		author = e.Mutation().Source.MachId
	}

	msg := fmt.Sprintf("[%s] %s\n", author, args.Msg)
	log.Print(msg)
	addLi(h.uiDoc, h.uiList, msg)
}

//

// Misc

//

func addLi(doc dom.Document, ul dom.Element, text string) {
	li := doc.CreateElement("li")
	li.SetTextContent(text)
	ul.AppendChild(li)
}
