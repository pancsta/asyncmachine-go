//go:build integration
package arpc_test

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestArpc(t *testing.T) {
	t.Skip("TODO")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// start server
	serverCmd := exec.CommandContext(ctx, "go", "run", "./server")
	serverOut, err := serverCmd.StdoutPipe()
	serverCmd.Stderr = nil // discard
	if err != nil {
		t.Fatal(err)
	}
	if err := serverCmd.Start(); err != nil {
		t.Fatalf("server start failed: %v", err)
	}
	defer serverCmd.Process.Kill()

	// wait for server to announce it's ready
	serverScanner := bufio.NewScanner(serverOut)
	serverReady := make(chan struct{}, 1)
	go func() {
		for serverScanner.Scan() {
			line := serverScanner.Text()
			t.Log("server:", line)
			if strings.Contains(line, "Started aRPC server") {
				serverReady <- struct{}{}
			}
		}
	}()

	select {
	case <-serverReady:
	case <-ctx.Done():
		t.Fatal("timeout waiting for server to start")
	}

	// start client
	clientCmd := exec.CommandContext(ctx, "go", "run", "./client")
	clientOut, err := clientCmd.StdoutPipe()
	clientCmd.Stderr = nil // discard
	if err != nil {
		t.Fatal(err)
	}
	if err := clientCmd.Start(); err != nil {
		t.Fatalf("client start failed: %v", err)
	}
	defer clientCmd.Process.Kill()

	// wait for client to report successful connection
	clientScanner := bufio.NewScanner(clientOut)
	clientReady := make(chan struct{}, 1)
	go func() {
		for clientScanner.Scan() {
			line := clientScanner.Text()
			t.Log("client:", line)
			if strings.Contains(line, "Connected to aRPC") {
				clientReady <- struct{}{}
			}
		}
	}()

	select {
	case <-clientReady:
	case <-ctx.Done():
		t.Fatal("timeout waiting for client to connect")
	}

	// let them exchange a few mutations
	time.Sleep(3 * time.Second)
}
