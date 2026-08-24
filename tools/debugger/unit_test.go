package debugger

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
