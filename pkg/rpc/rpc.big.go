//go:build !tinygo

package rpc

import (
	"errors"
	"reflect"
	"strconv"
	"strings"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func BindHandlersClient(h *Client, mach *am.Machine) (string, error) {
	return mach.HandlersBind(h)
}

func BindHandlersServer(h *Server, mach *am.Machine) (string, error) {
	return mach.HandlersBind(h)
}

// ///// ///// /////

// ///// NETWORK MACHINE

// ///// ///// /////

func newHandler(
	handlers any, name string, methods *reflect.Value,
) *handler {
	return &handler{
		id:           name,
		h:            handlers,
		methods:      methods,
		methodCache:  make(map[string]reflect.Value),
		missingCache: make(map[string]struct{}),
	}
}

// HandlersBind is [am.Api.BindHandlers].
//
// NetworkMachine supports only pipe handlers (final ones, without negotiation).
func (m *NetworkMachine) HandlersBind(
	handlers any, opts ...am.BindOpts,
) (string, error) {
	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return "", errors.New("BindTracer expects a pointer to a struct")
	}
	m.handlersMx.Lock()
	defer m.handlersMx.Unlock()

	o := optBindOpts(opts)
	// TODO name from stack trace
	name := "anon" + amhelp.RandId(4)
	if o.Id != "" {
		name = idRe.ReplaceAllString(o.Id, "")
	}
	num := m.nextHandlerNum
	m.nextHandlerNum++
	h := newHandler(handlers, name, &v)
	h.num = num
	m.handlers = append(m.handlers, h)
	m.log(am.LogOps, "[handlers] bind %d:%s", len(m.handlers)-1, name)

	return name, nil
}

// handle runs a single handler method (currently only pipes).
func (m *NetworkMachine) handle(h *handler, i int, state, suffix string) {
	h.mx.Lock()
	methodName := state + suffix
	e := am.NewEvent(nil, m)
	e.Name = methodName
	e.MachineId = m.remoteId

	// TODO descriptive name
	handlerName := strconv.Itoa(i) + ":" + h.id

	if m.semLogger.Level() >= am.LogEverything {
		emitterId := utils.TruncateStr(handlerName, 15)
		emitterId = utils.PadString(strings.ReplaceAll(
			emitterId, " ", "_",
		), 15, "_")
		m.log(am.LogEverything, "[handle:%-15s] %s", emitterId, methodName)
	}

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

	// call TODO should go-fork to avoid nested deadlocks?
	_ = method.Call([]reflect.Value{reflect.ValueOf(e)})

	// tracers
	// m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].HandlerEnd(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// locks
	h.mx.Unlock()
}
