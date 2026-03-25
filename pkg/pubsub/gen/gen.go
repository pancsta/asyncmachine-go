package main

import (
	"github.com/pancsta/asyncmachine-go/pkg/pubsub"
	ss "github.com/pancsta/asyncmachine-go/pkg/pubsub/states"
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
				States:   ss.TopicStates.Names(),
				Handlers: &pubsub.Topic{},
			},
		))
	if err != nil {
		panic(err)
	}
}
