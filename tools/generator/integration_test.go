//go:build integration

package generator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pancsta/asyncmachine-go/tools/generator/cli"
)

func TestStarterKit_E2E_Execution(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	yamlContent := []byte(dedent.Dedent(`
		Boot:
		    auto: true
		Ready:
		    require:
		        - Boot
		Processing:
		    multi: true
		    require:
		        - Ready
		    tags:
		        - args
	`))

	params := cli.StarterParams{
		FileContent: yamlContent,
		Name:        "Pipeline",
		Uri:         "testpipeline",
		Module:      true,
		Handlers:    true,
		Args:        true,
		Path:        tempDir,
	}

	err := GenStarterKit(ctx, &params)
	require.NoError(t, err)

	machDir := filepath.Join(tempDir, "pipeline")

	// Verify files written on disk
	assert.FileExists(t, filepath.Join(machDir, "states", "ss_pipeline.go"))
	assert.FileExists(t, filepath.Join(machDir, "pipeline.go"))
	assert.FileExists(t, filepath.Join(machDir, "pipeline_test.go"))
	assert.FileExists(t, filepath.Join(machDir, "handlers.go"))
	assert.FileExists(t, filepath.Join(machDir, "go.mod"))

	// Replace module dependency with local workspace version in go.mod
	wd, err := os.Getwd()
	require.NoError(t, err)
	repoRoot := filepath.Clean(filepath.Join(wd, "../.."))

	editArgs := []string{"mod", "edit", "-replace=github.com/pancsta/asyncmachine-go=" + repoRoot}

	cmdEdit := exec.CommandContext(ctx, "go", editArgs...)
	cmdEdit.Dir = machDir
	outEdit, err := cmdEdit.CombinedOutput()
	require.NoError(t, err, "go mod edit failed: %s", string(outEdit))

	// Run go mod tidy in the generated starter kit directory
	cmdTidy := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmdTidy.Dir = machDir
	outTidy, err := cmdTidy.CombinedOutput()
	require.NoError(t, err, "go mod tidy failed: %s", string(outTidy))

	// Run go test in the generated starter kit directory
	cmd := exec.CommandContext(ctx, "go", "test", "-v", "./...")
	cmd.Dir = machDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go test failed: %s", string(out))
	assert.Contains(t, string(out), "TestStart")
}
