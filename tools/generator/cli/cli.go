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
