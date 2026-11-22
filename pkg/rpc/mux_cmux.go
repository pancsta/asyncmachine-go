//go:build !tinygo

package rpc

import (
	"context"

	"github.com/soheilhy/cmux"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// muxImpl holds the cmux state. It is replaced with an empty struct in TinyGo
// builds, where the mux is not supported.
type muxImpl struct {
	cmux cmux.CMux
}

func newMuxMachine(
	ctx context.Context, d *Mux, opts *MuxOpts,
) (*am.Machine, error) {
	return am.NewCommon(ctx, "rm-"+d.Name, states.MuxSchema, ssM.Names(),
		d, opts.Parent, &am.Opts{
			Tags: []string{states.TagRpcMux},
		})
}

func (m *Mux) serveMux(ctx context.Context, e *am.Event) {
	_ = ctx

	// Create a new cmux instance.
	m.cmux = cmux.New(m.Listener)

	// fork
	l := m.cmux.Match(cmux.Any())
	go m.accept(e, l)

	// start cmux
	if err := m.cmux.Serve(); err != nil {
		m.Mach.AddErr(err, nil)
		m.Mach.EvRemove1(e, ssM.Start, nil)
	}
}
