//go:build integration

package main_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRawStrings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("example failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "done") {
		t.Errorf("expected 'done' in output, got:\n%s", out)
	}
}
