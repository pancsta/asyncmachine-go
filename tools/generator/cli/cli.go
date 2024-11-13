package cli

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

const pVersion = "version"

// RootParams are params for the root command.
type RootParams struct {
	// Version - print version
	Version bool
}

func ParseRootParams(cmd *cobra.Command, _ []string) RootParams {
	version, _ := cmd.Flags().GetBool(pVersion)

	return RootParams{
		Version: version,
	}
}

// TODO move to /internal
func GetVersion() string {
	build, ok := debug.ReadBuildInfo()
	if !ok {
		return "(devel)"
	}

	ver := build.Main.Version
	if ver == "" {
		return "(devel)"
	}

	return ver
}

// ///// ///// /////

// ///// GRAFANA

// ///// ///// /////

const (

	// grafana

	pGIds             = "ids"
	pGIdsShort        = "i"
	pGGrafanaUrl      = "grafana-url"
	pGGrafanaUrlShort = "g"
	pGName            = "name"
	pGNameShort       = "n"
	pGFolder          = "folder"
	pGFolderShort     = "f"
	pGSource          = "source"
	pGSourceShort     = "s"
)

func AddGrafanaFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringP(pGIds, pGIdsShort, "",
		"Machines IDs (comma separated). Required.")
	f.StringP(pGGrafanaUrl, pGGrafanaUrlShort, "",
		"Grafana URL to sync. Requires GRAFANA_TOKEN in CWD/.env")
	f.StringP(pGFolder, pGFolderShort, "",
		"Dashboard folder. Optional. Requires --"+pGGrafanaUrl)
	f.StringP(pGName, pGNameShort, "",
		"Dashboard name. Required.")
	f.StringP(pGSource, pGSourceShort, "",
		"$source variable (service_name or job). Required.")

}

// GrafanaParams are params for the grafana command.
type GrafanaParams struct {
	Ids        string
	GrafanaUrl string
	Folder     string
	Name       string
	Token      string
	Source     string
	// TODO interval
	// Interval   string
}

func ParseGrafanaParams(cmd *cobra.Command, _ []string) GrafanaParams {
	ids := strings.Trim(cmd.Flag(pGIds).Value.String(), "\n ")
	sync := strings.Trim(cmd.Flag(pGGrafanaUrl).Value.String(), "\n ")
	folder := strings.Trim(cmd.Flag(pGFolder).Value.String(), "\n ")
	name := strings.Trim(cmd.Flag(pGName).Value.String(), "\n ")
	source := strings.Trim(cmd.Flag(pGSource).Value.String(), "\n ")

	if ids == "" || strings.Contains(ids, " ") || strings.Contains(ids, ",,") {
		fmt.Println("Error: ids invalid")
		os.Exit(1)
	}

	if sync == "" || strings.Contains(sync, " ") {
		fmt.Println("Error: sync invalid")
		os.Exit(1)
	}

	if name == "" || strings.Contains(name, " ") {
		fmt.Println("Error: name invalid")
		os.Exit(1)
	}

	if source == "" || strings.Contains(source, " ") {
		fmt.Println("Error: source invalid")
		os.Exit(1)
	}

	return GrafanaParams{
		Ids:        ids,
		GrafanaUrl: sync,
		Folder:     folder,
		Name:       name,
		Source:     source,
	}
}

// ///// ///// /////

// ///// STATES FILE

// ///// ///// /////

// TODO validate param
// var inherits = []string{"basic", "connected", "rpc/worker", "node/worker"}

// SFParams are params for the states-file command.
type SFParams struct {
	// Version - print version
	Version bool
	// States - State names to generate. Eg: State1,State2
	States string
	// Inherit - Inherit from built-in states machines (comma separated):
	// - basic,connected
	// - rpc/worker
	// - node/worker
	Inherit string
	// Groups - Groups to generate. Eg: Group1,Group2
	Groups string
	// Name - Name of the state machine.
	Name string
	// Force - Overwrite existing files.
	Force bool
	// Utils - Generate states_utils.go in CWD. Overrides files.
	Utils bool
}

const (

	// states-file

	pSFStates       = "states"
	pSFStatesShort  = "s"
	pSFInherit      = "inherit"
	pSFInheritShort = "i"
	pSFGroups       = "groups"
	pSFGroupsShort  = "g"
	pSFName         = "name"
	pSFNameShort    = "n"
	pSFForce        = "force"
	pSFForceShort   = "f"
	pSFUtils        = "utils"
	pSFUtilsShort   = "u"
)

func AddStatesFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringP(pSFStates, pSFStatesShort, "",
		"State names to generate. Eg: State1,State2")
	f.StringP(pSFInherit, pSFInheritShort, "",
		"Inherit from a built-in states machine: " +
		"basic,connected,rpc/worker,node/worker")
	f.StringP(pSFGroups, pSFGroupsShort, "",
		"Groups to generate. Eg: Group1,Group2")
	f.StringP(pSFName, pSFNameShort, "",
		"Name of the state machine. Eg: MyMach")
	f.BoolP(pSFUtils, pSFUtilsShort, true,
		"Generate states_utils.go in CWD. Overrides files.")
	f.Bool(pVersion, false,
		"Print version and exit")
	f.BoolP(pSFForce, pSFForceShort, false,
		"Override output file (if any)")
}

func ParseSFParams(cmd *cobra.Command, _ []string) SFParams {

	states := strings.Trim(cmd.Flag(pSFStates).Value.String(), "\n ")
	inherit := strings.Trim(cmd.Flag(pSFInherit).Value.String(), "\n ")
	groups := strings.Trim(cmd.Flag(pSFGroups).Value.String(), "\n ")
	name := strings.Trim(cmd.Flag(pSFName).Value.String(), "\n ")
	force, _ := cmd.Flags().GetBool(pSFForce)
	utils, _ := cmd.Flags().GetBool(pSFUtils)

	if states == "" || strings.Contains(states, " ") ||
		strings.Contains(states, ",,") {
		fmt.Println("Error: states invalid")
		os.Exit(1)
	}

	if strings.Contains(groups, " ") || strings.Contains(groups, ",,") {
		fmt.Println("Error: groups invalid")
		os.Exit(1)
	}

	if strings.Contains(inherit, " ") || strings.Contains(inherit, ",,") {
		fmt.Println("Error: inherit invalid")
		os.Exit(1)
	}

	if name == "" || strings.Contains(name, " ") {
		fmt.Println("Error: name invalid")
		os.Exit(1)
	}

	return SFParams{
		// Version: version,
		States:  states,
		Inherit: inherit,
		Groups:  groups,
		Name:    name,
		Force:   force,
		Utils:   utils,
	}
}
