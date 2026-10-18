package debugger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/pancsta/cview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	"github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

func TestMarkerToggle(t *testing.T) {
	c := server.NewClient("connId", &dbg.DbgMsgStruct{ID: "mach-1"})

	assert.False(t, c.TxIsMarked("tx-0"))
	c.TxMark("tx-0", true)
	assert.True(t, c.TxIsMarked("tx-0"))
	c.TxMark("tx-0", false)
	assert.False(t, c.TxIsMarked("tx-0"))
}

func TestMarkersExportImport(t *testing.T) {
	tempDir := t.TempDir()

	d := &Debugger{
		params: types.Params{
			OutputDir: tempDir,
		},
		Clients: make(map[string]*Client),
		Mach:    am.New(nil, states.DebuggerSchema, nil),
	}

	t0 := time.Now()
	t1 := t0.Add(time.Second)

	client := &Client{
		Client: server.NewClient("connId", &dbg.DbgMsgStruct{
			ID: "test-mach",
		}),
	}
	client.MsgTxs = []*dbg.DbgMsgTx{
		{MachineID: "test-mach", ID: "tx-0", Time: &t0},
		{MachineID: "test-mach", ID: "tx-1", Time: &t1},
	}
	client.Markers = map[string]struct{}{
		"tx-1": {},
	}

	d.C = client
	d.Clients[client.Id] = client

	// export
	filename := "test-markers-export"
	d.hExportData(filename, false)

	// verify file exists
	gobPath := filepath.Join(tempDir, filename+".gob.br")
	_, err := os.Stat(gobPath)
	require.NoError(t, err)

	// import to new debugger instance
	dImport := &Debugger{
		Clients: make(map[string]*Client),
		Mach:    am.New(nil, states.DebuggerSchema, nil),
	}
	dImport.hImportData(gobPath)

	require.Contains(t, dImport.Clients, "test-mach")
	imported := dImport.Clients["test-mach"]
	assert.True(t, imported.TxIsMarked("tx-1"))
	assert.False(t, imported.TxIsMarked("tx-0"))
}

func TestTimelineMarkersUI(t *testing.T) {
	d := &Debugger{
		Clients: make(map[string]*Client),
		Mach:    am.New(nil, states.DebuggerSchema, nil),
	}
	d.hInitTimelineTx()
	d.hInitTimelineMarkers()

	t0 := time.Now()
	t1 := t0.Add(time.Second)

	c := &Client{
		Client: &server.Client{
			Id: "mach-1",
			Exportable: &server.Exportable{
				MsgStruct: &dbg.DbgMsgStruct{ID: "mach-1", StatesIndex: am.S{"A"}},
				MsgTxs: []*dbg.DbgMsgTx{
					{MachineID: "mach-1", ID: "tx0", Time: &t0},
					{MachineID: "mach-1", ID: "tx1", Time: &t1},
				},
				Markers: map[string]struct{}{
					"tx1": {},
				},
			},
		},
		CursorTx1: 2,
	}

	d.Clients[c.Id] = c
	d.C = c

	d.hUpdateTimelineTx()
	d.hBuildMarkersIndex()
	d.hUpdateTimelineMarker()

	assert.Equal(t, 1, d.timelineMarkers.GetMax())
	assert.Equal(t, 0, d.timelineMarkers.GetProgress())
	assert.Contains(t, d.timelineMarkers.GetTitle(), "Marker 1 / 1")
}

func TestExportMach(t *testing.T) {
	tempDir := t.TempDir()

	d := &Debugger{
		params: types.Params{
			OutputDir:  tempDir,
			OutputMach: true,
		},
		Clients: make(map[string]*Client),
	}

	err := d.hInitMachFile()
	require.NoError(t, err)
	defer func() { _ = d.hCloseMachFile() }()

	stateNames := am.S{"Foo", "Bar", "Baz"}
	schema := am.Schema{
		"Foo": {
			Auto: true,
			Add:  am.S{"Bar"},
		},
	}
	client := &Client{
		Client: &server.Client{
			Id: "test-mach",
			Exportable: &server.Exportable{
				MsgStruct: &dbg.DbgMsgStruct{
					ID:          "test-mach",
					StatesIndex: stateNames,
					States:      schema,
				},
				MsgTxs: []*dbg.DbgMsgTx{
					{
						MachineID: "test-mach",
						ID:        "tx-0",
						Clocks:    am.Time{1, 0, 0},
						QueueTick: 10,
					},
					{
						MachineID: "test-mach",
						ID:        "tx-1",
						Clocks:    am.Time{1, 2, 0},
						QueueTick: 25,
					},
				},
			},
		},
		CursorTx1: 2,
	}

	d.C = client
	d.Clients[client.Id] = client

	err = d.hExportMach()
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tempDir, "mach.yml"))
	require.NoError(t, err)
	assert.NotEmpty(t, content)

	var ser am.Serialized
	err = yaml.Unmarshal(content, &ser)
	require.NoError(t, err)

	assert.Equal(t, "test-mach", ser.ID)
	assert.Equal(t, stateNames, ser.StateNames)
	assert.Equal(t, am.Time{1, 2, 0}, ser.Time)
	assert.Equal(t, uint64(25), ser.QueueTick)
	assert.Equal(t, uint32(1), ser.MachineTick)

	// schema.yml
	schemaContent, err := os.ReadFile(filepath.Join(tempDir, "schema.yml"))
	require.NoError(t, err)
	assert.NotEmpty(t, schemaContent)

	var gotSchema am.Schema
	err = yaml.Unmarshal(schemaContent, &gotSchema)
	require.NoError(t, err)
	assert.Equal(t, schema, gotSchema)
}

func TestMcp(t *testing.T) {
	ctx := context.Background()
	mach, err := am.NewCommon(ctx, "dbg-test", states.DebuggerSchema, ss.Names(), nil, nil, nil)
	require.NoError(t, err)

	d := &Debugger{
		Mach:   mach,
		App:    cview.NewApplication(),
		params: types.Params{Version: "1.0.0"},
	}

	srv, err := newMcpServer(d)
	require.NoError(t, err)
	require.NotNil(t, srv)

	// test Keystroke with various cview keys
	keysToTest := []string{
		"enter",
		"tab",
		"shift+tab",
		"Backtab",
		"up",
		"down",
		"left",
		"right",
		"alt+h",
		"ctrl+q",
		"space",
		"esc",
		"home",
		"end",
		"pgup",
		"pgdn",
		"F1",
		"a",
		"?",
	}

	for _, key := range keysToTest {
		t.Run("Keystroke_"+key, func(t *testing.T) {
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "Keystroke",
					Arguments: map[string]interface{}{
						"key": key,
					},
				},
			}
			res, err := srv.pressKey(ctx, req)
			require.NoError(t, err)
			assert.False(t, res.IsError, "unexpected error for key %s", key)
		})
	}

	// test invalid key
	t.Run("Keystroke_Invalid", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "Keystroke",
				Arguments: map[string]interface{}{
					"key": "invalid_super_key_combination",
				},
			},
		}
		res, err := srv.pressKey(ctx, req)
		require.NoError(t, err)
		assert.True(t, res.IsError)
	})

	// test TypeText
	t.Run("TypeText", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "TypeText",
				Arguments: map[string]interface{}{
					"text": "hello\tworld\n",
				},
			},
		}
		res, err := srv.typeText(ctx, req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
	})

	// test MouseLeftClick
	t.Run("MouseLeftClick", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "MouseLeftClick",
				Arguments: map[string]interface{}{
					"x": 10,
					"y": 5,
				},
			},
		}
		res, err := srv.mouseLeftClick(ctx, req)
		require.NoError(t, err)
		assert.False(t, res.IsError)
	})

	// test MouseScroll
	for _, dir := range []string{"up", "down", "left", "right"} {
		t.Run("MouseScroll_"+dir, func(t *testing.T) {
			req := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name: "MouseScroll",
					Arguments: map[string]interface{}{
						"x":         10,
						"y":         5,
						"direction": dir,
						"amount":    3,
					},
				},
			}
			res, err := srv.mouseScroll(ctx, req)
			require.NoError(t, err)
			assert.False(t, res.IsError)
		})
	}

	// test MouseScroll invalid direction
	t.Run("MouseScroll_InvalidDir", func(t *testing.T) {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "MouseScroll",
				Arguments: map[string]interface{}{
					"x":         10,
					"y":         5,
					"direction": "diagonal",
				},
			},
		}
		res, err := srv.mouseScroll(ctx, req)
		require.NoError(t, err)
		assert.True(t, res.IsError)
	})
}

func TestHumanSort(t *testing.T) {
	data := []string{
		"_nco-dev-test-210946-00e4ff1",
		"_rs-nco-dev-test-210946-00e4ff1",
		"_rc-con-dev-test-2870ef",
		"_rs-nw-loc-dev-test-6791e5",
		"_rc-con-dev-test-6791e5",
		"_ns-dev-test-210946-0",
		"_rs-ns-pub-dev-test-210946-0",
		"_rs-ns-loc-dev-test-210946-0",
		"_rs-nco-dev-test-210946-0e7608f",
		"_rc-ns-dev-test-p-36317",
		"_rs-nw-loc-dev-test-b55a1b",
		"_nco-dev-test-210946-0c2bbae",
		"_rc-ns-dev-test-p-44331",
		"_rs-nw-loc-dev-test-2870ef",
		"_nw-dev-test-2870ef",
		"_nw-dev-test-6791e5",
		"_nco-dev-test-210946-0e7608f",
		"_nw-dev-test-b55a1b",
		"_rc-con-dev-test-b55a1b",
		"_rs-nco-dev-test-210946-0c2bbae",
	}

	expected := []string{
		"_nco-dev-test-210946-00e4ff1",
		"_nco-dev-test-210946-0c2bbae",
		"_nco-dev-test-210946-0e7608f",

		"_ns-dev-test-210946-0",

		"_nw-dev-test-2870ef",
		"_nw-dev-test-6791e5",
		"_nw-dev-test-b55a1b",

		"_rc-con-dev-test-2870ef",
		"_rc-con-dev-test-6791e5",
		"_rc-con-dev-test-b55a1b",
		"_rc-ns-dev-test-p-36317",
		"_rc-ns-dev-test-p-44331",
		"_rs-nco-dev-test-210946-00e4ff1",
		"_rs-nco-dev-test-210946-0c2bbae",
		"_rs-nco-dev-test-210946-0e7608f",
		"_rs-ns-loc-dev-test-210946-0",
		"_rs-ns-pub-dev-test-210946-0",
		"_rs-nw-loc-dev-test-2870ef",
		"_rs-nw-loc-dev-test-6791e5",
		"_rs-nw-loc-dev-test-b55a1b",
	}

	humanSort(data)
	join := strings.Join(data, "\n")

	// debug
	// t.Log("\n" + join)

	assert.Equal(t, strings.Join(expected, "\n"), join)
}

func TestHadErrSince(t *testing.T) {
	c := &Client{
		Client: &server.Client{
			Errors: []int{100, 50, 5, 1},
		},
	}

	assert.False(t, c.HadErrSinceTx(300, 5), "tx: %d, dist: %d", 300, 5)
	assert.False(t, c.HadErrSinceTx(105, 3), "tx: %d, dist: %d", 105, 3)

	assert.True(t, c.HadErrSinceTx(55, 10), "tx: %d, dist: %d", 55, 10)
	assert.True(t, c.HadErrSinceTx(6, 2), "tx: %d, dist: %d", 6, 2)
	assert.True(t, c.HadErrSinceTx(100, 2), "tx: %d, dist: %d", 100, 2)
	assert.True(t, c.HadErrSinceTx(1, 1), "tx: %d, dist: %d", 1, 1)
}
