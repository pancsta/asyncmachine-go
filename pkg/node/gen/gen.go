package main

import (
	"github.com/pancsta/asyncmachine-go/pkg/node"
	ss "github.com/pancsta/asyncmachine-go/pkg/node/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteHandlers(
		generator.GenHandlers("", &generator.Args{
			Id:     "A",
			IdRpc:  "ARpc",
			Val:    rpc.A{},
			ValRpc: rpc.ARpc{},
		},
			generator.Handlers{
				States:   ss.ClientStates.Names(),
				Handlers: &node.Client{},
			},
			generator.Handlers{
				States:   ss.SupervisorStates.Names(),
				Handlers: &node.Supervisor{},
			},
			generator.Handlers{
				States:   ss.WorkerStates.Names(),
				Handlers: &node.Worker{},
			},
			generator.Handlers{
				States:   ss.BootstrapStates.Names(),
				Handlers: &node.Bootstrap{},
			},
		))
	if err != nil {
		panic(err)
	}
}
