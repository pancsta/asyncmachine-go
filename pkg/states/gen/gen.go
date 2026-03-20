package main

import (
	ss "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteSchemaV3(
		generator.GenSchemaV3([]generator.SchemaV3{
			{
				ExportName: "Basic",
				States:     ss.BasicStates,
				Schema:     ss.BasicSchema,
			}, {
				ExportName: "Connected",
				States:     ss.ConnectedStates,
				Schema:     ss.ConnectedSchema,
				Groups:     ss.ConnectedGroups,
			}, {
				ExportName: "ConnPool",
				States:     ss.ConnPoolStates,
				Schema:     ss.ConnPoolSchema,
				Groups:     ss.ConnPoolGroups,
			}, {
				ExportName: "Disposed",
				States:     ss.DisposedStates,
				Schema:     ss.DisposedSchema,
				Groups:     ss.DisposedGroups,
			},
		}),
	)
	if err != nil {
		panic(err)
	}
}
