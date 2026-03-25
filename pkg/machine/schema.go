package machine

import (
	"maps"
)

// ///// ///// /////

// ///// STATES & SCHEMA

// ///// ///// /////

type StatesBase struct {
	// Exception is the only built-in state and mean a global error. All errors
	// have to [State.Require] the Exception state. If [Machine.PanicToErr] is
	// true, Exception will receive it.
	Exception   string
	names       S
	groups      map[string][]int
	groupsOrder []string
}

var _ States = &StatesBase{}

func (s *StatesBase) Names() S {
	return s.names
}

func (s *StatesBase) StateGroups() (map[string][]int, []string) {
	return s.groups, s.groupsOrder
}

func (s *StatesBase) SetNames(names S) {
	s.names = slicesUniq(names)
}

func (s *StatesBase) SetStateGroups(groups map[string][]int, order []string) {
	s.groups = groups
	s.groupsOrder = order
}

// States is the vase interface for schema states.
type States interface {
	// Names returns the state names of the state machine.
	Names() S
	StateGroups() (map[string][]int, []string)
	SetNames(S)
	SetStateGroups(map[string][]int, []string)
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "_am"

// AT represents typed arguments of pkg/machine, extracted from Event.Args
// via ParseArgs, or created manually to for Pass.
type AT struct {
	Err          error
	ErrTrace     string
	Panic        *ExceptionArgsPanic
	TargetStates S
	CalledStates S
	TimeBefore   Time
	TimeAfter    Time
	Event        *Event
	// MutDone chan gets closed by the machine once it's processed. Can cause chan
	// leaks when misused. Only for Can* checks.
	CheckDone *CheckDone
}

type ATRpc struct {
	Err          error
	ErrTrace     string
	Panic        *ExceptionArgsPanic
	TargetStates S
	CalledStates S
	TimeBefore   Time
	TimeAfter    Time
	Event        *Event
}

type CheckDone struct {
	// TODO close these on dispose and deadline
	Ch chan struct{}
	// Was the mutation canceled?
	Canceled bool
}

const (
	argErr       = "_am_err"
	argErrTrace  = "_am_errTrace"
	argPanic     = "_am_panic"
	argCheckDone = "_am_checkDone"
)

// ParseArgs extracts AT from A.
func ParseArgs(args A) *AT {
	ret := &AT{}

	if val, ok := args[argErr]; ok {
		ret.Err = val.(error)
	}
	if val, ok := args[argErrTrace]; ok {
		ret.ErrTrace = val.(string)
	}
	if val, ok := args[argPanic]; ok {
		ret.Panic = val.(*ExceptionArgsPanic)
	}
	if val, ok := args[argCheckDone]; ok {
		ret.CheckDone = val.(*CheckDone)
	}

	// TODO missing fields

	return ret
}

// Pass prepares A from AT, to pass to further mutations.
func Pass(args *AT) A {
	a := A{}

	if args.Err != nil {
		a[argErr] = args.Err
	}
	if args.ErrTrace != "" {
		a[argErrTrace] = args.ErrTrace
	}
	if args.Panic != nil {
		a[argPanic] = args.Panic
	}
	if args.CheckDone != nil {
		a[argCheckDone] = args.CheckDone
	}

	// TODO missing fields

	return a
}

// PassMerge prepares A from AT and existing A, to pass to further
// mutations.
func PassMerge(existing A, args *AT) A {
	var a A
	if existing == nil {
		a = A{}
	} else {
		a = maps.Clone(existing)
	}

	// unmarshal
	for k, v := range Pass(args) {
		a[k] = v
	}

	return a
}
