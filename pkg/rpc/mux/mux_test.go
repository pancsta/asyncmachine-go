package mux

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	"github.com/pancsta/asyncmachine-go/pkg/helpers"
	testing2 "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	"github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
)

func TestMux(t *testing.T) {
	// numClients := 10
	numClients := 3

	// TODO flaky
	//  test_help.go:60: error for cWorkers A: timeout
	//  --- FAIL: TestMux (2.04s)
	if os.Getenv(helpers.EnvAmTestRunner) != "" {
		t.Skip("FLAKY")
		return
	}
	if os.Getenv(machine.EnvAmTestDbgAddr) == "" {
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
	testing2.MachDebugEnv(t, netSrc)
	newServerFn := func(mux *Mux, id string, _ net.Conn) (*rpc.Server, error) {
		s, err := rpc.NewServer(ctx, serverAddr, t.Name()+"-"+id, netSrc, &rpc.ServerOpts{
			Parent: mux.Mach,
		})
		if err != nil {
			t.Fatal(err)
		}
		testing2.MachDebugEnv(t, s.Mach)

		return s, nil
	}
	mux, err := NewMux(ctx, "", t.Name(), nil, &MuxOpts{
		Parent:      netSrc,
		NewServerFn: newServerFn,
	})

	// client fac
	newC := func(num int) *rpc.Client {
		name := fmt.Sprintf("%s-%d", t.Name(), num)
		c, err := rpc.NewClient(ctx, connAddr, name, netSrc.Schema(),
			&rpc.ClientOpts{Parent: mux.Mach})
		if err != nil {
			t.Fatal(err)
		}
		testing2.MachDebugEnv(t, c.Mach)

		return c
	}

	// start cmux
	if err != nil {
		t.Fatal(err)
	}
	testing2.MachDebugEnv(t, mux.Mach)
	mux.Listener = listener
	mux.Start(nil)
	testing2.WaitForAll(t, "mux Ready", ctx, 2*time.Second,
		mux.Mach.When1(ssM.Ready, nil))

	var clients []*rpc.Client
	var clientsApi []machine.Api
	var netMachs []machine.Api

	// connect 10 clients to the worker
	for i := 0; i < numClients; i++ {
		c := newC(i)
		c.Start(nil)
		clients = append(clients, c)
		netMachs = append(netMachs, c.NetMach)
		clientsApi = append(clientsApi, c.Mach)
	}

	// wait for all clients to be ready
	testing2.WaitForAll(t, "group Ready", ctx, 2*time.Second,
		testing2.GroupWhen1(t, clientsApi, ssC.Ready, nil)...)

	for _, w := range netMachs {
		testing2.MachDebugEnv(t, w)
	}

	// start mutating (C adds auto A)
	clients[0].NetMach.Add1(states.C, nil)

	// wait for all clients to get the new state
	testing2.WaitForAll(t, "netMachs A", ctx, 2*time.Second,
		testing2.GroupWhen1(t, netMachs, states.A, nil)...)

	if helpers.IsTelemetry() {
		time.Sleep(1 * time.Second)
	}
}
