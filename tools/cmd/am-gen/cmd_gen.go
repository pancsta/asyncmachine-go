// am-gen generates schema files and Grafana dashboards.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
		p.Fail("missing subcommand (starter-kit, schema, schema-from-file, " +
			"grafana, lint)\n" + args.Description())
	}

	switch {
	case args.Starter != nil:
		if err := genStarterKit(ctx, args.Starter); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case args.Schema != nil:
		if err := genStatesFile(ctx, args.Schema); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case args.StatesFile != nil:
		if err := genStatesFile(ctx, args.StatesFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case args.SchemaFile != nil:
		if err := genSchemaYaml(ctx, args.SchemaFile); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case args.Grafana != nil:
		args.Grafana.Token = os.Getenv(EnvGrafanaToken)
		if err := genGrafana(ctx, args.Grafana); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case args.Lint != nil:
		if err := lintSchema(args.Lint); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
}

func genStarterKit(ctx context.Context, params *cli.StarterParams) error {
	return generator.GenStarterKit(ctx, params)
}

func genSchemaYaml(ctx context.Context, params *cli.SchemaFileParams) error {
	statesParams, err := generator.SchemaFileToParams(*params)
	if err != nil {
		return err
	}
	return genStatesFile(ctx, &statesParams)
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

func genStatesFile(ctx context.Context, params *cli.SchemaParams) error {
	if params.States == "" && len(params.State) == 0 {
		return errors.New("either --state or --states is required")
	}

	// generate
	gen, err := generator.NewSchemaGenerator(ctx, *params)
	if err != nil {
		return err
	}

	if params.Output {
		fmt.Print(gen.Output())
		return nil
	}

	name := fmt.Sprintf("ss_%s.go", camelToSnake(params.Name))

	if !fileExists(name) || params.Force {
		// save ss_
		content := []byte(gen.Output())
		err = os.WriteFile(name, content, 0666)
		if err != nil {
			return err
		}
		fmt.Printf("Generated %s\n", name)

		// save utils
		if !params.Global && params.Utils {
			content = []byte(generator.GenUtilsFile())
			err = os.WriteFile("states_utils.go", content, 0666)
			if err != nil {
				return err
			}
			fmt.Println("Generated states_utils.go")
		}

	} else {
		return fmt.Errorf("file %s already exists, delete it or use --force", name)
	}

	return nil
}

// lintFiles resolves the input path to a list of ss_*.go files. A directory
// resolves to all ss_*.go files inside it (non-recursive), an empty path to
// the current directory.
func lintFiles(path string) ([]string, error) {
	if path == "" {
		path = "."
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}

	matches, err := filepath.Glob(filepath.Join(path, "ss_*.go"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no ss_*.go files found in %s", path)
	}
	return matches, nil
}

func lintSchema(params *cli.LintParams) error {
	files, err := lintFiles(params.File)
	if err != nil {
		return err
	}

	changed := false
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}

		res, err := generator.LintSchemaOrder(file, src)
		if err != nil {
			return err
		}
		if !res.Changed {
			continue
		}
		changed = true

		if params.DryRun {
			fmt.Printf("--- %s (dry-run) ---\n", file)
			fmt.Print(string(res.Src))
			continue
		}

		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, res.Src, info.Mode().Perm()); err != nil {
			return err
		}

		msgs := make([]string, 0, 2)
		if res.Reordered {
			msgs = append(msgs, "reordered")
		}
		if len(res.Added) > 0 {
			msgs = append(msgs, "added "+strings.Join(res.Added, ", "))
		}
		fmt.Printf("Linted %s (%s)\n", file, strings.Join(msgs, ", "))
	}

	if !changed {
		fmt.Println("No changes")
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
