package machine

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
)

type (
	InternalLogFunc   func(level LogLevel, msg string, args ...any)
	InternalCheckFunc func(states S) bool
)

// Subscriptions is an embed responsible for binding subscriptions,
// managing their indexes, processing triggers, and garbage collection.
type Subscriptions struct {
	// Mx locks the subscription manager. TODO optimize?
	Mx sync.Mutex

	mach  Api
	clock Clock
	is    InternalCheckFunc
	not   InternalCheckFunc
	log   InternalLogFunc

	stateCtx IndexStateCtx

	when         IndexWhen
	whenCtx      map[context.Context][]*WhenBinding
	whenTime     IndexWhenTime
	whenTimeCtx  map[context.Context][]*WhenTimeBinding
	whenArgs     IndexWhenArgs
	whenArgsCtx  map[context.Context][]*WhenArgsBinding
	whenQuery    []*whenQueryBinding
	whenQueryCtx map[context.Context][]*whenQueryBinding

	whenQueueEnds []*whenQueueEndsBinding
	whenQueue     []*whenQueueBinding
}

func NewSubscriptionManager(
	mach Api, clock Clock, is, not InternalCheckFunc, log InternalLogFunc,
) *Subscriptions {
	return &Subscriptions{
		mach:  mach,
		clock: clock,
		is:    is,
		not:   not,
		log:   log,

		when:        IndexWhen{},
		whenTime:    IndexWhenTime{},
		whenArgs:    IndexWhenArgs{},
		stateCtx:    IndexStateCtx{},
		whenCtx:     map[context.Context][]*WhenBinding{},
		whenTimeCtx: map[context.Context][]*WhenTimeBinding{},
		whenArgsCtx: map[context.Context][]*WhenArgsBinding{},
	}
}

// ///// ///// /////

// ///// PROCESSING

// ///// ///// /////

// ProcessStateCtx collects all deactivated state contexts, and returns theirs
// cancel funcs. Uses transition caches.
func (sm *Subscriptions) ProcessStateCtx(deactivated S) []context.CancelFunc {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	var toCancel []context.CancelFunc
	for _, s := range deactivated {
		if _, ok := sm.stateCtx[s]; !ok {
			continue
		}

		toCancel = append(toCancel, sm.stateCtx[s].Cancel)
		sm.log(LogOps, "[ctx:match] %s", s)
		delete(sm.stateCtx, s)
	}

	return toCancel
}

// ProcessWhen collects all the matched active state subscriptions, and
// returns theirs channels.
func (sm *Subscriptions) ProcessWhen(activated, deactivated S) []chan struct{} {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	// collect ctx expirations
	// TODO optimize by skipping
	ret := sm.processWhenCtx()

	// collect matched bindings
	all := slices.Concat(activated, deactivated)
	for _, s := range all {
		// TODO optimize clone
		for _, binding := range slices.Clone(sm.when[s]) {

			if slices.Contains(activated, s) {

				// state activated, check the index
				if !binding.Negation {
					// match for When(
					if !binding.States[s] {
						binding.Matched++
					}
				} else {
					// match for WhenNot(
					if !binding.States[s] {
						binding.Matched--
					}
				}

				// update index: mark as active
				binding.States[s] = true
			} else {

				// state deactivated
				if !binding.Negation {
					// match for When(
					if binding.States[s] {
						binding.Matched--
					}
				} else {
					// match for WhenNot(
					if binding.States[s] {
						binding.Matched++
					}
				}

				// update index: mark as inactive
				binding.States[s] = false
			}

			// if not all matched, ignore for now
			expired := binding.Ctx != nil && binding.Ctx.Err() != nil
			if binding.Matched < binding.Total && !expired {
				continue
			}

			// completed - rm binding and collect ch
			sm.gcWhenBinding(binding, true)
			ret = append(ret, binding.Ch)
		}
	}

	return ret
}

func (sm *Subscriptions) processWhenCtx() []chan struct{} {
	var ret []chan struct{}

	// find expired ctxs
	for ctx, bindings := range sm.whenCtx {
		if ctx.Err() == nil {
			continue
		}

		// delete the ctx and all the bindings
		delete(sm.whenCtx, ctx)
		for _, binding := range bindings {
			sm.gcWhenBinding(binding, false)
		}
	}

	return ret
}

func (sm *Subscriptions) gcWhenBinding(binding *WhenBinding, gcCtx bool) {
	// completed - close and delete indexes for all involved states
	var names []string
	for state := range binding.States {
		// remove GC ctx
		if binding.Ctx != nil && gcCtx {
			sm.whenCtx[binding.Ctx] = slicesWithout(
				sm.whenCtx[binding.Ctx], binding)

			if len(sm.whenCtx[binding.Ctx]) == 0 {
				delete(sm.whenCtx, binding.Ctx)
			}
		}

		// delete state index
		names = append(names, state)
		if len(sm.when[state]) == 1 {
			delete(sm.when, state)
			continue
		}

		// delete with a lookup TODO optimize, GC later
		sm.when[state] = slicesWithout(sm.when[state], binding)
	}

	// log TODO sem logger
	if binding.Negation {
		sm.log(LogOps, "[whenNot:match] %s", j(names))
	} else {
		sm.log(LogOps, "[when:match] %s", j(names))
	}
}

// ProcessWhenTime collects all the time-based subscriptions, and
// returns theirs channels.
func (sm *Subscriptions) ProcessWhenTime(before Clock) []chan struct{} {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	// collect ctx expirations
	// TODO optimize by skipping
	ret := sm.processWhenTimeCtx()

	// collect all the ticked states
	// TODO optimize?
	allTicked := S{}
	for state, t := range before {
		// if changed, collect to check
		if sm.clock[state] != t {
			allTicked = append(allTicked, state)
		}
	}

	// check all the bindings for all the ticked states
	for _, s := range allTicked {
		// TODO optimize clone
		for _, binding := range slices.Clone(sm.whenTime[s]) {

			// check if the requested time has passed
			if !binding.Completed[s] &&
				sm.clock[s] >= binding.Times[binding.Index[s]] {
				binding.Matched++
				// mark in the index as completed
				binding.Completed[s] = true
			}

			// if not all matched, ignore for now
			expired := binding.Ctx != nil && binding.Ctx.Err() != nil
			if binding.Matched < binding.Total && !expired {
				continue
			}

			// completed - rm binding and collect ch
			sm.gcWhenTimeBinding(binding, true)
			ret = append(ret, binding.Ch)
		}
	}

	return ret
}

func (sm *Subscriptions) processWhenTimeCtx() []chan struct{} {
	var ret []chan struct{}

	// find expired ctxs
	for ctx, bindings := range sm.whenTimeCtx {
		if ctx.Err() == nil {
			continue
		}

		// delete the ctx and all the bindings
		delete(sm.whenTimeCtx, ctx)
		for _, binding := range bindings {
			sm.gcWhenTimeBinding(binding, false)
		}
	}

	return ret
}

func (sm *Subscriptions) gcWhenTimeBinding(
	binding *WhenTimeBinding, gcCtx bool,
) {
	// completed - close and delete indexes for all involved states
	var names []string
	for state := range binding.Index {
		// remove GC ctx
		if binding.Ctx != nil && gcCtx {
			sm.whenTimeCtx[binding.Ctx] = slicesWithout(
				sm.whenTimeCtx[binding.Ctx], binding)

			if len(sm.whenTimeCtx[binding.Ctx]) == 0 {
				delete(sm.whenTimeCtx, binding.Ctx)
			}
		}

		// remove state index
		names = append(names, state)
		if len(sm.whenTime[state]) == 1 {
			delete(sm.whenTime, state)
			continue
		}

		// delete with a lookup
		sm.whenTime[state] = slicesWithout(sm.whenTime[state], binding)
	}

	// log TODO sem logger
	sm.log(LogOps, "[whenTime:match] %s %d", j(names), binding.Times)
}

// ProcessWhenArgs collects all the args-matching subscriptions, and
// returns theirs channels.
func (sm *Subscriptions) ProcessWhenArgs(e *Event) []chan struct{} {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	// collect ctx expirations
	// TODO optimize by skipping
	ret := sm.processWhenArgsCtx()

	// collect arg matches
	for _, binding := range slices.Clone(sm.whenArgs[e.Name]) {
		expired := binding.ctx != nil && binding.ctx.Err() != nil
		// TODO better comparison
		if !compareArgs(e.Args, binding.args) && !expired {
			continue
		}

		// completed - rm binding and collect ch
		sm.gcWhenArgsBinding(binding, true)
		ret = append(ret, binding.ch)
	}

	return ret
}

func (sm *Subscriptions) processWhenArgsCtx() []chan struct{} {
	var ret []chan struct{}

	// find expired ctxs
	for ctx, bindings := range sm.whenArgsCtx {
		if ctx.Err() == nil {
			continue
		}

		// delete the ctx and all the bindings
		delete(sm.whenArgsCtx, ctx)
		for _, binding := range bindings {
			sm.gcWhenArgsBinding(binding, false)
		}
	}

	return ret
}

func (sm *Subscriptions) gcWhenArgsBinding(
	binding *WhenArgsBinding, gcCtx bool,
) {
	// remove GC ctx
	if binding.ctx != nil && gcCtx {
		sm.whenArgsCtx[binding.ctx] = slicesWithout(
			sm.whenArgsCtx[binding.ctx], binding)

		if len(sm.whenArgsCtx[binding.ctx]) == 0 {
			delete(sm.whenArgsCtx, binding.ctx)
		}
	}

	// GC
	if len(sm.whenArgs[binding.handler]) == 1 {
		delete(sm.whenArgs, binding.handler)
	} else {
		sm.whenArgs[binding.handler] = slicesWithout(
			sm.whenArgs[binding.handler], binding)
	}

	// log TODO sem logger
	argNames := jw(slices.Collect(maps.Keys(binding.args)), ",")
	// FooState -> Foo
	name, _ := strings.CutSuffix(binding.handler, SuffixState)
	sm.log(LogOps, "[whenArgs:match] %s (%s)", name, argNames)
}

func (sm *Subscriptions) ProcessWhenQuery() []chan struct{} {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	// collect ctx expirations
	// TODO optimize by skipping
	ret := sm.processWhenQueryCtx()

	// collect arg matches TODO optimize clone
	for _, binding := range slices.Clone(sm.whenQuery) {
		expired := binding.ctx != nil && binding.ctx.Err() != nil
		if !binding.fn(sm.clock) && !expired {
			continue
		}

		// completed - rm binding and collect ch
		sm.gcWhenQueryBinding(binding, true)
		ret = append(ret, binding.ch)
	}

	return ret
}

func (sm *Subscriptions) processWhenQueryCtx() []chan struct{} {
	var ret []chan struct{}

	// find expired ctxs
	for ctx, bindings := range sm.whenQueryCtx {
		if ctx.Err() == nil {
			continue
		}

		// delete the ctx and all the bindings
		delete(sm.whenArgsCtx, ctx)
		for _, binding := range bindings {
			sm.gcWhenQueryBinding(binding, false)
		}
	}

	return ret
}

func (sm *Subscriptions) gcWhenQueryBinding(
	binding *whenQueryBinding, gcCtx bool,
) {
	// remove GC ctx
	if binding.ctx != nil && gcCtx {
		sm.whenQueryCtx[binding.ctx] = slicesWithout(
			sm.whenQueryCtx[binding.ctx], binding)

		if len(sm.whenQueryCtx[binding.ctx]) == 0 {
			delete(sm.whenQueryCtx, binding.ctx)
		}
	}

	// GC
	idx := 0
	if len(sm.whenQuery) == 1 {
		sm.whenQuery = nil
	} else {
		idx = slices.Index(sm.whenQuery, binding)
		sm.whenQuery = slices.Delete(sm.whenQuery, idx, idx+1)
	}

	// log TODO sem logger
	sm.log(LogOps, "[whenQuery:match] %d", idx)
}

// ProcessWhenQueueEnds collects all queue-end subscriptions, and
// returns theirs channels.
func (sm *Subscriptions) ProcessWhenQueueEnds() []chan struct{} {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	// collect chans
	ret := make([]chan struct{}, len(sm.whenQueueEnds))
	for i, binding := range sm.whenQueueEnds {
		ret[i] = binding.ch
	}

	// clean up
	sm.whenQueueEnds = nil

	return ret
}

func (sm *Subscriptions) ProcessWhenQueue(queueTick uint64) []chan struct{} {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	// collect
	var toClose []chan struct{}
	var toCloseIdx []int
	for i, binding := range sm.whenQueue {
		if uint64(binding.tick) > queueTick {
			continue
		}

		// TODO sem logger
		sm.log(LogOps, "[whenQueue:match] %d", binding.tick)
		toClose = append(toClose, binding.ch)
		toCloseIdx = append(toCloseIdx, i)
	}

	// GC
	slices.Reverse(toCloseIdx)
	for _, idx := range toCloseIdx {
		sm.whenQueue = slices.Delete(sm.whenQueue, idx, idx+1)
	}

	// close and execute waits
	return toClose
}

// ///// ///// /////

// ///// BINDING

// ///// ///// /////

// NewStateCtx returns a new sub-context, bound to the current clock's tick of
// the passed state.
//
// Context cancels when the state has been deactivated, or right away,
// if it isn't currently active.
//
// State contexts are used to check state expirations and should be checked
// often inside goroutines.
func (sm *Subscriptions) NewStateCtx(state string) context.Context {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	if _, ok := sm.stateCtx[state]; ok {
		return sm.stateCtx[state].Ctx
	}

	// store a fingerprint
	v := CtxValue{
		Id:    sm.mach.Id(),
		State: state,
		Tick:  sm.clock[state],
	}
	stateCtx, cancel := context.WithCancel(context.WithValue(sm.mach.Ctx(),
		CtxKey, v))

	// cancel early
	if !sm.is(S{state}) {
		// TODO decision msg
		cancel()
		return stateCtx
	}

	binding := &CtxBinding{
		Ctx:    stateCtx,
		Cancel: cancel,
	}

	// add an index
	sm.stateCtx[state] = binding
	sm.log(LogOps, "[ctx:new] %s", state)

	return stateCtx
}

func (sm *Subscriptions) When(states S, ctx context.Context) <-chan struct{} {
	// TODO re-use channels with the same state set and context

	// if all active, close early
	if sm.is(states) {
		return newClosedChan()
	}

	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	ch := make(chan struct{})

	setMap := StateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = sm.is(S{s})
		if setMap[s] {
			matched++
		}
	}

	// add the binding to an index of each state
	binding := &WhenBinding{
		Ch:       ch,
		Negation: false,
		States:   setMap,
		Total:    len(states),
		Matched:  matched,
		Ctx:      ctx,
	}
	sm.log(LogOps, "[when:new] %s", j(states))

	// insert the binding
	for _, s := range states {
		sm.when[s] = append(sm.when[s], binding)

		if ctx != nil {
			sm.whenCtx[ctx] = append(sm.whenCtx[ctx], binding)
		}
	}

	return ch
}

// WhenNot returns a channel that will be closed when all the passed states
// become inactive or the machine gets disposed.
//
// ctx: optional context that will close the channel early.
func (sm *Subscriptions) WhenNot(
	states S, ctx context.Context,
) <-chan struct{} {
	// if all inactive, close early
	if sm.not(states) {
		return newClosedChan()
	}

	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	ch := make(chan struct{})
	setMap := StateIsActive{}
	matched := 0
	for _, s := range states {
		setMap[s] = sm.is(S{s})
		if !setMap[s] {
			matched++
		}
	}

	// add the binding to an index of each state
	binding := &WhenBinding{
		Ch:       ch,
		Negation: true,
		States:   setMap,
		Total:    len(states),
		Matched:  matched,
		Ctx:      ctx,
	}
	sm.log(LogOps, "[whenNot:new] %s", j(states))

	// insert the binding
	for _, s := range states {
		if _, ok := sm.when[s]; !ok {
			sm.when[s] = []*WhenBinding{binding}
		} else {
			sm.when[s] = append(sm.when[s], binding)
		}
	}
	if ctx != nil {
		sm.whenCtx[ctx] = append(sm.whenCtx[ctx], binding)
	}

	return ch
}

// WhenArgs returns a channel that will be closed when the passed state
// becomes active with all the passed args. Args are compared using the native
// '=='. It's meant to be used with async Multi states, to filter out
// a specific call.
//
// ctx: optional context that will close the channel when handler loop ends.
func (sm *Subscriptions) WhenArgs(
	state string, args A, ctx context.Context,
) <-chan struct{} {
	// TODO better val comparisons
	//  support regexp for strings
	// TODO support typed args
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	ch := make(chan struct{})
	handler := state + SuffixState

	// log TODO pass through logArgs?
	argNames := jw(slices.Collect(maps.Keys(args)), ",")
	sm.log(LogOps, "[whenArgs:new] %s (%s)", state, argNames)

	// try to reuse an existing channel
	for _, binding := range sm.whenArgs[handler] {
		if compareArgs(binding.args, args) {
			return binding.ch
		}
	}

	binding := &WhenArgsBinding{
		ch:      ch,
		handler: handler,
		args:    args,
		ctx:     ctx,
	}

	// insert the binding
	sm.whenArgs[handler] = append(sm.whenArgs[handler], binding)

	if ctx != nil {
		sm.whenArgsCtx[ctx] = append(sm.whenArgsCtx[ctx], binding)
	}

	return ch
}

// WhenTime returns a channel that will be closed when all the passed states
// have passed the specified time. The time is a logical clock of the state.
// Machine time can be sourced from [Machine.Time](), or [Machine.Clock]().
//
// ctx: optional context that will close the channel early.
func (sm *Subscriptions) WhenTime(
	states S, times Time, ctx context.Context,
) <-chan struct{} {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	ch := make(chan struct{})
	indexWhenTime := sm.whenTime

	// if all times passed, close early
	passed := true
	for i, s := range states {
		if sm.clock[s] < times[i] {
			passed = false
			break
		}
	}
	if passed {
		// TODO decision msg
		close(ch)
		return ch
	}

	completed := StateIsActive{}
	matched := 0
	index := map[string]int{}
	for i, s := range states {
		completed[s] = sm.clock[s] >= times[i]
		if completed[s] {
			matched++
		}
		index[s] = i
	}

	// add the binding to an index of each state
	binding := &WhenTimeBinding{
		Ch:        ch,
		Index:     index,
		Completed: completed,
		Total:     len(states),
		Matched:   matched,
		Times:     times,
		Ctx:       ctx,
	}
	sm.log(LogOps, "[whenTime:new] %s %s", jw(states, ","), times)

	// insert the binding
	for _, s := range states {
		indexWhenTime[s] = append(indexWhenTime[s], binding)
	}
	if ctx != nil {
		sm.whenTimeCtx[ctx] = append(sm.whenTimeCtx[ctx], binding)
	}

	return ch
}

func (sm *Subscriptions) WhenQuery(
	fn func(clock Clock) bool, ctx context.Context,
) <-chan struct{} {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	// add the binding to an index of each state
	ch := make(chan struct{})
	binding := &whenQueryBinding{
		ch:  ch,
		ctx: ctx,
		fn:  fn,
	}

	// insert the binding
	sm.log(LogOps, "[whenQuery:new] %d", len(sm.whenQuery))
	sm.whenQuery = append(sm.whenQuery, binding)
	if ctx != nil {
		sm.whenQueryCtx[ctx] = append(sm.whenQueryCtx[ctx], binding)
	}

	return ch
}

// WhenQueueEnds closes every time the queue ends, or the optional ctx expires.
// This function assumes the queue is running, and wont close early.
//
// ctx: optional context that will close the channel early.
func (sm *Subscriptions) WhenQueueEnds(
	ctx context.Context, mx *sync.RWMutex,
) <-chan struct{} {
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	// add the binding to an index of each state
	ch := make(chan struct{})
	binding := &whenQueueEndsBinding{
		ch:  ch,
		ctx: ctx,
	}

	// insert the binding
	sm.whenQueueEnds = append(sm.whenQueueEnds, binding)

	// TODO remove ctx
	// if ctx != nil {
	// 	// fork in this special case
	// 	go func() {
	// 		<-ctx.Done()
	// 		mx.Lock()
	// 		defer mx.Unlock()
	// 		sm.whenQueueEnds = slicesWithout(sm.whenQueueEnds, binding)
	// 		close(ch)
	// 	}()
	// }

	return ch
}

// WhenQueue waits until the passed queueTick gets processed.
func (sm *Subscriptions) WhenQueue(tick Result) <-chan struct{} {
	// TODO add gc ctx (just in case)
	// locks
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	// add the binding to an index of each state
	ch := make(chan struct{})
	binding := &whenQueueBinding{
		ch:   ch,
		tick: tick,
	}
	sm.log(LogOps, "[whenQueue:new] %d", tick)

	// insert the binding
	sm.whenQueue = append(sm.whenQueue, binding)

	return ch
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

func (sm *Subscriptions) HasWhenArgs() bool {
	sm.Mx.Lock()
	defer sm.Mx.Unlock()

	return len(sm.whenArgs) > 0
}

func (sm *Subscriptions) dispose() {
	// cancel ctx
	for _, binding := range sm.stateCtx {
		binding.Cancel()
	}

	// close channels
	for state := range sm.when {
		for _, binding := range sm.when[state] {
			closeSafe(binding.Ch)
		}
	}
	for state := range sm.whenTime {
		for _, binding := range sm.whenTime[state] {
			closeSafe(binding.Ch)
		}
	}
	for state := range sm.whenArgs {
		for _, binding := range sm.whenArgs[state] {
			closeSafe(binding.ch)
		}
	}
	for _, bind := range sm.whenQueueEnds {
		closeSafe(bind.ch)
	}
	for _, bind := range sm.whenQueue {
		closeSafe(bind.ch)
	}
}
