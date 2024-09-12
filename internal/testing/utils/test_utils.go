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
)

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

func RandListener(host string) net.Listener {
	ConnInit.Lock()
	defer ConnInit.Unlock()

	var (
		err error
		l   net.Listener
	)

	// try 10 times
	for i := 0; i < 10; i++ {

		addr := host + ":" + RandPort(40000, 50000)
		l, err = net.Listen("tcp", addr)
		if err == nil {
			return l
		}
	}

	panic("could not create listener on " + host)
}

// NewRels creates a new machine with basic relations between ss.
func NewRels(t *testing.T, initialState am.S) *am.Machine {
	// machine init
	mach := am.New(context.Background(), ss.States, &am.Opts{
		ID: "t-" + t.Name()})
	err := mach.VerifyStates(ss.Names)
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv("AM_DEBUG") != "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}

	return mach
}

// NewNoRels creates a new machine without relations between ss.
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

	if os.Getenv("AM_DEBUG") != "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}
	if initialState != nil {
		mach.Set(initialState, nil)
	}

	return mach
}

// NewCustomStates creates a new machine with custom ss.
func NewCustomStates(t *testing.T, states am.Struct) *am.Machine {
	mach := am.New(context.Background(), states, &am.Opts{
		ID: "t-" + t.Name()})
	err := mach.VerifyStates(append(maps.Keys(states), am.Exception))
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv("AM_DEBUG") != "" {
		mach.SetLogLevel(am.LogEverything)
		mach.HandlerTimeout = 2 * time.Minute
	}

	return mach
}

// EnableTestDebug sets env vars for debugging tested machines with am-dbg at
// port 9913.
func EnableTestDebug() {
	os.Setenv("AM_DEBUG", "1")
	os.Setenv("AM_DBG_ADDR", "localhost:9913")
	os.Setenv("AM_LOG", "2")
	os.Setenv("AM_RPC_LOG_CLIENT", "1")
	os.Setenv("AM_RPC_LOG_SERVER", "1")
}
