//go:build js

package deps

import (
	"fmt"
	"syscall/js"
)

// Log writes to the browser console.
func Log(args ...any) {
	js.Global().Get("console").Call("log", fmt.Sprint(args...))
}

// Logf writes a formatted message to the browser console.
func Logf(format string, args ...any) {
	Log(fmt.Sprintf(format, args...))
}

// Error writes to the browser console as an error.
func Error(args ...any) {
	js.Global().Get("console").Call("error", fmt.Sprint(args...))
}

// Errorf writes a formatted message to the browser console as an error.
func Errorf(format string, args ...any) {
	Error(fmt.Sprintf(format, args...))
}
