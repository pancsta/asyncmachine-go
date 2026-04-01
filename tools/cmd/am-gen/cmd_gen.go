// am-gen generates schema files and Grafana dashboards.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/joho/godotenv"
	"github.com/pancsta/asyncmachine-go/internal/utils"
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

	var args cli.Args
	p := arg.MustParse(&args)

	if args.Version {
		fmt.Println(utils.GetVersion())
		os.Exit(0)
	}

	if p.Subcommand() == nil {
		p.Fail("missing subcommand (states-file, grafana)\n" + args.Description())
	}

	switch {
	case args.StatesFile != nil:
		if err := genStatesFile(ctx, args.StatesFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case args.Grafana != nil:
		args.Grafana.Token = os.Getenv(EnvGrafanaToken)
		if err := genGrafana(ctx, args.Grafana); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
}

func genGrafana(ctx context.Context, params *cli.GrafanaParams) error {
	builder, err := generator.GenDashboard(*params)
	if err != nil {
		return err
	}

	if params.GrafanaUrl != "" {
		return generator.SyncDashboard(ctx, *params, builder)
	}

	fmt.Println(builder.MarshalIndentJSON())
	return nil
}

func genStatesFile(ctx context.Context, params *cli.SFParams) error {
	name := fmt.Sprintf("ss_%s.go", camelToSnake(params.Name))

	if !fileExists(name) || params.Force {

		// generate
		gen, err := generator.NewSchemaGenerator(ctx, *params)
		if err != nil {
			return err
		}

		// save ss_
		content := []byte(gen.Output())
		err = os.WriteFile(name, content, 0666)
		if err != nil {
			return err
		}
		fmt.Printf("Generated %s\n", name)

		// save utils
		content = []byte(generator.GenUtilsFile())
		err = os.WriteFile("states_utils.go", content, 0666)
		if err != nil {
			return err
		}
		fmt.Println("Generated states_utils.go")

	} else {
		return fmt.Errorf("file %s already exists, delete it or use --force", name)
	}

	return nil
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
