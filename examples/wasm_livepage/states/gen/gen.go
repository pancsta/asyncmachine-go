package main

import (
	ss "github.com/pancsta/asyncmachine-go/examples/wasm_livepage/states"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteSchemaV3("",
		generator.GenSchemaV3([]generator.SchemaV3{
			{
				ExportName: "Demo",
				States:     ss.DemoStates,
				Schema:     ss.DemoSchema,
				Groups:     ss.DemoGroups,
			},
		}),
	)
	if err != nil {
		panic(err)
	}
}
