package debugger

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/ssh"
	"github.com/gdamore/tcell/v2"
	"github.com/gdamore/tcell/v2/terminfo"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/pancsta/asyncmachine-go/tools/debugger/states"

	ammcp "github.com/pancsta/asyncmachine-go/pkg/integrations/mcp"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

const (
	// TODO customize
	playInterval = 500 * time.Millisecond
	// TODO add param --max-clients
	maxClients            = 5000
	timeFormat            = "15:04:05.000000000"
	fastJumpAmount        = 50
	arrowThrottleMs       = 200
	logUpdateDebounce     = 300 * time.Millisecond
	sidebarUpdateDebounce = time.Second
	searchAsTypeWindow    = 1500 * time.Millisecond
	heartbeatInterval     = 1 * time.Minute
	// heartbeatInterval = 5 * time.Second
	// msgMaxAge             = 0

	// maxMemMb = 100
	maxMemMb        = 50
	msgMaxThreshold = 300
	// max txs queued for scrolling the timelines
	scrollTxThrottle = 3
	logFile          = "log.md"
)

type Client struct {
	*server.Client

	CursorTx1       int
	ReaderCollapsed bool
	CursorStep1     int
	SelectedState   string
	// extracted log entries per tx ID
	// TODO GC when all entries are closedAt and the first client's tx is later
	//  than the latest closedAt; whole tx needs to be disposed at the same time
	LogReader map[string][]*types.LogReaderEntry

	logRenderedCursor1    int
	logRenderedLevel      am.LogLevel
	logRenderedFilters    *types.Filters
	logRenderedGroup      string
	logRenderedTimestamps bool
	logReaderMx           sync.Mutex
}

func init() {
	gob.Register(server.Exportable{})
	gob.Register(am.Relation(0))
}

func newClient(id, connId, schemaHash string, data *server.Exportable) *Client {
	c := &Client{
		Client: &server.Client{
			Id:         id,
			ConnId:     connId,
			SchemaHash: schemaHash,
			Exportable: data,
		},
		LogReader: make(map[string][]*types.LogReaderEntry),
	}
	c.ParseSchema()

	return c
}

func (c *Client) AddReaderEntry(txId string, entry *types.LogReaderEntry) int {
	c.logReaderMx.Lock()
	defer c.logReaderMx.Unlock()

	c.LogReader[txId] = append(c.LogReader[txId], entry)

	return len(c.LogReader[txId]) - 1
}

func (c *Client) GetReaderEntry(txId string, idx int) *types.LogReaderEntry {
	c.logReaderMx.Lock()
	defer c.logReaderMx.Unlock()

	ptrTx, ok := c.LogReader[txId]
	if !ok || idx >= len(ptrTx) {
		return nil
	}

	return ptrTx[idx]
}

// ///// ///// /////

// ///// SSH

// ///// ///// /////

func NewSessionScreen(s ssh.Session) (tcell.Screen, error) {
	pi, ch, ok := s.Pty()
	if !ok {
		return nil, errors.New("no pty requested")
	}
	ti, err := terminfo.LookupTerminfo(pi.Term)
	if err != nil {
		return nil, err
	}
	screen, err := tcell.NewTerminfoScreenFromTtyTerminfo(&tty{
		Session: s,
		size:    pi.Window,
		ch:      ch,
	}, ti)
	if err != nil {
		return nil, err
	}
	return screen, nil
}

type tty struct {
	ssh.Session
	size     ssh.Window
	ch       <-chan ssh.Window
	resizecb func()
	mu       sync.Mutex
}

func (t *tty) Start() error {
	go func() {
		for win := range t.ch {
			t.mu.Lock()
			t.size = win
			t.notifyResize()
			t.mu.Unlock()
		}
	}()
	return nil
}

func (t *tty) Stop() error {
	return nil
}

func (t *tty) Drain() error {
	return nil
}

func (t *tty) WindowSize() (window tcell.WindowSize, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return tcell.WindowSize{
		Width:  t.size.Width,
		Height: t.size.Height,
	}, nil
}

func (t *tty) NotifyResize(cb func()) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.resizecb = cb
}

func (t *tty) notifyResize() {
	if t.resizecb != nil {
		t.resizecb()
	}
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

// openURL opens the specified URL in the default browser of the user.
// https://gist.github.com/sevkin/9798d67b2cb9d07cb05f89f14ba682f8
func openURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd.exe"
		args = []string{
			"/c", "rundll32", "url.dll,FileProtocolHandler",
			strings.ReplaceAll(url, "&", "^&"),
		}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		if isWSL() {
			cmd = "cmd.exe"
			args = []string{"start", url}
		} else {
			cmd = "xdg-open"
			args = []string{url}
		}
	}

	e := exec.Command(cmd, args...)
	err := e.Start()
	if err != nil {
		return err
	}
	err = e.Wait()
	if err != nil {
		return err
	}

	return nil
}

// isWSL checks if the Go program is running inside Windows Subsystem for Linux
func isWSL() bool {
	releaseData, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(releaseData)), "microsoft")
}

// ///// ///// /////

// ///// MCP SERVER

// ///// ///// /////

type mcpServer struct {
	*ammcp.Server
	d *Debugger
}

func newMcpServer(d *Debugger) (*mcpServer, error) {
	srv, err := ammcp.New(d.Mach, ammcp.Opts{
		Name: "am-dbg",
		Desc: "MCP to control the asyncmachine-go TUI debugger. " +
			"The double-line border panel is currently focused. " +
			"Most features are accessible via ToggleTool.",
		MutCallback: func(ctx context.Context) error {
			ok := d.Mach.Eval("MCPTool", func() {
				d.hRedrawFull(false)
			}, ctx)
			if !ok {
				return am.ErrEvalTimeout
			}
			return nil
		},
		StatesInclude:  states.DebuggerGroups.Mcp,
		StatesReadonly: states.DebuggerGroups.McpReadonly,
		Args:           types.ArgsRpc,
		StateCalls:     types.StateCalls,
		Version:        d.params.Version,
	})
	if err != nil {
		return nil, err
	}

	// bind keys from keyboard.go
	keys := slices.Collect(maps.Keys(d.getKeystrokes()))
	keys = append(keys, "enter", "tab", "shift+tab", "up", "down")
	pressKey := mcp.NewTool(
		"PressKey",
		mcp.WithDescription("Press a key combination"),
		mcp.WithString(
			"key",
			mcp.Required(),
			mcp.Description("Name of the state to add"),
			mcp.Enum(keys...),
		),
	)

	// TODO mouse clicks

	// TUI

	textScreen := mcp.NewTool(
		"TextScreen",
		mcp.WithDescription("Get text content of the TUI screen"),
	)
	textReader := mcp.NewTool(
		"TextLogReader",
		mcp.WithDescription("Get text content of the Log Reader pane"),
	)
	textHelp := mcp.NewTool(
		"TextHelp",
		mcp.WithDescription("Get text content of the Help dialog"),
	)
	textLog := mcp.NewTool(
		"TextLog",
		mcp.WithDescription("Get text content of the current log buffer"),
	)
	textClients := mcp.NewTool(
		"TextClients",
		mcp.WithDescription("Get a full clients list"),
	)
	netGraph := mcp.NewTool(
		"NetworkGraph",
		mcp.WithDescription("Get an XML of the network graph"),
	)
	txSteps := mcp.NewTool(
		"Transition Steps",
		mcp.WithDescription(
			"Get a markdown sequence diagram of the transition steps",
		),
	)

	// methods

	goToMachAddr := mcp.NewTool("GoToMachAddr",
		mcp.WithDescription("Show a specific machine, transition, or mach time"),
		mcp.WithString(
			"url",
			mcp.Required(),
			mcp.Description("Address to go to"),
		))

	// server
	srvDbg := &mcpServer{
		Server: srv,
		d:      d,
	}

	// bind handlers
	srv.Mcp.AddTool(textScreen, srvDbg.textScreen)
	srv.Mcp.AddTool(textReader, srvDbg.textReader)
	srv.Mcp.AddTool(textHelp, srvDbg.textHelp)
	srv.Mcp.AddTool(textLog, srvDbg.textLog)
	srv.Mcp.AddTool(textClients, srvDbg.textClients)
	srv.Mcp.AddTool(netGraph, srvDbg.netGraph)
	srv.Mcp.AddTool(txSteps, srvDbg.txSteps)
	srv.Mcp.AddTool(goToMachAddr, srvDbg.goToMachAddr)
	srv.Mcp.AddTool(pressKey, srvDbg.pressKey)

	return srvDbg, nil
}

func (m *mcpServer) goToMachAddr(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	url, err := req.RequireString("url")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	machUrl, err := types.ParseMachUrl(url)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	m.d.GoToMachAddress(machUrl, false)

	return mcp.NewToolResultText("ok"), nil
}

func (m *mcpServer) textReader(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	// check if reader visible
	if m.d.Mach.Not1(ss.LogReaderVisible) {
		return mcp.NewToolResultError(fmt.Sprintf(
			"state %s not active", ss.LogReaderVisible,
		)), nil
	}

	return mcp.NewToolResultText(m.d.LogReaderText()), nil
}

func (m *mcpServer) textScreen(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(m.d.App.ScreenText()), nil
}

func (m *mcpServer) textHelp(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(textViewToText(m.d.helpDialogLeft) + "\n\n" +
		textViewToText(m.d.helpDialogRight)), nil
}

func (m *mcpServer) textLog(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	p := m.d.Params()
	if !p.OutputLog {
		return mcp.NewToolResultError("--output-log not set"), nil
	}

	log, err := os.ReadFile(path.Join(p.OutputDir, logFile))
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(log)), nil
}

func (m *mcpServer) textClients(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	p := m.d.Params()
	if !p.OutputLog {
		return mcp.NewToolResultError("--output-clients not set"), nil
	}

	log, err := os.ReadFile(path.Join(p.OutputDir, "clients.txt"))
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(log)), nil
}

func (m *mcpServer) netGraph(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	p := m.d.Params()
	if !p.OutputLog {
		return mcp.NewToolResultError("--output-clients not set"), nil
	}

	log, err := os.ReadFile(path.Join(p.OutputDir, "graph.xml"))
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(log)), nil
}

func (m *mcpServer) txSteps(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	p := m.d.Params()
	if !p.OutputLog {
		return mcp.NewToolResultError("--output-tx not set"), nil
	}

	log, err := os.ReadFile(path.Join(p.OutputDir, "tx.md"))
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(log)), nil
}

func (m *mcpServer) pressKey(
	ctx context.Context, req mcp.CallToolRequest,
) (*mcp.CallToolResult, error) {
	key, err := req.RequireString("key")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// split "key" param on + and parse the modifier
	parts := strings.Split(key, "+")
	var mod tcell.ModMask = tcell.ModNone
	keyName := parts[len(parts)-1]
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(part) {
		case "alt":
			mod |= tcell.ModAlt
		case "ctrl":
			mod |= tcell.ModCtrl
		case "shift":
			mod |= tcell.ModShift
		}
	}

	// create evKey and rune
	var evKey tcell.Key
	var rune rune
	stringToKey := make(map[string]tcell.Key)
	for key, name := range tcell.KeyNames {
		stringToKey[strings.ToLower(name)] = key
	}
	lowerStr := strings.ToLower(keyName)
	if key, exists := stringToKey[lowerStr]; exists {
		evKey = key
	} else {
		rune, _ = utf8.DecodeRuneInString(strings.ToLower(keyName))
		if rune == utf8.RuneError {
			return mcp.NewToolResultError("key rune err"), nil
		}
		evKey = tcell.KeyRune
	}

	m.d.App.QueueEvent(tcell.NewEventKey(evKey, rune, mod))
	return mcp.NewToolResultText("ok"), nil
}
