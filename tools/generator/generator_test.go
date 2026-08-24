//nolint:lll
package generator

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pancsta/asyncmachine-go/tools/generator/cli"
)

// ///// ///// /////

// ///// SCHEMA

// ///// ///// /////

func TestSchema_All(t *testing.T) {
	ctx := context.Background()
	params := cli.SchemaParams{
		States: "State1,State2:multi",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Version: false,
			Inherit: []string{"basic", "connected", "disposed", "node/worker", "rpc/statesrc"},
			Groups:  "Group1,Group2",
			Name:    "MyMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			ssnode "github.com/pancsta/asyncmachine-go/pkg/node/states"
			ssam "github.com/pancsta/asyncmachine-go/pkg/states"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string

			// inherit from BasicStatesDef
			*ssam.BasicStatesDef
			// inherit from ConnectedStatesDef
			*ssam.ConnectedStatesDef
			// inherit from DisposedStatesDef
			*ssam.DisposedStatesDef
			// inherit from node/StateSourceStatesDef
			*ssnode.StateSourceStatesDef
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct {
			*ssam.ConnectedGroupsDef
			*ssnode.WorkerGroupsDef
			Group1 S
			Group2 S
		}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{}.Merge(
			// inherit from BasicSchema
			ssam.BasicSchema,
			// inherit from ConnectedSchema
			ssam.ConnectedSchema,
			// inherit from DisposedSchema
			ssam.DisposedSchema,
			// inherit from node/WorkerSchema
			ssnode.WorkerSchema,
			am.Schema{
				ssM.State1: {},
				ssM.State2: {
					Multi: true,
				},
			},
		)

		// EXPORTS AND GROUPS
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{
				Group1: S{},
				Group2: S{},
			}, ssam.ConnectedGroups, ssnode.WorkerGroups)

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_BasicConnected(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2:multi \
	//				--inherit basic,connected \
	//				--groups Group1,Group2 \
	//				--name MyMach

	params := cli.SchemaParams{
		States: "State1,State2:multi",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Version: false,
			Inherit: []string{"basic", "connected"},
			Groups:  "Group1,Group2",
			Name:    "MyMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			ssam "github.com/pancsta/asyncmachine-go/pkg/states"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string

			// inherit from BasicStatesDef
			*ssam.BasicStatesDef
			// inherit from ConnectedStatesDef
			*ssam.ConnectedStatesDef
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct {
			*ssam.ConnectedGroupsDef
			Group1 S
			Group2 S
		}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{}.Merge(
			// inherit from BasicSchema
			ssam.BasicSchema,
			// inherit from ConnectedSchema
			ssam.ConnectedSchema,
			am.Schema{
				ssM.State1: {},
				ssM.State2: {
					Multi: true,
				},
			},
		)

		// EXPORTS AND GROUPS
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{
				Group1: S{},
				Group2: S{},
			}, ssam.ConnectedGroups)

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_Connected(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2:multi \
	//				--inherit basic,connected \
	//				--groups Group1,Group2 \
	//				--name MyMach

	params := cli.SchemaParams{
		States: "State1,State2:multi",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Version: false,
			Inherit: []string{"connected"},
			Groups:  "Group1,Group2",
			Name:    "MyMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			ssam "github.com/pancsta/asyncmachine-go/pkg/states"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string

			// inherit from ConnectedStatesDef
			*ssam.ConnectedStatesDef
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct {
			*ssam.ConnectedGroupsDef
			Group1 S
			Group2 S
		}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{}.Merge(
			// inherit from ConnectedSchema
			ssam.ConnectedSchema,
			am.Schema{
				ssM.State1: {},
				ssM.State2: {
					Multi: true,
				},
			},
		)

		// EXPORTS AND GROUPS
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{
				Group1: S{},
				Group2: S{},
			}, ssam.ConnectedGroups)

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_Minimum(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2
	//				--name MyMach

	params := cli.SchemaParams{
		States: "State1,State2",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Version: false,
			Name:    "MyMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct{}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{
			ssM.State1: {},
			ssM.State2: {},
		}

		// EXPORTS AND GROUPS
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{})

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_Relations(t *testing.T) {
	ctx := context.Background()
	// --states State1:Require(State2,State3),State2:Add(State1),State3
	//				--name MyMach

	params := cli.SchemaParams{
		States: "State1:auto:Require(State2;State3),State2:Add(State3),State3",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Version: false,
			Name:    "MyMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string
			State3 string
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct{}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{
			ssM.State1: {
				Auto:    true,
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

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_Groups(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2
	//				--name MyMach

	params := cli.SchemaParams{
		States: "State1,State2",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Version: false,
			Groups:  "Group1,Group2",
			Name:    "MyMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct {
			Group1 S
			Group2 S
		}

		// MyMachSchema represents all relations and properties of [MyMachStates].
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

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_GroupsStates(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2
	//				--name MyMach

	params := cli.SchemaParams{
		States: "State1:remove(_Group1),State2",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Version: false,
			Groups:  "Group1(State1;State2),Group2",
			Name:    "MyMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct {
			Group1 S
			Group2 S
		}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{
			ssM.State1: {
				Remove: sgM.Group1.Add(),
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

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_Global(t *testing.T) {
	ctx := context.Background()
	// --states State1,State2
	//				--name MyMach

	params := cli.SchemaParams{
		States: "State1,State2",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Version: false,
			Name:    "MyMach",
			Global:  true,
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			. "github.com/pancsta/asyncmachine-go/pkg/states/global"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct{}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{
			ssM.State1: {},
			ssM.State2: {},
		}

		// EXPORTS AND GROUPS
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{})

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_RepeatableStateAndInherit(t *testing.T) {
	ctx := context.Background()

	params := cli.SchemaParams{
		State: []string{"State1", "State2:multi"},
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Inherit: []string{"basic", "connected"},
			Groups:  "Group1,Group2",
			Name:    "MyMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			ssam "github.com/pancsta/asyncmachine-go/pkg/states"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string

			// inherit from BasicStatesDef
			*ssam.BasicStatesDef
			// inherit from ConnectedStatesDef
			*ssam.ConnectedStatesDef
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct {
			*ssam.ConnectedGroupsDef
			Group1 S
			Group2 S
		}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{}.Merge(
			// inherit from BasicSchema
			ssam.BasicSchema,
			// inherit from ConnectedSchema
			ssam.ConnectedSchema,
			am.Schema{
				ssM.State1: {},
				ssM.State2: {
					Multi: true,
				},
			},
		)

		// EXPORTS AND GROUPS
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{
				Group1: S{},
				Group2: S{},
			}, ssam.ConnectedGroups)

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_RepeatableGroup(t *testing.T) {
	ctx := context.Background()

	params := cli.SchemaParams{
		State: []string{"State1", "State2:multi"},
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Inherit: []string{"basic", "connected"},
			Group:   []string{"Group1", "Group2"},
			Name:    "MyMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	if err != nil {
		t.Fatal(err)
	}

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			ssam "github.com/pancsta/asyncmachine-go/pkg/states"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			State1 string
			State2 string

			// inherit from BasicStatesDef
			*ssam.BasicStatesDef
			// inherit from ConnectedStatesDef
			*ssam.ConnectedStatesDef
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct {
			*ssam.ConnectedGroupsDef
			Group1 S
			Group2 S
		}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{}.Merge(
			// inherit from BasicSchema
			ssam.BasicSchema,
			// inherit from ConnectedSchema
			ssam.ConnectedSchema,
			am.Schema{
				ssM.State1: {},
				ssM.State2: {
					Multi: true,
				},
			},
		)

		// EXPORTS AND GROUPS
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{
				Group1: S{},
				Group2: S{},
			}, ssam.ConnectedGroups)

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchema_StateTags(t *testing.T) {
	ctx := context.Background()

	params := cli.SchemaParams{
		States: "Start,BaseDBReady:Remove(BaseDBStarting)#tag1#tag2,CheckingMenuRefs:multi:Require(Start)#args",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Name: "TagMach",
		},
	}

	gen, err := NewSchemaGenerator(ctx, params)
	require.NoError(t, err)

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		// TagMachStatesDef contains all the states of the [TagMach] state-machine.
		type TagMachStatesDef struct {
			*am.StatesBase

			Start            string
			BaseDBReady      string
			CheckingMenuRefs string
		}

		// TagMachGroupsDef contains all the state groups [TagMach] state-machine.
		type TagMachGroupsDef struct{}

		// TagMachSchema represents all relations and properties of [TagMachStates].
		var TagMachSchema = am.Schema{
			ssT.Start: {},
			ssT.BaseDBReady: {
				Remove: S{ssT.BaseDBStarting},
				Tags:   []string{"tag1", "tag2"},
			},
			ssT.CheckingMenuRefs: {
				Multi:   true,
				Require: S{ssT.Start},
				Tags:    []string{"args"},
			},
		}

		// EXPORTS AND GROUPS
		var (
			ssT = am.NewStates(TagMachStatesDef{})
			sgT = am.NewStateGroups(TagMachGroupsDef{})

			// TagMachStates contains all the states for the [TagMach] state-machine.
			TagMachStates = ssT
			// TagMachGroups contains all the state groups for the [TagMach] state-machine.
			TagMachGroups = sgT
		)

		// NewTagMach creates a new [TagMach] state-machine in the most basic form.
		func NewTagMach(ctx context.Context) *am.Machine {
			return am.New(ctx, TagMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

// ///// ///// /////

// ///// YAML

// ///// ///// /////

func TestSchemaYaml(t *testing.T) {
	ctx := context.Background()

	yamlContent := []byte(dedent.Dedent(`
		Start:
		BaseDBReady:
		    remove:
		        - BaseDBStarting
		BaseDBSaving:
		    multi: true
		BaseDBStarting:
		    remove:
		        - BaseDBReady
		CharacterReady:
		    remove:
		        - RestoreCharacter
		        - GenCharacter
		CheckStories:
		    multi: true
		    require:
		        - Start
		CheckingMenuRefs:
		    multi: true
		    require:
		        - Start
		RestoreCharacter:
		GenCharacter:
	`))

	params := cli.SchemaFileParams{
		FileContent: yamlContent,
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Name: "MyMach",
		},
	}

	statesParams, err := SchemaFileToParams(params)
	require.NoError(t, err)

	expectedStates := "Start,BaseDBReady:Remove(BaseDBStarting),BaseDBSaving:multi,BaseDBStarting:Remove(BaseDBReady),CharacterReady:Remove(RestoreCharacter;GenCharacter),CheckStories:multi:Require(Start),CheckingMenuRefs:multi:Require(Start),RestoreCharacter,GenCharacter"
	assert.Equal(t, expectedStates, statesParams.States)
	assert.Equal(t, "MyMach", statesParams.Name)

	gen, err := NewSchemaGenerator(ctx, statesParams)
	require.NoError(t, err)

	generated := gen.Output()
	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			"context"
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		// MyMachStatesDef contains all the states of the [MyMach] state-machine.
		type MyMachStatesDef struct {
			*am.StatesBase

			Start            string
			BaseDBReady      string
			BaseDBSaving     string
			BaseDBStarting   string
			CharacterReady   string
			CheckStories     string
			CheckingMenuRefs string
			RestoreCharacter string
			GenCharacter     string
		}

		// MyMachGroupsDef contains all the state groups [MyMach] state-machine.
		type MyMachGroupsDef struct{}

		// MyMachSchema represents all relations and properties of [MyMachStates].
		var MyMachSchema = am.Schema{
			ssM.Start: {},
			ssM.BaseDBReady: {
				Remove: S{ssM.BaseDBStarting},
			},
			ssM.BaseDBSaving: {
				Multi: true,
			},
			ssM.BaseDBStarting: {
				Remove: S{ssM.BaseDBReady},
			},
			ssM.CharacterReady: {
				Remove: S{ssM.RestoreCharacter, ssM.GenCharacter},
			},
			ssM.CheckStories: {
				Multi:   true,
				Require: S{ssM.Start},
			},
			ssM.CheckingMenuRefs: {
				Multi:   true,
				Require: S{ssM.Start},
			},
			ssM.RestoreCharacter: {},
			ssM.GenCharacter:     {},
		}

		// EXPORTS AND GROUPS
		var (
			ssM = am.NewStates(MyMachStatesDef{})
			sgM = am.NewStateGroups(MyMachGroupsDef{})

			// MyMachStates contains all the states for the [MyMach] state-machine.
			MyMachStates = ssM
			// MyMachGroups contains all the state groups for the [MyMach] state-machine.
			MyMachGroups = sgM
		)

		// NewMyMach creates a new [MyMach] state-machine in the most basic form.
		func NewMyMach(ctx context.Context) *am.Machine {
			return am.New(ctx, MyMachSchema, nil)
		}
	`), "\n")

	assert.Equal(t, expected, removeEmptyLines(generated))
}

func TestSchemaYaml_Mach(t *testing.T) {
	yamlContent := []byte(dedent.Dedent(`
		id: my-serialized-mach
		state_names:
		    - Start
		    - BaseDBReady
		    - BaseDBSaving
		time:
		    - 1
		    - 2
		    - 0
		queue_tick: 25
		machine_tick: 1
	`))

	params := cli.SchemaFileParams{
		FileContent: yamlContent,
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Name: "OverriddenName",
		},
	}

	statesParams, err := SchemaFileToParams(params)
	require.NoError(t, err)

	assert.Equal(t, "Start,BaseDBReady,BaseDBSaving", statesParams.States)
	assert.Equal(t, "OverriddenName", statesParams.Name)

	// Test without explicit name (fallback to serialized ID)
	statesParams2, err := SchemaFileToParams(cli.SchemaFileParams{
		FileContent: yamlContent,
	})
	require.NoError(t, err)
	assert.Equal(t, "my-serialized-mach", statesParams2.Name)
}

func TestSchemaYaml_AllFeatures(t *testing.T) {
	yamlContent := []byte(dedent.Dedent(`
		State1:
		    auto: true
		    multi: true
		    require:
		        - State2
		        - State3
		    add:
		        - State2
		    remove:
		        - State4
		    after:
		        - State2
		State2: {}
		State3:
		State4:
	`))

	params := cli.SchemaFileParams{
		FileContent: yamlContent,
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Name:    "ComplexMach",
			Inherit: []string{"basic", "connected"},
			Groups:  "Group1",
			Global:  true,
		},
	}

	statesParams, err := SchemaFileToParams(params)
	require.NoError(t, err)

	expectedStates := "State1:auto:multi:Require(State2;State3):Add(State2):Remove(State4):After(State2),State2,State3,State4"
	assert.Equal(t, expectedStates, statesParams.States)
	assert.Equal(t, "ComplexMach", statesParams.Name)
	assert.Equal(t, []string{"basic", "connected"}, statesParams.Inherit)
	assert.Equal(t, "Group1", statesParams.Groups)
	assert.True(t, statesParams.Global)

	gen, err := NewSchemaGenerator(context.Background(), statesParams)
	require.NoError(t, err)
	assert.NotEmpty(t, gen.Output())
}

func TestSchemaYaml_ToFile(t *testing.T) {
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "schema.yml")

	yamlContent := dedent.Dedent(`
		Foo:
		    auto: true
		Bar:
		    require:
		        - Foo
	`)
	err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600)
	require.NoError(t, err)

	params := cli.SchemaFileParams{
		File: yamlPath,
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Name: "FileMach",
		},
	}

	statesParams, err := SchemaFileToParams(params)
	require.NoError(t, err)

	assert.Equal(t, "Foo:auto,Bar:Require(Foo)", statesParams.States)
	assert.Equal(t, "FileMach", statesParams.Name)
}

func TestSchemaYaml_Errors(t *testing.T) {
	// missing file
	_, err := SchemaFileToParams(cli.SchemaFileParams{
		File: "non-existent-file-12345.yml",
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Name: "Test",
		},
	})
	assert.Error(t, err)

	// invalid yaml
	_, err = SchemaFileToParams(cli.SchemaFileParams{
		FileContent:        []byte(":::invalid"),
		SchemaParamsCommon: cli.SchemaParamsCommon{Name: "Test"},
	})
	assert.Error(t, err)

	// empty document
	_, err = SchemaFileToParams(cli.SchemaFileParams{
		FileContent:        []byte(""),
		SchemaParamsCommon: cli.SchemaParamsCommon{Name: "Test"},
	})
	assert.Error(t, err)

	// root is not a mapping
	_, err = SchemaFileToParams(cli.SchemaFileParams{
		FileContent:        []byte("- item1\n- item2"),
		SchemaParamsCommon: cli.SchemaParamsCommon{Name: "Test"},
	})
	assert.Error(t, err)
}

func TestSchemaYaml_WithTags(t *testing.T) {
	ctx := context.Background()

	yamlContent := []byte(dedent.Dedent(`
		Start:
		CheckingMenuRefs:
		    multi: true
		    require:
		        - Start
		    tags:
		        - args
		        - custom-tag
	`))

	params := cli.SchemaFileParams{
		FileContent: yamlContent,
		SchemaParamsCommon: cli.SchemaParamsCommon{
			Name: "TagMach",
		},
	}

	statesParams, err := SchemaFileToParams(params)
	require.NoError(t, err)

	assert.Equal(t, "Start,CheckingMenuRefs:multi:Require(Start)#args#custom-tag", statesParams.States)

	gen, err := NewSchemaGenerator(ctx, statesParams)
	require.NoError(t, err)

	generated := gen.Output()
	assert.Contains(t, generated, `Tags:    []string{"args", "custom-tag"}`)
}

// ///// ///// /////

// ///// STARTER KIT

// ///// ///// /////

func TestStarter(t *testing.T) {
	tempDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	yamlPath := filepath.Join(tempDir, "schema.yml")
	yamlContent := "Start:\nBaseDBReady:\n"
	err = os.WriteFile(yamlPath, []byte(yamlContent), 0o600)
	require.NoError(t, err)

	params := &cli.StarterParams{
		File:     yamlPath,
		Name:     "MyMach",
		Handlers: true,
		Args:     true,
	}

	err = GenStarterKit(context.Background(), params)
	require.NoError(t, err)

	machDir := filepath.Join(tempDir, "my_mach")
	assert.True(t, fileExists(filepath.Join(machDir, "states", "ss_my_mach.go")))
	assert.True(t, fileExists(filepath.Join(machDir, "my_mach.go")))
	assert.True(t, fileExists(filepath.Join(machDir, "my_mach_test.go")))
	assert.True(t, fileExists(filepath.Join(machDir, "handlers.go")))

	myMachContent, err := os.ReadFile(filepath.Join(machDir, "my_mach.go"))
	require.NoError(t, err)

	expectedNew := dedent.Dedent(`
		func New(ctx context.Context) (*Handlers, error) {
			// handlers
			handlers := &Handlers{DisposedHandlers: &ssam.DisposedHandlers{}}
			mach, err := am.NewCommon(ctx, "my_mach", states.MyMachSchema, ss.Names(), handlers, nil, nil)
			if err != nil {
				return nil, err
			}
			handlers.Mach = mach

			// telemetry and logging
			mach.SetGroups(states.MyMachGroups, states.MyMachStates)
			// mach.SemLogger().SetLevel(am.LogChanges)
			mach.SemLogger().SetArgsMapper(amhelp.LogArgsMapper)
			amhelp.MachDebugEnv(mach)
			err, _ = arpc.MachReplEnv(mach, &arpc.ReplOpts{
				AddrDir: ".",
				Args:    ArgsRpc,
			})

			return handlers, nil
		}
	`)
	assert.Contains(t, string(myMachContent), strings.TrimSpace(expectedNew))
}

func TestStarter_WithStart(t *testing.T) {
	ctx := context.Background()
	schemaContent, err := os.ReadFile("testdata/schema.yml")
	require.NoError(t, err)

	params := cli.StarterParams{
		FileContent: schemaContent,
		Name:        "Starter",
		Uri:         "github.com/pancsta/asyncmachine-go/tools/generator/testdata/starter",
		Module:      true,
		Handlers:    true,
		Args:        true,
		Path:        "starter-kit",
	}

	files, err := GenStarterFiles(ctx, params)
	require.NoError(t, err)

	// Check files created
	assert.Contains(t, files, "states/ss_starter.go")
	assert.Contains(t, files, "starter.go")
	assert.Contains(t, files, "starter_test.go")
	assert.Contains(t, files, "handlers.go")
	assert.Contains(t, files, "go.mod")

	// Verify starter_test.go tests Start
	assert.Contains(t, files["starter_test.go"], "func TestStart(t *testing.T)")
	assert.Contains(t, files["starter_test.go"], "mach.Add1(ss.Start, nil)")
	assert.Contains(t, files["starter_test.go"], "amhelpt.AssertIs1(t, mach, ss.Start)")
	assert.Contains(t, files["starter_test.go"], "mach.Remove1(ss.Start, nil)")
	assert.Contains(t, files["starter_test.go"], "<-mach.WhenNot1(ss.Start, ctx)")

	// Verify handlers.go contains transition handlers
	assert.Contains(t, files["handlers.go"], "func (h *Handlers) StartBaseDBReady(e *am.Event) bool")
	assert.Contains(t, files["handlers.go"], "func (h *Handlers) StartAny(e *am.Event) bool")
	assert.Contains(t, files["handlers.go"], "func (h *Handlers) AnyStart(e *am.Event) bool")

	// Verify writing to disk
	err = GenStarterKit(ctx, &cli.StarterParams{
		File:     "testdata/schema.yml",
		Name:     "Starter",
		Uri:      "github.com/pancsta/asyncmachine-go/tools/generator/testdata/starter",
		Module:   true,
		Handlers: true,
		Args:     true,
		Path:     t.TempDir(),
		Force:    true,
	})
	require.NoError(t, err)

	// Verify states/ss_starter.go uses --global (dot import)
	assert.Contains(t, files["states/ss_starter.go"], `. "github.com/pancsta/asyncmachine-go/pkg/states/global"`)
}

func TestStarter_WithoutStart(t *testing.T) {
	ctx := context.Background()
	yamlContent := []byte(dedent.Dedent(`
		Init:
		    auto: true
		Running:
		    require:
		        - Init
		Done:
		    require:
		        - Running
	`))

	params := cli.StarterParams{
		FileContent: yamlContent,
		Name:        "Worker",
		Uri:         "example.com/worker",
		Module:      true,
		Handlers:    true,
		Args:        true,
		Inherit:     []string{"connected"},
	}

	files, err := GenStarterFiles(ctx, params)
	require.NoError(t, err)

	// Check files created
	assert.Contains(t, files, "states/ss_worker.go")
	assert.Contains(t, files, "worker.go")
	assert.Contains(t, files, "worker_test.go")
	assert.Contains(t, files, "handlers.go")
	assert.Contains(t, files, "go.mod")

	// Verify worker_test.go tests Init (first state when Start is absent)
	assert.Contains(t, files["worker_test.go"], "func TestInit(t *testing.T)")
	assert.Contains(t, files["worker_test.go"], "mach.Add1(ss.Init, nil)")
	assert.Contains(t, files["worker_test.go"], "amhelpt.AssertIs1(t, mach, ss.Init)")
	assert.Contains(t, files["worker_test.go"], "mach.Remove1(ss.Init, nil)")
	assert.Contains(t, files["worker_test.go"], "<-mach.WhenNot1(ss.Init, ctx)")

	// Verify states/ss_worker.go uses --global
	assert.Contains(t, files["states/ss_worker.go"], `. "github.com/pancsta/asyncmachine-go/pkg/states/global"`)
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func removeEmptyLines(input string) string {
	re := regexp.MustCompile(`(?m)^\s+$`)
	return re.ReplaceAllString(input, "")
}
