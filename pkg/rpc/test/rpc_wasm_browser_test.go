//go:build integration && !js

package test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/gobwas/ws"

	amutils "github.com/pancsta/asyncmachine-go/internal/utils"
)

const envWasmBrowser = "AM_WASM_BROWSER"

// TestWasmRPC compiles pkg/rpc/test for GOOS=js GOARCH=wasm, serves it with a
// native gobwas/ws pairing bridge, and runs the suite in headless Chromium.
// The browser side uses a WebSocket net.Conn pair for every newTest call.
func TestWasmRPC(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Skip("skipping browser WASM tests in CI")
	}
	bin := findWasmBrowser()
	if bin == "" {
		t.Skip("chromium/chrome not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	wasmPath := filepath.Join(t.TempDir(), "rpc.test.wasm")
	buildWasmTest(t, ctx, wasmPath)

	bridge := &wsBridge{waiting: map[string]net.Conn{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/pair", bridge.handle)
	mux.HandleFunc("/wasm_exec.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, wasmExecPath(t))
	})
	mux.HandleFunc("/rpc.test.wasm", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, wasmPath)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, wasmPage)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(bin),
		chromedp.NoSandbox,
		chromedp.Headless,
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	var (
		exitCode int
		log      string
	)
	if err := chromedp.Do(browserCtx, chromedp.Navigate(srv.URL+"/")); err != nil {
		t.Fatalf("navigate: %s", err)
	}
	if _, err := chromedp.Run(browserCtx,
		chromedp.Poll[bool](`window.__done === true`,
			chromedp.WithPollingTimeout(9*time.Minute)),
	); err != nil {
		log, _ = chromedp.Run(browserCtx,
			chromedp.Text(chromedp.ID("log")))
		t.Fatalf("browser WASM suite: %s\n%s", err, log)
	}

	exitCode, err := chromedp.Run(browserCtx,
		chromedp.Evaluate[int](`window.__exitCode ?? -1`))
	if err != nil {
		t.Fatal(err)
	}
	log, err = chromedp.Run(browserCtx,
		chromedp.Text(chromedp.ID("log")))
	if err != nil {
		t.Fatal(err)
	}

	if exitCode != 0 {
		t.Fatalf("browser WASM suite exit code %d\n%s", exitCode, log)
	}
	if os.Getenv("AM_WASM_VERBOSE") != "" {
		t.Log("\n" + log)
	}
	if !strings.Contains(log, "PASS") {
		t.Fatalf("browser WASM suite did not report PASS\n%s", log)
	}
	if strings.Contains(log, "--- FAIL") {
		t.Fatalf("browser WASM suite failed\n%s", log)
	}
}

// ///// ///// /////

// ///// BRIDGE

// ///// ///// /////

type wsBridge struct {
	mu      sync.Mutex
	waiting map[string]net.Conn
}

func (b *wsBridge) handle(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	conn, rw, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return
	}
	nc, err := amutils.NewServerWSConn(conn, rw)
	if err != nil {
		_ = conn.Close()
		return
	}

	b.mu.Lock()
	other, ok := b.waiting[id]
	if ok {
		delete(b.waiting, id)
	} else {
		b.waiting[id] = nc
	}
	b.mu.Unlock()

	if !ok {
		// wait for the pair, but do not leak a dangling half
		time.AfterFunc(30*time.Second, func() {
			b.mu.Lock()
			if b.waiting[id] == nc {
				delete(b.waiting, id)
				_ = nc.Close()
			}
			b.mu.Unlock()
		})
		return
	}

	go pipeConns(nc, other)
}

func pipeConns(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		_ = a.Close()
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, a)
		_ = b.Close()
	}()
	wg.Wait()
}

// ///// ///// /////

// ///// BUILD

// ///// ///// /////

func buildWasmTest(t *testing.T, ctx context.Context, out string) {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))

	cmd := exec.CommandContext(ctx, "go", "test", "-c", "-o", out,
		"./pkg/rpc/test")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	if buf, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build wasm test: %s\n%s", err, buf)
	}
}

func wasmExecPath(t *testing.T) string {
	t.Helper()

	p := filepath.Join(runtime.GOROOT(), "lib", "wasm", "wasm_exec.js")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("wasm_exec.js: %s", err)
	}

	return p
}

func findWasmBrowser() string {
	for _, env := range []string{envWasmBrowser, "CHROME_BIN"} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	for _, p := range []string{
		"/usr/bin/chromium",
		"/usr/sbin/chromium",
		"/usr/bin/chromium-browser",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/brave-browser",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

const wasmPage = `<!doctype html>
<html>
<head><title>aRPC WASM tests</title></head>
<body>
<pre id="log"></pre>
<script src="/wasm_exec.js"></script>
<script>
(() => {
  const logEl = document.getElementById("log");
  const append = (...args) => {
    logEl.textContent += args.map(a => String(a)).join(" ") + "\n";
  };
  const consoleLog = console.log.bind(console);
  console.log = (...args) => { consoleLog(...args); append(...args); };
  const consoleErr = console.error.bind(console);
  console.error = (...args) => { consoleErr(...args); append(...args); };

  window.__AM_WS_BRIDGE = "ws://" + location.host + "/pair";
  window.__done = false;
  window.__exitCode = null;

  const go = new Go();
  go.argv = ["rpc.test", "-test.v", "-test.timeout=10m"];
  go.env = { AM_TEST_RUNNER: "1" };
  go.exit = (code) => { window.__exitCode = code; };

  WebAssembly.instantiateStreaming(fetch("/rpc.test.wasm"), go.importObject)
    .then(({instance}) => go.run(instance))
    .then(() => { window.__done = true; })
    .catch((err) => {
      append("fatal: " + err);
      if (window.__exitCode === null) window.__exitCode = -1;
      window.__done = true;
    });
})();
</script>
</body>
</html>
`
