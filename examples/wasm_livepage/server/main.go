package main

import (
	"context"
	"log"

	example "github.com/pancsta/asyncmachine-go/examples/wasm_livepage"
)

func main() {
	ctx := context.Background()

	srv, err := example.StartServer(ctx, example.EnvHttpAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("listening on http://%s", srv.Addr)

	select {}
}
