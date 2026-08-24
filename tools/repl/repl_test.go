//go:build integration

package repl

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
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

// runArpc starts a CLI command which connects to the RPC server from (1).
// TODO avoid the shell
func runArpc(t *testing.T, addr string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{addr, "--"}, args...)

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
	runArpc(t, addr, "add", mach.Id(), "B")

	<-mach.When1(sst.B, nil)
	assert.True(t, mach.Is1(sst.B))
	assert.True(t, mach.Is1(sst.C), "B.Add should have cascaded into C")
}

func TestCli_Mutations_Remove(t *testing.T) {
	mach, addr := startServer(t)

	// prepare: activate B locally
	mach.Add1(sst.B, nil)
	<-mach.When1(sst.B, nil)

	runArpc(t, addr, "remove", mach.Id(), "B")

	<-mach.WhenNot1(sst.B, nil)
	assert.False(t, mach.Is1(sst.B))
}

// TODO test 2 machines
func TestCli_Mutations_GroupAdd(t *testing.T) {
	mach, addr := startServer(t)

	runArpc(t, addr, "group-add", "-r", "ns-.*", "B")

	<-mach.When1(sst.B, nil)
	assert.True(t, mach.Is1(sst.B))
	assert.True(t, mach.Is1(sst.C), "B.Add should have cascaded into C")
}

func TestCli_Mutations_GroupRemove(t *testing.T) {
	mach, addr := startServer(t)

	// prepare: activate B locally
	mach.Add1(sst.B, nil)
	<-mach.When1(sst.B, nil)

	runArpc(t, addr, "group-remove", "-r", "ns-.*", "B")

	<-mach.WhenNot1(sst.B, nil)
	assert.False(t, mach.Is1(sst.B))
}

func TestCli_Waiting_When(t *testing.T) {
	mach, addr := startServer(t)

	go func() {
		time.Sleep(100 * time.Millisecond)
		mach.Add1(sst.D, nil)
	}()

	runArpc(t, addr, "when", mach.Id(), "D")

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

	runArpc(t, addr, "when-not", mach.Id(), "D")

	assert.False(t, mach.Is1(sst.D))
}

func TestCli_Waiting_WhenTime(t *testing.T) {
	mach, addr := startServer(t)

	go func() {
		time.Sleep(100 * time.Millisecond)
		mach.Add1(sst.D, nil)
	}()

	runArpc(t, addr, "when-time", mach.Id(), "-s", "D", "-t", "1")

	// the machine time for D should be at least 1
	assert.GreaterOrEqual(t, mach.Time(am.S{sst.D})[0], uint64(1))
}

func TestCli_Checking_Inspect(t *testing.T) {
	mach, addr := startServer(t)
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	out := runArpc(t, addr, "inspect", mach.Id())
	assert.Contains(t, out, "ns-")
	assert.Contains(t, out, "D")
}

func TestCli_Checking_Mach(t *testing.T) {
	mach, addr := startServer(t)
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	out := runArpc(t, addr, "mach", mach.Id())
	// D.Add == {C, B}, all 3 should show up as active
	assert.Contains(t, out, "B")
	assert.Contains(t, out, "C")
	assert.Contains(t, out, "D")
}

func TestCli_Checking_Time(t *testing.T) {
	mach, addr := startServer(t)
	mach.Add1(sst.D, nil)
	<-mach.When1(sst.D, nil)

	out := runArpc(t, addr, "time", mach.Id())
	assert.NotEmpty(t, strings.TrimSpace(out))
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
	r.Addrs = []string{addr}
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
