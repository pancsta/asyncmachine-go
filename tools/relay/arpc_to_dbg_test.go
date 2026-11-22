package relay

import (
	"context"
	"net/rpc"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sst "github.com/pancsta/asyncmachine-go/internal/testing/states"
	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/relay/types"
)

// dbgSink is a minimal am-dbg stand-in, recording the schema and transition
// msgs sent by dbg tracers.
type dbgSink struct {
	mu      sync.Mutex
	schemas []*dbg.DbgMsgStruct
	txs     []*dbg.DbgMsgTx
}

func (s *dbgSink) DbgMsgSchema(msg *dbg.DbgMsgStruct, _ *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.schemas = append(s.schemas, msg)

	return nil
}

func (s *dbgSink) DbgMsgTx(msg *dbg.DbgMsgTx, _ *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.txs = append(s.txs, msg)

	return nil
}

func (s *dbgSink) netMachSchema() *dbg.DbgMsgStruct {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, schema := range s.schemas {
		if strings.HasPrefix(schema.ID, "rnm-") {
			return schema
		}
	}

	return nil
}

func (s *dbgSink) txCount(machId string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, tx := range s.txs {
		if tx.MachineID == machId {
			count++
		}
	}

	return count
}

// TestArpcToDbg verifies that am-relay arpc-to-dbg syncs the remote schema
// (with relations) from the aRPC server and forwards NetMach transitions to
// am-dbg.
func TestArpcToDbg(t *testing.T) {
	if amhelp.IsTestRunner() {
		t.Skip("MachDebug is a no-op in CI")
	}

	ctx, cancel := context.WithCancel(context.Background())
	// safety cleanup, runs last
	t.Cleanup(cancel)

	// am-dbg stand-in
	sinkLis := utils.RandListener("localhost")
	t.Cleanup(func() { _ = sinkLis.Close() })
	sink := &dbgSink{}
	sinkSrv := rpc.NewServer()
	require.NoError(t, sinkSrv.RegisterName("RPCServer", sink))
	go func() {
		for {
			conn, err := sinkLis.Accept()
			if err != nil {
				return
			}
			go sinkSrv.ServeConn(conn)
		}
	}()

	// aRPC source, with relations between A, B, C, D
	src := utils.NewRelsNetSrc(t, nil)
	t.Cleanup(func() {
		src.Dispose()
		<-src.WhenDisposed()
	})
	rpcAddr, _ := newMux(t, ctx, src)

	// relay
	r, err := New(ctx, types.CliArgs{
		ArpcToDbg: &types.ArgsArpcToDbg{
			RpcAddr: rpcAddr,
			DbgAddr: sinkLis.Addr().String(),
		},
	})
	require.NoError(t, err)
	// stop the relay/client before the source and the sink
	t.Cleanup(func() {
		cancel()
		<-r.Mach.WhenDisposed()
	})
	r.Start(nil)

	// wait for the NetMach schema
	require.Eventually(t, func() bool {
		schema := sink.netMachSchema()
		if schema == nil {
			return false
		}

		return len(schema.StatesIndex) > 0 && len(schema.States) > 0
	}, 10*time.Second, 50*time.Millisecond)

	schema := sink.netMachSchema()
	require.NotNil(t, schema)

	// the remote schema and its relations were synced
	require.True(t, schema.StatesIndex.Index1(sst.A) > -1,
		"source state A missing from the netmach schema")
	require.Contains(t, schema.States, sst.A)
	require.Equal(t, am.S{sst.C}, schema.States[sst.A].Require,
		"relation A requires C missing from the netmach schema")

	// wait for a transition of the synced source
	txBefore := sink.txCount(schema.ID)
	src.Add1(sst.B, nil)

	require.Eventually(t, func() bool {
		return sink.txCount(schema.ID) > txBefore
	}, 10*time.Second, 50*time.Millisecond)
}
