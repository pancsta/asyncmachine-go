package machine

import (
	"sort"

	"github.com/samber/lo"
)

// RelationsResolver is an interface for parsing relations between states.
// TODO support custom relation types
type RelationsResolver interface {
	// GetTargetStates returns the target states after parsing the relations.
	GetTargetStates(t *Transition, calledStates S) S
	// GetAutoMutation returns an (optional) auto mutation which is appended to
	// the queue after the transition is executed.
	GetAutoMutation() *Mutation
	// SortStates sorts the states in the order their handlers should be
	// executed.
	SortStates(states S)
}

// DefaultRelationsResolver is the default implementation of the
// RelationsResolver.
type DefaultRelationsResolver struct {
	Machine    *Machine
	Transition *Transition
}

func (rr *DefaultRelationsResolver) GetTargetStates(
	t *Transition, calledStates S,
) S {
	rr.Transition = t
	rr.Machine = t.Machine
	m := t.Machine
	calledStates = m.MustParseStates(calledStates)
	calledStates = rr.parseAdd(calledStates)
	calledStates = lo.Uniq(calledStates)
	calledStates = rr.parseRequire(calledStates)
	// start from the end
	resolvedS := lo.Reverse(calledStates)
	// collect blocked calledStates
	alreadyBlocked := S{}
	// remove already blocked calledStates
	resolvedS = lo.Filter(resolvedS, func(name string, _ int) bool {
		blockedBy := rr.stateBlockedBy(calledStates, name)
		// ignore blocking by already blocked states
		blockedBy = lo.Filter(blockedBy, func(blockerName string, _ int) bool {
			return !lo.Contains(alreadyBlocked, blockerName)
		})
		if len(blockedBy) == 0 {
			return true
		}
		alreadyBlocked = append(alreadyBlocked, name)
		// if state wasn't implied by another state (was one of the active
		// states) then make it a higher priority log msg
		var lvl LogLevel
		if m.Is(S{name}) {
			lvl = LogOps
		} else {
			lvl = LogDecisions
		}
		m.log(lvl, "[rel:remove] %s by %s", name, j(blockedBy))
		if m.Is(S{name}) {
			t.addSteps(newStep("", name, TransitionStepTypeRemove, nil))
		} else {
			t.addSteps(newStep("", name, TransitionStepTypeNoSet, nil))
		}
		return false
	})
	// states removed by the states which are about to be set
	toRemove := lo.Reduce(resolvedS, func(acc S, name string, _ int) S {
		state := m.States[name]
		if state.Remove != nil {
			acc = append(acc, state.Remove...)
		}
		return acc
	}, S{})
	resolvedS = lo.Filter(rr.parseAdd(resolvedS), func(name string,
		_ int,
	) bool {
		return !lo.Contains(toRemove, name)
	})
	resolvedS = lo.Uniq(resolvedS)
	// Parsing required states allows to avoid cross-removal of states
	targetStates := rr.parseRequire(lo.Reverse(resolvedS))
	rr.SortStates(targetStates)
	return targetStates
}

func (rr *DefaultRelationsResolver) GetAutoMutation() *Mutation {
	t := rr.Transition
	m := t.Machine
	var toAdd []string
	// check all Auto states
	for s := range m.States {
		if !m.States[s].Auto {
			continue
		}
		// check if the state is blocked by another active state
		isBlocked := func() bool {
			for _, active := range m.ActiveStates {
				if lo.Contains(m.States[active].Remove, s) {
					m.log(LogEverything, "[auto:rel:remove] %s by %s", s,
						active)
					return true
				}
			}
			return false
		}
		if !m.Is(S{s}) && !isBlocked() {
			toAdd = append(toAdd, s)
		}
	}
	if len(toAdd) < 1 {
		return nil
	}
	return &Mutation{
		Type:         MutationTypeAdd,
		CalledStates: toAdd,
		Auto:         true,
	}
}

func (rr *DefaultRelationsResolver) SortStates(states S) {
	t := rr.Transition
	m := t.Machine
	sort.SliceStable(states, func(i, j int) bool {
		name1 := states[i]
		name2 := states[j]
		state1 := m.States[name1]
		state2 := m.States[name2]
		if lo.Contains(state1.After, name2) {
			t.addSteps(newStep(name2, name1,
				TransitionStepTypeRelation, RelationAfter))
			return false
		} else if lo.Contains(state2.After, name1) {
			t.addSteps(newStep(name1, name2,
				TransitionStepTypeRelation, RelationAfter))
			return true
		}
		return false
	})
}

func (rr *DefaultRelationsResolver) parseAdd(states S) S {
	t := rr.Transition
	ret := states
	visited := S{}
	changed := true
	for changed {
		changed = false
		for _, name := range states {
			if lo.Contains(t.StatesBefore, name) {
				continue
			}
			state := t.Machine.States[name]
			if lo.Contains(visited, name) || state.Add == nil {
				continue
			}
			t.addSteps(newSteps(name, state.Add, TransitionStepTypeRelation,
				RelationAdd)...)
			t.addSteps(newSteps("", state.Add, TransitionStepTypeSet, nil)...)
			ret = append(ret, state.Add...)
			visited = append(visited, name)
			changed = true
		}
	}
	return ret
}

func (rr *DefaultRelationsResolver) stateBlockedBy(
	blockingStates S, blocked string,
) S {
	m := rr.Machine
	t := rr.Transition
	blockedBy := S{}
	for _, blocking := range blockingStates {
		state := m.States[blocking]
		if !lo.Contains(state.Remove, blocked) {
			continue
		}
		t.addSteps(newStep(blocking, blocked, TransitionStepTypeRelation,
			RelationRemove))
		blockedBy = append(blockedBy, blocking)
	}
	return blockedBy
}

func (rr *DefaultRelationsResolver) parseRequire(states S) S {
	t := rr.Transition
	lengthBefore := 0
	// maps of states with their required states missing
	missingMap := map[string]S{}
	for lengthBefore != len(states) {
		lengthBefore = len(states)
		states = lo.Filter(states, func(name string, _ int) bool {
			state := t.Machine.States[name]
			missingReqs := rr.getMissingRequires(name, state, states)
			if len(missingReqs) > 0 {
				missingMap[name] = missingReqs
			}
			return len(missingReqs) == 0
		})
	}
	if len(missingMap) > 0 {
		names := S{}
		for state, notFound := range missingMap {
			names = append(names, state+"(-"+jw(notFound, " -")+")")
		}
		if t.IsAuto() {
			t.Machine.log(LogDecisions, "[reject:auto] %s", jw(names, " "))
		} else {
			t.Machine.log(LogOps, "[reject] %s", jw(names, " "))
		}
	}
	return states
}

func (rr *DefaultRelationsResolver) getMissingRequires(
	name string, state State, states S,
) S {
	t := rr.Transition
	ret := S{}
	for _, req := range state.Require {
		t.addSteps(newStep(name, req, TransitionStepTypeRelation,
			RelationRequire))
		if lo.Contains(states, req) {
			continue
		}
		ret = append(ret, req)
		t.addSteps(newStep(name, "", TransitionStepTypeNoSet, nil))
		if lo.Contains(t.Mutation.CalledStates, name) {
			t.addSteps(newStep("", req,
				TransitionStepTypeCancel, nil))
		}
	}
	return ret
}
