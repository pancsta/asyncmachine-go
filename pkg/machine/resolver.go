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
	// NewStruct runs when Machine receives a new struct.
	NewStruct()
}

// DefaultRelationsResolver is the default implementation of the
// RelationsResolver with Add, Remove, Require and After. It can be overridden
// using Opts.Resolver.
type DefaultRelationsResolver struct {
	Machine    *Machine
	Transition *Transition
	topology   S
}

var _ RelationsResolver = &DefaultRelationsResolver{}

func (rr *DefaultRelationsResolver) NewStruct() {
	g := newGraph()
	for _, name := range rr.Machine.StateNames() {
		state := rr.Machine.states[name]
		for _, req := range state.Require {
			g.AddEdge(name, req)
		}
	}

	sorted, err := g.TopologicalSort()
	if err != nil {
		panic(fmt.Errorf("%w: %w for %s", ErrRelation, err, rr.Machine.id))
	} else {
		rr.topology = sorted
	}
}

// GetTargetStates implements RelationsResolver.GetTargetStates.
func (rr *DefaultRelationsResolver) GetTargetStates(
	t *Transition, statesToSet S,
) S {
	rr.Transition = t
	rr.Machine = t.Machine
	m := t.Machine
	statesToSet = m.MustParseStates(statesToSet)
	statesToSet = rr.parseAdd(statesToSet)
	statesToSet = slicesUniq(statesToSet)
	statesToSet = rr.parseRequire(statesToSet)

	// start from the end
	resolvedS := slicesReverse(statesToSet)

	// collect blocked calledStates
	alreadyBlocked := S{}

	// remove already blocked calledStates
	resolvedS = slicesFilter(resolvedS, func(name string, _ int) bool {
		blockedBy := rr.stateBlockedBy(statesToSet, name)

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
			t.addSteps(newStep("", name, StepRemove, 0))
		} else {
			t.addSteps(newStep("", name, StepRemoveNotActive, 0))
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

	rr.sortRequire(states)

	// sort by After
	sort.SliceStable(states, func(i, j int) bool {
		name1 := states[i]
		name2 := states[j]
		state1 := m.states[name1]
		state2 := m.states[name2]

		// forward relations
		if slices.Contains(state1.After, name2) {
			t.addSteps(newStep(name2, name1, StepRelation, RelationAfter))
			return false
		} else if slices.Contains(state2.After, name1) {
			t.addSteps(newStep(name1, name2, StepRelation, RelationAfter))
			return true
		}

		return false
	})
}

// sortRequire sorts the states by Require relations.
func (rr *DefaultRelationsResolver) sortRequire(states S) {
	// TODO optimize with an index
	sort.SliceStable(states, func(i, j int) bool {
		return slices.Index(rr.topology, states[i]) <
			slices.Index(rr.topology, states[j])
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

			if slices.Contains(t.StatesBefore(), name) && !state.Multi {
				continue
			}
			if slices.Contains(visited, name) {
				continue
			}

			// filter the Add relation from states called for removal
			var addStates S
			for _, add := range state.Add {
				if t.Type() == MutationRemove &&
					slices.Contains(t.Mutation.CalledStates, add) {
					continue
				}
				addStates = append(addStates, add)
			}
			if addStates == nil {
				continue
			}

			t.addSteps(newSteps(name, addStates, StepRelation,
				RelationAdd)...)
			t.addSteps(newSteps("", addStates, StepSet, 0)...)
			ret = append(ret, addStates...)
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
		t.addSteps(newStep(name, "", StepRemoveNotActive, 0))
		if slices.Contains(t.Mutation.CalledStates, name) {
			t.addSteps(newStep("", req,
				StepCancel, 0))
		}
	}

	return ret
}

// GetRelationsBetween returns a list of directional relation types between
// the given states. Not thread safe.
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

// graph represents a directed graph using an adjacency list.
type graph struct {
	vertices map[string][]string
}

// newGraph creates a new graph.
func newGraph() *graph {
	return &graph{vertices: make(map[string][]string)}
}

// AddEdge adds a directed edge from src to dest.
func (g *graph) AddEdge(src, dest string) {
	g.vertices[src] = append(g.vertices[src], dest)
}

// TopologicalSort performs a topological sort on the graph.
func (g *graph) TopologicalSort() ([]string, error) {
	visited := make(map[string]bool)
	var stack []string
	tempMarked := make(map[string]bool)

	var visit func(string) error
	visit = func(node string) error {
		if tempMarked[node] {
			return fmt.Errorf("state %s has a Require cycle", node)
		}
		if !visited[node] {
			tempMarked[node] = true
			for _, neighbor := range g.vertices[node] {
				if err := visit(neighbor); err != nil {
					return err
				}
			}
			tempMarked[node] = false
			visited[node] = true
			stack = append(stack, node)
		}
		return nil
	}

	for node := range g.vertices {
		if !visited[node] {
			if err := visit(node); err != nil {
				return nil, err
			}
		}
	}

	return stack, nil
}
