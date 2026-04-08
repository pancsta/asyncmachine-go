//go:build integration
package main_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDagDependencyGraph(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("example failed: %v\n%s", err, out)
	}
	outStr := string(out)
	for _, want := range []string{"A ok", "B ok", "C ok", "D ok"} {
		if !strings.Contains(outStr, want) {
			t.Errorf("expected %q in output, got:\n%s", want, outStr)
		}
	}
}
