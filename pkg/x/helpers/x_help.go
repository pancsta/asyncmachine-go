// Package helpers provides experimental and unstable helpers.
package helpers

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"strings"

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
	if am.IsQueued(res) && isMulti {
		// wait for the state to be activated again
		when := submach.WhenTime(am.S{state}, am.Time{tick + 2}, nil)
		return res, when, nil
	} else if am.IsQueued(res) {
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

			rels, err := mach.Resolver().RelationsBetween(s1, s2)
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

type FanFn func(num int, state, stateDone string)

type FanHandlers struct {
	Concurrency int
	AnyState    am.HandlerFinal
	GroupTasks  am.S
	GroupDone   am.S
}

// FanOutIn creates [total] number of state pairs of "Name1" and "Name1Done", as
// well as init and merge states ("Name", "NameDone"). [name] is treated as a
// namespace and can't have other states within. Retry can be achieved by adding
// the init state repetively. FanOutIn can be chained, but it should be called
// before any mutations or telemetry (as it changes the state struct). The
// returned handlers struct can be used to adjust the concurrency level.
func FanOutIn(
	mach *am.Machine, name string, total, concurrency int,
	fn FanFn,
) (any, error) {
	// TODO version with worker as states, instead of tasks as states
	//  main state tries to push a list of tasks through the "worker states",
	//  which results in a Done state.
	states := mach.Schema()
	suffixDone := "Done"

	if !mach.Has(am.S{name, name + suffixDone}) {
		return nil, fmt.Errorf("%w: %s", am.ErrStateMissing, name)
	}
	base := states[name]

	// create task states
	groupTasks := make(am.S, total)
	groupDone := make(am.S, total)
	for i := range total {
		num := strconv.Itoa(i)
		groupTasks[i] = name + num
		groupDone[i] = name + num + suffixDone
	}
	for i := range total {
		num := strconv.Itoa(i)
		// task state
		states[name+num] = base
		// task completed state, removes task state
		states[name+num+suffixDone] = am.State{Remove: am.S{name + num}}
	}

	// inject into a fan-in state
	done := am.StateAdd(states[name+suffixDone], am.State{
		Require: groupDone,
		Remove:  am.S{name},
	})
	done.Auto = true
	states[name+suffixDone] = done

	names := slices.Concat(mach.StateNames(), groupTasks, groupDone)
	err := mach.SetSchema(states, names)
	if err != nil {
		return nil, err
	}

	// handlers
	h := FanHandlers{
		Concurrency: concurrency,
		GroupTasks:  groupTasks,
		GroupDone:   groupDone,
	}

	// simulate fanout states and the init state
	// Task(e)
	// Task1(e)
	// Task2(e)
	h.AnyState = func(e *am.Event) {
		// get tx info
		called := e.Transition().CalledStates()
		if len(called) < 1 {
			return
		}

		for _, state := range called {

			// fan-out state handler, eg Task1
			// TODO extract to IsFanOutHandler(called am.S, false) bool
			if strings.HasPrefix(state, name) && len(state) > len(name) &&
				!strings.HasSuffix(state, suffixDone) {

				iStr, _ := strings.CutPrefix(state, name)
				num, err := strconv.Atoi(iStr)
				if err != nil {
					// TODO
					panic(err)
				}

				// call the function and mark as done
				fn(num, state, state+suffixDone)
			}

			// fan-out done handler, eg Task1Done
			// TODO extract to IsFanOutHandler(called am.S, true) bool
			if strings.HasPrefix(state, name) && len(state) > len(name) &&
				strings.HasSuffix(state, suffixDone) {

				// call the init state
				mach.Add1(name, nil)
			}

			// fan-out init handler, eg Task
			if state == name {

				// gather remaining ones
				var remaining am.S
				for _, name := range groupTasks {
					// check if done or in progress
					if mach.Any1(name+suffixDone, name) {
						continue
					}
					remaining = append(remaining, name)
				}

				// start N threads
				running := len(mach.ActiveStates(groupTasks))
				// TODO check against running via mach.CountActive
				for i := running; i < h.Concurrency && len(remaining) > 0; i++ {

					// add a random one
					idx := rand.Intn(len(remaining))
					mach.Add1(remaining[idx], nil)
					remaining = slices.Delete(remaining, idx, idx+1)
				}
			}
		}
	}

	err = mach.BindHandlers(&h)
	if err != nil {
		return nil, err
	}

	return h, nil
}
