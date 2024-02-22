package machine

import (
	"strings"

	"github.com/google/uuid"

	"github.com/samber/lo"
)

func newStep(from string, to string, stepType TransitionStepType,
	data any,
) *TransitionStep {
	return &TransitionStep{
		FromState: from,
		ToState:   to,
		Type:      stepType,
		Data:      data,
	}
}

func newSteps(from string, toStates S, stepType TransitionStepType,
	data any,
) []*TransitionStep {
	var ret []*TransitionStep
	for _, to := range toStates {
		ret = append(ret, newStep(from, to, stepType, data))
	}
	return ret
}

// Transition represents processing of a single mutation withing a machine.
type Transition struct {
	ID string
	// List of steps taken by this transition (so far).
	Steps []*TransitionStep
	// When true, execution of the transition has been completed.
	IsCompleted bool
	// states before the transition
	StatesBefore S
	// clocks of the states from before the transition
	ClocksBefore Clocks
	// States with "enter" handlers to execute
	Enters S
	// States with "exit" handlers to executed
	Exits S
	// target states after parsing the relations
	TargetStates S
	// was the transition accepted (during the negotiation phase)
	Accepted bool
	// Mutation call which cased this transition
	Mutation *Mutation
	// Parent machine
	Machine *Machine
	// Log entries produced during the transition
	LogEntries []string

	// Latest / current step of the transition
	latestStep *TransitionStep
}

// newTransition creates a new transition for the given mutation.
func newTransition(m *Machine, item *Mutation) *Transition {
	m.activeStatesLock.RLock()
	defer m.activeStatesLock.RUnlock()

	clocks := Clocks{}
	for _, state := range m.ActiveStates {
		clocks[state] = m.clock[state]
	}
	t := &Transition{
		ID:           uuid.New().String(),
		Mutation:     item,
		StatesBefore: m.ActiveStates,
		ClocksBefore: clocks,
		Machine:      m,
		Accepted:     true,
	}
	// set early to catch the logs
	m.Transition = t
	states := t.CalledStates()
	mutType := t.Type()
	t.addSteps(newSteps("", states, TransitionStepTypeRequested, nil)...)
	if item.Auto {
		m.log(LogDecisions, "[%s:auto] %s", mutType, j(states))
	} else {
		m.log(LogOps, "[%s] %s", mutType, j(states))
	}
	statesToSet := S{}
	switch mutType {
	case MutationTypeRemove:
		statesToSet = lo.Filter(m.ActiveStates, func(state string, _ int) bool {
			return !lo.Contains(states, state)
		})
		t.addSteps(newSteps("", states, TransitionStepTypeRemove, nil)...)
	case MutationTypeAdd:
		statesToSet = append(statesToSet, states...)
		statesToSet = append(statesToSet, m.ActiveStates...)
		t.addSteps(newSteps("", DiffStates(statesToSet, m.ActiveStates),
			TransitionStepTypeSet, nil)...)
	case MutationTypeSet:
		statesToSet = states
		t.addSteps(newSteps("", DiffStates(statesToSet, m.ActiveStates),
			TransitionStepTypeSet, nil)...)
		t.addSteps(newSteps("", DiffStates(m.ActiveStates, statesToSet),
			TransitionStepTypeRemove, nil)...)
	}
	t.TargetStates = m.Resolver.GetTargetStates(t, statesToSet)

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

// Args returns the argument object passed to the mutation method (if any).
func (t *Transition) Args() A {
	return t.Mutation.Args
}

// Type returns the type of the mutation (add, remove, set).
func (t *Transition) Type() MutationType {
	return t.Mutation.Type
}

func (t *Transition) addSteps(steps ...*TransitionStep) {
	t.Steps = append(t.Steps, steps...)
}

// String representation of the transition and the steps taken so far.
// TODO: implement, test
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

func (t *Transition) setupExitEnter() {
	m := t.Machine
	// collect the exit handlers
	exits := DiffStates(m.ActiveStates, t.TargetStates)
	m.Resolver.SortStates(exits)
	// collect the enters handlers
	enters := S{}
	for _, s := range t.TargetStates {
		// enter activate state only for multi states called directly
		// not implied by Add
		// TODO m.is()
		if m.Is(S{s}) && !(m.States[s].Multi && lo.Contains(t.CalledStates(), s)) {
			continue
		}
		enters = append(enters, s)
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
		step := newStep(s, "", TransitionStepTypeTransition, name)
		step.IsSelf = true
		ret, handlerCalled = m.emit(name, t.Mutation.Args, step)
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
		// states implied by Add cant cancel the transition
		isCalled := lo.Contains(t.CalledStates(), toState)
		args := t.Mutation.Args
		ret := t.emitHandler("Any", toState, "Any"+toState, args)
		if ret == Canceled && isCalled {
			return ret
		}
		ret = t.emitHandler("", toState, toState+"Enter", args)
		if ret == Canceled && isCalled {
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
		ret = t.emitHandler(from, "Any", from+"Any", nil)
		if ret == Canceled {
			return ret
		}
	}
	return Executed
}

func (t *Transition) emitHandler(from, to, event string, args A) Result {
	step := newStep(from, to, TransitionStepTypeTransition, event)
	t.latestStep = step
	ret, handlerCalled := t.Machine.emit(event, args, step)
	if handlerCalled {
		t.addSteps(step)
	}
	return ret
}

func (t *Transition) emitFinalEvents() {
	finals := S{}
	finals = append(finals, t.Exits...)
	finals = append(finals, t.Enters...)
	for _, s := range finals {
		isEnter := lo.Contains(t.Enters, s)
		var handler string
		if isEnter {
			handler = s + "State"
		} else {
			handler = s + "End"
		}
		step := newStep(s, "", TransitionStepTypeTransition, handler)
		step.IsFinal = true
		step.IsEnter = isEnter
		t.latestStep = step
		ret, handlerCalled := t.Machine.emit(handler, t.Mutation.Args, step)
		if handlerCalled {
			t.addSteps(step)
		}
		if ret == Canceled {
			break
		}
	}
}

func (t *Transition) emitEvents() Result {
	m := t.Machine
	result := Executed
	if !t.Accepted {
		result = Canceled
	}
	// TODO struct type
	txArgs := A{"transition": t}
	hasStateChanged := false

	// start emitting
	m.emit(EventTransitionStart, txArgs, nil)

	// NEGOTIATION CALLS PHASE (cancellable)
	// FooFoo handlers
	if result != Canceled && t.Type() != MutationTypeRemove {
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

	// FINAL HANDLERS (non cancellable)
	if result != Canceled {
		m.setActiveStates(t.CalledStates(), t.TargetStates, t.IsAuto())
		t.emitFinalEvents()
		t.IsCompleted = true
		hasStateChanged = m.HasStateChanged(t.StatesBefore, t.ClocksBefore)
		if hasStateChanged {
			m.emit(EventTick, A{"before": t.StatesBefore}, nil)
		}
	}

	// AUTO STATES
	if result == Canceled {
		m.emit(EventTransitionCancel, txArgs, nil)
	} else if hasStateChanged && !t.IsAuto() {
		autoMutation := m.Resolver.GetAutoMutation()
		if autoMutation != nil {
			m.log(LogOps, "[auto] %s", j((*autoMutation).CalledStates))
			// unshift
			m.Queue = append([]Mutation{*autoMutation}, m.Queue...)
		}
	}

	// stop emitting, collect previous log entries
	m.logEntriesLock.Lock()
	// TODO struct type
	txArgs["pre_logs"] = m.logEntries
	m.logEntries = []string{}
	m.logEntriesLock.Unlock()
	m.emit(EventTransitionEnd, txArgs, nil)

	if result == Canceled {
		return Canceled
	}
	if t.Type() == MutationTypeRemove {
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
	if t.Type() == MutationTypeRemove {
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
	t.addSteps(newSteps("", notAccepted, TransitionStepTypeCancel, nil)...)
}
