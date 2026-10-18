package generator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/dave/jennifer/jen"
	"gopkg.in/yaml.v3"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/generator/cli"
)

const (
	pkgHelpers = "github.com/pancsta/asyncmachine-go/pkg/helpers"
	pkgTesting = "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	pkgRpc     = "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

// GenStarterFiles generates all file contents for a starter kit project
// without writing them to disk. Returns a map of relative file path -> content.
func GenStarterFiles(
	ctx context.Context, params cli.StarterParams,
) (map[string]string, error) {
	var err error
	if len(params.FileContent) == 0 && params.File != "" {
		params.FileContent, err = os.ReadFile(params.File)
		if err != nil {
			return nil,
				fmt.Errorf("reading YAML schema file %s: %w", params.File, err)
		}
	}
	if len(params.FileContent) == 0 {
		return nil, fmt.Errorf("empty schema file content")
	}

	name := params.Name
	if name == "" {
		name = "MyMach"
	}
	name = capitalizeFirstLetter(name)
	nameSnake := camelToSnake(name)
	pkgName := strings.ReplaceAll(nameSnake, "_", "")

	var schema am.Schema
	if err := yaml.Unmarshal(params.FileContent, &schema); err != nil {
		return nil, fmt.Errorf("parsing YAML schema: %w", err)
	}
	if len(schema) == 0 {
		return nil, fmt.Errorf("empty YAML schema")
	}

	// state order
	statesList := stateNames(params.FileContent)
	if len(statesList) == 0 {
		return nil, fmt.Errorf("no states found in schema")
	}

	var argsStates []string
	for _, sn := range statesList {
		st, ok := schema[sn]
		if ok && (slices.Contains(st.Tags, "args") ||
			slices.Contains(st.Tags, "#args")) {
			argsStates = append(argsStates, sn)
		}
	}

	inherit := params.Inherit
	if len(inherit) == 0 {
		inherit = []string{"basic", "disposed", "rpc/statesrc"}
	}

	// Target state for test
	// Generate the same test every time, when Start state isn't a part of
	// the schema
	testState := "Start"
	if !slices.Contains(inherit, "basic") &&
		!slices.Contains(statesList, "Start") {
		testState = statesList[0]
	}

	// 1. Generate schema file (states/ss_<name>.go)
	var stateTokens []string
	for _, sn := range statesList {
		st, ok := schema[sn]
		if !ok {
			continue
		}
		token := genState(sn, st)
		stateTokens = append(stateTokens, token)
	}

	schemaParams := cli.SchemaParams{
		States: strings.Join(stateTokens, ","),
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Name:    name,
			Inherit: inherit,
			Global:  true, // Always use --global for schema
		},
	}

	gen, err := NewSchemaGenerator(ctx, schemaParams)
	if err != nil {
		return nil, fmt.Errorf("creating schema generator: %w", err)
	}

	files := make(map[string]string)

	// states/ss_<name>.go
	schemaFilePath := filepath.Join("states", fmt.Sprintf("ss_%s.go", nameSnake))
	files[schemaFilePath] = gen.Output()

	// 2. Generate <name>.go
	uri := params.Uri
	if uri == "" {
		uri = "asyncmachine.dev/mymach"
	}
	statesImport := uri + "/states"

	mainCode := genMainFile(
		pkgName, name, nameSnake, statesImport, statesList, argsStates,
		params.Args,
	)
	mainFilePath := fmt.Sprintf("%s.go", nameSnake)
	files[mainFilePath] = mainCode

	// 3. Generate handlers.go (if params.Handlers)
	if params.Handlers {
		handlersCode := genHandlersFile(pkgName, statesList, argsStates)
		files["handlers.go"] = handlersCode
	}

	// 4. Generate <name>_test.go
	testCode := genTestFile(pkgName, "h", testState)
	testFilePath := fmt.Sprintf("%s_test.go", nameSnake)
	files[testFilePath] = testCode

	// 5. Generate go.mod (if params.Module)
	if params.Module {
		files["go.mod"] = fmt.Sprintf(
			"module %s\n\ngo 1.26\n\n"+
				"require github.com/pancsta/asyncmachine-go v0.19.2\n",
			uri,
		)
	}

	return files, nil
}

// GenStarterKit writes all starter files to disk.
func GenStarterKit(ctx context.Context, params *cli.StarterParams) error {
	files, err := GenStarterFiles(ctx, *params)
	if err != nil {
		return err
	}

	dir := params.Path
	if dir == "" {
		dir = "."
	}

	dir = filepath.Join(dir, camelToSnake(params.Name))

	for relPath, content := range files {
		fullPath := filepath.Join(dir, relPath)
		if fileExists(fullPath) && !params.Force {
			return fmt.Errorf(
				"file %s already exists, delete it or use --force", fullPath,
			)
		}

		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", fullPath, err)
		}

		if err := os.WriteFile(fullPath, []byte(content), 0o666); err != nil {
			return fmt.Errorf("writing file %s: %w", fullPath, err)
		}

		fmt.Printf("Generated %s\n", fullPath)
	}

	if params.Module {
		cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
		cmd.Dir = dir
		_ = cmd.Run()
	}

	return nil
}

func genMainFile(
	pkgName, name, nameSnake, statesImport string, statesList,
	argsStates []string, genArgs bool,
) string {
	f := jen.NewFile(pkgName)
	f.ImportAlias(pkgHelpers, "amhelp")
	f.ImportAlias(pkgMachine, "am")
	f.ImportAlias(pkgStates, "ssam")
	f.ImportName(statesImport, "states")
	f.ImportAlias(pkgRpc, "arpc")
	if genArgs {
		f.ImportName("encoding/gob", "gob")
	}

	f.Var().Id("isDebug").Op("=").False()

	f.Func().Id("init").Params().Block(
		jen.If(jen.Op("!").Id("isDebug")).Block(
			jen.Return(),
		),
		jen.Line(),
		jen.Comment("manual logging"),
		jen.Comment("amhelp.SetEnvLogLevel(am.LogOps)"),
		jen.Comment("os.Setenv(amhelp.EnvAmLogPrint, \"2\")"),
		jen.Line(),
		jen.Comment("am-dbg is required for debugging, go run it"),
		jen.Comment("go run "+
			"github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest"),
		jen.Qual(pkgHelpers, "EnableDebugging").Call(jen.True()),
	)

	f.Var().Id("ss").Op("=").Qual(statesImport, name+"States")

	f.Comment("// ///// ///// /////")
	f.Line()
	f.Comment("// ///// MACHINE")
	f.Line()
	f.Comment("// ///// ///// /////")
	f.Line()

	f.Func().Id("New").Params(
		jen.Id("ctx").Qual("context", "Context"),
	).Params(
		jen.Op("*").Id("Handlers"),
		jen.Error(),
	).Block(
		jen.Comment("handlers"),
		jen.Id("handlers").Op(":=").Op("&").Id("Handlers").Values(jen.Dict{
			jen.Id("DisposedHandlers"): jen.Op("&").
				Qual(pkgStates, "DisposedHandlers").Values(),
		}),
		jen.List(jen.Id("mach"), jen.Err()).Op(":=").
			Qual(pkgMachine, "NewCommon").Call(
			jen.Id("ctx"),
			jen.Lit(nameSnake),
			jen.Qual(statesImport, name+"Schema"),
			jen.Id("ss").Dot("Names").Call(),
			jen.Id("handlers"),
			jen.Nil(),
			jen.Nil(),
		),
		jen.If(jen.Err().Op("!=").Nil()).Block(
			jen.Return(jen.Nil(), jen.Err()),
		),
		jen.Id("handlers").Dot("Mach").Op("=").Id("mach"),
		jen.Line(),
		jen.Comment("telemetry and logging"),
		jen.Id("mach").Dot("SetGroups").Call(
			jen.Qual(statesImport, name+"Groups"),
			jen.Qual(statesImport, name+"States"),
		),
		jen.Comment("mach.SemLogger().SetLevel(am.LogChanges)"),
		jen.Id("mach").Dot("SemLogger").Call().Dot("SetArgsMapper").Call(
			jen.Qual(pkgHelpers, "LogArgsMapper"),
		),
		jen.Qual(pkgHelpers, "MachDebugEnv").Call(jen.Id("mach")),
		func() jen.Code {
			opts := jen.Dict{
				jen.Id("AddrDir"): jen.Lit("."),
			}
			if genArgs {
				opts[jen.Id("Args")] = jen.Id("ArgsRpc")
			}
			return jen.List(jen.Err(), jen.Id("_")).Op("=").
				Qual(pkgRpc, "MachReplEnv").Call(
				jen.Id("mach"),
				jen.Op("&").Qual(pkgRpc, "ReplOpts").Values(opts),
			)
		}(),

		jen.Line(),
		jen.Return(jen.Id("handlers"), jen.Nil()),
	)

	f.Comment("// ///// ///// /////")
	f.Line()
	f.Comment("// ///// HANDLERS")
	f.Line()
	f.Comment("// ///// ///// /////")
	f.Line()

	f.Comment("// see handler.go for state-state handlers")
	f.Line()

	f.Type().Id("Handlers").Struct(
		jen.Op("*").Qual(pkgMachine, "ExceptionHandler"),
		jen.Op("*").Qual(pkgStates, "DisposedHandlers"),
		jen.Line(),
		jen.Id("Mach").Op("*").Qual(pkgMachine, "Machine"),
	)

	for _, st := range statesList {
		f.Var().Id("_").Op("=").Id("ss").Dot(st)
		f.Func().Params(jen.Id("h").Op("*").Id("Handlers")).
			Id(st + "Enter").Params(jen.Id("e").Op("*").
			Qual(pkgMachine, "Event")).Bool().Block(
			jen.Return(jen.True()),
		)
		f.Func().Params(jen.Id("h").Op("*").Id("Handlers")).
			Id(st + "State").Params(jen.Id("e").Op("*").
			Qual(pkgMachine, "Event")).Block()
		f.Func().Params(jen.Id("h").Op("*").Id("Handlers")).
			Id(st + "Exit").Params(jen.Id("e").Op("*").
			Qual(pkgMachine, "Event")).Bool().Block(
			jen.Return(jen.True()),
		)
		f.Func().Params(jen.Id("h").Op("*").Id("Handlers")).
			Id(st + "End").Params(jen.Id("e").Op("*").
			Qual(pkgMachine, "Event")).Block()
	}

	if genArgs {
		f.Comment("// ///// ///// /////")
		f.Line()
		f.Comment("// ///// ARGS")
		f.Line()
		f.Comment("// ///// ///// /////")
		f.Line()

		f.Const().Id("APrefix").Op("=").Lit(nameSnake)

		f.Comment("// Args is shared pkg args for Any state")
		f.Type().Id("Args").Struct(
			jen.Qual(pkgMachine, "ArgsBase").Tag(map[string]string{"json": "-"}),
		)

		f.Func().Params(jen.Id("Args")).Id("ArgsPrefix").Params().String().Block(
			jen.Return(jen.Id("APrefix")),
		)

		f.Line()
		f.Var().Id("ArgsRpc").Op("=").Index().Qual(pkgMachine, "ArgsApi").Values()
		f.Line()

		f.Func().Id("init").Params().Block(
			jen.For(jen.List(jen.Id("_"), jen.Id("arg")).
				Op(":=").Range().Id("ArgsRpc")).Block(
				jen.Qual("encoding/gob", "Register").Call(jen.Id("arg")),
			),
		)

		f.Comment("// A is an args struct common for all state handlers.")
		f.Type().Id("A").Struct(
			jen.Id("Args").Tag(map[string]string{"json": "-"}),
			jen.Line(),
			jen.Comment("// Return chan."),
			jen.Id("ReturnCh").Op("chan<-").Index().String(),
		)

		f.Func().Params(jen.Id("A")).Id("ArgsState").Params().String().Block(
			jen.Return(jen.Qual(pkgMachine, "StateAny")),
		)

		if len(argsStates) > 0 {
			f.Comment("// ----- per-state typed args")
			f.Line()
			for _, st := range argsStates {
				f.Type().Id("A"+st).Struct(
					jen.Id("Args").Tag(map[string]string{"json": "-"}),
					jen.Line(),
					jen.Comment("// TODO fields for "+st),
				)
				f.Func().Params(jen.Id("A" + st)).Id("ArgsState").
					Params().String().Block(
					jen.Return(jen.Id("ss").Dot(st)),
				)
			}
		}
	}

	return foldBoolReturnTrue(f.GoString())
}

func genHandlersFile(pkgName string, statesList, argsStates []string) string {
	f := jen.NewFile(pkgName)
	f.ImportAlias(pkgMachine, "am")

	if len(statesList) == 0 {
		return f.GoString()
	}

	for _, s1 := range statesList {
		f.Var().Id("_").Op("=").Id("ss").Dot(s1)
		f.Comment("// state-state negotiation handlers")
		f.Line()

		for _, s2 := range statesList {
			if s1 == s2 || slices.Contains(argsStates, s2) {
				continue
			}
			f.Func().Params(jen.Id("h").Op("*").Id("Handlers")).
				Id(s1 + s2).Params(jen.Id("e").Op("*").
				Qual(pkgMachine, "Event")).Bool().Block(
				jen.Return(jen.True()),
			)
		}

		f.Comment("// globals")
		f.Line()
		f.Func().Params(jen.Id("h").Op("*").Id("Handlers")).
			Id(s1 + "Any").Params(jen.Id("e").Op("*").
			Qual(pkgMachine, "Event")).Bool().Block(
			jen.Return(jen.True()),
		)
		f.Func().Params(jen.Id("h").Op("*").Id("Handlers")).
			Id("Any" + s1).Params(jen.Id("e").Op("*").
			Qual(pkgMachine, "Event")).Bool().Block(
			jen.Return(jen.True()),
		)
	}

	return foldBoolReturnTrue(f.GoString())
}

func foldBoolReturnTrue(s string) string {
	return strings.ReplaceAll(
		s, "bool {\n\treturn true\n}", "bool { return true }",
	)
}

func genTestFile(pkgName, handlerName, testState string) string {
	f := jen.NewFile(pkgName)
	f.ImportAlias(pkgTesting, "amhelpt")
	f.ImportAlias(pkgHelpers, "amhelp")

	f.Func().Id("Test"+testState).Params(
		jen.Id("t").Op("*").Qual("testing", "T"),
	).Block(
		jen.Id("ctx").Op(":=").Qual("context", "Background").Call(),
		jen.List(jen.Id(handlerName), jen.Err()).Op(":=").
			Id("New").Call(jen.Id("ctx")),
		jen.If(jen.Err().Op("!=").Nil()).Block(
			jen.Id("t").Dot("Fatal").Call(jen.Err()),
		),
		jen.Line(),
		jen.Comment("test "+testState),
		jen.Id("mach").Op(":=").Id(handlerName).Dot("Mach"),
		jen.Id("mach").Dot("Add1").Call(
			jen.Id("ss").Dot(testState), jen.Nil(),
		),
		jen.Qual(pkgTesting, "AssertIs1").Call(
			jen.Id("t"), jen.Id("mach"), jen.Id("ss").Dot(testState),
		),
		jen.Id("mach").Dot("GoAfter").Call(
			jen.Id("ctx"),
			jen.Qual("time", "Second"),
			jen.Func().Params().Block(
				jen.Id("mach").Dot("Remove1").Call(
					jen.Id("ss").Dot(testState), jen.Nil(),
				),
			),
		),
		jen.Op("<-").Id("mach").Dot("WhenNot1").Call(
			jen.Id("ss").Dot(testState), jen.Id("ctx"),
		),
		jen.Comment("enable debug with `true` or AM_DBG_ADDR=1"),
		jen.If(jen.Qual(pkgHelpers, "IsDebug").Call()).Block(
			jen.Qual("time", "Sleep").Call(
				jen.Lit(100).Op("*").Qual("time", "Millisecond"),
			),
		),
	)

	return f.GoString()
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
