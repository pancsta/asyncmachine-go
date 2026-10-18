package machine

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"strings"
)

// ///// ///// /////

// ///// STATES

// ///// ///// /////

// StatesBase is the base class for schema states definition, providing compiler
// safety and holding order of state names, as well as groups.
type StatesBase struct {
	// TODO rename to SchemaStates

	// TODO Prefix, support prefixing index and struct field vals

	// Exception is the only built-in state and mean a global error. All errors
	// have to [State.Require] the Exception state. If [Machine.PanicToErr] is
	// true, Exception will receive it.
	Exception   string
	names       S
	groups      map[string][]int
	groupsOrder []string
	// prefix for nesting
	prefix string
}

var _ States = &StatesBase{}

// Prefix returns the nesting prefix for this state definition.
func (b *StatesBase) Prefix() string {
	return b.prefix
}

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

// States is the vase interface for schema states. TODO name to SchemaStates?
type States interface {
	// Names returns the state names of the state machine.
	Names() S
	StateGroups() (map[string][]int, []string)
	SetNames(S)
	SetStateGroups(map[string][]int, []string)
}

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

			parseStateNames(value.Elem(), names, field.Name, groups, order, prefix+field.Name)

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

// ///// ///// /////

// ///// SCHEMA

// ///// ///// /////

// Schema is a map of state names to state definitions.
type Schema map[string]State

// Merge merges multiple state structs into one, overriding the previous
// state definitions. No relation-level merging takes place.
func (s Schema) Merge(schemas ...Schema) Schema {
	// TODO mark all-but-last states as Inherited?
	// TODO example
	// TODO test
	// defaults
	l := len(schemas)
	switch l {
	case 0:
		return s
	}

	ret := s.Clone()
	for i := 0; i < l; i++ {
		maps.Copy(ret, schemas[i])
	}

	// rm noname states
	delete(ret, "")

	return ret.Clone()
}

// Prefix will prefix all state names with [prefix]. skipPrefixed will skip
// overlaps eg "FooFooName" will remain "FooName".
func (s Schema) Prefix(
	prefix string, skipPrefixed bool, optAllowlist, optSkiplist S,
) Schema {
	ret := Schema{}
	for name, s := range s {
		s = s.Clone()

		if len(optAllowlist) > 0 && !slices.Contains(optAllowlist, name) {
			continue
		} else if len(optSkiplist) > 0 && slices.Contains(optSkiplist, name) {
			continue
		}

		for i, r := range s.After {
			newName := r
			if !skipPrefixed || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.After[i] = newName
		}
		for i, r := range s.Add {
			newName := r
			if !skipPrefixed || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.Add[i] = newName
		}
		for i, r := range s.Remove {
			newName := r
			if !skipPrefixed || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.Remove[i] = newName
		}
		for i, r := range s.Require {
			newName := r
			if !skipPrefixed || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.Require[i] = newName
		}

		newName := name
		if !skipPrefixed || !strings.HasPrefix(name, prefix) {
			newName = prefix + name
		}

		// build
		ret[newName] = s
	}

	return ret
}

// Clone deep clones the states struct and returns a copy.
func (s Schema) Clone() Schema {
	ret := make(Schema)

	for name, state := range s {
		ret[name] = state.Clone()
	}

	return ret
}

// Parse sanitizes the schema and returns potential errors.
func (s Schema) Parse() (Schema, error) {
	// TODO capitalize states
	// TODO ErrFoo must require Exception

	parsed := s.Clone()
	states := slices.Collect(maps.Keys(s))
	var errs error
	for name, state := range s {

		// avoid self removal
		if slices.Contains(state.Remove, name) {
			state.Remove = slicesWithout(state.Remove, name)
		}

		// don't Remove if in Add
		for _, add := range state.Add {
			if slices.Contains(state.Remove, add) {
				state.Remove = slicesWithout(state.Remove, add)

				// check if exists
			} else if !slices.Contains(states, add) {
				state.Add = slicesWithout(state.Add, add)
			}
		}

		// avoid being after itself
		if slices.Contains(state.After, name) {
			state.After = slicesWithout(state.After, name)
		}

		// detect require-remove conflicts
		for _, required := range state.Require {
			if slices.Contains(state.Remove, required) {
				errs = errors.Join(errs, fmt.Errorf(
					"%w: require-remove conflict for %s to %s",
					ErrSchema, name, required,
				))
			}
		}

		// remove references to non-existing states
		for _, n := range state.Remove {
			if !slices.Contains(states, n) {
				state.Remove = slicesWithout(state.Remove, n)
			}
		}
		for _, n := range state.After {
			if !slices.Contains(states, n) {
				state.After = slicesWithout(state.After, n)
			}
		}
		for _, n := range state.Remove {
			if !slices.Contains(states, n) {
				state.Remove = slicesWithout(state.Remove, n)
			}
		}

		parsed[name] = state
	}

	return parsed, errs
}

// FilterByTag returns a subset schema with states having the given tag.
func (s Schema) FilterByTag(tag string) Schema {
	ret := Schema{}
	for name, state := range s {
		if slices.Contains(state.Tags, tag) {
			ret[name] = state
		}
	}

	return ret
}

// FilterByTagKey returns a subset schema with states having a value tag
// prefixed with [key].
func (s Schema) FilterByTagKey(key string) Schema {
	ret := Schema{}
	for name, state := range s {
		for _, t := range state.Tags {
			if strings.HasPrefix(t, key+":") {
				ret[name] = state
				break
			}
		}
	}

	return ret
}

// Names is a random-order list of defined state names.
func (s Schema) Names() S {
	return slices.Collect(maps.Keys(s))
}

func (s Schema) AdjacentStates(state string) S {
	if _, ok := s[state]; !ok {
		return nil
	}
	data := s[state]

	return slices.Concat(data.Add, data.After, data.Require, data.Remove)
}

func (s Schema) Nest(prefix string, states S) Schema {

	ret := Schema{}

	for _, state := range states {
		st := s[state]
		st.Require = prefixRels(prefix, states, st.Require)
		st.Add = prefixRels(prefix, states, st.Add)
		st.Remove = prefixRels(prefix, states, st.Remove)
		st.After = prefixRels(prefix, states, st.After)
		ret[prefix+state] = st
	}

	return ret
}

func prefixRels(prefix string, states, rels S) S {
	if rels == nil {
		return nil
	}
	res := make(S, len(rels))
	for i, rel := range rels {
		// prefix only the states from this schema
		if !slices.Contains(states, rel) {
			res[i] = rel
			continue
		}

		res[i] = prefix + rel
	}

	return res
}

// ----- old

// SchemaMerge is deprecated, use [Schema.Merge].
func SchemaMerge(schemas ...Schema) Schema {
	l := len(schemas)
	switch l {
	case 0:
		return Schema{}
	case 1:
		return schemas[0]
	}

	ret := make(Schema)
	for i := 0; i < l; i++ {
		// TODO clone?
		maps.Copy(ret, schemas[i])
	}

	return ret.Clone()
}

// StatesByTag is deprecated, use
//
//	Schema.FilterByTag("").Names()
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
	// ArgsPrefix returns the argument prefix inside the [A] map. Should be
	// provided by the implementation.
	ArgsPrefix() string
	// ArgsState represents the state this argument struct belongs to.
	// Defaults to [StateAny].
	ArgsState() string
	// TODO deep clone interface, optional
	// ArgsClone() G
}

// ArgsBase is the base implementation of [ArgsApi] interface.
type ArgsBase struct {
	ArgsApi
}

// ArgsPrefix is a fallback to a stack trace hash of the implementation
// symbol. Each
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

// ParseArgsCheck parses [A] into typed arguments described by the
// passed generic
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

// TODO
// func (e *Event) ArgsState(state string, def any) any {
// }
