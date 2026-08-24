package test

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
	sst "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/tmc/go-iroh/key"
)

var readyTimeout = 3 * time.Second

func init() {
	if os.Getenv(am.EnvAmTestRunner) != "" {
		return
	}

	_ = godotenv.Load()

	if os.Getenv(am.EnvAmTestDebug) != "" {
		amhelp.EnableDebugging(true)
	}
}

func TestBasic(t *testing.T) {
	TemplateTestBasic(t, newTest)
}

func TestTypeSafe(t *testing.T) {
	TemplateTestTypeSafe(t, newTest)
}

func TestWaiting(t *testing.T) {
	TemplateTestWaiting(t, newTest)
}

func TestAddMany(t *testing.T) {
	TemplateTestAddMany(t, newTest)
}

func TestAddManyNoSync(t *testing.T) {
	TemplateTestAddManyNoSync(t, newTest)
}

func TestAddManyInstantClock(t *testing.T) {
	TemplateTestAddManyInstantClock(t, newTest)
}

func TestManyStates(t *testing.T) {
	TemplateTestManyStates(t, newTest)
}

func TestHighInstantClocks(t *testing.T) {
	TemplateTestHighInstantClocks(t, newTest)
}

func TestClockPush(t *testing.T) {
	TemplateTestClockPush(t, newTest)
}

func TestRetryCall(t *testing.T) {
	TemplateTestRetryCall(t, newTest)
}

func TestRetryConn(t *testing.T) {
	TemplateTestRetryConn(t, newTest)
}

func TestRetryErrNetworkTimeout(t *testing.T) {
	TemplateTestRetryErrNetworkTimeout(t, newTest)
}

func TestRetryClosedListener(t *testing.T) {
	TemplateTestRetryClosedListener(t, newTest)
}

func TestPayload(t *testing.T) {
	TemplateTestPayload(t, newTest)
}

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

	// init source & mux
	netSrc := utils.NewRelsNetSrc(t, nil)
	amhelpt.MachDebugEnv(t, netSrc)
	newServerFn := func(
		mux *arpc.Mux, id string, _ net.Conn,
	) (*arpc.Server, error) {
		s, err := arpc.NewServer(
			ctx, serverAddr, t.Name()+"-"+id, netSrc, &arpc.ServerOpts{
				Parent: mux.Mach,
			})
		if err != nil {
			t.Fatal(err)
		}
		amhelpt.MachDebugEnv(t, s.Mach)

		return s, nil
	}
	mux, err := arpc.NewMux(ctx, "", t.Name(), nil, &arpc.MuxOpts{
		Parent:      netSrc,
		NewServerFn: newServerFn,
	})

	// client fac
	newC := func(num int) *arpc.Client {
		name := fmt.Sprintf("%s-%d", t.Name(), num)
		c, err := arpc.NewClient(ctx, serverAddr, name, netSrc.Schema(),
			&arpc.ClientOpts{Parent: mux.Mach})
		if err != nil {
			t.Fatal(err)
		}
		amhelpt.MachDebugEnv(t, c.Mach)

		return c
	}

	// start cmux
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, mux.Mach)
	mux.Listener = listener
	mux.Start(nil)
	amhelpt.WaitForAll(t, "mux Ready", ctx, 2*time.Second,
		mux.Mach.When1(ssM.Ready, nil))

	var clients []*arpc.Client
	var clientsApi []am.Api
	var netMachs []am.Api

	// connect 10 clients to the worker
	for i := range numClients {
		c := newC(i)
		c.Start(nil)
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
	TemplateTestRetryingConnState(t, newTest)
}

func TestPartial(t *testing.T) {
	TemplateTestPartial(t, newTest)
}

func TestPartialInferred(t *testing.T) {
	TemplateTestPartialInferred(t, newTest)
}

func TestPartialNoSchema(t *testing.T) {
	TemplateTestPartialNoSchema(t, newTest)
}

func TestSchemaFilteringSync(t *testing.T) {
	TemplateTestSchemaFilteringSync(t, newTest)
}

func TestShallowSync(t *testing.T) {
	TemplateTestShallowSync(t, newTest)
}

func TestNoSchema(t *testing.T) {
	TemplateTestNoSchema(t, newTest)
}

func TestMutationsSync(t *testing.T) {
	TemplateTestMutationsSync(t, newTest)
}

func TestExport(t *testing.T) {
	TemplateTestExport(t, newTest)
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func newTest(t *testing.T, ctx context.Context, netSrc *am.Machine,
	disposeMeter <-chan struct{}, pushInterval time.Duration, skipStart bool,
	clientOpts *arpc.ClientOpts, serverOpts *arpc.ServerOpts,
) (<-chan int64, *am.Machine, *arpc.Server, *arpc.Client) {
	//

	t.Helper()

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

		go TCPMeter(counterListener, serverAddr, counter, disposeMeter)
		time.Sleep(100 * time.Millisecond)
	}

	// server init
	if serverOpts == nil {
		serverOpts = &arpc.ServerOpts{}
	}
	serverOpts.Parent = netSrc
	s, err := arpc.NewServer(ctx, serverAddr, t.Name(), netSrc, serverOpts)
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
		clientOpts = &arpc.ClientOpts{}
	}
	clientOpts.Parent = netSrc
	schema := netSrc.Schema()
	if clientOpts.NoSchema {
		schema = nil
	}
	c, err := arpc.NewClient(ctx, connAddr, t.Name(), schema, clientOpts)
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
		if os.Getenv(dbg.EnvAmDbgAddr) != "" {
			time.Sleep(100 * time.Millisecond)
		}
	})

	if skipStart {
		return counter, netSrc, s, c
	}

	// server start
	s.Start(nil)
	amhelpt.WaitForAll(t, "RpcReady", ctx, readyTimeout,
		s.Mach.When1(ssS.RpcReady, ctx))

	// client ready
	c.Start(nil)
	amhelpt.WaitForAll(t, "client-server Ready", ctx, readyTimeout,
		c.Mach.When1(ssC.Ready, ctx),
		s.Mach.When1(ssS.Ready, ctx))

	return counter, netSrc, s, c
}

func TestWebSocketAuth(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	netSrc := utils.NewNoRelsNetSrc(t, nil, "")

	listener := utils.RandListener("localhost")
	serverAddr := listener.Addr().String()
	listener.Close()

	// gen valid key
	validKey, _ := arpc.GenerateSecretKey()
	serverOpts := &arpc.ServerOpts{
		WebSocket:        true,
		WebSocketPubKeys: []key.PublicKey{validKey.Public()},
	}
	pubKey := validKey.Public()

	s, err := arpc.NewServer(ctx, serverAddr, t.Name(), netSrc, serverOpts)
	if err != nil {
		t.Fatal(err)
	}
	s.Start(nil)
	// For WebSocket servers, RpcReady is reached when the first client connects.

	clientOpts := &arpc.ClientOpts{
		WebSocket:       "/",
		WebSocketPubKey: &pubKey,
	}
	c, err := arpc.NewClient(
		ctx, serverAddr, t.Name()+"-c1", netSrc.Schema(), clientOpts)
	if err != nil {
		t.Fatal(err)
	}
	c.Start(nil)
	amhelpt.WaitForAll(t, "client Ready", ctx, 3*time.Second,
		c.Mach.When1(ssC.Ready, ctx))
	c.Stop(nil, nil, true)

	invalidKey, _ := arpc.GenerateSecretKey()
	invalidPK := invalidKey.Public()
	clientOptsFail := &arpc.ClientOpts{
		WebSocket:       "/",
		WebSocketPubKey: &invalidPK,
	}
	cFail, err := arpc.NewClient(
		ctx, serverAddr, t.Name()+"-c2", netSrc.Schema(), clientOptsFail)
	if err != nil {
		t.Fatal(err)
	}
	cFail.Start(nil)
	amhelpt.WaitForAll(t, "client Disconnected", ctx, 3*time.Second,
		cFail.Mach.When1(ssC.Disconnected, ctx))

	cFail.Stop(nil, nil, true)
	s.Stop(nil, true)
}

func TestAllowedIds(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	netSrc := utils.NewNoRelsNetSrc(t, nil, "")

	listener := utils.RandListener("localhost")
	serverAddr := listener.Addr().String()
	listener.Close()

	serverOpts := &arpc.ServerOpts{
		AllowedIds: []string{arpc.GetClientId("allowed-client")},
	}

	s, err := arpc.NewServer(ctx, serverAddr, t.Name(), netSrc, serverOpts)
	if err != nil {
		t.Fatal(err)
	}
	s.Start(nil)
	amhelpt.WaitForAll(t, "server RpcReady", ctx, 3*time.Second,
		s.Mach.When1(ssS.RpcReady, ctx))

	// allowed client
	c, err := arpc.NewClient(
		ctx, serverAddr, "allowed-client", netSrc.Schema(), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Start(nil)
	amhelpt.WaitForAll(t, "client Ready", ctx, 3*time.Second,
		c.Mach.When1(ssC.Ready, ctx))
	c.Stop(nil, nil, true)

	// disallowed client
	cFail, err := arpc.NewClient(
		ctx, serverAddr, "disallowed-client", netSrc.Schema(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cFail.Start(nil)
	amhelpt.WaitForAll(t, "client Exception", ctx, 3*time.Second,
		cFail.Mach.WhenErr(ctx))

	cFail.Stop(nil, nil, true)
	s.Stop(nil, true)
}
