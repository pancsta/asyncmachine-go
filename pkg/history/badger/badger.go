// Package badger provides machine history tracking and traversal using
// the Badger K/V database.
package badger

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	"github.com/vmihailenco/msgpack/v5"

	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type MatcherFn func(now *am.TimeIndex, txn *badger.Txn) []*amhist.MemoryRecord

type Config struct {
	amhist.BaseConfig
	EncJson bool
	// amount of records to save in bulk (default: 100)
	QueueBatch int32
}

const (
	prefixMachines = "_machines/"
	suffixTimes    = "/times/"
	suffixTxs      = "/txs/"
	// gcSize is the max number of deletes per GC transaction.
	gcSize = 1000
)

func machineKey(machId string) []byte {
	return []byte(prefixMachines + machId)
}

func timeKey(machId string, id uint64) []byte {
	k := make([]byte, len(machId)+len(suffixTimes)+8)
	copy(k, machId)
	copy(k[len(machId):], suffixTimes)
	binary.BigEndian.PutUint64(k[len(machId)+len(suffixTimes):], id)
	return k
}

func txKey(machId string, id uint64) []byte {
	k := make([]byte, len(machId)+len(suffixTxs)+8)
	copy(k, machId)
	copy(k[len(machId):], suffixTxs)
	binary.BigEndian.PutUint64(k[len(machId)+len(suffixTxs):], id)
	return k
}

// ///// ///// /////

// ///// TRACER

// ///// ///// /////

type tracer struct {
	*am.TracerNoOp

	mem *Memory
	id  string
}

func (t *tracer) TracerId() string {
	return "badger" + t.id
}

func (t *tracer) MachineInit(mach am.Api) context.Context {
	m := t.mem
	now := time.Now().UTC()

	// locks
	m.mx.Lock()
	defer m.mx.Unlock()

	// upsert machine record
	mTime := mach.Time(nil)
	// TODO handle DB errs
	rec, _ := GetMachine(m.Db, mach.Id())
	if rec == nil {
		m.machRec = &amhist.MachineRecord{
			MachId:        mach.Id(),
			FirstTracking: now,
			NextId:        1,
		}
		rec = m.machRec
	} else {
		m.machRec = rec
	}

	rec.MTimeSum = mTime.Sum(nil)
	rec.MTime = mTime
	rec.LastTracking = now
	rec.MachTick = mach.MachineTick()
	rec.LastSync = now
	m.nextId.Store(rec.NextId)

	// schema
	if m.Cfg.StoreSchema {
		rec.Schema = mach.Schema()
		rec.StateNames = mach.StateNames()
	}

	// save machine record
	err := m.Db.Update(func(txn *badger.Txn) error {
		enc, err := m.encode(m.machRec)
		if err != nil {
			return err
		}
		return txn.Set(machineKey(mach.Id()), enc)
	})
	if err != nil {
		m.onErr(err)
	}

	return nil
}

func (t *tracer) SchemaChange(machine am.Api, old am.Schema) {
	m := t.mem
	if m.Ctx.Err() != nil {
		_ = m.Dispose()
		return
	}

	// locks
	m.mx.Lock()
	defer m.mx.Unlock()

	// update schema
	if !m.Cfg.StoreSchema {
		return
	}
	rec := m.machRec
	rec.Schema = m.Mach.Schema()
	rec.StateNames = m.Mach.StateNames()

	err := m.Db.Update(func(txn *badger.Txn) error {
		enc, err := m.encode(rec)
		if err != nil {
			return err
		}
		return txn.Set(machineKey(rec.MachId), enc)
	})
	if err != nil {
		m.onErr(err)
	}
}

func (t *tracer) TransitionEnd(tx *am.Transition) {
	m := t.mem
	if m.Ctx.Err() != nil {
		_ = m.Dispose()
		return
	}

	if (!tx.IsAccepted.Load() && !m.Cfg.TrackRejected) || tx.Mutation.IsCheck {
		return
	}

	mach := m.Mach
	called := tx.CalledStates()
	changed := tx.TimeAfter.DiffSince(tx.TimeBefore).
		ToIndex(mach.StateNames()).NonZeroStates()
	mut := tx.Mutation
	cfg := m.Cfg
	match := (cfg.ChangedExclude || len(cfg.Changed) == 0) &&
		(cfg.CalledExclude || len(cfg.Called) == 0)
	mTime := tx.TimeAfter
	mTimeTracked := mTime.Filter(m.cacheTrackedIdxs)
	mTimeTrackedBefore := tx.TimeBefore.Filter(m.cacheTrackedIdxs)
	sum := mTime.Sum(nil)
	sumTracked := mTimeTracked.Sum(nil)

	// process called
	for _, name := range cfg.Called {
		listed := slices.Contains(called, name)
		if listed && cfg.CalledExclude {
			match = false
			break
		} else if !listed && !cfg.CalledExclude {
			match = true
			break
		}
	}

	// process changed
	for _, name := range cfg.Changed {
		listed := slices.Contains(changed, name)
		if listed && cfg.ChangedExclude {
			match = false
			break
		} else if !listed && !cfg.ChangedExclude {
			match = true
			break
		}
	}

	if !match {
		return
	}

	// lock
	m.mx.Lock()
	defer m.mx.Unlock()

	// time record
	var recordDiff uint64
	if m.lastRec != nil {
		recordDiff = sum - m.lastRec.MTimeSum
	}
	machTick := mach.MachineTick()
	now := time.Now().UTC()
	recTime := &amhist.TimeRecord{
		MutType:             mut.Type,
		MTimeSum:            sum,
		MTimeTrackedSum:     sumTracked,
		MTimeDiffSum:        sum - tx.TimeBefore.Sum(nil),
		MTimeTrackedDiffSum: sumTracked - tx.TimeBefore.Sum(m.cacheTrackedIdxs),
		MTimeRecordDiffSum:  recordDiff,
		MachTick:            machTick,
		HTime:               now,
		MTimeTracked:        mTimeTracked,
		MTimeTrackedDiff:    mTimeTracked.DiffSince(mTimeTrackedBefore),
	}

	// optional tx record
	var recTx *amhist.TransitionRecord
	if m.Cfg.StoreTransitions {
		// link time record
		recTx = &amhist.TransitionRecord{
			TransitionId: tx.Id,
			Called:       mut.Called,
			IsAuto:       mut.IsAuto,
			IsAccepted:   tx.IsAccepted.Load(),
			IsCheck:      mut.IsCheck,
			IsBroken:     tx.IsBroken.Load(),
			QueueLen:     mach.QueueLen(),
			QueuedAt:     mut.QueueTick,
			// TODO optimize?
			Arguments: mut.MapArgs(mach.SemLogger().ArgsMapper()),
		}

		// optional fields
		if mut.Source != nil {
			recTx.SourceTx = mut.Source.TxId
			recTx.SourceMach = mut.Source.MachId
		}
		if mut.QueueTick > 0 {
			qt := mach.QueueTick()
			recTx.ExecutedAt = qt
		}
	}

	// update machine record
	machRec := m.machRec
	machRec.MTime = mTime
	machRec.MTimeSum = sum
	machRec.LastSync = now
	machRec.MachTick = machTick
	machRec.NextId++

	// queue, cache, GC
	m.queue.ids = append(m.queue.ids, m.nextId.Load())
	m.nextId.Add(1)
	m.queue.times = append(m.queue.times, recTime)
	m.queue.txs = append(m.queue.txs, recTx)
	m.SavePending.Add(1)
	m.lastRec = recTime
	// TODO ensure save after a delay
	if m.SavePending.Load() >= m.Cfg.QueueBatch {
		m.syncMx.RLock()
		m.writeDb(true)
		m.checkGc()
	}
}

// ///// ///// /////

// ///// MEMORY

// ///// ///// /////

func NewDb(name string) (*badger.DB, error) {
	if name == "" {
		name = "amhist"
	}

	opts := badger.DefaultOptions(name + ".badger")
	if amhelp.IsWasm() || amhelp.IsWasi() {
		opts = badger.DefaultOptions("").WithInMemory(true)
	}

	// TODO handle logger
	opts.Logger = nil

	return badger.Open(opts)
}

type queue struct {
	times []*amhist.TimeRecord
	txs   []*amhist.TransitionRecord
	ids   []uint64
}

type Memory struct {
	*amhist.BaseMemory

	Db *badger.DB
	// read-only config for this history
	Cfg            *Config
	SavePending    atomic.Int32
	SaveInProgress atomic.Bool
	Saved          atomic.Uint64
	// Value of Saved at the end of the last GC
	SavedGc atomic.Uint64

	// sync lock (read: flush, write: sync)
	syncMx sync.RWMutex
	// nextId sequence ID
	nextId atomic.Uint64
	// garbage collector lock (read: query, write: GC)
	gcMx sync.RWMutex
	// TODO use Context
	disposed atomic.Bool
	queue    *queue
	onErr    func(err error)
	machRec  *amhist.MachineRecord
	// global lock, needed mostly for [Memory.MachineRecord].
	mx               sync.Mutex
	tr               *tracer
	cacheTrackedIdxs []int
	lastRec          *amhist.TimeRecord
}

func NewMemory(
	ctx context.Context, db *badger.DB, mach am.Api, cfg Config,
	onErr func(err error),
) (*Memory, error) {

	c := cfg
	if c.MaxRecords <= 0 {
		c.MaxRecords = 1000
	}
	if c.QueueBatch <= 0 {
		c.QueueBatch = 100
	}

	// include allowlists in tracked states
	if !c.CalledExclude {
		c.TrackedStates = slices.Concat(c.TrackedStates, c.Called)
	}
	if !c.ChangedExclude {
		c.TrackedStates = slices.Concat(c.TrackedStates, c.Changed)
	}

	c.TrackedStates = mach.ParseStates(c.TrackedStates)
	if len(c.TrackedStates) == 0 {
		return nil, fmt.Errorf("%w: no states to track", am.ErrStateMissing)
	}

	// init and bind tracer
	mem := &Memory{
		Cfg:              &c,
		Db:               db,
		cacheTrackedIdxs: mach.Index(c.TrackedStates),
		onErr:            onErr,
		queue:            &queue{},
	}
	mem.BaseMemory = amhist.NewBaseMemory(ctx, mach, cfg.BaseConfig, mem)
	tr := &tracer{
		mem: mem,
		id:  amhelp.RandId(4),
	}
	mem.tr = tr
	tr.MachineInit(mach)
	amhelp.DisposeBind(mach, func(id string, ctx context.Context) {
		err := mem.Dispose()
		if err != nil {
			mem.onErr(err)
		}
	})
	_, err := mach.BindTracer(tr)

	return mem, err
}

// FindLatest is [amhist.BaseMemory.FindLatest].
func (m *Memory) FindLatest(
	ctx context.Context, retTx bool, limit int, query amhist.Query,
) ([]*amhist.MemoryRecord, error) {

	if err := m.ValidateQuery(query); err != nil {
		return nil, err
	}
	s := query.Start
	e := query.End
	mach := m.Mach
	cfg := m.Cfg

	return m.Match(ctx, func(
		now *am.TimeIndex, txn *badger.Txn) []*amhist.MemoryRecord {

		machId := mach.Id()
		mTimeIdxs := mach.Index(s.MTimeStates)
		var older *amhist.MemoryRecord
		r := &amhist.MemoryRecord{
			Time: &amhist.TimeRecord{},
		}
		var ret []*amhist.MemoryRecord

		for id := m.nextId.Load() - 1; id > 0; id-- {
			if ctx.Err() != nil || m.Ctx.Err() != nil {
				return nil
			}

			v, err := getVal(txn, timeKey(machId, id))
			if err != nil {
				m.log("empty hit for %d", id)
				break
			}

			// read TimeRecord
			// 1st pass, move 1 more down
			if older == nil {
				id--
				r.Time, err = DecTimeRecord(machId, id, v, cfg.EncJson)
				v2, err2 := getVal(txn, timeKey(machId, id))
				if err2 == nil {
					older = &amhist.MemoryRecord{
						Time: &amhist.TimeRecord{},
					}
					older.Time, err = DecTimeRecord(machId, id, v2, cfg.EncJson)
					if err != nil {
						m.onErr(err)
						return nil
					}
				}

				// 2nd and later passes
			} else if v != nil {
				r = older
				older = &amhist.MemoryRecord{
					Time: &amhist.TimeRecord{},
				}
				older.Time, err = DecTimeRecord(machId, id, v, cfg.EncJson)
				// TODO tx

				// last pass
			} else {
				r = older
				older = nil
			}
			// err
			if err != nil {
				m.onErr(err)
				return nil
			}

			// states conditions
			t := r.Time

			// Active
			for _, state := range query.Active {
				if !am.IsActiveTick(t.MTimeTracked[m.Index1(state)]) {
					continue
				}
			}
			// Activated
			for _, state := range query.Activated {
				idx := m.Index1(state)
				if !am.IsActiveTick(t.MTimeTracked[idx]) {
					continue
				}
				// if has previously been active
				if older != nil && am.IsActiveTick(older.Time.MTimeTracked[idx]) {
					continue
				}
			}
			// Inactive
			for _, state := range query.Inactive {
				if am.IsActiveTick(t.MTimeTracked[mach.Index1(state)]) {
					continue
				}
			}
			// Deactivated
			for _, state := range query.Deactivated {
				idx := m.Index1(state)
				if am.IsActiveTick(t.MTimeTracked[idx]) {
					continue
				}
				// if has previously been inactive
				if older != nil && !am.IsActiveTick(older.Time.MTimeTracked[idx]) {
					continue
				}
			}
			// MTimeStates
			if len(s.MTimeStates) > 0 {
				// caution: slice a sliced time slice
				mTimeTrackedCond := t.MTimeTracked.Filter(mTimeIdxs)
				if mTimeTrackedCond.Before(false, s.MTime) ||
					mTimeTrackedCond.After(false, e.MTime) {

					continue
				}
			}

			// time conditions

			// HTime
			if !s.HTime.IsZero() && !e.HTime.IsZero() &&
				(t.HTime.Before(s.HTime) || t.HTime.After(e.HTime)) {

				continue
			}
			// MTimeSum
			if s.MTimeSum != 0 && e.MTimeSum != 0 &&
				(t.MTimeSum < s.MTimeSum || t.MTimeSum > e.MTimeSum) {

				continue
			}
			// MTimeTrackedSum
			if s.MTimeTrackedSum != 0 && e.MTimeTrackedSum != 0 &&
				(t.MTimeTrackedSum < s.MTimeTrackedSum ||
					t.MTimeTrackedSum > e.MTimeTrackedSum) {

				continue
			}
			// MTimeDiff
			if s.MTimeDiff != 0 && e.MTimeDiff != 0 &&
				(t.MTimeDiffSum < s.MTimeDiff || t.MTimeDiffSum > e.MTimeDiff) {

				continue
			}
			// MTimeTrackedDiff
			if s.MTimeTrackedDiff != 0 && e.MTimeTrackedDiff != 0 &&
				(t.MTimeTrackedDiffSum < s.MTimeTrackedDiff ||
					t.MTimeTrackedDiffSum > e.MTimeTrackedDiff) {

				continue
			}
			// MTimeRecordDiff
			if s.MTimeRecordDiff != 0 && e.MTimeRecordDiff != 0 &&
				(t.MTimeRecordDiffSum < s.MTimeRecordDiff ||
					t.MTimeRecordDiffSum > e.MTimeRecordDiff) {

				continue
			}
			// MachTick
			if s.MachTick != 0 && e.MachTick != 0 &&
				(t.MachTick < s.MachTick || t.MachTick > e.MachTick) {

				continue
			}

			// read TransitionRecord
			if retTx && cfg.StoreTransitions {
				txVal, _ := getVal(txn, txKey(machId, id))
				if txVal != nil {
					r.Transition, err = DecTransitionRecord(machId, id,
						txVal, cfg.EncJson)
					if err != nil {
						m.onErr(err)
					}
				}
			}

			// collect and return
			ret = append(ret, r)
			if limit > 0 && len(ret) >= limit {
				break
			}
		}

		return ret
	})
}

// Sync is [amhist.BaseMemory.Sync].
func (m *Memory) Sync() error {
	m.log("sync...")

	// locks
	m.mx.Lock()
	defer m.mx.Unlock()
	m.syncMx.Lock()
	defer m.syncMx.Unlock()
	m.writeDb(false)

	m.log("sync OK")

	return nil
}

// Match returns records that match the MatcherFn.
func (m *Memory) Match(
	ctx context.Context, matcherFn MatcherFn,
) ([]*amhist.MemoryRecord, error) {

	// stop GC and query
	m.gcMx.RLock()
	var ret []*amhist.MemoryRecord
	err := m.Db.View(func(txn *badger.Txn) error {
		now := m.Mach.Time(nil).ToIndex(m.Mach.StateNames())
		ret = matcherFn(now, txn)

		return nil
	})
	m.gcMx.RUnlock()

	// err
	if ctx.Err() != nil || m.Ctx.Err() != nil {
		return nil, errors.Join(ctx.Err(), m.Ctx.Err())
	} else if err != nil {
		return nil, err
	} else if ret == nil {
		return nil, nil
	}

	return ret, nil
}

// MachineRecord is [amhist.BaseMemory.MachineRecord].
func (m *Memory) MachineRecord() *amhist.MachineRecord {
	m.mx.Lock()
	defer m.mx.Unlock()

	// link to a copy
	cp := *m.machRec
	return &cp
}

// Dispose is [amhist.BaseMemory.Dispose]. TODO merge with ctx
func (m *Memory) Dispose() error {
	if !m.disposed.CompareAndSwap(false, true) {
		return nil
	}
	m.mx.Lock()
	defer m.mx.Unlock()
	m.gcMx.Lock()
	defer m.gcMx.Unlock()

	return errors.Join(
		m.Db.Close(),
		m.Mach.DetachTracer(m.tr.TracerId()),
	)
}

// Config is [amhist.BaseMemory.Config].
func (m *Memory) Config() amhist.BaseConfig {
	return m.Cfg.BaseConfig
}

// Machine is [amhist.BaseMemory.Machine].
func (m *Memory) Machine() am.Api {
	return m.Mach
}

func (m *Memory) encode(v any) ([]byte, error) {
	if m.Cfg.EncJson {
		return json.MarshalIndent(v, "", "  ")
	}

	return msgpack.Marshal(v)
}

// writeDb requires [Memory.mx].
func (m *Memory) writeDb(rLocked bool) {
	if m.SavePending.Load() <= 0 {
		return
	}

	q := m.queue

	// copy
	machRec := *m.machRec
	times := q.times
	q.times = nil
	txs := q.txs
	q.txs = nil
	ids := q.ids
	q.ids = nil
	m.log("writeDb for %d record", len(times))
	l := len(times)
	m.SavePending.Add(-int32(l))

	// fork
	go func() {
		if rLocked {
			defer m.syncMx.RUnlock()
		}

		wb := m.Db.NewWriteBatch()

		// update machine record
		encMach, err := m.encode(machRec)
		if err != nil {
			wb.Cancel()
			m.onErr(err)
			return
		}
		if err := wb.Set(machineKey(machRec.MachId), encMach); err != nil {
			wb.Cancel()
			m.onErr(err)
			return
		}

		for i, recTime := range times {
			// insert time
			encTime, err := m.encode(recTime)
			if err != nil {
				wb.Cancel()
				m.onErr(err)
				return
			}
			if err := wb.Set(timeKey(machRec.MachId, ids[i]), encTime); err != nil {
				wb.Cancel()
				m.onErr(err)
				return
			}

			// insert tx
			if !m.Cfg.StoreTransitions {
				continue
			}

			recTx := txs[i]
			encTx, err := m.encode(recTx)
			if err != nil {
				wb.Cancel()
				m.onErr(err)
				return
			}
			if err := wb.Set(txKey(machRec.MachId, ids[i]), encTx); err != nil {
				wb.Cancel()
				m.onErr(err)
				return
			}
		}

		if err := wb.Flush(); err != nil {
			m.onErr(err)
			return
		}

		// stats
		all := m.Saved.Add(uint64(l))
		m.log("saved %d records (total %d)", l, all)
	}()
}

func (m *Memory) checkGc() {
	// maybe GC TODO cap to max diff
	sinceLastGc := m.SavedGc.Load()
	now := m.Saved.Load()
	if float32(now-sinceLastGc) <= float32(m.Cfg.MaxRecords)*1.5 ||
		!m.gcMx.TryLock() {

		return
	}

	m.log("gc...")

	// dont block tracer
	go func() {
		defer m.gcMx.Unlock()

		machId := m.Mach.Id()
		upper := m.nextId.Load() - uint64(m.Cfg.MaxRecords)
		deleted := 0

		// delete in batches to stay within transaction size limits
		for batchStart := uint64(1); batchStart <= upper; batchStart += gcSize {
			batchEnd := min(batchStart+gcSize, upper+1)

			err := m.Db.Update(func(txn *badger.Txn) error {
				for id := batchStart; id < batchEnd; id++ {
					deleted++
					if err := txn.Delete(timeKey(machId, id)); err != nil &&
						!errors.Is(err, badger.ErrKeyNotFound) {
						return err
					}
					if !m.Cfg.StoreTransitions {
						continue
					}
					if err := txn.Delete(txKey(machId, id)); err != nil &&
						!errors.Is(err, badger.ErrKeyNotFound) {
						// show err, but dont stop deleting
						m.onErr(err)
					}
				}
				return nil
			})
			if err != nil {
				m.onErr(err)
			}
		}
		m.log("gc done for %d records", deleted)

		m.SavedGc.Store(m.Saved.Load())
	}()
}

func (m *Memory) log(msg string, args ...any) {
	if !m.Cfg.Log {
		return
	}

	log.Printf(msg, args...)
}

// ///// ///// /////

// ///// DB

// ///// ///// /////

// getVal is a helper to read a value from badger by key, returning nil if not
// found.
func getVal(txn *badger.Txn, key []byte) ([]byte, error) {
	item, err := txn.Get(key)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	return item.ValueCopy(nil)
}

// GetMachine returns a machine record for a given machine id.
func GetMachine(db *badger.DB, id string) (*amhist.MachineRecord, error) {
	var ret *amhist.MachineRecord
	err := db.View(func(txn *badger.Txn) error {
		v, err := getVal(txn, machineKey(id))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		ret = &amhist.MachineRecord{}
		return Decode(v, ret, true)
	})

	return ret, err
}

// ListMachines returns a list of all machines in a database.
func ListMachines(db *badger.DB) ([]*amhist.MachineRecord, error) {
	prefix := []byte(prefixMachines)
	var ret []*amhist.MachineRecord

	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		iter := txn.NewIterator(opts)
		defer iter.Close()

		for iter.Rewind(); iter.ValidForPrefix(prefix); iter.Next() {
			item := iter.Item()
			err := item.Value(func(v []byte) error {
				rec := &amhist.MachineRecord{}
				if err := Decode(v, rec, true); err != nil {
					return err
				}
				ret = append(ret, rec)
				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})

	return ret, err
}

func DecTimeRecord(
	machId string, id uint64, v []byte, tryJson bool,
) (*amhist.TimeRecord, error) {

	rec := &amhist.TimeRecord{}
	err := Decode(v, rec, tryJson)
	if err != nil {
		return nil, err
	}

	return rec, nil
}

func DecTransitionRecord(
	machId string, id uint64, v []byte, tryJson bool,
) (*amhist.TransitionRecord, error) {

	rec := &amhist.TransitionRecord{}
	err := Decode(v, rec, tryJson)
	if err != nil {
		return nil, err
	}

	return rec, nil
}

func Decode(v []byte, out any, tryJson bool) error {
	if tryJson {
		if err := json.Unmarshal(v, out); err == nil {
			return nil
		}
	}

	return msgpack.Unmarshal(v, out)
}
