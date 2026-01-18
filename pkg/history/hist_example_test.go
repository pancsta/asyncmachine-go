package history_test

import (
	"context"

	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	amss "github.com/pancsta/asyncmachine-go/pkg/states"
	ss "github.com/pancsta/asyncmachine-go/tools/debugger/states"
)

func ExampleNewMemory() {
	ctx := context.Background()

	// configs
	onErr := func(err error) {
		panic(err)
	}
	cfg := amhist.Config{
		TrackedStates: am.S{ss.Start},
	}

	// init machine and history
	mach := am.New(ctx, amss.BasicSchema, &am.Opts{Id: "MyMach1"})
	mem, err := amhist.NewMemory(ctx, nil, mach, cfg, onErr)
	if err != nil {
		panic(err)
	}
	// query etc
	_ = mem.Dispose()
}
