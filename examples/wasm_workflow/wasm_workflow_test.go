//go:build !js && unix && integration

package example

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// envBrowser overrides the headless browser binary used by the test.
const envBrowser = "AM_WASM_BROWSER"

const (
	// buildTimeout covers both builds - the orchestrator and the WASM browser.
	buildTimeout = 5 * time.Minute
	// startTimeout covers the orchestrator binding the relay's HTTP server.
	startTimeout = time.Minute
	// bootTimeout covers all the browser threads booting and tunneling their
	// REPLs through the relay.
	bootTimeout = 2 * time.Minute
	// workflowTimeout covers the DAG itself, which retries on random errors.
	workflowTimeout = 5 * time.Minute
)

// browserIds are the machine IDs of the browser threads - the UI thread, the
// dispatcher, and 2 leaf WebWorkers.
var browserIds = []string{"browser1", "browser2", "browser3", "browser4"}

// TestWasmWorkflow runs the example the same way `task start` does - builds the
// orchestrator and the WASM browser threads, starts the orchestrator with the
// relay, then opens the page in a headless browser.
//
// The test finishes on its own: with EXIT_ON_COMPLETED the orchestrator exits
// once the whole DAG delivered the expected hashes, so a clean exit means the
// workflow completed.
func TestWasmWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping WASM workflow integration test in -short mode")
	}
	if !EnvExitOnCompleted {
		t.Skip("EXIT_ON_COMPLETED not set in config.env, the workflow never ends")
	}
	ctx, cancel := context.WithTimeout(context.Background(),
		buildTimeout+startTimeout+bootTimeout+workflowTimeout)
	defer cancel()

	buildOrchestrator(t, ctx)
	buildBrowser(t, ctx)
	orch := startOrchestrator(t, ctx)
	// the relay serves ./web at RELAY_ADDR from config.env
	browser := startBrowser(t, ctx, "http://"+EnvRelayHttpAddr+"/")

	// the UI thread forks the dispatcher, which forks the leaf workers - each
	// of them tunnels a REPL through the relay, which writes an addr file
	t.Run("browser threads connect", func(t *testing.T) {
		for _, id := range browserIds {
			waitForReplAddr(t, ctx, orch, browser, id, bootTimeout)
		}
	})

	// the orchestrator verifies the hashes of browser3 and browser4, restarting
	// the workflow on corrupted data, and disposes itself when they match
	t.Run("workflow completes", func(t *testing.T) {
		waitForExit(t, ctx, orch, browser, workflowTimeout)
	})
}

// ///// ///// /////

// ///// EXAMPLE STACK

// ///// ///// /////

// buildOrchestrator compiles ./cmd/orchestrator, like the "build-orchestrator"
// task does.
func buildOrchestrator(t *testing.T, ctx context.Context) {
	t.Helper()

	// go build ./cmd/orchestrator
	build := exec.CommandContext(ctx, "go", "build", "./cmd/orchestrator")
	build.Dir = exampleDir(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build orchestrator: %s\n%s", err, out)
	}
}

// buildBrowser compiles ./cmd/browser for WASM and refreshes wasm_exec.js, like
// the "build-browser" task does. All the browser threads run this binary.
func buildBrowser(t *testing.T, ctx context.Context) {
	t.Helper()

	dir := exampleDir(t)

	// go build -o web/main.wasm ./cmd/browser
	build := exec.CommandContext(ctx, "go", "build", "-o", "web/main.wasm",
		"./cmd/browser")
	build.Dir = dir
	// CGO_ENABLED defaults to 0 for js/wasm, but an inherited CGO_ENABLED=1
	// overrides that default and breaks eg os/user
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build browser wasm: %s\n%s", err, out)
	}

	// cp $(go env GOROOT)/lib/wasm/wasm_exec.js ./web
	goRoot, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %s", err)
	}
	src := filepath.Join(strings.TrimSpace(string(goRoot)), "lib", "wasm",
		"wasm_exec.js")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read wasm_exec.js: %s", err)
	}
	dst := filepath.Join(dir, "web", "wasm_exec.js")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write wasm_exec.js: %s", err)
	}
}

// proc is a running child process, with its output collected for assertions
// and for diagnostics on failure.
type proc struct {
	name string
	cmd  *exec.Cmd
	// closed once the process exited
	exited chan struct{}
	// exit error, read after exited
	err error

	mu  sync.Mutex
	out strings.Builder
}

func (p *proc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.out.Write(b)
}

func (p *proc) Output() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.out.String()
}

// start runs the process in its own process group, so it can be killed
// together with its children.
func (p *proc) start(t *testing.T) {
	t.Helper()

	p.exited = make(chan struct{})
	p.cmd.Stdout = p
	p.cmd.Stderr = p
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := p.cmd.Start(); err != nil {
		t.Fatalf("start %s: %s", p.name, err)
	}
	go func() {
		p.err = p.cmd.Wait()
		close(p.exited)
	}()

	t.Cleanup(func() {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.exited
	})
}

// startOrchestrator starts the orchestrator with the relay, like the "start"
// task does, and waits until the relay serves the example's page.
func startOrchestrator(t *testing.T, ctx context.Context) *proc {
	t.Helper()

	dir := exampleDir(t)
	rmAddrFiles(t)

	// ./orchestrator
	orch := &proc{
		name: "orchestrator",
		cmd:  exec.Command(filepath.Join(dir, "orchestrator")),
	}
	orch.cmd.Dir = dir
	orch.start(t)

	// wait for the relay's HTTP server
	client := &http.Client{Timeout: time.Second}
	waitFor(t, ctx, orch, nil, "the relay at "+EnvRelayHttpAddr, startTimeout,
		func() bool {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet,
				"http://"+EnvRelayHttpAddr+"/", nil)
			if err != nil {
				return false
			}
			resp, err := client.Do(req)
			if err != nil {
				return false
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)

			return resp.StatusCode == http.StatusOK
		})

	return orch
}

// rmAddrFiles clears the REPL addr files, like the "start" task does. The test
// then uses them to detect connected browser threads.
func rmAddrFiles(t *testing.T) {
	t.Helper()

	dir := replDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	addrs, err := filepath.Glob(filepath.Join(dir, "*.addr"))
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addrs {
		if err := os.Remove(addr); err != nil {
			t.Fatal(err)
		}
	}
}

// waitForReplAddr waits for a browser thread to tunnel its REPL through the
// relay, which writes an addr file for it.
func waitForReplAddr(
	t *testing.T, ctx context.Context, orch *proc, browser *proc, id string,
	timeout time.Duration,
) {
	t.Helper()

	path := filepath.Join(replDir(t), "repl-"+id+".addr")
	waitFor(t, ctx, orch, browser, id+" to connect", timeout, func() bool {
		_, err := os.Stat(path)

		return err == nil
	})
}

// waitForExit waits for the orchestrator to complete the workflow and exit,
// which it does thanks to EXIT_ON_COMPLETED.
func waitForExit(
	t *testing.T, ctx context.Context, orch *proc, browser *proc,
	timeout time.Duration,
) {
	t.Helper()

	select {
	case <-orch.exited:
		if orch.err != nil {
			t.Fatalf("orchestrator: %s%s", orch.err, diag(orch, browser))
		}

	case <-time.After(timeout):
		t.Fatalf("timeout waiting for the workflow after %s%s", timeout,
			diag(orch, browser))

	case <-ctx.Done():
		t.Fatalf("ctx done waiting for the workflow%s", diag(orch, browser))
	}
}

func exampleDir(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

func replDir(t *testing.T) string {
	t.Helper()

	return filepath.Join(exampleDir(t), EnvReplDir)
}

// ///// ///// /////

// ///// BROWSER

// ///// ///// /////

// startBrowser launches a headless browser on page. Console output of all the
// threads is logged to stderr and collected for diagnostics.
func startBrowser(t *testing.T, ctx context.Context, page string) *proc {
	t.Helper()

	bin := browserPath(t)
	// A dedicated profile dir, removed only after the browser has fully exited,
	// not t.TempDir(): the harness cleans t.TempDir() as soon as the test
	// returns, which races the browser still flushing its Cache_Data and fails
	// with "directory not empty".
	profile, err := os.MkdirTemp("", "am-wasm-workflow-profile-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// the browser has been killed, remove its profile, retrying briefly in
		// case the OS is still releasing cache files
		for range 10 {
			if os.RemoveAll(profile) == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	})

	browser := &proc{
		name: "browser",
		// nolint:gosec
		cmd: exec.Command(bin,
			"--headless=new",
			"--disable-gpu",
			"--no-first-run",
			"--no-default-browser-check",
			// console.log of the UI thread and all the WebWorkers
			"--enable-logging=stderr",
			// keep the WASM machines ticking while the page is not visible
			"--disable-background-timer-throttling",
			"--disable-backgrounding-occluded-windows",
			"--disable-renderer-backgrounding",
			"--user-data-dir="+profile,
			page,
		),
	}
	browser.start(t)

	// a browser which cannot start (eg needs --no-sandbox) exits right away
	select {
	case <-browser.exited:
		t.Fatalf("browser exited: %s\n\n----- browser -----\n%s",
			browser.err, browser.Output())
	case <-time.After(time.Second):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	return browser
}

func browserPath(t *testing.T) string {
	t.Helper()

	candidates := []string{
		os.Getenv(envBrowser),
		"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"brave-browser",
		"google-chrome",
		"chromium",
		"chromium-browser",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if strings.ContainsRune(c, filepath.Separator) {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c
			}
			continue
		}
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
	}
	t.Skipf("no headless Chrome/Brave browser found, set %s", envBrowser)

	return ""
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// waitFor polls check until it passes, or fails the test with the collected
// output attached. browser is optional.
func waitFor(
	t *testing.T, ctx context.Context, orch *proc, browser *proc, desc string,
	timeout time.Duration, check func() bool,
) {
	t.Helper()

	start := time.Now()
	for {
		if check() {
			return
		}
		if time.Since(start) > timeout {
			t.Fatalf("timeout waiting for %s after %s%s", desc, timeout,
				diag(orch, browser))
		}

		// the orchestrator exiting early means it will never pass
		select {
		case <-orch.exited:
			t.Fatalf("orchestrator exited early while waiting for %s: %s%s",
				desc, orch.err, diag(orch, browser))
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("ctx done waiting for %s%s", desc, diag(orch, browser))
		}
	}
}

// diag renders the collected output of the example and the browser.
func diag(orch *proc, browser *proc) string {
	out := "\n\n----- orchestrator -----\n" + orch.Output()
	if browser != nil {
		out += "\n----- browser -----\n" + tail(browser.Output(), 100)
	}

	return out
}

// tail returns the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}
