package machine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// ///// ///// /////

// ///// PUB UTILS

// ///// ///// /////

// DiffStates returns the states that are in states1 but not in states2.
func DiffStates(states1 S, states2 S) S {
	return slicesFilter(states1, func(name string, i int) bool {
		return !slices.Contains(states2, name)
	})
}

func StatesEqual(states1 S, states2 S) bool {
	return slicesEvery(states1, states2) && slicesEvery(states2, states1)
}

// IsTimeAfter checks if time1 is after time2. Requires a deterministic states
// order, e.g. by using Machine.VerifyStates.
func IsTimeAfter(time1, time2 Time) bool {
	after := false
	for i, t1 := range time1 {
		if t1 < time2[i] {
			return false
		}
		if t1 > time2[i] {
			after = true
		}
	}
	return after
}

// CloneStates deep clones the states struct and returns a copy.
func CloneStates(stateStruct Struct) Struct {
	ret := make(Struct)

	for name, state := range stateStruct {
		ret[name] = cloneState(state)
	}

	return ret
}

func cloneState(state State) State {
	stateCopy := State{
		Auto:  state.Auto,
		Multi: state.Multi,
	}

	// TODO move to Resolver

	if state.Require != nil {
		stateCopy.Require = slices.Clone(state.Require)
	}
	if state.Add != nil {
		stateCopy.Add = slices.Clone(state.Add)
	}
	if state.Remove != nil {
		stateCopy.Remove = slices.Clone(state.Remove)
	}
	if state.After != nil {
		stateCopy.After = slices.Clone(state.After)
	}

	return stateCopy
}

// IsActiveTick returns true if the tick represents an active state
// (odd number).
func IsActiveTick(tick uint64) bool {
	return tick%2 == 1
}

// SAdd concatenates multiple state lists into one, removing duplicates.
// Useful for merging lists of states, eg a state group with other states
// involved in a relation.
func SAdd(states ...S) S {
	// TODO test
	// TODO move to resolver
	if len(states) == 0 {
		return S{}
	}

	s := slices.Clone(states[0])
	for i := 1; i < len(states); i++ {
		s = append(s, states[i]...)
	}

	return slicesUniq(s)
}

// StateAdd adds new states to relations of the source state, without
// removing existing ones. Useful for adjusting shared stated to a specific
// machine.
func StateAdd(source State, overlay State) State {
	// TODO example
	// TODO test
	// TODO move to resolver?
	s := cloneState(source)
	o := cloneState(overlay)

	// relations
	if o.Add != nil {
		s.Add = SAdd(s.Add, o.Add)
	}
	if o.Remove != nil {
		s.Remove = SAdd(s.Remove, o.Remove)
	}
	if o.Require != nil {
		s.Require = SAdd(s.Require, o.Require)
	}
	if o.After != nil {
		s.After = SAdd(s.After, o.After)
	}

	return s
}

// StateSet replaces passed relations and properties of the source state.
// Only relations in the overlay state are replaced, the rest is preserved.
// If [overlay] has all fields `nil`, then only [auto] and [multi] get applied.
func StateSet(source State, auto, multi bool, overlay State) State {
	// TODO example
	// TODO test
	// TODO move to resolver?
	s := cloneState(source)
	o := cloneState(overlay)

	// properties
	s.Auto = auto
	s.Multi = multi

	// relations
	if o.Add != nil {
		s.Add = o.Add
	}
	if o.Remove != nil {
		s.Remove = o.Remove
	}
	if o.Require != nil {
		s.Require = o.Require
	}
	if o.After != nil {
		s.After = o.After
	}

	return s
}

// StructMerge merges multiple state structs into one, overriding the previous
// state definitions. No relation-level merging takes place.
func StructMerge(stateStructs ...Struct) Struct {
	// TODO example
	// TODO test
	// TODO move to resolver?
	// defaults
	l := len(stateStructs)
	if l == 0 {
		return Struct{}
	} else if l == 1 {
		return stateStructs[0]
	}

	ret := make(Struct)
	for i := 0; i < l; i++ {
		maps.Copy(ret, stateStructs[i])
	}

	return CloneStates(ret)
}

// Serialized is a machine state serialized to a JSON/YAML/TOML compatible
// struct. One also needs the state Struct to re-create a state machine.
type Serialized struct {
	// ID is the ID of a state machine.
	ID string `json:"id" yaml:"id"`
	// Time represents machine time - a list of state activation counters.
	Time Time `json:"time" yaml:"time"`
	// StateNames is an ordered list of state names.
	StateNames S `json:"state_names" yaml:"state_names" toml:"state_names"`
}

// EnvLogLevel returns a log level from an environment variable, AM_LOG by
// default.
func EnvLogLevel(name string) LogLevel {
	// TODO cookbook
	if name == "" {
		name = EnvAmLog
	}
	v, _ := strconv.Atoi(os.Getenv(name))
	return LogLevel(v)
}

// ListHandlers returns a list of handler method names from a handlers struct.
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

		if s1 != "" || method == HandlerGlobal {
			methodNames = append(methodNames, method)
			// TODO verify method signatures early (returns and params)
		}
	}

	t := reflect.TypeOf(handlers)
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i).Name
		check(method)
	}

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

// TODO prevent using these names as state names
var handlerSuffixes = []string{
	SuffixEnter, SuffixExit, SuffixState, SuffixEnd, Any,
}

// IsHandler checks if a method name is a handler method, by returning a state
// name.
func IsHandler(states S, method string) (string, string) {
	if method == HandlerGlobal {
		return "", ""
	}

	// suffixes
	for _, suffix := range handlerSuffixes {
		if strings.HasSuffix(method, suffix) && len(method) != len(suffix) &&
			method != Any+suffix {
			return method[0 : len(method)-len(suffix)], ""
		}
	}

	// AnyFoo
	if strings.HasPrefix(method, Any) && len(method) != len(Any) &&
		method != Any+SuffixState {
		return method[len(Any):], ""
	}

	// FooBar
	for _, s := range states {
		if !strings.HasPrefix(method, s) {
			continue
		}

		for _, ss := range states {
			if s+ss == method {
				return s, ss
			}
		}
	}

	return "", ""
}

// MockClock mocks the internal clock of the machine. Only for testing.
func MockClock(mach *Machine, clock Clock) {
	mach.clock = clock
}

// AMerge merges 2 or more maps into 1. Useful for passing args from many
// packages.
func AMerge[K comparable, V any](maps ...map[K]V) map[K]V {
	out := map[K]V{}

	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}

	return out
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// j joins state names into a single string
func j(states []string) string {
	return strings.Join(states, " ")
}

// jw joins state names into a single string with a separator.
func jw(states []string, sep string) string {
	return strings.Join(states, sep)
}

func closeSafe[T any](ch chan T) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func padString(str string, length int, pad string) string {
	for {
		str += pad
		if len(str) > length {
			return str[0:length]
		}
	}
}

func parseStruct(states Struct) Struct {
	// TODO move to Resolver
	// TODO capitalize states

	parsedStates := CloneStates(states)
	for name, state := range states {

		// avoid self removal
		if slices.Contains(state.Remove, name) {
			state.Remove = slicesWithout(state.Remove, name)
		}

		// don't Remove if in Add
		for _, add := range state.Add {
			if slices.Contains(state.Remove, add) {
				state.Remove = slicesWithout(state.Remove, add)
			}
		}

		// avoid being after itself
		if slices.Contains(state.After, name) {
			state.After = slicesWithout(state.After, name)
		}

		parsedStates[name] = state
	}

	return parsedStates
}

// compareArgs return true if args2 is a subset of args1.
func compareArgs(args1, args2 A) bool {
	match := true

	for k, v := range args2 {
		// TODO better comparisons
		if args1[k] != v {
			match = false
			break
		}
	}

	return match
}

func truncateStr(s string, maxLength int) string {
	if len(s) <= maxLength {
		return s
	}
	if maxLength < 5 {
		return s[:maxLength]
	} else {
		return s[:maxLength-3] + "..."
	}
}

type handlerCall struct {
	fn reflect.Value
	// TODO debug only
	name    string
	event   *Event
	timeout bool
}

func randId() string {
	id := make([]byte, 16)
	_, err := rand.Read(id)
	if err != nil {
		return "error"
	}

	return hex.EncodeToString(id)
}

func slicesWithout[S ~[]E, E comparable](coll S, el E) S {
	idx := slices.Index(coll, el)
	ret := slices.Clone(coll)
	if idx == -1 {
		return ret
	}
	return slices.Delete(ret, idx, idx+1)
}

// slicesNone returns true if none of the elements of coll2 are in coll1.
func slicesNone[S1 ~[]E, S2 ~[]E, E comparable](col1 S1, col2 S2) bool {
	for _, el := range col2 {
		if slices.Contains(col1, el) {
			return false
		}
	}
	return true
}

// slicesEvery returns true if all elements of coll2 are in coll1.
func slicesEvery[S1 ~[]E, S2 ~[]E, E comparable](col1 S1, col2 S2) bool {
	for _, el := range col2 {
		if !slices.Contains(col1, el) {
			return false
		}
	}
	return true
}

func slicesFilter[S ~[]E, E any](coll S, fn func(item E, i int) bool) S {
	var ret S
	for i, el := range coll {
		if fn(el, i) {
			ret = append(ret, el)
		}
	}
	return ret
}

func slicesReverse[S ~[]E, E any](coll S) S {
	ret := make(S, len(coll))
	for i := range coll {
		ret[i] = coll[len(coll)-1-i]
	}
	return ret
}

func slicesUniq[S ~[]E, E comparable](coll S) S {
	dups := false
	l := len(coll)
	for i, el := range coll {
		for ii := i; ii < len(coll)-1; ii++ {
			if l > ii+1 && el == coll[ii+1] {
				dups = true
				break
			}
		}
	}
	if !dups {
		// return slices.Clone(coll)
		return coll
	}
	var ret S
	for _, el := range coll {
		if !slices.Contains(ret, el) {
			ret = append(ret, el)
		}
	}
	return ret
}

// disposeWithCtx handles early binding disposal caused by a canceled context.
// It's used by most of "when" methods.
// TODO GC in the handler loop instead
func disposeWithCtx[T comparable](
	mach *Machine, ctx context.Context, ch chan struct{}, states S, binding T,
	lock *sync.RWMutex, index map[string][]T, logMsg string,
) {
	// TODO groups waiting on the same context
	// TODO close using the handler loop?
	if ctx == nil {
		return
	}
	go func() {
		select {
		case <-ch:
			return
		case <-mach.ctx.Done():
			return
		case <-ctx.Done():
		}

		// TODO track
		closeSafe(ch)

		// GC only if needed
		if mach.disposed.Load() {
			return
		}
		lock.Lock()
		defer lock.Unlock()

		for _, s := range states {
			if _, ok := index[s]; ok {
				if len(index[s]) == 1 {
					delete(index, s)
				} else {
					index[s] = slicesWithout(index[s], binding)
				}

				if logMsg != "" {
					mach.log(LogOps, logMsg)
				}
			}
		}
	}()
}

func cloneOptions(opts *Opts) *Opts {
	if opts == nil {
		return &Opts{}
	}

	return &Opts{
		ID:                   opts.ID,
		HandlerTimeout:       opts.HandlerTimeout,
		DontPanicToException: opts.DontPanicToException,
		DontLogStackTrace:    opts.DontLogStackTrace,
		DontLogID:            opts.DontLogID,
		Resolver:             opts.Resolver,
		LogLevel:             opts.LogLevel,
		Tracers:              opts.Tracers,
		LogArgs:              opts.LogArgs,
		QueueLimit:           opts.QueueLimit,
		Parent:               opts.Parent,
		ParentID:             opts.ParentID,
		Tags:                 opts.Tags,
		DetectEval:           opts.DetectEval,
	}
}
