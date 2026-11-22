//go:build js

package test

import (
	"context"
	"net"
	"os"
	"sync/atomic"
	"syscall/js"
	"testing"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/nofmt"
	amutils "github.com/pancsta/asyncmachine-go/internal/utils"
)

var wasmPairId atomic.Uint64

// wasmPair dials two WebSockets to the native test bridge, which pairs them
// into a single bidirectional byte stream and returns one net.Conn for each
// side.
func wasmPair(
	t *testing.T, disposeMeter <-chan struct{},
) (net.Conn, net.Conn, <-chan int64) {
	t.Helper()

	base := wsBridgeURL()
	if base == "" {
		t.Fatal("AM_WS_BRIDGE not set and no browser location available")
	}

	id := nofmt.Sprintf("%s-%d", t.Name(), wasmPairId.Add(1))
	url := nofmt.Sprintf("%s?id=%s", base, id)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	timeout := 10 * time.Second
	srv, err := amutils.DialWebSocket(ctx, url, nil, timeout)
	if err != nil {
		t.Fatalf("dial ws pair (server): %s", err)
	}
	cli, err := amutils.DialWebSocket(ctx, url, nil, timeout)
	if err != nil {
		_ = srv.Close()
		t.Fatalf("dial ws pair (client): %s", err)
	}

	if disposeMeter == nil {
		return srv, cli, nil
	}

	// count payload bytes (both directions) exactly once
	cs := &countingConn{Conn: srv}
	cc := &countingConn{Conn: cli}
	counter := make(chan int64, 1)
	go func() {
		<-disposeMeter
		counter <- cs.written.Load() + cc.written.Load()
	}()

	return cs, cc, counter
}

// wsBridgeURL returns the WebSocket pairing bridge URL. The harness page sets
// __AM_WS_BRIDGE; Node-based runs can use AM_WS_BRIDGE instead.
func wsBridgeURL() string {
	v := js.Global().Get("__AM_WS_BRIDGE")
	if v.Type() == js.TypeString && v.String() != "" {
		return v.String()
	}

	if u := os.Getenv("AM_WS_BRIDGE"); u != "" {
		return u
	}

	loc := js.Global().Get("location")
	if loc.Type() == js.TypeObject && !loc.IsUndefined() {
		host := loc.Get("host")
		if host.Type() == js.TypeString && host.String() != "" {
			return "ws://" + host.String() + "/pair"
		}
	}

	return ""
}

type countingConn struct {
	net.Conn
	written atomic.Int64
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.written.Add(int64(n))

	return n, err
}
