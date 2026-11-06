package main

import (
	ss "github.com/pancsta/asyncmachine-go/examples/wasm/states"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteSchemaV3("",
		generator.GenSchemaV3([]generator.SchemaV3{
			{
				ExportName: "Shared",
				States:     ss.SharedStates,
				Schema:     ss.SharedSchema,
				Groups:     ss.SharedGroups,
			},
			{
				ExportName: "Foo",
				States:     ss.FooStates,
				Schema:     ss.FooSchema,
			}, {
				ExportName: "Bar",
				States:     ss.BarStates,
				Schema:     ss.BarSchema,
				Groups:     ss.BarGroups,
			},
		}),
	)
	if err != nil {
		panic(err)
	}
}
