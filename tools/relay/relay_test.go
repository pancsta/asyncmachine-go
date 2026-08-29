package relay

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/go-iroh/key"
	"gopkg.in/yaml.v3"

	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"

	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssC "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssS "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/tools/relay/types"
	amrelayt "github.com/pancsta/asyncmachine-go/tools/relay/types"
)

func init() {
	if os.Getenv(am.EnvAmTestRunner) != "" {
		return
	}

	_ = godotenv.Load()

	if os.Getenv(am.EnvAmTestDebug) != "" {
		amhelp.EnableDebugging(true)
	}
}

func TestTunnelMatchers(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	validPK, invalidPK, netSrc, relayAddr := testInit(t)
	clientChan := make(chan *arpc.Client, 1)

	// Setup Relay
	relay, err := New(ctx, amrelayt.CliArgs{
		Name:   "relay-test",
		Debug:  os.Getenv(am.EnvAmTestDebug) != "",
		Parent: netSrc,
		Wasm: &amrelayt.ArgsWasm{
			ListenAddr: relayAddr,
			TunnelMatchers: []amrelayt.TunnelMatcher{{
				Id: regexp.MustCompile("^browser-bar-"),
				NewClient: func(
					ctx context.Context, id string, conn net.Conn,
				) (*arpc.Client, error) {
					bar, err := arpc.NewClient(
						ctx, "", id, netSrc.Schema(), &arpc.ClientOpts{},
					)
					if err != nil {
						return nil, err
					}
					bar.Conn.Store(&conn)
					bar.Start(nil)
					select {
					case clientChan <- bar:
					default:
					}
					return bar, nil
				},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	relay.AllowedPubKeys.Store(&[]string{validPK.String()})
	relay.Start(nil)
	<-relay.Mach.When1(ssR.HttpReady, nil)

	// test

	// Authorized server tunneling to /listen/ (browser-bar tunneling to relay)
	barTcpListener := utils.RandListener("localhost")
	barTcpAddr := barTcpListener.Addr().String()
	barTcpListener.Close()

	serverOpts := &arpc.ServerOpts{
		WebSocketTunnel:       arpc.WsListenPath("browser-bar-1", barTcpAddr),
		WebSocketTunnelPubKey: validPK.String(),
	}
	serverValid, err := arpc.NewServer(
		ctx, relayAddr, "browser-bar-1", netSrc, serverOpts,
	)
	if err != nil {
		t.Fatal(err)
	}
	serverValid.Start(nil)
	// For TCP Server, RpcReady indicates the listening has started or tunnel
	// is established
	amhelpt.WaitForAll(t, "serverValid RpcReady", ctx, 3*time.Second,
		serverValid.Mach.When1(ssS.ServerStates.RpcReady, ctx))

	// Verify that the relayed client reaches Ready state
	select {
	case barClient := <-clientChan:
		amhelpt.WaitForAll(t, "relayed client Ready", ctx, 3*time.Second,
			barClient.Mach.When1(ssC.ClientStates.Ready, ctx))
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for relayed client")
	}

	// Unauthorized server tunneling to /listen/
	serverOptsFail := &arpc.ServerOpts{
		WebSocketTunnel:       arpc.WsListenPath("browser-bar-2", barTcpAddr),
		WebSocketTunnelPubKey: invalidPK.String(), // invalid
	}
	serverFail, err := arpc.NewServer(
		ctx, relayAddr, "browser-bar-2", netSrc, serverOptsFail,
	)
	if err != nil {
		t.Fatal(err)
	}
	serverFail.Start(nil)
	amhelpt.WaitForAll(t, "serverFail Exception", ctx, 3*time.Second,
		serverFail.Mach.WhenErr(ctx))
}

func TestDialMatchers(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	validPK, invalidPK, netSrc, relayAddr := testInit(t)

	// Mock server via Mux
	fooTcpAddr, mux := newMux(t, ctx, netSrc)

	// Setup Relay
	relay, err := New(ctx, amrelayt.CliArgs{
		Name:   "relay-test",
		Debug:  os.Getenv(am.EnvAmTestDebug) != "",
		Parent: netSrc,
		Wasm: &amrelayt.ArgsWasm{
			ListenAddr: relayAddr,
			DialMatchers: []amrelayt.DialMatcher{{
				Id: regexp.MustCompile("^browser-foo-"),
				NewServer: func(
					ctx context.Context, id string, conn net.Conn,
				) (*arpc.Server, error) {
					return mux.NewServer(nil, id, conn)
				},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	relay.AllowedPubKeys.Store(&[]string{validPK.String()})
	relay.Start(nil)
	<-relay.Mach.When1(ssR.HttpReady, nil)

	// test

	// Authorized client dialing to /dial/ (browser-foo dialing to
	// server-foo via relay)
	clientOpts := &arpc.ClientOpts{
		WebSocket:       arpc.WsDialPath("browser-foo-1", fooTcpAddr),
		WebSocketPubKey: validPK.String(),
	}
	clientValid, err := arpc.NewClient(
		ctx, relayAddr, "browser-foo-1", netSrc.Schema(), clientOpts,
	)
	if err != nil {
		t.Fatal(err)
	}
	clientValid.Start(nil)
	amhelpt.WaitForAll(t, "clientValid Ready", ctx, 3*time.Second,
		clientValid.Mach.When1(ssC.ClientStates.Ready, ctx))

	// Unauthorized client dialing to /dial/
	clientOptsFail := &arpc.ClientOpts{
		WebSocket:       arpc.WsDialPath("browser-foo-2", fooTcpAddr),
		WebSocketPubKey: invalidPK.String(), // invalid
	}
	clientFail, err := arpc.NewClient(
		ctx, relayAddr, "browser-foo-2", netSrc.Schema(), clientOptsFail,
	)
	if err != nil {
		t.Fatal(err)
	}
	clientFail.Start(nil)
	amhelpt.WaitForAll(t, "clientFail Exception", ctx, 3*time.Second,
		clientFail.Mach.WhenErr(ctx))
}

func TestOutputMach(t *testing.T) {
	tempDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r, err := New(ctx, types.CliArgs{
		RotateDbg: &types.ArgsRotateDbg{
			Dir:        tempDir,
			OutputMach: true,
		},
	})
	require.NoError(t, err)

	stateNames := am.S{"Foo", "Bar", "Baz"}
	client := &server.Client{
		Id: "relay-mach-1",
		Exportable: &server.Exportable{
			MsgStruct: &dbg.DbgMsgStruct{
				ID:          "relay-mach-1",
				StatesIndex: stateNames,
			},
			MsgTxs: []*dbg.DbgMsgTx{
				{
					MachineID: "relay-mach-1",
					ID:        "tx-0",
					Clocks:    am.Time{1, 0, 0},
					QueueTick: 5,
				},
				{
					MachineID: "relay-mach-1",
					ID:        "tx-1",
					Clocks:    am.Time{1, 3, 0},
					QueueTick: 12,
				},
			},
		},
	}
	r.dbgClients[client.Id] = client

	err = r.hExportMach(client)
	require.NoError(t, err)

	machFile := filepath.Join(tempDir, "machs", "relay-mach-1.yml")
	content, err := os.ReadFile(machFile)
	require.NoError(t, err)
	assert.NotEmpty(t, content)

	var ser am.Serialized
	err = yaml.Unmarshal(content, &ser)
	require.NoError(t, err)

	assert.Equal(t, "relay-mach-1", ser.ID)
	assert.Equal(t, stateNames, ser.StateNames)
	assert.Equal(t, am.Time{1, 3, 0}, ser.Time)
	assert.Equal(t, uint64(12), ser.QueueTick)
	assert.Equal(t, uint32(1), ser.MachineTick)
}

func TestOutputClients(t *testing.T) {
	tempDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r, err := New(ctx, types.CliArgs{
		RotateDbg: &types.ArgsRotateDbg{
			Dir:           tempDir,
			OutputClients: true,
		},
	})
	require.NoError(t, err)

	client1 := &server.Client{
		Id: "alpha-mach",
		Exportable: &server.Exportable{
			MsgStruct: &dbg.DbgMsgStruct{
				ID:          "alpha-mach",
				StatesIndex: am.S{ssam.BasicStates.Ready, "CustomState"},
				Tags:        []string{"tag-a", "tag-b"},
			},
			MsgTxs: []*dbg.DbgMsgTx{
				{
					MachineID: "alpha-mach",
					ID:        "tx-0",
					Clocks:    am.Time{1, 0},
				},
			},
		},
	}
	client1.Connected.Store(true)

	client2 := &server.Client{
		Id: "beta-mach",
		Exportable: &server.Exportable{
			MsgStruct: &dbg.DbgMsgStruct{
				ID:          "beta-mach",
				StatesIndex: am.S{"Foo"},
			},
		},
	}
	client2.Connected.Store(false)

	r.dbgClients[client1.Id] = client1
	r.dbgClients[client2.Id] = client2

	err = r.hExportClients()
	require.NoError(t, err)

	clientsFile := filepath.Join(tempDir, "clients.txt")
	content, err := os.ReadFile(clientsFile)
	require.NoError(t, err)

	text := string(content)
	assert.Contains(t, text, "alpha-mach R|1")
	assert.Contains(t, text, "#tag-a")
	assert.Contains(t, text, "#tag-b")
	assert.Contains(t, text, "beta-mach  |0 (disconnected)")

	lines := strings.Split(strings.TrimSpace(text), "\n")
	assert.True(t, len(lines) >= 3)
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func newMux(
	t *testing.T, ctx context.Context, netSrc *am.Machine,
) (string, *arpc.Mux) {
	fooTcpListener := utils.RandListener("localhost")
	fooTcpAddr := fooTcpListener.Addr().String()
	fooTcpListener.Close()

	mux, err := arpc.NewMux(
		ctx, fooTcpAddr, "server-foo", netSrc, &arpc.MuxOpts{},
	)
	if err != nil {
		t.Fatal(err)
	}
	mux.Start(nil)
	return fooTcpAddr, mux
}

func testInit(
	t *testing.T,
) (key.PublicKey, key.PublicKey, *am.Machine, string) {
	validKey, _ := arpc.GenerateSecretKey()
	invalidKey, _ := arpc.GenerateSecretKey()
	validPK := validKey.Public()
	invalidPK := invalidKey.Public()

	netSrc := utils.NewNoRelsNetSrc(t, nil, "")

	// Relay
	listener := utils.RandListener("localhost")
	relayAddr := listener.Addr().String()
	listener.Close()
	return validPK, invalidPK, netSrc, relayAddr
}
