package helpers

import (
	"context"
	"math/rand"
	"testing"

	"github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestDynMach(t *testing.T) {
	ctx := context.Background()

	// states
	var (
		StateFoo = "Foo"
		StateBar = "Bar"
		StateBaz = "Baz"
	)

	// machine
	mach, err := NewDynMach(ctx, machine.Schema{
		// schema

		StateFoo: {
			Auto:    true,
			Require: []string{StateBar},
		},
		StateBar: {
			Auto:    true,
			Require: []string{StateBaz},
		},
		StateBaz: {
			Auto: true,
		},
	}, map[string]machine.HandlerNegotiation{
		// FooEnter
		StateFoo + machine.SuffixEnter: func(e *machine.Event) bool {
			return rand.Intn(2) == 1
		},
	}, map[string]machine.HandlerFinal{
		// FooState
		StateFoo + machine.SuffixState: func(e *machine.Event) {
			mach := e.Machine()
			ctx := mach.NewStateCtx(StateFoo)

			mach.Fork(ctx, e, func() {
				mach.EvRemove1(e, StateBar, nil)
			})
		},
	},

		// initial state
		S{StateFoo},
	)

	// TODO asserts
	_ = mach

	if err != nil {
		panic(err)
	}
}
