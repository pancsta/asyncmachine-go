//go:build !tinygo

package test

// isTinyGo disables test cases which require reflection-based handler binding.
const isTinyGo = false
