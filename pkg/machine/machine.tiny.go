//go:build tinygo

package machine

import (
	"errors"
	"slices"
	"strings"
	"sync"
)

// ///// ///// /////

// ///// MACHINE

// ///// ///// /////

// TODO add `id string`, same to detach, same with gen-ed BindHandlersMach(id, ...)
func (m *Machine) BindHandlers(handlers any) error {
	panic("BindHandlers() not available in tinygo, use BindHandlerMaps()")
}

// BindTracer binds a Tracer to the machine. Tracers can cause StateException in
// submachines, before any handlers are bound. Use the Err() getter to examine
// such errors.
// TODO add `id string`, same to detach
func (m *Machine) BindTracer(tracer Tracer) error {
	if m.disposing.Load() {
		return nil
	}

	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	name := "TODO"

	m.tracers = append(m.tracers, tracer)
	m.log(LogOps, "[tracers] bind %s", name)

	return nil
}

// DetachTracer tries to remove a tracer from the machine. Returns an error in
// case the tracer wasn't bound.
func (m *Machine) DetachTracer(tracer Tracer) error {
	if m.disposing.Load() {
		return nil
	}

	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	name := "TODO"

	for i, t := range m.tracers {
		if t == tracer {
			// TODO check
			m.tracers = slices.Delete(m.tracers, i, i+1)
			m.log(LogOps, "[tracers] detach %s", name)

			return nil
		}
	}

	return errors.New("tracer not bound")
}

// SetGroups organizes the schema into a tree using schema-v2 structs.
func (m *Machine) SetGroups(groups any, optStates States) {
	panic("not implemented in tinygo")
}

// DetachHandlers detaches previously bound machine handlers.
func (m *Machine) DetachHandlers(handlers any) error {
	if m.disposing.Load() {
		return nil
	}

	m.handlersMx.Lock()
	defer m.handlersMx.Unlock()

	old := m.getHandlers(true)
	var match *handler
	var matchIndex int
	for i, h := range old {
		// TODO check
		if h.name == handlers {
			match = h
			matchIndex = i
			break
		}
	}

	if match == nil {
		return errors.New("handlers not bound")
	}

	m.setHandlers(true, slices.Delete(old, matchIndex, matchIndex+1))
	match.dispose()
	m.log(LogOps, "[handlers] detach %s", match.name)

	return nil
}

// ///// ///// /////

// ///// TYPES

// ///// ///// /////

func NewStates[G States](states G) G {
	return states
}

// TODO require a mixin with .Names(), like states
func NewStateGroups[G any](groups G, mixins ...any) G {
	// init nil embeds

	return groups
}

// handler represents a list of transition handler methods.
type handler struct {
	h            any
	name         string
	mx           sync.Mutex
	negotiations map[string]HandlerNegotiation
	finals       map[string]HandlerFinal
	methodNames  []string
}

// TODO remove
func (e *handler) dispose() {
}

type handlerCall struct {
	// TODO debug only
	name        string
	negotiation HandlerNegotiation
	final       HandlerFinal
	event       *Event
	timeout     bool
}

func (c *handlerCall) Exec() bool {
	if c.negotiation != nil {
		c.negotiation(c.event)
		return true
	}

	c.final(c.event)
	return true
}

// ListHandlers returns a list of handler method names from a handler struct,
// limited to [states].
func ListHandlers(handlers any, states S) ([]string, error) {
	panic("not implemented in tinygo")
}

func newHandlerCall(
	e *Event, h *handler, methodName string, m *Machine, i int,
	handlerName string,
) *handlerCall {
	//

	isFinal := strings.HasSuffix(methodName, SuffixState) ||
		strings.HasSuffix(methodName, SuffixEnd)

	if isFinal {
		if _, ok := h.finals[methodName]; !ok {
			h.mx.Unlock()
			return nil
		}
	} else {
		if _, ok := h.negotiations[methodName]; !ok {
			h.mx.Unlock()
			return nil
		}
	}
	h.mx.Unlock()

	// call the handler
	m.log(LogOps, "[handler:%d] %s", i, methodName)
	m.currentHandler.Store(methodName)

	// tracers
	m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(m.t.Load(), handlerName, methodName)
	}
	m.tracersMx.RUnlock()

	call := &handlerCall{
		name:    methodName,
		event:   e,
		timeout: false,
	}

	if isFinal {
		call.final = h.finals[methodName]
	} else {
		call.negotiation = h.negotiations[methodName]
	}

	return call
}
