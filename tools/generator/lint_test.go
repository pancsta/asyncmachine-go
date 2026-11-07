package generator

import (
	"strings"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ///// ///// /////

// ///// LINT

// ///// ///// /////

func TestLintSchemaOrder_ReorderAndAdd(t *testing.T) {
	src := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		type MyMachStatesDef struct {
			*am.StatesBase

			Start string
			Ready string
			Foo   string
			Bar   string
		}

		// MyMachSchema represents all relations and properties of MyMachStates.
		var MyMachSchema = am.Schema{
			ssM.Foo: {},
			ssM.Start: {
				Auto: true,
			},
			ssM.Bar: {},
		}

		var (
			ssM = am.NewStates(MyMachStatesDef{})
		)
	`), "\n")

	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		type MyMachStatesDef struct {
			*am.StatesBase

			Start string
			Ready string
			Foo   string
			Bar   string
		}

		// MyMachSchema represents all relations and properties of MyMachStates.
		var MyMachSchema = am.Schema{
			ssM.Start: {
				Auto: true,
			},
			ssM.Ready: {},
			ssM.Foo:   {},
			ssM.Bar:   {},
		}

		var (
			ssM = am.NewStates(MyMachStatesDef{})
		)
	`), "\n")

	res, err := LintSchemaOrder("ss_my_mach.go", []byte(src))
	require.NoError(t, err)

	assert.True(t, res.Changed)
	assert.True(t, res.Reordered)
	assert.Equal(t, []string{"Ready"}, res.Added)
	assert.Equal(t, expected, string(res.Src))
}

func TestLintSchemaOrder_AlreadyOrdered(t *testing.T) {
	src := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		type MyMachStatesDef struct {
			*am.StatesBase

			Start string
			Ready string
		}

		var MyMachSchema = am.Schema{
			ssM.Start: {},
			ssM.Ready: {},
		}

		var (
			ssM = am.NewStates(MyMachStatesDef{})
		)
	`), "\n")

	res, err := LintSchemaOrder("ss_my_mach.go", []byte(src))
	require.NoError(t, err)

	assert.False(t, res.Changed)
	assert.False(t, res.Reordered)
	assert.Empty(t, res.Added)
	assert.Equal(t, src, string(res.Src))
}

func TestLintSchemaOrder_MergeWithComments(t *testing.T) {
	src := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			ssam "github.com/pancsta/asyncmachine-go/pkg/states"
		)

		type FooStatesDef struct {
			*am.StatesBase

			Start           string
			Ready           string
			ClientConnected string
		}

		var FooSchema = ssam.BasicSchema.Merge(
			// inherit from BasicSchema
			ssam.BasicSchema,
			am.Schema{

				// rpc
				ssF.ClientConnected: {Require: S{ssF.Start}},

				// basics
				ssF.Start: {},
				ssF.Ready: {},
			},
		)

		var (
			ssF = am.NewStates(FooStatesDef{})
		)
	`), "\n")

	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
			ssam "github.com/pancsta/asyncmachine-go/pkg/states"
		)

		type FooStatesDef struct {
			*am.StatesBase

			Start           string
			Ready           string
			ClientConnected string
		}

		var FooSchema = ssam.BasicSchema.Merge(
			// inherit from BasicSchema
			ssam.BasicSchema,
			am.Schema{

				// basics
				ssF.Start: {},
				ssF.Ready: {},

				// rpc
				ssF.ClientConnected: {Require: S{ssF.Start}},
			},
		)

		var (
			ssF = am.NewStates(FooStatesDef{})
		)
	`), "\n")

	res, err := LintSchemaOrder("ss_foo.go", []byte(src))
	require.NoError(t, err)

	assert.True(t, res.Changed)
	assert.True(t, res.Reordered)
	assert.Empty(t, res.Added)
	assert.Equal(t, expected, string(res.Src))
}

func TestLintSchemaOrder_InheritedFirst(t *testing.T) {
	src := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		type ServerStatesDef struct {
			*am.StatesBase

			Ready string
			Foo   string
		}

		var ServerSchema = am.Schema{
			ssS.Foo:        {},
			ssS.Ready:      {},
			ssS.ErrNetwork: {Require: S{ssS.Exception}},
		}

		var (
			ssS = am.NewStates(ServerStatesDef{})
		)
	`), "\n")

	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		type ServerStatesDef struct {
			*am.StatesBase

			Ready string
			Foo   string
		}

		var ServerSchema = am.Schema{
			ssS.ErrNetwork: {Require: S{ssS.Exception}},
			ssS.Ready:      {},
			ssS.Foo:        {},
		}

		var (
			ssS = am.NewStates(ServerStatesDef{})
		)
	`), "\n")

	res, err := LintSchemaOrder("ss_server.go", []byte(src))
	require.NoError(t, err)

	assert.True(t, res.Changed)
	assert.True(t, res.Reordered)
	assert.Equal(t, expected, string(res.Src))
}

func TestLintSchemaOrder_EmptySchema(t *testing.T) {
	src := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		type MyMachStatesDef struct {
			*am.StatesBase

			Start string
			Ready string
		}

		var MyMachSchema = am.Schema{}

		var (
			ssM = am.NewStates(MyMachStatesDef{})
		)
	`), "\n")

	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		type MyMachStatesDef struct {
			*am.StatesBase

			Start string
			Ready string
		}

		var MyMachSchema = am.Schema{
			ssM.Start: {},
			ssM.Ready: {},
		}

		var (
			ssM = am.NewStates(MyMachStatesDef{})
		)
	`), "\n")

	res, err := LintSchemaOrder("ss_my_mach.go", []byte(src))
	require.NoError(t, err)

	assert.True(t, res.Changed)
	assert.False(t, res.Reordered)
	assert.Equal(t, []string{"Start", "Ready"}, res.Added)
	assert.Equal(t, expected, string(res.Src))
}

func TestLintSchemaOrder_NoStatesDef(t *testing.T) {
	// bare schemas (plain state names) must be left untouched
	src := strings.TrimLeft(dedent.Dedent(`
		package states

		import am "github.com/pancsta/asyncmachine-go/pkg/machine"

		var States = am.Schema{
			Init: {},
			Watching: {
				Add: S{Init},
			},
		}
	`), "\n")

	res, err := LintSchemaOrder("ss_watcher.go", []byte(src))
	require.NoError(t, err)

	assert.False(t, res.Changed)
	assert.Equal(t, src, string(res.Src))
}

func TestLintSchemaOrder_SingleLine(t *testing.T) {
	src := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		type MyMachStatesDef struct {
			*am.StatesBase

			Start string
			Ready string
			Foo   string
		}

		var MyMachSchema = am.Schema{ssM.Foo: {}, ssM.Start: {}}

		var (
			ssM = am.NewStates(MyMachStatesDef{})
		)
	`), "\n")

	expected := strings.TrimLeft(dedent.Dedent(`
		package states

		import (
			am "github.com/pancsta/asyncmachine-go/pkg/machine"
		)

		type MyMachStatesDef struct {
			*am.StatesBase

			Start string
			Ready string
			Foo   string
		}

		var MyMachSchema = am.Schema{ssM.Start: {}, ssM.Ready: {}, ssM.Foo: {}}

		var (
			ssM = am.NewStates(MyMachStatesDef{})
		)
	`), "\n")

	res, err := LintSchemaOrder("ss_my_mach.go", []byte(src))
	require.NoError(t, err)

	assert.True(t, res.Changed)
	assert.True(t, res.Reordered)
	assert.Equal(t, []string{"Ready"}, res.Added)
	assert.Equal(t, expected, string(res.Src))
}

func TestLintSchemaOrder_InvalidSource(t *testing.T) {
	_, err := LintSchemaOrder(
		"ss_broken.go", []byte("package states\n\nvar X = {"))
	assert.Error(t, err)
}
