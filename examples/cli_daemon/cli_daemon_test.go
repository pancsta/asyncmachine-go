//go:build integration
package cli_daemon_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCliDaemonFoo1(t *testing.T) {
	testCliDaemon(t, "--foo-1")
}

func TestCliDaemonBar2(t *testing.T) {
	testCliDaemon(t, "--bar-2")
}

func testCliDaemon(t *testing.T, opFlag string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// random number
	testAddr := fmt.Sprintf("localhost:%d", 18000+rand.Intn(100))

	// start daemon
	daemonCmd := exec.CommandContext(ctx, "go", "run", "./daemon",
		"--addr="+testAddr, "--debug=false")
	daemonOut, err := daemonCmd.StdoutPipe()
	daemonCmd.Stderr = nil
	if err != nil {
		t.Fatal(err)
	}
	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("daemon start failed: %v", err)
	}
	defer daemonCmd.Process.Kill()

	// scan daemon output; signal when the REPL line is announced (last startup line)
	daemonReady := make(chan struct{}, 1)
	go func() {
		scanner := bufio.NewScanner(daemonOut)
		for scanner.Scan() {
			line := scanner.Text()
			t.Log("daemon:", line)
			if strings.Contains(line, "REPL listening on") {
				daemonReady <- struct{}{}
			}
		}
	}()

	select {
	case <-daemonReady:
	case <-ctx.Done():
		t.Fatal("timeout waiting for daemon to start")
	}
	// give aRPC server time to fully bind after printing
	time.Sleep(500 * time.Millisecond)

	// start CLI — use short duration so the op finishes quickly
	// capture both stdout and stderr: "Connected to aRPC" is on stdout,
	// "Payload: done for" comes from println() which writes to stderr
	cliCmd := exec.CommandContext(ctx, "go", "run", "./cli",
		opFlag, "--duration=500ms", "--addr="+testAddr, "--debug=false")
	cliStdout, err := cliCmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cliStderr, err := cliCmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cliCmd.Start(); err != nil {
		t.Fatalf("cli start failed: %v", err)
	}

	// collect lines from both stdout and stderr
	var mu sync.Mutex
	var cliLines []string
	collect := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			t.Log("cli:", line)
			mu.Lock()
			cliLines = append(cliLines, line)
			mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); collect(cliStdout) }()
	go func() { defer wg.Done(); collect(cliStderr) }()

	// CLI exits naturally once it receives the payload
	cliDone := make(chan struct{})
	go func() { wg.Wait(); close(cliDone) }()

	select {
	case <-cliDone:
	case <-ctx.Done():
		cliCmd.Process.Kill()
		t.Fatal("timeout waiting for CLI to complete")
	}

	if err := cliCmd.Wait(); err != nil {
		t.Errorf("CLI exited with error: %v", err)
	}

	out := strings.Join(cliLines, "\n")
	if !strings.Contains(out, "Connected to aRPC") {
		t.Errorf("expected 'Connected to aRPC' in CLI output, got:\n%s", out)
	}
	if !strings.Contains(out, "Payload: done for") {
		t.Errorf("expected 'Payload: done for' in CLI output, got:\n%s", out)
	}
}
