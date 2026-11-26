package graph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"sync"
	"time"

	"gorm.io/gorm"

	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/types"
	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	amhistgorm "github.com/pancsta/asyncmachine-go/pkg/history/gorm"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
)

var (
	// ErrHistory is when a history graph operation fails.
	ErrHistory = errors.New("history graph error")
	// ErrHistoryParams is when a history action gets invalid params.
	ErrHistoryParams = errors.New("history params error")
	// ErrHistoryAction is when a history action is unknown.
	ErrHistoryAction = errors.New("history action error")
	// ErrMachineNotTracked is when a machine has no history (yet).
	ErrMachineNotTracked = errors.New("machine not tracked")
)

// HistoryGraph wraps a [Graph] with a sharded GORM history mirror. Each
// tracked machine gets a [rpc.NetworkMachine] and a GORM history tracer, all
// sharing [HistoryGraph.Cfg]. Clock updates from the graph are applied to the
// network machines, which emit transitions to their history tracers.
//
// Machines are distributed across [HistoryGraph.Dbs] shards using the least
// loaded shard at registration time. The databases and their connections are
// owned by the caller. A shard can be shared by many [HistoryGraph] instances.
type HistoryGraph struct {
	*Graph

	// Cfg is the history config shared by all machines.
	Cfg amhistgorm.Config
	// Dbs are the history database shards.
	Dbs []*gorm.DB

	ctx    context.Context
	onErr  func(err error)
	parent am.Api
	// ownPar is a private parent machine, disposed on Close.
	ownPar *am.Machine

	mx      sync.Mutex
	machs   map[string]*historyMach
	shardOf map[string]uint8
	// shardLen tracks the number of machines per shard, for new assignments.
	shardLen []int
	// gen cancels in-flight registrations on Remove and Clear.
	gen     map[string]uint64
	genSeq  uint64
	pending map[string]string
	// pendingClocks buffers the latest clocks of machines being registered.
	pendingClocks map[string]pendingClock

	regCh    chan trackReq
	regWg    sync.WaitGroup
	stop     chan struct{}
	stopOnce sync.Once
}

// pendingClock is the latest clock update of a machine being registered.
type pendingClock struct {
	mTime    am.Time
	qTick    uint64
	machTick uint32
}

// trackReq is a queued machine registration.
type trackReq struct {
	c     *Machine
	shard uint8
	gen   uint64
}

// HistoryGraphOpts configures a new [HistoryGraph].
type HistoryGraphOpts struct {
	// OnErr handles history errors. Defaults to [Graph.ParentMach.AddErr], or
	// [log/slog].
	OnErr func(err error)
	// Parent parents all history network machines. Defaults to
	// [Graph.ParentMach], or a private machine.
	Parent am.Api
}

// historyMach is a single machine's history mirror.
type historyMach struct {
	mx sync.Mutex

	netMach *rpc.NetworkMachine
	nmInt   *rpc.NetMachInternal
	mem     amhist.MemoryApi

	// hash is the schema hash of the mirrored machine.
	hash string
	// states is the number of state names of the mirrored machine.
	states int
	// shard is the history DB shard of the machine.
	shard uint8
}

// HistParams are the optional params of a history action.
type HistParams struct {
	// State is the state of the bool actions.
	State string
	// States is the state list of latest-activated / latest-deactivated.
	States am.S
	// Start is the start of the human time range of the bool actions and
	// find-latest.
	Start time.Time
	// End is the end of the human time range of the bool actions and
	// find-latest.
	End time.Time
	// Limit is the maximum number of find-latest records. Default: 100.
	Limit int
	// Query are the filters of find-latest.
	Query amhist.Query
}

// HistoryResp is the history payload of a machine.
type HistoryResp struct {
	// Action is the executed history action.
	Action amtypes.HistoryAction `json:"action"`
	// Machine is the machine ID.
	Machine string `json:"machine"`
	// Bool is the result of the bool actions.
	Bool bool `json:"bool,omitempty"`
	// State is the result of latest-activated / latest-deactivated.
	State string `json:"state,omitempty"`
	// Records is the result of find-latest.
	Records []*amhist.MemoryRecord `json:"records,omitempty"`
	// Record is the machine record of find-latest.
	Record *amhist.MachineRecord `json:"record,omitempty"`
}

// NewHistoryGraph wraps g with a sharded GORM history mirror and tracks all of
// its machines. Each db is migrated once and used as a history shard.
func NewHistoryGraph(
	g *Graph, dbs []*gorm.DB, cfg amhistgorm.Config, opts ...HistoryGraphOpts,
) (*HistoryGraph, error) {
	if g == nil {
		return nil, fmt.Errorf("%w: graph is nil", ErrHistory)
	}
	if len(dbs) == 0 {
		return nil, fmt.Errorf("%w: no dbs", ErrHistory)
	}
	if len(dbs) > 255 {
		return nil, fmt.Errorf("%w: too many shards: %d", ErrHistory, len(dbs))
	}
	for i, db := range dbs {
		if db == nil {
			return nil, fmt.Errorf("%w: db %d is nil", ErrHistory, i)
		}
		if err := amhistgorm.Migrate(db); err != nil {
			return nil, fmt.Errorf("%w: migrate shard %d: %w", ErrHistory, i, err)
		}
	}

	hg := &HistoryGraph{
		Graph:         g,
		Cfg:           cfg,
		Dbs:           dbs,
		ctx:           context.Background(),
		machs:         make(map[string]*historyMach),
		shardOf:       make(map[string]uint8),
		shardLen:      make([]int, len(dbs)),
		gen:           make(map[string]uint64),
		pending:       make(map[string]string),
		pendingClocks: make(map[string]pendingClock),
		regCh:         make(chan trackReq, 4096),
		stop:          make(chan struct{}),
	}
	// the db connections are shared by all machines
	hg.Cfg.KeepDb = true
	// the shards are migrated once above
	hg.Cfg.SkipMigrate = true
	if g.ParentMach != nil {
		hg.ctx = g.ParentMach.Context()
	}
	hg.onErr = func(err error) {
		if g.ParentMach != nil {
			g.ParentMach.AddErr(err, nil)
			return
		}
		slog.Error("history graph", "err", err)
	}
	hg.parent = g.ParentMach
	for _, opt := range opts {
		if opt.OnErr != nil {
			hg.onErr = opt.OnErr
		}
		if opt.Parent != nil {
			hg.parent = opt.Parent
		}
	}
	if hg.parent == nil {
		hg.ownPar = am.New(hg.ctx, nil, &am.Opts{Id: "history-graph"})
		hg.parent = hg.ownPar
	}

	g.hist = hg

	// one registration worker per shard, so sqlite writes don't serialize
	for range dbs {
		hg.regWg.Add(1)
		go hg.regLoop()
	}

	// track existing machines
	g.loop.Lock()
	for _, c := range g.Machines {
		hg.Track(c)
	}
	g.loop.Unlock()

	return hg, nil
}

// Track starts mirroring a graph machine into the history. It's called by
// [Graph.AddMachine]. Re-tracking with the same schema hash is a no-op.
// Registration is asynchronous; [HistoryGraph.Update] calls made meanwhile are
// buffered and applied once the machine is registered.
func (hg *HistoryGraph) Track(c *Machine) {
	if c == nil || c.Serialized == nil {
		return
	}

	hg.mx.Lock()
	if old := hg.machs[c.ID]; old != nil && old.hash == c.SchemaHash {
		hg.mx.Unlock()
		return
	}
	if hash, ok := hg.pending[c.ID]; ok && hash == c.SchemaHash {
		hg.mx.Unlock()
		return
	}
	shard := hg.assignShard(c.ID)
	hg.genSeq++
	gen := hg.genSeq
	hg.gen[c.ID] = gen
	hg.pending[c.ID] = c.SchemaHash
	hg.mx.Unlock()

	// Track may race with Close, don't enqueue after it
	select {
	case <-hg.stop:
		return
	default:
	}

	req := trackReq{c: c.Clone(), shard: shard, gen: gen}
	select {
	case hg.regCh <- req:
	case <-hg.stop:
	}
}

// assignShard returns the shard of a machine, assigning the least loaded one
// for new machines. The caller must hold [HistoryGraph.mx].
func (hg *HistoryGraph) assignShard(id string) uint8 {
	if s, ok := hg.shardOf[id]; ok {
		return s
	}

	s := 0
	for i := 1; i < len(hg.shardLen); i++ {
		if hg.shardLen[i] < hg.shardLen[s] {
			s = i
		}
	}
	hg.shardOf[id] = uint8(s)
	hg.shardLen[s]++

	return uint8(s)
}

// regLoop registers queued machines until Close.
func (hg *HistoryGraph) regLoop() {
	defer hg.regWg.Done()

	for {
		select {
		case <-hg.stop:
			return
		default:
		}

		select {
		case <-hg.stop:
			return
		case req := <-hg.regCh:
			hg.regMachine(req)
		}
	}
}

// regMachine creates the history mirror of a queued machine.
func (hg *HistoryGraph) regMachine(req trackReq) {
	c := req.c
	// netmachs require the schema to match the state names exactly
	schema := normalizeSchema(c.Schema, c.StateNames)

	netMach, nmInt, err := rpc.NewNetworkMachine(hg.ctx, c.ID, nil, schema,
		c.StateNames, hg.parent, nil, false)
	if err != nil {
		hg.regFailed(req, fmt.Errorf("%w: %s: %w", ErrHistory, c.ID, err))
		return
	}
	mem, err := amhistgorm.NewMemory(hg.ctx, hg.Dbs[req.shard], netMach,
		hg.Cfg, hg.onErr)
	if err != nil {
		netMach.Dispose()
		hg.regFailed(req, err)
		return
	}

	hm := &historyMach{
		netMach: netMach,
		nmInt:   nmInt,
		mem:     mem,
		hash:    c.SchemaHash,
		states:  len(c.StateNames),
		shard:   req.shard,
	}

	hg.mx.Lock()
	if hg.gen[c.ID] != req.gen {
		// removed or superseded while registering
		hg.mx.Unlock()
		_ = mem.Dispose()
		netMach.Dispose()
		return
	}
	old := hg.machs[c.ID]
	hg.machs[c.ID] = hm
	delete(hg.pending, c.ID)
	pc, hasPC := hg.pendingClocks[c.ID]
	delete(hg.pendingClocks, c.ID)
	hg.mx.Unlock()

	if old != nil {
		hg.dispose(old)
	}
	// record the latest clock, buffered while registering
	if hasPC {
		hm.update(pc.mTime, pc.qTick, pc.machTick)
	} else if c.MTimeSum > 0 {
		hm.update(c.Time, c.QueueTick, c.MachineTick)
	}
}

// regFailed reports a failed registration and clears its pending entry.
func (hg *HistoryGraph) regFailed(req trackReq, err error) {
	hg.mx.Lock()
	if hg.gen[req.c.ID] == req.gen {
		delete(hg.pending, req.c.ID)
		delete(hg.pendingClocks, req.c.ID)
	}
	hg.mx.Unlock()

	hg.onErr(err)
}

// Update applies the current clocks of a machine to its history mirror. It's a
// no-op for machines which aren't tracked. Updates of machines being
// registered are buffered and applied after registration.
func (hg *HistoryGraph) Update(
	id string, mTime am.Time, qTick uint64, machTick uint32,
) {
	hg.mx.Lock()
	// buffer for a pending registration, including schema changes
	_, pending := hg.pending[id]
	if hm := hg.machs[id]; hm != nil && !pending {
		hg.mx.Unlock()
		hm.update(mTime, qTick, machTick)
		return
	}
	if _, ok := hg.gen[id]; ok {
		hg.pendingClocks[id] = pendingClock{
			mTime:    slices.Clone(mTime),
			qTick:    qTick,
			machTick: machTick,
		}
	}
	hg.mx.Unlock()
}

// Remove stops mirroring a machine and disposes its history.
func (hg *HistoryGraph) Remove(id string) {
	hg.mx.Lock()
	// cancel in-flight registrations
	delete(hg.gen, id)
	delete(hg.pending, id)
	delete(hg.pendingClocks, id)

	hm := hg.machs[id]
	delete(hg.machs, id)
	if s, ok := hg.shardOf[id]; ok {
		delete(hg.shardOf, id)
		hg.shardLen[s]--
	}
	hg.mx.Unlock()

	if hm != nil {
		hg.dispose(hm)
	}
}

// ShardCounts returns the number of machines assigned to each history shard.
func (hg *HistoryGraph) ShardCounts() []int {
	hg.mx.Lock()
	defer hg.mx.Unlock()

	return slices.Clone(hg.shardLen)
}

// TrackedCount returns the number of machines with a registered history.
func (hg *HistoryGraph) TrackedCount() int {
	hg.mx.Lock()
	defer hg.mx.Unlock()

	return len(hg.machs)
}

// ShardOf returns the history shard of a machine.
func (hg *HistoryGraph) ShardOf(id string) (uint8, bool) {
	hg.mx.Lock()
	defer hg.mx.Unlock()

	if hm := hg.machs[id]; hm != nil {
		return hm.shard, true
	}
	s, ok := hg.shardOf[id]
	return s, ok
}

// History runs a history API method of a tracked machine.
func (hg *HistoryGraph) History(
	ctx context.Context, id string, action amtypes.HistoryAction, p HistParams,
) (*HistoryResp, error) {
	hg.mx.Lock()
	hm := hg.machs[id]
	hg.mx.Unlock()

	if hm == nil {
		return nil, fmt.Errorf("%w: %s", ErrMachineNotTracked, id)
	}

	resp := &HistoryResp{Action: action, Machine: id}
	mem := hm.mem

	switch action {
	case amtypes.HistActivatedBetween, amtypes.HistActiveBetween,
		amtypes.HistDeactivatedBetween, amtypes.HistInactiveBetween:

		if p.State == "" {
			return nil, fmt.Errorf("%w: state is required", ErrHistoryParams)
		}
		switch action {
		case amtypes.HistActivatedBetween:
			resp.Bool = mem.ActivatedBetween(ctx, p.State, p.Start, p.End)
		case amtypes.HistActiveBetween:
			resp.Bool = mem.ActiveBetween(ctx, p.State, p.Start, p.End)
		case amtypes.HistDeactivatedBetween:
			resp.Bool = mem.DeactivatedBetween(ctx, p.State, p.Start, p.End)
		case amtypes.HistInactiveBetween:
			resp.Bool = mem.InactiveBetween(ctx, p.State, p.Start, p.End)
		}

	case amtypes.HistLatestActivated, amtypes.HistLatestDeactivated:
		if len(p.States) == 0 {
			return nil, fmt.Errorf("%w: states are required", ErrHistoryParams)
		}
		if action == amtypes.HistLatestActivated {
			resp.State = mem.LatestActivated(ctx, p.States...)
		} else {
			resp.State = mem.LatestDeactivated(ctx, p.States...)
		}

	case amtypes.HistFindLatest:
		// flush the queue, so fresh records are queryable
		if err := mem.Sync(ctx); err != nil {
			return nil, err
		}
		limit := p.Limit
		if limit <= 0 {
			limit = 100
		}
		records, err := mem.FindLatest(ctx, true, limit, p.Query)
		if err != nil {
			return nil, err
		}
		resp.Records = records
		resp.Record = mem.MachineRecord()

	default:
		return nil, fmt.Errorf("%w: %s", ErrHistoryAction, action.Value)
	}

	return resp, nil
}

// Close stops mirroring all machines. The databases are left open.
func (hg *HistoryGraph) Close() error {
	// detach first, so no new machines get tracked
	hg.Graph.loop.Lock()
	if hg.Graph.hist == hg {
		hg.Graph.hist = nil
	}
	hg.Graph.loop.Unlock()

	hg.stopOnce.Do(func() { close(hg.stop) })
	hg.regWg.Wait()
	hg.clearAll()

	if hg.ownPar != nil {
		hg.ownPar.Dispose()
		hg.ownPar = nil
	}

	return nil
}

// private

// clearAll stops mirroring all machines.
func (hg *HistoryGraph) clearAll() {
	hg.mx.Lock()
	// cancel in-flight registrations
	clear(hg.gen)
	clear(hg.pending)
	clear(hg.pendingClocks)
	clear(hg.shardOf)
	for i := range hg.shardLen {
		hg.shardLen[i] = 0
	}

	list := make([]*historyMach, 0, len(hg.machs))
	for id, hm := range hg.machs {
		list = append(list, hm)
		delete(hg.machs, id)
	}
	hg.mx.Unlock()

	// dispose in parallel, the histories are independent
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for _, hm := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(hm *historyMach) {
			defer wg.Done()
			defer func() { <-sem }()
			hg.dispose(hm)
		}(hm)
	}
	wg.Wait()
}

// dispose disposes a machine's history and network machine. The history is
// disposed first, because the network machine runs its dispose handlers under
// a lock. The caller must not hold [HistoryGraph.mx].
func (hg *HistoryGraph) dispose(hm *historyMach) {
	// wait for in-flight clock updates
	hm.mx.Lock()
	defer hm.mx.Unlock()

	_ = hm.mem.Dispose()
	hm.netMach.Dispose()
}

// update applies clocks to the network machine, which emits them to the
// history tracer.
func (hm *historyMach) update(
	mTime am.Time, qTick uint64, machTick uint32,
) {
	hm.mx.Lock()
	defer hm.mx.Unlock()

	if len(mTime) != hm.states {
		return
	}
	hm.nmInt.Lock()
	hm.nmInt.UpdateClock(slices.Clone(mTime), qTick, machTick)
}

// normalizeSchema returns a schema matching the passed state names exactly,
// filling in missing states with empty definitions.
func normalizeSchema(schema am.Schema, stateNames am.S) am.Schema {
	ret := make(am.Schema, len(stateNames))
	for _, name := range stateNames {
		ret[name] = schema[name]
	}

	return ret
}
