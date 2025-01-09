// TODO rewrite:
//  - repeated cli params
//  - AST
//  - embed pkg/states/states_utils.go

package generator

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/lithammer/dedent"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/generator/cli"
	"github.com/pancsta/asyncmachine-go/tools/generator/states"
)

var (
	ssG = states.GeneratorStates
	sgG = states.GeneratorGroups
)

type Generator struct {
	Mach *am.Machine

	Name string
	// N is the first letter of Name
	N           string
	States      []string
	StatesAuto  []string
	StatesMulti []string
	Groups      []string
	// State1 -> Rel -> State2,State3
	Relations [][3]string
}

// TODO return err
func (g *Generator) parseParams(p cli.SFParams) {
	if p.Inherit != "" {
		for _, inherit := range strings.Split(p.Inherit, ",") {
			switch inherit {
			case "basic":
				g.Mach.Add1(ssG.InheritBasic, nil)
			case "connected":
				g.Mach.Add1(ssG.InheritConnected, nil)
			case "rpc/worker":
				g.Mach.Add1(ssG.InheritRpcWorker, nil)
			case "node/worker":
				g.Mach.Add1(ssG.InheritNodeWorker, nil)
			default:
				// TODO err
				panic(fmt.Sprintf("unknown inherit: %s", inherit))
			}
		}
	}

	// states
	for _, state := range strings.Split(p.States, ",") {
		if state == "" {
			continue
		}

		// multi, auto, relations
		props := strings.Split(state, ":")
		name := capitalizeFirstLetter(props[0])
		g.States = append(g.States, name)

		if len(props) < 2 {
			continue
		}
		props = props[1:]

		for _, prop := range props {
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
	for _, group := range strings.Split(p.Groups, ",") {
		if group == "" {
			continue
		}
		g.Groups = append(g.Groups, capitalizeFirstLetter(group))
		g.Mach.Add1(ssG.GroupsLocal, nil)
	}

	g.Name = capitalizeFirstLetter(p.Name)
	g.N = string(g.Name[0])
}

func (g *Generator) InheritEnter(e *am.Event) bool {
	return g.Mach.Any1(sgG.Inherit...)
}

func (g *Generator) GroupsEnter(e *am.Event) bool {
	return g.Mach.Any1(ssG.GroupsInherited, ssG.GroupsLocal)
}

func (g *Generator) Output() string {
	// TODO switch to github.com/dave/jennifer

	var impPkgStates string
	if g.Mach.Any1(ssG.InheritBasic, ssG.InheritConnected) {
		impPkgStates = "\n\tss \"github.com/pancsta/asyncmachine-go/pkg/states\""
	}

	// imports
	out := p(`
		package states
		
		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"%s
	`, impPkgStates)

	if g.Mach.Is1(ssG.InheritRpcWorker) {
		out += "\tssrpc \"github.com/pancsta/asyncmachine-go/pkg/rpc/states\"\n"
	}
	if g.Mach.Is1(ssG.InheritNodeWorker) {
		out += "\tssnode \"github.com/pancsta/asyncmachine-go/pkg/node/states\"\n"
	}

	// struct def
	out += p(`
		)
		
		// %sStatesDef contains all the states of the %s state machine.
		type %sStatesDef struct {
			*am.StatesBase
	
	`, g.Name, g.Name, g.Name)

	// state names
	for _, state := range g.States {
		out += fmt.Sprintf("\t%s string\n", state)
	}

	out += "\n"

	// inherits
	if g.Mach.Is1(ssG.InheritBasic) {
		out += "\t// inherit from BasicStatesDef\n\t*ss.BasicStatesDef\n"
	}
	if g.Mach.Is1(ssG.InheritConnected) {
		out += "\t// inherit from ConnectedStatesDef\n\t*ss.ConnectedStatesDef\n"
	}
	if g.Mach.Is1(ssG.InheritRpcWorker) {
		out += "\t// inherit from rpc/WorkerStatesDef\n\t*ssrpc.WorkerStatesDef\n"
	}
	if g.Mach.Is1(ssG.InheritNodeWorker) {
		out += "\t// inherit from node/WorkerStatesDef\n" +
			"\t*ssnode.WorkerStatesDef\n"
	}

	out += "}\n\n"

	// groups def
	out += p(`
			// %sGroupsDef contains all the state groups %s state machine.
			type %sGroupsDef struct {
		`, g.Name, g.Name, g.Name)
	if g.Mach.Is1(ssG.InheritConnected) {
		out += "\t*ss.ConnectedGroupsDef\n"
	}
	if g.Mach.Is1(ssG.InheritNodeWorker) {
		out += "\t*ssnode.WorkerGroupsDef\n"
	}

	for _, group := range g.Groups {
		out += fmt.Sprintf("\t%s S\n", strings.Split(group, "(")[0])
	}
	out += "}\n\n"

	// struct
	out += fmt.Sprintf(
		"// %sStruct represents all relations and properties of %sStates.\n",
		g.Name, g.Name)
	var indent string
	if g.Mach.Is1(ssG.Inherit) {
		indent = "\t"
		out += fmt.Sprintf("var %sStruct = StructMerge(\n", g.Name)
	} else {
		out += fmt.Sprintf("var %sStruct = am.Struct{\n", g.Name)
	}

	// struct inherit
	if g.Mach.Is1(ssG.InheritBasic) {
		out += "\t// inherit from BasicStruct\n\tss.BasicStruct,\n"
	}
	if g.Mach.Is1(ssG.InheritConnected) {
		out += fmt.Sprintf("\t// inherit from ConnectedStruct\n" +
			"\tss.ConnectedStruct,\n")
	}
	if g.Mach.Is1(ssG.InheritRpcWorker) {
		out += fmt.Sprintf("\t// inherit from rpc/WorkerStruct\n" +
			"\tssrpc.WorkerStruct,\n")
	}
	if g.Mach.Is1(ssG.InheritNodeWorker) {
		out += fmt.Sprintf("\t// inherit from node/WorkerStruct\n" +
			"\tssnode.WorkerStruct,\n")
	}

	if g.Mach.Is1(ssG.Inherit) {
		out += "\tam.Struct{\n"
	}

	// struct states
	out += "\n"
	for _, state := range g.States {

		// open state
		out += fmt.Sprintf("\t%sss%s.%s: {", indent, g.N, state)
		var nl bool
		if slices.Contains(g.StatesAuto, state) {
			out += fmt.Sprintf("\n\t\t%sAuto: true,", indent)
			nl = true
		}
		if slices.Contains(g.StatesMulti, state) {
			out += fmt.Sprintf("\n\t\t%sMulti: true,", indent)
			nl = true
		}

		// relations
		for _, rel := range g.Relations {
			if rel[0] != state {
				continue
			}
			nl = true
			source := strings.Split(rel[2], ";")

			// relation to a group TODO >1
			if strings.HasPrefix(source[0], "_") {
				out += fmt.Sprintf("\n\t\t%s%s: SAdd(sg%s.%s",
					indent, rel[1], g.N, source[0][1:])

				if len(source) > 1 {
					out += ",S{"
					for _, target := range source[1:] {
						out += fmt.Sprintf("ss%s.%s", g.N, target+",")
					}
					out = strings.TrimRight(out, ",") + "}),"
				} else {
					out += "),"
				}

				continue
			} else {

				// relation to states only
				var targets []string
				for _, target := range source {
					targets = append(targets, "ss"+g.N+"."+target)
				}

				out += fmt.Sprintf("\n\t\t%s%s: S{%s},",
					indent, rel[1], strings.Join(targets, ", "))
			}
		}

		// close state
		if nl {
			out += fmt.Sprintf("\n\t%s},\n", indent)
		} else {
			out += "},\n"
		}
	}

	// close states def
	if g.Mach.Is1(ssG.Inherit) {
		out += "})\n"
	} else {
		out += "}\n"
	}

	out += "\n" + p(`
	// EXPORTS AND GROUPS

	var (
		ss%s = am.NewStates(%sStatesDef{})
		sg%s = am.NewStateGroups(%sGroupsDef{`, g.N, g.Name, g.N, g.Name)

	for _, group := range g.Groups {
		if strings.Contains(group, "(") {
			data := strings.Split(strings.TrimRight(group, ")"), "(")
			var names []string

			for _, name := range strings.Split(data[1], ";") {
				names = append(names, fmt.Sprintf("ss%s.%s", g.N, name))
			}
			out += fmt.Sprintf("\n\t\t%s: S{%s},",
				data[0], strings.Join(names, ", "))
		} else {
			out += fmt.Sprintf("\n\t\t%s: S{},", group)
		}
	}

	if g.Mach.Is1(ssG.GroupsLocal) {
		out += "\n\t"
	}
	out += "}"

	if g.Mach.Is1(ssG.InheritConnected) {
		out += ", ss.ConnectedGroups"
	}
	if g.Mach.Is1(ssG.InheritNodeWorker) {
		out += ", ssnode.WorkerGroups"
	}

	out += p(`
		)

			// %sStates contains all the states for the %s machine.
			%sStates = ss%s
			// %sGroups contains all the state groups for the %s machine.
			%sGroups = sg%s
		)
	`, g.Name, g.Name, g.Name, g.N, g.Name, g.Name, g.Name, g.N)

	return out
}

func NewSFGenerator(
	ctx context.Context, param cli.SFParams,
) (*Generator, error) {
	g := &Generator{}
	mach, err := am.NewCommon(ctx, "gen", states.GeneratorStruct, ssG.Names(),
		g, nil, nil)
	if err != nil {
		return nil, err
	}

	g.Mach = mach
	g.parseParams(param)

	return g, nil
}

func GenUtilsFile() string {
	return dedent.Dedent(`
		package states
		
		import am "github.com/pancsta/asyncmachine-go/pkg/machine"
		
		// S is a type alias for a list of state names.
		type S = am.S

		// State is a type alias for a state definition. See [am.State].
		type State = am.State
		
		// SAdd is a func alias for merging lists of states.
		var SAdd = am.SAdd
		
		// StateAdd is a func alias for adding to an existing state definition.
		var StateAdd = am.StateAdd
		
		// StateSet is a func alias for replacing parts of an existing state
		// definition.
		var StateSet = am.StateSet
		
		// StructMerge is a func alias for extending an existing state structure.
		var StructMerge = am.StructMerge

		// Exception is a type alias for the exception state.
		var Exception = am.Exception
	`)
}

func capitalizeFirstLetter(s string) string {
	if len(s) == 0 {
		return s
	}
	return string(unicode.ToUpper(rune(s[0]))) + s[1:]
}

func p(txt string, args ...any) string {
	return fmt.Sprintf(dedent.Dedent(strings.Trim(txt, "\n")), args...)
}
