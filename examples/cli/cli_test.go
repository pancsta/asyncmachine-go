//go:build integration

package main_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCliFoo1(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".", "--foo-1", "--duration=100ms")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("example failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "start done") {
		t.Errorf("expected 'start done' in output, got:\n%s", out)
	}
}

func TestCliBar2(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".", "--bar-2", "--duration=100ms")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("example failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "start done") {
		t.Errorf("expected 'start done' in output, got:\n%s", out)
	}
}
