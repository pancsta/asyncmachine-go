//go:build integration

package new

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

// ///// ///// /////

// ///// TEST STATES

// ///// ///// /////

type fullMachStatesDef struct {
	*am.StatesBase

	Foo string
	Bar string

	// inherit from BasicStatesDef
	*ssam.BasicStatesDef
	// inherit from ConnectedStatesDef
	*ssam.ConnectedStatesDef
	// inherit from DisposedStatesDef
	*ssam.DisposedStatesDef
	// inherit from StateSourceStatesDef (for REPL)
	*ssrpc.StateSourceStatesDef
}

type fullMachGroupsDef struct {
	*ssam.ConnectedGroupsDef

	Work S
}

var (
	ssFM = am.NewStates(fullMachStatesDef{})
	sgFM = am.NewStateGroups(fullMachGroupsDef{
		Work: S{ssFM.Foo, ssFM.Bar},
	}, ssam.ConnectedGroups)

	fullMachSchema = ssam.BasicSchema.Merge(
		ssam.ConnectedSchema,
		ssam.DisposedSchema,
		ssrpc.StateSourceSchema,
		am.Schema{
			ssFM.Foo: {},
			ssFM.Bar: {},
		},
	)
)

// ///// ///// /////

// ///// TEST HANDLERS

// ///// ///// /////

type fullMachHandlers struct {
	*am.ExceptionHandler
	// DisposedHandlers is initialized by FullMach.
	*ssam.DisposedHandlers

	fooCalls atomic.Int32
}

func (h *fullMachHandlers) FooState(_ *am.Event) {
	h.fooCalls.Add(1)
}

// ///// ///// /////

// ///// TEST TRACER

// ///// ///// /////

type fullMachTracer struct {
	*am.TracerNoOp

	inited atomic.Bool
}

func (t *fullMachTracer) TracerId() string {
	return "fullmach-test"
}

func (t *fullMachTracer) MachineInit(_ am.Api) context.Context {
	t.inited.Store(true)

	return nil
}

// ///// ///// /////

// ///// TESTS

// ///// ///// /////

func TestFullMach(t *testing.T) {
	ctx := t.Context()
	// keep the env-gated REPL path a no-op
	t.Setenv(arpc.EnvAmReplAddr, "")

	id := "full-mach"
	handlers := &fullMachHandlers{
		ExceptionHandler: &am.ExceptionHandler{},
	}
	tracer := &fullMachTracer{
		TracerNoOp: &am.TracerNoOp{Id: "fullmach-test"},
	}

	ret, err := FullMach(FullMachParams[fullMachStatesDef, fullMachGroupsDef]{
		Ctx:         ctx,
		Id:          id,
		Schema:      fullMachSchema,
		States:      ssFM,
		Groups:      sgFM,
		Handlers:    handlers,
		MachineOpts: &am.Opts{Tags: []string{"fullmach"}},
		Rpc:         true,
		RpcAddr:     "127.0.0.1:0",
		Repl:        true,
		ReplOpts:    &arpc.ReplOpts{AddrDir: t.TempDir()},
		History:     true,
		HistoryOpts: amhist.BaseConfig{
			TrackedStates: S{ssFM.Foo, ssFM.Bar},
		},
		Tracer: tracer,
	})
	require.NoError(t, err)
	require.NotNil(t, ret)
	mach := ret.Mach
	require.NotNil(t, mach)
	t.Cleanup(mach.Dispose)

	// handlers were initialized via reflection
	assert.NotNil(t, handlers.DisposedHandlers)

	// machine opts
	assert.Equal(t, id, mach.Id())
	assert.Contains(t, mach.Tags(), "fullmach")
	assert.True(t, mach.StateNames().HasN(S{
		ssFM.Start, ssFM.Foo, ssFM.Bar,
		ssFM.Disposing, ssFM.Disposed,
		ssrpc.StateSourceStates.ErrOnClient,
	}))

	// groups were injected
	groups, order := mach.Groups()
	require.Contains(t, order, "Work")
	assert.Equal(t,
		[]int{mach.Index1(ssFM.Foo), mach.Index1(ssFM.Bar)},
		groups["Work"])

	// tracers
	assert.True(t, tracer.inited.Load(), "MachineInit was not called")
	assert.True(t, hasTracer(mach, tracer), "custom tracer not bound")
	assert.True(t, hasTracerPrefix(mach, "history"), "history tracer not bound")

	// rpc mux
	require.NotNil(t, ret.Rpc)
	assert.Same(t, mach, ret.Rpc.Source)
	ret.Rpc.Start(nil)
	muxStopped := false
	t.Cleanup(func() {
		if !muxStopped {
			ret.Rpc.Stop(nil, true)
		}
	})
	amhelpt.WaitForAll(t, "mux ready", ctx, 5*time.Second,
		ret.Rpc.Mach.When1(ssrpc.MuxStates.Ready, ctx))
	require.NotEmpty(t, ret.Rpc.Addr)

	// rpc client
	client, err := arpc.NewClient(
		ctx, ret.Rpc.Addr, "full-mach-client", fullMachSchema,
		&arpc.ClientOpts{SyncMutations: true})
	require.NoError(t, err)
	client.Start(nil)
	clientStopped := false
	t.Cleanup(func() {
		if !clientStopped {
			client.Stop(context.Background(), nil, true)
		}
	})
	amhelpt.WaitForAll(t, "client ready", ctx, 5*time.Second,
		client.Mach.When1(ssrpc.ClientStates.Ready, ctx))

	// source -> client
	mach.Add1(ssFM.Foo, nil)
	amhelpt.WaitForAll(t, "source->client Foo", ctx, 3*time.Second,
		client.NetMach.When1(ssFM.Foo, ctx))
	assert.Equal(t, int32(1), handlers.fooCalls.Load())

	// client -> source
	client.NetMach.Add1(ssFM.Bar, nil)
	amhelpt.WaitForAll(t, "client->source Bar", ctx, 3*time.Second,
		mach.When1(ssFM.Bar, ctx))

	// disposal
	disposed := make(chan string, 1)
	amhelp.DisposeBind(mach, func(id string, _ context.Context) {
		select {
		case disposed <- id:
		default:
		}
	})
	mach.Add1(ssFM.Disposing, nil)
	amhelpt.WaitForAll(t, "disposed state", ctx, 5*time.Second,
		mach.When1(ssFM.Disposed, ctx))

	select {
	case id := <-disposed:
		assert.Equal(t, mach.Id(), id)
	case <-time.After(3 * time.Second):
		t.Fatal("disposal handler not called")
	}

	amhelpt.WaitForAll(t, "machine disposed", ctx, 5*time.Second,
		mach.WhenDisposed())

	// stop rpc after disposal
	client.Stop(ctx, nil, true)
	clientStopped = true
	ret.Rpc.Stop(nil, true)
	muxStopped = true
}

func TestFullMachValidation(t *testing.T) {
	params := FullMachParams[fullMachStatesDef, fullMachGroupsDef]{
		Ctx:      t.Context(),
		Id:       "full-mach-invalid",
		States:   ssFM,
		Groups:   sgFM,
		Handlers: &fullMachHandlers{},
	}

	// no disposed states
	params.Schema = ssam.BasicSchema
	_, err := FullMach(params)
	require.ErrorContains(t, err, "disposed states")

	// no basic states
	params.Schema = ssam.DisposedSchema
	_, err = FullMach(params)
	require.ErrorContains(t, err, "basic states")
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func hasTracer(mach *am.Machine, tracer am.Tracer) bool {
	for _, tr := range mach.Tracers() {
		if tr == tracer {
			return true
		}
	}

	return false
}

func hasTracerPrefix(mach *am.Machine, prefix string) bool {
	for _, tr := range mach.Tracers() {
		if strings.HasPrefix(tr.TracerId(), prefix) {
			return true
		}
	}

	return false
}
