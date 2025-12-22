package utils

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/process"
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
	mach.SemLogger().SetArgsMapper(am.NewArgsMapper(am.LogArgs, 50))

	return mach
}

// NewRelsNetSrc creates a new network source machine with basic relations
// between ABCD states.
func NewRelsNetSrc(t *testing.T, initialState am.S) *am.Machine {

	// inherit from RPC worker

	// TODO define these in /states using v2 as RelWorkerStruct and
	//  RelWorkerStates, inheriting frm RelStructDef etc
	schema := am.SchemaMerge(ssrpc.NetSourceSchema, ss.States)
	names := am.SAdd(ss.Names, ssrpc.NetSourceStates.Names())

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
	mach.SemLogger().SetArgsMapper(am.NewArgsMapper(am.LogArgs, 50))

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
	mach.SemLogger().SetArgsMapper(am.NewArgsMapper(am.LogArgs, 50))

	return mach
}

// NewNoRelsNetSrc creates a new RPC worker without relations between states.
func NewNoRelsNetSrc(t *testing.T, initialState am.S) *am.Machine {

	// inherit from RPC worker
	schema := am.SchemaMerge(ssrpc.NetSourceSchema, am.Schema{
		ss.A: {},
		ss.B: {},
		ss.C: {},
		ss.D: {},
	})
	names := am.SAdd(ss.Names, ssrpc.NetSourceStates.Names())

	// machine init
	mach := am.New(context.Background(), schema, &am.Opts{Id: "ns-" + t.Name()})
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
	mach.SemLogger().SetArgsMapper(am.NewArgsMapper(am.LogArgs, 50))

	return mach
}

// NewNoRelsNetSrcSchema creates a new RPC worker without relations between
// states and applies a schema overlay.
func NewNoRelsNetSrcSchema(
	t *testing.T, initialState am.S, overlay am.Schema) *am.Machine {

	// inherit from RPC worker
	schema := am.SchemaMerge(ssrpc.NetSourceSchema, am.SchemaMerge(am.Schema{
		ss.A: {},
		ss.B: {},
		ss.C: {},
		ss.D: {},
	}, overlay))
	names := am.SAdd(ss.Names, ssrpc.NetSourceStates.Names())

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
	mach.SemLogger().SetArgsMapper(am.NewArgsMapper(am.LogArgs, 50))

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
	mach.SemLogger().SetArgsMapper(am.NewArgsMapper(am.LogArgs, 50))

	return mach
}

// NewCustomNetSrc creates a new worker with custom states.
func NewCustomNetSrc(t *testing.T, states am.Schema) *am.Machine {

	// inherit from RPC worker

	schema := am.SchemaMerge(ssrpc.NetSourceSchema, states)
	names := am.SAdd(maps.Keys(states), ssrpc.NetSourceStates.Names())

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
	mach.SemLogger().SetArgsMapper(am.NewArgsMapper(am.LogArgs, 50))

	return mach
}

// KillProcessesByName finds and attempts to terminate all processes with the
// given name. It returns a slice of PIDs that were successfully terminated and
// a slice of errors for any processes that could not be terminated.
func KillProcessesByName(
	processName string) (killedPIDs []int32, errs []error) {

	processes, err := process.Processes()
	if err != nil {
		err := fmt.Errorf("failed to get process list: %w", err)
		return nil, []error{err}
	}

	for _, p := range processes {
		name, err := p.Name()
		if err != nil {
			// Skip processes whose names cannot be retrieved (e.g., transient or
			// permission issues)
			continue
		}

		// Use strings.EqualFold for case-insensitive comparison, or `==` for
		// case-sensitive
		if strings.EqualFold(name, processName) { // Use `name == processName` for
			// exact case match
			pid := p.Pid

			// Try graceful termination first (SIGTERM)
			if termErr := p.Terminate(); termErr != nil {
				// If graceful termination fails, try forceful kill (SIGKILL)
				if os.IsPermission(termErr) {
					err := fmt.Errorf("permission denied for PID %d (%s): %w",
						pid, name, termErr)
					errs = append(errs, err)
					continue // Cannot kill this one, move to next
				}

				if killErr := p.Kill(); killErr != nil {
					killErr := fmt.Errorf("failed to force kill PID %d (%s): %w",
						pid, name, killErr)
					errs = append(errs, killErr)
					continue // Failed to kill, move to next
				}
			}

			// Give the process a very brief moment to react to the signal
			time.Sleep(50 * time.Millisecond)

			// Verify if the process is no longer running
			stillRunning, checkErr := p.IsRunning()
			if checkErr == nil && !stillRunning {
				killedPIDs = append(killedPIDs, pid)
			} else if checkErr != nil {
				errs = append(errs, fmt.Errorf(
					"could not verify status of PID %d (%s): %w", pid, name, checkErr))
			} else { // stillRunning is true
				errs = append(errs, fmt.Errorf(
					"PID %d (%s) is still running after termination attempts",
					pid, name))
			}
		}
	}

	return killedPIDs, errs
}
