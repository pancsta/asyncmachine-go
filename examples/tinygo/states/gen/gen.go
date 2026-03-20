package main

import (
	ss "github.com/pancsta/asyncmachine-go/examples/tinygo/states"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteSchemaV3(
		generator.GenSchemaV3([]generator.SchemaV3{
			{
				ExportName: "File",
				States:     ss.FileStates,
				Schema:     ss.FileSchema,
				Groups:     ss.FileGroups,
			},
		}),
	)
	if err != nil {
		panic(err)
	}
}
