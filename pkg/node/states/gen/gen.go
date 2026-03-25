package main

import (
	ss "github.com/pancsta/asyncmachine-go/pkg/node/states"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteSchemaV3(
		generator.GenSchemaV3([]generator.SchemaV3{
			{
				ExportName: "Bootstrap",
				States:     ss.BootstrapStates,
				Schema:     ss.BootstrapSchema,
			},
			{
				ExportName: "Supervisor",
				States:     ss.SupervisorStates,
				Schema:     ss.SupervisorSchema,
			},
			{
				ExportName: "Worker",
				States:     ss.WorkerStates,
				Schema:     ss.WorkerSchema,
				Groups:     ss.WorkerGroups,
			},
			{
				ExportName: "Client",
				States:     ss.ClientStates,
				Schema:     ss.ClientSchema,
				Groups:     ss.ClientGroups,
			},
		}),
	)
	if err != nil {
		panic(err)
	}
}
