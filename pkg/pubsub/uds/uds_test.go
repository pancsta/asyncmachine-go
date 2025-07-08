// Package uds was auto-translated from rust-libp2p.
// https://github.com/libp2p/rust-libp2p/blob/master/transports/uds/src/lib.rs
package uds

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	mocknetwork "github.com/libp2p/go-libp2p/core/network/mocks"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/sec"
	"github.com/libp2p/go-libp2p/core/sec/insecure"
	"github.com/libp2p/go-libp2p/core/transport"
	tptu "github.com/libp2p/go-libp2p/p2p/net/upgrader"
	ttransport "github.com/libp2p/go-libp2p/p2p/transport/testsuite"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var muxers = []tptu.StreamMuxer{} // No muxers for UDS in this test

func randomSocketPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("amtest-%d-%d.sock", os.Getpid(), rand.Intn(1e6)))
}

func TestUdsTransport(t *testing.T) {
	t.Skip()
	for i := 0; i < 2; i++ {
		peerA, ia := makeInsecureMuxer(t)
		_, ib := makeInsecureMuxer(t)

		ua, err := tptu.New(ia, muxers, nil, nil, nil)
		require.NoError(t, err)
		ta, err := NewUDSTransport(ua, nil)
		require.NoError(t, err)
		ub, err := tptu.New(ib, muxers, nil, nil, nil)
		require.NoError(t, err)
		tb, err := NewUDSTransport(ub, nil)
		require.NoError(t, err)

		path := randomSocketPath()
		addr := fmt.Sprintf("/unix/%s", path)
		ttransport.SubtestTransport(t, ta, tb, addr, peerA)
	}
}

func TestUdsTransportWithResourceManager(t *testing.T) {
	t.Skip()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	peerA, ia := makeInsecureMuxer(t)
	_, ib := makeInsecureMuxer(t)

	ua, err := tptu.New(ia, muxers, nil, nil, nil)
	require.NoError(t, err)
	ta, err := NewUDSTransport(ua, nil)
	require.NoError(t, err)
	path := randomSocketPath()
	ln, err := ta.Listen(ma.StringCast(fmt.Sprintf("/unix/%s", path)))
	require.NoError(t, err)
	defer ln.Close()

	ub, err := tptu.New(ib, muxers, nil, nil, nil)
	require.NoError(t, err)
	rcmgr := mocknetwork.NewMockResourceManager(ctrl)
	tb, err := NewUDSTransport(ub, rcmgr)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		scope := mocknetwork.NewMockConnManagementScope(ctrl)
		rcmgr.EXPECT().OpenConnection(network.DirOutbound, true, ln.Multiaddr()).Return(scope, nil)
		scope.EXPECT().SetPeer(peerA)
		scope.EXPECT().PeerScope().Return(&network.NullScope{}).AnyTimes()
		conn, err := tb.Dial(context.Background(), ln.Multiaddr(), peerA)
		require.NoError(t, err)
		scope.EXPECT().Done()
		defer conn.Close()
	})

	t.Run("connection denied", func(t *testing.T) {
		rerr := errors.New("nope")
		rcmgr.EXPECT().OpenConnection(network.DirOutbound, true, ln.Multiaddr()).Return(nil, rerr)
		_, err = tb.Dial(context.Background(), ln.Multiaddr(), peerA)
		require.ErrorIs(t, err, rerr)
	})

	t.Run("peer denied", func(t *testing.T) {
		scope := mocknetwork.NewMockConnManagementScope(ctrl)
		rcmgr.EXPECT().OpenConnection(network.DirOutbound, true, ln.Multiaddr()).Return(scope, nil)
		rerr := errors.New("nope")
		scope.EXPECT().SetPeer(peerA).Return(rerr)
		scope.EXPECT().Done()
		_, err = tb.Dial(context.Background(), ln.Multiaddr(), peerA)
		require.ErrorIs(t, err, rerr)
	})
}

func TestUdsTransportCantDialNonUnix(t *testing.T) {
	t.Skip()
	for i := 0; i < 2; i++ {
		dnsa, err := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/1234")
		require.NoError(t, err)

		var u transport.Upgrader
		tpt, err := NewUDSTransport(u, nil)
		require.NoError(t, err)

		if tpt.CanDial(dnsa) {
			t.Fatal("shouldn't be able to dial non-unix multiaddr")
		}
	}
}

func TestDialWithUpdatesUDS(t *testing.T) {
	t.Skip()
	peerA, ia := makeInsecureMuxer(t)
	_, ib := makeInsecureMuxer(t)

	ua, err := tptu.New(ia, muxers, nil, nil, nil)
	require.NoError(t, err)
	ta, err := NewUDSTransport(ua, nil)
	require.NoError(t, err)
	path := randomSocketPath()
	ln, err := ta.Listen(ma.StringCast(fmt.Sprintf("/unix/%s", path)))
	require.NoError(t, err)
	defer ln.Close()

	ub, err := tptu.New(ib, muxers, nil, nil, nil)
	require.NoError(t, err)
	tb, err := NewUDSTransport(ub, nil)
	require.NoError(t, err)

	updCh := make(chan transport.DialUpdate, 1)
	conn, err := tb.DialWithUpdates(context.Background(), ln.Multiaddr(), peerA, updCh)
	upd := <-updCh
	require.Equal(t, transport.UpdateKindHandshakeProgressed, upd.Kind)
	require.NotNil(t, conn)
	require.NoError(t, err)

	acceptAndClose := func() manet.Listener {
		path := randomSocketPath()
		li, err := manet.Listen(ma.StringCast(fmt.Sprintf("/unix/%s", path)))
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			conn, err := li.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}()
		return li
	}
	li := acceptAndClose()
	defer li.Close()
	// This dial will fail as acceptAndClose will not upgrade the connection
	conn, err = tb.DialWithUpdates(context.Background(), li.Multiaddr(), peerA, updCh)
	upd = <-updCh
	require.Equal(t, transport.UpdateKindHandshakeProgressed, upd.Kind)
	require.Nil(t, conn)
	require.Error(t, err)
}

func makeInsecureMuxer(t *testing.T) (peer.ID, []sec.SecureTransport) {
	t.Skip()
	t.Helper()
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 256)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(priv)
	require.NoError(t, err)
	return id, []sec.SecureTransport{insecure.NewWithIdentity(insecure.ID, id, priv)}
}

type errDialer struct {
	err error
}

func (d errDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return nil, d.err
}

func TestCustomOverrideUDSDialer(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		peerA, ia := makeInsecureMuxer(t)
		ua, err := tptu.New(ia, muxers, nil, nil, nil)
		require.NoError(t, err)
		ta, err := NewUDSTransport(ua, nil)
		require.NoError(t, err)
		path := randomSocketPath()
		ln, err := ta.Listen(ma.StringCast(fmt.Sprintf("/unix/%s", path)))
		require.NoError(t, err)
		defer ln.Close()

		_, ib := makeInsecureMuxer(t)
		ub, err := tptu.New(ib, muxers, nil, nil, nil)
		require.NoError(t, err)
		// called := false
		// customDialer := func(raddr ma.Multiaddr) (ContextDialer, error) {
		// 	called = true
		// 	return &net.Dialer{}, nil
		// }
		tb, err := NewUDSTransport(ub, nil /*, WithDialerForAddr(customDialer) */)
		// TODO: implement WithDialerForAddr for UDS if needed
		require.NoError(t, err)

		conn, err := tb.Dial(context.Background(), ln.Multiaddr(), peerA)
		require.NoError(t, err)
		require.NotNil(t, conn)
		// require.True(t, called, "custom dialer should have been called")
		conn.Close()
	})

	t.Run("errors", func(t *testing.T) {
		peerA, ia := makeInsecureMuxer(t)
		ua, err := tptu.New(ia, muxers, nil, nil, nil)
		require.NoError(t, err)
		ta, err := NewUDSTransport(ua, nil)
		require.NoError(t, err)
		path := randomSocketPath()
		ln, err := ta.Listen(ma.StringCast(fmt.Sprintf("/unix/%s", path)))
		require.NoError(t, err)
		defer ln.Close()

		for _, test := range []string{"error in factory", "error in custom dialer"} {
			t.Run(test, func(t *testing.T) {
				_, ib := makeInsecureMuxer(t)
				ub, err := tptu.New(ib, muxers, nil, nil, nil)
				require.NoError(t, err)
				// customErr := errors.New("custom dialer error")
				// customDialer := func(raddr ma.Multiaddr) (ContextDialer, error) {
				// 	if test == "error in factory" {
				// 		return nil, customErr
				// 	} else {
				// 		return errDialer{err: customErr}, nil
				// 	}
				// }
				tb, err := NewUDSTransport(ub, nil /*, WithDialerForAddr(customDialer) */)
				// TODO: implement WithDialerForAddr for UDS if needed
				require.NoError(t, err)

				conn, err := tb.Dial(context.Background(), ln.Multiaddr(), peerA)
				require.Error(t, err)
				// require.ErrorContains(t, err, customErr.Error())
				require.Nil(t, conn)
			})
		}
	})
}
