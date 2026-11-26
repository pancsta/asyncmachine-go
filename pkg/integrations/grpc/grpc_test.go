package grpc

import (
	"context"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	testutils "github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

func TestListAndGet(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, client := newSrv(t, mach)
	ctx := context.Background()

	resp, err := client.ListMachines(ctx, &ListMachinesReq{})
	require.NoError(t, err)
	require.Len(t, resp.Machines, 1)
	assert.Equal(t, mach.Id(), resp.Machines[0].Id)

	mach.Add1("C", nil)
	ser, err := client.GetMachine(ctx, &GetMachineReq{Id: mach.Id()})
	require.NoError(t, err)
	assert.Equal(t, mach.Id(), ser.Id)
	idx := slices.Index(ser.StateNames, "C")
	require.NotEqual(t, -1, idx)
	assert.True(t, am.IsActiveTick(ser.Time[idx]))
}

func TestFilter(t *testing.T) {
	mach1 := testutils.NewRels(t, nil)
	mach2 := testutils.NewNoRels(t, nil, "-2")
	_, client := newSrv(t, mach1, mach2)
	ctx := context.Background()
	mach1.Add1("C", nil)

	all, err := client.ListMachines(ctx, &ListMachinesReq{})
	require.NoError(t, err)
	assert.Len(t, all.Machines, 2)

	active, err := client.ListMachines(ctx,
		&ListMachinesReq{Active: []string{"C"}})
	require.NoError(t, err)
	require.Len(t, active.Machines, 1)
	assert.Equal(t, mach1.Id(), active.Machines[0].Id)

	_, err = client.ListMachines(ctx, &ListMachinesReq{IdRegexp: "["})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreate(t *testing.T) {
	srv, client := newSrv(t)
	ctx := context.Background()
	schema, err := structpb.NewStruct(map[string]any{
		"Foo": map[string]any{"require": []any{"Bar"}},
		"Bar": map[string]any{},
	})
	require.NoError(t, err)

	ser, err := client.CreateMachine(ctx, &CreateMachineReq{
		Serialized: &Serialized{Id: "created", Tags: []string{"x"}},
		Schema:     schema,
	})
	require.NoError(t, err)
	assert.Equal(t, "created", ser.Id)
	assert.ElementsMatch(t, []string{"Bar", "Exception", "Foo"}, ser.StateNames)
	assert.Equal(t, []string{"x"}, ser.Tags)

	mach := srv.mach("created")
	require.NotNil(t, mach)
	mach.Add(am.S{"Bar", "Foo"}, nil)
	assert.True(t, mach.Is1("Foo"))

	_, err = client.CreateMachine(ctx, &CreateMachineReq{
		Serialized: &Serialized{Id: "created"},
		Schema:     schema,
	})
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestCreateRestore(t *testing.T) {
	srv, client := newSrv(t)
	schema, err := structpb.NewStruct(map[string]any{
		"Foo": map[string]any{"require": []any{"Bar"}},
		"Bar": map[string]any{},
	})
	require.NoError(t, err)

	ser, err := client.CreateMachine(context.Background(), &CreateMachineReq{
		Serialized: &Serialized{
			Id:          "restored",
			StateNames:  []string{"Bar", "Exception", "Foo"},
			Time:        []uint64{1, 0, 0},
			MachineTick: 5,
		},
		Schema: schema,
	})
	require.NoError(t, err)
	assert.Equal(t, "restored", ser.Id)

	mach := srv.mach("restored")
	require.NotNil(t, mach)
	assert.True(t, mach.Is1("Bar"))
	assert.False(t, mach.Is1("Foo"))
	assert.Equal(t, uint32(6), mach.MachineTick())
}

func TestMutate(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, client := newSrv(t, mach)
	ctx := context.Background()

	resp, err := client.Mutate(ctx, &MutateReq{Id: mach.Id(), Add: []string{"C"}})
	require.NoError(t, err)
	assert.Equal(t, int64(am.Executed), resp.Result)
	assert.True(t, mach.Is1("C"))
}

func TestWait(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, client := newSrv(t, mach)

	type result struct {
		resp *WaitResp
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := client.Wait(context.Background(),
			&WaitReq{Id: mach.Id(), States: []string{"D"}})
		ch <- result{resp: resp, err: err}
	}()

	mach.Add1("D", nil)

	out := <-ch
	require.NoError(t, out.err)
	assert.Equal(t, mach.Id(), out.resp.Id)
	assert.Equal(t, []string{"D"}, out.resp.States)
}

func TestDispose(t *testing.T) {
	mach := testutils.NewRels(t, nil)
	_, client := newSrv(t, mach)
	ctx := context.Background()

	resp, err := client.Dispose(ctx, &DisposeReq{Id: mach.Id()})
	require.NoError(t, err)
	assert.True(t, resp.Disposed)
	assert.Eventually(t, mach.IsDisposed, time.Second, 10*time.Millisecond)

	_, err = client.Dispose(ctx, &DisposeReq{Id: mach.Id()})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestErrors(t *testing.T) {
	_, client := newSrv(t)
	ctx := context.Background()

	_, err := client.GetMachine(ctx, &GetMachineReq{Id: "nope"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = client.CreateMachine(ctx, &CreateMachineReq{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func newSrv(
	t *testing.T, machs ...am.Api,
) (*Server, AsyncmachineServiceClient) {
	t.Helper()

	if len(machs) == 0 {
		machs = []am.Api{testutils.NewRels(t, nil)}
	}

	srv := New(context.Background(), amhelp.MachGroup(machs))
	ln, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	go func() {
		_ = srv.Serve(ln)
	}()

	conn, err := ggrpc.NewClient(ln.Addr().String(),
		ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
		_ = srv.Close()
		for _, mach := range srv.machs {
			if !mach.IsDisposed() {
				mach.Dispose()
			}
			<-mach.WhenDisposed()
		}
	})

	return srv, NewAsyncmachineServiceClient(conn)
}
