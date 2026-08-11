package vfs

import (
	"fmt"
	"reflect"

	"github.com/gookit/goutil/dump"
	// all testdata/* imports are replaced with "host" in yaegi
	"github.com/pancsta/asyncmachine-go/pkg/integrations/yaegi/testdata/vfs/_pkg/src/host"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

func Run() *host.Ret {

	// TODO move to StartState
	// w := &mutodon2{
	// 	WorkflowHost: newWorkflow(tbot, mutodon2Markdown),
	// }
	// w.Prompts["Mutodon2Checking"] = NewPromptMutodon2Checking(tbot.A)

	// TODO collect many schemas, IoC SetSchema
	schema := host.H.Mach.Schema()
	names := host.H.Mach.StateNames().Add(ss.Names())
	schema = schema.Merge(Schema)
	// names = append(names, "Names"()...)
	err := host.H.Mach.SetSchema(schema, names)
	if err != nil {
		dump.Println("names", names)
		dump.Println("len schema", len(schema))
		fmt.Println("PANIC!!!")
		panic(err)
	}

	h := &Handlers{}
	// _, err = host.Mach.BindHandlers(h)
	id, err := host.H.Mach.HandlersBindMaps(nil, map[string]am.HandlerFinal{
		"StartState": h.StartState,
	})
	if err != nil {
		panic(err)
	}

	host.H.Mach.Add1("Start", nil)

	return &host.Ret{
		Schema:    Schema,
		Names:     ss.Names(),
		BindingId: id,
	}
}

type Handlers struct{}

func (h *Handlers) StartState(e *am.Event) {
	e.Machine().Add1(ss.Bar, nil)
}

// -----------------------------------------------------------------------------
// -----------------------------------------------------------------------------
// schema.go -------------------------------------------------------------------
// -----------------------------------------------------------------------------
// -----------------------------------------------------------------------------

// StatesDef contains all the states of the MachTemplate state machine.
type StatesDef struct {
	*am.StatesBase

	ErrExample string
	Foo        string
	Bar        string
	Baz        string
	BazDone    string
	Channel    string

	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
	// inherit from ConnectedStatesDef
	*ssam.ConnectedStatesDef
	// inherit from DisposedStatesDef
	*ssam.DisposedStatesDef
	// inherit from StateSourceStatesDef
	*ssrpc.StateSourceStatesDef
}

// GroupsDef contains all the state groups MachTemplate state machine.
type GroupsDef struct {
	*ssam.ConnectedGroupsDef
	Group1 S
	Group2 S
}

// Schema represents all relations and properties of MachTemplateStates.
var Schema = ssam.BasicSchema.Merge(
	// inherit from ConnectedSchema
	ssam.ConnectedSchema,
	// inherit from DisposedSchema
	ssam.DisposedSchema,
	// inherit from StateSourceStatesDef
	ssrpc.StateSourceSchema,
	am.Schema{

		ss.ErrExample: {
			Require: S{ss.Exception},
		},
		ss.Foo: {
			Require: S{ss.Bar},
		},
		ss.Bar: {},
		ss.Baz: {
			Multi: true,
		},
		ss.BazDone: {
			Multi: true,
		},
		ss.Channel: {},
	})

// EXPORTS AND GROUPS

var (
	ss = NewStates(StatesDef{})
	sg = NewStateGroups(GroupsDef{
		Group1: S{},
		Group2: S{},
	}, ssam.ConnectedGroups)

	// States contains all the states for the MachTemplate machine.
	States = ss
	// Groups contains all the state groups for the MachTemplate machine.
	Groups = sg
)

// -----------------------------------------------------------------------------
// -----------------------------------------------------------------------------
// GENERICS BOILERPLATE --------------------------------------------------------
// -----------------------------------------------------------------------------
// -----------------------------------------------------------------------------

type S = am.S

func NewStates(states StatesDef) StatesDef {
	states.StatesBase = &am.StatesBase{}
	// read and assign names of all the embedded structs
	names := am.S{}
	groups := map[string][]int{}
	v := reflect.ValueOf(&states).Elem()
	order := []string{}
	parseStateNames(v, &names, "self", groups, &order)
	states.SetNames(names)
	states.SetStateGroups(groups, order)

	return states
}

// NewStateGroups inits a *GroupsDef struct with state lists and optionally
// inherits from parent *GroupsDefs instances.
func NewStateGroups(groups GroupsDef, mixins ...any) GroupsDef {
	// init nil embeds
	v := reflect.ValueOf(&groups).Elem()
	initNilEmbeds(v)

	// assign values from parent mixins into the local instance
	for i := range mixins {
		copyFields(mixins[i], &groups)
	}

	return groups
}

func parseStateNames(
	v reflect.Value, names *am.S, group string, groups map[string][]int,
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

		if kind == reflect.Ptr &&
			// embedded struct (inherit states)
			field.Type.Elem().Kind() == reflect.Struct {

			if value.IsNil() {
				elem := reflect.New(field.Type.Elem())
				value.Set(elem)
			}
			parseStateNames(value.Elem(), names, field.Name, groups, order)

		} else if value.CanSet() && kind == reflect.String {
			// local state name TODO prefix
			value.SetString(field.Name)
			found := false
			for _, name := range *names {
				if name == field.Name {
					found = true
					break
				}
			}
			if !found {
				if group != "StatesBase" {
					groups[group] = append(groups[group], len(*names))
				}
				*names = append(*names, field.Name)
			}
		}
	}
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
	if src == nil {
		return
	}
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
