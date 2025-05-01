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
) (am.Result, <-chan struct{}, error) {
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
	tick := submach.Tick(state)
	isMulti := submach.Schema()[state].Multi
	if am.IsActiveTick(tick) && !isMulti {
		return am.Canceled, nil, fmt.Errorf("nested state %s is active", state)
	}

	// fwd the state
	res := submach.Add1(state, e.Args)

	// handle queuing with a timeout
	if res == am.Queued && isMulti {
		// wait for the state to be activated again
		when := submach.WhenTime(am.S{state}, am.Time{tick + 2}, nil)
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

// TimeMatrix returns a matrix of state clocks for the given machines.
func TimeMatrix(machines []*am.Machine) ([]am.Time, error) {
	if len(machines) == 0 {
		return nil, errors.New("no machines provided")
	}

	matrix := make([]am.Time, len(machines))
	prevLen := len(machines[0].Schema())
	for i, mach := range machines {
		if len(mach.Schema()) != prevLen {
			return nil, errors.New("machines have different state lengths")
		}
		matrix[i] = mach.Time(nil)
	}

	return matrix, nil
}

func RelationsMatrix(mach *am.Machine) ([][]int, error) {
	names := mach.StateNames()
	shiftRow := 2
	appendRows := 2
	matrix := make([][]int, shiftRow+len(names)+appendRows)

	for i, s1 := range names {
		matrix[i+shiftRow] = make([]int, len(names))

		// -1 shiftRows
		if i < shiftRow {
			matrix[i] = make([]int, len(names))

			for j := range matrix[i] {
				matrix[i][j] = -1
			}
		}

		for j, s2 := range names {

			rels, err := mach.Resolver().GetRelationsBetween(s1, s2)
			if err != nil {
				return nil, err
			}
			for _, rel := range rels {
				matrix[i+shiftRow][j] += int(rel)
			}
		}
	}

	// -1 shiftRows
	row := len(names) + shiftRow
	for i := row; i < row+appendRows; i++ {
		matrix[i] = make([]int, len(names))

		for j := range matrix[i] {
			matrix[i][j] = -1
		}
	}

	return matrix, nil
}

func TransitionMatrix(tx, prevTx *am.Transition, index am.S) ([][]int, error) {

	shiftRow := 2
	appendRows := 2
	matrix := make([][]int, shiftRow+len(index)+appendRows)

	// row 0: currently set
	matrix[0] = make([]int, len(index))
	for i := range index {

		matrix[0][i] = 0
		if am.IsActiveTick(tx.TimeBefore[i]) {
			matrix[0][i] = 1
		}
	}

	// row 1: called states
	matrix[1] = make([]int, len(index))
	for i, name := range index {
		v := 0
		if slices.Contains(tx.CalledStates(), name) {
			v = 1
		}

		matrix[1][i] = v
	}

	// steps
	for iRow, name := range index {
		matrix[shiftRow+iRow] = make([]int, len(index))

		for iCol, source := range index {
			v := 0

			for _, step := range tx.Steps {

				// TODO style just the cells
				if step.FromState == source &&
					((step.ToState == "" && source == name) ||
						step.ToState == name) {
					v += int(step.Type)
				}

				matrix[shiftRow+iRow][iCol] = v
			}
		}
	}

	// ticks
	row := len(index) + shiftRow
	matrix[row] = make([]int, len(index))
	for i := range index {

		var pTick uint64
		if prevTx != nil {
			pTick = prevTx.TimeAfter[i]
		}
		tick := tx.TimeAfter[i]
		v := tick - pTick

		// add row
		matrix[row][i] = int(v)
	}

	// active after
	row = len(index) + shiftRow + 1
	matrix[row] = make([]int, len(index))
	for i := range index {

		matrix[row][i] = 0
		if am.IsActiveTick(tx.TimeAfter[i]) {
			matrix[row][i] = 1
		}
	}

	return matrix, nil
}
