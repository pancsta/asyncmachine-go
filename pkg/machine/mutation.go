package machine

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
)

// Mutation represents an atomic change (or an attempt) of machine's active
// states. Mutation causes a Transition.
type Mutation struct {
	// add, set, remove
	Type MutationType
	// States explicitly passed to the mutation method, as their indexes. Use
	// Transition.CalledStates or IndexToStates to get the actual state names.
	Called []int
	// argument map passed to the mutation method (if any).
	Args A
	// this mutation has been triggered by an auto state
	// TODO rename to IsAuto
	Auto bool
	// Source is the source event for this mutation.
	Source *MutSource
	// IsCheck indicates that this mutation is a check, see [Machine.CanAdd].
	IsCheck bool

	// optional queue info

	// QueueTickNow is the queue tick during which this mutation was scheduled.
	QueueTickNow uint64
	// QueueLen is the length of the queue at the time when the mutation was
	// queued.
	QueueLen int32
	// QueueTokensLen is the amount of unexecuted queue tokens (priority queue).
	// TODO impl
	QueueTokensLen int32
	// QueueTick is the assigned queue tick at the time when the mutation was
	// queued. 0 for prepended mutations.
	QueueTick uint64
	// QueueToken is a unique ID, which is given to prepended mutations.
	// Tokens are assigned in a series but executed in random order.
	QueueToken uint64

	// internals

	// specific context for this mutation (optional)
	ctx context.Context
	// optional eval func, only for mutationEval
	eval        func()
	evalSource  string
	cacheCalled atomic.Pointer[S]
}

func (m *Mutation) String() string {
	return fmt.Sprintf("[%s] %v", m.Type, m.Called)
}

func (m *Mutation) IsCalled(idx int) bool {
	return slices.Contains(m.Called, idx)
}

func (m *Mutation) CalledIndex(index S) *TimeIndex {
	return NewTimeIndex(index, m.Called)
}

func (m *Mutation) StringFromIndex(index S) string {
	called := NewTimeIndex(index, m.Called)
	return "[" + m.Type.String() + "] " + j(called.ActiveStates(nil))
}

// MapArgs returns arguments of this Mutation which match the passed [mapper].
// The returned map is never nil.
func (m *Mutation) MapArgs(mapper LogArgsMapperFn) map[string]string {
	if mapper == nil {
		return map[string]string{}
	}

	if ret := mapper(m.Args); ret == nil {
		return map[string]string{}
	} else {
		return ret
	}
}

// LogArgs returns a text snippet with arguments which should be logged for this
// Mutation.
func (m *Mutation) LogArgs(mapper LogArgsMapperFn) string {
	return MutationFormatArgs(m.MapArgs(mapper))
}

// StepType enum
type StepType int8

const (
	StepRelation StepType = 1 << iota
	StepHandler
	// TODO rename to StepActivate
	StepSet
	// StepRemove indicates a step where a state goes active->inactive
	// TODO rename to StepDeactivate
	StepRemove
	// StepRemoveNotActive indicates a step where a state goes inactive->inactive
	// TODO rename to StepDeactivatePassive
	StepRemoveNotActive
	StepRequested
	StepCancel
)

func (tt StepType) String() string {
	switch tt {
	case StepRelation:
		return "rel"
	case StepHandler:
		return "handler"
	case StepSet:
		return "activate"
	case StepRemove:
		return "deactivate"
	case StepRemoveNotActive:
		return "deactivate-passive"
	case StepRequested:
		return "requested"
	case StepCancel:
		return "cancel"
	}
	return ""
}

// Step struct represents a single step within a Transition, either a relation
// resolving step or a handler call.
type Step struct {
	Type StepType
	// Only for Type == StepRelation.
	RelType Relation
	// marks a final handler (FooState, FooEnd)
	IsFinal bool
	// marks a self handler (FooFoo)
	IsSelf bool
	// marks an enter handler (FooState, but not FooEnd). Requires IsFinal.
	IsEnter bool
	// Deprecated, use GetFromState(). TODO remove
	FromState string
	// TODO implement
	FromStateIdx int
	// Deprecated, use GetToState(). TODO remove
	ToState string
	// TODO implement
	ToStateIdx int
	// Deprecated, use RelType. TODO remove
	Data any
	// TODO emitter name and num
}

// GetFromState returns the source state of a step. Optional, unless no
// GetToState().
func (s *Step) GetFromState(index S) string {
	// TODO rename to FromState
	if s.FromState != "" {
		return s.FromState
	}
	if s.FromStateIdx == -1 {
		return ""
	}
	if s.FromStateIdx < len(index) {
		return index[s.FromStateIdx]
	}

	return ""
}

// GetToState returns the target state of a step. Optional, unless no
// GetFromState().
func (s *Step) GetToState(index S) string {
	// TODO rename to ToState
	if s.ToState != "" {
		return s.ToState
	}
	if s.ToStateIdx == -1 {
		return ""
	}
	if s.ToStateIdx < len(index) {
		return index[s.ToStateIdx]
	}

	return ""
}

func (s *Step) StringFromIndex(idx S) string {
	var line string

	// collect
	from := s.GetFromState(idx)
	to := s.GetToState(idx)
	if from == "" && to == "" {
		to = StateAny
	}

	// format TODO markdown?
	if from != "" {
		from = "**" + from + "**"
	}
	if to != "" {
		to = "**" + to + "**"
	}

	// output
	if from != "" && to != "" {
		line += from + " " + s.RelType.String() + " " + to
	} else {
		line = from
	}
	if line == "" {
		line = to
	}

	if s.Type == StepRelation {
		return line
	}

	suffix := ""
	if s.Type == StepHandler {
		if s.IsSelf {
			suffix = line
		} else if s.IsFinal && s.IsEnter {
			suffix = SuffixState
		} else if s.IsFinal && !s.IsEnter {
			suffix = SuffixEnd
		} else if !s.IsFinal && s.IsEnter {
			suffix = SuffixEnter
		} else if !s.IsFinal && !s.IsEnter {
			suffix = SuffixExit
		}
	}

	// infer the name
	return s.Type.String() + " " + line + suffix
}

func newStep(from string, to string, stepType StepType,
	relType Relation,
) *Step {
	ret := &Step{
		// TODO refac with the new dbg protocol, use indexes only
		FromState: from,
		ToState:   to,
		Type:      stepType,
		RelType:   relType,
	}
	// default values TODO use real values
	if from == "" {
		ret.FromStateIdx = -1
	}
	if to == "" {
		ret.ToStateIdx = -1
	}

	return ret
}

func newSteps(from string, toStates S, stepType StepType,
	relType Relation,
) []*Step {
	// TODO optimize, only use during debug
	var ret []*Step
	for _, to := range toStates {
		ret = append(ret, newStep(from, to, stepType, relType))
	}

	return ret
}
