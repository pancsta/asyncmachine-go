//go:build js

package utils

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall/js"
	"time"
)

// DialWebSocket opens a browser WebSocket and exposes it as a net.Conn.
// protocols are sent as Sec-WebSocket-Protocol values (browsers can not set
// custom headers).
func DialWebSocket(
	ctx context.Context, urlStr string, protocols []string,
	timeout time.Duration,
) (net.Conn, error) {
	var ws js.Value
	if len(protocols) == 0 {
		ws = js.Global().Get("WebSocket").New(urlStr)
	} else {
		list := js.Global().Get("Array").New(len(protocols))
		for i, p := range protocols {
			list.SetIndex(i, js.ValueOf(p))
		}
		ws = js.Global().Get("WebSocket").New(urlStr, list)
	}
	ws.Set("binaryType", "arraybuffer")

	if ctx == nil {
		ctx = context.Background()
	}

	c := &wsConn{
		ws:      ws,
		ctx:     ctx,
		sig:     make(chan struct{}, 1),
		opened:  make(chan struct{}),
		openErr: make(chan error, 1),
	}
	c.onOpen = js.FuncOf(func(js.Value, []js.Value) any {
		c.settle(nil)
		return nil
	})
	c.onError = js.FuncOf(func(js.Value, []js.Value) any {
		err := errors.New("websocket error")
		c.fail(err)
		c.settle(err)
		return nil
	})
	c.onClose = js.FuncOf(func(js.Value, []js.Value) any {
		c.fail(io.EOF)
		c.settle(errWSClosed)
		return nil
	})
	c.onMessage = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		data := args[0].Get("data")
		if data.IsNull() || data.IsUndefined() {
			return nil
		}

		u8 := js.Global().Get("Uint8Array").New(data)
		b := make([]byte, u8.Get("length").Int())
		js.CopyBytesToGo(b, u8)
		c.push(b)

		return nil
	})
	ws.Set("onopen", c.onOpen)
	ws.Set("onerror", c.onError)
	ws.Set("onclose", c.onClose)
	ws.Set("onmessage", c.onMessage)

	var timeoutCh <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case <-c.opened:
		return c, nil
	case err := <-c.openErr:
		c.release()
		_ = c.Close()
		return nil, err
	case <-ctx.Done():
		c.release()
		_ = c.Close()
		return nil, ctx.Err()
	case <-timeoutCh:
		c.release()
		_ = c.Close()
		return nil, errors.New("websocket open timeout")
	}
}

// NewServerWSConn is not supported in browser WASM, because a browser cannot
// accept incoming WebSocket connections.
func NewServerWSConn(
	conn net.Conn, rw *bufio.ReadWriter,
) (net.Conn, error) {
	return nil, errors.New("websocket server not supported in browser WASM")
}

var (
	errWSClosed = errors.New("websocket closed")

	_ net.Conn = (*wsConn)(nil)
)

// wsConn adapts a browser WebSocket to net.Conn. Each Write sends a single
// binary message; Read exposes all received messages as a byte stream.
type wsConn struct {
	ws  js.Value
	ctx context.Context

	mu     sync.Mutex
	buf    []byte
	closed bool
	err    error

	// sig wakes up a blocked Read. It has a buffer of 1 and is only a hint -
	// Read always checks buf first.
	sig chan struct{}

	opened   chan struct{}
	openErr  chan error
	openOnce sync.Once
	relOnce  sync.Once

	readTimer  *time.Timer
	writeTimer *time.Timer

	onOpen    js.Func
	onError   js.Func
	onClose   js.Func
	onMessage js.Func
}

func (c *wsConn) settle(err error) {
	c.openOnce.Do(func() {
		if err != nil {
			c.openErr <- err
		} else {
			close(c.opened)
		}
	})
}

func (c *wsConn) push(b []byte) {
	c.mu.Lock()
	if !c.closed {
		c.buf = append(c.buf, b...)
	}
	c.mu.Unlock()
	c.wake()
}

func (c *wsConn) fail(err error) {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.err = err
	}
	c.mu.Unlock()
	c.wake()
}

func (c *wsConn) wake() {
	select {
	case c.sig <- struct{}{}:
	default:
	}
}

func (c *wsConn) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		if len(c.buf) > 0 {
			n := copy(p, c.buf)
			c.buf = c.buf[n:]
			c.mu.Unlock()
			return n, nil
		}
		if c.closed {
			err := c.err
			c.mu.Unlock()
			if err == nil {
				err = io.EOF
			}
			return 0, err
		}
		c.mu.Unlock()

		select {
		case <-c.sig:
		case <-c.ctx.Done():
			return 0, io.EOF
		}
	}
}

func (c *wsConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, errWSClosed
	}

	u8 := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(u8, p)
	if err := c.send(u8); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (c *wsConn) send(v js.Value) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("websocket send: %v", r)
		}
	}()
	c.ws.Call("send", v)

	return nil
}

func (c *wsConn) Close() error {
	c.mu.Lock()
	wasClosed := c.closed
	c.closed = true
	if c.err == nil {
		c.err = io.EOF
	}
	readTimer := c.readTimer
	writeTimer := c.writeTimer
	c.readTimer = nil
	c.writeTimer = nil
	c.mu.Unlock()
	c.wake()

	if readTimer != nil {
		readTimer.Stop()
	}
	if writeTimer != nil {
		writeTimer.Stop()
	}

	if !wasClosed {
		func() {
			defer func() { _ = recover() }()
			c.ws.Call("close")
		}()
	}
	c.release()

	return nil
}

func (c *wsConn) release() {
	c.relOnce.Do(func() {
		// Detach the handlers first, so a late close event can not call
		// a released js.Func.
		c.detach()
		c.onOpen.Release()
		c.onError.Release()
		c.onClose.Release()
		c.onMessage.Release()
	})
}

func (c *wsConn) detach() {
	func() {
		defer func() { _ = recover() }()
		c.ws.Set("onopen", js.Null())
		c.ws.Set("onerror", js.Null())
		c.ws.Set("onclose", js.Null())
		c.ws.Set("onmessage", js.Null())
	}()
}

func (c *wsConn) LocalAddr() net.Addr                { return wsAddr{} }
func (c *wsConn) RemoteAddr() net.Addr               { return wsAddr{} }
func (c *wsConn) SetDeadline(t time.Time) error      { return c.setDeadline(t, &c.readTimer, &c.writeTimer) }
func (c *wsConn) SetReadDeadline(t time.Time) error  { return c.setDeadline(t, &c.readTimer, nil) }
func (c *wsConn) SetWriteDeadline(t time.Time) error { return c.setDeadline(t, nil, &c.writeTimer) }

func (c *wsConn) setDeadline(
	t time.Time, readTimer, writeTimer **time.Timer,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	apply := func(timer **time.Timer) {
		if *timer != nil {
			(*timer).Stop()
			*timer = nil
		}
		if t.IsZero() {
			return
		}

		d := time.Until(t)
		if d < 0 {
			d = 0
		}
		*timer = time.AfterFunc(d, func() {
			c.fail(os.ErrDeadlineExceeded)
			_ = c.Close()
		})
	}
	if readTimer != nil {
		apply(readTimer)
	}
	if writeTimer != nil {
		apply(writeTimer)
	}

	return nil
}

type wsAddr struct{}

func (wsAddr) Network() string { return "websocket" }
func (wsAddr) String() string  { return "websocket" }
