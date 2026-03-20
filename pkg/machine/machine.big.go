//go:build !tinygo

package machine

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// ///// ///// /////

// ///// MACHINE

// ///// ///// /////

// BindHandlers binds a struct of handler methods to machine's states, based on
// the naming convention, eg `FooState(e *Event)`. Negotiation handlers can
// optionally return bool. Not supported in TinyGo, use
// [Machine.BindHandlerMaps] instead.
func (m *Machine) BindHandlers(handlers any) error {
	// TODO optimize: optionally loose reflect (pointer map)
	if m.disposing.Load() {
		return nil
	}
	first := false
	if !m.handlerLoopRunning.Load() {
		first = true
		m.handlerLoopRunning.Store(true)

		// start the handler loop
		go m.handlerLoop()
	}

	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindHandlers expects a pointer to a struct")
	}

	// extract the name
	name := reflect.TypeOf(handlers).Elem().Name()
	if name == "" {
		name = "anon"
		if os.Getenv(EnvAmDebug) != "" {
			buf := make([]byte, 4024)
			n := runtime.Stack(buf, false)
			stack := string(buf[:n])
			lines := strings.Split(stack, "\n")
			name = lines[len(lines)-2]
			name = strings.TrimLeft(emitterNameRe.FindString(name), "/")
		}
	}

	// detect methods
	var methodNames []string
	if m.detectEval || os.Getenv(EnvAmDebug) != "" {
		var err error
		methodNames, err = ListHandlers(handlers, m.stateNames)
		if err != nil {
			return fmt.Errorf("listing handlers: %w", err)
		}
	}

	h := m.newHandler(handlers, name, &v, methodNames)
	old := m.getHandlers(false)
	m.setHandlers(false, append(old, h))
	if name != "" {
		m.log(LogOps, "[handlers] bind %s", name)
	} else {
		// index for anon handlers
		m.log(LogOps, "[handlers] bind %d", len(old))
	}
	// TODO sem logger for handlers

	// if already in Exception when 1st handler group is bound, re-add the err
	if first && m.IsErr() {
		m.AddErr(m.Err(), nil)
	}

	return nil
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
		if h.h == handlers {
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

// BindTracer binds a Tracer to the machine. Tracers can cause StateException in
// submachines, before any handlers are bound. Use the Err() getter to examine
// such errors.
func (m *Machine) BindTracer(tracer Tracer) error {
	if m.disposing.Load() {
		return nil
	}

	m.tracersMx.Lock()
	defer m.tracersMx.Unlock()

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("BindTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

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

	v := reflect.ValueOf(tracer)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return errors.New("DetachTracer expects a pointer to a struct")
	}
	name := reflect.TypeOf(tracer).Elem().Name()

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
	// TODO rename to SchemaOrganize(optGroups, optStates, ...)
	// TODO call VerifyStates from optStates.Names()

	m.schemaMx.Lock()
	defer m.schemaMx.Unlock()
	list := map[string][]int{}
	order := []string{}
	index := m.stateNames

	// add all the groups
	if groups != nil {
		// TODO recursive for inherited groups
		val := reflect.ValueOf(groups)
		typ := val.Type()
		for i := 0; i < val.NumField(); i++ {
			field := typ.Field(i)
			kind := field.Type.Kind()
			if kind != reflect.Slice {
				continue
			}
			name := field.Name
			value := val.Field(i).Interface()
			if states, ok := value.(S); ok {
				list[name] = StatesToIndex(index, states)
				order = append(order, name)
			}
		}
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

// newHandler creates a new handler for Machine.
// Each handler should be consumed by one receiver only to guarantee the
// delivery of all events.
func (m *Machine) newHandler(
	handlers any, name string, methods *reflect.Value, methodNames []string,
) *handler {
	if m.disposing.Load() {
		return &handler{}
	}
	e := &handler{
		name:         name,
		h:            handlers,
		methods:      methods,
		methodNames:  methodNames,
		methodCache:  make(map[string]reflect.Value),
		missingCache: make(map[string]struct{}),
	}

	return e
}

// ///// ///// /////

// ///// STATES & SCHEMA

// ///// ///// /////

func NewStates[G States](states G) G {
	// read and assign names of all the embedded structs
	names := S{}
	groups := map[string][]int{}
	v := reflect.ValueOf(&states).Elem()
	order := []string{}
	parseStateNames(v, &names, "self", groups, &order)
	states.SetNames(names)
	states.SetStateGroups(groups, order)

	return states
}

func parseStateNames(
	v reflect.Value, names *S, group string, groups map[string][]int,
	order *[]string,
) {
	if group != "StatesBase" {
		groups[group] = []int{}
		*order = append(*order, group)
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {

		field := t.Field(i)
		value := v.Field(i)
		kind := field.Type.Kind()

		if field.Anonymous && kind == reflect.Ptr &&
			// embedded struct (inherit states)
			field.Type.Elem().Kind() == reflect.Struct {

			if value.IsNil() {
				elem := reflect.New(field.Type.Elem())
				value.Set(elem)
			}
			parseStateNames(value.Elem(), names, field.Name, groups, order)

		} else if value.CanSet() && kind == reflect.String {
			// local state name
			value.SetString(field.Name)
			if !slices.Contains(*names, field.Name) {
				if group != "StatesBase" {
					groups[group] = append(groups[group], len(*names))
				}
				*names = append(*names, field.Name)
			}
		}
	}
}

// TODO require a mixin with .Names(), like states
func NewStateGroups[G any](groups G, mixins ...any) G {
	// init nil embeds
	v := reflect.ValueOf(&groups).Elem()
	initNilEmbeds(v)

	// assign values from parent mixins into the local instance
	for i := range mixins {
		copyFields(mixins[i], &groups)
	}

	return groups
}

func initNilEmbeds(v reflect.Value) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {

		field := t.Field(i)
		value := v.Field(i)
		kind := field.Type.Kind()

		if field.Anonymous && kind == reflect.Ptr &&
			field.Type.Elem().Kind() == reflect.Struct {

			if value.IsNil() {
				elem := reflect.New(field.Type.Elem())
				value.Set(elem)
			}
			initNilEmbeds(value.Elem())
		}
	}
}

func copyFields(src, dst interface{}) {
	srcVal := reflect.ValueOf(src)
	dstVal := reflect.ValueOf(dst)

	if srcVal.Kind() == reflect.Ptr {
		srcVal = srcVal.Elem()
	}
	if dstVal.Kind() == reflect.Ptr {
		dstVal = dstVal.Elem()
	}

	for i := 0; i < srcVal.NumField(); i++ {
		name := srcVal.Type().Field(i).Name
		srcField := srcVal.Field(i)
		dstField := dstVal.FieldByName(name)

		if srcField.Kind() == reflect.Struct {
			copyFields(srcField.Addr().Interface(), dstField.Addr().Interface())
		} else {
			if dstField.CanSet() {
				dstField.Set(srcField)
			}
		}
	}
}

// MISC

// handler represents a list of transition handler methods.
type handler struct {
	h any
	// TODO rename to id
	name string
	mx   sync.Mutex
	// disposed     bool
	methods      *reflect.Value
	negotiations map[string]HandlerNegotiation
	finals       map[string]HandlerFinal
	methodNames  []string
	methodCache  map[string]reflect.Value
	missingCache map[string]struct{}
}

// TODO remove
func (e *handler) dispose() {
}

type handlerCall struct {
	fn      reflect.Value
	name    string
	event   *Event
	timeout bool
}

func (c *handlerCall) Exec() bool {
	callRet := c.fn.Call([]reflect.Value{reflect.ValueOf(c.event)})
	if len(callRet) > 0 {
		// TODO log err, dont panic
		return callRet[0].Interface().(bool)
	}

	return true
}

// ListHandlers returns a list of handler method names from a handler struct,
// limited to [states].
func ListHandlers(handlers any, states S) ([]string, error) {
	var methodNames []string
	var errs []error

	check := func(method string) {
		s1, s2 := IsHandler(states, method)
		if s1 != "" && !slices.Contains(states, s1) {
			errs = append(errs, fmt.Errorf(
				"%w: %s from handler %s", ErrStateMissing, s1, method))
		}
		if s2 != "" && !slices.Contains(states, s2) {
			errs = append(errs, fmt.Errorf(
				"%w: %s from handler %s", ErrStateMissing, s2, method))
		}

		if s1 != "" || method == StateAny+SuffixEnter ||
			method == StateAny+SuffixState {

			methodNames = append(methodNames, method)
			// TODO verify method signatures early (returns and params)
		}
	}

	// methods
	t := reflect.TypeOf(handlers)
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i).Name
		check(method)
	}

	// fields
	val := reflect.ValueOf(handlers).Elem()
	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		kind := typ.Field(i).Type.Kind()
		if kind != reflect.Func {
			continue
		}
		method := typ.Field(i).Name
		check(method)
	}

	return methodNames, errors.Join(errs...)
}

func newHandlerCall(
	e *Event, h *handler, methodName string, m *Machine, i int,
	handlerName string,
) *handlerCall {
	//

	// cache
	_, ok := h.missingCache[methodName]
	if ok {
		h.mx.Unlock()
		return nil
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
			return nil
		}
		h.methodCache[methodName] = method
	}
	h.mx.Unlock()

	// call the handler
	m.log(LogOps, "[handler:%d] %s", i, methodName)
	m.currentHandler.Store(methodName)

	// tracers
	m.tracersMx.RLock()
	tx := m.t.Load()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(tx, handlerName, methodName)
	}
	m.tracersMx.RUnlock()

	return &handlerCall{
		fn:      method,
		name:    methodName,
		event:   e,
		timeout: false,
	}
}
