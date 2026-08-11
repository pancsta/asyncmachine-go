package yaegi

import (
	"fmt"
	"io"
	"reflect"
	"testing/fstest"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"

	amsym "github.com/pancsta/asyncmachine-go/pkg/integrations/yaegi/symbols"
	yhost "github.com/pancsta/asyncmachine-go/pkg/integrations/yaegi/testdata/vfs/_pkg/src/host"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// Exec executes the given code in a yaegi VM. The [host] is passed as `host.H`,
// and [symbols] allow for custom imports. IO streams are optional and default
// to STD.
func Exec[G any](
	mach am.Api, code fstest.MapFS, host G, symbols interp.Exports,
	ioOut, ioErr io.Writer,
) (*yhost.Ret, error) {
	// TODO handler errs

	opts := interp.Options{
		SourcecodeFilesystem: code,
		GoPath:               "./_pkg",
	}
	if ioOut != nil {
		opts.Stdout = ioOut
	}
	if ioErr != nil {
		opts.Stderr = ioErr
	}

	// init base VM
	vm := interp.New(opts)
	if err := vm.Use(stdlib.Symbols); err != nil {
		return nil, err
	}

	// load local symbols
	if err := vm.Use(amsym.Symbols); err != nil {
		return nil, err
	}

	// load user symbols
	if len(symbols) > 0 {
		if err := vm.Use(symbols); err != nil {
			return nil, err
		}
	}
	err := vm.Use(interp.Exports{
		"host/host": map[string]reflect.Value{
			"H":   reflect.ValueOf(host),
			"Ret": reflect.ValueOf((*yhost.Ret)(nil)),
		},
	})
	if err != nil {
		return nil, err
	}

	_, err = vm.EvalPath(`interpreted.go`)
	if err != nil {
		return nil, err
	}
	ret, err := vm.Eval("vfs.Run()")
	if err != nil {
		return nil, err
	}

	result, ok := ret.Interface().(*yhost.Ret)
	if !ok {
		err = fmt.Errorf("expected *host.Ret, got %T", ret.Interface())
	}

	return result, err
}
