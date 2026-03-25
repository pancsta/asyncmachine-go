//go:build tinygo

package node

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

func BindHandlersClient(h *Client, mach *am.Machine) error {
	return mach.BindHandlerMaps("Client",
		ClientNegotiations(h), ClientFinals(h))
}

func BindHandlersSupervisor(h *Supervisor, mach *am.Machine) error {
	return mach.BindHandlerMaps("Supervisor",
		SupervisorNegotiations(h), SupervisorFinals(h))
}

func BindHandlersWorker(h *Worker, mach *am.Machine) error {
	return mach.BindHandlerMaps("Worker",
		WorkerNegotiations(h), WorkerFinals(h))
}

func BindHandlersBootstrap(h *Bootstrap, mach *am.Machine) error {
	return mach.BindHandlerMaps("Bootstrap",
		BootstrapNegotiations(h), BootstrapFinals(h))
}

// TODO args
