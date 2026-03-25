package main

import (
	ss "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteSchemaV3(
		generator.GenSchemaV3([]generator.SchemaV3{
			{
				ExportName: "Shared",
				States:     ss.SharedStates,
				Schema:     ss.SharedSchema,
				Groups:     ss.SharedGroups,
			},
			{
				ExportName: "StateSource",
				States:     ss.StateSourceStates,
				Schema:     ss.StateSourceSchema,
			}, {
				ExportName: "Server",
				States:     ss.ServerStates,
				Schema:     ss.ServerSchema,
				Groups:     ss.ServerGroups,
			}, {
				ExportName: "Client",
				States:     ss.ClientStates,
				Schema:     ss.ClientSchema,
				Groups:     ss.ClientGroups,
			}, {
				ExportName: "Mux",
				States:     ss.MuxStates,
				Schema:     ss.MuxSchema,
				Groups:     ss.MuxGroups,
			}, {
				ExportName: "Consumer",
				States:     ss.ConsumerStates,
				Schema:     ss.ConsumerSchema,
			},
		}),
	)
	if err != nil {
		panic(err)
	}
}
