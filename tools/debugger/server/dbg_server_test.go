package server_test

import (
	"context"
	"errors"
	"net/rpc"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pancsta/asyncmachine-go/internal/testing/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	ssdbg "github.com/pancsta/asyncmachine-go/tools/debugger/states"
	"github.com/pancsta/asyncmachine-go/tools/debugger/types"
)

func init() {
	if os.Getenv(am.EnvAmTestDebug) != "" {
		amhelp.EnableDebugging(false)
	}
}

// fwdSink is a minimal stand-in for an am-dbg instance. It accepts the RPC
// calls the dbg server makes on its fwd targets and counts them, so tests can
// observe how much telemetry actually arrives.
//
// Only msgs of machId are counted, so clients of other tests never affect
// these counts.
type fwdSink struct {
	machId    string
	schemaCnt atomic.Int32
	txCnt     atomic.Int32
}

// DbgMsgSchemaFwd is the fwd-target side of [server.RPCServer.DbgMsgSchemaFwd].
func (s *fwdSink) DbgMsgSchemaFwd(
	msg *server.DbgMsgSchemaFwd, _ *string,
) error {
	if msg.MsgStruct.ID != s.machId {
		return nil
	}
	s.schemaCnt.Add(1)
	return nil
}

// DbgMsgTxFwd is the fwd-target side of [server.RPCServer.DbgMsgTxFwd].
func (s *fwdSink) DbgMsgTxFwd(msg *server.DbgMsgTx, _ *string) error {
	if msg.MsgTx.MachineID != s.machId {
		return nil
	}
	s.txCnt.Add(1)
	return nil
}

// TestFwdMissingSinkDoesntKillOthers reproduces am-relay forwarding to 2
// am-dbg instances where the 2nd one is missing: the 1st (healthy) one stops
// receiving telemetry after a short while, even though it's online the whole
// time.
//
// Cause: the dbg server returns the fwd error of the missing sink to the
// sending client ([server.RPCServer.DbgMsgTx]), the client's dbg tracer counts
// it as a send error and detaches itself permanently after >10 errors, which
// cuts telemetry to every sink. See [server.fwdConn] and
// [dbg.Tracer.TransitionEnd].
func TestFwdMissingSinkDoesntKillOthers(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) == "" {
		t.Parallel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1st am-dbg: healthy, counts forwarded msgs
	sinkLis := utils.RandListener("localhost")
	defer sinkLis.Close()
	sink := &fwdSink{machId: "test-client"}
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

	// 2nd am-dbg: missing, nothing listens at this addr
	deadLis := utils.RandListener("localhost")
	deadAddr := deadLis.Addr().String()
	require.NoError(t, deadLis.Close())

	// am-relay: a dbg server forwarding to both sinks
	relayLis := utils.RandListener("localhost")
	relayAddr := relayLis.Addr().String()
	require.NoError(t, relayLis.Close())
	relayMach, err := am.NewCommon(ctx, "relay-test", ssdbg.DebuggerSchema,
		ssdbg.DebuggerStates.Names(), nil, nil, &am.Opts{})
	require.NoError(t, err)
	// am-relay serves in Start, which the server events require
	relayMach.Add1(ssdbg.DebuggerStates.Start, nil)

	mux, _, err := server.New(relayMach, relayAddr, func() *types.Params {
		return &types.Params{
			// 1st healthy, 2nd missing
			FwdData: []string{sinkLis.Addr().String(), deadAddr},
		}
	})
	require.NoError(t, err)
	go func() { _ = mux.Serve() }()

	// client machine, sending telemetry to the relay like an am-dbg client does
	clientMach, err := am.NewCommon(ctx, "test-client", ssam.BasicSchema,
		ssam.BasicStates.Names(), nil, nil, &am.Opts{})
	require.NoError(t, err)
	require.NoError(t, dbg.TransitionsToDbg(clientMach, relayAddr))

	// wait for the client's schema to reach the healthy sink
	require.Eventually(t, func() bool {
		return sink.schemaCnt.Load() > 0
	}, 5*time.Second, 20*time.Millisecond,
		"schema not forwarded to the healthy sink")

	// produce more transitions than the client's 10-error detach threshold
	const transitions = 40
	for i := 0; i < transitions; i++ {
		if i%2 == 0 {
			amhelp.Add1Sync(ctx, clientMach, ssam.BasicStates.Start, nil)
		} else {
			amhelp.Remove1Sync(ctx, clientMach, ssam.BasicStates.Start, nil)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// every transition must reach the healthy sink; the missing 2nd sink must
	// not affect the others
	ok := assert.Eventually(t, func() bool {
		return sink.txCnt.Load() >= transitions
	}, 10*time.Second, 50*time.Millisecond)
	if !ok {
		t.Fatalf("healthy fwd sink received %d/%d txs; "+
			"a missing 2nd fwd sink must not stop the other sinks",
			sink.txCnt.Load(), transitions)
	}
}

// errRejected simulates a fwd target which accepts connections, but can't take
// telemetry yet.
var errRejected = errors.New("sink rejected")

// replaySink is a minimal fwd target which records the tx msgs of a single
// client, so tests can assert what was replayed after a (re)connect.
type replaySink struct {
	// reject makes every call fail, simulating a target which is up at the
	// TCP level, but is not ready for telemetry
	reject    atomic.Bool
	machId    string
	schemaCnt atomic.Int32

	mx     sync.Mutex
	mine   int
	seenTx map[string]struct{}
}

// DbgMsgSchemaFwd is the fwd-target side of [server.RPCServer.DbgMsgSchemaFwd].
func (s *replaySink) DbgMsgSchemaFwd(
	msg *server.DbgMsgSchemaFwd, _ *string,
) error {
	if s.reject.Load() {
		return errRejected
	}
	if msg.MsgStruct.ID != s.machId {
		return nil
	}
	s.schemaCnt.Add(1)

	return nil
}

// DbgMsgTxFwd is the fwd-target side of [server.RPCServer.DbgMsgTxFwd].
func (s *replaySink) DbgMsgTxFwd(msg *server.DbgMsgTx, _ *string) error {
	if s.reject.Load() {
		return errRejected
	}
	if msg.MsgTx.MachineID != s.machId {
		return nil
	}

	s.mx.Lock()
	defer s.mx.Unlock()
	s.mine++
	s.seenTx[msg.ConnId+"/"+msg.MsgTx.ID] = struct{}{}

	return nil
}

// stats returns the number of received msgs of machId and the number of unique
// ones. Equal values mean no msg was re-sent.
func (s *replaySink) stats() (received, unique int) {
	s.mx.Lock()
	defer s.mx.Unlock()

	return s.mine, len(s.seenTx)
}

// TestFwdReplayAfterReconnect verifies that a fwd target which comes online
// late (or comes back after downtime) receives the client's schema followed by
// the recent tx msgs. A fresh target gets them once; a reconnecting target gets
// the whole buffer again. See [server.fwdConn.dial],
// [server.fwdConn.pushClientMsgs], and [server.fwdConn.pushClientSchemas].
func TestFwdReplayAfterReconnect(t *testing.T) {
	if os.Getenv(am.EnvAmTestDbgAddr) != "" {
		t.Skip("skipping under a real am-dbg addr")
	}
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	machId := "replay-client"

	// fwd target: rejects telemetry until un-rejected
	sink := &replaySink{machId: machId, seenTx: make(map[string]struct{})}
	sink.reject.Store(true)
	sinkLis := utils.RandListener("localhost")
	sinkAddr := sinkLis.Addr().String()
	defer sinkLis.Close()
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

	// am-relay: a dbg server forwarding to the sink
	relayLis := utils.RandListener("localhost")
	relayAddr := relayLis.Addr().String()
	require.NoError(t, relayLis.Close())
	relayMach, err := am.NewCommon(ctx, "relay-replay-test", ssdbg.DebuggerSchema,
		ssdbg.DebuggerStates.Names(), nil, nil, &am.Opts{})
	require.NoError(t, err)
	relayMach.Add1(ssdbg.DebuggerStates.Start, nil)

	mux, _, err := server.New(relayMach, relayAddr, func() *types.Params {
		return &types.Params{FwdData: []string{sinkAddr}}
	})
	require.NoError(t, err)
	go func() { _ = mux.Serve() }()

	// client machine, sending telemetry to the relay like an am-dbg client does
	clientMach, err := am.NewCommon(ctx, machId, ssam.BasicSchema,
		ssam.BasicStates.Names(), nil, nil, &am.Opts{})
	require.NoError(t, err)
	// pre-warm the lazy state-name cache, so the tracer's initial schema send
	// (StateNames) doesn't race with the first transition when running with
	// -race; Machine.StateNames writes this cache under a read lock
	clientMach.StateNames()
	require.NoError(t, dbg.TransitionsToDbg(clientMach, relayAddr))

	toggle := func(n int) {
		for i := 0; i < n; i++ {
			if i%2 == 0 {
				amhelp.Add1Sync(ctx, clientMach, ssam.BasicStates.Start, nil)
			} else {
				amhelp.Remove1Sync(ctx, clientMach, ssam.BasicStates.Start, nil)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// phase 1: while the sink rejects, the relay buffers the msgs
	const phase1 = 20
	toggle(phase1)

	// phase 2: accept telemetry - the schema and the buffered msgs must arrive
	sink.reject.Store(false)
	require.Eventually(t, func() bool {
		return sink.schemaCnt.Load() > 0
	}, 15*time.Second, 50*time.Millisecond, "schema not replayed")
	require.Eventually(t, func() bool {
		received, _ := sink.stats()
		return received >= phase1
	}, 15*time.Second, 50*time.Millisecond, "buffered msgs not replayed")
	received2, unique2 := sink.stats()
	assert.Equal(t, received2, unique2, "msgs replayed twice")

	// phase 3: reject again, send more msgs, accept - the whole buffer is
	// replayed again, so msgs may arrive more than once
	sink.reject.Store(true)
	const phase3 = 10
	toggle(phase3)
	sink.reject.Store(false)
	require.Eventually(t, func() bool {
		received, _ := sink.stats()
		return received >= received2+phase1+phase3
	}, 15*time.Second, 50*time.Millisecond,
		"buffered msgs not fully replayed after a reconnect")
	received3, unique3 := sink.stats()
	assert.Greater(t, received3, unique3,
		"a reconnect must push all buffered msgs again")
	assert.GreaterOrEqual(t, unique3, phase1+phase3,
		"replayed msgs lost after a reconnect")
}

func TestTxAtStateTick(t *testing.T) {
	t.Parallel()

	c := server.NewClient("connId", &dbg.DbgMsgStruct{
		ID:          "mach-1",
		StatesIndex: am.S{"Ready", "Foo"},
	})
	assert.Equal(t, -1, c.TxAtStateTick("Ready", 1), "no transitions")

	c.MsgTxs = []*dbg.DbgMsgTx{
		{Clocks: am.Time{1, 0}},
		{Clocks: am.Time{1, 2}},
		{Clocks: am.Time{1, 4}},
		{Clocks: am.Time{3, 5}},
	}

	assert.Equal(t, 0, c.TxAtStateTick("Ready", 1))
	assert.Equal(t, 3, c.TxAtStateTick("Ready", 2))
	assert.Equal(t, 3, c.TxAtStateTick("Ready", 3))
	assert.Equal(t, 1, c.TxAtStateTick("Foo", 2))
	assert.Equal(t, 3, c.TxAtStateTick("Foo", 5))
	assert.Equal(t, 3, c.TxAtStateTick("Foo", 6), "closest later transition")
	assert.Equal(t, -1, c.TxAtStateTick("Unknown", 1))
}
