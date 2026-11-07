package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math/rand/v2"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/vmihailenco/msgpack/v5"

	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
)

// ErrGraphDrop indicates a test-injected dropped graph update.
var ErrGraphDrop = errors.New("graph update dropped")

// SchemaInfo describes a source schema for remote graph mirroring.
type SchemaInfo struct {
	Schema      am.Schema
	StatesIndex am.S
}

// NetTracer is a graph source backed by a [rpc.SourceTracer].
type NetTracer struct {
	*rpc.SourceTracer

	// Mach is the source machine.
	Mach am.Api
	// MachId is the source machine ID.
	MachId string
	// SchemaHash identifies the source machine schema.
	SchemaHash string
	// DataLatestSent is the latest graph data sent for this source.
	DataLatestSent *NetTracerData
	// Tags is the last successfully sent tag list.
	Tags []string
	// TagsTime is the machine time of the latest change in tags.
	TagsTime uint64
}

func (t *NetTracer) SchemaChange(mach am.Api, oldSchema am.Schema) {
	t.SchemaHash = amhelp.SchemaHash(mach.Schema())
}

// NetTracerData is a tracer snapshot decorated with source metadata.
type NetTracerData struct {
	*rpc.TracerData

	// MachId is the source machine ID.
	MachId string
	// SchemaHash identifies the source machine schema.
	SchemaHash string
}

// MsgGraphReqSerialized is when the sender requests a resend of full data
// for mach ID
type MsgGraphReqSerialized struct {
	// MachId is the ID of the machine to resend in full serialized form.
	MachId string `json:"m" msgpack:"m"`
}

// MsgGraphUpdate is a diff update pushed to sinks.
type MsgGraphUpdate struct {
	*rpc.MsgSrvUpdate `json:"u,omitempty" msgpack:"u"`

	// MachId is the machine's ID.
	MachId string `json:"i" msgpack:"i"`
	// MTimeSum is the machine's time, used as a history ID.
	MTimeSum uint64 `json:"m"`
	// SchemaHash identifies the machine's schema.
	SchemaHash string `json:"s" msgpack:"s"`
	// TagsTime is the machine time of the latest change in tags.
	TagsTime uint64 `json:"tt" msgpack:"tt"`

	// Parent is the machine's parent ID. TODO cache
	Parent string `json:"p,omitempty" msgpack:"p"`
	// Tags are the source machine tags. nil means unchanged.
	Tags []string `json:"t" msgpack:"t"`
}

const (
	// sink dial backoff bounds for reconnects.
	sinkDialBackoffMin = 50 * time.Millisecond
	sinkDialBackoffMax = 5 * time.Second
	// dbSyncChunk is the max number of machines synced to the DB per tick, so a
	// burst doesn't block readers for its whole duration.
	dbSyncChunk = 8
)

// ///// ///// /////

// ///// NET GRAPH

// ///// ///// /////

// NetGraph transports directional graph updates over UDP using the standard
// library and [GraphUpdate] as the wire message.
type NetGraph struct {
	// Addr is the local UDP listen address.
	Addr string
	// Graph is the graph being mirrored.
	Graph *Graph
	// Db optionally mirrors all graph changes into a graph database.
	// See [DbGraph] and pkg/graph/frogql ("frogql" build tag).
	// TODO expose via HTTP+JSON, open way for more transports later
	Db amtypes.DbGraph
	// PushInterval is the interval for emitting clock updates. 0 disables
	// periodic updates.
	PushInterval time.Duration
	// Json uses Json instead of msgpack for the wire format.
	Json bool
	// Logger is used for diagnostics. Defaults to [slog.Default].
	Logger *slog.Logger
	// SchemaStore maps schema hashes to source schemas.
	// TODO migrate to am.Serialized
	SchemaStore map[string]SchemaInfo
	// MissingSchemas contains schema hashes seen in updates but not yet
	// registered in SchemaStore.
	MissingSchemas map[string]struct{}

	// tests injections

	// Enable random drops of broadcast messages.
	TestBroadcastRandomDrops atomic.Bool
	// TODO MetricsEnabled atomic.Bool
	// MetricResendRequests counts sent MsgGraphReqSerialized messages.
	MetricResendRequests atomic.Int32
	MetricUpdateTooOld   atomic.Int32
	// MetricResendsHandled counts handled MsgGraphReqSerialized messages.
	MetricResendsHandled atomic.Int32
	// MetricReconnects counts successful sink dials, incl. reconnects.
	MetricReconnects atomic.Int32

	schemaStoreMx sync.Mutex

	relayPendingMx sync.Mutex
	relayPending   map[string]struct{}
	relaySentMx    sync.Mutex
	relaySent      map[string]*am.Serialized

	listener *net.UDPConn
	ctx      context.Context

	tracedMx sync.Mutex
	traced   []*NetTracer

	sinksMx      sync.Mutex
	sinks        map[string]*net.UDPConn
	sinkRetry    map[string]chan struct{}
	sinkStop     chan struct{}
	sinkStopOnce sync.Once

	dbSrvMx sync.Mutex
	dbSrv   *Server

	// coalesced clock syncs to the DB
	dbPendingMx sync.Mutex
	dbPending   map[string]struct{}
	dbDone      chan struct{}
	dbDoneOnce  sync.Once
	dbLoopOnce  sync.Once
}

// NewNetGraph creates a NetGraph listening on addr and mirroring g.
func NewNetGraph(
	ctx context.Context, addr string, g *Graph,
) (*NetGraph, error) {
	if g == nil {
		return nil, errors.New("graph is nil")
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}

	n := &NetGraph{
		Addr:           conn.LocalAddr().String(),
		Graph:          g,
		PushInterval:   250 * time.Millisecond,
		Logger:         slog.Default(),
		SchemaStore:    make(map[string]SchemaInfo),
		MissingSchemas: make(map[string]struct{}),

		relayPending: make(map[string]struct{}),
		relaySent:    make(map[string]*am.Serialized),
		dbPending:    make(map[string]struct{}),
		dbDone:       make(chan struct{}),
		listener:     conn,
		ctx:          ctx,
		sinks:        make(map[string]*net.UDPConn),
		sinkRetry:    make(map[string]chan struct{}),
		sinkStop:     make(chan struct{}),
	}

	// fork loops
	go n.receiveFromDownstream()
	if n.PushInterval > 0 {
		go n.broadcastLoop(n.PushInterval)
	}

	return n, nil
}

// TraceMach adds a machine to the mirrored graph and starts emitting its clock
// updates. It returns the tracer ID used by the source machine.
func (n *NetGraph) TraceMach(mach am.Api) (string, error) {
	id := mach.Id()
	schema := mach.Schema()
	states := mach.StateNames()
	schemaHash := amhelp.SchemaHash(schema)
	n.schemaStoreMx.Lock()
	n.SchemaStore[schemaHash] = SchemaInfo{
		Schema:      schema,
		StatesIndex: states,
	}
	delete(n.MissingSchemas, schemaHash)
	n.schemaStoreMx.Unlock()

	graphMach := &Machine{
		Serialized: &am.Serialized{
			ID:          id,
			StateNames:  slices.Clone(states),
			Time:        make(am.Time, len(states)),
			QueueTick:   1,
			MachineTick: 0,
			Tags:        slices.Clone(mach.Tags()),
		},
		SchemaHash: schemaHash,
		Schema:     schema.Clone(),
		Parent:     mach.ParentId(),
	}
	if err := n.Graph.AddMachine(graphMach); err != nil {
		return "", err
	}

	// push the new machine to the DB
	if n.Db != nil {
		if err := n.Db.UpsertMachine(graphMach); err != nil {
			n.Logger.Error("DbGraph upsert failed", "mach", id, "err", err)
		}
	}

	tracerSrc := rpc.NewSourceTracer(id, nil)
	tracerSrc.Active.Store(true)
	tracer := &NetTracer{
		SourceTracer: tracerSrc,
		Mach:         mach,
		MachId:       id,
		SchemaHash:   schemaHash,
		DataLatestSent: &NetTracerData{
			TracerData: tracerSrc.DataLatest.Load(),
			MachId:     id,
			SchemaHash: schemaHash,
		},
	}
	traceId, err := mach.TracerBind(tracer)
	if err != nil {
		return traceId, err
	}
	n.tracedMx.Lock()
	n.traced = append(n.traced, tracer)
	n.tracedMx.Unlock()

	return traceId, nil
}

// EmitToAddr registers a directional UDP destination for graph updates.
// Cancel the returned context to stop emitting to the address.
//
// The first dial happens synchronously (so an immediate broadcast lands), then
// the sink is maintained in the background: failed dials are retried with
// exponential backoff, and broken sinks (write errors) are dropped and
// re-dialed. Use [NetGraph.Sinks] to check the live set and
// [NetGraph.MetricReconnects] for dial counts. Only one sink per address is
// supported; a duplicate call returns an already-cancelled context.
func (n *NetGraph) EmitToAddr(
	addr string,
) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(n.ctx)
	retry := make(chan struct{}, 1)

	n.sinksMx.Lock()
	if _, ok := n.sinkRetry[addr]; ok {
		n.sinksMx.Unlock()
		n.Logger.Warn("sink already emitting", "addr", addr)
		cancel()
		return ctx, cancel
	}
	n.sinkRetry[addr] = retry
	n.sinksMx.Unlock()

	// try the first dial synchronously, so an immediate broadcast lands
	conn, err := n.dialSink(addr)
	if err != nil {
		n.Logger.Warn("sink dial failed, retrying", "addr", addr, "err", err)
	} else {
		n.MetricReconnects.Add(1)
		n.sinksMx.Lock()
		n.sinks[addr] = conn
		n.sinksMx.Unlock()
		go n.receiveFromUpstream(conn)
	}

	go n.sinkLoop(ctx, addr, retry, conn)

	return ctx, cancel
}

// Sinks returns the addresses of the currently connected sinks.
func (n *NetGraph) Sinks() []string {
	n.sinksMx.Lock()
	defer n.sinksMx.Unlock()

	addrs := make([]string, 0, len(n.sinks))
	for addr := range n.sinks {
		addrs = append(addrs, addr)
	}
	slices.Sort(addrs)

	return addrs
}

// Broadcast sends an update to every registered UDP destination.
func (n *NetGraph) Broadcast(update *MsgGraphUpdate) error {
	if n.TestBroadcastRandomDrops.Load() && rand.IntN(2) == 0 {
		return ErrGraphDrop
	}

	data, err := n.marshal(update)
	if err != nil {
		return err
	}

	if len(data) > 1472 {
		n.Logger.Warn("data exceeds 1,472 bytes", "len", len(data))
	}

	type peer struct {
		addr string
		conn *net.UDPConn
	}
	n.sinksMx.Lock()
	peers := make([]peer, 0, len(n.sinks))
	for addr, conn := range n.sinks {
		peers = append(peers, peer{addr: addr, conn: conn})
	}
	n.sinksMx.Unlock()

	var errs []error
	for _, p := range peers {
		if _, err := p.conn.Write(data); err != nil {
			errs = append(errs, err)
			n.dropSink(p.addr, p.conn)
		}
	}

	return errors.Join(errs...)
}

// Close stops the receiver and all emitters.
func (n *NetGraph) Close() error {
	// stop the DB sync loop and flush the pending clock updates
	n.dbDoneOnce.Do(func() { close(n.dbDone) })
	n.flushDbSyncAll()

	// stop sink reconnects, then close the sinks
	n.sinkStopOnce.Do(func() { close(n.sinkStop) })

	// close the DB server first, it reads from the graph
	n.dbSrvMx.Lock()
	dbSrv := n.dbSrv
	n.dbSrv = nil
	n.dbSrvMx.Unlock()
	if dbSrv != nil {
		_ = dbSrv.Close()
	}

	n.sinksMx.Lock()
	for _, conn := range n.sinks {
		_ = conn.Close()
	}
	clear(n.sinks)
	clear(n.sinkRetry)
	n.sinksMx.Unlock()

	err := n.listener.Close()

	return err
}

// PRIVATE

// sinkLoop maintains one sink connection, re-dialing on write errors and
// failed dials.
func (n *NetGraph) sinkLoop(
	ctx context.Context, addr string, retry chan struct{}, conn *net.UDPConn,
) {
	defer func() {
		n.sinksMx.Lock()
		delete(n.sinkRetry, addr)
		n.sinksMx.Unlock()
	}()

	backoff := sinkDialBackoffMin
	for {
		if conn == nil {
			var err error
			conn, err = n.dialSink(addr)
			if err != nil {
				n.Logger.Warn("sink dial failed, retrying", "addr", addr,
					"err", err, "backoff", backoff)
				select {
				case <-ctx.Done():
					return
				case <-n.sinkStop:
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, sinkDialBackoffMax)
				continue
			}
			backoff = sinkDialBackoffMin

			n.sinksMx.Lock()
			select {
			case <-n.sinkStop:
				// closed concurrently
				n.sinksMx.Unlock()
				_ = conn.Close()
				return
			default:
			}
			n.sinks[addr] = conn
			n.sinksMx.Unlock()

			n.MetricReconnects.Add(1)
			go n.receiveFromUpstream(conn)
		}

		select {
		case <-ctx.Done():
			n.removeSink(addr, conn)
			return
		case <-n.sinkStop:
			n.removeSink(addr, conn)
			return
		case <-retry:
			// dropped by Broadcast, re-dial
			conn = nil
		}
	}
}

// dialSink resolves and dials a UDP sink.
func (n *NetGraph) dialSink(addr string) (*net.UDPConn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	return net.DialUDP("udp", nil, udpAddr)
}

// removeSink closes and unregisters a sink, if it's still current.
func (n *NetGraph) removeSink(addr string, conn *net.UDPConn) {
	n.sinksMx.Lock()
	defer n.sinksMx.Unlock()

	if cur, ok := n.sinks[addr]; ok && cur == conn {
		delete(n.sinks, addr)
	}
	_ = conn.Close()
}

// dropSink removes a broken sink and schedules a reconnect.
func (n *NetGraph) dropSink(addr string, conn *net.UDPConn) {
	n.sinksMx.Lock()
	defer n.sinksMx.Unlock()

	if cur, ok := n.sinks[addr]; !ok || cur != conn {
		return
	}
	delete(n.sinks, addr)
	_ = conn.Close()

	if retry := n.sinkRetry[addr]; retry != nil {
		select {
		case retry <- struct{}{}:
		default:
		}
	}
}

func (n *NetGraph) pushLocalTracer(tracer *NetTracer) {
	data := tracer.DataLatest.Load()
	if data == nil {
		return
	}

	dataLatest := &NetTracerData{
		TracerData: data,
		MachId:     tracer.MachId,
		SchemaHash: tracer.SchemaHash,
	}
	tags := tracer.Mach.Tags()
	last := tracer.DataLatestSent

	tagsChanged := !slices.Equal(tags, tracer.Tags)
	machChanged := last.MachTick != data.MachTick
	dataChanged := last.MTrackedTimeSum != data.MTrackedTimeSum ||
		last.QueueTick != data.QueueTick
	if !dataChanged && !tagsChanged && !machChanged {
		return
	}

	update := rpc.CalcUpdate(false,
		dataLatest.TracerData, last.TracerData,
		false)
	if len(update.Indexes) == 0 && !tagsChanged && !machChanged {
		// TODO warn?
		return
	}

	msg := &MsgGraphUpdate{
		MsgSrvUpdate: update,
		MTimeSum:     data.MTrackedTimeSum,
		MachId:       tracer.MachId,
		SchemaHash:   tracer.SchemaHash,
		Parent:       tracer.Mach.ParentId(),
		TagsTime:     tracer.TagsTime,
	}
	if tagsChanged {
		if tags == nil {
			tags = []string{}
		}
		msg.Tags = tags
		tracer.Tags = tags
		tracer.TagsTime = data.MTrackedTimeSum
	}
	// TODO optimize: send parent 10 times, and never after a full serialized msg
	if err := n.Broadcast(msg); err != nil {
		n.Logger.Error("pushLocalTracer", "err", err)
		return
	}

	// memorize
	tracer.DataLatestSent = dataLatest
}

// broadcastLoop emits local tracers and relays relayPending mirrored machines.
func (n *NetGraph) broadcastLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			// TODO optimize: collect updates into a slice and keep under UDP limit
			for _, tracer := range n.tracers() {
				n.pushLocalTracer(tracer)
			}
			n.pushPendingRelay()
		}
	}
}

func (n *NetGraph) tracers() []*NetTracer {
	n.tracedMx.Lock()
	defer n.tracedMx.Unlock()

	return n.traced
}

func (n *NetGraph) pushPendingRelay() {
	n.relayPendingMx.Lock()
	ids := slices.Collect(maps.Keys(n.relayPending))
	clear(n.relayPending)
	n.relayPendingMx.Unlock()

	for _, id := range ids {
		n.Graph.loop.Lock()
		c := n.Graph.Machines[id]
		ser := &am.Serialized{
			ID:          c.ID,
			StateNames:  slices.Clone(c.StateNames),
			Time:        slices.Clone(c.Time),
			QueueTick:   c.QueueTick,
			MachineTick: c.MachineTick,
			Tags:        slices.Clone(c.Tags),
		}
		schemaHash := c.SchemaHash
		parent := c.Parent
		tagTick := c.TagsMTimeSum
		n.Graph.loop.Unlock()

		n.relaySentMx.Lock()
		last := n.relaySent[id]
		n.relaySentMx.Unlock()

		tagsChanged := last == nil || !slices.Equal(last.Tags, ser.Tags)
		dataChanged := last == nil || !slices.Equal(last.Time, ser.Time) ||
			last.QueueTick != ser.QueueTick ||
			last.MachineTick != ser.MachineTick
		if !dataChanged && !tagsChanged {
			continue
		}

		lastData := &rpc.TracerData{QueueTick: 1}
		if last != nil {
			lastData = tracerDataFromSerialized(last)
		}
		data := tracerDataFromSerialized(ser)
		update := rpc.CalcUpdate(true, data, lastData, false)
		if len(update.Indexes) == 0 && !tagsChanged {
			continue
		}
		if err := n.Broadcast(&MsgGraphUpdate{
			MsgSrvUpdate: update,
			MTimeSum:     ser.Time.Sum(nil),
			MachId:       ser.ID,
			SchemaHash:   schemaHash,
			Parent:       parent,
			Tags:         ser.Tags,
			TagsTime:     tagTick,
		}); err != nil {
			continue
		}

		n.relaySentMx.Lock()
		n.relaySent[id] = ser
		n.relaySentMx.Unlock()
	}
}

// queueDbSync schedules a coalesced clock sync of machId to the DB.
func (n *NetGraph) queueDbSync(machId string) {
	if n.Db == nil || machId == "" {
		return
	}

	n.dbPendingMx.Lock()
	n.dbPending[machId] = struct{}{}
	n.dbPendingMx.Unlock()

	n.dbLoopOnce.Do(func() { go n.dbSyncLoop(n.dbDone) })
}

// dbSyncLoop flushes coalesced clock syncs on the push interval.
func (n *NetGraph) dbSyncLoop(done chan struct{}) {
	interval := n.PushInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			n.flushDbSync()
		}
	}
}

// flushDbSync pushes up to [dbSyncChunk] pending clock syncs to the DB.
func (n *NetGraph) flushDbSync() {
	if n.Db == nil {
		return
	}

	n.dbPendingMx.Lock()
	ids := make([]string, 0, dbSyncChunk)
	for id := range n.dbPending {
		ids = append(ids, id)
		delete(n.dbPending, id)
		if len(ids) >= dbSyncChunk {
			break
		}
	}
	n.dbPendingMx.Unlock()

	for _, id := range ids {
		m, ok := n.Graph.Machine(id)
		if !ok {
			continue
		}
		if err := n.Db.SyncMachine(m); err != nil {
			n.Logger.Error("DbGraph sync failed", "mach", id, "err", err)
		}
	}
}

// flushDbSyncAll drains all pending clock syncs.
func (n *NetGraph) flushDbSyncAll() {
	for {
		n.dbPendingMx.Lock()
		pending := len(n.dbPending)
		n.dbPendingMx.Unlock()
		if pending == 0 {
			return
		}
		n.flushDbSync()
	}
}

func (n *NetGraph) marshal(v any) ([]byte, error) {
	if n.Json {
		return json.Marshal(v)
	}
	return msgpack.Marshal(v)
}

func (n *NetGraph) unmarshal(data []byte, v any) error {
	if n.Json {
		return json.Unmarshal(data, v)
	}
	return msgpack.Unmarshal(data, v)
}

func (n *NetGraph) schemaInfo(hash string) (SchemaInfo, bool) {
	n.schemaStoreMx.Lock()
	defer n.schemaStoreMx.Unlock()

	schema, ok := n.SchemaStore[hash]
	if ok {
		delete(n.MissingSchemas, hash)
	} else {
		n.MissingSchemas[hash] = struct{}{}
	}
	return schema, ok
}

// receiveFromUpstream receives messages from upstream peers from
// [NetGraph.EmitToAddr]. It's usually due to data corruption.
func (n *NetGraph) receiveFromUpstream(conn *net.UDPConn) {
	buf := make([]byte, 64*1024)
	for {
		read, err := conn.Read(buf)
		if err != nil {
			return
		}

		var msg MsgGraphReqSerialized
		if err := n.unmarshal(buf[:read], &msg); err != nil {
			continue
		}
		if msg.MachId != "" {
			n.sendSerializedToUpstream(msg.MachId, conn)
		}
	}
}

func (n *NetGraph) receiveFromDownstream() {
	buf := make([]byte, 64*1024)
	for {
		read, addr, err := n.listener.ReadFromUDP(buf)
		if err != nil {
			return
		}

		var update MsgGraphUpdate
		err = n.unmarshal(buf[:read], &update)
		if err != nil || update.MachId == "" {
			// a full serialized machine resend (see sendSerializedToUpstream)
			var ser am.Serialized
			if serErr := n.unmarshal(buf[:read], &ser); serErr == nil &&
				ser.ID != "" {
				n.processSerializedMachine(&ser)
				continue
			}
			if err != nil {
				n.Logger.Warn("graph update decode failed", "err", err)
			}
			continue
		}

		n.processDownstreamUpdate(&update, addr)
	}
}

// processSerializedMachine applies a full machine resend to the local mirror.
func (n *NetGraph) processSerializedMachine(ser *am.Serialized) {
	n.Graph.loop.Lock()
	defer n.Graph.loop.Unlock()

	gm := n.Graph.Machines[ser.ID]
	if gm == nil || gm.Serialized == nil {
		return
	}
	if !slices.Equal(gm.StateNames, ser.StateNames) {
		return
	}

	gm.Time = slices.Clone(ser.Time)
	gm.QueueTick = ser.QueueTick
	gm.MachineTick = ser.MachineTick
	gm.MTimeSum = ser.Time.Sum(nil)
	gm.TagsMTimeSum = gm.MTimeSum
	n.Graph.setClientTags(gm, ser.Tags)

	// drop the relay baseline, so the next relay is a full update
	n.relaySentMx.Lock()
	delete(n.relaySent, ser.ID)
	n.relaySentMx.Unlock()

	// queue the DB clock sync, coalesced on the sync tick
	n.queueDbSync(ser.ID)

	// relay upstream
	n.relayPendingMx.Lock()
	n.relayPending[ser.ID] = struct{}{}
	n.relayPendingMx.Unlock()
}

func (n *NetGraph) processDownstreamUpdate(
	msg *MsgGraphUpdate, from *net.UDPAddr,
) {
	n.Graph.loop.Lock()
	defer n.Graph.loop.Unlock()

	dbUpsert := false

	// init if missing
	gm := n.Graph.Machines[msg.MachId]
	if gm == nil {
		schema, ok := n.schemaInfo(msg.SchemaHash)
		if !ok {
			return
		}
		graphMach := &Machine{
			Serialized: &am.Serialized{
				ID:          msg.MachId,
				StateNames:  slices.Clone(schema.StatesIndex),
				Time:        make(am.Time, len(schema.StatesIndex)),
				QueueTick:   1,
				MachineTick: 0,
			},
			SchemaHash: msg.SchemaHash,
			Schema:     schema.Schema.Clone(),
			Parent:     msg.Parent,
		}
		err := n.Graph.AddMachine(graphMach)
		if err != nil {
			return
		}
		dbUpsert = true
		gm = n.Graph.Machines[msg.MachId]
	}

	// replace the client when the source schema changes
	if gm != nil && gm.Serialized != nil && gm.SchemaHash != msg.SchemaHash {
		schema, ok := n.schemaInfo(msg.SchemaHash)
		if !ok {
			return
		}
		graphMach := &Machine{
			Serialized: &am.Serialized{
				ID:          msg.MachId,
				StateNames:  slices.Clone(schema.StatesIndex),
				Time:        make(am.Time, len(schema.StatesIndex)),
				QueueTick:   1,
				MachineTick: 0,
			},
			SchemaHash: msg.SchemaHash,
			Schema:     schema.Schema.Clone(),
			Parent:     msg.Parent,
		}
		err := n.Graph.AddMachine(graphMach)
		if err != nil {
			return
		}
		dbUpsert = true

		// clear relay cache
		n.relaySentMx.Lock()
		delete(n.relaySent, msg.MachId)
		n.relaySentMx.Unlock()
	}

	// push the new machine to the DB
	if n.Db != nil && dbUpsert {
		if fresh := n.Graph.Machines[msg.MachId]; fresh != nil {
			if err := n.Db.UpsertMachine(fresh); err != nil {
				n.Logger.Error("DbGraph upsert failed",
					"mach", msg.MachId, "err", err)
			}
		}
	}

	if msg.MTimeSum != 0 && msg.MTimeSum < gm.MTimeSum {
		n.MetricUpdateTooOld.Add(1)

		return
	}

	// process and fwd
	if n.Graph.ParseMsgRpc(msg) {
		// queue the DB clock sync, coalesced on the sync tick
		n.queueDbSync(msg.MachId)

		n.relayPendingMx.Lock()
		n.relayPending[msg.MachId] = struct{}{}
		n.relayPendingMx.Unlock()

		return
	}

	n.Logger.Warn("graph apply failed, requesting resend", "mach", msg.MachId)
	n.MetricResendRequests.Add(1)
	_ = n.reqSerializedFromDownstream(from, msg.MachId)
}

func (n *NetGraph) reqSerializedFromDownstream(
	addr *net.UDPAddr, machID string,
) error {
	data, err := n.marshal(&MsgGraphReqSerialized{MachId: machID})
	if err != nil {
		return err
	}
	_, err = n.listener.WriteToUDP(data, addr)

	return err
}

func (n *NetGraph) sendSerializedToUpstream(machId string, conn *net.UDPConn) {
	if machId == "" {
		return
	}
	n.MetricResendsHandled.Add(1)

	// locally traced machines are re-sent from the live machine, the local
	// graph mirror holds the zero baseline
	ser := n.serializedLocal(machId)

	// mirrored machines are re-sent from the mirrored graph
	if ser == nil {
		gm, ok := n.Graph.Machine(machId)
		if !ok || gm.Serialized == nil {
			return
		}
		ser = &am.Serialized{
			ID:          gm.ID,
			StateNames:  gm.StateNames,
			Time:        gm.Time,
			QueueTick:   gm.QueueTick,
			MachineTick: gm.MachineTick,
			Tags:        gm.Tags,
		}
	}

	data, err := n.marshal(ser)
	if err != nil {
		n.Logger.Error("sendSerializedToUpstream", "err", err)
		return
	}
	if _, err := conn.Write(data); err != nil {
		n.Logger.Error("sendSerializedToUpstream", "err", err)
	}
}

// serializedLocal returns a full serialized snapshot of a locally traced
// machine, or nil if the machine is not traced.
func (n *NetGraph) serializedLocal(machId string) *am.Serialized {
	n.tracedMx.Lock()
	defer n.tracedMx.Unlock()

	for _, tracer := range n.traced {
		if tracer.MachId != machId {
			continue
		}
		mach := tracer.Mach

		return &am.Serialized{
			ID:          machId,
			StateNames:  slices.Clone(mach.StateNames()),
			Time:        slices.Clone(mach.Time(nil)),
			QueueTick:   mach.QueueTick(),
			MachineTick: mach.MachineTick(),
			Tags:        slices.Clone(mach.Tags()),
		}
	}

	return nil
}

// ///// ///// /////

// ///// SERVER

// ///// ///// /////

// DbServerOpts configures the Db HTTP server.
type DbServerOpts struct {
	// ApiKeys authorizes every request when non-empty. Clients pass a key in
	// the X-API-Key header, or as a Sec-WebSocket-Protocol token for
	// browser/WASM WebSocket clients (like tools/relay).
	ApiKeys []string
}

// Server exposes a types.DbGraph over HTTP+JSON and WebSocket on a single
// listener. Raw GQL is executed against [NetGraph.Db].
//
//	POST /query          raw GQL body, JSON reply (msgpack via Accept)
//	GET  /machine?id=X   one machine as [am.Serialized], lowercase JSON
//	GET  /ws             WebSocket, one frame per request:
//	                     GQL / {"machine": "X"} -> JSON or msgpack reply,
//	                     matching the frame type
type Server struct {
	// Addr is the actual listen address.
	Addr string

	graph   *NetGraph
	srv     *http.Server
	apiKeys []string

	wsMx sync.Mutex
	// TODO option to send JSON over WS
	ws map[*websocket.Conn]struct{}
}

// StartDbHttpServer starts a [Server] on addr exposing [NetGraph.Db]. It
// returns the actual listen address. [NetGraph.Close] shuts it down.
func (n *NetGraph) StartDbHttpServer(
	addr string, opts ...DbServerOpts,
) (string, error) {
	if n.Db == nil {
		return "", errors.New("graph: Db is nil")
	}

	n.dbSrvMx.Lock()
	defer n.dbSrvMx.Unlock()

	if n.dbSrv != nil {
		return "", errors.New("graph: DB server already started")
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}

	s := &Server{
		Addr:  ln.Addr().String(),
		graph: n,
		ws:    make(map[*websocket.Conn]struct{}),
	}
	for _, opt := range opts {
		s.apiKeys = slices.Clone(opt.ApiKeys)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /query", s.handleQuery)
	mux.HandleFunc("GET /machine", s.handleMachine)
	mux.HandleFunc("GET /ws", s.handleWS)
	s.srv = &http.Server{Handler: s.authMiddleware(mux)}

	go func() {
		if err := s.srv.Serve(ln); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			n.Logger.Error("DbServer", "err", err)
		}
	}()

	n.dbSrv = s

	return s.Addr, nil
}

// authMiddleware authorizes every request when API keys are configured. Keys
// ride in the X-API-Key header, or in Sec-WebSocket-Protocol for
// browser/WASM WebSocket clients. Mirrors tools/relay.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.apiKeys) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		key := r.Header.Get("X-API-Key")
		if key == "" {
			// browser/WASM clients can't set headers on the WS handshake
			for _, proto := range strings.Split(
				r.Header.Get("Sec-WebSocket-Protocol"), ",") {
				proto = strings.TrimSpace(proto)
				if proto != "" && !strings.EqualFold(proto, "X-API-Key") {
					key = proto
					break
				}
			}
		}

		if !slices.Contains(s.apiKeys, key) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Close stops the server and its WebSocket connections.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}

	s.wsMx.Lock()
	for conn := range s.ws {
		_ = conn.CloseNow()
	}
	s.ws = nil
	s.wsMx.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.srv.Shutdown(ctx); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

// PRIVATE

// handleQuery serves POST /query with a raw GQL statement.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		s.writeErr(w, r, http.StatusBadRequest, err)
		return
	}

	q := strings.TrimSpace(string(body))
	if q == "" {
		s.writeErr(w, r, http.StatusBadRequest, errors.New("empty query"))
		return
	}

	res, err := s.graph.Db.Exec(q)
	if err != nil {
		s.writeErr(w, r, http.StatusBadRequest, err)
		return
	}

	s.write(w, r, http.StatusOK, res)
}

// handleMachine serves GET /machine?id=X with an [am.Serialized].
func (s *Server) handleMachine(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		s.writeErr(w, r, http.StatusBadRequest, errors.New("missing id"))
		return
	}

	ser, err := s.machineSerialized(id)
	if err != nil {
		s.writeErr(w, r, http.StatusInternalServerError, err)
		return
	}
	if ser == nil {
		s.writeErr(w, r, http.StatusNotFound, errors.New("machine not found"))
		return
	}

	s.write(w, r, http.StatusOK, ser)
}

// handleWS serves GET /ws, one frame per request:
//
//	GQL text frame                -> JSON rows
//	{"machine": "<id>"} text      -> JSON [am.Serialized]
//	msgpack GQL string (binary)   -> msgpack rows
//	msgpack {"machine": "<id>"}   -> msgpack [am.Serialized]
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()

	s.wsMx.Lock()
	if s.ws == nil {
		// closed concurrently
		s.wsMx.Unlock()
		return
	}
	s.ws[conn] = struct{}{}
	s.wsMx.Unlock()

	ctx := s.graph.ctx
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			break
		}

		q, machID := wsFrameRequest(typ, data)

		var res any
		switch {
		case machID != "":
			ser, err := s.machineSerialized(machID)
			switch {
			case err != nil:
				res = map[string]string{"error": err.Error()}
			case ser == nil:
				res = map[string]string{"error": "machine not found"}
			default:
				res = ser
			}
		case q == "":
			res = map[string]string{"error": "empty query"}
		default:
			if rows, err := s.graph.Db.Exec(q); err != nil {
				res = map[string]string{"error": err.Error()}
			} else {
				res = rows
			}
		}

		out, err := encodeWS(typ, res)
		if err != nil {
			break
		}
		if err := conn.Write(ctx, typ, out); err != nil {
			break
		}
	}

	s.wsMx.Lock()
	delete(s.ws, conn)
	s.wsMx.Unlock()
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func tracerDataFromSerialized(ser *am.Serialized) *rpc.TracerData {
	trackedIdxs := make([]int, len(ser.StateNames))
	for i := range trackedIdxs {
		trackedIdxs[i] = i
	}
	sum := ser.Time.Sum(nil)

	return &rpc.TracerData{
		MTime:           append(am.Time(nil), ser.Time...),
		MTrackedTimeSum: sum,
		QueueTick:       ser.QueueTick,
		MachTick:        ser.MachineTick,
		Checksum:        rpc.Checksum(sum, ser.QueueTick, ser.MachineTick),
		Tracked:         ser.StateNames,
		TrackedIdxs:     trackedIdxs,
	}
}

// wsFrameRequest parses a WS frame into a GQL query or a serialized machine
// request, matching the frame encoding.
func wsFrameRequest(
	typ websocket.MessageType, data []byte,
) (gqlQuery string, machID string) {
	unmarshal := json.Unmarshal
	if typ == websocket.MessageBinary {
		unmarshal = msgpack.Unmarshal
	}

	// a JSON/msgpack envelope, eg {"machine": "id"}
	var env map[string]string
	if err := unmarshal(data, &env); err == nil {
		if id := env["machine"]; id != "" {
			return "", id
		}
	}

	q := string(data)
	if typ == websocket.MessageBinary {
		// a msgpack-encoded GQL string
		var str string
		if err := msgpack.Unmarshal(data, &str); err == nil {
			q = str
		}
	}

	return strings.TrimSpace(q), ""
}

// machineSerialized rebuilds an [am.Serialized] from the mirrored DB, with
// states ordered by the schema's state index.
func (s *Server) machineSerialized(id string) (*am.Serialized, error) {
	g := s.graph
	lit := gqlStr(id)

	// machine props, states, and tags in one statement
	res, err := g.Db.Exec(fmt.Sprintf(
		"MATCH (m:Machine {id: %s})"+
			" OPTIONAL MATCH (m)-[:HAS]->(s:State)"+
			" OPTIONAL MATCH (m)-[:HAS_TAG]->(t:Tag)"+
			" RETURN m.schema_hash AS schema_hash,"+
			" m.queue_tick AS queue_tick, m.mach_tick AS mach_tick,"+
			" s.name AS state_name, s.tick AS tick, t.name AS tag", lit))
	if err != nil {
		return nil, err
	}
	rows, ok := res.([]any)
	if !ok || len(rows) == 0 {
		return nil, nil
	}

	ticks := make(map[string]uint64)
	tagSet := make(map[string]struct{})
	var props map[string]any
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		if props == nil {
			props = m
		}
		if name, ok := m["state_name"].(string); ok {
			ticks[name] = toUint64(m["tick"])
		}
		if tag, ok := m["tag"].(string); ok {
			tagSet[tag] = struct{}{}
		}
	}
	if props == nil {
		return nil, errors.New("unexpected machine row")
	}

	var tags []string
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	slices.Sort(tags)

	ser := &am.Serialized{
		ID:          id,
		QueueTick:   toUint64(props["queue_tick"]),
		MachineTick: uint32(toUint64(props["mach_tick"])),
		Tags:        tags,
	}

	// states ordered by the schema index
	if hash, ok := props["schema_hash"].(string); ok {
		g.schemaStoreMx.Lock()
		schema, ok := g.SchemaStore[hash]
		g.schemaStoreMx.Unlock()
		if ok {
			ser.StateNames = slices.Clone(schema.StatesIndex)
			ser.Time = make(am.Time, len(schema.StatesIndex))
			for i, name := range schema.StatesIndex {
				ser.Time[i] = ticks[name]
			}
		}
	}

	return ser, nil
}

// write encodes v according to the request's Accept header.
func (s *Server) write(
	w http.ResponseWriter, r *http.Request, code int, v any,
) {
	if wantsMsgpack(r) {
		out, err := msgpack.Marshal(v)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/msgpack")
		w.WriteHeader(code)
		_, _ = w.Write(out)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr writes a lowercase error envelope.
func (s *Server) writeErr(
	w http.ResponseWriter, r *http.Request, code int, err error,
) {
	s.write(w, r, code, map[string]string{"error": err.Error()})
}

// encodeWS encodes v in the frame type's format.
func encodeWS(typ websocket.MessageType, v any) ([]byte, error) {
	if typ == websocket.MessageBinary {
		return msgpack.Marshal(v)
	}

	return json.Marshal(v)
}

// wantsMsgpack reports whether the request asks for a msgpack reply.
func wantsMsgpack(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/msgpack") ||
		strings.Contains(r.Header.Get("Content-Type"), "application/msgpack")
}

// toUint64 converts a decoded GQL number to uint64.
func toUint64(v any) uint64 {
	switch n := v.(type) {
	case float64:
		return uint64(n)
	case float32:
		return uint64(n)
	case int:
		return uint64(n)
	case int64:
		return uint64(n)
	case uint64:
		return n
	default:
		return 0
	}
}

// gqlStr formats a GQL string literal. The frogql lexer has no escape syntax,
// values containing a single quote are unsupported.
func gqlStr(s string) string {
	return "'" + s + "'"
}
