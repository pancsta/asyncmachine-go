//go:build !js && unix && integration

package example

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestWasmLivepage runs the example like `task start` does: builds the WASM
// client, starts the server, opens the page in a headless browser and asserts
// that aRPC and livepage HTML share a single WebSocket, both ways.
func TestWasmLivepage(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Skip("skipping browser WASM tests in CI")
	}
	chrome := findBrowser()
	if chrome == "" {
		t.Skip("no local browser found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	tinygo := os.Getenv("AM_WASM_TINYGO") != ""
	buildWasm(t, ctx, tinygo)

	// in-process server, so the test can inspect it
	srv, err := StartServer(ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = srv.Close()
		srv.Mach.Dispose()
	})

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.NoSandbox,
		chromedp.Flag("disable-gpu", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	browserCtx, cancelTimeout := context.WithTimeout(browserCtx, 4*time.Minute)
	defer cancelTimeout()

	// browser console for diagnostics
	var (
		consoleMx sync.Mutex
		console   []string
	)
	messages := chromedp.Console(browserCtx)
	go func() {
		for msg, err := range messages {
			if err != nil {
				return
			}
			consoleMx.Lock()
			console = append(console, msg.String())
			consoleMx.Unlock()
		}
	}()
	consoleLog := func() string {
		consoleMx.Lock()
		defer consoleMx.Unlock()

		return strings.Join(console, "\n")
	}

	if err := chromedp.Do(browserCtx,
		chromedp.Navigate("http://"+srv.Addr+"/"),
		chromedp.WaitVisible("#rpc-log"),
	); err != nil {
		t.Fatalf("navigate: %s\n%s", err, consoleLog())
	}

	logText := func() string {
		txt, _ := chromedp.Run(browserCtx, chromedp.Text("#rpc-log"))
		server, _ := chromedp.Run(browserCtx, chromedp.Text("#server-log"))
		return txt + "\n-- server --\n" + server
	}
	poll := func(expr, what string, timeout time.Duration) {
		t.Helper()

		_, err := chromedp.Run(browserCtx,
			chromedp.Poll[bool](expr, chromedp.WithPollingTimeout(timeout)))
		if err != nil {
			t.Fatalf("wait for %s: %s\nlog:\n%s\nconsole:\n%s",
				what, err, logText(), consoleLog())
		}
	}
	waitText := func(sel, sub string, timeout time.Duration) {
		t.Helper()

		expr := fmt.Sprintf(`(() => { const el = document.querySelector(%q);`+
			` return !!el && el.textContent.indexOf(%q) !== -1 })()`, sel, sub)
		poll(expr, quote(sel+" "+sub), timeout)
	}
	waitLog := func(sub string, timeout time.Duration) {
		t.Helper()

		waitText("#rpc-log", sub, timeout)
	}
	waitConsole := func(sub string, timeout time.Duration) {
		t.Helper()

		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			if strings.Contains(consoleLog(), sub) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("console missing %q:\n%s", sub, consoleLog())
	}

	// the WASM app boots and connects over the multiplexed WebSocket
	waitLog("wasm: aRPC connected", time.Minute)
	// the dedicated console pkg mirrors the log to the browser console
	waitConsole("[wasm] wasm: aRPC connected", 10*time.Second)
	waitLog("wasm: remote machine ready", 30*time.Second)

	// server -> wasm over aRPC, with args (stream 1)
	waitLog("wasm received hello", 30*time.Second)

	// wasm -> server over aRPC, with args (stream 0), rendered by livepage
	waitText("#server-log", "server: hello from wasm", 30*time.Second)

	// wasm -> server over aRPC: the server-side counter was incremented
	poll(`(() => { const el = document.querySelector("#count");`+
		` return !!el && el.textContent !== "0" })()`, "aRPC incr", 30*time.Second)

	// aRPC and HTML share a single livepage WebSocket
	if got := srv.Page.Clients(); got != 1 {
		t.Fatalf("expected 1 livepage WebSocket, got %d", got)
	}

	// text frames (DOM events) still work on the same socket
	if err := chromedp.Do(browserCtx, chromedp.Click("#incr")); err != nil {
		t.Fatal(err)
	}
	poll(`(() => { const el = document.querySelector("#count");`+
		` return !!el && el.textContent === "2" })()`, "DOM click", 30*time.Second)
}

// ///// ///// /////

// ///// WASM BUILD

// ///// ///// /////

func buildWasm(t *testing.T, ctx context.Context, tinygo bool) {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "client"), 0o755); err != nil {
		t.Fatal(err)
	}

	var build *exec.Cmd
	if tinygo {
		// tinygo build -target wasm -o client/main.wasm ./client
		build = exec.CommandContext(ctx, "tinygo", "build", "-target", "wasm",
			"-buildvcs=false", "-o", "client/main.wasm", "./client")
		build.Dir = dir
		build.Env = os.Environ()
	} else {
		// go build -o client/main.wasm ./client
		build = exec.CommandContext(ctx, "go", "build",
			"-buildvcs=false", "-o", "client/main.wasm", "./client")
		build.Dir = dir
		build.Env = append(os.Environ(),
			"GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	}
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build wasm: %s\n%s", err, out)
	}

	// copy the matching wasm_exec.js
	var src string
	if tinygo {
		root, err := exec.CommandContext(ctx, "tinygo", "env",
			"TINYGOROOT").Output()
		if err != nil {
			t.Fatalf("tinygo env TINYGOROOT: %s", err)
		}
		src = filepath.Join(strings.TrimSpace(string(root)), "targets",
			"wasm_exec.js")
	} else {
		root, err := exec.CommandContext(ctx, "go", "env",
			"GOROOT").Output()
		if err != nil {
			t.Fatalf("go env GOROOT: %s", err)
		}
		src = filepath.Join(strings.TrimSpace(string(root)), "lib", "wasm",
			"wasm_exec.js")
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read wasm_exec.js: %s", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "client", "wasm_exec.js"),
		data, 0o644); err != nil {
		t.Fatalf("write wasm_exec.js: %s", err)
	}
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func findBrowser() string {
	if bin := os.Getenv("AM_WASM_BROWSER"); bin != "" {
		return bin
	}

	for _, bin := range []string{
		"chromium", "chromium-browser", "google-chrome",
		"google-chrome-stable", "chrome",
	} {
		if p, err := exec.LookPath(bin); err == nil {
			return p
		}
	}

	return ""
}

func quote(s string) string {
	return fmt.Sprintf("%q", s)
}
