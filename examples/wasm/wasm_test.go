//go:build !js && unix && integration

package example

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// envBrowser overrides the headless browser binary used by the tests.
const envBrowser = "AM_WASM_BROWSER"

const (
	// buildTimeout covers the GOOS=js GOARCH=wasm build of ./client.
	buildTimeout = 3 * time.Minute
	// startTimeout covers ./server binding the relay's HTTP server.
	startTimeout = time.Minute
)

// TestWasmExample runs the example the same way `task start` does - builds the
// WASM client, starts server-foo with the relay, then opens the page in a
// headless browser and asserts that mutations flow both ways over aRPC.
//
// The subtests share a single stack, because the example binds fixed addresses
// from .env and starting it is expensive.
func TestWasmExample(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping WASM integration test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(),
		buildTimeout+startTimeout+2*time.Minute)
	defer cancel()

	buildWasm(t, ctx)
	srv := startServer(t, ctx)
	// the relay serves ./client at RELAY_HTTP_ADDR from .env
	browser := startBrowser(t, ctx, "http://"+EnvRelayHttpAddr+"/")

	// browser-bar boots, tunnels its RPC server over WebSocket and dials
	// server-foo, all via the relay. StartState is the last step of
	// initMachines.
	t.Run("browser connects over aRPC", func(t *testing.T) {
		waitForListItem(t, ctx, browser, srv, "[bar] StartState", time.Minute)
		waitForOutput(t, ctx, srv, browser, "aRPC connected via WebSocket",
			10*time.Second)
	})

	// server-foo pushes Msg with args to browser-bar's net machine, which
	// renders it in the browser's DOM.
	t.Run("server pushes a msg to the browser", func(t *testing.T) {
		waitForListItem(t, ctx, browser, srv, "hello", 30*time.Second)
	})

	// typing into #input adds SubmitMsg to browser-bar, which renders locally
	// and pushes Msg with args to server-foo's net machine.
	t.Run("browser pushes a msg to the server", func(t *testing.T) {
		msg := fmt.Sprintf("browser-msg-%d", time.Now().UnixNano())
		submitMsg(t, ctx, browser, srv, msg)

		// rendered as "[browser-bar-xxx] browser-msg-x" - the author is the
		// mutation's source machine, with a random session suffix
		waitForListItem(t, ctx, browser, srv, msg, 30*time.Second)
		// server-foo's MsgState prints "[browser-bar-xxx] browser-msg-x"
		waitForOutput(t, ctx, srv, browser, msg, 30*time.Second)
	})
}

// ///// ///// /////

// ///// EXAMPLE STACK

// ///// ///// /////

// buildWasm compiles ./client and refreshes wasm_exec.js, like the "build"
// task does.
func buildWasm(t *testing.T, ctx context.Context) {
	t.Helper()

	dir := exampleDir(t)

	// go build -o client/main.wasm ./client
	build := exec.CommandContext(ctx, "go", "build", "-o", "client/main.wasm",
		"./client")
	build.Dir = dir
	// CGO_ENABLED defaults to 0 for js/wasm, but an inherited CGO_ENABLED=1
	// overrides that default and breaks eg os/user
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build wasm: %s\n%s", err, out)
	}

	// cp $(go env GOROOT)/lib/wasm/wasm_exec.js ./client
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
	dst := filepath.Join(dir, "client", "wasm_exec.js")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write wasm_exec.js: %s", err)
	}
}

// proc is a running child process. Its output is drained continuously - an
// unread pipe fills up and blocks the process - and kept for assertions and
// for diagnostics on failure.
type proc struct {
	name string
	cmd  *exec.Cmd
	// closed once the process exited
	exited chan struct{}
	// exit error, read after exited
	err error
	// onLine is called for each complete output line, under the lock
	onLine func(string)

	mu sync.Mutex
	// all the output so far
	out strings.Builder
	// the last, still incomplete line
	line strings.Builder
}

func (p *proc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n, err := p.out.Write(b)
	if p.onLine == nil {
		return n, err
	}

	// feed complete lines to onLine
	for _, c := range b {
		if c != '\n' {
			p.line.WriteByte(c)

			continue
		}
		p.onLine(p.line.String())
		p.line.Reset()
	}

	return n, err
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

// startServer starts server-foo with the relay, like the "start" task does,
// and waits until the relay serves the example's page. The process group is
// killed on cleanup, as `go run` spawns the compiled server binary.
func startServer(t *testing.T, ctx context.Context) *proc {
	t.Helper()

	dir := exampleDir(t)

	// rm -f tmp/*.addr
	addrs, err := filepath.Glob(filepath.Join(dir, "tmp", "*.addr"))
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addrs {
		if err := os.Remove(addr); err != nil {
			t.Fatal(err)
		}
	}

	// go run ./server
	srv := &proc{name: "server", cmd: exec.Command("go", "run", "./server")}
	srv.cmd.Dir = dir
	srv.start(t)

	// wait for the relay's HTTP server
	client := &http.Client{Timeout: time.Second}
	waitFor(t, ctx, srv, nil, "the relay at "+EnvRelayHttpAddr, startTimeout,
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

	return srv
}

// exampleDir returns the dir of this example, which is the test's working dir.
func exampleDir(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

// waitForOutput waits for a substring in the output of ./server.
func waitForOutput(
	t *testing.T, ctx context.Context, srv *proc, b *browser, sub string,
	timeout time.Duration,
) {
	t.Helper()

	waitFor(t, ctx, srv, b, "server output "+quote(sub), timeout, func() bool {
		return strings.Contains(srv.Output(), sub)
	})
}

// ///// ///// /////

// ///// BROWSER

// ///// ///// /////

// waitForListItem waits for a <li> in #list containing sub. Both machines
// render their msgs there.
func waitForListItem(
	t *testing.T, ctx context.Context, b *browser, srv *proc, sub string,
	timeout time.Duration,
) {
	t.Helper()

	const expr = `JSON.stringify(Array.from(
		document.querySelectorAll("#list li")).map(e => e.textContent))`

	waitFor(t, ctx, srv, b, "list item "+quote(sub), timeout, func() bool {
		val, err := b.cdp.eval(ctx, expr)
		if err != nil {
			t.Fatalf("read #list: %s%s", err, diag(srv, b))
		}
		var items []string
		if err := json.Unmarshal([]byte(val), &items); err != nil {
			t.Fatalf("parse #list: %s%s", err, diag(srv, b))
		}
		for _, item := range items {
			if strings.Contains(item, sub) {
				return true
			}
		}

		return false
	})
}

// submitMsg types into #input and confirms with Enter, like a user would.
func submitMsg(
	t *testing.T, ctx context.Context, b *browser, srv *proc, msg string,
) {
	t.Helper()

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	expr := fmt.Sprintf(`(() => {
		const input = document.querySelector("#input");
		if (!input) return "no-input";
		input.value = %s;
		input.dispatchEvent(new KeyboardEvent("keydown", {
			key: "Enter", bubbles: true, cancelable: true }));
		return "ok";
	})()`, data)

	val, err := b.cdp.eval(ctx, expr)
	if err != nil {
		t.Fatalf("submit msg: %s%s", err, diag(srv, b))
	}
	if val != "ok" {
		t.Fatalf("submit msg: %s%s", val, diag(srv, b))
	}
}

// browser is a running headless browser, attached via the DevTools protocol.
type browser struct {
	*proc
	cdp *cdpConn
}

// startBrowser launches a headless browser on page and attaches to it via the
// DevTools protocol.
func startBrowser(t *testing.T, ctx context.Context, page string) *browser {
	t.Helper()

	bin := browserPath(t)
	// A dedicated profile dir, removed only after the browser has fully exited,
	// not t.TempDir(): the harness cleans t.TempDir() as soon as the test
	// returns, which races the browser still flushing its Cache_Data and fails
	// with "directory not empty".
	profile, err := os.MkdirTemp("", "am-wasm-profile-")
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

	// the browser prints its DevTools endpoint to stderr, once
	devtools := make(chan string, 1)
	found := false

	b := &browser{proc: &proc{
		name: "browser",
		// nolint:gosec
		cmd: exec.Command(bin,
			"--headless=new",
			"--disable-gpu",
			"--no-first-run",
			"--no-default-browser-check",
			// keep the WASM machines ticking while the page is not visible
			"--disable-background-timer-throttling",
			"--disable-backgrounding-occluded-windows",
			"--disable-renderer-backgrounding",
			"--user-data-dir="+profile,
			"--remote-debugging-port=0",
			page,
		),
	}}
	b.onLine = func(line string) {
		const prefix = "DevTools listening on "
		if i := strings.Index(line, prefix); i >= 0 && !found {
			found = true
			devtools <- strings.TrimSpace(line[i+len(prefix):])
		}
	}
	b.start(t)

	// a browser which cannot start (eg needs --no-sandbox) exits right away
	var addr string
	select {
	case addr = <-devtools:
	case <-b.exited:
		t.Fatalf("browser exited: %s%s", b.err, diag(nil, b))
	case <-time.After(30 * time.Second):
		t.Fatalf("timeout waiting for the devtools url%s", diag(nil, b))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	target, err := pageWebSocketUrl(ctx, addr)
	if err != nil {
		t.Fatalf("devtools target: %s%s", err, diag(nil, b))
	}
	b.cdp, err = newCdpConn(ctx, target)
	if err != nil {
		t.Fatalf("devtools connect: %s%s", err, diag(nil, b))
	}
	t.Cleanup(b.cdp.Close)

	return b
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

// pageWebSocketUrl polls the DevTools target list for the example's page.
func pageWebSocketUrl(ctx context.Context, devtools string) (string, error) {
	u, err := url.Parse(devtools)
	if err != nil {
		return "", err
	}
	list := url.URL{Scheme: "http", Host: u.Host, Path: "/json"}

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			list.String(), nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var targets []struct {
				Type                 string `json:"type"`
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			decErr := json.NewDecoder(resp.Body).Decode(&targets)
			resp.Body.Close()
			if decErr == nil {
				for _, target := range targets {
					if target.Type == "page" && target.WebSocketDebuggerURL != "" {
						return target.WebSocketDebuggerURL, nil
					}
				}
			}
		}

		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// ///// ///// /////

// ///// DEVTOOLS PROTOCOL

// ///// ///// /////

// cdpConn is a minimal Chrome DevTools Protocol client - request/response for
// Runtime.evaluate, plus collected console output for diagnostics.
type cdpConn struct {
	conn   *websocket.Conn
	cancel context.CancelFunc
	err    atomic.Pointer[error]

	mu      sync.Mutex
	lastId  int
	pending map[int]chan *cdpMsg

	logsMu sync.Mutex
	logs   []string
}

type cdpMsg struct {
	Id     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// maxCdpLogs caps the collected console output.
const maxCdpLogs = 500

func newCdpConn(ctx context.Context, target string) (*cdpConn, error) {
	conn, _, err := websocket.Dial(ctx, target, nil)
	if err != nil {
		return nil, err
	}
	// no limit, the page can log a lot
	conn.SetReadLimit(-1)

	readCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	c := &cdpConn{
		conn:    conn,
		cancel:  cancel,
		pending: map[int]chan *cdpMsg{},
	}
	go c.read(readCtx)

	// console output and page errors
	if _, err := c.call(ctx, "Runtime.enable", nil); err != nil {
		c.Close()

		return nil, err
	}
	if _, err := c.call(ctx, "Log.enable", nil); err != nil {
		c.Close()

		return nil, err
	}

	return c, nil
}

func (c *cdpConn) Close() {
	c.cancel()
	_ = c.conn.Close(websocket.StatusNormalClosure, "")
}

// Logs returns the collected console output of the page.
func (c *cdpConn) Logs() string {
	c.logsMu.Lock()
	defer c.logsMu.Unlock()

	return strings.Join(c.logs, "\n")
}

// eval evaluates expr in the page and returns its string value.
func (c *cdpConn) eval(ctx context.Context, expr string) (string, error) {
	msg, err := c.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return "", err
	}

	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(msg.Result, &res); err != nil {
		return "", err
	}
	if res.ExceptionDetails != nil {
		return "", fmt.Errorf("page exception: %s", res.ExceptionDetails.Text)
	}

	var val string
	if err := json.Unmarshal(res.Result.Value, &val); err != nil {
		return "", fmt.Errorf("unexpected result %s", res.Result.Value)
	}

	return val, nil
}

func (c *cdpConn) call(
	ctx context.Context, method string, params any,
) (*cdpMsg, error) {
	if err := c.err.Load(); err != nil {
		return nil, *err
	}
	if params == nil {
		params = map[string]any{}
	}

	c.mu.Lock()
	c.lastId++
	id := c.lastId
	ch := make(chan *cdpMsg, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	data, err := json.Marshal(map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	})
	if err != nil {
		return nil, err
	}
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return nil, err
	}

	select {
	case msg := <-ch:
		if msg == nil {
			if err := c.err.Load(); err != nil {
				return nil, *err
			}

			return nil, fmt.Errorf("devtools connection closed")
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, msg.Error)
		}

		return msg, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// read dispatches responses to callers and collects console output.
func (c *cdpConn) read(ctx context.Context) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			c.fail(err)

			return
		}

		msg := &cdpMsg{}
		if err := json.Unmarshal(data, msg); err != nil {
			continue
		}

		// an event
		if msg.Id == 0 {
			c.addLog(msg)

			continue
		}

		// a response
		c.mu.Lock()
		ch := c.pending[msg.Id]
		delete(c.pending, msg.Id)
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

func (c *cdpConn) fail(err error) {
	c.err.CompareAndSwap(nil, &err)

	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

func (c *cdpConn) addLog(msg *cdpMsg) {
	var line string

	switch msg.Method {
	case "Runtime.consoleAPICalled":
		var p struct {
			Args []struct {
				Value       any    `json:"value"`
				Description string `json:"description"`
			} `json:"args"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		var parts []string
		for _, arg := range p.Args {
			if arg.Value != nil {
				parts = append(parts, fmt.Sprint(arg.Value))
			} else if arg.Description != "" {
				parts = append(parts, arg.Description)
			}
		}
		line = strings.Join(parts, " ")

	case "Log.entryAdded":
		var p struct {
			Entry struct {
				Text string `json:"text"`
			} `json:"entry"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		line = p.Entry.Text

	case "Runtime.exceptionThrown":
		var p struct {
			ExceptionDetails struct {
				Text      string `json:"text"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		line = p.ExceptionDetails.Text + " " +
			p.ExceptionDetails.Exception.Description

	default:
		return
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	c.logsMu.Lock()
	defer c.logsMu.Unlock()
	if len(c.logs) >= maxCdpLogs {
		c.logs = c.logs[1:]
	}
	c.logs = append(c.logs, line)
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// waitFor polls check until it passes, or fails the test with the collected
// output attached. browser is optional.
func waitFor(
	t *testing.T, ctx context.Context, srv *proc, b *browser, desc string,
	timeout time.Duration, check func() bool,
) {
	t.Helper()

	// a dead process will never make check pass
	dead := make(chan struct{})
	if b != nil {
		dead = b.exited
	}

	start := time.Now()
	for {
		if check() {
			return
		}
		if time.Since(start) > timeout {
			t.Fatalf("timeout waiting for %s after %s%s", desc, timeout,
				diag(srv, b))
		}

		select {
		case <-srv.exited:
			t.Fatalf("server exited early while waiting for %s: %s%s", desc,
				srv.err, diag(srv, b))
		case <-dead:
			t.Fatalf("browser exited early while waiting for %s: %s%s", desc,
				b.err, diag(srv, b))
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("ctx done waiting for %s%s", desc, diag(srv, b))
		}
	}
}

// diag renders the collected output of the example and the browser. Both are
// optional.
func diag(srv *proc, b *browser) string {
	var out string
	if srv != nil {
		out += "\n\n----- ./server -----\n" + srv.Output()
	}
	if b != nil {
		out += "\n\n----- browser -----\n" + tail(b.Output(), 50)
		if b.cdp != nil {
			out += "\n\n----- browser console -----\n" + tail(b.cdp.Logs(), 100)
		}
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

func quote(s string) string {
	return `"` + s + `"`
}
