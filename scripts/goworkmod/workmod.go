package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type mode string

const (
	modeApply  mode = "apply"
	modeRevert mode = "revert"
)

const (
	sourceUse  = "use"
	sourceWork = "go.work replace"
)

// config as parsed from the CLI.
type config struct {
	mode     mode
	workPath string
	dryRun   bool
	tidy     bool
	strict   bool
	verbose  bool
}

// edit is a single replace planned for the go.mod.
type edit struct {
	// key is the old side in the go CLI form (path or path@version)
	key string
	// value is the new side as written in a go.mod (path or "path version")
	value string
	// arg is the new side in the go CLI form (path or path@version)
	arg string
	// source is sourceUse or sourceWork, for revert matching
	source string
	// dir is the abs dir of a use entry, for revert matching
	dir string
}

func (e edit) replaceFlag() string {
	return "-replace=" + e.key + "=" + e.arg
}

func (e edit) dropFlag() string {
	return "-dropreplace=" + e.key
}

// modVersion is a module path and an optional version, as used in the
// replace directives of go.mod and go.work.
type modVersion struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
}

type modReplace struct {
	Old modVersion `json:"Old"`
	New modVersion `json:"New"`
}

// modFile is the subset of `go mod edit -json` we need.
type modFile struct {
	Module struct {
		Path string `json:"Path"`
	} `json:"Module"`
	Go      string       `json:"Go"`
	Replace []modReplace `json:"Replace"`
}

type workUse struct {
	DiskPath   string `json:"DiskPath"`
	ModulePath string `json:"ModulePath"`
}

// workFile is the subset of `go work edit -json` we need.
type workFile struct {
	Go      string       `json:"Go"`
	Use     []workUse    `json:"Use"`
	Replace []modReplace `json:"Replace"`
}

func run(cfg *config) error {
	workPath, found, err := findWorkPath(cfg.workPath)
	if err != nil {
		return err
	}
	workDir := filepath.Dir(workPath)
	modPath := filepath.Join(workDir, "go.mod")
	if _, err := os.Stat(modPath); err != nil {
		return fmt.Errorf("%s: only a go.mod in the same dir as go.work is "+
			"supported: %w", workDir, err)
	}

	var wf *workFile
	if found {
		wf, err = readWork(workDir)
		if err != nil {
			return err
		}
	} else {
		// assume an empty go.work, e.g. before `link-pubsub` in CI
		wf = &workFile{}
	}
	mf, err := readMod(workDir)
	if err != nil {
		return err
	}
	if mf.Module.Path == "" {
		return fmt.Errorf("%s: missing module path", modPath)
	}

	fmt.Printf("%s %s\n", cfg.mode, modPath)
	if found {
		fmt.Printf("  work: %s\n", workPath)
	} else {
		fmt.Printf("  work: %s (not found, assuming empty)\n", workPath)
	}
	// tidy needs the workspace replaces, so skip it for an assumed empty work
	tidy := cfg.tidy && found
	if cfg.tidy && !found {
		fmt.Printf("  ! no go.work found, skipping tidy\n")
	}

	// on revert, a missing use dir only means the key can't be derived
	strict := cfg.strict && cfg.mode == modeApply
	plan, warns, err := planEdits(workDir, wf, mf.Module.Path, strict)
	if err != nil {
		return err
	}
	for _, w := range warns {
		fmt.Printf("  ! %s\n", w)
	}

	existing := make(map[string]modVersion, len(mf.Replace))
	for _, r := range mf.Replace {
		existing[keyStr(r.Old)] = r.New
	}

	var edits []edit
	if cfg.mode == modeApply {
		edits = filterApplies(cfg, plan, existing)
	} else {
		edits = filterReverts(plan, existing, workDir)
	}

	if len(edits) == 0 {
		fmt.Printf("%s: nothing to do\n", cfg.mode)
		// still honor -tidy, e.g. for an assumed empty go.work in CI
		if tidy && !cfg.dryRun {
			if err := tidyMod(workDir); err != nil {
				return err
			}
		}
		return nil
	}

	if cfg.dryRun {
		fmt.Printf("%s: %d change(s), dry-run, nothing written\n",
			cfg.mode, len(edits))
		return nil
	}

	args := make([]string, 0, len(edits)+2)
	args = append(args, "mod", "edit")
	for _, e := range edits {
		switch cfg.mode {
		case modeApply:
			args = append(args, e.replaceFlag())
		case modeRevert:
			args = append(args, e.dropFlag())
		}
	}
	if _, err := goRun(workDir, args...); err != nil {
		return err
	}
	if _, err := goRun(workDir, "mod", "edit", "-fmt"); err != nil {
		return err
	}

	if cfg.mode == modeRevert {
		reportLeftovers(existing, edits)
	}

	if tidy {
		if err := tidyMod(workDir); err != nil {
			return err
		}
	}

	fmt.Printf("%s: %d change(s)\n", cfg.mode, len(edits))
	return nil
}

func tidyMod(workDir string) error {
	fmt.Printf("  tidy: GOWORK=off go mod tidy\n")
	if _, err := goRunEnv(workDir, []string{"GOWORK=off"},
		"mod", "tidy"); err != nil {
		return err
	}
	return nil
}

// planEdits derives the replace edits from the go.work for the given target
// module path.
func planEdits(workDir string, wf *workFile, targetMod string, strict bool) (
	[]edit, []string, error,
) {
	var (
		out   []edit
		warns []string
	)
	warn := func(format string, a ...any) error {
		msg := fmt.Sprintf(format, a...)
		if strict {
			return errors.New(msg)
		}
		warns = append(warns, msg)
		return nil
	}

	// use dirs become local path replaces
	for _, use := range wf.Use {
		if use.DiskPath == "" {
			continue
		}
		abs := use.DiskPath
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(workDir, abs)
		}
		abs = filepath.Clean(abs)

		// the target module itself
		if abs == workDir {
			continue
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			if err := warn("use dir %q does not exist, skipping",
				use.DiskPath); err != nil {
				return nil, nil, err
			}
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err != nil {
			if err := warn("use dir %q has no go.mod, skipping",
				use.DiskPath); err != nil {
				return nil, nil, err
			}
			continue
		}

		mod := use.ModulePath
		if mod == "" {
			m, err := modulePath(abs)
			if err != nil {
				return nil, nil, err
			}
			mod = m
		}
		// self replace
		if mod == targetMod {
			continue
		}

		rel, err := filepath.Rel(workDir, abs)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", abs, err)
		}
		rel = filepath.ToSlash(rel)
		if !isLocalPath(rel) {
			rel = "./" + rel
		}
		out = append(out, edit{
			key:    mod,
			value:  rel,
			arg:    rel,
			source: sourceUse,
			dir:    abs,
		})
	}

	// explicit go.work replaces are copied as-is
	for _, r := range wf.Replace {
		if r.Old.Path == "" || r.New.Path == "" {
			continue
		}
		// self replace
		if r.Old.Path == targetMod {
			continue
		}
		out = append(out, edit{
			key:    keyStr(r.Old),
			value:  newDisplay(r.New),
			arg:    newArg(r.New),
			source: sourceWork,
		})
	}

	// one edit per key, a go.work replace wins over a use dir
	idx := make(map[string]int, len(out))
	dedup := make([]edit, 0, len(out))
	for _, e := range out {
		if i, ok := idx[e.key]; ok {
			dedup[i] = e
			continue
		}
		idx[e.key] = len(dedup)
		dedup = append(dedup, e)
	}

	return dedup, warns, nil
}

// filterApplies drops no-ops and reports overwrites.
func filterApplies(cfg *config, plan []edit,
	existing map[string]modVersion,
) []edit {
	var out []edit
	for _, e := range plan {
		if cur, ok := existing[e.key]; ok {
			if newDisplay(cur) == e.value {
				if cfg.verbose {
					fmt.Printf("  = %s => %s\n", e.key, e.value)
				}
				continue
			}
			fmt.Printf("  ! %s: overwriting existing replace %s "+
				"(revert can't restore it)\n", e.key, newDisplay(cur))
		}
		fmt.Printf("  + %s => %s\n", e.key, e.value)
		out = append(out, e)
	}
	return out
}

// filterReverts drops the replaces which currently point at the module the
// go.work points at (or match a go.work replace verbatim).
func filterReverts(plan []edit, existing map[string]modVersion,
	modDir string,
) []edit {
	var out []edit
	for _, e := range plan {
		cur, ok := existing[e.key]
		if !ok {
			continue
		}
		switch e.source {
		case sourceUse:
			// never touch version/fork replaces not coming from the go.work
			if cur.Version != "" {
				continue
			}
			if !sameDir(modDir, cur.Path, e.dir) {
				continue
			}
		case sourceWork:
			if newDisplay(cur) != e.value {
				continue
			}
		}
		fmt.Printf("  - %s => %s\n", e.key, newDisplay(cur))
		out = append(out, e)
	}
	return out
}

// reportLeftovers lists local replaces which revert left untouched.
func reportLeftovers(existing map[string]modVersion, dropped []edit) {
	kept := make(map[string]bool, len(dropped))
	for _, e := range dropped {
		kept[e.key] = true
	}

	var left []string
	for key, v := range existing {
		if kept[key] || v.Version != "" || !isLocalPath(v.Path) {
			continue
		}
		left = append(left, fmt.Sprintf("%s => %s", key, v.Path))
	}
	if len(left) == 0 {
		return
	}
	sort.Strings(left)
	fmt.Printf("  note: %d local replace(s) left untouched: %s\n",
		len(left), strings.Join(left, ", "))
}

// findWorkPath resolves the go.work to use. If none is found, the path where
// it would be is returned with found=false, so the caller can assume an empty
// go.work.
func findWorkPath(flagPath string) (string, bool, error) {
	if flagPath != "" {
		return existingWorkPath(flagPath)
	}
	if env := os.Getenv("GOWORK"); env != "" && env != "off" {
		return existingWorkPath(env)
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		p := filepath.Join(dir, "go.work")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true, nil
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return filepath.Join(wd, "go.work"), false, nil
}

// existingWorkPath resolves path to a go.work file. If the file is missing,
// found=false is returned with the path where it would be.
func existingWorkPath(path string) (string, bool, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		p = filepath.Join(p, "go.work")
	}
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return p, false, nil
	}
	return p, true, nil
}

func readWork(dir string) (*workFile, error) {
	out, err := goRun(dir, "work", "edit", "-json")
	if err != nil {
		return nil, err
	}
	var wf workFile
	if err := json.Unmarshal(out, &wf); err != nil {
		return nil, fmt.Errorf("%s/go.work: %w", dir, err)
	}
	return &wf, nil
}

func readMod(dir string) (*modFile, error) {
	out, err := goRun(dir, "mod", "edit", "-json")
	if err != nil {
		return nil, err
	}
	var mf modFile
	if err := json.Unmarshal(out, &mf); err != nil {
		return nil, fmt.Errorf("%s/go.mod: %w", dir, err)
	}
	return &mf, nil
}

// modulePath reads the module path of a local module.
func modulePath(dir string) (string, error) {
	mf, err := readMod(dir)
	if err != nil {
		return "", err
	}
	if mf.Module.Path == "" {
		return "", fmt.Errorf("%s/go.mod: missing module path", dir)
	}
	return mf.Module.Path, nil
}

func goRun(dir string, args ...string) ([]byte, error) {
	return goRunEnv(dir, nil, args...)
}

// goRunEnv runs the go CLI in dir, without an inherited GOWORK.
func goRunEnv(dir string, env []string, args ...string) ([]byte, error) {
	args = append([]string{"-C", dir}, args...)
	cmd := exec.Command("go", args...)
	cmd.Env = append(cleanEnv(), env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args[2:], " "),
				err, msg)
		}
		return nil, fmt.Errorf("go %s: %w", strings.Join(args[2:], " "), err)
	}
	return stdout.Bytes(), nil
}

// cleanEnv returns the env without GOWORK, so `go -C` decides the workspace.
func cleanEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "GOWORK=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// sameDir reports whether rel (relative to base) resolves to want.
func sameDir(base, rel, want string) bool {
	p := rel
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	p = filepath.Clean(p)
	if p == filepath.Clean(want) {
		return true
	}
	rp, err1 := filepath.EvalSymlinks(p)
	rw, err2 := filepath.EvalSymlinks(want)
	return err1 == nil && err2 == nil && rp == rw
}

// isLocalPath reports whether p is a filesystem path (not a module path).
func isLocalPath(p string) bool {
	return p == "." || p == ".." || strings.HasPrefix(p, "./") ||
		strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") ||
		filepath.IsAbs(p)
}

// keyStr formats the old side of a replace in the go CLI form.
func keyStr(v modVersion) string {
	if v.Version != "" {
		return v.Path + "@" + v.Version
	}
	return v.Path
}

// newArg formats the new side of a replace in the go CLI form.
func newArg(v modVersion) string {
	if v.Version != "" {
		return v.Path + "@" + v.Version
	}
	return v.Path
}

// newDisplay formats the new side of a replace as written in a go.mod.
func newDisplay(v modVersion) string {
	if v.Version != "" {
		return v.Path + " " + v.Version
	}
	return v.Path
}
