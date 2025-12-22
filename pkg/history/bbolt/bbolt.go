// Package bbolt provides machine history tracking and traversal using
// the bbolt K/V database.
package bbolt

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

	"github.com/vmihailenco/msgpack/v5"
	"go.etcd.io/bbolt"
	"golang.org/x/sync/errgroup"

	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type MatcherFn func(
	now *am.TimeIndex, machBucket *bbolt.Bucket,
) []*amhist.MemoryRecord

type Config struct {
	amhist.BaseConfig
	EncJson bool
	// amount of records to save in bulk (default: 100)
	QueueBatch int32
}

const (
	BuckMachines    = "_machines"
	BuckTransitions = "transitions"
	BuckTimes       = "times"
)

// ///// ///// /////

// ///// TRACER

// ///// ///// /////

type tracer struct {
	*am.TracerNoOp

	mem *Memory
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

	// save
	err := m.Db.Update(func(dbTx *bbolt.Tx) error {
		// insert machine
		mb := dbTx.Bucket([]byte(BuckMachines))
		enc, err := m.encode(m.machRec)
		if err != nil {
			return err
		}
		machIdBt := []byte(mach.Id())
		err = mb.Put(machIdBt, enc)
		if err != nil {
			return err
		}

		// create buckets
		b, err := dbTx.CreateBucketIfNotExists(machIdBt)
		if err != nil {
			return err
		}
		_, err = b.CreateBucketIfNotExists([]byte(BuckTransitions))
		if err != nil {
			return err
		}
		_, err = b.CreateBucketIfNotExists([]byte(BuckTimes))
		if err != nil {
			return err
		}

		return nil
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

	// sync mach record
	err := m.Db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BuckMachines))
		encRec, err := m.encode(rec)
		if err != nil {
			return err
		}
		return b.Put([]byte(rec.MachId), encRec)
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
	// toSave := m.SavePending.Add(1)
	m.lastRec = recTime
	// TODO ensure save after a delay
	if m.SavePending.Load() >= m.Cfg.QueueBatch {
		m.syncMx.RLock()
		m.writeDb(true)
		m.checkGc()
	}
}

// itob returns an 8-byte big endian representation of v.
func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// ///// ///// /////

// ///// MEMORY

// ///// ///// /////

func NewDb(name string) (*bbolt.DB, error) {
	if name == "" {
		name = "amhist"
	}

	// TODO optimize: use NoSync?
	return bbolt.Open(name+".db", 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
}

type queue struct {
	times []*amhist.TimeRecord
	txs   []*amhist.TransitionRecord
	ids   []uint64
}

type Memory struct {
	*amhist.BaseMemory

	Db *bbolt.DB
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
	// TODO use Ctx
	disposed    atomic.Bool
	queue       *queue
	queueWorker *errgroup.Group
	onErr       func(err error)
	machRec     *amhist.MachineRecord
	// global lock, needed mostly for [Memory.MachineRecord].
	mx               sync.Mutex
	tr               *tracer
	cacheTrackedIdxs []int
	lastRec          *amhist.TimeRecord
}

func NewMemory(
	ctx context.Context, db *bbolt.DB, mach am.Api, cfg Config,
	onErr func(err error),
) (*Memory, error) {

	// init DB
	err := db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(BuckMachines))
		return err
	})
	if err != nil {
		return nil, err
	}

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
		queueWorker:      &errgroup.Group{},
	}
	mem.BaseMemory = amhist.NewBaseMemory(ctx, mach, cfg.BaseConfig, mem)
	mem.queueWorker.SetLimit(1)
	tr := &tracer{
		mem: mem,
	}
	mem.tr = tr
	tr.MachineInit(mach)

	return mem, mach.BindTracer(tr)
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
		now *am.TimeIndex, machBuck *bbolt.Bucket) []*amhist.MemoryRecord {

		mTimeIdxs := mach.Index(s.MTimeStates)
		b := machBuck.Bucket([]byte(BuckTimes))
		var older *amhist.MemoryRecord
		r := &amhist.MemoryRecord{
			Time: &amhist.TimeRecord{},
		}
		var ret []*amhist.MemoryRecord

		for id := m.nextId.Load() - 1; id > 0; id-- {
			if ctx.Err() != nil || m.Ctx.Err() != nil {
				return nil
			}

			v := b.Get(itob(id))
			if v == nil {
				m.log("empty hit for %d", id)
				break
			}

			// read TimeRecord
			var err error
			// 1st pass, move 1 more down
			if older == nil {
				id--
				r.Time, err = DecTimeRecord(mach.Id(), id, v, cfg.EncJson)
				v := b.Get(itob(id))
				if v != nil {
					older = &amhist.MemoryRecord{
						Time: &amhist.TimeRecord{},
					}
					older.Time, err = DecTimeRecord(mach.Id(), id, v, cfg.EncJson)
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
				older.Time, err = DecTimeRecord(mach.Id(), id, v, cfg.EncJson)
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
				r.Transition, err = DecTransitionRecord(mach.Id(), id,
					machBuck.Get(itob(id)), cfg.EncJson)
				if err != nil {
					m.onErr(err)
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

// Match returns the first record that matches the MatcherFn.
func (m *Memory) Match(
	ctx context.Context, matcherFn MatcherFn,
) ([]*amhist.MemoryRecord, error) {

	// stop GC and query
	m.gcMx.RLock()
	var ret []*amhist.MemoryRecord
	err := m.Db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(m.Mach.Id()))
		now := m.Mach.Time(nil).ToIndex(m.Mach.StateNames())
		ret = matcherFn(now, b)

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
		m.Mach.DetachTracer(m.tr),
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

		err := m.Db.Batch(func(dbTx *bbolt.Tx) error {

			// buckets
			bMachs := dbTx.Bucket([]byte(BuckMachines))
			machIdBt := []byte(machRec.MachId)
			b := dbTx.Bucket(machIdBt)
			bTimes := b.Bucket([]byte(BuckTimes))
			var bTxs *bbolt.Bucket
			if m.Cfg.StoreTransitions {
				bTxs = b.Bucket([]byte(BuckTransitions))
			}

			// update machine
			enc, err := m.encode(machRec)
			if err != nil {
				return err
			}
			err = bMachs.Put(machIdBt, enc)
			if err != nil {
				return err
			}

			for i, recTime := range times {
				id := itob(ids[i])

				// insert time
				encTime, err := m.encode(recTime)
				if err != nil {
					return err
				}
				err = bTimes.Put(id, encTime)
				if err != nil {
					return err
				}

				// insert tx
				if !m.Cfg.StoreTransitions {
					continue
				}

				recTx := txs[i]
				encTx, err := m.encode(recTx)
				if err != nil {
					return err
				}
				err = bTxs.Put(id, encTx)
				if err != nil {
					return err
				}
			}

			// stats
			all := m.Saved.Add(uint64(l))
			m.log("saved %d records (total %d)", l, all)

			return nil
		})
		if err != nil {
			m.onErr(err)
		}
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

		// trim the bottom
		err := m.Db.Update(func(dbTx *bbolt.Tx) error {
			b := dbTx.Bucket([]byte(m.Mach.Id()))
			bTimes := b.Bucket([]byte(BuckTimes))
			var bTxs *bbolt.Bucket
			if m.Cfg.StoreTransitions {
				bTxs = b.Bucket([]byte(BuckTransitions))
			}
			i := 0

			// go 1 by 1 and delete stuff
			for id := m.nextId.Load() - uint64(m.Cfg.MaxRecords); id > 0; id-- {
				i++

				// time
				err := bTimes.Delete(itob(id))
				if err != nil {
					return err
				}

				// tx
				if !m.Cfg.StoreTransitions {
					continue
				}
				err = bTxs.Delete(itob(id))
				if err != nil {
					// show err, but dont stop deleting
					m.onErr(err)
				}
			}
			m.log("gc done for %d records", i)

			return nil
		})
		if err != nil {
			m.onErr(err)
		}

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

// GetMachine returns a machine record for a given machine id.
func GetMachine(db *bbolt.DB, id string) (*amhist.MachineRecord, error) {
	var ret *amhist.MachineRecord
	err := db.View(func(dbTx *bbolt.Tx) error {
		b := dbTx.Bucket([]byte(BuckMachines))
		pack := b.Get([]byte(id))
		if pack == nil {
			return nil
		}
		return Decode(pack, ret, true)
	})

	return ret, err
}

// ListMachines returns a list of all machines in a database.
func ListMachines(db *bbolt.DB) ([]*amhist.MachineRecord, error) {
	ret := make([]*amhist.MachineRecord, 0)
	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(BuckMachines))
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			rec := &amhist.MachineRecord{}
			err := Decode(v, rec, true)
			if err != nil {
				return err
			}
			ret = append(ret, rec)
		}

		return nil
	})

	return ret, err
}

func DecTimeRecord(
	machId string, id uint64, v []byte, tryJson bool,
) (*amhist.TimeRecord, error) {

	// TODO optimize: cache [machId-key]
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

	// TODO optimize: cache [machId-key]
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
