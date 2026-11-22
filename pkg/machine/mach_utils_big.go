//go:build !tinygo

package machine

import (
	"os"
	"reflect"
)

// handlerReflect holds struct-based handler state. Not available in TinyGo.
type handlerReflect struct {
	h            any
	methods      *reflect.Value
	methodCache  map[string]reflect.Value
	missingCache map[string]struct{}
}

// handlerCallReflect holds the reflected handler method. Not available in
// TinyGo.
type handlerCallReflect struct {
	fn *reflect.Value
}

func newHandlerCallStruct(
	e *Event, h *handler, methodName string, m *Machine,
) *handlerCall {
	//

	// cache
	_, ok := h.refl.missingCache[methodName]
	if ok {
		h.mx.Unlock()

		return nil
	}
	method, ok := h.refl.methodCache[methodName]
	if !ok {
		method = h.refl.methods.MethodByName(methodName)

		// support field handlers
		if !method.IsValid() {
			method = h.refl.methods.Elem().FieldByName(methodName)
		}
		if !method.IsValid() {
			h.refl.missingCache[methodName] = struct{}{}
			h.mx.Unlock()

			return nil
		}
		h.refl.methodCache[methodName] = method
	}
	h.mx.Unlock()

	// call the handler
	m.log(LogOps, "[handler:%d] %s", h.num, methodName)
	m.currentHandler.Store(methodName)

	// tracers
	m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(m.t.Load(), h.id, methodName)
	}
	m.tracersMx.RUnlock()

	c := &handlerCall{
		refl:    handlerCallReflect{fn: &method},
		name:    methodName,
		event:   e,
		timeout: false,
	}

	// TODO not via env?
	if os.Getenv(EnvAmDetectEval) != "" {
		c.debug = h.id + "." + methodName
	}

	return c
}
