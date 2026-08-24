package cli

import (
	"fmt"
	"strings"

	"github.com/lithammer/dedent"
)

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

// Args is the top-level args struct for am-gen.
//
//nolint:lll
type Args struct {
	Starter    *StarterParams    `arg:"subcommand:starter-kit" help:"Generate a starter project from schema.yml / mach.yml"`
	Schema     *SchemaParams     `arg:"subcommand:schema" help:"Generate state schema from CLI params"`
	SchemaFile *SchemaFileParams `arg:"subcommand:schema-from-file" help:"Generate state schema from schema.yml / mach.yml"`
	Grafana    *GrafanaParams    `arg:"subcommand:grafana" help:"Generate Grafana dashboards"`
	Version    bool              `arg:"-v,--version" help:"Print version and exit"`

	StatesFile *SchemaParams `arg:"subcommand:states-file" help:"Deprecated, use schema"`
}

// Description returns the formatted CLI description and examples.
func (Args) Description() string {
	//nolint:lll
	desc := dedent.Dedent(`
		am-gen generates schemas, project boilerplate, and Grafana dashboards for
			asyncmachine-go state machines.
		
		Example:
		$ am-gen schema --state State1 --state State2:multi \
			--inherit basic --inherit connected \
			--group Group1 --group Group2 \
			--name MyMach
		
		Example:
		$ am-gen starter-kit schema.yml --name MyMach --uri github.com/my/project
		
		Example:
		$ am-gen schema-from-file schema.yml --name MyMach
		
		Example:
		$ am-gen grafana --IDs MyMach1,MyMach2 \
			--sync grafana-host.com
		
		Valid for --inherit:
		- %s
	
		`)

	return fmt.Sprintf(
		strings.TrimSpace(desc)+"\n", strings.Join(Inherits, "\n- "))
}

// ///// ///// /////

// ///// GRAFANA

// ///// ///// /////

// GrafanaParams are params for the grafana subcommand.
//
//nolint:lll
type GrafanaParams struct {
	Ids        string `arg:"-i,--ids,required" help:"Machine IDs (comma separated)"`
	GrafanaUrl string `arg:"-g,--grafana-url" help:"Grafana URL to sync. Requires GRAFANA_TOKEN in CWD/.env"`
	Folder     string `arg:"-f,--folder" help:"Dashboard folder (optional, requires --grafana-url)"`
	Name       string `arg:"-n,--name,required" help:"Dashboard name"`
	Source     string `arg:"-s,--source,required" help:"$source variable (service_name or job)"`
	Token      string `arg:"-"`
	// TODO interval
	// Interval string
}

// ///// ///// /////

// ///// SCHEMA FILE

// ///// ///// /////

// TODO enum, merge with schema.go
var Inherits = []string{
	"basic", "connected", "disposed", "rpc/statesrc", "node/worker",
}

// SchemaParams are params for the states-file subcommand.
//
//nolint:lll
type SchemaParams struct {

	SchemaParamsCommon

	// State - State name to generate (repeatable). Eg: --state State1 --state State2:multi
	State []string `arg:"--state,separate" help:"Repeatable state name to generate. Eg: --state State1 --state State2:multi"`
	// States - State names to generate. Eg: State1,State2
	States string `arg:"-s,--states" help:"State names to generate. Eg: State1,State2"`
}

// ///// ///// /////

// ///// SCHEMA FILE

// ///// ///// /////

// SchemaFileParams are params for the schema-yaml subcommand.
//
//nolint:lll
type SchemaFileParams struct {
	SchemaParamsCommon

	// File - Path to YAML schema file.
	File string `arg:"positional,required" help:"Path to schema.yml / mach.yml"`

	// internal
	FileContent []byte `arg:"-"`
}

//nolint:lll
type SchemaParamsCommon struct {
	// Version - print version
	Version bool
	// Inherit - Inherit from built-in states machines (comma separated or repeatable):
	// - basic,connected
	// - rpc/statesrc
	// - node/worker
	Inherit []string `arg:"-i,--inherit,separate" help:"Inherit from built-in state-machines: basic,disposed,connected,rpc/statesrc,node/worker"`
	// Group - Group to generate (repeatable). Eg: --group Group1 --group Group2
	Group []string `arg:"--group,separate" help:"Repeatable group to generate. Eg: --group Group1 --group Group2"`
	// Groups - Groups to generate. Eg: Group1,Group2
	Groups string `arg:"-g,--groups" help:"Groups to generate. Eg: Group1,Group2"`
	// Name - Name of the state machine.
	Name string `arg:"-n,--name" default:"MyMach" help:"Name of the state machine. Eg: MyMach"`
	// Force - Overwrite existing files.
	Force bool `arg:"-f,--force" help:"Override output file (if any)"`
	// Utils - Generate states_utils.go in CWD. Overrides files.
	Utils bool `arg:"-u,--utils" default:"true" help:"Generate states_utils.go in CWD. Overrides files."`
	// Global - Import pkg/states/global and skip generating states_utils.go.
	Global bool `arg:"--global" default:"true" help:"Import pkg/states/global and skip generating states_utils.go"`
	// Output - Print output to stdout instead of writing to a file.
	Output bool `arg:"-o,--output" help:"Print output to stdout"`
}

//nolint:lll
type StarterParams struct {
	// File - Path to YAML schema file.
	File string `arg:"positional,required" help:"Path to schema.yml / mach.yml"`

	Name     string   `arg:"-n,--name" default:"MyMach" help:"Name of the state machine and the project directory"`
	Uri      string   `arg:"-u,--uri" default:"asyncmachine.dev/mymach" help:"URI of this module for imports"`
	Module   bool     `arg:"-m,--module" default:"true" help:"Generate go.mod"`
	Handlers bool     `arg:"-h,--handlers" default:"true" help:"Generate state-state handlers"`
	Args     bool     `arg:"-a,--args" default:"true" help:"Generate common args and for each state tagged #args"`
	Path     string   `arg:"-p,--path" help:"Base path for the generated project directory"`
	Force    bool     `arg:"-f,--force" help:"Override output files (if any)"`
	Inherit  []string `arg:"-i,--inherit,separate" help:"Inherit from built-in state-machines: basic,connected,disposed,rpc/statesrc,node/worker (default: basic, disposed)"`
	// later: Typesafe bool

	// internal
	FileContent []byte `arg:"-"`
}
