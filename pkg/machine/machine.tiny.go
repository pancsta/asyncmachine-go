//go:build tinygo

package machine

// ///// ///// /////

// ///// MACHINE

// ///// ///// /////

func (m *Machine) HandlersBind(handlers any, opts ...BindOpts) (string, error) {
	panic("not available in tinygo, use BindHandlerMaps()")
}




func (c *handlerCall) Exec() bool {
	ret := true
	if c.negotiation != nil {
		ret = c.negotiation(c.event)
	} else if c.final != nil {
		c.final(c.event)
	} else {
		panic("invalid handler call " + c.name)
	}

	return ret
}