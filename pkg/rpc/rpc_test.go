package rpc

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/pancsta/asyncmachine-go/pkg/helpers"

	ss "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssCli "github.com/pancsta/asyncmachine-go/pkg/rpc/states/client"
	ssSrv "github.com/pancsta/asyncmachine-go/pkg/rpc/states/server"
)

func init() {
	if os.Getenv("AM_TEST_DEBUG") != "" {
		utils.EnableTestDebug()
	}
}

func TestBasic(t *testing.T) {
	t.Parallel()

	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// init worker
	worker := am.New(ctx, am.Struct{
		"Foo": {},
		"Bar": {Require: am.S{"Foo"}},
	}, &am.Opts{ID: "w-" + t.Name()})
	err := worker.VerifyStates(am.S{"Foo", "Bar", am.Exception})
	if err != nil {
		t.Fatal(err)
	}
	helpers.MachDebugT(t, worker, amDbgAddr, logLvl, true)

	// init server and client
	_, _, s, c := NewTest(t, ctx, worker, nil, nil)

	// test
	c.Worker.Add1("Foo", nil)

	// assert
	assert.True(t, s.Mach.Is1(ssCli.Ready), "Server ready")
	assert.True(t, c.Mach.Is1(ssCli.Ready), "Client ready")

	assert.True(t, s.Mach.Not1(am.Exception), "No server errors")
	assert.True(t, c.Mach.Not1(am.Exception), "No client errors")

	assert.True(t, worker.Is1("Foo"), "Worker state set on the server")
	assert.True(t, c.Worker.Is1("Foo"), "Worker state set on the client")

	c.Mach.Log("OK")
	s.Mach.Log("OK")

	// shut down
	c.Mach.Remove1(ssCli.Start, nil)
	s.Mach.Remove1(ssSrv.Start, nil)
}

func TestTypeSafe(t *testing.T) {
	t.Parallel()

	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := utils.NewRels(t, nil)
	helpers.MachDebugT(t, worker, amDbgAddr, logLvl, true)

	// init server and client
	_, _, s, c := NewTest(t, ctx, worker, nil, nil)

	// test
	states := am.S{ss.A, ss.C}
	c.Worker.Add(states, nil)

	// assert
	assert.True(t, s.Mach.Is1(ssCli.Ready), "Server ready")
	assert.True(t, c.Mach.Is1(ssCli.Ready), "Client ready")

	assert.True(t, s.Mach.Not1(am.Exception), "No server errors")
	assert.True(t, c.Mach.Not1(am.Exception), "No client errors")

	assert.True(t, worker.Is(states), "Worker state set on the server")
	assert.True(t, c.Worker.Is(states),
		"Worker state set on the client")

	c.Mach.Log("OK")
	s.Mach.Log("OK")

	// shut down
	c.Mach.Remove1(ssCli.Start, nil)
	s.Mach.Remove1(ssSrv.Start, nil)
}

func TestWaiting(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	counter, _, s, c := NewTest(t, ctx, nil, end, nil)

	// test
	whenA := make(chan struct{})
	go func() {
		<-c.Worker.When1(ss.A, ctx)
		close(whenA)
	}()
	states := am.S{ss.A, ss.C}
	c.Worker.Add(states, nil)
	<-whenA

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	assert.Equal(t, 0, int(s.CallCount),
		"Server piggybacked clock on resp")
	assert.Equal(t, 3, int(c.CallCount),
		"Client called RemoteHandshake, RemoteHandshakeAck, RemoteAdd")
	bytesCount := <-counter
	assert.LessOrEqual(t, 400, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 500, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(c, s)
}

func TestAddMany(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	counter, _, s, c := NewTest(t, ctx, nil, end, nil)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(ss.D, ctx)
		close(whenD)
	}()
	states := am.S{ss.A, ss.C}
	for i := 0; i < 500; i++ {
		c.Worker.Add(states, nil)
	}
	c.Worker.Add1(ss.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	assert.Equal(t, 0, int(s.CallCount),
		"Server piggybacked clock on resp")
	bytesCount := <-counter
	assert.LessOrEqual(t, 16100, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")
	assert.GreaterOrEqual(t, 16300, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")

	disposeTest(c, s)
}

func TestAddManyNoSync(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	// disable clock pushes
	interval := 0 * time.Hour
	counter, _, s, c := NewTest(t, ctx, nil, end, &interval)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(ss.D, ctx)
		close(whenD)
	}()
	states := am.S{ss.A, ss.C}
	for i := 0; i < 500; i++ {
		c.Worker.AddNS(states, nil)
	}
	c.Worker.Add1NS(ss.D, nil)

	// wait for the network to settle, as Sync arrives before +D, this can be
	// flaky, and it's better to Add1(ss.D, nil), but Sync() is being tested here
	time.Sleep(1000 * time.Millisecond)

	// manual sync, should tick D
	c.Worker.Sync()
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	bytesCount := <-counter
	assert.LessOrEqual(t, 7850, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")
	assert.GreaterOrEqual(t, 8000, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")

	disposeTest(c, s)
}

func TestAddManyInstantClock(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	// disable clock optimization (instant pushes)
	interval := 1 * time.Nanosecond
	counter, _, s, c := NewTest(t, ctx, nil, end, &interval)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(ss.D, ctx)
		close(whenD)
	}()
	states := am.S{ss.A, ss.C}
	for i := 0; i < 500; i++ {
		c.Worker.Add(states, nil)
	}
	c.Worker.Add1(ss.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	bytesCount := <-counter
	assert.LessOrEqual(t, 16100, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 16400, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(c, s)
}

func TestManyStates(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})

	// reuse the worker and add many rand states
	states := am.CloneStates(ss.States)
	names := slices.Clone(ss.Names)
	randAmount := 100
	for i := 0; i < randAmount; i++ {
		n := fmt.Sprintf("State%d", i)
		names = append(names, n)
		states[n] = am.State{}
	}
	worker, err := am.NewCommon(context.Background(), "w-"+t.Name(), states,
		names, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	counter, _, s, c := NewTest(t, ctx, worker, end, nil)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(ss.D, ctx)
		close(whenD)
	}()
	for i := 5; i < randAmount-5; i++ {
		c.Worker.Remove1(names[i-3], nil)
		c.Worker.Add1(names[i], nil)
	}
	c.Worker.Add1(ss.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	assert.Equal(t, 183, int(c.CallCount),
		"Client called handshake (2) and mutations (181)")
	bytesCount := <-counter
	assert.LessOrEqual(t, 7400, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 7650, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(c, s)
}

func TestHighInstantClocks(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})

	// reuse the worker and bump the clocks high
	worker := utils.NewRels(t, nil)
	clock := worker.Clock(nil)
	clock[ss.A] = 1_000_000
	clock[ss.C] = 1_000_000
	am.MockClock(worker, clock)
	// disable clock optimization
	interval := 0 * time.Second
	counter, _, s, c := NewTest(t, ctx, worker, end, &interval)

	// test
	assert.GreaterOrEqual(t, 1_000_000, int(worker.Tick(ss.A)),
		"Bytes transferred (both ways)")
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(ss.D, ctx)
		close(whenD)
	}()
	states := am.S{ss.A, ss.C}
	for i := 0; i < 500; i++ {
		c.Worker.Add(states, nil)
		c.Worker.Remove(states, nil)
	}
	c.Worker.Add1(ss.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	// byte count should be the same as in TestAddManyInstantClock
	bytesCount := <-counter
	assert.LessOrEqual(t, 40600, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 40850, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(c, s)
}

func TestClockPush(t *testing.T) {
	// TODO
	t.Skip("TODO")
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func NewTest(
	t *testing.T, ctx context.Context, worker *am.Machine,
	disposeMeter <-chan struct{}, clockInterval *time.Duration,
) (<-chan int64, *am.Machine, *Server, *Client) {
	// read env
	amDbgAddr := os.Getenv("AM_DBG_ADDR")
	logLvl := am.EnvLogLevel("")

	// bind to an open port
	listener := utils.RandListener("localhost")
	serverAddr := listener.Addr().String()
	if os.Getenv("AM_DEBUG") != "" {
		t.Logf("Server addr: %s", serverAddr)
	}

	// worker init
	if worker == nil {
		worker = utils.NewRels(t, nil)
	}
	helpers.MachDebugT(t, worker, amDbgAddr, logLvl, true)

	connAddr := serverAddr

	// traffic counter init
	var counter chan int64
	if disposeMeter != nil {
		counterListener := utils.RandListener("localhost")
		connAddr = counterListener.Addr().String()
		if os.Getenv("AM_DEBUG") != "" {
			t.Logf("Meter addr: %s", connAddr)
		}
		counter = make(chan int64, 1)

		go TrafficMeter(counterListener, serverAddr, counter, disposeMeter)
		time.Sleep(100 * time.Millisecond)
	}

	// server init
	s, err := NewServer(ctx, serverAddr, t.Name(), worker, nil)
	if err != nil {
		t.Fatal(err)
	}
	// set the test listener to avoid port conflicts
	s.Listener = listener
	helpers.MachDebugT(t, s.Mach, amDbgAddr, logLvl, true)
	if clockInterval != nil {
		s.ClockInterval = *clockInterval
	}
	// let it settle
	time.Sleep(10 * time.Millisecond)

	// client init
	c, err := NewClient(ctx, connAddr, t.Name(), worker.GetStruct(),
		worker.StateNames())
	if err != nil {
		t.Fatal(err)
	}
	helpers.MachDebugT(t, c.Mach, amDbgAddr, logLvl, true)

	// tear down
	t.Cleanup(func() {
		<-s.Mach.WhenDisposed()
		<-c.Mach.WhenDisposed()
		// cool off am-dbg and free the ports
		if amDbgAddr != "" {
			time.Sleep(100 * time.Millisecond)
		}
	})

	// start with a timeout
	timeout := 3 * time.Second
	if os.Getenv("AM_DEBUG") != "" || os.Getenv("AM_TEST") != "" {
		timeout = 100 * time.Second
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// server start
	s.Start()
	select {
	case <-readyCtx.Done():
		err := s.Mach.Err()
		// timeout
		if readyCtx.Err() != nil {
			err = readyCtx.Err()
		}
		t.Fatal(err)
	case <-s.Mach.When1(ssSrv.RpcReady, readyCtx):
	}

	// client ready
	c.Start()
	select {
	case <-c.Mach.WhenErr(readyCtx):
		err := c.Mach.Err()
		// timeout
		if readyCtx.Err() != nil {
			err = readyCtx.Err()
		}
		t.Fatal(err)
	case <-c.Mach.When1(ssCli.Ready, readyCtx):
	}

	// server ready
	select {
	case <-s.Mach.WhenErr(readyCtx):
		err := s.Mach.Err()
		// timeout
		if readyCtx.Err() != nil {
			err = readyCtx.Err()
		}
		t.Fatal(err)
	case <-s.Mach.When1(ssSrv.Ready, readyCtx):
	}

	return counter, worker, s, c
}
