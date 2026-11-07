//go:build integration

package iroh

import (
	"os"
	"testing"

	"github.com/pancsta/asyncmachine-go/pkg/rpc/test"
)

func TestMutationsSyncMany(t *testing.T) {
	// TODO fix CI
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Skip("Skipping test in GitHub CI environment")
	}
	test.TemplateTestMutationsSyncMany(t, newTest)
}
