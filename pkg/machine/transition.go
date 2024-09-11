package machine

import (
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// Transition represents processing of a single mutation within a machine.
type Transition struct {
	// ID is a unique identifier of the transition.
	ID string
	// Steps is a list of steps taken by this transition (so far).
	Steps []*Step
	// StatesBefore is a list of states before the transition.
	StatesBefore S
	// TimeBefore is the machine time from before the transition.
	TimeBefore Time
	// TimeAfter is the machine time from after the transition. If the transition
	// has been canceled, this will be the same as TimeBefore.
	TimeAfter Time
	// Enters is a list of states with enter handlers in this transition.
	Enters S
	// Enters is a list of states with exit handlers in this transition.
	Exits S
	// TargetStates is a list of states after parsing the relations.
	TargetStates S
	// Accepted tells if the transition was accepted during the negotiation phase.
	Accepted bool
	// Mutation call which caused this transition
	Mutation *Mutation
	// Machine is the parent machine of this transition.
	Machine *Machine
	// LogEntries are log msgs produced during the transition.
	LogEntries []*LogEntry
	// PreLogEntries are log msgs produced before during the transition.
	PreLogEntries []*LogEntry
	// QueueLen is the length of the queue after the transition.
	QueueLen int

	isCompleted    atomic.Bool
	logEntriesLock sync.Mutex
	// Latest / current step of the transition
	latestStep *Step
}

// newTransition creates a new transition for the given mutation.
func newTransition(m *Machine, item *Mutation) *Transition {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	t := &Transition{
		ID:           randID(),
		Mutation:     item,
		StatesBefore: m.activeStates,
		TimeBefore:   m.time(nil),
		Machine:      m,
		Accepted:     true,
	}

	// assign early to catch the logs
	m.transitionLock.Lock()
	defer m.transitionLock.Unlock()
	m.t.Store(t)

	// tracers
	m.tracersLock.RLock()
	for i := 0; !t.Machine.Disposed.Load() && i < len(t.Machine.Tracers); i++ {
		t.Machine.Tracers[i].TransitionInit(t)
	}
	m.tracersLock.RUnlock()

	// log
	states := t.CalledStates()
	mutType := t.Type()
	logArgs := t.LogArgs()
	t.addSteps(newSteps("", states, StepRequested, nil)...)

	if item.Auto {
		m.log(LogDecisions, "[%s:auto] %s%s", mutType, j(states), logArgs)
	} else {
		m.log(LogOps, "[%s] %s%s", mutType, j(states), logArgs)
	}

	statesToSet := S{}
	switch mutType {

	case MutationRemove:
		statesToSet = slicesFilter(m.activeStates, func(state string, _ int) bool {
			return !slices.Contains(states, state)
		})
		t.addSteps(newSteps("", states, StepRemove, nil)...)

	case MutationAdd:
		statesToSet = append(statesToSet, states...)
		statesToSet = append(statesToSet, m.activeStates...)
		t.addSteps(newSteps("", DiffStates(statesToSet, m.activeStates),
			StepSet, nil)...)

	case MutationSet:
		statesToSet = states
		t.addSteps(newSteps("", DiffStates(statesToSet, m.activeStates),
			StepSet, nil)...)
		t.addSteps(newSteps("", DiffStates(m.activeStates, statesToSet),
			StepRemove, nil)...)
	}

	t.TargetStates = m.resolver.GetTargetStates(t, statesToSet)

	impliedStates := DiffStates(t.TargetStates, statesToSet)
	if len(impliedStates) > 0 {
		m.log(LogOps, "[implied] %s", j(impliedStates))
	}

	t.setupAccepted()
	if t.Accepted {
		t.setupExitEnter()
	}

	return t
}

// IsAuto returns true if the transition was triggered by an auto state.
// Thus, it cant trigger any other auto state mutations.
func (t *Transition) IsAuto() bool {
	return t.Mutation.Auto
}

// CalledStates return explicitly called / requested states of the transition.
func (t *Transition) CalledStates() S {
	return t.Mutation.CalledStates
}

// ClockBefore return the Clock from before the transition.
func (t *Transition) ClockBefore() Clock {
	ret := Clock{}
	states := t.Machine.StateNames()
	for k, v := range t.TimeBefore {
		ret[states[k]] = v
	}

	return ret
}

// ClockAfter return the Clock from before the transition.
func (t *Transition) ClockAfter() Clock {
	ret := Clock{}
	states := t.Machine.StateNames()
	for k, v := range t.TimeAfter {
		ret[states[k]] = v
	}

	return ret
}

// Args returns the argument map passed to the mutation method
// (or an empty one).
func (t *Transition) Args() A {
	return t.Mutation.Args
}

// Type returns the type of the mutation (add, remove, set).
func (t *Transition) Type() MutationType {
	return t.Mutation.Type
}

// LogArgs returns a text snippet with arguments which should be logged for this
// Mutation.
func (t *Transition) LogArgs() string {
	matcher := t.Machine.GetLogArgs()
	if matcher == nil {
		return ""
	}

	var args []string
	for k, v := range matcher(t.Mutation.Args) {
		args = append(args, k+"="+v)
	}
	if len(args) == 0 {
		return ""
	}

	return " (" + strings.Join(args, " ") + ")"
}

// String representation of the transition and the steps taken so far.
func (t *Transition) String() string {
	var lines []string
	for k := range t.Steps {

		touch := t.Steps[k]
		line := ""
		if touch.ToState != "" {
			line += touch.ToState + " -> "
		}

		line += touch.FromState
		if touch.Type > 0 {
			line += touch.Type.String()
		}
		if touch.Data != nil {
			line += "   (" + touch.Data.(string) + ")"
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (t *Transition) addSteps(steps ...*Step) {
	t.Steps = append(t.Steps, steps...)
}

func (t *Transition) setupExitEnter() {
	m := t.Machine

	// collect the exit handlers
	exits := DiffStates(m.activeStates, t.TargetStates)
	m.resolver.SortStates(exits)

	// collect the enters handlers
	var enters S
	for _, s := range t.TargetStates {
		// enter activate state only for multi states called directly
		state := m.states[s]
		if !m.is(S{s}) || (state.Multi && slices.Contains(t.CalledStates(), s)) {
			enters = append(enters, s)
		}
	}

	// save
	t.Exits = exits
	t.Enters = enters
}

func (t *Transition) emitSelfEvents() Result {
	m := t.Machine
	ret := Executed
	var handlerCalled bool
	for _, s := range t.CalledStates() {
		// only the active states
		if !t.Machine.Is(S{s}) {
			continue
		}
		name := s + s
		step := newStep(s, "", StepHandler, name)
		step.IsSelf = true
		ret, handlerCalled = m.handle(name, t.Mutation.Args, step, false)
		if handlerCalled {
			t.addSteps(step)
		}
		if ret == Canceled {
			break
		}
	}
	return ret
}

func (t *Transition) emitEnterEvents() Result {
	for _, toState := range t.Enters {
		args := t.Mutation.Args

		ret := t.emitHandler(Any, toState, Any+toState, args)
		if ret == Canceled {
			return ret
		}

		ret = t.emitHandler("", toState, toState+"Enter", args)
		if ret == Canceled {
			return ret
		}
	}

	return Executed
}

func (t *Transition) emitExitEvents() Result {
	for _, from := range t.Exits {
		ret := t.emitHandler(from, "", from+"Exit", t.Mutation.Args)
		if ret == Canceled {
			return ret
		}
		for _, state := range t.TargetStates {
			handler := from + state
			ret = t.emitHandler(from, state, handler, t.Mutation.Args)
			if ret == Canceled {
				return ret
			}
		}
		ret = t.emitHandler(from, Any, from+Any, nil)
		if ret == Canceled {
			return ret
		}
	}
	return Executed
}

func (t *Transition) emitHandler(from, to, event string, args A) Result {
	step := newStep(from, to, StepHandler, event)
	t.latestStep = step
	ret, handlerCalled := t.Machine.handle(event, args, step, false)
	if handlerCalled {
		t.addSteps(step)
	}
	return ret
}

func (t *Transition) emitFinalEvents() Result {
	finals := S{}
	finals = append(finals, t.Exits...)
	finals = append(finals, t.Enters...)

	for _, s := range finals {
		isEnter := slices.Contains(t.Enters, s)

		var handler string
		if isEnter {
			handler = s + "State"
		} else {
			handler = s + "End"
		}

		step := newStep(s, "", StepHandler, handler)
		step.IsFinal = true
		step.IsEnter = isEnter
		t.latestStep = step
		ret, handlerCalled := t.Machine.handle(handler, t.Mutation.Args, step,
			false)

		if handlerCalled {
			t.addSteps(step)
		}

		// final handler cancel means timeout
		if ret == Canceled {
			return ret
		}
	}

	return Executed
}

func (t *Transition) emitEvents() Result {
	m := t.Machine
	result := Executed
	if !t.Accepted {
		result = Canceled
	}
	hasStateChanged := false

	// tracers
	m.tracersLock.RLock()
	for i := 0; !t.Machine.Disposed.Load() && i < len(t.Machine.Tracers); i++ {
		t.Machine.Tracers[i].TransitionStart(t)
	}
	m.tracersLock.RUnlock()

	// NEGOTIATION CALLS PHASE (cancellable)

	// FooFoo handlers
	if result != Canceled && t.Type() != MutationRemove {
		result = t.emitSelfEvents()
	}

	// FooExit handlers
	if result != Canceled {
		result = t.emitExitEvents()
	}

	// FooEnter handlers
	if result != Canceled {
		result = t.emitEnterEvents()
	}

	// global AnyAny handler
	ret := t.emitHandler(Any, Any, Any+Any, t.Mutation.Args)
	if ret == Canceled {
		return ret
	}

	// FINAL HANDLERS (non cancellable)
	if result != Canceled {

		m.setActiveStates(t.CalledStates(), t.TargetStates, t.IsAuto())
		result = t.emitFinalEvents()

		hasStateChanged = !m.IsTime(t.TimeBefore, nil)
	}

	// gather new clock values
	t.TimeAfter = m.time(nil)

	// AUTO STATES
	if result == Canceled {

		t.Accepted = false
	} else if hasStateChanged && !t.IsAuto() {

		autoMutation := m.resolver.GetAutoMutation()
		if autoMutation != nil {
			m.log(LogOps, "[auto] %s", j(autoMutation.CalledStates))
			// unshift
			m.queueLock.Lock()
			m.queue = append([]*Mutation{autoMutation}, m.queue...)
			m.queueLen.Store(int32(len(m.queue)))
			m.queueLock.Unlock()
		}
	}

	// handlers done, collect previous log entries
	m.logEntriesLock.Lock()
	t.PreLogEntries = m.logEntries
	t.QueueLen = int(m.queueLen.Load())
	m.logEntries = nil
	m.logEntriesLock.Unlock()
	t.isCompleted.Store(true)

	// tracers
	m.tracersLock.RLock()
	for i := 0; !t.Machine.Disposed.Load() && i < len(t.Machine.Tracers); i++ {
		t.Machine.Tracers[i].TransitionEnd(t)
	}
	m.tracersLock.RUnlock()

	if result == Canceled {
		return Canceled
	}
	if t.Type() == MutationRemove {
		if m.Not(t.CalledStates()) {
			return Executed
		} else {
			// TODO error?
			return Canceled
		}
	} else {
		if m.Is(t.TargetStates) {
			return Executed
		} else {
			// TODO error?
			return Canceled
		}
	}
}

func (t *Transition) setupAccepted() {
	m := t.Machine
	// Dropping states doesn't require an acceptance
	if t.Type() == MutationRemove {
		return
	}
	notAccepted := DiffStates(t.CalledStates(), t.TargetStates)
	// Auto-states can be set partially
	if t.IsAuto() {
		// partially accepted
		if len(notAccepted) < len(t.CalledStates()) {
			return
		}
		// all rejected, reject the whole transition
		t.Accepted = false
	}
	if len(notAccepted) <= 0 {
		return
	}
	t.Accepted = false
	m.log(LogOps, "[cancel:reject] %s", j(notAccepted))
	t.addSteps(newSteps("", notAccepted, StepCancel, nil)...)
}

// IsCompleted is true when the execution of the transition has been fully
// completed.

func (t *Transition) IsCompleted() bool {
	return t.isCompleted.Load()
}
