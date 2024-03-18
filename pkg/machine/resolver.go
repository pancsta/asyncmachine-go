package machine

import (
	"fmt"
	"slices"
	"sort"
)

// RelationsResolver is an interface for parsing relations between states.
// Not thread-safe.
// TODO support custom relation types and additional state properties.
type RelationsResolver interface {
	// GetTargetStates returns the target states after parsing the relations.
	GetTargetStates(t *Transition, calledStates S) S
	// GetAutoMutation returns an (optional) auto mutation which is appended to
	// the queue after the transition is executed.
	GetAutoMutation() *Mutation
	// SortStates sorts the states in the order their handlers should be
	// executed.
	SortStates(states S)
	// GetRelationsOf returns a list of relation types of the given state.
	GetRelationsOf(fromState string) ([]Relation, error)
	// GetRelationsBetween returns a list of relation types between the given
	// states.
	GetRelationsBetween(fromState, toState string) ([]Relation, error)
}

// DefaultRelationsResolver is the default implementation of the
// RelationsResolver with Add, Remove, Require and After. It can be overridden
// using Opts.Resolver.
type DefaultRelationsResolver struct {
	Machine    *Machine
	Transition *Transition
}

// GetTargetStates implements RelationsResolver.GetTargetStates.
func (rr *DefaultRelationsResolver) GetTargetStates(
	t *Transition, calledStates S,
) S {
	rr.Transition = t
	rr.Machine = t.Machine
	m := t.Machine
	calledStates = m.MustParseStates(calledStates)
	calledStates = rr.parseAdd(calledStates)
	calledStates = slicesUniq(calledStates)
	calledStates = rr.parseRequire(calledStates)

	// start from the end
	resolvedS := slicesReverse(calledStates)

	// collect blocked calledStates
	alreadyBlocked := S{}

	// remove already blocked calledStates
	resolvedS = slicesFilter(resolvedS, func(name string, _ int) bool {
		blockedBy := rr.stateBlockedBy(calledStates, name)

		// ignore blocking by already blocked states
		blockedBy = slicesFilter(blockedBy, func(blockerName string, _ int) bool {
			return !slices.Contains(alreadyBlocked, blockerName)
		})
		if len(blockedBy) == 0 {
			return true
		}

		alreadyBlocked = append(alreadyBlocked, name)
		// if state wasn't implied by another state (was one of the active
		// states) then make it a higher priority log msg
		var lvl LogLevel
		if m.is(S{name}) {
			lvl = LogOps
		} else {
			lvl = LogDecisions
		}

		m.log(lvl, "[rel:remove] %s by %s", name, j(blockedBy))
		if m.is(S{name}) {
			t.addSteps(newStep("", name, StepRemove, nil))
		} else {
			t.addSteps(newStep("", name, StepRemoveNotActive, nil))
		}

		return false
	})

	// states removed by the states which are about to be set
	var toRemove S
	for _, name := range resolvedS {
		state := m.states[name]
		if state.Remove != nil {
			toRemove = append(toRemove, state.Remove...)
		}
	}
	resolvedS = slicesFilter(rr.parseAdd(resolvedS), func(name string,
		_ int,
	) bool {
		return !slices.Contains(toRemove, name)
	})
	resolvedS = slicesUniq(resolvedS)

	// Parsing required states allows to avoid cross-removal of states
	targetStates := rr.parseRequire(slicesReverse(resolvedS))
	rr.SortStates(targetStates)

	return targetStates
}

// GetAutoMutation implements RelationsResolver.GetAutoMutation.
func (rr *DefaultRelationsResolver) GetAutoMutation() *Mutation {
	t := rr.Transition
	m := t.Machine
	var toAdd []string
	// check all Auto states
	for s := range m.states {
		if !m.states[s].Auto {
			continue
		}
		// check if the state is blocked by another active state
		isBlocked := func() bool {
			for _, active := range m.activeStates {
				if slices.Contains(m.states[active].Remove, s) {
					m.log(LogEverything, "[auto:rel:remove] %s by %s", s,
						active)
					return true
				}
			}
			return false
		}
		if !m.is(S{s}) && !isBlocked() {
			toAdd = append(toAdd, s)
		}
	}
	if len(toAdd) < 1 {
		return nil
	}
	return &Mutation{
		Type:         MutationAdd,
		CalledStates: toAdd,
		Auto:         true,
	}
}

// SortStates implements RelationsResolver.SortStates.
func (rr *DefaultRelationsResolver) SortStates(states S) {
	t := rr.Transition
	m := rr.Machine
	sort.SliceStable(states, func(i, j int) bool {
		name1 := states[i]
		name2 := states[j]
		state1 := m.states[name1]
		state2 := m.states[name2]
		if slices.Contains(state1.After, name2) {
			t.addSteps(newStep(name2, name1,
				StepRelation, RelationAfter))
			return false
		} else if slices.Contains(state2.After, name1) {
			t.addSteps(newStep(name1, name2,
				StepRelation, RelationAfter))
			return true
		}
		return false
	})
}

// TODO docs
func (rr *DefaultRelationsResolver) parseAdd(states S) S {
	t := rr.Transition
	ret := states
	visited := S{}
	changed := true
	for changed {
		changed = false
		for _, name := range states {
			state := rr.Machine.states[name]

			if slices.Contains(t.StatesBefore, name) && !state.Multi {
				continue
			}
			if slices.Contains(visited, name) || state.Add == nil {
				continue
			}

			t.addSteps(newSteps(name, state.Add, StepRelation,
				RelationAdd)...)
			t.addSteps(newSteps("", state.Add, StepSet, nil)...)
			ret = append(ret, state.Add...)
			visited = append(visited, name)
			changed = true
		}
	}
	return ret
}

// TODO docs
func (rr *DefaultRelationsResolver) stateBlockedBy(
	blockingStates S, blocked string,
) S {
	m := rr.Machine
	t := rr.Transition
	blockedBy := S{}

	for _, blocking := range blockingStates {
		state := m.states[blocking]
		if !slices.Contains(state.Remove, blocked) {
			continue
		}

		t.addSteps(newStep(blocking, blocked, StepRelation,
			RelationRemove))
		blockedBy = append(blockedBy, blocking)
	}

	return blockedBy
}

// TODO docs
func (rr *DefaultRelationsResolver) parseRequire(states S) S {
	t := rr.Transition
	lengthBefore := 0
	// maps of states with their required states missing
	missingMap := map[string]S{}

	for lengthBefore != len(states) {
		lengthBefore = len(states)
		states = slicesFilter(states, func(name string, _ int) bool {
			state := rr.Machine.states[name]
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

// TODO docs
func (rr *DefaultRelationsResolver) getMissingRequires(
	name string, state State, states S,
) S {
	t := rr.Transition
	ret := S{}

	for _, req := range state.Require {
		t.addSteps(newStep(name, req, StepRelation,
			RelationRequire))
		if slices.Contains(states, req) {
			continue
		}
		ret = append(ret, req)
		t.addSteps(newStep(name, "", StepRemoveNotActive, nil))
		if slices.Contains(t.Mutation.CalledStates, name) {
			t.addSteps(newStep("", req,
				StepCancel, nil))
		}
	}

	return ret
}

// GetRelationsBetween returns a list of relation types between the given
// states. Not thread safe.
func (rr *DefaultRelationsResolver) GetRelationsBetween(
	fromState, toState string,
) ([]Relation, error) {
	m := rr.Machine

	if !m.Has1(fromState) {
		return nil, fmt.Errorf("%w: %s", ErrStateUnknown, fromState)
	}
	if !m.Has1(toState) {
		return nil, fmt.Errorf("%w: %s", ErrStateUnknown, toState)
	}

	state := m.states[fromState]
	var relations []Relation

	if state.Add != nil && slices.Contains(state.Add, toState) {
		relations = append(relations, RelationAdd)
	}

	if state.Require != nil && slices.Contains(state.Require, toState) {
		relations = append(relations, RelationRequire)
	}

	if state.Remove != nil && slices.Contains(state.Remove, toState) {
		relations = append(relations, RelationRemove)
	}

	if state.After != nil && slices.Contains(state.After, toState) {
		relations = append(relations, RelationAfter)
	}

	return relations, nil
}

// GetRelationsOf returns a list of relation types of the given state.
// Not thread safe.
func (rr *DefaultRelationsResolver) GetRelationsOf(fromState string) (
	[]Relation, error,
) {
	m := rr.Machine

	if !m.Has1(fromState) {
		return nil, fmt.Errorf("%w: %s", ErrStateUnknown, fromState)
	}
	state := m.states[fromState]

	var relations []Relation
	if state.Add != nil {
		relations = append(relations, RelationAdd)
	}

	if state.Require != nil {
		relations = append(relations, RelationRequire)
	}

	if state.Remove != nil {
		relations = append(relations, RelationRemove)
	}

	if state.After != nil {
		relations = append(relations, RelationAfter)
	}

	return relations, nil
}
