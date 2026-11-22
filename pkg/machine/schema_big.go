//go:build !tinygo

package machine

import (
	"reflect"
	"slices"
)

// NewStates inits the *StatesDef struct by assigning state names to string
// values and storing the name order.
func NewStates[G States](states G) G {
	// read and assign names of all the embedded structs
	names := S{}
	groups := map[string][]int{}
	v := reflect.ValueOf(&states).Elem()
	order := []string{}
	parseStateNames(v, &names, "self", groups, &order, "")
	states.SetNames(names)
	states.SetStateGroups(groups, order)

	return states
}

func parseStateNames(
	v reflect.Value, names *S, group string, groups map[string][]int,
	order *[]string, prefix string,
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

		// nested struct (inherit states with a prefix)
		if !field.Anonymous && kind == reflect.Pointer &&
			field.Type.Elem().Kind() == reflect.Struct {

			if value.IsNil() {
				elem := reflect.New(field.Type.Elem())
				value.Set(elem)
			}

			// set prefix on nested struct's StatesBase
			baseField := value.Elem().FieldByName("StatesBase")
			if baseField.IsValid() && baseField.Kind() == reflect.Pointer {
				if baseField.IsNil() {
					baseField.Set(reflect.New(baseField.Type().Elem()))
				}
				if base, ok := baseField.Interface().(*StatesBase); ok {
					base.prefix = prefix + field.Name
				}
			}

			parseStateNames(
				value.Elem(), names, field.Name, groups, order, prefix+field.Name)

		} else if field.Anonymous && kind == reflect.Pointer &&
			// embedded struct (inherit states)
			field.Type.Elem().Kind() == reflect.Struct {

			if value.IsNil() {
				elem := reflect.New(field.Type.Elem())
				value.Set(elem)
			}

			parseStateNames(value.Elem(), names, field.Name, groups, order, prefix)

		} else if value.CanSet() && kind == reflect.String {
			// local state name
			stateName := prefix + field.Name
			value.SetString(stateName)
			if !slices.Contains(*names, stateName) {
				if group != "StatesBase" {
					groups[group] = append(groups[group], len(*names))
				}
				*names = append(*names, stateName)
			}
		}
	}
}

// NewStateGroups inits a *GroupsDef struct with state lists and optionally
// inherits from parent *GroupsDefs instances.
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

		if field.Anonymous && kind == reflect.Pointer &&
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

	if srcVal.Kind() == reflect.Pointer {
		srcVal = srcVal.Elem()
	}
	if dstVal.Kind() == reflect.Pointer {
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
