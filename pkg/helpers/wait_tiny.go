//go:build tinygo

package helpers

import (
	"context"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// WaitForAny waits for any of the channels to close, or until the context is
// done, or until the timeout is reached. Returns nil if any channel is
// closed, or ErrTimeout, or ctx.Err().
//
// This TinyGo version avoids reflect.Select by polling with select/default.
func WaitForAny(
	ctx context.Context, timeout time.Duration, chans ...<-chan struct{},
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if IsDebug() {
		timeout = 100 * timeout
	}
	deadline := time.Now().Add(timeout)

	for {
		for _, ch := range chans {
			select {
			case <-ch:
				return nil
			default:
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !time.Now().Before(deadline) {
			return am.ErrTimeout
		}
		if !Wait(ctx, time.Millisecond) {
			return ctx.Err()
		}
	}
}

// WaitForErrAny is like WaitForAny, but also waits on the machine error.
func WaitForErrAny(
	ctx context.Context, timeout time.Duration, mach *am.Machine,
	chans ...<-chan struct{},
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if IsDebug() {
		timeout = 100 * timeout
	}
	deadline := time.Now().Add(timeout)

	for {
		for _, ch := range chans {
			select {
			case <-ch:
				return nil
			default:
			}
		}
		if mach != nil && mach.Err() != nil {
			return mach.Err()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !time.Now().Before(deadline) {
			return am.ErrTimeout
		}
		if !Wait(ctx, time.Millisecond) {
			return ctx.Err()
		}
	}
}
