package machine

import (
	"maps"
)

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

// AT represents typed arguments of pkg/machine, extracted from Event.Args
// via ParseArgs, or created manually to for Pass.
type AT struct {
	Err      error
	ErrTrace string
	Panic    *ExceptionArgsPanic
	// TODO
	// Event
}

// ParseArgs extracts AT from A.
func ParseArgs(args A) *AT {
	ret := &AT{}

	if val, ok := args["err"]; ok {
		ret.Err = val.(error)
	}
	if val, ok := args["err.trace"]; ok {
		ret.ErrTrace = val.(string)
	}
	if val, ok := args["panic"]; ok {
		ret.Panic = val.(*ExceptionArgsPanic)
	}

	return ret
}

// Pass prepares A from AT, to pass to further mutations.
func Pass(args *AT) A {
	a := A{}

	if args.Err != nil {
		a["err"] = args.Err
	}
	if args.ErrTrace != "" {
		a["err.trace"] = args.ErrTrace
	}
	if args.Panic != nil {
		a["panic"] = args.Panic
	}

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

	if args.Err != nil {
		a["err"] = args.Err
	}
	if args.ErrTrace != "" {
		a["err.trace"] = args.ErrTrace
	}
	if args.Panic != nil {
		a["panic"] = args.Panic
	}

	return a
}
