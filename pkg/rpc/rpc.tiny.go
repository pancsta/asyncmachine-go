//go:build tinygo

package rpc

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/cenkalti/rpc2"
	"github.com/pancsta/asyncmachine-go/internal/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func BindHandlersClient(h *Client, mach *am.Machine) error {
	return mach.BindHandlerMaps("Client",
		ClientNegotiations(h), ClientFinals(h))
}

func BindHandlersServer(h *Server, mach *am.Machine) error {
	return mach.BindHandlerMaps("Server",
		ServerNegotiations(h), ServerFinals(h))
}

func BindStateSourcePayload(h *SendPayloadHandlers, mach am.Api) error {
	return mach.BindHandlerMaps("Server",
		nil, map[string]am.HandlerFinal{
			"SendPayloadState": h.SendPayloadState,
		})
}

// ParseArgs extracts A from [am.Event.Args][APrefix].
func ParseArgs(args am.A) *A {
	if r, _ := args[APrefix].(*ARpc); r != nil {
		ret := ArgsRpcToArgs(r)
		return &ret
	}
	if a, _ := args[APrefix].(*A); a != nil {
		return a
	}
	return &A{}
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	return am.A{APrefix: args}
}

// PassRpc prepares [am.A] from A to pass over RPC.
func PassRpc(args *A) am.A {
	return am.A{APrefix: ArgsToArgsRpc(args)}
}

// ///// ///// /////

// ///// NETWORK MACHINE

// ///// ///// /////

// handler represents a list of transition handler methods.
type handler struct {
	h           any
	name        string
	mx          sync.Mutex
	finals      map[string]am.HandlerFinal
	methodNames []string
}

func newHandler(name string, finals map[string]am.HandlerFinal) *handler {
	//

	return &handler{
		name:   name,
		finals: finals,
	}
}

// BindHandlers is [am.Api.BindHandlers]. Not supported in TinyGo, use
// // [Machine.BindHandlerMaps] instead.
func (m *NetworkMachine) BindHandlers(handlers any) error {
	panic("BindHandlers() not available in tinygo, use BindHandlerMaps()")
}

// DetachHandlers is [am.Api.DetachHandlers].
func (m *NetworkMachine) DetachHandlers(handlers any) error {
	old := m.handlers

	for _, h := range old {
		if h.name == handlers {
			m.handlers = utils.SlicesWithout(old, h)
			// TODO
			// h.dispose()

			return nil
		}
	}

	return errors.New("handlers not bound")
}

// handle runs a single handler method (currently only pipes).
func (m *NetworkMachine) handle(h *handler, i int, state, suffix string) {
	h.mx.Lock()
	methodName := state + suffix
	e := am.NewEvent(nil, m)
	e.Name = methodName
	e.MachineId = m.remoteId

	// TODO descriptive name
	handlerName := strconv.Itoa(i) + ":" + h.name

	if m.semLogger.Level() >= am.LogEverything {
		emitterId := utils.TruncateStr(handlerName, 15)
		emitterId = utils.PadString(strings.ReplaceAll(
			emitterId, " ", "_"), 15, "_")
		m.log(am.LogEverything, "[handle:%-15s] %s", emitterId, methodName)
	}

	method, ok := h.finals[methodName]
	if !ok {
		return
	}

	// call the handler (pipes dont block)
	m.log(am.LogOps, "[handler:%d] %s", i, methodName)

	// tracers
	// m.tracersMx.RLock()
	tx := m.t.Load()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// call synchronously
	method(e)

	// tracers
	// m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].HandlerEnd(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// locks
	h.mx.Unlock()
}

// BindTracer is [am.Machine.BindTracer].
//
// NetworkMachine tracers cannot mutate synchronously, as network machines
// don't have a queue and WILL deadlock when nested.
func (m *NetworkMachine) BindTracer(tracer am.Tracer) error {
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	// TODO
	name := "TODO"

	m.tracers = append(m.tracers, tracer)
	m.log(am.LogOps, "[tracers] bind %s", name)

	return nil
}

// DetachTracer is [am.Api.DetachTracer].
func (m *NetworkMachine) DetachTracer(tracer am.Tracer) error {
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	// TODO
	name := "TODO"

	for i, t := range m.tracers {
		if t == tracer {
			// TODO check
			m.tracers = slices.Delete(m.tracers, i, i+1)
			m.log(am.LogOps, "[tracers] detach %s", name)

			return nil
		}
	}

	return errors.New("tracer not bound")
}

// ///// ///// /////

// ///// SERVER

// ///// ///// /////

func (s *Server) RemoteArgs(
	_ *rpc2.Client, _ *MsgEmpty, resp *MsgSrvArgs,
) error {
	if s.Mach.Not1(ssS.Start) {
		return am.ErrCanceled
	}

	// args TODO take from codegen

	return nil
}

// bindCustomSendPayload creates and binds SendPayload handlers for a custom
// (dynamic) state name. Useful when binding >1 RPC server into the same state
// source.
func bindCustomSendPayload(s *Server, source am.Api, stateName string) error {
	fn := newSendPayloadState(s, stateName)

	finalsMap := map[string]am.HandlerFinal{
		stateName + am.SuffixState: fn,
	}

	return source.BindHandlerMaps("SendPayload"+stateName, nil, finalsMap)
}
