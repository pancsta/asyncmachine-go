package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/invopop/jsonschema"

	"github.com/pancsta/asyncmachine-go/pkg/integrations"
)

func main() {
	r := new(jsonschema.Reflector)
	if err := r.AddGoComments("github.com/pancsta/asyncmachine-go",
		"./pkg/integrations"); err != nil {
		panic(err)
	}

	// Create docs/jsonschema directory if it doesn't exist
	schemaDir := filepath.Join("docs", "jsonschema")
	if err := os.MkdirAll(schemaDir, 0755); err != nil {
		panic(err)
	}

	// List of structs to generate schemas for
	structs := []struct {
		name string
		obj  interface{}
	}{
		{"msg_kind_req", &integrations.MsgKindReq{}},
		{"msg_kind_resp", &integrations.MsgKindResp{}},
		{"mutation_req", &integrations.MutationReq{}},
		{"mutation_resp", &integrations.MutationResp{}},
		{"waiting_req", &integrations.WaitingReq{}},
		{"waiting_resp", &integrations.WaitingResp{}},
		{"getter_req", &integrations.GetterReq{}},
		{"getter_resp", &integrations.GetterResp{}},
	}

	for _, s := range structs {
		schema := r.Reflect(s.obj)
		jsonData, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			panic(fmt.Errorf("failed to marshal schema for %s: %v", s.name, err))
		}

		filename := filepath.Join(schemaDir, s.name+".json")
		if err := os.WriteFile(filename, jsonData, 0644); err != nil {
			panic(fmt.Errorf("failed to write schema for %s: %v", s.name, err))
		}

		fmt.Printf("Generated /%s\n", filename)
	}
}
