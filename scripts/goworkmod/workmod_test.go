package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fixture creates a workspace with a root module, one local lib and one
// local lib without a go.mod.
func fixture(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.21\n")
	write(t, filepath.Join(root, "liba", "go.mod"),
		"module example.com/liba\n\ngo 1.21\n")
	if err := os.MkdirAll(filepath.Join(root, "nolib"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "go.work"),
		"go 1.21\n\nuse (\n\t.\n\t./liba\n)\n")
	return root
}

func TestApplyRevertRoundTrip(t *testing.T) {
	root := fixture(t)
	modPath := filepath.Join(root, "go.mod")
	orig := read(t, modPath)

	cfg := &config{mode: modeApply, workPath: filepath.Join(root, "go.work")}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}

	applied := read(t, modPath)
	if want := "replace example.com/liba => ./liba"; !strings.Contains(applied, want) {
		t.Fatalf("missing %q in:\n%s", want, applied)
	}
	// self replace must be skipped
	if strings.Contains(applied, "example.com/root =>") {
		t.Fatalf("self replace added:\n%s", applied)
	}

	// idempotent
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	if got := read(t, modPath); got != applied {
		t.Fatalf("second apply changed the file:\n%s", got)
	}

	cfg.mode = modeRevert
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	if got := read(t, modPath); got != orig {
		t.Fatalf("revert did not restore the file, want:\n%s\ngot:\n%s", orig, got)
	}
}

func TestApplyParentModule(t *testing.T) {
	// a nested module linking to its parent, like pkg/pubsub
	root := t.TempDir()
	sub := filepath.Join(root, "pkg", "sub")
	write(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.21\n")
	write(t, filepath.Join(sub, "go.mod"),
		"module example.com/root/pkg/sub\n\ngo 1.21\n")
	write(t, filepath.Join(sub, "go.work"),
		"go 1.21\n\nuse (\n\t.\n\t../..\n)\n")

	cfg := &config{mode: modeApply, workPath: filepath.Join(sub, "go.work")}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	applied := read(t, filepath.Join(sub, "go.mod"))
	if want := "replace example.com/root => ../.."; !strings.Contains(applied, want) {
		t.Fatalf("missing %q in:\n%s", want, applied)
	}

	cfg.mode = modeRevert
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(sub, "go.mod")); strings.Contains(got, "replace") {
		t.Fatalf("revert left a replace:\n%s", got)
	}
}

func TestApplyWorkReplace(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "go.work"), "go 1.21\n\nuse .\n\n"+
		"replace example.com/foo => ../foo\n\n"+
		"replace example.com/bar v1.0.0 => example.com/fork/bar v9.9.9\n")

	cfg := &config{mode: modeApply, workPath: filepath.Join(root, "go.work")}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}

	applied := read(t, filepath.Join(root, "go.mod"))
	for _, want := range []string{
		"example.com/foo => ../foo",
		"example.com/bar v1.0.0 => example.com/fork/bar v9.9.9",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("missing %q in:\n%s", want, applied)
		}
	}

	cfg.mode = modeRevert
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, "go.mod")); strings.Contains(got, "replace") {
		t.Fatalf("revert left a replace:\n%s", got)
	}
}

func TestApplyMissingUseDir(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "go.work"),
		"go 1.21\n\nuse (\n\t.\n\t./nope\n\t./nolib\n)\n")
	modPath := filepath.Join(root, "go.mod")
	orig := read(t, modPath)

	// warn and skip
	cfg := &config{mode: modeApply, workPath: filepath.Join(root, "go.work")}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	if got := read(t, modPath); got != orig {
		t.Fatalf("missing use dirs changed the file:\n%s", got)
	}

	// fail on strict
	cfg = &config{
		mode:     modeApply,
		workPath: filepath.Join(root, "go.work"),
		strict:   true,
	}
	if err := run(cfg); err == nil {
		t.Fatal("strict apply did not fail on a missing use dir")
	}
}

func TestDryRun(t *testing.T) {
	root := fixture(t)
	modPath := filepath.Join(root, "go.mod")
	orig := read(t, modPath)

	cfg := &config{
		mode:     modeApply,
		workPath: filepath.Join(root, "go.work"),
		dryRun:   true,
	}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	if got := read(t, modPath); got != orig {
		t.Fatalf("dry-run changed the file:\n%s", got)
	}
}

func TestRevertKeepsForeignReplace(t *testing.T) {
	root := fixture(t)
	modPath := filepath.Join(root, "go.mod")
	write(t, modPath, "module example.com/root\n\ngo 1.21\n\n"+
		"replace example.com/other => ../other\n")

	cfg := &config{mode: modeApply, workPath: filepath.Join(root, "go.work")}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}

	cfg.mode = modeRevert
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}

	got := read(t, modPath)
	if strings.Contains(got, "example.com/liba") {
		t.Fatalf("revert left the workspace replace:\n%s", got)
	}
	if !strings.Contains(got, "example.com/other => ../other") {
		t.Fatalf("revert dropped a foreign replace:\n%s", got)
	}
}

func TestApplyDedupesUseAndWorkReplace(t *testing.T) {
	root := fixture(t)
	write(t, filepath.Join(root, "go.work"), "go 1.21\n\nuse (\n\t.\n\t./liba\n)\n\n"+
		"replace example.com/liba => ./liba\n")

	cfg := &config{mode: modeApply, workPath: filepath.Join(root, "go.work")}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}

	applied := read(t, filepath.Join(root, "go.mod"))
	if n := strings.Count(applied, "example.com/liba =>"); n != 1 {
		t.Fatalf("want 1 replace for example.com/liba, got %d:\n%s", n, applied)
	}

	cfg.mode = modeRevert
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, "go.mod")); strings.Contains(got, "replace") {
		t.Fatalf("revert left a replace:\n%s", got)
	}
}

func TestRevertNothingToDo(t *testing.T) {
	root := fixture(t)
	cfg := &config{mode: modeRevert, workPath: filepath.Join(root, "go.work")}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
}

// TestApplyBuildsWithoutWork proves the applied replaces really link the
// workspace modules in module mode.
func TestApplyBuildsWithoutWork(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.21\n\n"+
		"require example.com/liba v0.0.0\n")
	write(t, filepath.Join(root, "main.go"), `package main

import "example.com/liba"

func main() {
	println(liba.A())
}
`)
	write(t, filepath.Join(root, "liba", "go.mod"),
		"module example.com/liba\n\ngo 1.21\n")
	write(t, filepath.Join(root, "liba", "a.go"),
		"package liba\n\nfunc A() string { return \"a\" }\n")
	write(t, filepath.Join(root, "go.work"),
		"go 1.21\n\nuse (\n\t.\n\t./liba\n)\n")

	// without the replace, a module-mode build can't resolve the local lib
	_, err := goRunEnv(root, []string{"GOWORK=off"}, "build", "./...")
	if err == nil {
		t.Fatal("build without a replace unexpectedly succeeded")
	}

	cfg := &config{mode: modeApply, workPath: filepath.Join(root, "go.work")}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := goRunEnv(root, []string{"GOWORK=off"}, "build", "./..."); err != nil {
		t.Fatalf("build with the applied replace failed: %v", err)
	}

	cfg.mode = modeRevert
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := goRunEnv(root, []string{"GOWORK=off"}, "build", "./..."); err == nil {
		t.Fatal("build after revert unexpectedly succeeded")
	}
}
