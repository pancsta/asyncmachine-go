// Based on https://en.wikipedia.org/wiki/Finite-state_machine
//
// === RUN   TestTurnstile
// [state] +Locked
//    fsm_test.go:74: Push into (Locked:1)
//    fsm_test.go:79: Coin into (Locked:1)
// [state] +InputCoin
// [state] -InputCoin
// [state] +Unlocked -Locked
//    fsm_test.go:85: Coin into (Unlocked:1)
// [state] +InputCoin
// [state] -InputCoin
//    fsm_test.go:91: Push into (Unlocked:1)
// [state] +InputPush
// [state] -InputPush
// [state] +Locked -Unlocked
//    fsm_test.go:96: End with (Locked:3)
// --- PASS: TestTurnstile (0.00s)
// PASS

package fsm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/joho/godotenv"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func init() {
	// load .env
	_ = godotenv.Load()

	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	// amhelp.EnableDebugging(false)
	// amhelp.SetLogLevel(am.LogChanges)
}

// state enum pkg

var (
	states = am.Schema{
		// input states
		InputPush: {},
		InputCoin: {},

		// "state" states
		Locked: {
			Auto:   true,
			Remove: groupUnlocked,
		},
		Unlocked: {Remove: groupUnlocked},
	}

	groupUnlocked = am.S{Locked, Unlocked}

	InputPush = "InputPush"
	InputCoin = "InputCoin"
	Locked    = "Locked"
	Unlocked  = "Unlocked"

	Names = am.S{InputPush, InputCoin, Locked, Unlocked}
)

// handlers

type Turnstile struct{}

func (t *Turnstile) InputPushEnter(e *am.Event) bool {
	return e.Machine().Is1(Unlocked)
}

func (t *Turnstile) InputPushState(e *am.Event) {
	e.Machine().Remove1(InputPush, nil)
	e.Machine().Add1(Locked, nil)
}

func (t *Turnstile) InputCoinState(e *am.Event) {
	e.Machine().Remove1(InputCoin, nil)
	e.Machine().Add1(Unlocked, nil)
}

// example

func TestTurnstile(t *testing.T) {
	mach := am.New(context.Background(), states, &am.Opts{
		Id:                   "turnstile",
		DontPanicToException: true,
		DontLogID:            true,
		LogLevel:             am.LogChanges,
		HandlerTimeout:       time.Minute,
	})

	_ = mach.BindHandlers(&Turnstile{})
	_ = mach.VerifyStates(Names)

	// start
	mach.Add1(Locked, nil)
	assert.True(t, mach.Is1(Locked))

	t.Logf("Push into %s", mach)
	mach.Add1(InputPush, nil)
	assert.True(t, mach.Is1(Locked))

	// coin
	t.Logf("Coin into %s", mach)
	mach.Add1(InputCoin, nil)
	assert.True(t, mach.Not1(Locked))
	assert.True(t, mach.Is1(Unlocked))

	// coin
	t.Logf("Coin into %s", mach)
	mach.Add1(InputCoin, nil)
	assert.True(t, mach.Not1(Locked))
	assert.True(t, mach.Is1(Unlocked))

	// push
	t.Logf("Push into %s", mach)
	mach.Add1(InputPush, nil)
	assert.True(t, mach.Not1(Unlocked))
	assert.True(t, mach.Is1(Locked))

	t.Logf("End with %s", mach)
}
