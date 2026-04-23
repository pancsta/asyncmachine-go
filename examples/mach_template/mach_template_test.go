//go:build integration

package main_test

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMachTemplate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer cmd.Process.Kill()

	var mu sync.Mutex
	var lines []string
	collect := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			t.Log(line)
			mu.Lock()
			lines = append(lines, line)
			mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); collect(stdout) }()
	go func() { defer wg.Done(); collect(stderr) }()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timeout waiting for mach_template to complete")
	}

	if err := cmd.Wait(); err != nil {
		t.Errorf("mach_template exited with error: %v", err)
	}

	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "[hello machines]") {
		t.Errorf("expected '[hello machines]' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "BazState:") {
		t.Errorf("expected 'BazState:' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "done") {
		t.Errorf("expected 'done:' in output, got:\n%s", out)
	}
}
