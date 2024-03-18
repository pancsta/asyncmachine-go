// Package helpers provides some utility functions for asyncmachine, which are
// out of scope of the main package.
package helpers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func RaceCtx[T *any](ctx context.Context, ch chan T) (T, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case v := <-ch:
		return v, nil
	}
}

// NestedState forwards the mutation to one of the composed submachines. Parent
// state should be a Multi state and only called directly (not from a relation).
// TODO test case, solve locking by passing the event to the submachine
func NestedState(
	e *am.Event, strIDField string, machGetter func(id string) *am.Machine,
) (am.Result, chan struct{}, error) {
	// validate
	if e.Mutation().Type != am.MutationAdd {
		return am.Canceled, nil, fmt.Errorf(
			"unsupported nested mutation %s", e.Mutation().Type)
	}

	// extract ID from params
	strID, ok := e.Args[strIDField].(string)
	if !ok || strID == "" {
		return am.Canceled, nil, am.ErrInvalidArgs
	}

	// init vars
	submach := machGetter(strID)
	if submach == nil {
		return am.Canceled, nil, fmt.Errorf("submachine %s not found", strID)
	}
	state := e.Name[0 : len(e.Name)-5]

	// ignore active non-multi nested states
	tick := submach.Clock(state)
	isMulti := submach.GetStruct()[state].Multi
	if am.IsActiveTick(tick) && !isMulti {
		return am.Canceled, nil, fmt.Errorf("nested state %s is active", state)
	}

	// fwd the state
	res := submach.Add1(state, e.Args)

	// handle queuing with a timeout
	if res == am.Queued && isMulti {
		// wait for the state to be activated again
		when := submach.WhenTime(am.S{state}, am.T{tick + 2}, nil)
		return res, when, nil
	} else if res == am.Queued {
		when := submach.When1(state, nil)
		return res, when, nil
	}

	return res, nil, nil
}

// ErrFromCtxs returns the first non-nil error from a list of contexts.
func ErrFromCtxs(ctxs ...context.Context) error {
	for _, ctx := range ctxs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return nil
}

// StatesToIndexes converts a list of state names to a list of state indexes,
// for a given machine.
func StatesToIndexes(mach *am.Machine, states am.S) []int {
	var indexes []int
	for _, state := range states {
		indexes = append(indexes, slices.Index(mach.StateNames, state))
	}

	return indexes
}

// IndexesToStates converts a list of state indexes to a list of state names,
// for a given machine.
func IndexesToStates(mach *am.Machine, indexes []int) am.S {
	states := am.S{}
	for _, index := range indexes {
		states = append(states, mach.StateNames[index])
	}

	return states
}

// TimeMatrix returns a matrix of state clocks for the given machines.
func TimeMatrix(machines []*am.Machine) ([]am.T, error) {
	if len(machines) == 0 {
		return nil, errors.New("no machines provided")
	}

	matrix := make([]am.T, len(machines))
	prevLen := len(machines[0].GetStruct())
	for i, mach := range machines {
		if len(mach.GetStruct()) != prevLen {
			return nil, errors.New("machines have different state lengths")
		}
		matrix[i] = mach.Time(nil)
	}

	return matrix, nil
}
