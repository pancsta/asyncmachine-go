package main

import (
	"github.com/pancsta/asyncmachine-go/pkg/rpc/mux"
	ss "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/tools/generator"
)

func main() {
	err := generator.WriteHandlers(
		generator.GenHandlers("", nil,
			generator.Handlers{
				States:   ss.MuxStates.Names(),
				Handlers: &mux.Mux{},
			},
		))
	if err != nil {
		panic(err)
	}
}
