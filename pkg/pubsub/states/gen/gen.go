package main

import (
	ss "github.com/pancsta/asyncmachine-go/pkg/pubsub/states"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteSchemaV3(
		generator.GenSchemaV3([]generator.SchemaV3{
			{
				ExportName: "Topic",
				States:     ss.TopicStates,
				Schema:     ss.TopicSchema,
				Groups:     ss.TopicGroups,
			},
		}),
	)
	if err != nil {
		panic(err)
	}
}
