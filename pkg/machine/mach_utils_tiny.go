//go:build tinygo

package machine

// handlerReflect is empty in TinyGo, which has no struct-based handlers.
type handlerReflect struct{}

// handlerCallReflect is empty in TinyGo, which has no struct-based handlers.
type handlerCallReflect struct{}

func newHandlerCallStruct(
	e *Event, h *handler, methodName string, m *Machine,
) *handlerCall {
	panic("not available in tinygo, use HandlersBindMaps()")
}
