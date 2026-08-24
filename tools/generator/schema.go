// Package generator generates state-machine schemas and grafana dashboards.
package generator

// TODO rewrite:
//  - repeated cli params
//  - AST
//  - embed pkg/states/states_utils.go
//  	- optional with pkg/states/global

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/dave/jennifer/jen"
	"gopkg.in/yaml.v3"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/tools/generator/cli"
	"github.com/pancsta/asyncmachine-go/tools/generator/states"
)

const (
	pkgMachine    = "github.com/pancsta/asyncmachine-go/pkg/machine"
	pkgStates     = "github.com/pancsta/asyncmachine-go/pkg/states"
	pkgRpcStates  = "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	pkgNodeStates = "github.com/pancsta/asyncmachine-go/pkg/node/states"
	pkgGlobal     = "github.com/pancsta/asyncmachine-go/pkg/states/global"
)

var (
	ssG = states.GeneratorStates
	sgG = states.GeneratorGroups
)

type SchemaGenerator struct {
	Mach *am.Machine

	Name string
	// N is the first letter of Name
	N           string
	Global      bool
	States      []string
	StatesAuto  []string
	StatesMulti []string
	Groups      []string
	// State1 -> Rel -> State2,State3
	Relations [][3]string
	StateTags map[string][]string
}

// TODO return err
func (g *SchemaGenerator) parseParams(p cli.SchemaParams) {
	g.StateTags = make(map[string][]string)

	for _, item := range p.Inherit {
		for _, inherit := range strings.Split(item, ",") {
			inherit = strings.TrimSpace(inherit)
			if inherit == "" {
				continue
			}
			// TODO enum, merge with CLI
			switch inherit {
			case "basic":
				g.Mach.Add1(ssG.InheritBasic, nil)
			case "connected":
				g.Mach.Add1(ssG.InheritConnected, nil)
			case "disposed":
				g.Mach.Add1(ssG.InheritDisposed, nil)
			case "rpc/statesrc":
				g.Mach.Add1(ssG.InheritRpcStateSource, nil)
			case "node/worker":
				g.Mach.Add1(ssG.InheritNodeWorker, nil)
			default:
				// TODO err
				panic(fmt.Sprintf("unknown inherit: %s", inherit))
			}
		}
	}

	// states
	var rawStates []string
	if p.States != "" {
		rawStates = append(rawStates, strings.Split(p.States, ",")...)
	}
	rawStates = append(rawStates, p.State...)

	reTag := regexp.MustCompile(`#([a-zA-Z0-9_\-\/]+)`)

	for _, state := range rawStates {
		state = strings.TrimSpace(state)
		if state == "" {
			continue
		}

		var tags []string
		matches := reTag.FindAllStringSubmatch(state, -1)
		for _, match := range matches {
			if len(match) > 1 && match[1] != "" {
				tags = append(tags, match[1])
			}
		}
		state = reTag.ReplaceAllString(state, "")

		// multi, auto, relations
		props := strings.Split(state, ":")
		name := capitalizeFirstLetter(props[0])
		g.States = append(g.States, name)

		if len(tags) > 0 {
			g.StateTags[name] = tags
		}

		if len(props) < 2 {
			continue
		}
		props = props[1:]

		for _, prop := range props {
			if prop == "" {
				continue
			}
			switch prop {
			case "auto":
				g.StatesAuto = append(g.StatesAuto, name)
			case "multi":
				g.StatesMulti = append(g.StatesMulti, name)
			default:
				// Require(
				if !strings.Contains(prop, "(") {
					fmt.Printf("wrong format")
					os.Exit(1)
				}

				rel := strings.Split(strings.TrimRight(prop, ")"), "(")
				if len(rel[0]) == 0 || len(rel[1]) == 0 {
					fmt.Printf("wrong format")
					os.Exit(1)
				}
				relName := capitalizeFirstLetter(rel[0])
				relStates := rel[1]

				g.Relations = append(g.Relations, [3]string{name, relName, relStates})
			}
		}
	}

	// groups
	var rawGroups []string
	if p.Groups != "" {
		rawGroups = append(rawGroups, strings.Split(p.Groups, ",")...)
	}
	rawGroups = append(rawGroups, p.Group...)

	for _, group := range rawGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		g.Groups = append(g.Groups, capitalizeFirstLetter(group))
		g.Mach.Add1(ssG.GroupsLocal, nil)
	}

	g.Name = capitalizeFirstLetter(p.Name)
	g.N = string(g.Name[0])
	g.Global = p.Global
}

var _ = ssG.Inherit

func (g *SchemaGenerator) InheritEnter(e *am.Event) bool {
	return g.Mach.Any1(sgG.Inherit...)
}

var _ = ssG.Groups

func (g *SchemaGenerator) GroupsEnter(e *am.Event) bool {
	return g.Mach.Any1(ssG.GroupsInherited, ssG.GroupsLocal)
}

// Output renders the generated schema file using github.com/dave/jennifer.
func (g *SchemaGenerator) Output() string {
	ssN := "ss" + g.N
	sgN := "sg" + g.N

	f := jen.NewFile("states")

	f.ImportAlias(pkgMachine, "am")
	if g.Mach.Any1(ssG.InheritBasic, ssG.InheritConnected, ssG.InheritDisposed) {
		f.ImportAlias(pkgStates, "ssam")
	}
	if g.Mach.Is1(ssG.InheritRpcStateSource) {
		f.ImportAlias(pkgRpcStates, "ssrpc")
	}
	if g.Mach.Is1(ssG.InheritNodeWorker) {
		f.ImportAlias(pkgNodeStates, "ssnode")
	}
	if g.Global {
		f.ImportAlias(pkgGlobal, ".")
		// --global replaces the local states_utils.go (S, SAdd, ...) with a
		// dot-import of pkg/states/global. jennifer only auto-registers imports
		// that are referenced via Qual, but this one must always be present even
		// if this particular file ends up not using any of its symbols (e.g. no
		// groups/relations), so force it and fix up the alias below.
		f.Anon(pkgGlobal)
	}

	g.genStatesDef(f)
	g.genGroupsDef(f)
	g.genSchemaVar(f, ssN)
	g.genExports(f, ssN, sgN)
	g.genConstructor(f)

	out := f.GoString()
	if g.Global {
		out = strings.Replace(out, `_ "`+pkgGlobal+`"`, `. "`+pkgGlobal+`"`, 1)
	}

	return out
}

// genStatesDef renders the {Name}StatesDef struct.
func (g *SchemaGenerator) genStatesDef(f *jen.File) {
	f.Commentf("%sStatesDef contains all the states of the [%s] state-machine.",
		g.Name, g.Name)

	f.Type().Id(g.Name + "StatesDef").StructFunc(func(s *jen.Group) {
		s.Op("*").Qual(pkgMachine, "StatesBase")
		s.Line()

		for _, state := range g.States {
			s.Id(state).String()
		}

		firstInherit := true
		addInherit := func(comment, path, typ string) {
			if firstInherit {
				s.Line()
				firstInherit = false
			}
			s.Comment(comment)
			s.Op("*").Qual(path, typ)
		}

		if g.Mach.Is1(ssG.InheritBasic) {
			addInherit(
				"inherit from BasicStatesDef", pkgStates, "BasicStatesDef",
			)
		}
		if g.Mach.Is1(ssG.InheritConnected) {
			addInherit(
				"inherit from ConnectedStatesDef", pkgStates, "ConnectedStatesDef",
			)
		}
		if g.Mach.Is1(ssG.InheritDisposed) {
			addInherit(
				"inherit from DisposedStatesDef", pkgStates, "DisposedStatesDef",
			)
		}
		if g.Mach.Is1(ssG.InheritRpcStateSource) {
			addInherit(
				"inherit from rpc/StateSourceStatesDef", pkgRpcStates,
				"StateSourceStatesDef",
			)
		}
		if g.Mach.Is1(ssG.InheritNodeWorker) {
			addInherit(
				"inherit from node/StateSourceStatesDef", pkgNodeStates,
				"StateSourceStatesDef",
			)
		}
	})
}

// genGroupsDef renders the {Name}GroupsDef struct.
func (g *SchemaGenerator) genGroupsDef(f *jen.File) {
	f.Commentf("%sGroupsDef contains all the state groups [%s] state-machine.",
		g.Name, g.Name)

	f.Type().Id(g.Name + "GroupsDef").StructFunc(func(s *jen.Group) {
		if g.Mach.Is1(ssG.InheritConnected) {
			s.Op("*").Qual(pkgStates, "ConnectedGroupsDef")
		}
		if g.Mach.Is1(ssG.InheritNodeWorker) {
			s.Op("*").Qual(pkgNodeStates, "WorkerGroupsDef")
		}
		for _, group := range g.Groups {
			s.Id(strings.Split(group, "(")[0]).Add(g.idS())
		}
	})
}

// idS resolves the S type/identifier: package-qualified (dot-imported) when
// the schema is generated with --global, otherwise a plain local identifier
// coming from the package-local states_utils.go. Both render as the bare
// "S" token; only the import registration differs.
func (g *SchemaGenerator) idS() *jen.Statement {
	if g.Global {
		return jen.Qual(pkgGlobal, "S")
	}
	return jen.Id("S")
}

// multiLit renders a bracketed, comma-separated, always-multiline list of
// items. Unlike jen.Values/jen.Call, this keeps one item per line even when
// items carry their own leading comment, so comments stay attached to the
// right item once gofmt reflows the call/literal.
func multiLit(open, close string, items ...jen.Code) *jen.Statement {
	return jen.Custom(jen.Options{
		Open:      open,
		Close:     close,
		Separator: ",",
		Multi:     true,
	}, items...)
}

// genSchemaVar renders the {Name}Schema var, merging inherited schemas (if
// any) with the local one.
func (g *SchemaGenerator) genSchemaVar(f *jen.File, ssN string) {
	f.Commentf("%sSchema represents all relations and properties of [%sStates].",
		g.Name, g.Name)

	schemaLit := jen.Qual(pkgMachine, "Schema").
		Add(multiLit("{", "}", g.stateEntries(ssN)...))
	if !g.Mach.Is1(ssG.Inherit) {
		f.Var().Id(g.Name + "Schema").Op("=").Add(schemaLit)
		return
	}

	var mergeArgs []jen.Code
	addInherit := func(comment string, code jen.Code) {
		mergeArgs = append(mergeArgs, jen.Comment(comment).Line().Add(code))
	}

	if g.Mach.Is1(ssG.InheritBasic) {
		addInherit("inherit from BasicSchema",
			jen.Qual(pkgStates, "BasicSchema"))
	}
	if g.Mach.Is1(ssG.InheritConnected) {
		addInherit("inherit from ConnectedSchema",
			jen.Qual(pkgStates, "ConnectedSchema"))
	}
	if g.Mach.Is1(ssG.InheritDisposed) {
		addInherit("inherit from DisposedSchema",
			jen.Qual(pkgStates, "DisposedSchema"))
	}
	if g.Mach.Is1(ssG.InheritRpcStateSource) {
		addInherit("inherit from rpc/StateSourceSchema",
			jen.Qual(pkgRpcStates, "StateSourceSchema"))
	}
	if g.Mach.Is1(ssG.InheritNodeWorker) {
		addInherit("inherit from node/WorkerSchema",
			jen.Qual(pkgNodeStates, "WorkerSchema"))
	}
	mergeArgs = append(mergeArgs, schemaLit)

	f.Var().Id(g.Name+"Schema").Op("=").
		Qual(pkgMachine, "Schema").Values().Dot("Merge").
		Add(multiLit("(", ")", mergeArgs...))
}

// stateEntries renders the ordered ss{N}.State: {...} entries of a schema
// literal.
func (g *SchemaGenerator) stateEntries(ssN string) []jen.Code {
	var entries []jen.Code
	for _, state := range g.States {
		entries = append(entries,
			jen.Id(ssN).Dot(state).Op(":").Add(g.stateValue(ssN, state)))
	}
	return entries
}

// stateValue renders the {Auto, Multi, <relations>, Tags} value of a single
// state.
func (g *SchemaGenerator) stateValue(ssN, state string) jen.Code {
	var props []jen.Code

	if slices.Contains(g.StatesAuto, state) {
		props = append(props, jen.Id("Auto").Op(":").True())
	}
	if slices.Contains(g.StatesMulti, state) {
		props = append(props, jen.Id("Multi").Op(":").True())
	}
	for _, rel := range g.Relations {
		if rel[0] != state {
			continue
		}
		props = append(props, g.relationValue(ssN, rel))
	}
	if tags, ok := g.StateTags[state]; ok && len(tags) > 0 {
		var tagLits []jen.Code
		for _, tag := range tags {
			tagLits = append(tagLits, jen.Lit(tag))
		}
		props = append(props,
			jen.Id("Tags").Op(":").Index().String().Values(tagLits...))
	}

	return multiLit("{", "}", props...)
}

// relationValue renders a single Require/Add/Remove/etc relation, either to
// other states, or to a group (optionally extended with extra states).
func (g *SchemaGenerator) relationValue(ssN string, rel [3]string) jen.Code {
	sgN := "sg" + g.N
	source := strings.Split(rel[2], ";")

	// relation to a group TODO >1
	if strings.HasPrefix(source[0], "_") {
		group := jen.Id(sgN).Dot(source[0][1:])
		var extra []jen.Code
		if len(source) > 1 {
			extra = append(extra, g.statesLit(ssN, source[1:]))
		}
		return jen.Id(rel[1]).Op(":").Add(group).Dot("Add").Call(extra...)
	}

	// relation to states only
	return jen.Id(rel[1]).Op(":").Add(g.statesLit(ssN, source))
}

// statesLit renders an S{ss{N}.State1, ss{N}.State2, ...} literal.
func (g *SchemaGenerator) statesLit(ssN string, targets []string) jen.Code {
	var items []jen.Code
	for _, target := range targets {
		items = append(items, jen.Id(ssN).Dot(target))
	}
	return g.idS().Values(items...)
}

// genExports renders the EXPORTS AND GROUPS var block.
func (g *SchemaGenerator) genExports(f *jen.File, ssN, sgN string) {
	f.Comment("EXPORTS AND GROUPS")

	f.Var().DefsFunc(func(v *jen.Group) {
		v.Id(ssN).Op("=").Qual(pkgMachine, "NewStates").
			Call(jen.Id(g.Name + "StatesDef").Values())

		newGroupsArgs := []jen.Code{
			jen.Id(g.Name + "GroupsDef").
				Add(multiLit("{", "}", g.groupsDefEntries(ssN)...)),
		}
		if g.Mach.Is1(ssG.InheritConnected) {
			newGroupsArgs = append(newGroupsArgs,
				jen.Qual(pkgStates, "ConnectedGroups"))
		}
		if g.Mach.Is1(ssG.InheritNodeWorker) {
			newGroupsArgs = append(newGroupsArgs,
				jen.Qual(pkgNodeStates, "WorkerGroups"))
		}
		v.Id(sgN).Op("=").Qual(pkgMachine, "NewStateGroups").Call(newGroupsArgs...)

		v.Line()
		v.Commentf("%sStates contains all the states for the [%s] state-machine.",
			g.Name, g.Name)
		v.Id(g.Name + "States").Op("=").Id(ssN)
		v.Commentf(
			"%sGroups contains all the state groups for the [%s] state-machine.",
			g.Name, g.Name,
		)
		v.Id(g.Name + "Groups").Op("=").Id(sgN)
	})
}

// groupsDefEntries renders the ordered Group: S{...} entries of a
// {Name}GroupsDef literal.
func (g *SchemaGenerator) groupsDefEntries(ssN string) []jen.Code {
	var entries []jen.Code
	for _, group := range g.Groups {
		if strings.Contains(group, "(") {
			data := strings.Split(strings.TrimRight(group, ")"), "(")
			states := strings.Split(data[1], ";")
			entries = append(entries,
				jen.Id(data[0]).Op(":").Add(g.statesLit(ssN, states)))
		} else {
			entries = append(entries, jen.Id(group).Op(":").Add(g.idS().Values()))
		}
	}
	return entries
}

// genConstructor renders the New{Name} constructor function.
func (g *SchemaGenerator) genConstructor(f *jen.File) {
	f.Commentf("New%s creates a new [%s] state-machine in the most basic form.",
		g.Name, g.Name)

	f.Func().Id("New"+g.Name).
		Params(jen.Id("ctx").Qual("context", "Context")).
		Op("*").Qual(pkgMachine, "Machine").
		Block(
			jen.Return(jen.Qual(pkgMachine, "New").
				Call(jen.Id("ctx"), jen.Id(g.Name+"Schema"), jen.Nil())),
		)
}

func NewSchemaGenerator(
	ctx context.Context, param cli.SchemaParams,
) (*SchemaGenerator, error) {
	g := &SchemaGenerator{}
	mach, err := am.NewCommon(ctx, "gen", states.GeneratorSchema, ssG.Names(),
		g, nil, nil)
	if err != nil {
		return nil, err
	}
	// TODO env var?
	// amhelp.MachDebugEnv(mach)

	g.Mach = mach
	g.parseParams(param)

	return g, nil
}

func GenUtilsFile() string {
	return ssam.StatesUtilsFile
}

func capitalizeFirstLetter(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(unicode.ToUpper(rune(s[0]))) + s[1:]
}

// SCHEMA FROM FILE

// SchemaFileToParams reads the YAML schema file specified in params and
// converts it to StatesParams.
func SchemaFileToParams(params cli.SchemaFileParams) (cli.SchemaParams, error) {
	var err error
	if params.File != "" {
		params.FileContent, err = os.ReadFile(params.File)
		if err != nil {
			return cli.SchemaParams{},
				fmt.Errorf("reading YAML schema file %s: %w", params.File, err)
		}
	}

	// Check if this is an am.Serialized machine export
	var ser am.Serialized
	if err := yaml.Unmarshal(params.FileContent, &ser); err == nil &&
		len(ser.StateNames) > 0 {
		p := params.SchemaParamsCommon
		if p.Name == "" && ser.ID != "" {
			p.Name = ser.ID
		}
		return cli.SchemaParams{
			SchemaParamsCommon: p,
			States:             strings.Join(ser.StateNames, ","),
		}, nil
	}

	var schema am.Schema
	if err := yaml.Unmarshal(params.FileContent, &schema); err != nil {
		return cli.SchemaParams{}, fmt.Errorf("parsing YAML schema: %w", err)
	}
	if len(schema) == 0 {
		return cli.SchemaParams{}, errors.New("empty YAML schema")
	}

	// state order
	stateNames := stateNames(params.FileContent)
	if len(stateNames) == 0 {
		stateNames = slices.Collect(maps.Keys(schema))
	}

	var stateTokens []string
	for _, stateName := range stateNames {
		st, ok := schema[stateName]
		if !ok {
			continue
		}

		token := genState(stateName, st)
		stateTokens = append(stateTokens, token)
	}

	statesStr := strings.Join(stateTokens, ",")

	return cli.SchemaParams{
		SchemaParamsCommon: params.SchemaParamsCommon,
		States:             statesStr,
	}, nil
}

func genState(stateName string, st am.State) string {
	parts := []string{stateName}
	if st.Auto {
		parts = append(parts, "auto")
	}
	if st.Multi {
		parts = append(parts, "multi")
	}
	if len(st.Require) > 0 {
		req := strings.Join(st.Require, ";")
		parts = append(parts, fmt.Sprintf("Require(%s)", req))
	}
	if len(st.Add) > 0 {
		add := strings.Join(st.Add, ";")
		parts = append(parts, fmt.Sprintf("Add(%s)", add))
	}
	if len(st.Remove) > 0 {
		rem := strings.Join(st.Remove, ";")
		parts = append(parts, fmt.Sprintf("Remove(%s)", rem))
	}
	if len(st.After) > 0 {
		aft := strings.Join(st.After, ";")
		parts = append(parts, fmt.Sprintf("After(%s)", aft))
	}
	res := strings.Join(parts, ":")
	for _, tag := range st.Tags {
		tag = strings.TrimPrefix(tag, "#")
		if tag != "" {
			res += "#" + tag
		}
	}
	return res
}

func stateNames(yamlData []byte) []string {
	// read YAML key order
	var root yaml.Node
	var stateNames []string
	if err := yaml.Unmarshal(yamlData, &root); err == nil {
		var mapNode *yaml.Node
		if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
			mapNode = root.Content[0]
		} else if root.Kind == yaml.MappingNode {
			mapNode = &root
		}
		if mapNode != nil && mapNode.Kind == yaml.MappingNode {
			for i := 0; i < len(mapNode.Content); i += 2 {
				name := strings.TrimSpace(mapNode.Content[i].Value)
				if name != "" {
					stateNames = append(stateNames, name)
				}
			}
		}
	}
	return stateNames
}
