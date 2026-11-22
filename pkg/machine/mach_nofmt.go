// Local copy of the internal nofmt formatter for pkg/machine. pkg/machine must
// not import internal packages, so keep this file in sync with the original.
// It keeps error wrapping compatible with fmt.Errorf (%w, errors.Is,
// errors.As).
package machine

import (
	"errors"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// stringer is the local equivalent of fmt.Stringer.
type stringer interface {
	String() string
}

// sprintf formats according to a format specifier and returns the resulting
// string. Supported verbs: %s, %d, %v, %+v, %w, and %%.
func sprintf(format string, args ...any) string {
	out, _ := formatString(format, args)
	return out
}

// errorf formats according to a format specifier and returns an error. %w
// wraps the corresponding error, including multiple %w operands.
func errorf(format string, args ...any) error {
	msg, wrapped := formatString(format, args)
	switch len(wrapped) {
	case 0:
		return errors.New(msg)
	case 1:
		return &wrapError{msg: msg, err: wrapped[0]}
	default:
		return &wrapErrors{msg: msg, errs: wrapped}
	}
}

// sprint formats operands using their default %v representation. Spaces are
// added between operands when neither is a string, like fmt.Sprint.
func sprint(args ...any) string {
	var b strings.Builder
	prevString := false
	for i, arg := range args {
		curString := isStringOperand(arg)
		if i > 0 && !prevString && !curString {
			b.WriteByte(' ')
		}
		b.WriteString(formatValue(reflect.ValueOf(arg), 'v', false))
		prevString = curString
	}
	return b.String()
}

// printLine writes operands to stdout, separated by spaces and followed by a
// newline, like fmt.Println.
func printLine(args ...any) {
	var b strings.Builder
	for i, arg := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(formatValue(reflect.ValueOf(arg), 'v', false))
	}
	b.WriteByte('\n')
	_, _ = os.Stdout.WriteString(b.String())
}

func isStringOperand(v any) bool {
	if v == nil {
		return false
	}
	return reflect.ValueOf(v).Kind() == reflect.String
}

type wrapError struct {
	msg string
	err error
}

func (e *wrapError) Error() string { return e.msg }
func (e *wrapError) Unwrap() error { return e.err }

type wrapErrors struct {
	msg  string
	errs []error
}

func (e *wrapErrors) Error() string   { return e.msg }
func (e *wrapErrors) Unwrap() []error { return e.errs }

func formatString(format string, args []any) (string, []error) {
	var b strings.Builder
	var wrapped []error
	ai := 0

	for i := 0; i < len(format); {
		if format[i] != '%' {
			b.WriteByte(format[i])
			i++
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			b.WriteByte('%')
			i += 2
			continue
		}

		start := i
		j := i + 1
		plus := false
		minus := false
		for j < len(format) {
			switch format[j] {
			case '+':
				plus = true
				j++
			case '-':
				minus = true
				j++
			case '#', '0', ' ':
				j++
			default:
				goto flagsDone
			}
		}
	flagsDone:
		width := 0
		hasWidth := false
		for j < len(format) && format[j] >= '0' && format[j] <= '9' {
			hasWidth = true
			width = width*10 + int(format[j]-'0')
			j++
		}
		if j < len(format) && format[j] == '.' {
			j++
			for j < len(format) && format[j] >= '0' && format[j] <= '9' {
				j++
			}
		}
		if j >= len(format) {
			b.WriteString(format[start:])
			break
		}

		verb := rune(format[j])
		j++
		if ai >= len(args) {
			b.WriteString("%!")
			b.WriteByte(byte(verb))
			b.WriteString("(MISSING)")
			i = j
			continue
		}

		arg := args[ai]
		ai++
		var out string
		if verb == 'w' {
			if arg == nil {
				out = "%!w(<nil>)"
			} else if err, ok := arg.(error); ok {
				wrapped = append(wrapped, err)
				if err == nil {
					out = "%!w(<nil>)"
				} else {
					out = err.Error()
				}
			} else {
				out = "%!w(" + typeName(reflect.ValueOf(arg)) + "=" +
					formatValue(reflect.ValueOf(arg), 'v', plus) + ")"
			}
		} else {
			out = formatValue(reflect.ValueOf(arg), verb, plus)
		}
		if hasWidth && (verb == 's' || verb == 'v' || verb == 'd') {
			out = pad(out, width, minus)
		}
		b.WriteString(out)
		i = j
	}

	if ai < len(args) {
		b.WriteString("%!(EXTRA")
		for ; ai < len(args); ai++ {
			b.WriteByte(' ')
			rv := reflect.ValueOf(args[ai])
			b.WriteString(typeName(rv))
			b.WriteByte('=')
			b.WriteString(formatValue(rv, 'v', false))
		}
		b.WriteByte(')')
	}

	return b.String(), wrapped
}

func formatValue(rv reflect.Value, verb rune, plus bool) string {
	if !rv.IsValid() {
		return "<nil>"
	}
	if rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return "<nil>"
		}
		return formatValue(rv.Elem(), verb, plus)
	}

	if (verb == 'v' || verb == 's') && rv.CanInterface() {
		if err, ok := rv.Interface().(error); ok && err != nil {
			return err.Error()
		}
		if s, ok := rv.Interface().(stringer); ok {
			return s.String()
		}
	}

	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return strconv.FormatBool(rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(rv.Uint(), 10)
	case reflect.Float32:
		return strconv.FormatFloat(rv.Float(), 'g', -1, 32)
	case reflect.Float64:
		return strconv.FormatFloat(rv.Float(), 'g', -1, 64)
	case reflect.Complex64, reflect.Complex128:
		c := rv.Complex()
		re := strconv.FormatFloat(real(c), 'g', -1, 64)
		im := strconv.FormatFloat(imag(c), 'g', -1, 64)
		if imag(c) >= 0 {
			im = "+" + im
		}
		return "(" + re + im + "i)"
	case reflect.Slice, reflect.Array:
		if verb == 's' && rv.Type().Elem().Kind() == reflect.Uint8 {
			if rv.Kind() == reflect.Slice {
				return string(rv.Bytes())
			}
			bs := make([]byte, rv.Len())
			for i := range bs {
				bs[i] = byte(rv.Index(i).Uint())
			}
			return string(bs)
		}
		parts := make([]string, rv.Len())
		for i := range parts {
			parts[i] = formatValue(rv.Index(i), verb, plus)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case reflect.Map:
		keys := rv.MapKeys()
		sortKeys(keys)
		parts := make([]string, len(keys))
		for i, key := range keys {
			val := rv.MapIndex(key)
			parts[i] = formatValue(key, verb, plus) + ":" +
				formatValue(val, verb, plus)
		}
		return "map[" + strings.Join(parts, " ") + "]"
	case reflect.Struct:
		fields := make([]string, rv.NumField())
		for i := range fields {
			name := rv.Type().Field(i).Name
			fields[i] = formatValue(rv.Field(i), verb, plus)
			if plus {
				fields[i] = name + ":" + fields[i]
			}
		}
		return "{" + strings.Join(fields, " ") + "}"
	case reflect.Pointer:
		if rv.IsNil() {
			return "<nil>"
		}
		switch rv.Elem().Kind() {
		case reflect.Struct, reflect.Slice, reflect.Array, reflect.Map:
			return "&" + formatValue(rv.Elem(), verb, plus)
		}
		return "0x" + strconv.FormatUint(uint64(rv.Pointer()), 16)
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return "0x" + strconv.FormatUint(uint64(rv.Pointer()), 16)
	}

	if rv.CanInterface() {
		return sprint(rv.Interface())
	}
	return "<?>"
}

func sortKeys(keys []reflect.Value) {
	sort.SliceStable(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		switch a.Kind() {
		case reflect.String:
			return a.String() < b.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
			reflect.Int64:
			return a.Int() < b.Int()
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
			reflect.Uint64, reflect.Uintptr:
			return a.Uint() < b.Uint()
		case reflect.Float32, reflect.Float64:
			return a.Float() < b.Float()
		}
		return formatValue(a, 'v', false) < formatValue(b, 'v', false)
	})
}

func pad(s string, width int, left bool) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	spaces := strings.Repeat(" ", width-n)
	if left {
		return s + spaces
	}
	return spaces + s
}

func typeName(rv reflect.Value) string {
	if !rv.IsValid() {
		return "<nil>"
	}
	return rv.Type().String()
}
