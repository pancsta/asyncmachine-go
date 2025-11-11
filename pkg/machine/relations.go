package machine

import (
	"fmt"
	"slices"
	"sort"
)

// RelationsResolver is an interface for parsing relations between states.
// Not thread-safe.
// TODO support custom relation types and additional state properties.
// TODO refac Relations
type RelationsResolver interface {
	// TargetStates returns the target states after parsing the relations.
	TargetStates(t *Transition, calledStates, index S) S
	// NewAutoMutation returns an (optional) auto mutation which is appended to
	// the queue after the transition is executed. It also returns the names of
	// the called states.
	NewAutoMutation() (*Mutation, S)
	// SortStates sorts the states in the order their handlers should be
	// executed.
	SortStates(states S)
	// RelationsOf returns a list of relation types of the given state.
	RelationsOf(fromState string) ([]Relation, error)
	// RelationsBetween returns a list of relation types between the given
	// states.
	RelationsBetween(fromState, toState string) ([]Relation, error)
	InboundRelationsOf(toState string) (S, error)
	// TODO InboundRelationsBetween

	// NewSchema runs when Machine receives a new struct.
	NewSchema(schema Schema, states S)
}

// DefaultRelationsResolver is the default implementation of the
// RelationsResolver with Add, Remove, Require and After. It can be overridden
// using Opts.Resolver.
// TODO refac RelationsDefault
type DefaultRelationsResolver struct {
	Machine      *Machine
	Transition   *Transition
	Index        S
	topology     S
	statesBefore S
}

var _ RelationsResolver = &DefaultRelationsResolver{}

func (rr *DefaultRelationsResolver) NewSchema(schema Schema, states S) {
	if rr.Machine == nil {
		// manual resolver instance
		return
	}

	rr.Index = states

	g := newGraph()
	for _, name := range rr.Index {
		state := rr.Machine.schema[name]
		for _, req := range state.Require {
			g.AddEdge(name, req)
		}
	}

	sorted, err := g.TopologicalSort()
	if err != nil {
		// cycle, keep unsorted
		rr.Machine.log(LogChanges, "[resolver] %s: %s for %s",
			ErrRelation, err, rr.Machine.id)
	} else {
		rr.topology = sorted
	}
}

// TargetStates implements RelationsResolver.TargetStates.
func (rr *DefaultRelationsResolver) TargetStates(
	t *Transition, statesToSet, index S,
) S {
	rr.Transition = t
	rr.Machine = t.Machine
	rr.Index = index
	rr.statesBefore = t.StatesBefore()

	m := t.Machine
	statesToSet = m.mustParseStates(statesToSet)
	statesToSet = rr.parseAdd(statesToSet)
	statesToSet = slicesUniq(statesToSet)
	statesToSet = rr.parseRequire(statesToSet)

	// start from the end
	// TODO optimize?
	resolvedS := slicesReverse(statesToSet)

	// collect blocked calledStates
	alreadyBlocked := map[string]struct{}{}

	// remove already blocked calledStates
	resolvedS = slicesFilter(resolvedS, func(name string, _ int) bool {
		blockedBy := rr.stateBlockedBy(statesToSet, name)

		// ignore blocking by already blocked states
		blockedBy = slicesFilter(blockedBy, func(blockerName string, _ int) bool {
			_, ok := alreadyBlocked[blockerName]
			return !ok
		})
		if len(blockedBy) == 0 {
			return true
		}

		alreadyBlocked[name] = struct{}{}
		// if state wasn't implied by another state (was one of the active
		// states) then make it a higher priority log msg
		var lvl LogLevel
		if m.is(S{name}) {
			lvl = LogOps
		} else {
			lvl = LogDecisions
		}

		m.log(lvl, "[rel:remove] %s by %s", name, j(blockedBy))
		if t.isLogSteps() {
			if m.is(S{name}) {
				t.addSteps(newStep("", name, StepRemove, 0))
			} else {
				t.addSteps(newStep("", name, StepRemoveNotActive, 0))
			}
		}

		return false
	})

	// states removed by the states which are about to be set
	var toRemove S
	for _, name := range resolvedS {
		state := m.schema[name]
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

// NewAutoMutation implements RelationsResolver.GetAutoMutation.
func (rr *DefaultRelationsResolver) NewAutoMutation() (*Mutation, S) {
	t := rr.Transition
	m := t.Machine
	var toAdd S

	// check all Auto states
	for s := range m.schema {
		if !m.schema[s].Auto {
			continue
		}
		// check if the state is blocked by another active state
		isBlocked := func() bool {
			for _, active := range m.activeStates {
				if slices.Contains(m.schema[active].Remove, s) {
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
		return nil, nil
	}

	mut := Mutation{
		Type:   MutationAdd,
		Called: m.Index(toAdd),
		Auto:   true,
	}
	mut.cacheCalled.Store(&toAdd)

	return &mut, toAdd
}

// SortStates implements RelationsResolver.SortStates.
func (rr *DefaultRelationsResolver) SortStates(states S) {
	t := rr.Transition
	m := rr.Machine

	rr.sortRequire(states)

	// sort by After
	// TODO optimize / cache (but not in debug, to have steps)
	sort.SliceStable(states, func(i, j int) bool {
		name1 := states[i]
		name2 := states[j]
		state1 := m.schema[name1]
		state2 := m.schema[name2]

		// forward relations
		if slices.Contains(state1.After, name2) {
			if t.isLogSteps() {
				t.addSteps(newStep(name2, name1, StepRelation, RelationAfter))
			}
			return false

		} else if slices.Contains(state2.After, name1) {
			if t.isLogSteps() {
				t.addSteps(newStep(name1, name2, StepRelation, RelationAfter))
			}
			return true
		}

		return false
	})
}

// sortRequire sorts the states by Require relations.
func (rr *DefaultRelationsResolver) sortRequire(states S) {
	// TODO optimize with an index / cache
	sort.SliceStable(states, func(i, j int) bool {
		return slices.Index(rr.topology, states[i]) <
			slices.Index(rr.topology, states[j])
	})
}

// TODO docs
func (rr *DefaultRelationsResolver) parseAdd(states S) S {
	// TODO optimize: loose Contains?
	t := rr.Transition
	ret := states
	visited := S{}
	changed := true
	for changed {
		changed = false
		for _, name := range states {
			state := rr.Machine.schema[name]

			if slices.Contains(rr.statesBefore, name) && !state.Multi {
				continue
			}
			if slices.Contains(visited, name) {
				continue
			}

			// filter the Add relation from states called for removal
			var addStates S
			for _, add := range state.Add {
				idxAdd := slices.Index(rr.Index, add)
				if t.Type() == MutationRemove &&
					slices.Contains(t.Mutation.Called, idxAdd) {

					continue
				}
				addStates = append(addStates, add)
			}
			if addStates == nil {
				continue
			}

			if rr.Machine.semLogger.IsSteps() {
				t.addSteps(newSteps(name, addStates, StepRelation,
					RelationAdd)...)
				t.addSteps(newSteps("", addStates, StepSet, 0)...)
			}
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

	// TODO optimize by schema index eg ["Foo-Bar-Remove"]
	for _, blocking := range blockingStates {
		state := m.schema[blocking]
		if !slices.Contains(state.Remove, blocked) {
			continue
		}

		if t.isLogSteps() {
			t.addSteps(newStep(blocking, blocked, StepRelation,
				RelationRemove))
		}
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
			state := rr.Machine.schema[name]
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
		if t.isLogSteps() {
			t.addSteps(newStep(name, req, StepRelation,
				RelationRequire))
		}
		if slices.Contains(states, req) {
			continue
		}
		ret = append(ret, req)
		if t.isLogSteps() {
			t.addSteps(newStep(name, "", StepRemoveNotActive, 0))
		}

		idx := slices.Index(rr.Index, name)
		if slices.Contains(t.Mutation.Called, idx) && t.isLogSteps() {
			// TODO optimize: stop on cancel
			t.addSteps(newStep("", req, StepCancel, 0))
		}
	}

	return ret
}

// GetRelationsBetween returns a list of outbound relations between
// fromState -> toState. Not thread safe.
func (rr *DefaultRelationsResolver) RelationsBetween(
	fromState, toState string,
) ([]Relation, error) {
	m := rr.Machine

	if !m.Has1(fromState) {
		return nil, fmt.Errorf("%w: %s", ErrStateUnknown, fromState)
	}
	if !m.Has1(toState) {
		return nil, fmt.Errorf("%w: %s", ErrStateUnknown, toState)
	}

	state := m.schema[fromState]
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
func (rr *DefaultRelationsResolver) RelationsOf(fromState string) (
	[]Relation, error,
) {
	m := rr.Machine

	if !m.Has1(fromState) {
		return nil, fmt.Errorf("%w: %s", ErrStateUnknown, fromState)
	}
	state := m.schema[fromState]

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

// InboundRelationsOf returns a list of states pointing to [toState].
// Not thread safe.
func (rr *DefaultRelationsResolver) InboundRelationsOf(toState string) (
	S, error,
) {
	m := rr.Machine

	if !m.Has1(toState) {
		return nil, fmt.Errorf("%w: %s", ErrStateUnknown, toState)
	}

	var states S
	for name, state := range m.schema {
		if name == toState {
			continue
		}

		if slices.Contains(state.Add, toState) {
			states = append(states, name)
		}

		if slices.Contains(state.Require, toState) {
			states = append(states, name)
		}

		if slices.Contains(state.Remove, toState) {
			states = append(states, name)
		}

		if slices.Contains(state.After, toState) {
			states = append(states, name)
		}
	}

	return states, nil
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
