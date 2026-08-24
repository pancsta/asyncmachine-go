package iroh

import (
	"context"
	"fmt"
	"net"
	"net/netip"

	"github.com/tmc/go-iroh/endpointticket"
	"github.com/tmc/go-iroh/iroh"
	"github.com/tmc/go-iroh/key"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

const ALPN = "arpc/1"

type ServerOpts struct {
	arpc.ServerOpts
	SecretKey      *key.SecretKey
	AllowedPubKeys []key.PublicKey
	OptsIroh       []iroh.Option
}

type ClientOpts struct {
	arpc.ClientOpts
	Key      key.SecretKey
	OptsIroh []iroh.Option
}

type MuxOpts struct {
	arpc.MuxOpts
	SecretKey      *key.SecretKey
	AllowedPubKeys []key.PublicKey
	OptsIroh       []iroh.Option
}

// NewServer is [arpc.NewServer] decorated with a go-iroh overlay.
func NewServer(
	ctx context.Context, addr string, name string, stateSource am.Api,
	opts *ServerOpts,
) (*arpc.Server, *iroh.Endpoint, error) {
	//

	if opts == nil {
		opts = &ServerOpts{}
	}
	if addr == "" {
		return nil, nil, fmt.Errorf("addr required for iroh server")
	}

	bindOpts := []iroh.Option{}
	bindAddr := iroh.WithBindAddr(netip.MustParseAddrPort(addr))
	bindOpts = append(bindOpts, bindAddr)

	if opts.SecretKey != nil {
		bindOpts = append(bindOpts, iroh.WithSecretKey(*opts.SecretKey))
	}
	bindOpts = append(bindOpts, opts.OptsIroh...)

	ep, err := iroh.Bind(ctx, bindOpts...)
	if err != nil {
		return nil, nil, err
	}

	err = ep.SetALPNs([]string{ALPN})
	if err != nil {
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	lis, err := ep.ListenStreams()
	if err != nil {
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	s, err := arpc.NewServer(ctx, "", name, stateSource, &opts.ServerOpts)
	if err != nil {
		lis.Close()
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	// bind iroh-specific cleanup
	amhelp.DisposeBind(s.Mach, func(_ string, ctx context.Context) {
		_ = ep.Shutdown(ctx)
	})

	var nlis net.Listener = &irohListener{
		StreamListener: lis,
		allowedPubKeys: opts.AllowedPubKeys,
	}
	s.Listener.Store(&nlis)

	return s, ep, nil
}

// NewClient is [arpc.NewClient] decorated with a go-iroh overlay.
func NewClient(
	ctx context.Context, irohAddr string, id string, netSrcSchema am.Schema,
	opts *ClientOpts,
) (*arpc.Client, *iroh.Endpoint, error) {
	//

	if opts == nil {
		opts = &ClientOpts{}
	}
	if irohAddr == "" {
		return nil, nil, fmt.Errorf("irohAddr required for iroh client")
	}

	epAddr, err := endpointticket.Decode(irohAddr)
	if err != nil {
		return nil, nil, err
	}

	bindOpts := []iroh.Option{}
	if !opts.Key.IsZero() {
		bindOpts = append(bindOpts, iroh.WithSecretKey(opts.Key))
	}
	bindOpts = append(bindOpts, opts.OptsIroh...)

	ep, err := iroh.Bind(ctx, bindOpts...)
	if err != nil {
		return nil, nil, err
	}

	c, err := arpc.NewClient(ctx, "", id, netSrcSchema, &opts.ClientOpts)
	if err != nil {
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	conn, err := ep.Connect(ctx, epAddr, ALPN)
	if err != nil {
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	cliConn, err := conn.OpenStreamConn(ctx)
	if err != nil {
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	// Write a dummy byte to force the stream to open on the server side
	if _, err := cliConn.Write([]byte{0}); err != nil {
		cliConn.Close()
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	c.Conn.Store(&cliConn)

	return c, ep, nil
}

// NewMux is [arpc.NewMux] decorated with a go-iroh overlay.
func NewMux(
	ctx context.Context, addr string, name string, stateSource am.Api,
	opts *MuxOpts,
) (*arpc.Mux, *iroh.Endpoint, error) {
	//

	if opts == nil {
		opts = &MuxOpts{}
	}
	if addr == "" {
		return nil, nil, fmt.Errorf("addr required for iroh mux")
	}

	bindOpts := []iroh.Option{}
	bindAddr := iroh.WithBindAddr(netip.MustParseAddrPort(addr))
	bindOpts = append(bindOpts, bindAddr)

	if opts.SecretKey != nil {
		bindOpts = append(bindOpts, iroh.WithSecretKey(*opts.SecretKey))
	}
	bindOpts = append(bindOpts, opts.OptsIroh...)

	ep, err := iroh.Bind(ctx, bindOpts...)
	if err != nil {
		return nil, nil, err
	}

	err = ep.SetALPNs([]string{ALPN})
	if err != nil {
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	lis, err := ep.ListenStreams()
	if err != nil {
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	m, err := arpc.NewMux(ctx, "", name, stateSource, &opts.MuxOpts)
	if err != nil {
		lis.Close()
		_ = ep.Shutdown(ctx)
		return nil, nil, err
	}

	m.Listener = &irohListener{
		StreamListener: lis,
		allowedPubKeys: opts.AllowedPubKeys,
	}

	return m, ep, nil
}

type irohListener struct {
	*iroh.StreamListener
	allowedPubKeys []key.PublicKey
}

func (l *irohListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.StreamListener.Accept()
		if err != nil {
			return nil, err
		}

		if len(l.allowedPubKeys) > 0 {
			var remoteID key.EndpointID
			allowed := false
			if idConn, ok := conn.(interface{ RemoteID() key.EndpointID }); ok {
				remoteID = idConn.RemoteID()
				for _, k := range l.allowedPubKeys {
					if key.EndpointID(k) == remoteID {
						allowed = true
						break
					}
				}
			}
			if !allowed {
				conn.Close()
				continue
			}
		}

		// Read the dummy byte
		dummy := make([]byte, 1)
		if _, err := conn.Read(dummy); err != nil {
			conn.Close()
			return nil, err
		}
		return conn, nil
	}
}
