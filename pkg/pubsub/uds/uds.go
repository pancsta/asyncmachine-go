// Package uds was auto-translated from rust-libp2p.
// https://github.com/libp2p/rust-libp2p/blob/master/transports/uds/src/lib.rs
package uds

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/transport"
	ma "github.com/multiformats/go-multiaddr"
	mafmt "github.com/multiformats/go-multiaddr-fmt"
	manet "github.com/multiformats/go-multiaddr/net"
)

const defaultConnectTimeout = 5 * time.Second

var log = logging.Logger("uds-tpt")

// UdsTransport is the Unix Domain Socket transport.
type UdsTransport struct {
	upgrader       transport.Upgrader
	connectTimeout time.Duration
	rcmgr          network.ResourceManager
}

var _ transport.Transport = &UdsTransport{}
var _ transport.DialUpdater = &UdsTransport{}

// NewUDSTransport creates a UDS transport object.
func NewUDSTransport(upgrader transport.Upgrader, rcmgr network.ResourceManager, opts ...func(*UdsTransport) error) (*UdsTransport, error) {
	if rcmgr == nil {
		rcmgr = &network.NullResourceManager{}
	}
	tr := &UdsTransport{
		upgrader:       upgrader,
		connectTimeout: defaultConnectTimeout,
		rcmgr:          rcmgr,
	}
	for _, o := range opts {
		if err := o(tr); err != nil {
			return nil, err
		}
	}
	return tr, nil
}

var udsMatcher = mafmt.Base(ma.P_UNIX)

// CanDial returns true if this transport believes it can dial the given multiaddr.
func (t *UdsTransport) CanDial(addr ma.Multiaddr) bool {
	return udsMatcher.Matches(addr)
}

func multiaddrToPath(addr ma.Multiaddr) (string, error) {
	for _, p := range addr.Protocols() {
		if p.Code == ma.P_UNIX {
			path, err := addr.ValueForProtocol(ma.P_UNIX)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(path) {
				return "", errors.New("unix socket path must be absolute")
			}
			return path, nil
		}
	}
	return "", errors.New("not a unix multiaddr")
}

func (t *UdsTransport) maDial(ctx context.Context, raddr ma.Multiaddr) (manet.Conn, error) {
	if t.connectTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.connectTimeout)
		defer cancel()
	}
	path, err := multiaddrToPath(raddr)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	nconn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	return manet.WrapNetConn(nconn)
}

// Dial dials the peer at the remote address.
func (t *UdsTransport) Dial(ctx context.Context, raddr ma.Multiaddr, p peer.ID) (transport.CapableConn, error) {
	return t.DialWithUpdates(ctx, raddr, p, nil)
}

func (t *UdsTransport) DialWithUpdates(ctx context.Context, raddr ma.Multiaddr, p peer.ID, updateChan chan<- transport.DialUpdate) (transport.CapableConn, error) {
	connScope, err := t.rcmgr.OpenConnection(network.DirOutbound, true, raddr)
	if err != nil {
		log.Debugw("resource manager blocked outgoing connection", "peer", p, "addr", raddr, "error", err)
		return nil, err
	}

	c, err := t.dialWithScope(ctx, raddr, p, connScope, updateChan)
	if err != nil {
		connScope.Done()
		return nil, err
	}
	return c, nil
}

func (t *UdsTransport) dialWithScope(ctx context.Context, raddr ma.Multiaddr, p peer.ID, connScope network.ConnManagementScope, updateChan chan<- transport.DialUpdate) (transport.CapableConn, error) {
	if err := connScope.SetPeer(p); err != nil {
		log.Debugw("resource manager blocked outgoing connection for peer", "peer", p, "addr", raddr, "error", err)
		return nil, err
	}
	conn, err := t.maDial(ctx, raddr)
	if err != nil {
		return nil, err
	}
	if updateChan != nil {
		select {
		case updateChan <- transport.DialUpdate{Kind: transport.UpdateKindHandshakeProgressed, Addr: raddr}:
		default:
		}
	}
	direction := network.DirOutbound
	if ok, isClient, _ := network.GetSimultaneousConnect(ctx); ok && !isClient {
		direction = network.DirInbound
	}
	return t.upgrader.Upgrade(ctx, t, conn, direction, p, connScope)
}

// Listen listens on the given multiaddr.
func (t *UdsTransport) Listen(laddr ma.Multiaddr) (transport.Listener, error) {
	path, err := multiaddrToPath(laddr)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove old unix socket: %w", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	mln, err := manet.WrapNetListener(ln)
	if err != nil {
		ln.Close()
		return nil, err
	}
	return t.upgrader.UpgradeListener(t, mln), nil
}

// Protocols returns the list of terminal protocols this transport can dial.
func (t *UdsTransport) Protocols() []int {
	return []int{ma.P_UNIX}
}

// Proxy always returns false for the UDS transport.
func (t *UdsTransport) Proxy() bool {
	return false
}

func (t *UdsTransport) String() string {
	return "UDS"
}
