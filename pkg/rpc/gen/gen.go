package main

import (
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ss "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
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
				Handlers: &rpc.Client{},
			},
			generator.Handlers{
				States:   ss.ServerStates.Names(),
				Handlers: &rpc.Server{},
			},
		))
	if err != nil {
		panic(err)
	}
}
