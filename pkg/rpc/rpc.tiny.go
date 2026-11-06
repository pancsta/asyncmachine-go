//go:build tinygo

package rpc

import (
	"strconv"
	"strings"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func BindHandlersClient(h *Client, mach *am.Machine) (string, error) {
	return mach.HandlersBindMaps(
		ClientNegotiations(h), ClientFinals(h))
}

func BindHandlersServer(h *Server, mach *am.Machine) (string, error) {
	return mach.HandlersBindMaps(
		ServerNegotiations(h), ServerFinals(h))
}

// ///// ///// /////

// ///// NETWORK MACHINE

// ///// ///// /////

// newHandlerStruct creates a [handler] binding for a map.
func (m *NetworkMachine) newHandlerMap(
	negotiations map[string]am.HandlerNegotiation, finals map[string]am.HandlerFinal,
) *handler {
	e := &handler{
		isMap:        true,
		finals:       finals,
		negotiations: negotiations,
	}

	return e
}

// HandlersBind is [am.Api.BindHandlers]. Not supported in TinyGo, use
// [am.Machine.HandlerBindMaps] instead.
func (m *NetworkMachine) HandlersBind(
	handlers any, opts ...am.BindOpts,
) (string, error) {
	panic("HandlersBind() not available in tinygo, use HandlersBindMaps()")
}

// handle runs a single handler method (currently only pipes).
func (m *NetworkMachine) handle(h *handler, i int, state, suffix string) {
	// TODO only maps
	h.mx.Lock()
	methodName := state + suffix
	e := am.NewEvent(nil, m)
	e.Name = methodName
	e.MachineId = m.remoteId

	// TODO descriptive name
	handlerName := strconv.Itoa(i) + ":" + h.id
	// handlerNeg, ok1 := h.methods

	if m.semLogger.Level() >= am.LogEverything {
		emitterId := utils.TruncateStr(handlerName, 15)
		emitterId = utils.PadString(strings.ReplaceAll(
			emitterId, " ", "_",
		), 15, "_")
		m.log(am.LogEverything, "[handle:%-15s] %s", emitterId, methodName)
	}

	// ret := true
	// if c.negotiation != nil {
	// 	ret = c.negotiation(c.event)
	// } else if c.final != nil {
	// 	c.final(c.event)
	// } else {
	// 	panic("invalid handler call " + c.name)
	// }

	// cache
	_, ok := h.missingCache[methodName]
	if ok {
		h.mx.Unlock()

		return
	}
	method, ok := h.methodCache[methodName]
	if !ok {
		method = h.methods.MethodByName(methodName)

		// support field handlers
		if !method.IsValid() {
			method = h.methods.Elem().FieldByName(methodName)
		}
		if !method.IsValid() {
			h.missingCache[methodName] = struct{}{}
			h.mx.Unlock()
			return
		}
		h.methodCache[methodName] = method
	}

	// call the handler (pipes dont block)
	m.log(am.LogOps, "[handler:%d] %s", h.num, methodName)

	// tracers
	// m.tracersMx.RLock()
	tx := m.t.Load()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// TODO remove
	// call TODO should go-fork to avoid nested deadlocks?
	// _ = method.Call([]reflect.Value{reflect.ValueOf(e)})

	// tracers
	// m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].HandlerEnd(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// locks
	h.mx.Unlock()
}
