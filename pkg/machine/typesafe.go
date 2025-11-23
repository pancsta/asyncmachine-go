package machine

import (
	"maps"
	"reflect"
	"slices"
)

// ///// ///// /////

// ///// STATES & SCHEMA

// ///// ///// /////

type StatesBase struct {
	// Exception is the only built-in state and mean a global error. All errors
	// have to [State.Require] the Exception state. If [Machine.PanicToErr] is
	// true, Exception will receive it.
	Exception   string
	names       S
	groups      map[string][]int
	groupsOrder []string
}

var _ States = &StatesBase{}

func (b *StatesBase) Names() S {
	return b.names
}

func (b *StatesBase) StateGroups() (map[string][]int, []string) {
	return b.groups, b.groupsOrder
}

func (b *StatesBase) SetNames(names S) {
	b.names = slicesUniq(names)
}

func (b *StatesBase) SetStateGroups(groups map[string][]int, order []string) {
	b.groups = groups
	b.groupsOrder = order
}

type States interface {
	// Names returns the state names of the state machine.
	Names() S
	StateGroups() (map[string][]int, []string)
	SetNames(S)
	SetStateGroups(map[string][]int, []string)
}

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

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

// AT represents typed arguments of pkg/machine, extracted from Event.Args
// via ParseArgs, or created manually to for Pass.
type AT struct {
	Err          error
	ErrTrace     string
	Panic        *ExceptionArgsPanic
	TargetStates S
	CalledStates S
	TimeBefore   Time
	TimeAfter    Time
	Event        *Event
	// MutDone chan gets closed by the machine once it's processed. Can cause chan
	// leaks when misused. Only for Can* checks.
	CheckDone *CheckDone
}

type CheckDone struct {
	// TODO close these on dispose and deadline
	Ch chan struct{}
	// Was the mutation canceled?
	Canceled bool
}

const (
	argErr       = "_am_err"
	argErrTrace  = "_am_errTrace"
	argPanic     = "_am_panic"
	argCheckDone = "_am_checkDone"
)

// ParseArgs extracts AT from A.
func ParseArgs(args A) *AT {
	ret := &AT{}

	if val, ok := args[argErr]; ok {
		ret.Err = val.(error)
	}
	if val, ok := args[argErrTrace]; ok {
		ret.ErrTrace = val.(string)
	}
	if val, ok := args[argPanic]; ok {
		ret.Panic = val.(*ExceptionArgsPanic)
	}
	if val, ok := args[argCheckDone]; ok {
		ret.CheckDone = val.(*CheckDone)
	}

	return ret
}

// Pass prepares A from AT, to pass to further mutations.
func Pass(args *AT) A {
	a := A{}

	if args.Err != nil {
		a[argErr] = args.Err
	}
	if args.ErrTrace != "" {
		a[argErrTrace] = args.ErrTrace
	}
	if args.Panic != nil {
		a[argPanic] = args.Panic
	}
	if args.CheckDone != nil {
		a[argCheckDone] = args.CheckDone
	}

	return a
}

// PassMerge prepares A from AT and existing A, to pass to further
// mutations.
func PassMerge(existing A, args *AT) A {
	var a A
	if existing == nil {
		a = A{}
	} else {
		a = maps.Clone(existing)
	}

	// unmarshal
	for k, v := range Pass(args) {
		a[k] = v
	}

	return a
}
