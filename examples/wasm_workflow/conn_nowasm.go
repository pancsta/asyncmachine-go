//go:build !wasm

package example

import (
	"io"
	"net"
	"time"
)

// PortConn adapts a JS MessagePort to net.Conn
type PortConn struct {
	port      any
	closeFunc func()
	reader    *io.PipeReader
	writer    *io.PipeWriter
}

func (c *PortConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

func (c *PortConn) Write(b []byte) (int, error) { return 0, nil }

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
