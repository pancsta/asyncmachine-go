package main

import (
	example "github.com/pancsta/asyncmachine-go/examples/tinygo"
	ss "github.com/pancsta/asyncmachine-go/examples/tinygo/states"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteHandlers(
		generator.GenHandlers("", nil,
			generator.Handlers{
				States:   ss.FileStates.Names(),
				Handlers: &example.MachineHandlers{},
			},
		),
	)
	if err != nil {
		panic(err)
	}
}
