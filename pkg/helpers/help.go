// Package helpers is a set of useful functions when working with async state
// machines.
package helpers

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/retrypolicy"
	"github.com/pancsta/asyncmachine-go/internal/utils"
	"github.com/pancsta/asyncmachine-go/pkg/states/pipes"
	"golang.org/x/sync/errgroup"

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

type (
	S      = am.S
	A      = am.A
	Schema = am.Schema
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
	return mach.Schema()[state].Multi
}

// StatesToIndexes converts a list of state names to a list of state indexes,
// for the given machine. It returns -1 for unknown states.
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
		if idx == -1 {
			states[i] = "unknown" + strconv.Itoa(i)
			continue
		}
		states[i] = allStates[idx]
	}

	return states
}

// MachDebug sets up a machine for debugging, based on the AM_DEBUG env var,
// passed am-dbg address, log level, and stdout flag.
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

	if amDbgAddr == "" {
		return
	}

	// trace to telemetry
	err := telemetry.TransitionsToDbg(mach, amDbgAddr)
	if err != nil {
		// TODO dont panic
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
//  stable for the duration.

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
	return res, ResultToErr(res)
}

// Wait waits for a duration, or until the context is done. Returns true if the
// duration has passed, or false if ctx is done.
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

// Activations return the number of state activations from the number of ticks
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
func ArgsToLogMap(args interface{}, maxLen int) map[string]string {
	if maxLen == 0 {
		maxLen = max(4, am.LogArgsMaxLen)
	}
	skipMaxLen := false
	result := make(map[string]string)
	val := reflect.ValueOf(args).Elem()
	if !val.IsValid() {
		return result
	}
	typ := reflect.TypeOf(args).Elem()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		key := typ.Field(i).Tag.Get("log")
		if key == "" {
			continue
		}

		// check if the field is of a known type
		switch v := field.Interface().(type) {
		// strings
		case string:
			if v == "" {
				continue
			}
			result[key] = v
		case []string:
			// combine []string into a single comma-separated string
			if len(v) == 0 {
				continue
			}
			skipMaxLen = true
			txt := ""

			ii := 0
			for _, el := range v {
				if reflect.ValueOf(v).IsNil() {
					continue
				}
				if txt != "" {
					txt += ", "
				}

				txt += `"` + am.TruncateStr(el, maxLen/2) + `"`
				if ii >= maxLen/2 {
					txt += fmt.Sprintf(" ... (%d more)", len(v)-ii)
					break
				}

				ii++
			}
			if txt == "" {
				continue
			}
			result[key] = txt

		case bool:
			// skip default
			if !v {
				continue
			}
			result[key] = fmt.Sprintf("%v", v)
		case []bool:
			// combine []bool into a single comma-separated string
			if len(v) == 0 {
				continue
			}
			result[key] = fmt.Sprintf("%v", v)
			// TODO fix highlighting issues in am-dbg
			result[key] = strings.Trim(result[key], "[]")

		case int:
			// skip default
			if v == 0 {
				continue
			}
			result[key] = fmt.Sprintf("%d", v)
		case []int:
			// combine []int into a single comma-separated string
			if len(v) == 0 {
				continue
			}
			result[key] = fmt.Sprintf("%d", v)
			// TODO fix highlighting issues in am-dbg
			result[key] = strings.Trim(result[key], "[]")

			// duration
		case time.Duration:
			if v.Seconds() == 0 {
				continue
			}
			result[key] = v.String()

			// String() method
		case fmt.Stringer:
			if reflect.ValueOf(v).IsNil() {
				continue
			}
			txt := v.String()
			if txt == "" {
				continue
			}
			result[key] = txt

			// skip unknown types, besides []fmt.Stringer
		default:
			if field.Kind() != reflect.Slice {
				continue
			}

			valLen := field.Len()
			skipMaxLen = true
			txt := ""
			ii := 0

			for i := 0; i < valLen; i++ {
				el := field.Index(i).Interface()
				s, ok := el.(fmt.Stringer)
				if ok && s.String() != "" {
					if txt != "" {
						txt += ", "
					}
					txt += `"` + am.TruncateStr(s.String(), maxLen/2) + `"`
					if i >= maxLen/2 {
						txt += fmt.Sprintf(" ... (%d more)", valLen-ii)
						break
					}
				}

				ii++
			}

			if txt == "" {
				continue
			}
			result[key] = txt
		}

		result[key] = strings.ReplaceAll(result[key], "\n", " ")
		if !skipMaxLen && len(result[key]) > maxLen {
			result[key] = am.TruncateStr(result[key], maxLen)
		}
	}

	return result
}

// ArgsToArgs converts [am.A] (arguments) into an overlapping [am.A]. Useful for
// removing fields which can't be passed over RPC, and back. Both params should
// be pointers to a struct and share at least one field.
func ArgsToArgs[T any](src interface{}, dest T) T {
	// TODO test
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

// TODO func WhenAny1(mach am.Api, states am.S, ctx context.Context)
//  []<-chan struct{}

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
		return time != nil && am.IsActiveTick(time.Get(i))
	}

	for i, name := range index {
		if is(before, i) && !is(after, i) {
			removed = append(removed, name)
		} else if !is(before, i) && is(after, i) {
			added = append(added, name)
		} else if before != nil && before.Get(i) != after.Get(i) {
			// treat multi states as added
			added = append(added, name)
		}
	}

	// touched
	touched = am.S{}
	for _, step := range tx.Steps {
		if s := step.GetFromState(index); s != "" {
			touched = append(touched, s)
		}
		if s := step.GetToState(index); s != "" {
			touched = append(touched, s)
		}
	}

	return added, removed, touched
}

// TODO batch and merge with am-dbg
// func Batch(input <-chan any, state string, arg string, window time.Duration,
//   maxElement int) {
//
// }

func ResultToErr(result am.Result) error {
	switch result {
	case am.Canceled:
		return am.ErrCanceled
	// case am.Queued:
	// 	return am.ErrQueued
	default:
		return nil
	}
}

type MachGroup []am.Api

func (g *MachGroup) Is1(state string) bool {
	if g == nil {
		return false
	}

	for _, m := range *g {
		if m.Not1(state) {
			return false
		}
	}

	return true
}

func Pool(limit int) *errgroup.Group {
	g := &errgroup.Group{}
	g.SetLimit(limit)
	return g
}

// Cond is a set of state conditions, which when all met make the condition
// true.
type Cond struct {
	// TODO IsMatch, AnyMatch, ... for regexps

	// Only if all these states are active.
	Is S
	// TODO implement
	// Only if any of these groups of states are active.
	Any []S
	// Only if any of these states is active.
	Any1 S
	// Only if none of these states are active.
	Not S
	// Only if the clock is equal or higher then.
	Clock am.Clock

	// TODO time queries
	// Query string
	// AnyQuery string
	// NotQuery string
}

func (c Cond) String() string {
	return fmt.Sprintf("is: %s, any: %s, not: %s, clock: %v",
		c.Is, c.Any1, c.Not, c.Clock)
}

// Check compares the specified conditions against the passed machine. When mach
// is nil, Check returns false.
func (c Cond) Check(mach am.Api) bool {
	if mach == nil {
		return false
	}

	if !mach.Is(c.Is) {
		return false
	}
	if mach.Any1(c.Not...) {
		return false
	}
	if len(c.Any1) > 0 && !mach.Any1(c.Any1...) {
		return false
	}
	if !mach.WasClock(c.Clock) {
		return false
	}

	return true
}

// IsEmpty returns false if no condition is defined.
func (c Cond) IsEmpty() bool {
	return c.Is == nil && c.Any1 == nil && c.Not == nil && c.Clock == nil
}

// TODO thread safety via atomics
type StateLoop struct {
	loopState string
	ctxStates am.S
	mach      am.Api
	ended     bool
	interval  time.Duration
	threshold int
	check     func() bool

	lastSTime uint64
	lastHTime time.Time
	// mach time of [ctxStates] when started
	startSTime uint64
	// Start Human Time
	startHTime time.Time
}

func (l *StateLoop) String() string {
	ok := "ok"
	if l.ended {
		ok = "ended"
	}

	return fmt.Sprintf("StateLoop: %s for %s/%s", ok, l.mach.Id(), l.loopState)
}

// Break breaks the loop.
func (l *StateLoop) Break() {
	l.ended = true
	l.mach.Log(l.String())
}

// Sum returns a sum of state time from all context states.
func (l *StateLoop) Sum() uint64 {
	return l.mach.TimeSum(l.ctxStates)
}

// Ok returns true if the loop should continue.
func (l *StateLoop) Ok(ctx context.Context) bool {
	if l.ended {
		return false
	} else if ctx != nil && ctx.Err() != nil {
		err := fmt.Errorf("loop: arg ctx expired for %s/%s", l.mach.Id(),
			l.loopState)
		l.mach.AddErr(err, nil)
		l.ended = true

		return false

	} else if l.mach.Not1(l.loopState) {
		err := fmt.Errorf("loop: state ctx expired for %s/%s", l.mach.Id(),
			l.loopState)
		l.mach.AddErr(err, nil)
		l.ended = true

		return false
	}

	// stop on a function check
	if l.check != nil && !l.check() {
		l.ended = true

		return false
	}

	// reset counters on a new interval window
	sum := l.mach.TimeSum(l.ctxStates)
	if time.Since(l.lastHTime) > l.interval {
		l.lastHTime = time.Now()
		l.lastSTime = sum

		return true

		// check the current interval window
	} else if int(sum) > l.threshold {
		err := fmt.Errorf("loop: threshold exceeded for %s/%s", l.mach.Id(),
			l.loopState)
		l.mach.AddErr(err, nil)
		l.ended = true

		return false
	}

	l.lastSTime = sum

	return true
}

// Ended returns the ended flag, but does not any context. Useful for
// negotiation handles which don't have state context yet.
func (l *StateLoop) Ended() bool {
	return l.ended
}

// NewStateLoop helper creates a state loop guard bound to a specific state
// (eg Heartbeat), preventing infinite loops. It monitors context, off states,
// ticks of related "context states", and an optional check function.
// Not thread safe ATM.
func NewStateLoop(
	mach *am.Machine, loopState string, optCheck func() bool,
) *StateLoop {
	schema := mach.Schema()
	mach.MustParseStates(S{loopState})

	// collect related states
	ctxStates := S{loopState}

	// collect dependencies of the loopState
	ctxStates = append(ctxStates, schema[loopState].Require...)

	// collect states adding the loop state
	resolver := mach.Resolver()
	inbound, _ := resolver.InboundRelationsOf(loopState)
	for _, name := range inbound {
		rels, _ := resolver.RelationsBetween(name, loopState)
		if len(rels) > 0 {
			ctxStates = append(ctxStates, name)
		}
	}

	l := &StateLoop{
		loopState:  loopState,
		mach:       mach,
		ctxStates:  ctxStates,
		startHTime: time.Now(),
		startSTime: mach.TimeSum(ctxStates),
		// TODO config
		interval: time.Second,
		// TODO config
		threshold: 500,
		check:     optCheck,
	}
	mach.Log(l.String())

	return l
}

// SLOG

var SlogToMachLogOpts = &slog.HandlerOptions{
	ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		// omit these
		if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
			return slog.Attr{}
		}
		return a
	},
}

type SlogToMachLog struct {
	Mach am.Api
}

func (l SlogToMachLog) Write(p []byte) (n int, err error) {
	s, _ := strings.CutPrefix(string(p), "msg=")
	l.Mach.Log(s)
	return len(p), nil
}

// STATE UTILS

// TagValue returns the value part from a text tag "key:value". For tag without
// value, it returns the tag name.
func TagValue(tags []string, key string) string {
	for _, t := range tags {
		// no value
		if t == key {
			return key
		}

		// check value
		p := key + ":"
		if !strings.HasPrefix(t, p) {
			continue
		}

		val, _ := strings.CutPrefix(t, p)
		return val
	}

	return ""
}

// PrefixStates will prefix all state names with [prefix]. removeDups will skip
// overlaps eg "FooFooName" will be "Foo".
func PrefixStates(
	schema am.Schema, prefix string, removeDups bool, optWhitelist,
	optBlacklist S,
) am.Schema {
	schema = am.CloneSchema(schema)

	for name, s := range schema {
		if len(optWhitelist) > 0 && !slices.Contains(optWhitelist, name) {
			continue
		} else if len(optBlacklist) > 0 && slices.Contains(optBlacklist, name) {
			continue
		}

		for i, r := range s.After {
			newName := r
			if !removeDups || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.After[i] = newName
		}
		for i, r := range s.Add {
			newName := r
			if !removeDups || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.Add[i] = newName
		}
		for i, r := range s.Remove {
			newName := r
			if !removeDups || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.Remove[i] = newName
		}
		for i, r := range s.Require {
			newName := r
			if !removeDups || !strings.HasPrefix(name, prefix) {
				newName = prefix + r
			}
			s.Require[i] = newName
		}

		newName := name
		if !removeDups || !strings.HasPrefix(name, prefix) {
			newName = prefix + name
		}

		// replace
		delete(schema, name)
		schema[newName] = s
	}

	return schema
}

// CountRelations will count all referenced states in all relations of the
// given state.
func CountRelations(state *am.State) int {
	return len(state.Remove) + len(state.Add) + len(state.Require) +
		len(state.After)
}

// NewMirror creates a submachine which mirrors the given source machine. If
// [flat] is true, only mutations changing the state will be propagated, along
// with the currently active states.
//
// At this point, the handlers' struct needs to be defined manually with fields
// of type `am.HandlerFinal`.
//
// [id] is optional.
func NewMirror(
	id string, flat bool, source *am.Machine, handlers any, states am.S,
) (*am.Machine, error) {
	// TODO create handlers
	// TODO dont create a new machine, add to an existing one

	v := reflect.ValueOf(handlers)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return nil, errors.New("BindHandlers expects a pointer to a struct")
	}
	vElem := v.Elem()

	// detect methods
	var methodNames []string
	methodNames, err := am.ListHandlers(handlers, states)
	if err != nil {
		return nil, fmt.Errorf("listing handlers: %w", err)
	}

	// TODO support am.Api
	if id == "" {
		id = "mirror-" + source.Id()
	}
	sourceSchema := source.Schema()
	names := am.S{am.Exception}
	schema := am.Schema{}
	for _, name := range states {
		schema[name] = am.State{
			Multi: sourceSchema[name].Multi,
		}
		names = append(names, name)
	}
	mirror := am.New(source.Ctx(), schema, &am.Opts{
		Id:     id,
		Parent: source,
	})

	// set up pipes TODO loop over handlers
	for _, method := range methodNames {
		var state string
		var isAdd bool
		field := vElem.FieldByName(method)

		// check handler method
		if strings.HasSuffix(method, am.SuffixState) {
			state = method[:len(method)-len(am.SuffixState)]
			isAdd = true
		} else if strings.HasSuffix(method, am.SuffixEnd) {
			state = method[:len(method)-len(am.SuffixEnd)]
		} else {
			return nil, fmt.Errorf("unsupported handler %s for %s", method, id)
		}

		// pipe
		var p am.HandlerFinal
		if flat {
			// sync active for flats
			if source.Is1(state) {
				mirror.Add1(state, nil)
			}
			if isAdd {
				p = pipes.AddFlat(source, mirror, state, "")
			} else {
				p = pipes.RemoveFlat(source, mirror, state, "")
			}

		} else {
			if isAdd {
				p = pipes.Add(source, mirror, state, "")
			} else {
				p = pipes.Remove(source, mirror, state, "")
			}
		}

		field.Set(reflect.ValueOf(p))
	}

	// bind pipe handlers
	if err := source.BindHandlers(handlers); err != nil {
		return nil, err
	}

	return mirror, nil
}

// CopySchema copies states from the source to target schema, from the passed
// list of states. Returns a list of copied states, and an error. CopySchema
// verifies states.
func CopySchema(source am.Schema, target *am.Machine, states am.S) error {
	if len(states) == 0 {
		return nil
	}

	newSchema := target.Schema()
	for _, name := range states {
		if _, ok := source[name]; !ok {
			return fmt.Errorf("%w: state %s in source schema",
				am.ErrStateMissing, name)
		}

		newSchema[name] = source[name]
	}
	newStates := utils.SlicesUniq(slices.Concat(target.StateNames(), states))

	return target.SetSchema(newSchema, newStates)
}

// SchemaHash computes an MD5 hash of the passed schema. The order of states
// is not important.
func SchemaHash(schema am.Schema) string {
	ret := ""
	keys := slices.Collect(maps.Keys(schema))
	sort.Strings(keys)
	for _, k := range keys {
		ret += k + ":"

		// properties
		if schema[k].Auto {
			ret += "a,"
		}
		if schema[k].Multi {
			ret += "m,"
		}

		// relations
		after := slices.Clone(schema[k].After)
		sort.Strings(after)
		for _, r := range after {
			ret += r + ","
		}
		ret += ";"

		remove := slices.Clone(schema[k].Remove)
		sort.Strings(remove)
		for _, r := range remove {
			ret += r + ","
		}
		ret += ";"

		add := slices.Clone(schema[k].Add)
		sort.Strings(add)
		for _, r := range add {
			ret += r + ","
		}
		ret += ";"

		require := slices.Clone(schema[k].Require)
		sort.Strings(require)
		for _, r := range require {
			ret += r + ","
		}
		ret += ";"
	}

	hasher := md5.New()
	hasher.Write([]byte(ret))

	return hex.EncodeToString(hasher.Sum(nil))
}
