package utils

import (
	"context"
	"math/rand"
	"net"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/exp/maps"

	ss "github.com/pancsta/asyncmachine-go/internal/testing/states"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssnode "github.com/pancsta/asyncmachine-go/pkg/node/states"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

const EnvAmTestRunner = "AM_TEST_RUNNER"

var (
	ports    []int
	ConnInit sync.Mutex
)

func RandPort(min, max int) string {

	p := rand.Intn(max-min+1) + min
	for slices.Contains(ports, p) {
		p = rand.Intn(max-min+1) + min
	}
	// remember used ports, but dont reuse them
	ports = append(ports, p)

	return strconv.Itoa(p)
}

// RandListener creates a new listener on an open port between 40000 and 50000.
// It allows avoiding conflicts with other tests, using predefined addresses,
// unlike using port 0.
func RandListener(host string) net.Listener {

	ConnInit.Lock()
	defer ConnInit.Unlock()

	// var (
	// 	err error
	// 	l   net.Listener
	// )
	//
	// // try 10 times
	// for i := 0; i < 10; i++ {

	// addr := host + ":" + RandPort(40000, 50000)
	l, err := net.Listen("tcp4", host+":0")
	if err == nil {
		return l
	}
	// }

	panic("could not create listener on " + host)
}

// NewRels creates a new machine with basic relations between ABCD states.
func NewRels(t *testing.T, initialState am.S) *am.Machine {
	// machine init
	mach := am.New(context.Background(), ss.States, &am.Opts{
		Id: "t-" + t.Name()})
	err := mach.VerifyStates(ss.Names)
	if err != nil {
		t.Fatal(err)
	}

	mach.SemLogger().SetLevel(am.EnvLogLevel(os.Getenv(am.EnvAmLog)))
	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}
	mach.SemLogger().SetArgsMapper(am.NewLogArgsMapper(50, am.LogArgs))

	return mach
}

// NewRelsNetSrc creates a new network source machine with basic relations
// between ABCD states.
func NewRelsNetSrc(t *testing.T, initialState am.S) *am.Machine {

	// inherit from RPC worker

	// TODO define these in /states using v2 as RelWorkerStruct and
	//  RelWorkerStates, inheriting frm RelStructDef etc
	schema := am.SchemaMerge(ssrpc.StateSourceSchema, ss.States)
	names := am.SAdd(ss.Names, ssrpc.StateSourceStates.Names())

	// machine init
	mach := am.New(context.Background(), schema, &am.Opts{
		Id: "ns-" + t.Name()})
	err := mach.VerifyStates(names)
	if err != nil {
		t.Fatal(err)
	}

	mach.SemLogger().SetLevel(am.EnvLogLevel(os.Getenv(am.EnvAmLog)))
	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}

	return mach
}

// inherit from RPC worker

var (
	RelsNodeWorkerSchema = am.SchemaMerge(ssnode.WorkerSchema, ss.States)
	RelsNodeWorkerStates = am.SAdd(ss.Names, ssnode.WorkerStates.Names())
)

// NewRelsNodeWorker creates a new Node worker machine with basic relations
// between ABCD states.
func NewRelsNodeWorker(t *testing.T, initialState am.S) *am.Machine {

	// machine init
	mach := am.New(context.Background(), RelsNodeWorkerSchema, &am.Opts{
		Id: "t-" + t.Name()})
	err := mach.VerifyStates(RelsNodeWorkerStates)
	if err != nil {
		t.Fatal(err)
	}

	mach.SemLogger().SetLevel(am.EnvLogLevel(os.Getenv(am.EnvAmLog)))
	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}
	mach.SemLogger().SetArgsMapper(am.NewLogArgsMapper(50, am.LogArgs))

	return mach
}

// NewNoRels creates a new machine without relations between states.
func NewNoRels(t *testing.T, initialState am.S, suffix string) *am.Machine {
	id := t.Name() + suffix

	// machine init
	mach := am.New(context.Background(), am.Schema{
		ss.A: {},
		ss.B: {},
		ss.C: {},
		ss.D: {},
	}, &am.Opts{Id: "t-" + id})
	err := mach.VerifyStates(ss.Names)
	if err != nil {
		t.Fatal(err)
	}

	mach.SemLogger().SetLevel(am.EnvLogLevel(os.Getenv(am.EnvAmLog)))
	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}
	mach.SemLogger().SetArgsMapper(am.NewLogArgsMapper(50, am.LogArgs))

	return mach
}

// NewNoRelsNetSrc creates a new RPC worker without relations between states.
func NewNoRelsNetSrc(
	t *testing.T, initialState am.S, suffix string,
) *am.Machine {

	id := "ns-" + t.Name() + suffix

	// inherit from RPC worker
	schema := am.SchemaMerge(ssrpc.StateSourceSchema, am.Schema{
		ss.A: {},
		ss.B: {},
		ss.C: {},
		ss.D: {},
	})
	names := am.SAdd(ss.Names, ssrpc.StateSourceStates.Names())

	// machine init
	mach := am.New(context.Background(), schema, &am.Opts{Id: id})
	err := mach.VerifyStates(names)
	if err != nil {
		t.Fatal(err)
	}

	mach.SemLogger().SetLevel(am.EnvLogLevel(os.Getenv(am.EnvAmLog)))
	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}
	mach.SemLogger().SetArgsMapper(am.NewLogArgsMapper(50, am.LogArgs))

	return mach
}

// NewNoRelsNetSrcSchema creates a new RPC worker without relations between
// states and applies a schema overlay.
func NewNoRelsNetSrcSchema(
	t *testing.T, initialState am.S, overlay am.Schema) *am.Machine {

	// inherit from RPC worker
	schema := am.SchemaMerge(ssrpc.StateSourceSchema, am.SchemaMerge(am.Schema{
		ss.A: {},
		ss.B: {},
		ss.C: {},
		ss.D: {},
	}, overlay))
	names := am.SAdd(ss.Names, ssrpc.StateSourceStates.Names())

	// machine init
	mach := am.New(context.Background(), schema, &am.Opts{Id: "t-" + t.Name()})
	err := mach.VerifyStates(names)
	if err != nil {
		t.Fatal(err)
	}

	mach.SemLogger().SetLevel(am.EnvLogLevel(os.Getenv(am.EnvAmLog)))
	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}
	mach.SemLogger().SetArgsMapper(am.NewLogArgsMapper(50, am.LogArgs))

	return mach
}

// NewCustom creates a new machine with custom states.
func NewCustom(t *testing.T, states am.Schema) *am.Machine {
	mach := am.New(context.Background(), states, &am.Opts{
		Id: "t-" + t.Name()})
	err := mach.VerifyStates(append(maps.Keys(states), am.StateException))
	if err != nil {
		t.Fatal(err)
	}

	mach.SemLogger().SetLevel(am.EnvLogLevel(os.Getenv(am.EnvAmLog)))
	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.HandlerTimeout = 2 * time.Minute
	}
	mach.SemLogger().SetArgsMapper(am.NewLogArgsMapper(50, am.LogArgs))

	return mach
}

// NewCustomNetSrc creates a new worker with custom states.
func NewCustomNetSrc(t *testing.T, states am.Schema) *am.Machine {

	// inherit from RPC worker

	schema := am.SchemaMerge(ssrpc.StateSourceSchema, states)
	names := am.SAdd(maps.Keys(states), ssrpc.StateSourceStates.Names())

	mach := am.New(context.Background(), schema, &am.Opts{
		Id: "t-" + t.Name()})
	err := mach.VerifyStates(names)
	if err != nil {
		t.Fatal(err)
	}

	mach.SemLogger().SetLevel(am.EnvLogLevel(os.Getenv(am.EnvAmLog)))
	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.HandlerTimeout = 2 * time.Minute
	}
	mach.SemLogger().SetArgsMapper(am.NewLogArgsMapper(50, am.LogArgs))

	return mach
}
