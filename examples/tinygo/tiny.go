//go:build tinygo

package tinygo

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func BindMachineHandlers(h *MachineHandlers, mach *am.Machine) error {
	return mach.BindHandlerMaps("MachineHandlers",
		MachineHandlersNegotiations(h), MachineHandlersFinals(h))
}
