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
// It allows to avoid conflicts with other tests, using predefined addresses,
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
	l, err := net.Listen("tcp", ":0")
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
		ID: "t-" + t.Name()})
	err := mach.VerifyStates(ss.Names)
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}

	return mach
}

// NewRelsRpcWorker creates a new RPC worker machine with basic relations
// between ABCD states.
func NewRelsRpcWorker(t *testing.T, initialState am.S) *am.Machine {

	// inherit from RPC worker

	// TODO define these in /states using v2 as RelWorkerStruct and
	//  RelWorkerStates, inheriting frm RelStructDef etc
	ssStruct := am.StructMerge(ssrpc.WorkerStruct, ss.States)
	ssNames := am.SAdd(ss.Names, ssrpc.WorkerStates.Names())

	// machine init
	mach := am.New(context.Background(), ssStruct, &am.Opts{
		ID: "t-" + t.Name()})
	err := mach.VerifyStates(ssNames)
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}

	return mach
}

// inherit from RPC worker

var (
	RelsNodeWorkerStruct = am.StructMerge(ssnode.WorkerStruct, ss.States)
	RelsNodeWorkerStates = am.SAdd(ss.Names, ssnode.WorkerStates.Names())
)

// NewRelsNodeWorker creates a new Node worker machine with basic relations
// between ABCD states.
func NewRelsNodeWorker(t *testing.T, initialState am.S) *am.Machine {

	// machine init
	mach := am.New(context.Background(), RelsNodeWorkerStruct, &am.Opts{
		ID: "t-" + t.Name()})
	err := mach.VerifyStates(RelsNodeWorkerStates)
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}

	return mach
}

// NewNoRels creates a new machine without relations between states.
func NewNoRels(t *testing.T, initialState am.S) *am.Machine {
	// machine init
	mach := am.New(context.Background(), am.Struct{
		ss.A: {},
		ss.B: {},
		ss.C: {},
		ss.D: {},
	}, &am.Opts{ID: "t-" + t.Name()})
	err := mach.VerifyStates(ss.Names)
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}

	return mach
}

// NewNoRelsRpcWorker creates a new RPC worker without relations between states.
func NewNoRelsRpcWorker(t *testing.T, initialState am.S) *am.Machine {

	// inherit from RPC worker

	ssStruct := am.StructMerge(ssrpc.WorkerStruct, am.Struct{
		ss.A: {},
		ss.B: {},
		ss.C: {},
		ss.D: {},
	})
	ssNames := am.SAdd(ss.Names, ssrpc.WorkerStates.Names())

	// machine init
	mach := am.New(context.Background(), ssStruct, &am.Opts{ID: "t-" + t.Name()})
	err := mach.VerifyStates(ssNames)
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}

	return mach
}

// NewCustom creates a new machine with custom states.
func NewCustom(t *testing.T, states am.Struct) *am.Machine {
	mach := am.New(context.Background(), states, &am.Opts{
		ID: "t-" + t.Name()})
	err := mach.VerifyStates(append(maps.Keys(states), am.Exception))
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}

	return mach
}

// NewCustomRpcWorker creates a new worker with custom states.
func NewCustomRpcWorker(t *testing.T, states am.Struct) *am.Machine {

	// inherit from RPC worker

	ssStruct := am.StructMerge(ssrpc.WorkerStruct, states)
	ssNames := am.SAdd(maps.Keys(states), ssrpc.WorkerStates.Names())

	mach := am.New(context.Background(), ssStruct, &am.Opts{
		ID: "t-" + t.Name()})
	err := mach.VerifyStates(ssNames)
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv(am.EnvAmDebug) != "" && os.Getenv(EnvAmTestRunner) == "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}

	return mach
}
