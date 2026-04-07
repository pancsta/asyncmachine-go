package cli

import (
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
	StatesFile *StatesParams  `arg:"subcommand:states-file" help:"Generate state schema files"`
	Grafana    *GrafanaParams `arg:"subcommand:grafana" help:"Generate Grafana dashboards"`
	Version    bool           `arg:"-v,--version" help:"Print version and exit"`
}

// TODO dedicated descriptions
func (Args) Description() string {
	//nolint:lll
	return strings.TrimLeft(dedent.Dedent(`
			am-gen generates state files and Grafana dashboards for asyncmachine-go state machines.
	
			Example:
			$ am-gen states-file --states State1,State2:multi \
				--inherit basic,connected \
				--groups Group1,Group2 \
				--name MyMach
	
			Example:
			$ am-gen grafana --IDs MyMach1,MyMach2 \
				--sync grafana-host.com
		`), "\n")
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

// TODO validate param
// var inherits = []string{"basic", "connected", "rpc/worker", "node/worker"}

// StatesParams are params for the states-file subcommand.
//
//nolint:lll
type StatesParams struct {
	// Version - print version
	Version bool
	// States - State names to generate. Eg: State1,State2
	States string `arg:"-s,--states,required" help:"State names to generate. Eg: State1,State2"`
	// Inherit - Inherit from built-in states machines (comma separated):
	// - basic,connected
	// - rpc/statesrc
	// - node/worker
	Inherit string `arg:"-i,--inherit" help:"Inherit from built-in state-machines: basic,connected,rpc/statesrc,node/worker"`
	// Groups - Groups to generate. Eg: Group1,Group2
	Groups string `arg:"-g,--groups" help:"Groups to generate. Eg: Group1,Group2"`
	// Name - Name of the state machine.
	Name string `arg:"-n,--name,required" help:"Name of the state machine. Eg: MyMach"`
	// Force - Overwrite existing files.
	Force bool `arg:"-f,--force" help:"Override output file (if any)"`
	// Utils - Generate states_utils.go in CWD. Overrides files.
	Utils bool `arg:"-u,--utils" default:"true" help:"Generate states_utils.go in CWD. Overrides files."`
}
