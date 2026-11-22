//go:build !js

package test

import (
	"net"
	"testing"
)

// wasmPair is only used in browser WASM builds.
func wasmPair(
	t *testing.T, disposeMeter <-chan struct{},
) (net.Conn, net.Conn, <-chan int64) {
	t.Helper()
	t.Fatal("wasmPair called outside of browser WASM")

	return nil, nil, nil
}
