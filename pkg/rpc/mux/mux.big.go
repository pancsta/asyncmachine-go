//go:build !tinygo

package mux

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func BindHandlers(h *Mux, mach *am.Machine) error {
	return mach.BindHandlers(h)
}
