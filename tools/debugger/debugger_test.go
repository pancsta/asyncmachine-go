package main

import (
	"testing"
)

func TestInspectSteps(t *testing.T) {
	t.Skip("TODO")
	// expected := `
	// 	A:
	// 	  State:   true 1
	// 	  Auto:    true
	// 	  Require: C
	//
	// 	[B]:
	// 	  State:   false 0
	// 	  Multi:   true
	// 	  [Add]:   [C]
	//
	// 	[C]:
	// 	  State:   [false 1]
	// 	  After:   D
	//
	// 	D:
	// 	  State:   false 0
	// 	  Add:     C B
	//
	// 	Exception:
	// 	  State:   false 0
	// 	`
}
