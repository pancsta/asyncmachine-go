//go:build !tinygo

package machine

import (
	"errors"
	"reflect"
	"runtime"
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

func (c *handlerCall) Exec() *handlerReturn {
	ret := handlerReturn{}
	if c.refl.fn != nil {
		callRet := c.refl.fn.Call([]reflect.Value{reflect.ValueOf(c.event)})
		ret.called = true
		if len(callRet) > 0 {
			// TODO log err?
			v1, ok1 := callRet[0].Interface().(bool)
			v2, ok2 := callRet[0].Interface().(S)
			if ok1 {
				ret.retBool = v1
			} else if ok2 {
				// TODO optimize: with nil as prev value
				ret.retStates = v2
				ret.partialNeg = true
			}
		} else {
			// default
			ret.retBool = true
		}
	} else if c.negotiation != nil {
		ret.retBool = c.negotiation(c.event)
		ret.called = true
	} else if c.negotiationPartial != nil {
		ret.retStates = c.negotiationPartial(c.event)
		ret.partialNeg = true
		ret.called = true
	} else if c.final != nil {
		c.final(c.event)
		ret.called = true
	} else {
		panic("invalid handler call " + c.name)
	}

	return &ret
}

// newHandlerStruct creates a [handler] binding for a struct.
func (m *Machine) newHandlerStruct(
	handlers any, methods *reflect.Value,
) *handler {
	if m.disposing.Load() {
		return &handler{}
	}
	e := &handler{
		isMap: false,
		refl: handlerReflect{
			h:            handlers,
			methods:      methods,
			methodCache:  make(map[string]reflect.Value),
			missingCache: make(map[string]struct{}),
		},
	}

	return e
}

func tracerValidate(tracer Tracer) error {
	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}

	return nil
}

// SetGroups organizes the schema into a tree using schema-v2 structs.
func (m *Machine) SetGroups(groups any, optStates States) {
	// TODO rename to SchemaOrganize(optGroups, optStates, ...)
	// TODO call VerifyStates from optStates.Names()

	m.schemaMx.Lock()
	defer m.schemaMx.Unlock()
	list := map[string][]int{}
	order := []string{}
	index := m.stateNames

	// add all the groups
	if groups != nil {
		// TODO extract
		var walk func(reflect.Value)
		walk = func(val reflect.Value) {
			if val.Kind() == reflect.Pointer {
				if val.IsNil() {
					return
				}
				val = val.Elem()
			}
			if val.Kind() != reflect.Struct {
				return
			}

			typ := val.Type()
			// Local groups first.
			for i := 0; i < val.NumField(); i++ {
				field := typ.Field(i)
				if field.Type.Kind() != reflect.Slice {
					continue
				}
				value := val.Field(i)
				if !value.CanInterface() {
					continue
				}
				states, ok := value.Interface().(S)
				if !ok {
					continue
				}
				if _, ok := list[field.Name]; ok {
					continue
				}
				list[field.Name] = index.Index(states)
				order = append(order, field.Name)
			}

			// Then inherited embedded groups.
			for i := 0; i < val.NumField(); i++ {
				field := typ.Field(i)
				if !field.Anonymous {
					continue
				}
				kind := field.Type.Kind()
				if kind == reflect.Struct ||
					(kind == reflect.Pointer &&
						field.Type.Elem().Kind() == reflect.Struct) {

					walk(val.Field(i))
				}
			}
		}
		walk(reflect.ValueOf(groups))
	}

	// add all the schemas (nested)

	// state schema structure
	if optStates != nil {
		groups, order2 := optStates.StateGroups()
		for _, name := range order2 {
			list[name] = groups[name]
			order = append(order, name)
		}
	}

	m.groups = list
	m.groupsOrder = order
}

// TODO move
func funcName(fn func()) string {
	funcPtr := reflect.ValueOf(fn).Pointer()
	funcObj := runtime.FuncForPC(funcPtr)
	if funcObj == nil {
		return ""
	}
	funcName := funcObj.Name()

	// file, line := funcObj.FileLine(funcPtr)
	return funcName
}
