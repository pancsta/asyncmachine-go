//go:build tinygo

package rpc

import (
	"context"
	"errors"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ErrMuxUnsupported is returned by the mux when running in TinyGo, where
// multiplexing is disabled.
var ErrMuxUnsupported = errors.New("rpc mux is not supported in tinygo")

// muxImpl is empty in TinyGo builds; cmux is excluded via build tags.
type muxImpl struct{}

func newMuxMachine(
	ctx context.Context, d *Mux, opts *MuxOpts,
) (*am.Machine, error) {
	return nil, ErrMuxUnsupported
}

func (m *Mux) serveMux(ctx context.Context, e *am.Event) {
	_ = ctx

	m.Mach.AddErr(ErrMuxUnsupported, nil)
	m.Mach.EvRemove1(e, ssM.Start, nil)
}
