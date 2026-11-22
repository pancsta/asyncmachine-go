//go:build !js

// Package deps logs to the browser console in WASM and to stderr elsewhere.
package deps

import (
	"fmt"
	"os"
)

// Log writes to stderr.
func Log(args ...any) {
	_, _ = fmt.Fprintln(os.Stderr, args...)
}

// Logf writes a formatted message to stderr.
func Logf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// Error writes to stderr.
func Error(args ...any) {
	_, _ = fmt.Fprintln(os.Stderr, args...)
}

// Errorf writes a formatted message to stderr.
func Errorf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
}
