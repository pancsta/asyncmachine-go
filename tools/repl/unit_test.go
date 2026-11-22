package repl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

func TestPromptStr(t *testing.T) {
	tests := []struct {
		name           string
		connectedFully bool
		connected      bool
		wantColor      string
	}{
		{"all connected", true, true, promptColorGreen},
		{"some connected", false, true, promptColorYellow},
		{"none connected", false, false, promptColorGrey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.wantColor + "arpc>" + promptColorReset + " "
			if got := promptStr(tt.connectedFully, tt.connected); got != want {
				t.Fatalf("promptStr(%t, %t) = %q, want %q",
					tt.connectedFully, tt.connected, got, want)
			}
		})
	}
}

func TestValidateConnectAddr(t *testing.T) {
	valid := []string{
		"localhost:6452",
		"127.0.0.1:6452",
		"localhost:6452/ws",
		"[::1]:6452",
		"[::1]:6452/ws",
	}
	for _, addr := range valid {
		assert.NoError(t, validateConnectAddr(addr), addr)
	}

	invalid := []string{
		"",
		"localhost",
		":6452",
		"localhost:",
		"ws://localhost:6452",
	}
	for _, addr := range invalid {
		assert.Error(t, validateConnectAddr(addr), addr)
	}
}

func TestNextCLIName(t *testing.T) {
	assert.Equal(t, "CLI0", nextCLIName(nil))
	assert.Equal(t, "CLI1", nextCLIName([]NamedAddr{{Name: "CLI0"}}))
	assert.Equal(t, "CLI1", nextCLIName([]NamedAddr{
		{Name: "file.addr"},
		{Name: "CLI0"},
	}))
	assert.Equal(t, "CLI2", nextCLIName([]NamedAddr{
		{Name: "CLI0"},
		{Name: "CLI1"},
	}))
}

func TestMergeManualAddrs(t *testing.T) {
	watched := []NamedAddr{
		{Name: "a.addr", Addr: "localhost:1"},
	}
	manual := []NamedAddr{
		{Name: "CLI0", Addr: "localhost:2", Manual: true},
		{Name: "CLI1", Addr: "localhost:1", Manual: true}, // duplicate addr
	}

	merged := mergeManualAddrs(watched, manual)
	require.Len(t, merged, 2)
	assert.Equal(t, "a.addr", merged[0].Name)
	assert.Equal(t, "CLI0", merged[1].Name)
	assert.True(t, merged[1].Manual)
}

func TestCmdNames(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range CmdNames.Members() {
		assert.NotEmpty(t, name.Value)
		assert.False(t, seen[name.Value], "duplicate command %q", name.Value)
		seen[name.Value] = true
	}

	assert.True(t, CmdNames.Contains(CmdName{"add"}))
	assert.False(t, CmdNames.Contains(CmdName{"nope"}))
}

func TestCmdNamesCoverRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r, err := New(ctx, "test-cmd-names")
	require.NoError(t, err)
	defer r.Mach.Dispose()

	root := NewRootCommand(r, nil, nil)
	for _, cmd := range root.Commands() {
		assert.True(t, CmdNames.Contains(cmdName(cmd)),
			"command %q missing from CmdNames", cmd.Name())
	}
}

func TestConnStatus(t *testing.T) {
	tests := []struct {
		ready, total int
		connected    bool
		fully        bool
	}{
		{0, 0, false, false},
		{0, 1, false, false},
		{1, 1, true, true},
		{1, 2, true, false},
		{2, 2, true, true},
	}

	for _, tt := range tests {
		connected, fully := connStatus(tt.ready, tt.total)
		assert.Equal(t, tt.connected, connected,
			"connected %d/%d", tt.ready, tt.total)
		assert.Equal(t, tt.fully, fully, "fully %d/%d", tt.ready, tt.total)
	}
}

func newTestClient(name, addr string) *replClient {
	return &replClient{
		Name:   name,
		Client: &arpc.Client{Addr: addr},
	}
}

func TestPlanReconcile(t *testing.T) {
	t.Run("unchanged", func(t *testing.T) {
		old := []*replClient{
			newTestClient("a.addr", "localhost:1"),
			newTestClient("b.addr", "localhost:2"),
		}
		keep, removed := planReconcile(old, []NamedAddr{
			{Name: "a.addr", Addr: "localhost:1"},
			{Name: "b.addr", Addr: "localhost:2"},
		})

		require.Len(t, keep, 2)
		assert.Same(t, old[0], keep[0])
		assert.Same(t, old[1], keep[1])
		assert.Empty(t, removed)
	})

	t.Run("renamed file, same addr", func(t *testing.T) {
		old := []*replClient{
			newTestClient("a.addr", "localhost:1"),
		}
		keep, removed := planReconcile(old, []NamedAddr{
			{Name: "renamed.addr", Addr: "localhost:1"},
		})

		require.Len(t, keep, 1)
		assert.Same(t, old[0], keep[0])
		assert.Empty(t, removed)

		// renaming keeps the connection, without mutating the old client
		renamed := renameClient(keep[0], "renamed.addr")
		assert.Equal(t, "renamed.addr", renamed.Name)
		assert.Same(t, old[0].Client, renamed.Client)
		assert.Equal(t, "a.addr", keep[0].Name)
	})

	t.Run("changed addr, same name", func(t *testing.T) {
		old := []*replClient{
			newTestClient("a.addr", "localhost:1"),
		}
		keep, removed := planReconcile(old, []NamedAddr{
			{Name: "a.addr", Addr: "localhost:2"},
		})

		require.Len(t, keep, 1)
		assert.Nil(t, keep[0])
		require.Len(t, removed, 1)
		assert.Same(t, old[0], removed[0])
	})

	t.Run("added and removed", func(t *testing.T) {
		old := []*replClient{
			newTestClient("a.addr", "localhost:1"),
		}
		keep, removed := planReconcile(old, []NamedAddr{
			{Name: "b.addr", Addr: "localhost:2"},
		})

		require.Len(t, keep, 1)
		assert.Nil(t, keep[0])
		require.Len(t, removed, 1)
		assert.Same(t, old[0], removed[0])
	})

	t.Run("duplicate addr", func(t *testing.T) {
		old := []*replClient{
			newTestClient("a.addr", "localhost:1"),
		}
		keep, removed := planReconcile(old, []NamedAddr{
			{Name: "a.addr", Addr: "localhost:1"},
			{Name: "b.addr", Addr: "localhost:1"},
		})

		require.Len(t, keep, 2)
		assert.Same(t, old[0], keep[0])
		assert.Nil(t, keep[1])
		assert.Empty(t, removed)
	})
}

func TestNamedAddrsFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mymach.addr")
	require.NoError(t, os.WriteFile(path, []byte(" localhost:1234 \n"), 0o600))

	addrs, err := namedAddrsFromFile(path)
	require.NoError(t, err)
	require.Len(t, addrs, 1)
	assert.Equal(t, "mymach.addr", addrs[0].Name)
	assert.Equal(t, "localhost:1234", addrs[0].Addr)

	// empty file
	require.NoError(t, os.WriteFile(path, []byte(" \n"), 0o600))
	addrs, err = namedAddrsFromFile(path)
	require.NoError(t, err)
	assert.Empty(t, addrs)
}

func TestAddrsFromDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "b.addr"), []byte("localhost:2"), 0o600))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "a.addr"), []byte("localhost:1"), 0o600))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "empty.addr"), []byte(" \n"), 0o600))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "ignored.txt"), []byte("localhost:3"), 0o600))

	addrs, err := addrsFromDir(dir)
	require.NoError(t, err)
	require.Len(t, addrs, 2)

	// lexical order, empty and non-.addr skipped
	assert.Equal(t, NamedAddr{Name: "a.addr", Addr: "localhost:1"}, addrs[0])
	assert.Equal(t, NamedAddr{Name: "b.addr", Addr: "localhost:2"}, addrs[1])
}
