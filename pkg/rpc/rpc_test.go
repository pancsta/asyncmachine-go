package rpc

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"

	sst "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

func init() {
	_ = godotenv.Load()

	if os.Getenv(am.EnvAmTestDebug) != "" {
		amhelp.EnableDebugging(true)
	}
}

func TestBasic(t *testing.T) {
	t.Parallel()
	// amhelp.EnableDebugging(true)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// init worker
	ssStruct := am.SchemaMerge(ssrpc.WorkerSchema, am.Schema{
		"Foo": {},
		"Bar": {Require: am.S{"Foo"}},
	})
	ssNames := am.SAdd(ssrpc.WorkerStates.Names(), am.S{"Foo", "Bar"})
	worker := am.New(ctx, ssStruct, &am.Opts{Id: "w-" + t.Name()})
	err := worker.VerifyStates(ssNames)
	if err != nil {
		t.Fatal(err)
	}

	amhelpt.MachDebugEnv(t, worker)

	// init server and client
	_, _, s, c := NewTest(t, ctx, worker, nil, nil, nil, false)

	// test
	c.Worker.Add1("Foo", nil)

	// assert
	assert.True(t, s.Mach.Is1(ssrpc.ServerStates.Ready), "Server ready")
	assert.True(t, c.Mach.Is1(ssrpc.ClientStates.Ready), "Client ready")

	assert.True(t, s.Mach.Not1(am.StateException), "No server errors")
	assert.True(t, c.Mach.Not1(am.StateException), "No client errors")

	assert.True(t, worker.Is1("Foo"), "Worker state set on the server")
	assert.True(t, c.Worker.Is1("Foo"), "Worker state set on the client")

	c.Mach.Log("OK")
	s.Mach.Log("OK")

	// shut down
	c.Mach.Remove1(ssrpc.ClientStates.Start, nil)
	s.Mach.Remove1(ssrpc.ServerStates.Start, nil)
}

func TestTypeSafe(t *testing.T) {
	t.Parallel()

	// read env
	amDbgAddr := os.Getenv(telemetry.EnvAmDbgAddr)
	logLvl := am.EnvLogLevel("")

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := utils.NewRelsRpcWorker(t, nil)
	amhelpt.MachDebug(t, worker, amDbgAddr, logLvl, true)

	// init server and client
	_, _, s, c := NewTest(t, ctx, worker, nil, nil, nil, false)

	// test
	states := am.S{sst.A, sst.C}
	c.Worker.Add(states, nil)

	// assert
	assert.True(t, s.Mach.Is1(ssrpc.ServerStates.Ready), "Server ready")
	assert.True(t, s.Mach.Is1(ssrpc.ClientStates.Ready), "Client ready")

	assert.True(t, s.Mach.Not1(am.StateException), "No server errors")
	assert.True(t, c.Mach.Not1(am.StateException), "No client errors")

	assert.True(t, worker.Is(states), "Worker state set on the server")
	assert.True(t, c.Worker.Is(states),
		"Worker state set on the client")

	c.Mach.Log("OK")
	s.Mach.Log("OK")

	// shut down
	c.Mach.Remove1(ssrpc.ClientStates.Start, nil)
	s.Mach.Remove1(ssrpc.ServerStates.Start, nil)
}

func TestWaiting(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	counter, _, s, c := NewTest(t, ctx, nil, end, nil, nil, false)

	// test
	whenA := make(chan struct{})
	go func() {
		<-c.Worker.When1(sst.A, ctx)
		close(whenA)
	}()
	states := am.S{sst.A, sst.C}
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
		"Client called RemoteHello, RemoteHandshake, RemoteAdd")
	bytesCount := <-counter
	assert.LessOrEqual(t, 650, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 1_000, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(t, c, s, true)
}

func TestAddMany(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	counter, _, s, c := NewTest(t, ctx, nil, end, nil, nil, false)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(sst.D, ctx)
		close(whenD)
	}()
	states := am.S{sst.A, sst.C}
	for i := 0; i < 500; i++ {
		c.Worker.Add(states, nil)
	}
	c.Worker.Add1(sst.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	assert.Equal(t, 0, int(s.CallCount),
		"Server piggybacked clock on resp")
	bytesCount := <-counter
	assert.LessOrEqual(t, 15_000, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")
	assert.GreaterOrEqual(t, 16_000, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")

	disposeTest(t, c, s, true)
}

func TestAddManyNoSync(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	// disable clock pushes
	interval := 0 * time.Hour
	counter, _, s, c := NewTest(t, ctx, nil, end, &interval, nil, false)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(sst.D, ctx)
		close(whenD)
	}()
	states := am.S{sst.A, sst.C}
	for i := 0; i < 500; i++ {
		c.Worker.AddNS(states, nil)
	}
	c.Worker.Add1NS(sst.D, nil)

	// wait for the network to settle, as Sync arrives before +D, this can be
	// flaky, and it's better to Add1(sstest.D, nil), but Sync() is being tested
	// here
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
	assert.LessOrEqual(t, 8_050, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")
	assert.GreaterOrEqual(t, 8_500, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")

	disposeTest(t, c, s, true)
}

func TestAddManyInstantClock(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	// disable clock optimization (instant pushes)
	interval := 1 * time.Nanosecond
	counter, _, s, c := NewTest(t, ctx, nil, end, &interval, nil, false)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(sst.D, ctx)
		close(whenD)
	}()
	states := am.S{sst.A, sst.C}
	for i := 0; i < 500; i++ {
		c.Worker.Add(states, nil)
	}
	c.Worker.Add1(sst.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	bytesCount := <-counter
	assert.LessOrEqual(t, 15_000, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 16_000, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(t, c, s, true)
}

func TestManyStates(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})

	// reuse the worker and add many rand states
	ssStruct := am.SchemaMerge(ssrpc.WorkerSchema, sst.States)
	ssNames := am.SAdd(ssrpc.WorkerStates.Names(), sst.Names)
	randAmount := 100
	for i := 0; i < randAmount; i++ {
		n := fmt.Sprintf("State%d", i)
		ssNames = append(ssNames, n)
		ssStruct[n] = am.State{}
	}
	worker, err := am.NewCommon(context.Background(), "w-"+t.Name(), ssStruct,
		ssNames, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	counter, _, s, c := NewTest(t, ctx, worker, end, nil, nil, false)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(sst.D, ctx)
		close(whenD)
	}()
	for i := 5; i < randAmount-5; i++ {
		c.Worker.Remove1(ssNames[i-3], nil)
		c.Worker.Add1(ssNames[i], nil)
	}
	c.Worker.Add1(sst.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	assert.Equal(t, 183, int(c.CallCount),
		"Client called handshake (2) and mutations (181)")
	bytesCount := <-counter
	assert.LessOrEqual(t, 7_500, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 8_000, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(t, c, s, true)
}

func TestHighInstantClocks(t *testing.T) {
	t.Parallel()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})

	// reuse the worker and bump the clocks high
	worker := utils.NewRelsRpcWorker(t, nil)
	clock := worker.Clock(nil)
	clock[sst.A] = 1_000_000
	clock[sst.C] = 1_000_000
	am.MockClock(worker, clock)
	// disable clock optimization
	interval := 0 * time.Second
	counter, _, s, c := NewTest(t, ctx, worker, end, &interval, nil, false)

	// test
	assert.GreaterOrEqual(t, 1_000_000, int(worker.Tick(sst.A)),
		"Bytes transferred (both ways)")
	whenD := make(chan struct{})
	go func() {
		<-c.Worker.When1(sst.D, ctx)
		close(whenD)
	}()
	states := am.S{sst.A, sst.C}
	for i := 0; i < 500; i++ {
		c.Worker.Add(states, nil)
		c.Worker.Remove(states, nil)
	}
	c.Worker.Add1(sst.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	// byte count should be the same as in TestAddManyInstantClock
	bytesCount := <-counter
	assert.LessOrEqual(t, 39_000, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 40_000, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(t, c, s, true)
}

// TestRetryCall

func TestClockPush(t *testing.T) {
	// TODO TestClockPush
	t.Skip("test server-side mutations push their clock")
}

type TestRetryCallHandlers struct {
	blocked bool
}

func (h *TestRetryCallHandlers) DState(e *am.Event) {
	if h.blocked {
		return
	}

	e.Machine().Log("Blocking for 1s")
	time.Sleep(1 * time.Second)
	h.blocked = true
}

func TestRetryCall(t *testing.T) {
	t.Parallel()
	// amhelp.EnableDebugging()

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, w, s, c := NewTest(t, ctx, nil, nil, nil, nil, false)
	handlers := &TestRetryCallHandlers{}
	w.MustBindHandlers(handlers)

	// inject a fake error
	c.tmpTestErr = fmt.Errorf("IGNORE MOCK ERR")
	whenRetrying := c.Mach.When1(ssrpc.ClientStates.RetryingCall, nil)
	c.Worker.Add1(sst.A, nil)
	amhelpt.WaitForAll(t, "RetryingCall", ctx, 2*time.Second, whenRetrying)

	// .TODO amtest
	assert.True(t, s.Mach.Is1(ssrpc.ServerStates.Ready), "Server ready")
	assert.True(t, s.Mach.Is1(ssrpc.ClientStates.Ready), "Client ready")

	c.Mach.Log("Generic err retried")

	// extend the timeout to cause a network one (handler blocks for 1s)
	w.HandlerTimeout = 5 * time.Second
	c.CallTimeout = 500 * time.Millisecond
	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		// this will block and retry
		c.Worker.Add1(sst.D, nil)
		wg.Done()
	}()
	go func() {
		<-c.Mach.When1(ssrpc.ClientStates.RetryingCall, ctx)
		c.Mach.Log("Timeout err retried")
		wg.Done()
	}()

	wg.Wait()

	// TODO amtest asserts
	assert.True(t, w.Is1(sst.D), "Worker state set")
	assert.True(t, s.Mach.Is1(ssrpc.ServerStates.Ready), "Server ready")
	assert.True(t, s.Mach.Is1(ssrpc.ClientStates.Ready), "Client ready")
	assert.True(t, handlers.blocked, "Handlers should block")

	disposeTest(t, c, s, false)
}

func TestRetryConn(t *testing.T) {
	t.Parallel()
	// amhelp.EnableDebugging(true)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// test
	_, _, s, c := NewTest(t, ctx, nil, nil, nil, nil, true)
	lis := *s.Listener.Load()
	addr := lis.Addr()
	lis.Close()
	s.Addr = addr.String()

	go func() {
		// wait for client to reconnect and then start the server
		<-c.Mach.WhenTime(am.S{ssC.Connecting}, am.Time{3}, nil)
		s.Start()
	}()

	// client ready
	c.Start()
	amhelpt.WaitForAll(t, "client-server Ready", ctx, 3*time.Second,
		c.Mach.When1(ssC.Ready, ctx),
		s.Mach.When1(ssS.Ready, ctx))

	c.Worker.Add1(sst.A, nil)

	// assert
	amhelpt.AssertIs1(t, c.Mach, ssrpc.ClientStates.Ready)
	amhelpt.AssertIs1(t, s.Mach, ssrpc.ServerStates.Ready)

	amhelpt.AssertNot1(t, c.Mach, ssrpc.ClientStates.RetryingCall)
	amhelpt.AssertNot1(t, c.Mach, ssrpc.ClientStates.RetryingConn)

	c.Mach.Log("Network err retried")

	disposeTest(t, c, s, false)
}

// TestRetryErrNetworkTimeout

type TestRetryErrNetworkTimeoutHandlers struct {
	blocked     bool
	shouldBlock bool
}

func (h *TestRetryErrNetworkTimeoutHandlers) DState(e *am.Event) {
	if !h.shouldBlock {
		return
	}
	e.Machine().Log("Blocking for 1s")
	time.Sleep(1 * time.Second)
	h.blocked = true
}

func TestRetryErrNetworkTimeout(t *testing.T) {
	t.Parallel()
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, w, s, c := NewTest(t, ctx, nil, nil, nil, nil, false)
	handlers := &TestRetryErrNetworkTimeoutHandlers{
		shouldBlock: true,
	}
	w.MustBindHandlers(handlers)

	// test network timeout
	// extend the handler timeout (handler blocks for 1s)
	w.HandlerTimeout = 5 * time.Second
	c.CallTimeout = 500 * time.Millisecond
	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		<-c.Mach.When1(ssrpc.ClientStates.RetryingCall, ctx)
		c.Mach.Log("Timeout err retried")
		wg.Done()
	}()
	go func() {
		// this will block until connections restarts
		c.Worker.Add1(sst.D, nil)
		wg.Done()
	}()

	wg.Wait()

	// assert
	amhelpt.AssertIs1(t, w, sst.D)

	amhelpt.AssertIs1(t, c.Mach, ssrpc.ClientStates.Ready)
	amhelpt.AssertIs1(t, s.Mach, ssrpc.ServerStates.Ready)

	amhelpt.AssertNot1(t, c.Mach, ssrpc.ClientStates.RetryingCall)
	amhelpt.AssertNot1(t, c.Mach, ssrpc.ClientStates.RetryingConn)

	assert.True(t, handlers.blocked, "Handlers should block")

	disposeTest(t, c, s, false)
}

func TestRetryClosedListener(t *testing.T) {
	t.Parallel()
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, s, c := NewTest(t, ctx, nil, nil, nil, nil, false)

	// close the listener and try a mutation
	lis := *s.Listener.Load()
	_ = lis.Close()
	time.Sleep(100 * time.Millisecond)
	c.Worker.Add1(sst.D, nil)

	// wait for D
	_ = amhelp.WaitForAll(ctx, 2*time.Second, c.Worker.When1(sst.D, ctx))

	// assert
	amhelpt.AssertIs1(t, c.Mach, ssrpc.ClientStates.Ready)
	amhelpt.AssertIs1(t, s.Mach, ssrpc.ServerStates.Ready)

	amhelpt.AssertNot1(t, c.Mach, ssrpc.ClientStates.RetryingCall)
	amhelpt.AssertNot1(t, c.Mach, ssrpc.ClientStates.RetryingConn)

	disposeTest(t, c, s, true)
}

// TestPayload

type TestPayloadWorker struct{}

// CState will trigger SendPayload
func (w *TestPayloadWorker) CState(e *am.Event) {
	// TODO use v2 state def
	e.Machine().Remove1(sst.C, nil)
	args := ParseArgs(e.Args)
	argsOut := &A{
		Name:    args.Name,
		Payload: &ArgsPayload{Data: "Hello", Name: args.Name},
	}

	e.Machine().Add1(ssW.SendPayload, Pass(argsOut))
}

type TestPayloadConsumer struct {
	t         *testing.T
	delivered bool
}

func (c *TestPayloadConsumer) WorkerPayloadState(e *am.Event) {
	e.Machine().Remove1(ssCo.WorkerPayload, nil)

	args := ParseArgs(e.Args)
	assert.Equal(c.t, "TestPayload", args.Name)
	assert.Equal(c.t, "Hello", args.Payload.Data.(string))

	c.delivered = true
}

func TestPayload(t *testing.T) {
	t.Parallel()
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ssCo := ssrpc.ConsumerStates

	// consumer
	consHandlers := &TestPayloadConsumer{t: t}
	consMach, err := am.NewCommon(ctx, "TestPayloadConsumer",
		ssrpc.ConsumerSchema, ssCo.Names(), consHandlers, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// worker
	worker := utils.NewNoRelsRpcWorker(t, nil)
	err = worker.BindHandlers(&TestPayloadWorker{})
	if err != nil {
		t.Fatal(err)
	}

	// init RPC
	_, _, s, c := NewTest(t, ctx, worker, nil, nil, consMach, false)

	whenDelivered := consMach.When1(ssCo.WorkerPayload, nil)
	// Consumer requests a payload from the remote worker
	c.Worker.Add1(sst.C, PassRpc(&A{Name: "TestPayload"}))
	// Consumer waits for WorkerDelivered
	err = amhelp.WaitForAll(ctx, 2*time.Second, whenDelivered)

	// assert
	assert.NoError(t, err, "Timeout when waiting for the package")
	assert.True(t, consHandlers.delivered, "Consumer got the package")

	disposeTest(t, c, s, true)
}

func TestVerifyWorkerStates(t *testing.T) {
	// TODO TestVerifyWorkerStates
	t.Skip("TODO")
}

// TODO test gob errors (although not user-facing)

func TestMux(t *testing.T) {
	// TODO flaky
	//  test_help.go:60: error for cWorkers A: timeout
	//  --- FAIL: TestMux (2.04s)
	if os.Getenv(amhelp.EnvAmTestRunner) != "" {
		t.Skip("FLAKY")
		return
	}

	t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx := context.Background()

	// bind to an open port
	listener := utils.RandListener("localhost")
	serverAddr := listener.Addr().String()
	connAddr := serverAddr

	// init source & mux
	w := utils.NewRelsRpcWorker(t, nil)
	amhelpt.MachDebugEnv(t, w)
	mux, err := NewMux(ctx, t.Name(), nil, &MuxOpts{
		Parent: w,
	})

	// client fac
	newC := func(num int) *Client {
		name := fmt.Sprintf("%s-%d", t.Name(), num)
		c, err := NewClient(ctx, connAddr, name, w.Schema(), w.StateNames(),
			&ClientOpts{Parent: mux.Mach})
		if err != nil {
			t.Fatal(err)
		}
		amhelpt.MachDebugEnv(t, c.Mach)

		return c
	}

	// server fac
	mux.NewServerFn = func(num int, _ net.Conn) (*Server, error) {
		name := fmt.Sprintf("%s-%d", t.Name(), num)
		s, err := NewServer(ctx, serverAddr, name, w, &ServerOpts{
			Parent: mux.Mach,
		})
		if err != nil {
			t.Fatal(err)
		}
		amhelpt.MachDebugEnv(t, s.Mach)

		return s, nil
	}

	// start cmux
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, mux.Mach)
	mux.Listener = listener
	mux.Start()
	amhelpt.WaitForAll(t, "mux Ready", ctx, 2*time.Second,
		mux.Mach.When1(ssM.Ready, nil))

	var clients []*Client
	var clientsApi []am.Api
	var cWorkers []am.Api

	// connect 10 clients to the worker
	for i := 0; i < 10; i++ {
		c := newC(i)
		c.Start()
		clients = append(clients, c)
		cWorkers = append(cWorkers, c.Worker)
		clientsApi = append(clientsApi, c.Mach)
	}

	// wait for all clients to be ready
	amhelpt.WaitForAll(t, "group Ready", ctx, 2*time.Second,
		amhelpt.GroupWhen1(t, clientsApi, ssC.Ready, nil)...)

	for _, w := range cWorkers {
		amhelpt.MachDebugEnv(t, w)
	}

	// start mutating (C adds auto A)
	// TODO use v2 state def
	clients[0].Worker.Add1(sst.C, nil)

	// wait for all clients to get the new state
	amhelpt.WaitForAll(t, "cWorkers A", ctx, 2*time.Second,
		// TODO use v2 state def
		amhelpt.GroupWhen1(t, cWorkers, sst.A, nil)...)

	if amhelp.IsTelemetry() {
		time.Sleep(1 * time.Second)
	}
}

func TestRetryingConnState(t *testing.T) {
	t.Skip("TODO")
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////}

func NewTest(
	t *testing.T, ctx context.Context, worker *am.Machine,
	disposeMeter <-chan struct{}, clockInterval *time.Duration,
	consumer *am.Machine, skipStart bool,
) (<-chan int64, *am.Machine, *Server, *Client) {
	// bind to an open port
	listener := utils.RandListener("localhost")
	serverAddr := listener.Addr().String()
	connAddr := serverAddr

	// worker init
	if worker == nil {
		worker = utils.NewRelsRpcWorker(t, nil)
	}
	amhelpt.MachDebugEnv(t, worker)

	// traffic counter init
	var counter chan int64
	if disposeMeter != nil {
		counterListener := utils.RandListener("localhost")
		connAddr = counterListener.Addr().String()
		if amhelp.IsDebug() {
			t.Logf("Meter addr: %s", connAddr)
		}
		counter = make(chan int64, 1)

		go TrafficMeter(counterListener, serverAddr, counter, disposeMeter)
		time.Sleep(100 * time.Millisecond)
	}

	// server init
	s, err := NewServer(ctx, serverAddr, t.Name(), worker, &ServerOpts{
		Parent: worker,
	})
	if err != nil {
		t.Fatal(err)
	}
	// set the test listener to avoid port conflicts
	s.Listener.Store(&listener)
	amhelpt.MachDebugEnv(t, s.Mach)
	if clockInterval != nil {
		s.PushInterval = *clockInterval
	}
	// let it settle
	time.Sleep(10 * time.Millisecond)

	// client init
	c, err := NewClient(ctx, connAddr, t.Name(), worker.Schema(),
		worker.StateNames(), &ClientOpts{
			Consumer: consumer,
			Parent:   worker,
		})
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, c.Mach)
	if consumer != nil {
		amhelpt.MachDebugEnv(t, consumer)
	}

	// tear down
	t.Cleanup(func() {
		<-s.Mach.WhenDisposed()
		<-c.Mach.WhenDisposed()
		// cool off am-dbg and free the ports
		if os.Getenv(telemetry.EnvAmDbgAddr) != "" {
			time.Sleep(100 * time.Millisecond)
		}
	})

	if skipStart {
		return counter, worker, s, c
	}

	// server start
	s.Start()
	amhelpt.WaitForAll(t, "RpcReady", ctx, 3*time.Second,
		s.Mach.When1(ssS.RpcReady, ctx))

	// client ready
	c.Start()
	amhelpt.WaitForAll(t, "client-server Ready", ctx, 3*time.Second,
		c.Mach.When1(ssC.Ready, ctx),
		s.Mach.When1(ssS.Ready, ctx))

	return counter, worker, s, c
}
