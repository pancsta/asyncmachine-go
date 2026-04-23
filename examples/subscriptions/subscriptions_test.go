//go:build integration

package main_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSubscriptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("example failed: %v\n%s", err, out)
	}
	outStr := string(out)
	for _, want := range []string{
		"1 - Foo activated",
		"2 - Bar deactivated",
		"3 - Bar activated with ID=123",
		"4 - Foo tick >= 1 and Bar tick >= 3",
		"5 - Foo tick increased by 2 since now",
		"6 - Error",
	} {
		if !strings.Contains(outStr, want) {
			t.Errorf("expected %q in output, got:\n%s", want, outStr)
		}
	}
}
