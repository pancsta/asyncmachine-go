//go:build tinygo

package machine

import "errors"

// ///// ///// /////

// ///// MACHINE

// ///// ///// /////

// HandlersBind binds a struct of handler methods to machine's states, based on
// the naming convention, eg `FooState(e *Event)`. Negotiation handlers can
// optionally return bool.
//
// # TinyGo
//
// Not supported in TinyGo, use [Machine.HandlersBindMaps] instead.
func (m *Machine) HandlersBind(handlers any, opts ...BindOpts) (string, error) {
	panic("not available in tinygo, use BindHandlerMaps()")
}

func (c *handlerCall) Exec() *handlerReturn {
	ret := handlerReturn{}
	if c.negotiation != nil {
		ret.retBool = c.negotiation(c.event)
		ret.called = true
		// TODO List ret for map handlers
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

func tracerValidate(tracer Tracer) error {
	if tracer == nil {
		return errors.New("BindTracer expects a pointer to a struct")
	}

	return nil
}

// SetGroups organizes the schema into a tree using schema-v2 structs. TinyGo
// has no struct reflection, so only the states-based groups are used.
func (m *Machine) SetGroups(groups any, optStates States) {
	m.schemaMx.Lock()
	defer m.schemaMx.Unlock()
	list := map[string][]int{}
	order := []string{}

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

func funcName(fn func()) string {
	return ""
}
