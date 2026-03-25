//go:build !tinygo

package node

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func BindHandlersClient(h *Client, mach *am.Machine) error {
	return mach.BindHandlers(h)
}

func BindHandlersSupervisor(h *Supervisor, mach *am.Machine) error {
	return mach.BindHandlers(h)
}

func BindHandlersBootstrap(h *Bootstrap, mach *am.Machine) error {
	return mach.BindHandlers(h)
}

func BindHandlersWorker(h *Worker, mach *am.Machine) error {
	return mach.BindHandlers(h)
}

// TODO args
