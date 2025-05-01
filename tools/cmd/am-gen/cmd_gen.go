// am-gen generates states files and Grafana dashboards.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"

	"github.com/pancsta/asyncmachine-go/tools/generator"
	"github.com/pancsta/asyncmachine-go/tools/generator/cli"
)

const EnvGrafanaToken = "GRAFANA_TOKEN"

func init() {
	// read .env
	_ = godotenv.Load()
}

func main() {
	ctx := context.Background()

	rootCmd := &cobra.Command{
		Use: "am-gen",
		Long: strings.Trim(dedent.Dedent(`
			am-gen generates state files for asyncmachine-go state machines.
	
			Example:
			$ am-gen states-file --states State1,State2:multi \
				--inherit basic,connected \
				--groups Group1,Group2 \
				--name MyMach
	
			Example:
			$ am-gen grafana --IDs MyMach1,MyMach2 \
				--sync grafana-host.com
		`), "\n"),
		Run: func(cmd *cobra.Command, args []string) {
			params := cli.ParseRootParams(cmd, args)

			// print the version
			ver := cli.GetVersion()
			if params.Version {
				fmt.Println(ver)
				os.Exit(0)
			}
		},
	}

	// states-file
	statesCmd := &cobra.Command{
		Use: "states-file --name MyMach --states State1,State2:multi",
		Run: genStatesFile(ctx),
	}
	cli.AddStatesFlags(statesCmd)
	rootCmd.AddCommand(statesCmd)

	// grafana
	grafanaCmd := &cobra.Command{
		Use: "grafana --name MyDash --ids my-mach-1,my-mach-2 --source my-service",
		Run: genGrafana(ctx),
	}
	cli.AddGrafanaFlags(grafanaCmd)
	rootCmd.AddCommand(grafanaCmd)

	err := rootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func genGrafana(ctx context.Context) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		params := cli.ParseGrafanaParams(cmd, args)
		params.Token = os.Getenv(EnvGrafanaToken)

		builder, err := generator.GenDashboard(params)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if params.GrafanaUrl != "" {
			err := generator.SyncDashboard(ctx, params, builder)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		} else {
			fmt.Println(builder.MarshalIndentJSON())
		}
	}
}

func genStatesFile(ctx context.Context) func(
	cmd *cobra.Command, args []string,
) {
	return func(cmd *cobra.Command, args []string) {
		params := cli.ParseSFParams(cmd, args)
		name := fmt.Sprintf("ss_%s.go", camelToSnake(params.Name))

		if !fileExists(name) || params.Force {

			// generate
			gen, err := generator.NewSFGenerator(ctx, params)
			if err != nil {
				panic(err)
			}

			// save ss_
			content := []byte(gen.Output())
			err = os.WriteFile(name, content, 0666)
			if err != nil {
				panic(err)
			}
			fmt.Printf("Generated %s\n", name)

			// save utils
			content = []byte(generator.GenUtilsFile())
			err = os.WriteFile("states_utils.go", content, 0666)
			if err != nil {
				panic(err)
			}
			fmt.Println("Generated states_utils.go")

		} else {
			fmt.Printf("Error: file %s already exists, delete it or use --force\n",
				name)
			os.Exit(1)
		}
	}
}

func camelToSnake(s string) string {
	re := regexp.MustCompile("([a-z0-9])([A-Z])")
	snake := re.ReplaceAllString(s, "${1}_${2}")
	return strings.ToLower(snake)
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	return !info.IsDir()
}
