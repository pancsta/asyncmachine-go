package machine

import (
	"reflect"
	"runtime"
	"slices"
	"strings"
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

// States is the vase interface for schema states.
type States interface {
	// Names returns the state names of the state machine.
	Names() S
	// TODO
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

// NewStateGroups accepts the target group with values (FooGroupsDef) and
// inherited GroupsDefs (optionally).
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

// TODO refac to Schema.StatesByTag
func StatesByTag(schema Schema, tag string) S {
	ret := S{}
	for name, s := range schema {
		for _, t := range s.Tags {
			if t == tag || strings.HasPrefix(t, tag+":") {
				ret = append(ret, name)
				break
			}
		}
	}

	return ret
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "_am"

// ArgsApi is the base interface for typed arguments.
type ArgsApi interface {
	// ArgsPrefix returns the argument prefix inside the [A] map. Should be provided by the implementation.
	ArgsPrefix() string
	// ArgsState represents the state this argument struct belongs to. Defaults to [StateAny].
	ArgsState() string
	// TODO deep clone interface, optional
	// ArgsClone() G
}

// ArgsBase is the base implementation of [ArgsApi] interface.
type ArgsBase struct {
	ArgsApi
}

// ArgsPrefix is a fallback to a stack trace hash of the implementation symbol. Each
// pkg should overwrite this method.
func (a ArgsBase) ArgsPrefix() string {
	buf := make([]byte, 4024)
	n := runtime.Stack(buf, false)
	stack := string(buf[:n])
	lines := strings.Split(stack, "\n")
	hash := Hash(lines[3], 10)

	return hash
}

// State is the default Any state.
func (a ArgsBase) ArgsState() string {
	return StateAny
}

// Clone for deep cloning.
func (a ArgsBase) Clone() ArgsApi {
	// shallow clone
	return a
}

// pkg args

// Args for this pkg. Do not reuse.
type Args struct {
	ArgsBase
}

func (Args) ArgsPrefix() string {
	return APrefix
}

// -----

// ExceptionArgsPanic is an optional argument ["panic"] for the StateException
// state which describes a panic within a Transition handler.
type ExceptionArgsPanic struct {
	CalledStates S
	StatesBefore S
	Transition   *Transition
	LastStep     *Step
	StackTrace   string
}

type AException struct {
	Args
	Err      error `log:"err"`
	ErrTrace string
	Panic    *ExceptionArgsPanic

	// TODO handle
	Fn string `log:"fn"`

	// deadline

	TargetStates S
	CalledStates S
	TimeBefore   Time
	TimeAfter    Time
	Event        *Event
}

func (AException) ArgsState() string {
	return StateException
}

// ACheck with a CheckDone chan which gets closed by the machine once it's
// processed. Can cause chan leaks when misused. Only for Can* checks.
type ACheck struct {
	Args
	// TODO close these on dispose and deadline
	CheckDone chan struct{}
	// Was the mutation canceled?
	Canceled bool
}

// -----

// PassMerge merged one or more [A] into [A]. Technically a `map[string]any`
// merge.
func PassMerge(args ...A) A {
	ret := A{}
	for _, set := range args {
		for k, v := range set {
			ret[k] = v
		}
	}

	return ret
}

// Pass accepts pointers of [ArgsBase] to pass to the machine as [A].
func Pass(args ...ArgsApi) A {
	ret := A{}
	for _, a := range args {
		ret[ArgIndex(a)] = a
	}

	return ret
}

// ParseArgs is [ParseArgsCheck] but without the check.
func ParseArgs[G ArgsApi](args A) *G {
	v, _ := ParseArgsCheck[G](args)
	return v
}

// ParseArgsCheck parses [A] into typed arguments described by the passed generic
// type. Returns
func ParseArgsCheck[G ArgsApi](args A) (*G, bool) {
	var ret G
	idx := ArgIndex(ret)
	v, ok := args[idx].(*G)
	if !ok {
		// RPC support
		v2, ok := args[idx].(G)
		if !ok {
			// fallback
			return &ret, false
		}
		v = &v2
	}

	return v, true
}

// ArgIndex return an index of [arg] inside [A].
func ArgIndex(arg ArgsApi) string {
	ns := arg.ArgsPrefix()
	state := arg.ArgsState()
	return ns + "__" + state
}
