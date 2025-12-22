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
	"github.com/stretchr/testify/require"

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
		_ = os.Setenv(EnvAmRpcLogClient, "1")
		_ = os.Setenv(EnvAmRpcLogServer, "1")
		_ = os.Setenv(EnvAmRpcLogMux, "1")
	}
}

func TestBasic(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(true)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// init worker
	schema := am.SchemaMerge(ssrpc.NetSourceSchema, am.Schema{
		"Foo": {},
		"Bar": {Require: am.S{"Foo"}},
	})
	names := am.SAdd(ssrpc.NetSourceStates.Names(), am.S{"Foo", "Bar"})
	netSrc := am.New(ctx, schema, &am.Opts{Id: "ns-" + t.Name()})
	err := netSrc.VerifyStates(names)
	if err != nil {
		t.Fatal(err)
	}

	amhelpt.MachDebugEnv(t, netSrc)

	// init server and client
	_, _, s, c := NewTest(t, ctx, netSrc, nil, 0, false, nil, nil)

	// test
	c.NetMach.Add1("Foo", nil)

	// assert
	assert.True(t, s.Mach.Is1(ssrpc.ServerStates.Ready), "Server ready")
	assert.True(t, c.Mach.Is1(ssrpc.ClientStates.Ready), "Client ready")

	assert.True(t, s.Mach.Not1(am.StateException), "No server errors")
	assert.True(t, c.Mach.Not1(am.StateException), "No client errors")

	assert.True(t, netSrc.Is1("Foo"), "NetworkMachine state set on the server")
	assert.True(t, c.NetMach.Is1("Foo"), "NetworkMachine state set on the client")

	c.Mach.Log("OK")
	s.Mach.Log("OK")

	// shut down
	c.Mach.Remove1(ssrpc.ClientStates.Start, nil)
	s.Mach.Remove1(ssrpc.ServerStates.Start, nil)
}

func TestTypeSafe(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	// read env
	amDbgAddr := os.Getenv(telemetry.EnvAmDbgAddr)
	logLvl := am.EnvLogLevel("")

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := utils.NewRelsNetSrc(t, nil)
	amhelpt.MachDebug(t, worker, amDbgAddr, logLvl, true)

	// init server and client
	_, _, s, c := NewTest(t, ctx, worker, nil, 0, false, nil, nil)

	// test
	states := am.S{sst.A, sst.C}
	c.NetMach.Add(states, nil)

	// assert
	assert.True(t, s.Mach.Is1(ssrpc.ServerStates.Ready), "Server ready")
	assert.True(t, s.Mach.Is1(ssrpc.ClientStates.Ready), "Client ready")

	assert.True(t, s.Mach.Not1(am.StateException), "No server errors")
	assert.True(t, c.Mach.Not1(am.StateException), "No client errors")

	assert.True(t, worker.Is(states), "NetworkMachine state set on the server")
	assert.True(t, c.NetMach.Is(states),
		"NetworkMachine state set on the client")

	c.Mach.Log("OK")
	s.Mach.Log("OK")

	// shut down
	c.Mach.Remove1(ssrpc.ClientStates.Start, nil)
	s.Mach.Remove1(ssrpc.ServerStates.Start, nil)
}

func TestWaiting(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	counter, _, s, c := NewTest(t, ctx, nil, end, 0, false, nil, nil)

	// test
	whenA := make(chan struct{})
	go func() {
		<-c.NetMach.When1(sst.A, ctx)
		close(whenA)
	}()
	states := am.S{sst.A, sst.C}
	c.NetMach.Add(states, nil)
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
	assert.LessOrEqual(t, 1_400, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 1_500, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(t, c, s, true)
}

func TestAddMany(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	counter, _, s, c := NewTest(t, ctx, nil, end, 0, false, nil, nil)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.NetMach.When1(sst.D, ctx)
		close(whenD)
	}()
	states := am.S{sst.A, sst.C}
	for i := 0; i < 500; i++ {
		c.NetMach.Add(states, nil)
	}
	c.NetMach.Add1(sst.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	assert.Equal(t, 0, int(s.CallCount),
		"Server piggybacked clock on resp")
	bytesCount := <-counter
	assert.LessOrEqual(t, 20_000, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")
	assert.GreaterOrEqual(t, 21_000, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")

	disposeTest(t, c, s, true)
}

func TestAddManyNoSync(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	// disable clock pushes
	counter, _, s, c := NewTest(t, ctx, nil, end, 0, false, nil, nil)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.NetMach.When1(sst.D, ctx)
		close(whenD)
	}()
	states := am.S{sst.A, sst.C}
	for i := 0; i < 500; i++ {
		c.NetMach.AddNS(states, nil)
	}
	c.NetMach.Add1NS(sst.D, nil)

	// wait for the network to settle, as Sync arrives before +D, this can be
	// flaky, and it's better to Add1(sstest.D, nil), but Sync() is being tested
	// here
	time.Sleep(1000 * time.Millisecond)

	// manual sync, should tick D
	c.Sync()
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	bytesCount := <-counter
	assert.LessOrEqual(t, 10_000, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")
	assert.GreaterOrEqual(t, 11_000, int(bytesCount),
		"Client called handshake (2) and A,C (500) and D(1)")

	disposeTest(t, c, s, true)
}

func TestAddManyInstantClock(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	// disable clock optimization (instant pushes)
	interval := 1 * time.Nanosecond
	counter, _, s, c := NewTest(t, ctx, nil, end, interval, false, nil, nil)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.NetMach.When1(sst.D, ctx)
		close(whenD)
	}()
	states := am.S{sst.A, sst.C}
	for i := 0; i < 500; i++ {
		c.NetMach.Add(states, nil)
	}
	c.NetMach.Add1(sst.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	bytesCount := <-counter
	assert.LessOrEqual(t, 20_000, int(bytesCount),
		"Bytes transferred (both ways)")
	// 549_751
	assert.GreaterOrEqual(t, 21_000, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(t, c, s, true)
}

func TestManyStates(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})

	// reuse the net src and add many rand states
	schema := am.SchemaMerge(ssrpc.NetSourceSchema, sst.States)
	names := am.SAdd(ssrpc.NetSourceStates.Names(), sst.Names)
	randAmount := 100
	for i := 0; i < randAmount; i++ {
		n := fmt.Sprintf("State%d", i)
		names = append(names, n)
		schema[n] = am.State{}
	}
	netSrc, err := am.NewCommon(context.Background(), "ns-"+t.Name(), schema,
		names, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	counter, _, s, c := NewTest(t, ctx, netSrc, end, 0, false, nil, nil)

	// test
	whenD := make(chan struct{})
	go func() {
		<-c.NetMach.When1(sst.D, ctx)
		close(whenD)
	}()
	c.NetMach.Add1(sst.C, nil)
	for i := 5; i < randAmount-5; i++ {
		c.NetMach.Remove1(names[i-3], nil)
		c.NetMach.Add1(names[i], nil)
	}
	c.NetMach.Add1(sst.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	assert.Equal(t, 184, int(c.CallCount),
		"Client called handshake (2) and mutations (181)")
	bytesCount := <-counter
	assert.LessOrEqual(t, 10_000, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 10_500, int(bytesCount),
		"Bytes transferred (both ways)")

	disposeTest(t, c, s, true)
}

func TestHighInstantClocks(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})

	// reuse the worker and bump the clocks high
	worker := utils.NewRelsNetSrc(t, nil)
	clock := worker.Clock(nil)
	clock[sst.A] = 1_000_000
	clock[sst.C] = 1_000_000
	am.MockClock(worker, clock)
	// disable clock optimization
	counter, _, s, c := NewTest(t, ctx, worker, end, 0, false, nil, nil)

	// test
	assert.GreaterOrEqual(t, 1_000_000, int(worker.Tick(sst.A)),
		"Bytes transferred (both ways)")
	whenD := make(chan struct{})
	go func() {
		<-c.NetMach.When1(sst.D, ctx)
		close(whenD)
	}()
	states := am.S{sst.A, sst.C}
	for i := 0; i < 500; i++ {
		c.NetMach.Add(states, nil)
		c.NetMach.Remove(states, nil)
	}
	c.NetMach.Add1(sst.D, nil)
	<-whenD

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	// byte count should be the same as in TestAddManyInstantClock
	bytesCount := <-counter
	assert.LessOrEqual(t, 48_000, int(bytesCount),
		"Bytes transferred (both ways)")
	assert.GreaterOrEqual(t, 50_000, int(bytesCount),
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
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, w, s, c := NewTest(t, ctx, nil, nil, 0, false, nil, nil)
	handlers := &TestRetryCallHandlers{}
	w.MustBindHandlers(handlers)

	// inject a fake error
	c.tmpTestErr = fmt.Errorf("IGNORE MOCK ERR")
	whenRetrying := c.Mach.When1(ssrpc.ClientStates.RetryingCall, nil)
	c.NetMach.Add1(sst.A, nil)
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
		c.NetMach.Add1(sst.D, nil)
		wg.Done()
	}()
	go func() {
		<-c.Mach.When1(ssrpc.ClientStates.RetryingCall, ctx)
		c.Mach.Log("Timeout err retried")
		wg.Done()
	}()

	wg.Wait()

	// TODO amtest asserts
	assert.True(t, w.Is1(sst.D), "NetworkMachine state set")
	assert.True(t, s.Mach.Is1(ssrpc.ServerStates.Ready), "Server ready")
	assert.True(t, s.Mach.Is1(ssrpc.ClientStates.Ready), "Client ready")
	assert.True(t, handlers.blocked, "Handlers should block")

	disposeTest(t, c, s, false)
}

func TestRetryConn(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(true)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// test
	_, _, s, c := NewTest(t, ctx, nil, nil, 0, true, nil, nil)
	lis := *s.Listener.Load()
	addr := lis.Addr()
	_ = lis.Close()
	s.Addr = addr.String()

	go func() {
		// wait for client to reconnect and then start the server
		<-c.Mach.WhenTime1(ssC.Connecting, 3, nil)
		s.Start()
	}()

	// client ready
	c.Start()
	amhelpt.WaitForAll(t, "client-server Ready", ctx, 3*time.Second,
		c.Mach.When1(ssC.Ready, ctx),
		s.Mach.When1(ssS.Ready, ctx))

	c.NetMach.Add1(sst.A, nil)

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
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, w, s, c := NewTest(t, ctx, nil, nil, 0, false, nil, nil)
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
		c.NetMach.Add1(sst.D, nil)
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
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(true)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, s, c := NewTest(t, ctx, nil, nil, 0, false, nil, nil)

	// close the listener and try a mutation
	lis := *s.Listener.Load()
	_ = lis.Close()
	time.Sleep(100 * time.Millisecond)
	c.NetMach.Add1(sst.D, nil)

	// wait for D
	_ = amhelp.WaitForAll(ctx, 2*time.Second, c.NetMach.When1(sst.D, ctx))

	// assert
	amhelpt.AssertIs1(t, c.Mach, ssrpc.ClientStates.Ready)
	amhelpt.AssertIs1(t, s.Mach, ssrpc.ServerStates.Ready)

	amhelpt.AssertNot1(t, c.Mach, ssrpc.ClientStates.RetryingCall)
	amhelpt.AssertNot1(t, c.Mach, ssrpc.ClientStates.RetryingConn)

	assertTime(t, c.Mach, am.S{ssC.Exception, ssC.ErrRpc}, am.Time{2, 2})
	disposeTest(t, c, s, false)
}

// TestPayload

type TestPayloadHandlers struct{}

// CState will trigger SendPayload
func (w *TestPayloadHandlers) CState(e *am.Event) {
	// TODO use v2 state def
	e.Machine().Remove1(sst.C, nil)
	args := ParseArgs(e.Args)
	argsOut := &A{
		Name:    args.Name,
		Payload: &MsgSrvPayload{Data: "Hello", Name: args.Name},
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
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
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
	worker := utils.NewNoRelsNetSrc(t, nil)
	err = worker.BindHandlers(&TestPayloadHandlers{})
	if err != nil {
		t.Fatal(err)
	}

	// init RPC
	_, _, s, c := NewTest(t, ctx, worker, nil, 0, false, &ClientOpts{
		Consumer: consMach,
	}, nil)

	whenDelivered := consMach.When1(ssCo.WorkerPayload, nil)
	// Consumer requests a payload from the remote worker
	c.NetMach.Add1(sst.C, PassRpc(&A{Name: "TestPayload"}))
	// Consumer waits for WorkerDelivered
	err = amhelp.WaitForAll(ctx, 2*time.Second, whenDelivered)

	// assert
	assert.NoError(t, err, "Timeout when waiting for the package")
	assert.True(t, consHandlers.delivered, "Consumer got the package")

	disposeTest(t, c, s, true)
}

// TODO test gob errors (although not user-facing)

func TestMux(t *testing.T) {
	// numClients := 10
	numClients := 3

	// TODO flaky
	//  test_help.go:60: error for cWorkers A: timeout
	//  --- FAIL: TestMux (2.04s)
	if os.Getenv(amhelp.EnvAmTestRunner) != "" {
		t.Skip("FLAKY")
		return
	}
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)
	ctx := context.Background()

	// bind to an open port
	listener := utils.RandListener("localhost")
	serverAddr := listener.Addr().String()
	connAddr := serverAddr

	// init source & mux
	netSrc := utils.NewRelsNetSrc(t, nil)
	amhelpt.MachDebugEnv(t, netSrc)
	mux, err := NewMux(ctx, t.Name(), nil, &MuxOpts{
		Parent: netSrc,
	})

	// client fac
	newC := func(num int) *Client {
		name := fmt.Sprintf("%s-%d", t.Name(), num)
		c, err := NewClient(ctx, connAddr, name, netSrc.Schema(),
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
		s, err := NewServer(ctx, serverAddr, name, netSrc, &ServerOpts{
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
	var netMachs []am.Api

	// connect 10 clients to the worker
	for i := 0; i < numClients; i++ {
		c := newC(i)
		c.Start()
		clients = append(clients, c)
		netMachs = append(netMachs, c.NetMach)
		clientsApi = append(clientsApi, c.Mach)
	}

	// wait for all clients to be ready
	amhelpt.WaitForAll(t, "group Ready", ctx, 2*time.Second,
		amhelpt.GroupWhen1(t, clientsApi, ssC.Ready, nil)...)

	for _, w := range netMachs {
		amhelpt.MachDebugEnv(t, w)
	}

	// start mutating (C adds auto A)
	clients[0].NetMach.Add1(sst.C, nil)

	// wait for all clients to get the new state
	amhelpt.WaitForAll(t, "netMachs A", ctx, 2*time.Second,
		amhelpt.GroupWhen1(t, netMachs, sst.A, nil)...)

	if amhelp.IsTelemetry() {
		time.Sleep(1 * time.Second)
	}
}

func TestRetryingConnState(t *testing.T) {
	t.Skip("TODO")
}

func TestPartial(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// net source mach with non-zero clocks
	source := utils.NewNoRelsNetSrc(t, nil)
	source.Add1(sst.C, nil)
	source.Remove1(sst.C, nil)

	// init RPC
	_, _, s, c := NewTest(t, ctx, source, nil, 0, false, &ClientOpts{
		AllowedStates: am.S{sst.A, sst.B, sst.C},
		SkippedStates: am.S{sst.C},
	}, nil)

	// test
	source.Add1(sst.A, nil)
	source.Add(am.S{sst.B, sst.C}, nil)
	source.Add1(sst.D, nil)

	// assert
	amhelpt.WaitForAll(t, "TestPartial(A, B)", ctx, time.Second,
		c.NetMach.When(am.S{sst.A, sst.B}, nil))
	assertStates(t, c.NetMach, am.S{sst.A, sst.B})

	// TODO schema change
	// TODO full sync

	// dispose
	disposeTest(t, c, s, true)
	if amhelp.IsTelemetry() {
		time.Sleep(1 * time.Second)
	}
}

func TestPartialInferred(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// net source mach
	source := utils.NewRelsNetSrc(t, nil)

	// init RPC
	_, _, s, c := NewTest(t, ctx, source, nil, 0, false, &ClientOpts{
		AllowedStates: am.S{sst.A},
	}, nil)

	// test (C will add A, and netmach will infer it from the schema)
	source.Add1(sst.C, nil)

	// assert
	amhelpt.WaitForAll(t, "TestPartial(A, C)", ctx, time.Second,
		c.NetMach.When(am.S{sst.A, sst.C}, nil))
	assertStates(t, c.NetMach, am.S{sst.A, sst.C})

	// TODO schema change
	// TODO full sync

	// dispose
	disposeTest(t, c, s, true)
	if amhelp.IsTelemetry() {
		time.Sleep(1 * time.Second)
	}
}

func TestPartialNoSchema(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// net source mach
	source := utils.NewRelsNetSrc(t, nil)

	// init RPC
	_, _, s, c := NewTest(t, ctx, source, nil, 0, false, &ClientOpts{
		AllowedStates: am.S{sst.C},
		NoSchema:      true,
	}, nil)

	// test (C will add A, but netmach A wont be inferred)
	source.Add1(sst.C, nil)

	// assert
	expected := am.S{sst.C}
	amhelpt.WaitForAll(t, "TestPartial(A, C)", ctx, time.Second,
		c.NetMach.When(expected, nil))
	assertStates(t, c.NetMach, expected)

	// TODO schema change
	// TODO full sync

	// dispose
	disposeTest(t, c, s, true)
	if amhelp.IsTelemetry() {
		time.Sleep(1 * time.Second)
	}
}

// TestSchemaFilteringSync

type TestSchemaFilteringSyncTracer struct {
	*am.TracerNoOp
	amount int
}

func (t *TestSchemaFilteringSyncTracer) MutationQueued(
	machine am.Api, mutation *am.Mutation,
) {
	t.amount++
}

func TestSchemaFilteringSync(t *testing.T) {
	// TODO
	t.Skip("mutation filtering not implemented yet")
	return

	// amhelp.EnableDebugging(false)

	// // config
	// ctx, cancel := context.WithCancel(context.Background())
	// defer cancel()
	// end := make(chan struct{})
	// counter, netSrc, s, c := NewTest(t, ctx, nil, end, time.Second, false,
	// 	nil, nil)
	//
	// qCount := &TestSchemaFilteringSyncTracer{}
	// require.NoError(t, netSrc.BindTracer(qCount))
	//
	// // test
	// // A req C
	// c.NetMach.Add1(sst.A, nil)
	// c.NetMach.Add(am.S{sst.C, sst.A}, nil)
	//
	// // mark log and counter
	// c.Mach.Log("OK")
	// s.Mach.Log("OK")
	// close(end)
	//
	// // assert
	// assert.Len(t, c.NetMach.schema, 1+len(sst.States)+
	//   len(ssrpc.NetSourceSchema), "schema len")
	// assert.Equal(t, 1, qCount.amount,
	// 	"one queued mutation, one filtered out")
	// bytesCount := <-counter
	// assert.LessOrEqual(t, 1_000, int(bytesCount))
	// assert.GreaterOrEqual(t, 2_000, int(bytesCount))
	// assertStates(t, netSrc, am.S{sst.C, sst.A})
	//
	// disposeTest(t, c, s, true)
}

func TestShallowSync(t *testing.T) {
	t.Skip("TODO")
}

func TestNoSchema(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// net source mach
	source := utils.NewRelsNetSrc(t, nil)

	// init RPC
	_, _, s, c := NewTest(t, ctx, source, nil, 0, false, &ClientOpts{
		NoSchema: true,
	}, nil)

	// test (C will add A, but netmach A wont be inferred)
	source.Add1(sst.C, nil)

	// assert
	expected := am.S{sst.A, sst.C}
	amhelpt.WaitForAll(t, "TestNoSchema(A, B, C, D)", ctx, time.Second,
		c.NetMach.When(expected, nil))
	assertStates(t, c.NetMach, expected)

	// TODO schema change
	// TODO full sync

	// dispose
	disposeTest(t, c, s, true)
	if amhelp.IsTelemetry() {
		time.Sleep(1 * time.Second)
	}
}

// TestMutationsSync

type TestMutationsSyncTracer struct {
	*am.TracerNoOp
	amount int
}

func (t *TestMutationsSyncTracer) TransitionEnd(tx *am.Transition) {
	t.amount++

	// TODO assert mut types, called states
}

func TestMutationsSync(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	// amhelp.EnableDebugging(false)

	// config
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	end := make(chan struct{})
	counter, netSrc, s, c := NewTest(t, ctx, nil, end, time.Second, false,
		&ClientOpts{
			SyncMutations: true,
		}, nil)

	txCount := &TestMutationsSyncTracer{}
	require.NoError(t, c.NetMach.BindTracer(txCount))

	// test
	// add B and cause auto:A
	netSrc.Add1(sst.B, nil)
	netSrc.Add1(sst.D, nil)
	// let the source tracer finish
	time.Sleep(100 * time.Millisecond)
	// force push
	intNano := time.Nanosecond
	s.PushInterval.Store(&intNano)
	s.pushClient()
	amhelpt.WaitForAll(t, "mutations pushed", ctx, time.Second,
		c.NetMach.When1(sst.D, nil))

	// mark log and counter
	c.Mach.Log("OK")
	s.Mach.Log("OK")
	close(end)

	// assert
	assert.Equal(t, 3, txCount.amount,
		"3 mutations came from the server")
	bytesCount := <-counter
	assert.LessOrEqual(t, 1_000, int(bytesCount))
	assert.GreaterOrEqual(t, 2_000, int(bytesCount))

	disposeTest(t, c, s, true)
}

func TestExport(t *testing.T) {
	t.Skip("TODO")

	// TODO assert mach tick

	// TODO schema change
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////}

func NewTest(t *testing.T, ctx context.Context, netSrc *am.Machine,
	disposeMeter <-chan struct{}, pushInterval time.Duration, skipStart bool,
	clientOpts *ClientOpts, serverOpts *ServerOpts,
) (<-chan int64, *am.Machine, *Server, *Client) {
	// bind to an open port
	listener := utils.RandListener("localhost")
	serverAddr := listener.Addr().String()
	connAddr := serverAddr

	// worker init
	if netSrc == nil {
		netSrc = utils.NewRelsNetSrc(t, nil)
	}
	amhelpt.MachDebugEnv(t, netSrc)

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
	if serverOpts == nil {
		serverOpts = &ServerOpts{}
	}
	serverOpts.Parent = netSrc
	s, err := NewServer(ctx, serverAddr, t.Name(), netSrc, serverOpts)
	if err != nil {
		t.Fatal(err)
	}
	// set the test listener to avoid port conflicts
	s.Listener.Store(&listener)
	amhelpt.MachDebugEnv(t, s.Mach)
	if pushInterval > 0 {
		s.PushInterval.Store(&pushInterval)
	}
	// let it settle
	time.Sleep(10 * time.Millisecond)

	// client init
	if clientOpts == nil {
		clientOpts = &ClientOpts{}
	}
	clientOpts.Parent = netSrc
	schema := netSrc.Schema()
	if clientOpts.NoSchema {
		schema = nil
	}
	c, err := NewClient(ctx, connAddr, t.Name(), schema, clientOpts)
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, c.Mach)
	if clientOpts.Consumer != nil {
		amhelpt.MachDebugEnv(t, clientOpts.Consumer)
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
		return counter, netSrc, s, c
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

	return counter, netSrc, s, c
}
