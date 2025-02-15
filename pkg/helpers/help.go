// Package helpers is a set of useful functions when working with async state
// machines.
package helpers

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/retrypolicy"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

const (
	// EnvAmHealthcheck enables a healthcheck ticker for every debugged machine.
	EnvAmHealthcheck = "AM_HEALTHCHECK"
	// EnvAmTestRunner indicates the main test tunner, disables any telemetry.
	EnvAmTestRunner     = "AM_TEST_RUNNER"
	healthcheckInterval = 30 * time.Second
)

// Add1Block activates a state and waits until it becomes active. If it's a
// multi state, it also waits for it te de-activate. Returns early if a
// non-multi state is already active. Useful to avoid the queue.
func Add1Block(
	ctx context.Context, mach am.Api, state string, args am.A,
) am.Result {
	// TODO support args["sync_token"] via WhenArgs

	// support for multi states

	if IsMulti(mach, state) {
		when := mach.WhenTicks(state, 2, ctx)
		res := mach.Add1(state, args)
		<-when

		return res
	}

	if mach.Is1(state) {
		return am.ResultNoOp
	}

	ctxWhen, cancel := context.WithCancel(ctx)
	defer cancel()

	when := mach.WhenTicks(state, 1, ctxWhen)
	res := mach.Add1(state, args)
	if res == am.Canceled {
		// dispose "when" ch early
		cancel()

		return res
	}
	<-when

	return res
}

// Add1BlockCh is like Add1Block, but returns a channel to compose with other
// "when" methods.
func Add1BlockCh(
	ctx context.Context, mach am.Api, state string, args am.A,
) <-chan struct{} {
	// TODO support args["sync_token"] via WhenArgs

	// support for multi states

	if IsMulti(mach, state) {
		when := mach.WhenTicks(state, 2, ctx)
		_ = mach.Add1(state, args)

		return when
	}

	if mach.Is1(state) {
		return nil
	}

	ctxWhen, cancel := context.WithCancel(ctx)
	defer cancel()

	when := mach.WhenTicks(state, 1, ctxWhen)
	res := mach.Add1(state, args)
	if res == am.Canceled {
		// dispose "when" ch early
		cancel()

		return nil
	}

	return when
}

// Add1AsyncBlock adds a state from an async op and waits for another one
// from the op to become active. Theoretically, it should work with any state
// pair, including Multi states (assuming they remove themselves).
func Add1AsyncBlock(
	ctx context.Context, mach am.Api, waitState string,
	addState string, args am.A,
) am.Result {
	ticks := 1
	// wait 2 ticks for multi states
	if IsMulti(mach, waitState) {
		ticks = 2
	}

	ctxWhen, cancel := context.WithCancel(ctx)
	defer cancel()

	when := mach.WhenTicks(waitState, ticks, ctxWhen)
	res := mach.Add1(addState, args)
	if res == am.Canceled {
		// dispose "when" ch early
		cancel()

		return res
	}
	<-when

	return res
}

// TODO AddSync

// IsMulti returns true if a state is a multi state.
func IsMulti(mach am.Api, state string) bool {
	return mach.GetStruct()[state].Multi
}

// StatesToIndexes converts a list of state names to a list of state indexes,
// for a given machine.
func StatesToIndexes(allStates am.S, states am.S) []int {
	indexes := make([]int, len(states))
	for i, state := range states {
		indexes[i] = slices.Index(allStates, state)
	}

	return indexes
}

// IndexesToStates converts a list of state indexes to a list of state names,
// for a given machine.
func IndexesToStates(allStates am.S, indexes []int) am.S {
	states := make(am.S, len(indexes))
	for i, idx := range indexes {
		states[i] = allStates[idx]
	}

	return states
}

// MachDebug sets up a machine for debugging, based on the AM_DEBUG env var,
// passed am-dbg address, log level and stdout flag.
func MachDebug(
	mach am.Api, amDbgAddr string, logLvl am.LogLevel, stdout bool,
) {
	if IsTestRunner() {
		return
	}

	if stdout {
		mach.SetLogLevel(logLvl)
	} else {
		mach.SetLoggerEmpty(logLvl)
	}

	// TODO increate timeouts on AM_DEBUG

	if amDbgAddr == "" {
		return
	}

	// trace to telemetry
	err := telemetry.TransitionsToDbg(mach, amDbgAddr)
	if err != nil {
		panic(err)
	}

	if os.Getenv(EnvAmHealthcheck) != "" {
		Healthcheck(mach)
	}
}

// Healthcheck adds a state to a machine every 5 seconds, until the context is
// done. This makes sure all the logs are pushed to the telemetry server.
func Healthcheck(mach am.Api) {
	if !mach.Has1("Healthcheck") {
		return
	}

	go func() {
		t := time.NewTicker(healthcheckInterval)
		for {
			select {
			case <-t.C:
				mach.Add1(ss.BasicStates.Healthcheck, nil)
			case <-mach.Ctx().Done():
				t.Stop()
			}
		}
	}()
}

// MachDebugEnv sets up a machine for debugging, based on env vars only:
// AM_DBG_ADDR, AM_LOG, and AM_DEBUG. This function should be called right
// after the machine is created, to catch all the log entries.
func MachDebugEnv(mach am.Api) {
	amDbgAddr := os.Getenv(telemetry.EnvAmDbgAddr)
	logLvl := am.EnvLogLevel("")
	stdout := os.Getenv(am.EnvAmDebug) == "2"

	// inherit default
	if amDbgAddr == "1" {
		amDbgAddr = telemetry.DbgAddr
	}

	MachDebug(mach, amDbgAddr, logLvl, stdout)
}

// TODO StableWhen(dur, states, ctx) - like When, but makes sure the state is
// stable for the duration.

// NewReqAdd creates a new failsafe request to add states to a machine. See
// See MutRequest for more info and NewMutRequest for the defaults.
func NewReqAdd(mach am.Api, states am.S, args am.A) *MutRequest {
	return NewMutRequest(mach, am.MutationAdd, states, args)
}

// NewReqAdd1 creates a new failsafe request to add a single state to a machine.
// See MutRequest for more info and NewMutRequest for the defaults.
func NewReqAdd1(mach am.Api, state string, args am.A) *MutRequest {
	return NewReqAdd(mach, am.S{state}, args)
}

// NewReqRemove creates a new failsafe request to remove states from a machine.
// See MutRequest for more info and NewMutRequest for the defaults.
func NewReqRemove(mach am.Api, states am.S, args am.A) *MutRequest {
	return NewMutRequest(mach, am.MutationRemove, states, args)
}

// NewReqRemove1 creates a new failsafe request to remove a single state from a
// machine. See MutRequest for more info and NewMutRequest for the defaults.
func NewReqRemove1(mach am.Api, state string, args am.A) *MutRequest {
	return NewReqRemove(mach, am.S{state}, args)
}

// MutRequest is a failsafe request for a machine mutation. It supports retries,
// backoff, max duration, delay, and timeout policies. It will try to mutate
// the machine until the context is done, or the max duration is reached. Queued
// mutations are considered supported a success.
type MutRequest struct {
	Mach    am.Api
	MutType am.MutationType
	States  am.S
	Args    am.A

	// PolicyRetries is the max number of retries.
	PolicyRetries int
	// PolicyDelay is the delay before the first retry, then doubles.
	PolicyDelay time.Duration
	// PolicyBackoff is the max time to wait between retries.
	PolicyBackoff time.Duration
	// PolicyMaxDuration is the max time to wait for the mutation to be accepted.
	PolicyMaxDuration time.Duration
}

// NewMutRequest creates a new MutRequest with defaults - 10 retries, 100ms
// delay, 5s backoff, and 5s max duration.
func NewMutRequest(
	mach am.Api, mutType am.MutationType, states am.S, args am.A,
) *MutRequest {
	// TODO increase duration for AM_DEBUG

	return &MutRequest{
		Mach:    mach,
		MutType: mutType,
		States:  states,
		Args:    args,

		// defaults

		PolicyRetries:     10,
		PolicyDelay:       100 * time.Millisecond,
		PolicyBackoff:     5 * time.Second,
		PolicyMaxDuration: 5 * time.Second,
	}
}

func (r *MutRequest) Clone(
	mach am.Api, mutType am.MutationType, states am.S, args am.A,
) *MutRequest {
	return &MutRequest{
		Mach:    mach,
		MutType: mutType,
		States:  states,
		Args:    args,

		PolicyRetries:     r.PolicyRetries,
		PolicyBackoff:     r.PolicyBackoff,
		PolicyMaxDuration: r.PolicyMaxDuration,
		PolicyDelay:       r.PolicyDelay,
	}
}

func (r *MutRequest) Retries(retries int) *MutRequest {
	r.PolicyRetries = retries
	return r
}

func (r *MutRequest) Backoff(backoff time.Duration) *MutRequest {
	r.PolicyBackoff = backoff
	return r
}

func (r *MutRequest) MaxDuration(maxDuration time.Duration) *MutRequest {
	r.PolicyMaxDuration = maxDuration
	return r
}

func (r *MutRequest) Delay(delay time.Duration) *MutRequest {
	r.PolicyDelay = delay
	return r
}

func (r *MutRequest) Run(ctx context.Context) (am.Result, error) {
	// policies
	retry := retrypolicy.Builder[am.Result]().
		WithMaxDuration(r.PolicyMaxDuration).
		WithMaxRetries(r.PolicyRetries)

	if r.PolicyBackoff != 0 {
		retry = retry.WithBackoff(r.PolicyDelay, r.PolicyBackoff)
	} else {
		retry = retry.WithDelay(r.PolicyDelay)
	}

	res, err := failsafe.NewExecutor[am.Result](retry.Build()).WithContext(ctx).
		Get(r.get)

	return res, err
}

func (r *MutRequest) get() (am.Result, error) {
	res := r.Mach.Add(r.States, r.Args)
	if res == am.Canceled {
		return res, am.ErrCanceled
	}

	return res, nil
}

// Wait waits for a duration, or until the context is done. Returns nil if the
// duration has passed, or err is ctx is done.
func Wait(ctx context.Context, length time.Duration) bool {
	t := time.After(length)

	select {
	case <-ctx.Done():
		return false
	case <-t:
		return true
	}
}

// Interval runs a function at a given interval, for a given duration, or until
// the context is done. Returns nil if the duration has passed, or err is ctx is
// done. The function should return false to stop the interval.
func Interval(
	ctx context.Context, length time.Duration, interval time.Duration,
	fn func() bool,
) error {
	end := time.Now().Add(length)
	t := time.NewTicker(interval)

	for {
		select {

		case <-ctx.Done():
			t.Stop()
			return ctx.Err()

		case <-t.C:
			if time.Now().After(end) {
				t.Stop()
				return nil
			}

			if !fn() {
				t.Stop()
				return nil
			}
		}
	}
}

// WaitForAll waits for a list of channels to close, or until the context is
// done, or until the timeout is reached. Returns nil if all channels are
// closed, or ErrTimeout, or ctx.Err().
//
// It's advised to check the state ctx after this call, as it usually means
// expiration and not a timeout.
func WaitForAll(
	ctx context.Context, timeout time.Duration, chans ...<-chan struct{},
) error {
	// TODO test
	// TODO support mach disposal via am.ErrDisposed

	// exit early
	if len(chans) == 0 {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// timeout
	if IsDebug() {
		timeout = 100 * timeout
	}
	t := time.After(timeout)

	// wait on all chans
	for _, ch := range chans {
		select {
		case <-ctx.Done():
			// TODO check and log state ctx name
			return ctx.Err()
		case <-t:
			return am.ErrTimeout
		case <-ch:
			// pass
		}
	}

	return nil
}

// WaitForErrAll is like WaitForAll, but also waits on WhenErr of a passed
// machine. For state machines with error handling (like retry) it's recommended
// to measure machine time of [am.Exception] instead.
func WaitForErrAll(
	ctx context.Context, timeout time.Duration, mach am.Api,
	chans ...<-chan struct{},
) error {
	// TODO test

	// exit early
	if len(chans) == 0 {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// timeout
	if IsDebug() {
		timeout = 100 * timeout
	}
	t := time.After(timeout)
	whenErr := mach.WhenErr(ctx)

	// wait on all chans
	for _, ch := range chans {
		select {
		case <-ctx.Done():
			// TODO check and log state ctx name
			return ctx.Err()
		case <-whenErr:
			return fmt.Errorf("%s: %w", am.Exception, mach.Err())
		case <-t:
			return am.ErrTimeout
		case <-ch:
			// pass
		}
	}

	return nil
}

// WaitForAny waits for any of the channels to close, or until the context is
// done, or until the timeout is reached. Returns nil if any channel is
// closed, or ErrTimeout, or ctx.Err().
//
// It's advised to check the state ctx after this call, as it usually means
// expiration and not a timeout.
//
// This function uses reflection to wait for multiple channels at once.
func WaitForAny(
	ctx context.Context, timeout time.Duration, chans ...<-chan struct{},
) error {
	// TODO test
	// TODO reflection-less selectes for 1/2/3 chans
	// exit early
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if IsDebug() {
		timeout = 100 * timeout
	}
	t := time.After(timeout)

	// create select cases
	cases := make([]reflect.SelectCase, 2+len(chans))
	cases[0] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	}
	cases[1] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(t),
	}
	for i, ch := range chans {
		cases[i+2] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ch),
		}
	}

	// wait
	chosen, _, _ := reflect.Select(cases)

	switch chosen {
	case 0:
		// TODO check and log state ctx name
		return ctx.Err()
	case 1:
		return am.ErrTimeout
	default:
		return nil
	}
}

// WaitForErrAny is like WaitForAny, but also waits on WhenErr of a passed
// machine. For state machines with error handling (like retry) it's recommended
// to measure machine time of [am.Exception] instead.
func WaitForErrAny(
	ctx context.Context, timeout time.Duration, mach *am.Machine,
	chans ...<-chan struct{},
) error {
	// TODO test
	// TODO reflection-less selectes for 1/2/3 chans
	// exit early
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if IsDebug() {
		timeout = 100 * timeout
	}
	t := time.After(timeout)

	// create select cases
	predef := 3
	cases := make([]reflect.SelectCase, predef+len(chans))
	cases[0] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	}
	cases[1] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(t),
	}
	cases[2] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(t),
	}
	for i, ch := range chans {
		cases[predef+i] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ch),
		}
	}

	// wait
	chosen, _, _ := reflect.Select(cases)

	switch chosen {
	case 0:
		// TODO check and log state ctx name (if any)
		return ctx.Err()
	case 1:
		return am.ErrTimeout
	case 2:
		return mach.Err()
	default:
		return nil
	}
}

// Activations returns the number of state activations from an amount of ticks
// passed.
func Activations(u uint64) int {
	return int((u + 1) / 2)
}

// ExecAndClose closes the chan when the function ends.
func ExecAndClose(fn func()) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		fn()
		close(ch)
	}()

	return ch
}

// EnableDebugging sets env vars for debugging tested machines with am-dbg on
// port 6831.
func EnableDebugging(stdout bool) {
	if stdout {
		_ = os.Setenv(am.EnvAmDebug, "2")
	} else {
		_ = os.Setenv(am.EnvAmDebug, "1")
	}
	_ = os.Setenv(telemetry.EnvAmDbgAddr, "localhost:6831")
	_ = os.Setenv(am.EnvAmLog, "2")
	// _ = os.Setenv(EnvAmHealthcheck, "1")
}

// SetLogLevel sets AM_LOG env var to the passed log level. It will affect all
// future state machines using MachDebugEnv.
func SetLogLevel(level am.LogLevel) {
	_ = os.Setenv(am.EnvAmLog, strconv.Itoa(int(level)))
}

// Implements checks is statesChecked implement statesNeeded. It's an equivalent
// of Machine.Has(), but for slices of state names, and with better error msgs.
func Implements(statesChecked, statesNeeded am.S) error {
	for _, state := range statesNeeded {
		if !slices.Contains(statesChecked, state) {
			return errors.New("missing state: " + state)
		}
	}

	return nil
}

// ArgsToLogMap converts an [A] (arguments) struct to a map of strings using
// `log` tags as keys, and their cased string values.
func ArgsToLogMap(s interface{}) map[string]string {
	result := make(map[string]string)
	val := reflect.ValueOf(s).Elem()
	if !val.IsValid() {
		return result
	}
	typ := reflect.TypeOf(s).Elem()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		tag := typ.Field(i).Tag.Get("log")

		if tag != "" {
			v, _ := field.Interface().(string)
			if v == "" {
				continue
			}
			result[tag] = field.Interface().(string)
		}
	}

	return result
}

// ArgsToArgs converts [A] (arguments) into an overlapping [A]. Useful for
// removing fields which can't be passed over RPC, and back. Both params should
// be pointers to struct and share at least one field.
func ArgsToArgs[T any](src interface{}, dest T) T {
	srcVal := reflect.ValueOf(src).Elem()
	destVal := reflect.ValueOf(dest).Elem()

	for i := 0; i < srcVal.NumField(); i++ {
		srcField := srcVal.Field(i)
		destField := destVal.FieldByName(srcVal.Type().Field(i).Name)

		if destField.IsValid() && destField.CanSet() {
			destField.Set(srcField)
		}
	}

	return dest
}

// IsDebug returns true if the process is in simple debug mode.
func IsDebug() bool {
	return os.Getenv(am.EnvAmDebug) != "" && !IsTestRunner()
}

// IsTelemetry returns true if the process is in telemetry debug mode.
func IsTelemetry() bool {
	return os.Getenv(telemetry.EnvAmDbgAddr) != "" && !IsTestRunner()
}

func IsTestRunner() bool {
	return os.Getenv(EnvAmTestRunner) != ""
}

// GroupWhen1 will create wait channels for the same state in a group of
// machines, or return a [am.ErrStateMissing].
func GroupWhen1(
	machs []am.Api, state string, ctx context.Context,
) ([]<-chan struct{}, error) {
	// validate states
	for _, mach := range machs {
		if !mach.Has1(state) {
			return nil, fmt.Errorf(
				"%w: %s in machine %s", am.ErrStateMissing, state, mach.Id())
		}
	}

	// create chans
	var chans []<-chan struct{}
	for _, mach := range machs {
		chans = append(chans, mach.When1(state, ctx))
	}

	return chans, nil
}

// RemoveMulti creates a final handler which removes a multi state from a
// machine. Useful to avoid FooState-Remove1-Foo repetition.
func RemoveMulti(mach am.Api, state string) am.HandlerFinal {
	return func(_ *am.Event) {
		mach.Remove1(state, nil)
	}
}

// GetTransitionStates will extract added, removed, and touched states from
// transition's clock values and steps. Requires a state names index.
func GetTransitionStates(
	tx *am.Transition, index am.S,
) (added am.S, removed am.S, touched am.S) {
	before := tx.TimeBefore
	after := tx.TimeAfter

	is := func(time am.Time, i int) bool {
		return time != nil && am.IsActiveTick(time[i])
	}

	for i, name := range index {
		if is(before, i) && !is(after, i) {
			removed = append(removed, name)
		} else if !is(before, i) && is(after, i) {
			added = append(added, name)
		} else if before != nil && before[i] != after[i] {
			// treat multi states as added
			added = append(added, name)
		}
	}

	// touched
	touched = am.S{}
	for _, step := range tx.Steps {
		if step.FromState != "" {
			touched = append(touched, step.FromState)
		}
		if step.ToState != "" {
			touched = append(touched, step.ToState)
		}
	}

	return added, removed, touched
}

// TODO
// func Batch(input <-chan any, state string, arg string, window time.Duration,
//   maxElement int) {
//
// }

type FanFn func(num int, state, stateDone string)

type FanHandlers struct {
	Concurrency int
	AnyState    am.HandlerFinal
	GroupTasks  am.S
	GroupDone   am.S
}

// FanOutIn creates [total] numer of state pairs of "Name1" and "Name1Done", as
// well as init and merge states ("Name", "NameDone"). [name] is treated as a
// namespace and can't have other states within. Retry can be achieved by adding
// the init state repetively. FanOutIn can be chained, but it should be called
// before any mutations or telemetry (as it changes the state struct). The
// returned handlers struct can be used to adjust concurrency level.
func FanOutIn(
	mach *am.Machine, name string, total, concurrency int,
	fn FanFn,
) (any, error) {
	states := mach.GetStruct()
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
	err := mach.SetStruct(states, names)
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
				running := mach.CountActive(groupTasks)
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
