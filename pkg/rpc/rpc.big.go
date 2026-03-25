//go:build !tinygo

package rpc

import (
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/cenkalti/rpc2"
	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func BindHandlersClient(h *Client, mach *am.Machine) error {
	return mach.BindHandlers(h)
}

func BindHandlersServer(h *Server, mach *am.Machine) error {
	return mach.BindHandlers(h)
}

func BindStateSourcePayload(h *SendPayloadHandlers, mach am.Api) error {
	return mach.BindHandlers(h)
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

// ParseArgs extracts A from [am.Event.Args][APrefix].
func ParseArgs(args am.A) *A {
	if r, _ := args[APrefix].(*ARpc); r != nil {
		return amhelp.ArgsToArgs(r, &A{})
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
	return am.A{APrefix: amhelp.ArgsToArgs(args, &ARpc{})}
}

// ///// ///// /////

// ///// NETWORK MACHINE

// ///// ///// /////

// handler represents a list of transition handler methods.
type handler struct {
	h            any
	name         string
	mx           sync.Mutex
	methods      *reflect.Value
	methodCache  map[string]reflect.Value
	missingCache map[string]struct{}
	finals       map[string]am.HandlerFinal
	methodNames  []string
}

func newHandler(
	handlers any, name string, methods *reflect.Value,
) *handler {
	return &handler{
		name:         name,
		h:            handlers,
		methods:      methods,
		methodCache:  make(map[string]reflect.Value),
		missingCache: make(map[string]struct{}),
	}
}

// BindHandlers is [am.Api.BindHandlers].
//
// NetworkMachine supports only pipe handlers (final ones, without negotiation).
func (m *NetworkMachine) BindHandlers(handlers any) error {
	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(handlers).Elem().Name()
	m.handlersMx.Lock()
	defer m.handlersMx.Unlock()

	h := newHandler(handlers, name, &v)

	// TODO race
	// old := m.getHandlers(false)
	// m.setHandlers(false, append(old, h))

	m.handlers = append(m.handlers, h)
	if name != "" {
		m.log(am.LogOps, "[handlers] bind %s", name)
	} else {
		// index for anon handlers
		// TODO race
		m.log(am.LogOps, "[handlers] bind %d", len(m.handlers))
	}

	return nil
}

// DetachHandlers is [am.Api.DetachHandlers].
func (m *NetworkMachine) DetachHandlers(handlers any) error {
	m.handlersMx.Lock()
	defer m.handlersMx.Unlock()

	old := m.handlers

	for _, h := range old {
		if h.h == handlers {
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
	m.log(am.LogOps, "[handler:%d] %s", i, methodName)

	// tracers
	// m.tracersMx.RLock()
	tx := m.t.Load()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// call synchronously
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

// BindTracer is [am.Machine.BindTracer].
//
// NetworkMachine tracers cannot mutate synchronously, as network machines
// don't have a queue and WILL deadlock when nested.
func (m *NetworkMachine) BindTracer(tracer am.Tracer) error {
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

	m.tracers = append(m.tracers, tracer)
	m.log(am.LogOps, "[tracers] bind %s", name)

	return nil
}

// DetachTracer is [am.Api.DetachTracer].
func (m *NetworkMachine) DetachTracer(tracer am.Tracer) error {
	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("DetachTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

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

	// args TODO cache
	if s.Args != nil {
		// TODO use JSON field names
		args, err := utils.StructFields(s.Args)
		if err != nil {
			return err
		}
		(*resp).Args = args
	}

	return nil
}

// bindCustomSendPayload creates and binds SendPayload handlers for a custom
// (dynamic) state name. Useful when binding >1 RPC server into the same state
// source.
func bindCustomSendPayload(s *Server, source am.Api, stateName string) error {
	// TODO migrate to ampipe.Bind
	fn := newSendPayloadState(s, stateName)

	// define a struct with the handler
	structType := reflect.StructOf([]reflect.StructField{
		{
			Name: stateName + am.SuffixState,
			Type: reflect.TypeOf(fn),
		},
	})

	// new instance and set handler
	val := reflect.New(structType).Elem()
	val.Field(0).Set(reflect.ValueOf(fn))
	handlers := val.Addr().Interface()

	return source.BindHandlers(handlers)
}
