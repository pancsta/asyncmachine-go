package iroh

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tmc/go-iroh/endpointticket"
	"github.com/tmc/go-iroh/iroh"
	"github.com/tmc/go-iroh/key"
	"github.com/tmc/go-iroh/netaddr"

	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/test"
)

var readyTimeout = 3 * time.Second

func TestBasic(t *testing.T) {
	test.TemplateTestBasic(t, newTest)
}

func TestTypeSafe(t *testing.T) {
	test.TemplateTestTypeSafe(t, newTest)
}

func TestWaiting(t *testing.T) {
	test.TemplateTestWaiting(t, newTest)
}

func TestAddMany(t *testing.T) {
	test.TemplateTestAddMany(t, newTest)
}

func TestAddManyNoSync(t *testing.T) {
	test.TemplateTestAddManyNoSync(t, newTest)
}

func TestAddManyInstantClock(t *testing.T) {
	test.TemplateTestAddManyInstantClock(t, newTest)
}

func TestManyStates(t *testing.T) {
	test.TemplateTestManyStates(t, newTest)
}

func TestHighInstantClocks(t *testing.T) {
	test.TemplateTestHighInstantClocks(t, newTest)
}

func TestClockPush(t *testing.T) {
	test.TemplateTestClockPush(t, newTest)
}

func TestRetryCall(t *testing.T) {
	test.TemplateTestRetryCall(t, newTest)
}

func TestRetryConn(t *testing.T) {
	t.Skip("Iroh uses tickets, cannot reconnect with same addr")
	test.TemplateTestRetryConn(t, newTest)
}

func TestRetryErrNetworkTimeout(t *testing.T) {
	test.TemplateTestRetryErrNetworkTimeout(t, newTest)
}

func TestRetryClosedListener(t *testing.T) {
	t.Skip("Iroh uses tickets, closing listener causes dial timeout")
	test.TemplateTestRetryClosedListener(t, newTest)
}

func TestPayload(t *testing.T) {
	test.TemplateTestPayload(t, newTest)
}

func TestRetryingConnState(t *testing.T) {
	test.TemplateTestRetryingConnState(t, newTest)
}

func TestPartial(t *testing.T) {
	test.TemplateTestPartial(t, newTest)
}

func TestPartialInferred(t *testing.T) {
	test.TemplateTestPartialInferred(t, newTest)
}

func TestPartialNoSchema(t *testing.T) {
	test.TemplateTestPartialNoSchema(t, newTest)
}

func TestSchemaFilteringSync(t *testing.T) {
	test.TemplateTestSchemaFilteringSync(t, newTest)
}

func TestShallowSync(t *testing.T) {
	test.TemplateTestShallowSync(t, newTest)
}

func TestNoSchema(t *testing.T) {
	test.TemplateTestNoSchema(t, newTest)
}

func TestMutationsSync(t *testing.T) {
	// "TestMutationsSync: Manual invocations of s.PushClient() experience
	//   timeouts due to Iroh stream latency/timing quirks."
	t.Skip("s.PushClient() timeout in Iroh due to stream handling")
	test.TemplateTestMutationsSync(t, newTest)
}

func TestExport(t *testing.T) {
	test.TemplateTestExport(t, newTest)
}

func TestAllowedPubKeysServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	allowedKey, err := key.GenerateSecretKey()
	assert.NoError(t, err)
	disallowedKey, err := key.GenerateSecretKey()
	assert.NoError(t, err)

	netSrc := utils.NewRelsNetSrc(t, nil)
	srvKey, _ := key.GenerateSecretKey()
	sOpts := &ServerOpts{
		ServerOpts:     arpc.ServerOpts{Parent: netSrc},
		SecretKey:      &srvKey,
		AllowedPubKeys: []key.PublicKey{allowedKey.Public()},
	}

	s, srvEp, err := NewServer(ctx, "127.0.0.1:0", t.Name(), netSrc, sOpts)
	assert.NoError(t, err)
	defer func() { _ = srvEp.Shutdown(ctx) }()
	defer s.Mach.Dispose()
	s.Start(nil)

	addr := netaddr.NewEndpointAddr(srvEp.ID()).WithIP(srvEp.LocalAddr())
	ticket := endpointticket.Encode(addr)

	// Disallowed Client
	cOpts1 := &ClientOpts{
		Key: disallowedKey,
	}
	c1, cliEp1, err := NewClient(
		ctx, ticket, "disallowed", netSrc.Schema(), cOpts1)
	assert.NoError(t, err)
	defer func() { _ = cliEp1.Shutdown(ctx) }()
	defer c1.Mach.Dispose()

	c1.Start(nil)
	amhelp.Wait(ctx, 500*time.Millisecond)
	// Should fail (not reach Ready)
	assert.False(t, c1.Mach.Is1(ssrpc.ClientStates.Ready))
	c1.Mach.Dispose()

	// Allowed Client
	cOpts2 := &ClientOpts{
		Key: allowedKey,
	}
	c2, cliEp2, err := NewClient(
		ctx, ticket, "allowed", netSrc.Schema(), cOpts2)
	assert.NoError(t, err)
	defer func() { _ = cliEp2.Shutdown(ctx) }()
	defer c2.Mach.Dispose()

	c2.Start(nil)
	amhelpt.WaitForAll(t, "allowed client", ctx, 5*time.Second,
		c2.Mach.When1(ssrpc.ClientStates.Ready, ctx))
	assert.True(t, c2.Mach.Is1(ssrpc.ClientStates.Ready))
}

func TestAllowedPubKeysMux(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	allowedKey, err := key.GenerateSecretKey()
	assert.NoError(t, err)
	disallowedKey, err := key.GenerateSecretKey()
	assert.NoError(t, err)

	netSrc := utils.NewRelsNetSrc(t, nil)
	srvKey, _ := key.GenerateSecretKey()
	mOpts := &MuxOpts{
		MuxOpts:        arpc.MuxOpts{Parent: netSrc},
		SecretKey:      &srvKey,
		AllowedPubKeys: []key.PublicKey{allowedKey.Public()},
	}

	m, srvEp, err := NewMux(ctx, "127.0.0.1:0", t.Name(), netSrc, mOpts)
	assert.NoError(t, err)
	defer func() { _ = srvEp.Shutdown(ctx) }()
	defer m.Mach.Dispose()
	m.Start(nil)

	addr := netaddr.NewEndpointAddr(srvEp.ID()).WithIP(srvEp.LocalAddr())
	ticket := endpointticket.Encode(addr)

	// Disallowed Client
	cOpts1 := &ClientOpts{
		Key: disallowedKey,
	}
	c1, cliEp1, err := NewClient(
		ctx, ticket, "disallowed", netSrc.Schema(), cOpts1)
	assert.NoError(t, err)
	defer func() { _ = cliEp1.Shutdown(ctx) }()
	defer c1.Mach.Dispose()

	c1.Start(nil)
	amhelp.Wait(ctx, 500*time.Millisecond)
	// Should fail (not reach Ready)
	assert.False(t, c1.Mach.Is1(ssrpc.ClientStates.Ready))
	c1.Mach.Dispose()

	// Allowed Client
	cOpts2 := &ClientOpts{
		Key: allowedKey,
	}
	c2, cliEp2, err := NewClient(
		ctx, ticket, "allowed", netSrc.Schema(), cOpts2)
	assert.NoError(t, err)
	defer func() { _ = cliEp2.Shutdown(ctx) }()
	defer c2.Mach.Dispose()

	c2.Start(nil)
	amhelpt.WaitForAll(t, "allowed client", ctx, 5*time.Second,
		c2.Mach.When1(ssrpc.ClientStates.Ready, ctx))
	assert.True(t, c2.Mach.Is1(ssrpc.ClientStates.Ready))
}

// ///// ///// /////

// ///// IROH TESTS

// ///// ///// /////

func newNetSrc(t *testing.T, ctx context.Context) *am.Machine {
	t.Helper()

	schema := ssrpc.StateSourceSchema.Merge(am.Schema{
		"Foo": {},
		"Bar": {Require: am.S{"Foo"}},
	})
	names := am.SAdd(ssrpc.StateSourceStates.Names(), am.S{"Foo", "Bar"})
	netSrc := am.New(ctx, schema, &am.Opts{Id: "ns-" + t.Name()})
	err := netSrc.VerifyStates(names)
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, netSrc)

	return netSrc
}

// TestClientMutation tests that a mutation from the client propagates to the
// server's source machine.
func TestIroh(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	netSrc := newNetSrc(t, ctx)
	_, _, s, c := newTest(t, ctx, netSrc, nil, 0, false, nil, nil)

	assert.False(t, netSrc.Is1("Foo"), "Foo set on server source")
	assert.False(t, c.NetMach.Is1("Foo"), "Foo set on client net machine")

	// mutate from the client
	c.NetMach.Add1("Foo", nil)

	// assert
	assert.True(t, s.Mach.Is1(ssrpc.ServerStates.Ready), "Server ready")
	assert.True(t, c.Mach.Is1(ssrpc.ClientStates.Ready), "Client ready")

	assert.True(t, s.Mach.Not1(am.StateException), "No server errors")
	assert.True(t, c.Mach.Not1(am.StateException), "No client errors")

	assert.True(t, netSrc.Is1("Foo"), "Foo set on server source")
	assert.True(t, c.NetMach.Is1("Foo"), "Foo set on client net machine")

	// shut down
	c.Mach.Remove1(ssrpc.ClientStates.Start, nil)
	s.Mach.Remove1(ssrpc.ServerStates.Start, nil)
}

func TestIrohMux(t *testing.T) {
	numClients := 3
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}
	ctx := context.Background()

	// init source & mux
	netSrc := newNetSrc(t, ctx)
	amhelpt.MachDebugEnv(t, netSrc)

	srvKey, err := key.GenerateSecretKey()
	if err != nil {
		t.Fatal(err)
	}

	newServerFn := func(
		mux *arpc.Mux, id string, conn net.Conn,
	) (*arpc.Server, error) {
		s, err := arpc.NewServer(ctx, "", t.Name()+"-"+id, netSrc, &arpc.ServerOpts{
			Parent: mux.Mach,
		})
		if err != nil {
			t.Fatal(err)
		}
		s.Conn = conn
		amhelpt.MachDebugEnv(t, s.Mach)

		return s, nil
	}

	muxOpts := &MuxOpts{
		MuxOpts: arpc.MuxOpts{
			Parent:      netSrc,
			NewServerFn: newServerFn,
		},
		SecretKey: &srvKey,
	}
	mux, muxEp, err := NewMux(ctx, "127.0.0.1:0", t.Name(), nil, muxOpts)
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, mux.Mach)
	t.Cleanup(func() { _ = muxEp.Shutdown(ctx) })

	mux.Start(nil)
	amhelpt.WaitForAll(t, "mux Ready", ctx, readyTimeout,
		mux.Mach.When1(ssrpc.MuxStates.Ready, nil))

	addr := netaddr.NewEndpointAddr(muxEp.ID()).WithIP(muxEp.LocalAddr())
	ticket := endpointticket.Encode(addr)

	// client fac
	var cliEps []*iroh.Endpoint
	newC := func(num int) *arpc.Client {
		name := fmt.Sprintf("%s-%d", t.Name(), num)
		cOpts := &ClientOpts{
			ClientOpts: arpc.ClientOpts{Parent: mux.Mach},
		}
		c, cliEp, err := NewClient(ctx, ticket, name, netSrc.Schema(), cOpts)
		if err != nil {
			t.Fatal(err)
		}
		cliEps = append(cliEps, cliEp)
		amhelpt.MachDebugEnv(t, c.Mach)
		return c
	}

	var clients []*arpc.Client
	var clientsApi []am.Api
	var netMachs []am.Api

	// connect clients to the worker
	for i := range numClients {
		c := newC(i)
		c.Start(nil)
		clients = append(clients, c)
		netMachs = append(netMachs, c.NetMach)
		clientsApi = append(clientsApi, c.Mach)
	}
	t.Cleanup(func() {
		for _, ep := range cliEps {
			_ = ep.Shutdown(ctx)
		}
	})

	// wait for all clients to be ready
	amhelpt.WaitForAll(t, "group Ready", ctx, readyTimeout,
		amhelpt.GroupWhen1(t, clientsApi, ssrpc.ClientStates.Ready, nil)...)

	// start mutating (Foo adds Bar)
	clients[0].NetMach.Add1("Foo", nil)

	// wait for all clients to get the new state
	amhelpt.WaitForAll(t, "netMachs Foo", ctx, readyTimeout,
		amhelpt.GroupWhen1(t, netMachs, "Foo", nil)...)
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// newTest creates an aRPC server and client connected via iroh net.Conn
// pair instead of TCP.
func newTest(
	t *testing.T, ctx context.Context, netSrc *am.Machine,
	disposeMeter <-chan struct{}, pushInterval time.Duration, skipStart bool,
	clientOpts *arpc.ClientOpts, serverOpts *arpc.ServerOpts,
) (<-chan int64, *am.Machine, *arpc.Server, *arpc.Client) {
	//

	t.Helper()

	srvKey, err := key.GenerateSecretKey()
	if err != nil {
		t.Fatal(err)
	}

	// worker init
	if netSrc == nil {
		netSrc = utils.NewRelsNetSrc(t, nil)
	}
	amhelpt.MachDebugEnv(t, netSrc)

	// traffic counter init
	var counter chan int64
	var proxyClose func()
	if disposeMeter != nil {
		counter = make(chan int64, 1)
	}

	// server init
	if serverOpts == nil {
		serverOpts = &arpc.ServerOpts{}
	}
	serverOpts.Parent = netSrc
	sOpts := &ServerOpts{
		ServerOpts: *serverOpts,
		SecretKey:  &srvKey,
	}
	s, srvEp, err := NewServer(ctx, "127.0.0.1:0", t.Name(), netSrc, sOpts)
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, s.Mach)
	if pushInterval > 0 {
		s.PushInterval.Store(&pushInterval)
	}
	t.Cleanup(func() { _ = srvEp.Shutdown(ctx) })

	// let it settle
	time.Sleep(10 * time.Millisecond)

	serverAddr := srvEp.LocalAddr()
	addr := netaddr.NewEndpointAddr(srvEp.ID()).WithIP(serverAddr)

	if disposeMeter != nil {
		addr, proxyClose, counter = UDPMeter(
			addr, srvEp, serverAddr, ctx, proxyClose, disposeMeter, counter)
	}

	ticket := endpointticket.Encode(addr)

	// client init
	if clientOpts == nil {
		clientOpts = &arpc.ClientOpts{}
	}
	clientOpts.Parent = netSrc
	cOpts := &ClientOpts{
		ClientOpts: *clientOpts,
	}

	schema := netSrc.Schema()
	if clientOpts.NoSchema {
		schema = nil
	}

	c, cliEp, err := NewClient(
		ctx, ticket, t.Name(), schema, cOpts)
	if err != nil {
		t.Fatal(err)
	}
	amhelpt.MachDebugEnv(t, c.Mach)
	if clientOpts.Consumer != nil {
		amhelpt.MachDebugEnv(t, clientOpts.Consumer)
	}
	t.Cleanup(func() { _ = cliEp.Shutdown(ctx) })

	// tear down
	t.Cleanup(func() {
		<-s.Mach.WhenDisposed()
		<-c.Mach.WhenDisposed()
		if proxyClose != nil {
			proxyClose()
		}
	})

	if skipStart {
		return counter, netSrc, s, c
	}

	// server start
	s.Start(nil)
	amhelpt.WaitForAll(t, "RpcReady", ctx, readyTimeout,
		s.Mach.When1(ssrpc.ServerStates.RpcReady, ctx))

	// client start
	c.Start(nil)
	amhelpt.WaitForAll(t, "client-server Ready", ctx, readyTimeout,
		c.Mach.When1(ssrpc.ClientStates.Ready, ctx),
		s.Mach.When1(ssrpc.ServerStates.Ready, ctx))

	return counter, netSrc, s, c
}

func UDPMeter(
	addr netaddr.EndpointAddr, srvEp *iroh.Endpoint, serverAddr netip.AddrPort,
	ctx context.Context, proxyClose func(), disposeMeter <-chan struct{},
	counter chan int64,
) (netaddr.EndpointAddr, func(), chan int64) {
	//

	proxyAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	pConn, _ := net.ListenUDP("udp", proxyAddr)
	metered := amhelpt.WrapUDP(pConn)

	pAddr := netip.MustParseAddrPort(pConn.LocalAddr().String())
	addr = netaddr.NewEndpointAddr(srvEp.ID()).WithIP(pAddr)

	srvUDPAddr := net.UDPAddrFromAddrPort(serverAddr)
	var clientAddr net.Addr

	proxyCtx, proxyCancel := context.WithCancel(ctx)
	proxyClose = func() {
		proxyCancel()
		pConn.Close()
	}

	go func() {
		buf := make([]byte, 65535)
		for {
			select {
			case <-proxyCtx.Done():
				return
			default:
			}

			// Using deadline to allow context cancellation checks
			_ = pConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, cAddr, err := metered.ReadFrom(buf)
			if err != nil {
				continue
			}

			if cAddr.String() == srvUDPAddr.String() {
				// From Server -> To Client
				if clientAddr != nil {
					_, _ = metered.WriteTo(buf[:n], clientAddr)
				}
			} else {
				// From Client -> To Server
				clientAddr = cAddr
				_, _ = metered.WriteTo(buf[:n], srvUDPAddr)
			}
		}
	}()

	go func() {
		<-disposeMeter
		total := metered.BytesIn() + metered.BytesOut()
		counter <- int64(total)
		close(counter)
	}()
	return addr, proxyClose, counter
}
