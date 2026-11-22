//go:build integration

package repl

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"

	sst "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
)

// TODO TUI completion tests for
//  - fix REPL tests and "getCursorPos() not supported by terminal emulator"
//  - no inactive states for "add"
//  - registered arg names showing in completion
//    - add typed args to the mock machine, with a REPL mapper
//  - multiple cmds with args with completion working ok
//    - instead of "--val --args count not equal"

var arpcBin string

func TestMain(m *testing.M) {
	// build the arpc binary once for all tests to speed up execution
	arpcBin = filepath.Join(os.TempDir(), "arpc-test")
	cmd := exec.Command("go", "build", "-o", arpcBin,
		"github.com/pancsta/asyncmachine-go/tools/cmd/arpc")
	if err := cmd.Run(); err != nil {
		panic(err)
	}
	defer os.Remove(arpcBin)
	os.Exit(m.Run())
}

// startServer inits a state machine (with the A/B/C/D relations from
// utils.NewRels) and an RPC server for it to be reached via arpc.
func startServer(t *testing.T) (*am.Machine, string) {
	t.Helper()
	mach := utils.NewRelsNetSrc(t, nil)
	addrCh := make(chan string, 1)
	err := rpc.MachRepl(mach, "127.0.0.1:0", &rpc.ReplOpts{
		AddrCh:            addrCh,
		InternalForceTest: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	addr := <-addrCh
	return mach, addr
}

// ///// ///// /////

// ///// CLI

// ///// ///// /////

// runArpc starts a CLI command which connects to the RPC servers from addrs.
// Multiple addresses are loaded through a temporary addr directory.
// TODO avoid the shell
func runArpc(t *testing.T, addrs []string, args ...string) string {
	t.Helper()
	if len(addrs) == 0 {
		t.Fatal("no addresses")
	}

	connArgs := []string{addrs[0]}
	if len(addrs) > 1 {
		dir := t.TempDir()
		for i, addr := range addrs {
			path := filepath.Join(dir, strconv.Itoa(i)+".addr")
			if err := os.WriteFile(path, []byte(addr), 0o600); err != nil {
				t.Fatalf("write addr file: %v", err)
			}
		}
		connArgs = []string{"-d", dir}
	}

	cmdArgs := make([]string, 0, len(connArgs)+len(args)+1)
	cmdArgs = append(cmdArgs, connArgs...)
	cmdArgs = append(cmdArgs, "--")
	cmdArgs = append(cmdArgs, args...)

	// avoid hanging tests
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, arpcBin, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("arpc command failed: %v\nOutput: %s", err, out)
	}
	t.Logf("arpc output: %s", out)

	return string(out)
}

func TestCli_Mutations_Add(t *testing.T) {
	mach, addr := startServer(t)

	// B has no Require, so it can be added right away and cascades into C
	// (B.Add == {C}).
	runArpc(t, []string{addr}, "add", mach.Id(), "B")

	<-mach.When1(sst.B, nil)
	assert.True(t, mach.Is1(sst.B))
	assert.True(t, mach.Is1(sst.C), "B.Add should have cascaded into C")
}

func TestCli_Mutations_Remove(t *testing.T) {
	mach, addr := startServer(t)

	// prepare: activate B locally
	mach.Add1(sst.B, nil)
	<-mach.When1(sst.B, nil)

	runArpc(t, []string{addr}, "remove", mach.Id(), "B")

	<-mach.WhenNot1(sst.B, nil)
	assert.False(t, mach.Is1(sst.B))
}

func TestCli_Mutations_Set(t *testing.T) {
	mach, addr := startServer(t)

	// prepare: activate D locally
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	runArpc(t, []string{addr}, CmdSet.Value, mach.Id(), "B")

	<-mach.When1(sst.B, nil)
	assert.True(t, mach.Is1(sst.B))
	assert.True(t, mach.Is1(sst.C), "B.Add should have cascaded into C")
	assert.False(t, mach.Is1(sst.D))
}

func TestCli_Mutations_GroupAdd(t *testing.T) {
	mach1, addr1 := startServer(t)
	mach2, addr2 := startServer(t)

	runArpc(t, []string{addr1, addr2}, CmdGroupAdd.Value, "-r", "ns-.*", "B")

	for _, mach := range []*am.Machine{mach1, mach2} {
		<-mach.When1(sst.B, nil)
		assert.True(t, mach.Is1(sst.B))
		assert.True(t, mach.Is1(sst.C), "B.Add should have cascaded into C")
	}
}

func TestCli_Mutations_GroupRemove(t *testing.T) {
	mach, addr := startServer(t)

	// prepare: activate B locally
	mach.Add1(sst.B, nil)
	<-mach.When1(sst.B, nil)

	runArpc(t, []string{addr}, "group-remove", "-r", "ns-.*", "B")

	<-mach.WhenNot1(sst.B, nil)
	assert.False(t, mach.Is1(sst.B))
}

func TestCli_Mutations_GroupSet(t *testing.T) {
	mach1, addr1 := startServer(t)
	mach2, addr2 := startServer(t)

	// prepare: activate D locally on both machines
	for _, mach := range []*am.Machine{mach1, mach2} {
		mach.Add1(sst.D, nil)
		<-mach.When1(sst.D, nil)
	}

	runArpc(t, []string{addr1, addr2}, CmdGroupSet.Value, "-r", "ns-.*", "B")

	for _, mach := range []*am.Machine{mach1, mach2} {
		<-mach.When1(sst.B, nil)
		assert.True(t, mach.Is1(sst.B))
		assert.True(t, mach.Is1(sst.C), "B.Add should have cascaded into C")
		assert.False(t, mach.Is1(sst.D))
	}
}

func TestCli_Waiting_When(t *testing.T) {
	mach, addr := startServer(t)

	go func() {
		time.Sleep(100 * time.Millisecond)
		mach.Add1(sst.D, nil)
	}()

	runArpc(t, []string{addr}, "when", mach.Id(), "D")

	assert.True(t, mach.Is1(sst.D))
	// D.Add == {C, B}, so both should be active as a side effect
	assert.True(t, mach.Is1(sst.C))
	assert.True(t, mach.Is1(sst.B))
}

func TestCli_Waiting_WhenNot(t *testing.T) {
	mach, addr := startServer(t)

	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	go func() {
		time.Sleep(100 * time.Millisecond)
		mach.Remove1(sst.D, nil)
	}()

	runArpc(t, []string{addr}, "when-not", mach.Id(), "D")

	assert.False(t, mach.Is1(sst.D))
}

func TestCli_Waiting_WhenTime(t *testing.T) {
	mach, addr := startServer(t)

	go func() {
		time.Sleep(100 * time.Millisecond)
		mach.Add1(sst.D, nil)
	}()

	runArpc(t, []string{addr}, "when-time", mach.Id(), "-s", "D", "-t", "1")

	// the machine time for D should be at least 1
	assert.GreaterOrEqual(t, mach.Time(am.S{sst.D})[0], uint64(1))
}

func TestCli_Checking_Inspect(t *testing.T) {
	mach, addr := startServer(t)
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	out := runArpc(t, []string{addr}, "inspect", mach.Id())
	assert.Contains(t, out, "ns-")
	assert.Contains(t, out, "D")
}

func TestCli_Checking_Mach(t *testing.T) {
	mach, addr := startServer(t)
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	out := runArpc(t, []string{addr}, "mach", mach.Id())
	// D.Add == {C, B}, all 3 should show up as active
	assert.Contains(t, out, "B")
	assert.Contains(t, out, "C")
	assert.Contains(t, out, "D")
}

func TestCli_Checking_Time(t *testing.T) {
	mach, addr := startServer(t)
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	out := runArpc(t, []string{addr}, "time", mach.Id())
	assert.NotEmpty(t, strings.TrimSpace(out))
}

func TestReplConnectAddr(t *testing.T) {
	_, addr1 := startServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r, err := New(ctx, "test-connect")
	require.NoError(t, err)
	defer r.Mach.Dispose()

	r.Addrs = []NamedAddr{{Name: "CLI0", Addr: addr1}}
	r.Mach.Add1(ss.Start, nil)

	select {
	case <-r.Mach.When1(ss.ConnectedFully, nil):
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the first connection")
	}

	_, addr2 := startServer(t)
	connect := func(addr string) {
		t.Helper()
		cmd := ReplCmds(r)[0]
		cmd.SetArgs([]string{addr})
		require.NoError(t, cmd.Execute())
	}

	connect(addr2)
	require.Eventually(t, func() bool {
		return len(r.addrs()) == 2
	}, 3*time.Second, 50*time.Millisecond)

	require.Eventually(t, func() bool {
		clients := r.clients()
		if len(clients) != 2 {
			return false
		}
		for _, c := range clients {
			if c.Mach.Not1(ssrpc.ClientStates.Ready) {
				return false
			}
		}
		return true
	}, 5*time.Second, 50*time.Millisecond, "second client not ready")

	addrs := r.addrs()
	require.Len(t, addrs, 2)
	assert.Equal(t, "CLI1", addrs[1].Name)
	assert.Equal(t, addr2, addrs[1].Addr)
	assert.True(t, addrs[1].Manual)

	// duplicate address is not added twice
	connect(addr2)
	assert.Len(t, r.addrs(), 2)

	// watched address updates keep manual addresses
	r.reconcileClients(nil, []NamedAddr{{Name: "CLI0", Addr: addr1}})
	assert.Len(t, r.addrs(), 2)
}

// ///// ///// /////

// ///// REPL

// ///// ///// /////

func runRepl(t *testing.T, addr string, commands []string) string {
	t.Helper()
	// 1. Create pseudo-terminal master/slave pair
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("failed to open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	// Put slave into raw mode so readline can read without line buffering
	oldState, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		t.Fatalf("failed to set raw mode: %v", err)
	}
	defer func() {
		_ = term.Restore(int(tty.Fd()), oldState)
	}()

	// 2. Redirect standard OS streams to the slave TTY
	oldStdin, oldStdout, oldStderr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = tty, tty, tty
	defer func() {
		os.Stdin, os.Stdout, os.Stderr = oldStdin, oldStdout, oldStderr
	}()

	// 3. Initialize console application and test command
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r, err := New(ctx, "test-repl")
	if err != nil {
		t.Fatalf("failed to create repl: %v", err)
	}

	rootCmd := NewRootCommand(r, nil, nil)
	for _, c := range ReplCmds(r) {
		rootCmd.AddCommand(c)
	}
	r.Cmd = rootCmd
	r.Addrs = []NamedAddr{{Name: "CLI0", Addr: addr}}
	r.Mach.Add1(ss.Start, nil)

	// Wait for connection
	select {
	case <-r.Mach.When1(ss.ConnectedFully, nil):
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connection")
	}

	r.Mach.Add1(ss.ReplMode, nil)

	// 5. Asynchronously drain output from the master terminal side and reply to DSR
	var buf bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		b := make([]byte, 1024)
		for {
			n, err := ptmx.Read(b)
			if n > 0 {
				buf.Write(b[:n])
				if bytes.Contains(b[:n], []byte("\x1b[6n")) {
					_, _ = ptmx.WriteString("\x1b[1;1R")
				}
			}
			if err != nil {
				break
			}
		}
		close(readDone)
	}()

	// Allow readline prompt initialization time
	time.Sleep(100 * time.Millisecond)

	// 6. Send simulated keypresses/commands into the master terminal
	for _, cmd := range commands {
		if _, err := ptmx.WriteString(cmd + "\r"); err != nil {
			t.Fatalf("failed writing command to ptmx: %v", err)
		}
		// Wait for command execution and render cycle
		time.Sleep(200 * time.Millisecond)
	}

	// 7. Trigger shutdown and unblock output reader
	cancel()
	r.Mach.Dispose()
	_ = ptmx.Close()
	select {
	case <-readDone:
	case <-time.After(500 * time.Millisecond):
		// timeout waiting for io.Copy to finish
	}

	return buf.String()
}

func TestRepl__Mutations_Add(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)

	// in TUI mode, we just type the command without `arpc` prefix
	out := runRepl(t, addr, []string{
		"add " + mach.Id() + " B",
	})

	select {
	case <-mach.When1(sst.B, nil):
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for state B. Output: %s", out)
	}
	assert.True(t, mach.Is1(sst.B))
	assert.True(t, mach.Is1(sst.C), "B.Add should have cascaded into C")
}

func TestRepl__Mutations_Remove(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)

	// prepare: activate B locally
	mach.Add1(sst.B, nil)
	<-mach.When1(sst.B, nil)

	out := runRepl(t, addr, []string{
		"remove " + mach.Id() + " B",
	})

	select {
	case <-mach.WhenNot1(sst.B, nil):
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for not state B. Output: %s", out)
	}
	assert.False(t, mach.Is1(sst.B))
}

func TestRepl__Mutations_GroupAdd(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)

	out := runRepl(t, addr, []string{
		"group-add -r ns-.* B",
	})

	select {
	case <-mach.When1(sst.B, nil):
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for state B. Output: %s", out)
	}
	assert.True(t, mach.Is1(sst.B))
	assert.True(t, mach.Is1(sst.C), "B.Add should have cascaded into C")
}

func TestRepl__Mutations_GroupRemove(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)

	// prepare: activate B locally
	mach.Add1(sst.B, nil)
	<-mach.When1(sst.B, nil)

	out := runRepl(t, addr, []string{
		"group-remove -r ns-.* B",
	})

	select {
	case <-mach.WhenNot1(sst.B, nil):
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for not state B. Output: %s", out)
	}
	assert.False(t, mach.Is1(sst.B))
}

func TestRepl__Waiting_When(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)

	go func() {
		time.Sleep(100 * time.Millisecond)
		mach.Add1(sst.D, nil)
	}()

	runRepl(t, addr, []string{
		"when " + mach.Id() + " D",
	})

	assert.True(t, mach.Is1(sst.D))
	// D.Add == {C, B}, so both should be active as a side effect
	assert.True(t, mach.Is1(sst.C))
	assert.True(t, mach.Is1(sst.B))
}

func TestRepl__Waiting_WhenNot(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)

	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	go func() {
		time.Sleep(100 * time.Millisecond)
		mach.Remove1(sst.D, nil)
	}()

	runRepl(t, addr, []string{
		"when-not " + mach.Id() + " D",
	})

	assert.False(t, mach.Is1(sst.D))
}

func TestRepl__Waiting_WhenTime(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)

	go func() {
		time.Sleep(100 * time.Millisecond)
		mach.Add1(sst.D, nil)
	}()

	runRepl(t, addr, []string{
		"when-time " + mach.Id() + " -s D -t 1",
	})

	// the machine time for D should be at least 1
	assert.GreaterOrEqual(t, mach.Time(am.S{sst.D})[0], uint64(1))
}

func TestRepl__Checking_Inspect(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	out := runRepl(t, addr, []string{
		"inspect " + mach.Id(),
	})

	assert.Contains(t, out, "ns-")
	assert.Contains(t, out, "D")
}

func TestRepl__Checking_Mach(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	out := runRepl(t, addr, []string{
		"mach " + mach.Id(),
	})
	// D.Add == {C, B}, all 3 should show up as active
	assert.Contains(t, out, "B")
	assert.Contains(t, out, "C")
	assert.Contains(t, out, "D")
}

func TestRepl__Checking_Time(t *testing.T) {
	t.Skip("flaky")

	mach, addr := startServer(t)
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	out := runRepl(t, addr, []string{
		"time " + mach.Id(),
	})
	assert.NotEmpty(t, strings.TrimSpace(out))
}

// ///// ///// /////

// ///// LIVEPAGE

// ///// ///// /////

// startLivepage starts arpc in livepage mode and returns its HTTP base URL.
func startLivepage(t *testing.T, rpcAddr string) (
	string, *exec.Cmd, *bytes.Buffer, *bytes.Buffer,
) {
	t.Helper()

	lpAddr := "localhost:" + utils.RandPort(56001, 57000)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, arpcBin, rpcAddr,
		"--livepage", "--livepage-addr", lpAddr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())

	t.Cleanup(func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	return "http://" + lpAddr, cmd, &stdout, &stderr
}

func TestLivepageUI(t *testing.T) {
	_, rpcAddr := startServer(t)
	base, cmd, stdout, stderr := startLivepage(t, rpcAddr)

	get := func(path string) (int, string) {
		res, err := http.Get(base + path)
		if err != nil {
			return 0, ""
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return res.StatusCode, ""
		}
		return res.StatusCode, string(body)
	}

	var page string
	require.Eventually(t, func() bool {
		code, body := get("/")
		page = body
		return code == http.StatusOK
	}, 15*time.Second, 100*time.Millisecond, "livepage not served")

	assert.Contains(t, page, `class="tcell-screen"`)
	assert.Contains(t, page, `class="tcell-grid"`)
	assert.Contains(t, page, `data-ws="/ws"`)

	code, _ := get("/assets/livepage.js")
	assert.Equal(t, http.StatusOK, code)

	// /livepage endpoints are gone
	code, _ = get("/livepage/")
	assert.Equal(t, http.StatusNotFound, code)

	// the parent must not run the REPL on its TTY
	assert.NotContains(t, stdout.String(), "arpc>")
	assert.Contains(t, stderr.String(), "Livepage: http://")

	// the parent and its PTY child must still be running
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, cmd.Process.Signal(syscall.Signal(0)))
}

// TestLivepageReconnect checks that a fresh WebSocket receives the current
// terminal grid, without a preceding page load.
func TestLivepageReconnect(t *testing.T) {
	_, rpcAddr := startServer(t)
	base, _, _, _ := startLivepage(t, rpcAddr)
	lpAddr := base[len("http://"):]

	// wait for the HTTP server without loading the page (which would refresh
	// the cached HTML and hide a stale frame)
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", lpAddr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 10*time.Second, 100*time.Millisecond)

	// allow the child REPL to render its prompt
	time.Sleep(1500 * time.Millisecond)

	readFirst := func() string {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, _, err := websocket.Dial(ctx, "ws://"+lpAddr+"/ws", nil)
		require.NoError(t, err)
		defer conn.CloseNow()

		_, data, err := conn.Read(ctx)
		require.NoError(t, err)
		return string(data)
	}

	for i := 1; i <= 2; i++ {
		html := readFirst()
		assert.Contains(t, html, "arpc", "connection %d got stale HTML", i)
	}
}

func TestLivepageCliConflict(t *testing.T) {
	_, rpcAddr := startServer(t)

	cmd := exec.Command(arpcBin, rpcAddr, "--livepage", "--", "status")
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(out), "livepage is not supported in CLI mode")
}
