//go:build !tinygo

package helpers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

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

				txt += `"` + utils.TruncateStr(el, maxLen/2) + `"`
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

			// MutString() method
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
					txt += `"` + utils.TruncateStr(s.String(), maxLen/2) + `"`
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
			result[key] = utils.TruncateStr(result[key], maxLen)
		}
	}

	return result
}

// ArgsToArgs converts a typed arguments struct into an overlapping typed
// arguments struct. Useful for removing fields which can't be passed over RPC,
// and back. Both params should be pointers to a struct and share at least one
// field.
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

// NewMirror creates a submachine which mirrors the given source machine. If
// [flat] is true, only mutations changing the state will be propagated, along
// with the currently active states.
//
// At this point, the handlers' struct needs to be defined manually with fields
// of type [am.HandlerFinal].
//
// [id] is optional.
func NewMirror(
	id string, flat bool, source *am.Machine, handlers any, states am.S,
) (*am.Machine, error) {
	// TODO remove in favor or ampipes.BindAny?
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
	names := am.S{am.StateException}
	schema := am.Schema{}
	for _, name := range states {
		schema[name] = am.State{
			Multi: sourceSchema[name].Multi,
		}
		names = append(names, name)
	}
	mirror := am.New(source.Context(), schema, &am.Opts{
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
				p = ampipe.AddFlat(source, mirror, state, "")
			} else {
				p = ampipe.RemoveFlat(source, mirror, state, "")
			}

		} else {
			if isAdd {
				p = ampipe.Add(source, mirror, state, "")
			} else {
				p = ampipe.Remove(source, mirror, state, "")
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

// WaitForErrAny is like WaitForAny, but also waits on WhenErr of the passed
// machine. For state machines with error handling (like retry) it's recommended
// to measure machine time of [am.StateException] instead.
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
		Chan: reflect.ValueOf(mach.WhenErr(ctx)),
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
		// TODO check ctx first?
		return mach.Err()
	default:
		return nil
	}
}
