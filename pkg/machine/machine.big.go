//go:build !tinygo

package machine

import (
	"errors"
	"reflect"
)

// ///// ///// /////

// ///// MACHINE

// ///// ///// /////

// HandlersBind binds a struct of handler methods to machine's states, based on
// the naming convention, eg `FooState(e *Event)`. Negotiation handlers can
// optionally return bool.
func (m *Machine) HandlersBind(handlers any, opts ...BindOpts) (string, error) {
	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return "", errors.New("BindHandlers expects a pointer to a struct")
	}

	return m.bindHandlers(m.newHandlerStruct(handlers, &v), optBindOpts(opts))
}

func (c *handlerCall) Exec() bool {
	ret := true
	if c.fn != nil {
		callRet := c.fn.Call([]reflect.Value{reflect.ValueOf(c.event)})
		if len(callRet) > 0 {
			// TODO log err?
			ret, _ = callRet[0].Interface().(bool)
		}
	} else if c.negotiation != nil {
		ret = c.negotiation(c.event)
	} else if c.final != nil {
		c.final(c.event)
	} else {
		panic("invalid handler call " + c.name)
	}

	return ret
}