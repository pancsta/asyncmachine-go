package machine

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// ///// ///// /////

// ///// PUB UTILS

// ///// ///// /////

// IsActiveTick returns true if the tick represents an active state
// (odd number).
func IsActiveTick(tick uint64) bool {
	return tick%2 == 1
}

// NextActive returns the absolute tick when the state will be active again
// (excluding the currently active tick).
func NextActive(tick uint64) uint64 {
	if IsActiveTick(tick) {
		return tick + 2
	}
	return tick + 1
}

// NextInactive returns the absolute tick when the state will be inactive again
// (excluding the currently inactive tick).
func NextInactive(tick uint64) uint64 {
	if !IsActiveTick(tick) {
		return tick + 2
	}
	return tick + 1
}

// NextActiveIn returns the number of ticks until the state will be active again
// (excluding the currently active tick).
func NextActiveIn(tick uint64) int {
	if IsActiveTick(tick) {
		return 2
	}
	return 1
}

// NextInactiveIn returns the number of ticks until the state will be inactive
// again (excluding the currently inactive tick).
func NextInactiveIn(tick uint64) int {
	if !IsActiveTick(tick) {
		return 2
	}
	return 1
}

// IsQueued returns true if the mutation has been queued, and the result
// represents the queue time it will be processed.
func IsQueued(result Result) bool {
	return result > Canceled
}

// EnvLogLevel returns a log level from an environment variable, AM_LOG by
// default.
func EnvLogLevel(name string) LogLevel {
	if name == "" {
		name = EnvAmLog
	}
	v, _ := strconv.Atoi(os.Getenv(name))

	return LogLevel(v)
}

// TODO prevent using these names as state names
var handlerSuffixes = []string{
	SuffixEnter, SuffixExit, SuffixState, SuffixEnd, StateAny,
}

// IsHandler checks if a method name is a handler method, by returning a state
// name.
func IsHandler(states S, method string) (string, string) {
	if method == StateAny+SuffixEnter || method == StateAny+SuffixState {
		return "", ""
	}

	// suffixes
	for _, suffix := range handlerSuffixes {
		if strings.HasSuffix(method, suffix) && len(method) != len(suffix) &&
			method != StateAny+suffix {
			return method[0 : len(method)-len(suffix)], ""
		}
	}

	// AnyFoo
	if strings.HasPrefix(method, StateAny) && len(method) != len(StateAny) &&
		method != StateAny+SuffixState {

		return method[len(StateAny):], ""
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

// TestMockClock mocks the internal clock of the machine. Only for testing.
func TestMockClock(mach *Machine, clock Clock) {
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

// ///// STATE LIST

// ///// ///// /////

// S (state names) is a string list of state names.
type S []string

// FilterIndex returns a subset of S from specified indexes.
func (s S) FilterIndex(idxs []int) S {
	return IndexToStates(s, idxs)
}

// FilterPrefix returns all the states with a prefix.
func (s S) FilterPrefix(prefix ...string) S {
	ret := S{}
	for _, p := range prefix {
		ret = ret.Add(StatesWithPrefix(p, s))
	}

	return ret
}

// FilterMatch returns all the states matching a regexp.
func (s S) FilterMatch(re *regexp.Regexp) S {
	ret := S{}
	for _, name := range s {
		if re.MatchString(name) {
			ret = append(ret, name)
		}
	}

	return ret
}

// Prefix lists all states with a prefix.
func (s S) Prefix(prefix string) S {
	return StatesPrefix(prefix, s)
}

// Add concatenates multiple state lists into one, removing duplicates.
// Useful for merging lists of states, eg a state group with other states
// involved in a relation.
func (s S) Add(states ...S) S {
	if len(states) == 0 {
		return s
	}

	states = append([]S{s}, states...)
	return slicesUniq(slices.Concat(states...))
}

// Add1 is like [S.Add], but for single state names.
func (s S) Add1(state ...string) S {
	clone := slices.Clone(s)
	states := append(clone, state...)
	return slicesUniq(states)
}

// Delete removes states from all passed state groups.
func (s S) Delete(states ...S) S {
	return SRem(s, states...)
}

// Delete1 is like [S.Delete], but for single state names.
func (s S) Delete1(states ...string) S {
	return SRem(s, states)
}

// Sub returns the states from [s] that are missing in [other] (subtract).
func (s S) Sub(other S) S {
	return StatesDiff(s, other)
}

// Shared returns states present in both S and other (overlap).
func (s S) Shared(other S) S {
	return StatesShared(s, other)
}

// Equal returns true if states1 and states2 are equal, regardless of
// order.
func (s S) Equal(other S) bool {
	return StatesEqual(s, other)
}

// EqualOrder is like [S.Equal], but also checks the order of states.
func (s S) EqualOrder(other S) bool {
	if len(s) != len(other) {
		return false
	}

	for i := range s {
		if s[i] != other[i] {
			return false
		}
	}

	return true
}

// Index returns an index of passed [states]. Unknown states are
// represented by -1.
func (s S) Index(states S) []int {
	return StatesToIndex(s, states)
}

func (s S) Hash() string {
	return Hash(strings.Join(s, ","), 4)
}

// ----- old

// IndexToStates is deprecated, use [S.FilterIndex].
func IndexToStates(index S, states []int) S {
	ret := make(S, len(states))
	for i := range states {
		name := "unknown" + strconv.Itoa(states[i])
		if len(index) > states[i] && states[i] != -1 {
			name = index[states[i]]
		}
		ret[i] = name
	}

	return ret
}

// StatesToIndex is deprecated, use [S.Index].
func StatesToIndex(index S, states S) []int {
	ret := make([]int, len(states))
	for i := range states {
		ret[i] = slices.Index(index, states[i])
	}

	return ret
}

// StatesDiff is deprecated, use [S.Sub].
func StatesDiff(states1 S, states2 S) S {
	// TODO optimize
	return slicesFilter(states1, func(name string, i int) bool {
		return !slices.Contains(states2, name)
	})
}

// StatesShared is deprecated, use [S.Shared].
func StatesShared(states1 S, states2 S) S {
	return slicesFilter(states1, func(name string, i int) bool {
		return slices.Contains(states2, name)
	})
}

// StatesEqual is deprecated, use [S.Equal].
func StatesEqual(states1 S, states2 S) bool {
	return slicesEvery(states1, states2) && slicesEvery(states2, states1)
}

// StatesPrefix is deprecated, use [S.Prefix].
func StatesPrefix(prefix string, states S) S {
	ret := make(S, len(states))
	for i, name := range states {
		ret[i] = prefix + name
	}

	return ret
}

// StatesWithPrefix is deprecated, use [S.FilterPrefix].
func StatesWithPrefix(prefix string, states S) S {
	ret := S{}
	for _, name := range states {
		if strings.HasPrefix(name, prefix) {
			ret = append(ret, name)
		}
	}

	return ret
}

// SAdd is deprecated, use [S.Add].
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

// SRem is deprecated, use [S.Delete].
func SRem(src S, states ...S) S {
	// TODO test
	// TODO move to resolver
	s := slices.Clone(src)
	if len(states) == 0 {
		return s
	}

	for i := 1; i < len(states); i++ {
		for ii := 0; ii < len(states[i]); ii++ {
			s = slicesWithout(s, states[i][ii])
		}
	}

	return s
}

// ///// ///// /////n

// ///// SCHEMA

// ///// ///// /////

// ----- STATE

// State defines a single state of a machine, its properties, relations, and tags.
// nolint:lll
type State struct {
	Auto    bool     `json:"auto,omitempty" yaml:"auto,omitempty" toml:"auto,omitempty"`
	Multi   bool     `json:"multi,omitempty" yaml:"multi,omitempty" toml:"multi,omitempty"`
	Require S        `json:"require,omitempty" yaml:"require,omitempty" toml:"require,omitempty"`
	Add     S        `json:"add,omitempty" yaml:"add,omitempty" toml:"add,omitempty"`
	Remove  S        `json:"remove,omitempty" yaml:"remove,omitempty" toml:"remove,omitempty"`
	After   S        `json:"after,omitempty" yaml:"after,omitempty" toml:"after,omitempty"`
	Tags    []string `json:"tags,omitempty" yaml:"tags,omitempty" toml:"tags,omitempty"`
}

// Extend adds new states to relations of the source state, without
// removing existing ones. Useful for adjusting shared stated to a specific
// machine. Only "true" values for Auto and Multi are applied from [overlay].
//
//	ssS.HandshakeDone: SharedSchema[ssS.HandshakeDone].Extend(
//		am.State{
//			Require: S{ssS.ClientConnected},
//			Remove: S{Exception},
//		}),
func (s State) Extend(overlay State) State {
	return StateAdd(s, overlay)
}

// Set replaces passed relations and properties of the source state.
// Only relations in the overlay state are replaced, the rest is preserved.
// If [overlay] has all fields `nil`, then only [auto] and [multi] get applied.
//
//	 ssS.HandshakeDone: s.Set(false, true, State{
//			Remove: S{"C"},
//		})
func (s State) Set(auto, multi bool, overlay State) State {
	return StateSet(s, auto, multi, overlay)
}

// SetRels is like [State.Set], but inherits properties (Auto, Multi).
func (s State) SetRels(overlay State) State {
	return StateSet(s, s.Auto, s.Multi, overlay)
}

func (s State) Clone() State {
	stateCopy := State{
		Auto:  s.Auto,
		Multi: s.Multi,
	}

	if s.Require != nil {
		stateCopy.Require = slices.Clone(s.Require)
	}
	if s.Add != nil {
		stateCopy.Add = slices.Clone(s.Add)
	}
	if s.Remove != nil {
		stateCopy.Remove = slices.Clone(s.Remove)
	}
	if s.After != nil {
		stateCopy.After = slices.Clone(s.After)
	}
	if s.Tags != nil {
		stateCopy.Tags = slices.Clone(s.Tags)
	}

	return stateCopy
}

func (s State) HasTag(tag string) bool {
	return slices.Contains(s.Tags, tag)
}

// ----- old

// StateAdd is deprecated, use [State.Extend].
func StateAdd(source State, overlay State) State {
	// TODO example
	// TODO test
	s := source.Clone()
	o := overlay.Clone()

	if o.Auto {
		s.Auto = true
	}
	if o.Multi {
		s.Multi = true
	}

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

// StateSet is Deprecated, use [State.Set].
func StateSet(source State, auto, multi bool, overlay State) State {
	// TODO example
	// TODO test
	s := source.Clone()
	o := overlay.Clone()

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

// ----- SCHEMA

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

	return ret.Clone()
}

// Prefix will prefix all state names with [prefix]. removeDups will skip
// overlaps eg "FooFooName" will be "Foo".
func (s Schema) Prefix(
	prefix string, removeDups bool, optAllowlist, optSkiplist S,
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
			if !removeDups || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.After[i] = newName
		}
		for i, r := range s.Add {
			newName := r
			if !removeDups || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.Add[i] = newName
		}
		for i, r := range s.Remove {
			newName := r
			if !removeDups || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.Remove[i] = newName
		}
		for i, r := range s.Require {
			newName := r
			if !removeDups || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.Require[i] = newName
		}

		newName := name
		if !removeDups || !strings.HasPrefix(name, prefix) {
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
					ErrSchema, name, required))
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

// Names is a random-order list of defined state names.
func (s Schema) Names() S {
	return slices.Collect(maps.Keys(s))
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

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

// handler represents a list of transition handler methods.
type handler struct {
	id  string
	num int
	// map-based handler
	isMap bool
	mx    sync.Mutex
	opts  *BindOpts

	// maps
	negotiations map[string]HandlerNegotiation
	finals       map[string]HandlerFinal

	// reflect
	h            any
	methods      *reflect.Value
	methodCache  map[string]reflect.Value
	missingCache map[string]struct{}
}

// BindOpts define how the methods are bound to the state machine. Optional.
type BindOpts struct {
	// Id of this binding, optional.
	Id string
	// method name will have this prefix removed
	MethodPrefixTrim string
	// only states with this prefix
	StatePrefix string
}

type handlerCall struct {
	fn *reflect.Value
	// TODO debug only
	name        string
	negotiation HandlerNegotiation
	final       HandlerFinal
	event       *Event
	timeout     bool
}

func (c *handlerCall) Exec() bool {
	ret := true
	if c.fn != nil {
		callRet := c.fn.Call([]reflect.Value{reflect.ValueOf(c.event)})
		if len(callRet) > 0 {
			// TODO log err?
			ret, _ = callRet[0].Interface().(bool)
		}
	} else if c.negotiation != nil {
		ret = c.negotiation(c.event)
	} else if c.final != nil {
		c.final(c.event)
	} else {
		panic("invalid handler call " + c.name)
	}

	return ret
}

func newHandlerCallStruct(
	e *Event, h *handler, methodName string, m *Machine,
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
	m.log(LogOps, "[handler:%d] %s", h.num, methodName)
	m.currentHandler.Store(methodName)

	// tracers
	m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(m.t.Load(), h.id, methodName)
	}
	m.tracersMx.RUnlock()

	return &handlerCall{
		fn:      &method,
		name:    methodName,
		event:   e,
		timeout: false,
	}
}

func newHandlerCallMap(
	e *Event, h *handler, methodName string, m *Machine,
) *handlerCall {
	//

	isFinal := strings.HasSuffix(methodName, SuffixState) ||
		strings.HasSuffix(methodName, SuffixEnd)

	if isFinal {
		if _, ok := h.finals[methodName]; !ok {
			h.mx.Unlock()
			return nil
		}
	} else {
		if _, ok := h.negotiations[methodName]; !ok {
			h.mx.Unlock()
			return nil
		}
	}
	h.mx.Unlock()

	// call the handler
	m.log(LogOps, "[handler:%d] %s", h.num, methodName)
	m.currentHandler.Store(methodName)

	// tracers
	m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(m.t.Load(), h.id, methodName)
	}
	m.tracersMx.RUnlock()

	call := &handlerCall{
		name:    methodName,
		event:   e,
		timeout: false,
	}

	if isFinal {
		call.final = h.finals[methodName]
	} else {
		call.negotiation = h.negotiations[methodName]
	}

	return call
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// truncateStr with shorten the string and leave a tripedot suffix.
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

// RandId generates a random ID of the given length (defaults to 8).
func randId(strLen int) string {
	if strLen == 0 {
		strLen = 16
	}
	strLen++
	strLen = strLen / 2

	id := make([]byte, strLen)
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

// TODO replace with slices.DeleteFunc
func slicesFilter[S ~[]E, E any](coll S, fn func(item E, i int) bool) S {
	ret := make(S, 0, len(coll))
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

func slicesUniq[T comparable](coll []T) []T {
	if len(coll) == 0 {
		return []T{}
	}
	seen := make(map[T]struct{}, len(coll))
	ret := make([]T, 0, len(coll))
	for _, v := range coll {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			ret = append(ret, v)
		}
	}

	return ret
}

func cloneOptions(opts *Opts) *Opts {
	if opts == nil {
		return &Opts{}
	}

	return &Opts{
		Id:                   opts.Id,
		HandlerTimeout:       opts.HandlerTimeout,
		DontPanicToException: opts.DontPanicToException,
		DontLogStackTrace:    opts.DontLogStackTrace,
		DontLogId:            opts.DontLogId,
		Resolver:             opts.Resolver,
		LogLevel:             opts.LogLevel,
		Tracers:              opts.Tracers,
		LogArgs:              opts.LogArgs,
		QueueLimit:           opts.QueueLimit,
		Parent:               opts.Parent,
		ParentId:             opts.ParentId,
		Tags:                 opts.Tags,
		DetectEval:           opts.DetectEval,
	}
}

func Capitalize(s string) string {
	if s == "" {
		return ""
	}

	// Decode the first character to find out exactly how many bytes it uses
	r, size := utf8.DecodeRuneInString(s)

	// Uppercase the first character, then append the rest of the original string
	return strings.ToUpper(string(r)) + s[size:]
}

// OptArgs will return the first [A] from a list.
func OptArgs(args []A) A {
	if len(args) > 0 {
		return args[0]
	}
	return nil
}

// OptCtx will return the first [context.Context] from a list.
func OptCtx(ctxs []context.Context) context.Context {
	if len(ctxs) > 0 {
		return ctxs[0]
	}
	return nil
}

// OptEv will return the first [*Event] from a list.
func OptEv(events []*Event) *Event {
	if len(events) > 0 {
		return events[0]
	}
	return nil
}

// optBindOpts will return the first [BindOpts] from a list.
func optBindOpts(args []BindOpts) BindOpts {
	if len(args) > 0 {
		return args[0]
	}
	return BindOpts{}
}

// goroutineNum returns the ID of the current goroutine, or 0 if err.
// Numbers are re-assigned, so this is not a bulletproof way of IDing threads.
func goroutineNum() int64 {
	buf := make([]byte, 4024)
	n := runtime.Stack(buf, false)
	stack := string(buf[:n])
	lines := strings.Split(stack, "\n")
	first := strings.Split(lines[0], " ")
	num, _ := strconv.Atoi(first[1])

	return int64(num)
}

func captureStackTrace() string {
	buf := make([]byte, 4024)
	n := runtime.Stack(buf, false)
	stack := string(buf[:n])
	lines := strings.Split(stack, "\n")
	isPanic := strings.Contains(stack, "panic")
	slices.Reverse(lines)

	heads := []string{
		"AddErr", "AddErrState", "Remove", "Remove1", "Add", "Add1", "Set",
	}
	// TODO trim tails start at reflect.Value.Call({
	//  with asyncmachine 2 frames down

	// trim the head, remove junk
	stop := false
	for i, line := range lines {
		if isPanic && strings.HasPrefix(line, "panic(") {
			lines = lines[:i-1]
			break
		}

		for _, head := range heads {
			if strings.Contains("machine.(*Machine)."+line+"(", head) {
				lines = lines[:i-1]
				stop = true
				break
			}
		}
		if stop {
			break
		}
	}
	slices.Reverse(lines)
	join := strings.Join(lines, "\n")

	if filter := os.Getenv(EnvAmTraceFilter); filter != "" {
		join = strings.ReplaceAll(join, filter, "")
	}

	return join
}

// Hash is a general hashing function.
func Hash(in string, l int) string {
	hasher := md5.New()
	hasher.Write([]byte(in))
	if l == 0 {
		l = 6
	}

	hash := hex.EncodeToString(hasher.Sum(nil))
	// short hash
	return hash[:l]
}
