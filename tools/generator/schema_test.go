//nolint:lll
package generator

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"

	"github.com/pancsta/asyncmachine-go/tools/generator/cli"
)

func removeEmptyLines(input string) string {
	re := regexp.MustCompile(`(?m)^\s+$`)
	return re.ReplaceAllString(input, "")
}

func TestAll(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2:multi \
	//				--inherit basic,connected,node/worker,rpc/worker \
	//				--groups Group1,Group2 \
	//				--name MyMach

	params := cli.SFParams{
		Version: false,
		States:  "State1,State2:multi",
		Inherit: "basic,connected,node/worker,rpc/worker",
		Groups:  "Group1,Group2",
		Name:    "MyMach",
	}

	gen, err := NewSFGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states
		
		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			ss "github.com/pancsta/asyncmachine-go/pkg/states"
			ssnode "github.com/pancsta/asyncmachine-go/pkg/node/states"
		)
		
		// MyMachStatesDef contains all the states of the MyMach state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase
		
			State1 string
			State2 string
		
			// inherit from BasicStatesDef
			*ss.BasicStatesDef
			// inherit from ConnectedStatesDef
			*ss.ConnectedStatesDef
			// inherit from node/WorkerStatesDef
			*ssnode.WorkerStatesDef
		}
		
		// MyMachGroupsDef contains all the state groups MyMach state-machine.
		type MyMachGroupsDef struct {
			*ss.ConnectedGroupsDef
			*ssnode.WorkerGroupsDef
			Group1 S
			Group2 S
		}
		
		// MyMachSchema represents all relations and properties of MyMachStates.
		var MyMachSchema = SchemaMerge(
			// inherit from BasicSchema
			ss.BasicSchema,
			// inherit from ConnectedSchema
			ss.ConnectedSchema,
			// inherit from node/WorkerSchema
			ssnode.WorkerSchema,
			am.Schema{
		
				ssM.State1: {},
				ssM.State2: {
					Multi: true,
				},
		})
		
		// EXPORTS AND GROUPS
		
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{
				Group1: S{},
				Group2: S{},
			}, ss.ConnectedGroups, ssnode.WorkerGroups)
		
			// MyMachStates contains all the states for the MyMach state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the MyMach state-machine.
			MyMachGroups = sgM
		)
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestBasicConnected(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2:multi \
	//				--inherit basic,connected \
	//				--groups Group1,Group2 \
	//				--name MyMach

	params := cli.SFParams{
		Version: false,
		States:  "State1,State2:multi",
		Inherit: "basic,connected",
		Groups:  "Group1,Group2",
		Name:    "MyMach",
	}

	gen, err := NewSFGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states
		
		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			ss "github.com/pancsta/asyncmachine-go/pkg/states"
		)
		
		// MyMachStatesDef contains all the states of the MyMach state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase
		
			State1 string
			State2 string
		
			// inherit from BasicStatesDef
			*ss.BasicStatesDef
			// inherit from ConnectedStatesDef
			*ss.ConnectedStatesDef
		}
		
		// MyMachGroupsDef contains all the state groups MyMach state-machine.
		type MyMachGroupsDef struct {
			*ss.ConnectedGroupsDef
			Group1 S
			Group2 S
		}
		
		// MyMachSchema represents all relations and properties of MyMachStates.
		var MyMachSchema = SchemaMerge(
			// inherit from BasicSchema
			ss.BasicSchema,
			// inherit from ConnectedSchema
			ss.ConnectedSchema,
			am.Schema{
		
				ssM.State1: {},
				ssM.State2: {
					Multi: true,
				},
		})
		
		// EXPORTS AND GROUPS
		
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{
				Group1: S{},
				Group2: S{},
			}, ss.ConnectedGroups)
		
			// MyMachStates contains all the states for the MyMach state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the MyMach state-machine.
			MyMachGroups = sgM
		)
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestMinimum(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2
	//				--name MyMach

	params := cli.SFParams{
		Version: false,
		States:  "State1,State2",
		Name:    "MyMach",
	}

	gen, err := NewSFGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states
		
		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)
		
		// MyMachStatesDef contains all the states of the MyMach state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase
		
			State1 string
			State2 string
		
		}
		
		// MyMachGroupsDef contains all the state groups MyMach state-machine.
		type MyMachGroupsDef struct {
		}
		
		// MyMachSchema represents all relations and properties of MyMachStates.
		var MyMachSchema = am.Schema{
		
			ssM.State1: {},
			ssM.State2: {},
		}
		
		// EXPORTS AND GROUPS
		
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{})
		
			// MyMachStates contains all the states for the MyMach state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the MyMach state-machine.
			MyMachGroups = sgM
		)
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestRelations(t *testing.T) {
	ctx := context.Background()
	// --states State1:Require(State2,State3),State2:Add(State1),State3
	//				--name MyMach

	params := cli.SFParams{
		Version: false,
		States:  "State1:auto:Require(State2;State3),State2:Add(State3),State3",
		Name:    "MyMach",
	}

	gen, err := NewSFGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states
		
		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)
		
		// MyMachStatesDef contains all the states of the MyMach state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase
		
			State1 string
			State2 string
			State3 string
		
		}
		
		// MyMachGroupsDef contains all the state groups MyMach state-machine.
		type MyMachGroupsDef struct {
		}
		
		// MyMachSchema represents all relations and properties of MyMachStates.
		var MyMachSchema = am.Schema{
		
			ssM.State1: {
				Auto: true,
				Require: S{ssM.State2, ssM.State3},
			},
			ssM.State2: {
				Add: S{ssM.State3},
			},
			ssM.State3: {},
		}
		
		// EXPORTS AND GROUPS
		
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{})
		
			// MyMachStates contains all the states for the MyMach state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the MyMach state-machine.
			MyMachGroups = sgM
		)
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestGroups(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2
	//				--name MyMach

	params := cli.SFParams{
		Version: false,
		States:  "State1,State2",
		Groups:  "Group1,Group2",
		Name:    "MyMach",
	}

	gen, err := NewSFGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states
		
		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)
		
		// MyMachStatesDef contains all the states of the MyMach state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase
		
			State1 string
			State2 string
	
		}
		
		// MyMachGroupsDef contains all the state groups MyMach state-machine.
		type MyMachGroupsDef struct {
			Group1 S
			Group2 S
		}
		
		// MyMachSchema represents all relations and properties of MyMachStates.
		var MyMachSchema = am.Schema{
	
			ssM.State1: {},
			ssM.State2: {},
		}
		
		// EXPORTS AND GROUPS
		
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{
				Group1: S{},
				Group2: S{},
			})
		
			// MyMachStates contains all the states for the MyMach state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the MyMach state-machine.
			MyMachGroups = sgM
		)
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestGroupsStates(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2
	//				--name MyMach

	params := cli.SFParams{
		Version: false,
		States:  "State1:remove(_Group1),State2",
		Groups:  "Group1(State1;State2),Group2",
		Name:    "MyMach",
	}

	gen, err := NewSFGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states
		
		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)
		
		// MyMachStatesDef contains all the states of the MyMach state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase
		
			State1 string
			State2 string
	
		}
		
		// MyMachGroupsDef contains all the state groups MyMach state-machine.
		type MyMachGroupsDef struct {
			Group1 S
			Group2 S
		}
		
		// MyMachSchema represents all relations and properties of MyMachStates.
		var MyMachSchema = am.Schema{
	
			ssM.State1: {
				Remove: SAdd(sgM.Group1),
			},
			ssM.State2: {},
		}
		
		// EXPORTS AND GROUPS
		
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{
				Group1: S{ssM.State1, ssM.State2},
				Group2: S{},
			})
		
			// MyMachStates contains all the states for the MyMach state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the MyMach state-machine.
			MyMachGroups = sgM
		)
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}
