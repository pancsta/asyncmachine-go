package example

import (
	"io"
	"net"
	"syscall/js"
	"time"
)

// PortConn adapts a JS MessagePort to net.Conn
type PortConn struct {
	port      js.Value
	closeFunc func()
	reader    *io.PipeReader
	writer    *io.PipeWriter
}

var _ net.Conn = &PortConn{}

// NewPortConn creates a connection around a specific MessagePort
// TODO move to pkg/rpc
func NewPortConn(port js.Value) *PortConn {
	pr, pw := io.Pipe()
	wc := &PortConn{
		port:   port,
		reader: pr,
		writer: pw,
	}

	// 1. Setup 'onmessage' on the specific port
	onMessage := js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		event := args[0]
		data := event.Get("data")

		if data.IsNull() || data.IsUndefined() {
			return nil
		}

		length := data.Get("length").Int()
		if length == 0 {
			return nil
		}

		// Copy JS Memory -> Go Memory
		buf := make([]byte, length)
		js.CopyBytesToGo(buf, data)

		// Push to pipe (async)
		go func() {
			pw.Write(buf)
		}()
		return nil
	})

	wc.port.Set("onmessage", onMessage)

	// Cleanup callback
	wc.closeFunc = func() {
		onMessage.Release()
		wc.port.Call("close") // Close the MessagePort
	}

	return wc
}

func (c *PortConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

func (c *PortConn) Write(b []byte) (int, error) {
	// 1. Allocate JS Memory
	uint8Array := js.Global().Get("Uint8Array").New(len(b))

	// 2. Copy Go -> JS (Unavoidable)
	js.CopyBytesToJS(uint8Array, b)

	// 3. TRANSFER the JS memory to the other thread (Zero-Copy)
	// Syntax: port.postMessage(data, [transferList])
	// We wrap the buffer in a JS Array to pass as the transfer list.
	transferList := js.Global().Get("Array").New()
	transferList.Call("push", uint8Array.Get("buffer"))

	c.port.Call("postMessage", uint8Array, transferList)

	return len(b), nil
}

func (c *PortConn) Close() error {
	c.closeFunc()
	c.reader.Close()
	return c.writer.Close()
}

// Boilerplate stubs...
func (c *PortConn) LocalAddr() net.Addr                { return dummyAddr{} }
func (c *PortConn) RemoteAddr() net.Addr               { return dummyAddr{} }
func (c *PortConn) SetDeadline(t time.Time) error      { return nil }
func (c *PortConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *PortConn) SetWriteDeadline(t time.Time) error { return nil }

type dummyAddr struct{}

func (d dummyAddr) Network() string { return "js-port" }
func (d dummyAddr) String() string  { return "message-channel" }
