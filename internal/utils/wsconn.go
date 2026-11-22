//go:build !js

package utils

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// DialWebSocket dials a WebSocket endpoint and returns a net.Conn carrying
// binary messages as a byte stream. Useful for tunneling stream protocols.
func DialWebSocket(
	ctx context.Context, urlStr string, protocols []string, timeout time.Duration,
) (net.Conn, error) {
	// ctx is honored by the dialer
	d := ws.Dialer{
		Protocols: protocols,
		Timeout:   timeout,
	}
	conn, br, _, err := d.Dial(ctx, urlStr)
	if err != nil {
		return nil, err
	}

	return newWSConn(conn, br, ws.StateClientSide), nil
}

// NewServerWSConn wraps an already upgraded server-side WebSocket into
// a net.Conn.
func NewServerWSConn(
	conn net.Conn, rw *bufio.ReadWriter,
) (net.Conn, error) {
	if conn == nil || rw == nil {
		return nil, errors.New("ws conn required")
	}

	return newWSConn(conn, rw.Reader, ws.StateServerSide), nil
}

// wsConn adapts a WebSocket to net.Conn. Every Write becomes a single binary
// message; reads are exposed as a continuous byte stream.
type wsConn struct {
	conn net.Conn
	rd   *bufio.Reader
	side ws.State

	readMu  sync.Mutex
	writeMu sync.Mutex
	buf     []byte

	closeMu sync.Mutex
	closed  bool
}

var _ net.Conn = (*wsConn)(nil)

func newWSConn(conn net.Conn, rd *bufio.Reader, side ws.State) *wsConn {
	if rd == nil {
		rd = bufio.NewReader(conn)
	}

	return &wsConn{
		conn: conn,
		rd:   rd,
		side: side,
	}
}

func (c *wsConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for len(c.buf) == 0 {
		var (
			data []byte
			err  error
		)
		rw := wsRW{r: c.rd, w: c.conn}
		if c.side == ws.StateServerSide {
			data, _, err = wsutil.ReadClientData(rw)
		} else {
			data, _, err = wsutil.ReadServerData(rw)
		}
		if err != nil {
			return 0, mapWSErr(err)
		}
		c.buf = data
	}

	n := copy(p, c.buf)
	c.buf = c.buf[n:]

	return n, nil
}

func (c *wsConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	var err error
	if c.side == ws.StateServerSide {
		err = wsutil.WriteServerMessage(c.conn, ws.OpBinary, p)
	} else {
		err = wsutil.WriteClientMessage(c.conn, ws.OpBinary, p)
	}
	if err != nil {
		return 0, mapWSErr(err)
	}

	return len(p), nil
}

func (c *wsConn) Close() error {
	c.closeMu.Lock()
	wasClosed := c.closed
	c.closed = true
	c.closeMu.Unlock()

	if !wasClosed {
		// best-effort close frame
		f := ws.NewCloseFrame(ws.NewCloseFrameBody(
			ws.StatusNormalClosure, ""))
		if c.side == ws.StateClientSide {
			f = ws.MaskFrameInPlace(f)
		}
		_ = ws.WriteFrame(c.conn, f)
	}

	return c.conn.Close()
}

func (c *wsConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *wsConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *wsConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *wsConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *wsConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// wsRW combines a buffered reader with a plain writer. Control frames are
// written directly to the underlying conn, so they are not stuck in a buffer.
type wsRW struct {
	r io.Reader
	w io.Writer
}

func (rw wsRW) Read(p []byte) (int, error)  { return rw.r.Read(p) }
func (rw wsRW) Write(p []byte) (int, error) { return rw.w.Write(p) }

func mapWSErr(err error) error {
	var closed wsutil.ClosedError
	if errors.As(err, &closed) {
		return io.EOF
	}

	return err
}
