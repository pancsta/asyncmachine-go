package gorm

// TODO link godocs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/ncruces/go-sqlite3/embed"
	"github.com/ncruces/go-sqlite3/gormlite"
	"github.com/ncruces/go-sqlite3/vfs"
	"golang.org/x/sync/errgroup"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type MatcherFn func(now *am.TimeIndex, query *gorm.DB) *gorm.DB

type Config struct {
	amhist.BaseConfig

	// amount of records to save in bulk (default: 1000)
	QueueBatch int32
	// amount of goroutines doing bulk saving (default: 10)
	SavePool int
}

// ///// ///// /////

// ///// SCHEMA

// ///// ///// /////
// TODO optimize: gen a dedicated schema for each machine?

// Machine is a SQL version of [amhist.MachineRecord].
type Machine struct {
	// PK

	ID uint32 `gorm:"primaryKey"`

	// rels

	Times  []Time
	States []State

	// data

	MachId     string `gorm:"column:mach_id;index:mach_id"`
	StateNames datatypes.JSON
	Schema     datatypes.JSON

	// human times

	// first time the machine has been tracked
	FirstTracking time.Time
	// last time a tracking of this machine has started
	LastTracking time.Time
	// last time a sync has been performed
	LastSync time.Time

	// data - machine times

	// current machine start tick
	MachTick uint32
	// current (total) machine time
	// TODO optimize: collect in a separate query
	MTime datatypes.JSON
	// sum of the current machine time
	MTimeSum uint64
	// next ID for time records
	NextId uint64

	// cache

	cacheMTime am.Time
}

// Time is a SQL version of [amhist.TimeRecord].
type Time struct {
	// PK

	ID        uint64 `gorm:"primaryKey;autoIncrement:false"`
	MachineID uint32 `gorm:"primaryKey"`

	// rels

	Ticks []Tick `gorm:"foreignKey:TimeID,MachineID;references:ID,MachineID"`

	// data

	// MutType is a mutation type.
	MutType am.MutationType
	// MTimeSum is a machine time sum after this transition.
	MTimeSum uint64
	// MTimeSum is a machine time sum after this transition for tracked states
	// only.
	MTimeTrackedSum uint64
	// MTimeDiffSum is a machine time difference for this transition.
	MTimeDiffSum uint64
	// MTimeDiffSum is a machine time difference for this transition for tracked
	// states only.
	MTimeTrackedDiffSum uint64
	// MTimeRecordDiffSum is a machine time difference since the previous
	// [amhist.TimeRecord].
	MTimeRecordDiffSum uint64
	// HTime is a human time in UTC.
	HTime time.Time
	// MTime is a machine time for tracked states after this mutation.
	MTimeTracked datatypes.JSON
	// MTimeTrackedDiff is a machine time diff compared to the previous mutation
	// (not a record).
	MTimeTrackedDiff datatypes.JSON
	// MachTick is the machine tick at the time of this transition.
	MachTick uint32

	// cache

	cacheMTimeTracked am.Time

	// Transition

	TxId string `gorm:"index:tx_id"`

	// data

	TxSourceTx   *string
	TxSourceMach *string
	TxIsAuto     bool
	TxIsAccepted bool
	TxIsCheck    bool
	TxIsBroken   bool
	TxQueueLen   uint16

	// normal queue props

	TxQueuedAt   *uint64
	TxExecutedAt *uint64

	// extra

	TxCalled    datatypes.JSON
	TxArguments *datatypes.JSON
}

type State struct {
	// PK

	ID        uint   `gorm:"primaryKey"`
	MachineID string `gorm:"index:machine_state"`
	// Index is the state index in the machine (not the tracked states index).
	Index int `gorm:"index:machine_state"`

	// data

	Name string
}

type Tick struct {
	// PK

	TimeID    uint64 `gorm:"primaryKey;autoIncrement:false;index:activated"`
	MachineID uint32 `gorm:"primaryKey;autoIncrement:false;index:activated"`
	StateID   uint   `gorm:"primaryKey;autoIncrement:false;index:activated"`

	// rels

	State State

	// data

	Tick uint64
	// was the state activated in this transition?
	Activated bool `gorm:"index:activated"`
	// was the state deactivated in this transition?
	Deactivated bool
	// state is currently active
	Active bool
	// TODO last change distance
}

// ///// ///// /////

// ///// TRACER

// ///// ///// /////

type tracer struct {
	*am.NoOpTracer

	mem *Memory
}

func (t *tracer) MachineInit(mach am.Api) context.Context {
	m := t.mem
	now := time.Now().UTC()
	var err error

	// locks
	m.mx.Lock()
	defer m.mx.Unlock()

	// select existing mach
	rec, _ := GetMachine(m.Db, mach.Id(), true)
	if rec == nil {
		// initial default
		rec = &Machine{
			MachId:        mach.Id(),
			FirstTracking: now,
			NextId:        1,
		}
	}

	// machine record
	mTime := mach.Time(nil)
	mTimeBt, _ := json.Marshal(mTime)
	rec.MachId = m.Mach.Id()
	rec.LastTracking = now
	rec.FirstTracking = now
	rec.LastSync = now
	rec.MTime = mTimeBt
	rec.MTimeSum = mTime.Sum(nil)
	rec.MachTick = mach.MachineTick()
	rec.cacheMTime = mTime
	m.nextId.Store(rec.NextId)

	// schema
	if m.Cfg.StoreSchema {
		// TODO cache
		rec.Schema, err = json.Marshal(mach.Schema())
		if err != nil {
			m.onErr(err)
			return nil
		}
		rec.StateNames, err = json.Marshal(mach.StateNames())
		if err != nil {
			m.onErr(err)
			return nil
		}
	}

	// states
	existing := make(map[string]bool, len(rec.States))
	for _, state := range rec.States {
		m.cacheDbIdxs[state.Name] = state.ID
		existing[state.Name] = true
	}
	added := false
	for _, state := range m.Cfg.TrackedStates {
		if existing[state] {
			continue
		}

		// add state
		added = true
		rec.States = append(rec.States, State{
			MachineID: m.Mach.Id(),
			Index:     m.Mach.Index1(state),
			Name:      state,
		})
	}

	// upsert machine
	err = m.Db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"last_sync", "last_tracking", "mach_tick", "m_time", "m_time_sum",
			"schema"}),
		// TODO cascade delete data in other tables
	}).Create(rec).Error
	if err != nil {
		m.onErr(err)
		return nil
	}
	m.machRec = rec

	// get DB IDs for state names
	if added {
		rec, _ = GetMachine(m.Db, mach.Id(), true)
		for _, state := range rec.States {
			m.cacheDbIdxs[state.Name] = state.ID
		}
	}

	return nil
}

func (t *tracer) SchemaChange(machine am.Api, old am.Schema) {
	m := t.mem
	var err error

	// locks
	m.mx.Lock()
	defer m.mx.Unlock()

	// update states TODO move to Memory.UpdateTracked
	// db := gorm.G[State](m.Db)
	// for _, state := range m.BaseConfig.TrackedStates {
	// 	idx := m.Mach.Index1(state)
	// 	// skip existing
	// 	if slices.Contains(slices.Collect(maps.Keys(old)), state) {
	// 		continue
	// 	}
	//
	// 	// insert
	// 	err = db.Create(m.Mach.Ctx(), &State{
	// 		MachineID: machine.Id(),
	// 		Index:     idx,
	// 		Name:      state,
	// 	})
	// 	if err != nil {
	// 		m.onErr(err)
	// 		return
	// 	}
	// }

	// update schema
	if !m.Cfg.StoreSchema {
		return
	}
	rec := m.machRec
	rec.Schema, err = json.Marshal(m.Mach.Schema())
	if err != nil {
		m.onErr(err)
		return
	}
	rec.StateNames, err = json.Marshal(m.Mach.StateNames())
	if err != nil {
		m.onErr(err)
		return
	}

	// sync mach record
	if err := m.Db.Save(m.machRec).Error; err != nil {
		m.onErr(fmt.Errorf("failed to save: %w", err))
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

	// locks
	m.mx.Lock()
	defer m.mx.Unlock()

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

	// json
	calledBt, _ := json.Marshal(tx.Mutation.Called)
	args := tx.Mutation.MapArgs(mach.SemLogger().ArgsMapper())
	argsBt, _ := json.Marshal(args)
	mTimeBt, _ := json.Marshal(mTime)
	mTimeTrackedBt, _ := json.Marshal(mTimeTracked)
	mTimeTrackedDiffBt, _ := json.Marshal(
		mTimeTracked.DiffSince(mTimeTrackedBefore))

	// time record
	var recordDiff uint64
	if t.mem.lastRec != nil {
		recordDiff = sum - t.mem.lastRec.MTimeSum
	}
	machTick := mach.MachineTick()
	now := time.Now().UTC()
	id := m.nextId.Load()
	timeRec := Time{
		ID:                  id,
		MachineID:           m.machRec.ID,
		MutType:             mut.Type,
		MTimeSum:            sum,
		MTimeTrackedSum:     sumTracked,
		MTimeDiffSum:        sum - tx.TimeBefore.Sum(nil),
		MTimeTrackedDiffSum: sumTracked - tx.TimeBefore.Sum(m.cacheTrackedIdxs),
		MTimeRecordDiffSum:  recordDiff,
		HTime:               now,
		MTimeTracked:        mTimeTrackedBt,
		MTimeTrackedDiff:    mTimeTrackedDiffBt,
		MachTick:            machTick,
		cacheMTimeTracked:   mTimeTracked,
	}

	// optional tx record
	if cfg.StoreTransitions {
		timeRec.TxId = tx.Id
		timeRec.TxCalled = calledBt
		timeRec.TxIsAuto = mut.Auto
		timeRec.TxIsAccepted = tx.IsAccepted.Load()
		timeRec.TxIsCheck = mut.IsCheck
		timeRec.TxIsBroken = tx.IsBroken.Load()
		timeRec.TxQueueLen = tx.QueueLen

		// optional fields
		if len(args) > 0 {
			j := datatypes.JSON(argsBt)
			timeRec.TxArguments = &j
		}
		if mut.Source != nil {
			timeRec.TxSourceMach = &mut.Source.MachId
			timeRec.TxSourceTx = &mut.Source.TxId
		}
		if mut.QueueTick > 0 {
			t := mut.QueueTick
			timeRec.TxExecutedAt = &t
		}
	}

	// insert state ticks
	ticks := make([]Tick, len(mTimeTracked))
	i := 0
	for hIdx, state := range m.Cfg.TrackedStates {
		isActive := am.IsActiveTick(mTimeTracked[hIdx])
		tickRec := Tick{
			StateID:   m.cacheDbIdxs[state],
			Tick:      mTimeTracked[hIdx],
			TimeID:    id,
			MachineID: m.machRec.ID,
			Active:    isActive,
		}

		// activated & deactivated
		if isActive &&
			(m.lastRec == nil ||
				m.lastRec.cacheMTimeTracked[hIdx] != mTimeTracked[hIdx]) {

			tickRec.Activated = true
		}
		if !isActive &&
			m.lastRec != nil &&
			(m.lastRec.cacheMTimeTracked[hIdx] != mTimeTracked[hIdx]) {

			tickRec.Deactivated = true
		}

		ticks[i] = tickRec
		i++
	}
	m.queue.ticks = append(m.queue.ticks, ticks...)

	// update machine record
	m.machRec.MTime = mTimeBt
	m.machRec.MTimeSum = sum
	m.machRec.LastSync = now
	m.machRec.MachTick = machTick
	m.machRec.NextId = id + 1
	m.machRec.cacheMTime = mTime
	m.nextId.Store(id + 1)

	// queue, cache, GC
	m.queue.times = append(m.queue.times, timeRec)
	m.SavePending.Add(1)
	m.lastRec = &timeRec
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

type queue struct {
	times []Time
	ticks []Tick
}

type Memory struct {
	*amhist.BaseMemory

	Db             *gorm.DB
	Cfg            *Config
	SavePending    atomic.Int32
	SaveInProgress atomic.Bool
	Saved          atomic.Uint64
	// Value of Saved at the end of the last GC
	SavedGc atomic.Uint64

	savePool *errgroup.Group
	// sync lock (read: flush, write: sync)
	syncMx sync.RWMutex
	// garbage collector lock (read: query, write: GC)
	gcMx sync.RWMutex
	// TODO use Ctx
	disposed atomic.Bool
	machRec  *Machine
	queue    *queue
	onErr    func(err error)
	// global lock, needed mostly for [Memory.MachineRecord].
	mx               sync.Mutex
	cacheTrackedIdxs []int
	cacheDbIdxs      map[string]uint
	tr               *tracer
	lastRec          *Time
	nextId           atomic.Uint64
}

func NewMemory(
	ctx context.Context, db *gorm.DB, mach am.Api, cfg Config,
	onErr func(err error),
) (*Memory, error) {

	// update the DB schema
	err := db.AutoMigrate(
		&Machine{}, &Time{}, &State{}, &Tick{},
	)
	if err != nil {
		return nil, err
	}

	// TODO view per machine with state names as columns
	// viewSQL := `
	// CREATE VIEW IF NOT EXISTS active_user_view(
	//    AlbumTitle,
	//    Minutes
	// ) AS
	// SELECT
	// 	id AS user_id,
	// 	username AS user_name,
	// 	email AS user_email
	// FROM
	// 	users
	// WHERE
	// 	is_active = 1;`
	//
	// if err := db.Exec(viewSQL).Error; err != nil {
	// 	log.Fatal("Failed to create view:", err)
	// }

	c := cfg
	if c.MaxRecords <= 0 {
		c.MaxRecords = 1000
	}
	if c.QueueBatch <= 0 {
		c.QueueBatch = 1000
	}
	if c.SavePool <= 0 {
		c.SavePool = 10
	}

	// include allowlists in tracked states
	if !c.CalledExclude {
		c.TrackedStates = slices.Concat(c.TrackedStates, c.Called)
	}
	if !c.ChangedExclude {
		c.TrackedStates = slices.Concat(c.TrackedStates, c.Changed)
	}

	if c.TrackedStates == nil {
		c.TrackedStates = mach.StateNames()
	}

	c.TrackedStates = mach.ParseStates(c.TrackedStates)
	if len(c.TrackedStates) == 0 {
		return nil, fmt.Errorf("%w: no states to track", am.ErrStateMissing)
	}

	// init and bind
	mem := &Memory{
		Cfg:              &c,
		Db:               db,
		savePool:         &errgroup.Group{},
		onErr:            onErr,
		queue:            &queue{},
		cacheTrackedIdxs: mach.Index(c.TrackedStates),
		cacheDbIdxs:      make(map[string]uint),
	}
	mem.BaseMemory = amhist.NewBaseMemory(ctx, mach, cfg.BaseConfig, mem)
	mem.savePool.SetLimit(c.SavePool)
	tr := &tracer{
		mem: mem,
	}
	mem.tr = tr
	tr.MachineInit(mach)
	mach.OnDispose(func(id string, ctx context.Context) {
		err := mem.Dispose()
		if err != nil {
			mem.onErr(err)
		}
	})

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

	return m.Match(ctx, limit, func(now *am.TimeIndex, db *gorm.DB) *gorm.DB {

		// states conditions

		joins := []string{}
		// Active
		for _, state := range query.Active {
			db = db.Where(state+".active = ?", true)
			joins = append(joins, state)
		}
		// Activated
		for _, state := range query.Activated {
			db = db.Where(state+".activated = ?", true)
			joins = append(joins, state)
		}
		// Inactive
		for _, state := range query.Inactive {
			db = db.Where(state+".active = ?", false)
			joins = append(joins, state)
		}
		// Deactivated
		for _, state := range query.Deactivated {
			db = db.Where(state+".deactivated = ?", true)
			joins = append(joins, state)
		}
		// MTimeStates
		for i, state := range s.MTimeStates {
			db = db.Where(state+".m_time >= ?", s.MTime[i])
			joins = append(joins, state)
		}
		for i, state := range e.MTimeStates {
			db = db.Where(state+".m_time <= ?", e.MTime[i])
			joins = append(joins, state)
		}

		// joins

		// states
		for _, state := range utils.SlicesUniq(joins) {
			db = m.JoinState(db, state)
		}
		// tx
		if retTx && m.Cfg.StoreTransitions {
			db = m.JoinTransition(db)
		}

		// time conditions

		// HTime
		if !s.HTime.IsZero() && !e.HTime.IsZero() {
			db = db.Where(
				"times.h_time >= ? AND times.h_time <= ?",
				s.HTime, e.HTime,
			)
		}
		// MTimeSum
		if s.MTimeSum != 0 && e.MTimeSum != 0 {
			db = db.Where(
				"times.m_time_sum >= ? AND times.m_time_sum <= ?",
				s.MTimeSum, e.MTimeSum,
			)
		}
		// MTimeTrackedSum
		if s.MTimeTrackedSum != 0 && e.MTimeTrackedSum != 0 {
			db = db.Where(
				"times.m_time_tracked_sum >= ? AND times.m_time_tracked_sum <= ?",
				s.MTimeTrackedSum, e.MTimeTrackedSum,
			)
		}
		// MTimeDiff
		if s.MTimeDiff != 0 && e.MTimeDiff != 0 {
			db = db.Where(
				"times.m_time_diff >= ? AND times.m_time_diff <= ?",
				s.MTimeDiff, e.MTimeDiff,
			)
		}
		// MTimeTrackedDiff
		if s.MTimeTrackedDiff != 0 && e.MTimeTrackedDiff != 0 {
			db = db.Where(
				"times.m_time_tracked_diff >= ? AND times.m_time_tracked_diff <= ?",
				s.MTimeTrackedDiff, e.MTimeTrackedDiff,
			)
		}
		// MTimeRecordDiff
		if s.MTimeRecordDiff != 0 && e.MTimeRecordDiff != 0 {
			db = db.Where(
				"times.m_time_record_diff >= ? AND times.m_time_record_diff <= ?",
				s.MTimeRecordDiff, e.MTimeRecordDiff,
			)
		}
		// MachTick
		if s.MachTick != 0 && e.MachTick != 0 {
			db = db.Where(
				"times.mach_tick >= ? AND times.mach_tick <= ?",
				s.MachTick, e.MachTick,
			)
		}

		// order
		if len(joins) > 0 {
			db = db.Order(joins[len(joins)-1] + ".time_id DESC")
		} else {
			db = db.Order("times.id DESC")
		}

		return db
	})
}

// MachineRecord is [amhist.BaseMemory.MachineRecord].
func (m *Memory) MachineRecord() *amhist.MachineRecord {
	m.mx.Lock()
	defer m.mx.Unlock()

	r := m.machRec
	ret := &amhist.MachineRecord{
		MachId:        r.MachId,
		FirstTracking: r.FirstTracking,
		LastTracking:  r.LastTracking,
		LastSync:      r.LastSync,
		MachTick:      r.MachTick,
		MTime:         r.cacheMTime,
		MTimeSum:      r.MTimeSum,
		NextId:        r.NextId,
	}

	if m.Config().StoreSchema {
		err := json.Unmarshal(r.Schema, &ret.Schema)
		if err != nil {
			m.onErr(err)
			return nil
		}
		err = json.Unmarshal(r.StateNames, &ret.StateNames)
		if err != nil {
			m.onErr(err)
			return nil
		}
	}

	return ret
}

// Dispose is [amhist.BaseMemory.Dispose]. TODO merge with ctx
func (m *Memory) Dispose() error {
	if !m.disposed.CompareAndSwap(false, true) {
		return nil
	}
	m.mx.Lock()
	defer m.mx.Unlock()

	trErr := m.Mach.DetachTracer(m.tr)
	if stdDb, err := m.Db.DB(); err != nil {
		return errors.Join(err, trErr)
	} else {
		return errors.Join(stdDb.Close(), err)
	}
}

func (m *Memory) JoinState(query *gorm.DB, name string) *gorm.DB {
	// TODO lock, ok check
	sId := m.cacheDbIdxs[name]

	return query.Joins(utils.Sp(`
		JOIN ticks `+name+`
			ON `+name+`.time_id = times.id 
			AND `+name+`.machine_id = times.machine_id
	`)).Where(name+".state_id = ?", sId)
}

func (m *Memory) JoinTransition(query *gorm.DB) *gorm.DB {
	// TODO test
	return query.Preload("transitions")
}

// func (m *Memory) SelectTime(query *gorm.DB, name string) *gorm.DB {
// 	return m.Db.Debug().Model(&Time{})
// }

// Match returns the latest record that matches the given matcher function.
func (m *Memory) Match(
	ctx context.Context, limit int, matcherFn MatcherFn,
) ([]*amhist.MemoryRecord, error) {
	// TODO validate state is tracked and err

	var rows = []Time{}
	q := m.Db.Model(&Time{})
	mTime := m.Mach.Time(nil).ToIndex(m.Mach.StateNames())
	q = matcherFn(mTime, q)
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.WithContext(ctx).
		// TODO optimize: dont select TX when not storing
		Find(&rows).Error
	// errs
	if ctx.Err() != nil || m.Ctx.Err() != nil {
		return nil, errors.Join(ctx.Err(), m.Ctx.Err())
	} else if err != nil {
		return nil, err
	} else if len(rows) == 0 {
		return nil, nil
	}

	// build results
	var ret = make([]*amhist.MemoryRecord, len(rows))
	for i, r := range rows {
		tTracked := am.Time{}
		if err := json.Unmarshal(r.MTimeTracked, &tTracked); err != nil {
			return nil, err
		}
		tTrackedDiff := am.Time{}
		if err := json.Unmarshal(r.MTimeTracked, &tTrackedDiff); err != nil {
			return nil, err
		}

		// time record
		ret[i] = &amhist.MemoryRecord{
			Time: &amhist.TimeRecord{
				MutType:             r.MutType,
				MTimeSum:            r.MTimeSum,
				MTimeTrackedSum:     r.MTimeTrackedSum,
				MTimeDiffSum:        r.MTimeDiffSum,
				MTimeTrackedDiffSum: r.MTimeTrackedDiffSum,
				MTimeRecordDiffSum:  r.MTimeRecordDiffSum,
				MTimeTracked:        tTracked,
				MTimeTrackedDiff:    tTrackedDiff,
				HTime:               r.HTime,
				MachTick:            r.MachTick,
			},
		}

		// TODO extract

		// optional tx record
		if m.Cfg.StoreTransitions {
			ret[i].Transition = &amhist.TransitionRecord{
				TransitionId: r.TxId,
				IsAuto:       r.TxIsAuto,
				IsAccepted:   r.TxIsAccepted,
				IsCheck:      r.TxIsCheck,
				IsBroken:     r.TxIsBroken,
				QueueLen:     r.TxQueueLen,
				Called:       nil,
				Arguments:    nil,
			}

			// optional fields
			retTx := ret[i].Transition
			if r.TxSourceTx != nil && r.TxSourceMach != nil {
				retTx.SourceTx = *r.TxSourceTx
				retTx.SourceMach = *r.TxSourceMach
			}
			if r.TxQueuedAt != nil && r.TxExecutedAt != nil {
				retTx.QueuedAt = *r.TxQueuedAt
				retTx.ExecutedAt = *r.TxExecutedAt
			}

			// json fields
			if r.TxCalled != nil {
				if err := json.Unmarshal(r.TxCalled, &retTx.Called); err != nil {
					return nil, err
				}
			}
			if r.TxArguments != nil {
				if err := json.Unmarshal(*r.TxArguments, &retTx.Arguments); err != nil {
					return nil, err
				}
			}
		}
	}

	return ret, nil
}

// func (m *Memory) dbTime() gorm.Interface[Time] {
// 	return gorm.G[Time](m.Db)
// }

func (m *Memory) checkGc() {
	// TODO flush WAL checkpoint?
	// maybe GC TODO cap to max diff
	sinceLastGc := m.SavedGc.Load()
	now := m.Saved.Load()
	if float32(now-sinceLastGc) <= float32(m.Cfg.MaxRecords)*1.5 ||
		!m.gcMx.TryLock() {

		return
	}

	timeDb := gorm.G[Time](m.Db)
	_, err := timeDb.
		Where("machine_id = ?", m.machRec.ID).
		Where("id NOT IN (?)", timeDb.
			Select("id").
			Where("machine_id = ?", m.machRec.ID).
			Order("id DESC").
			Limit(m.Cfg.MaxRecords)).
		Delete(m.Ctx)
	if err != nil {
		m.onErr(fmt.Errorf("failed to GC: %w", err))
	}

	m.SavedGc.Store(m.Saved.Load())
}

// Config is [amhist.BaseMemory.Config].
func (m *Memory) Config() amhist.BaseConfig {
	return m.Cfg.BaseConfig
}

// Machine is [amhist.BaseMemory.Machine].
func (m *Memory) Machine() am.Api {
	return m.Mach
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

// writeDb requires [Memory.mx].
func (m *Memory) writeDb(rLocked bool) {
	if m.SavePending.Load() <= 0 {
		return
	}

	q := m.queue

	// copy
	machRec := *m.machRec
	// TODO schema race?
	times := q.times
	q.times = nil
	ticks := q.ticks
	q.ticks = nil
	m.log("writeDb for %d record", len(times))
	l := len(times)
	m.SavePending.Add(-int32(l))

	// fork
	go m.savePool.Go(func() error {
		if m.disposed.Load() {
			return nil
		}
		if rLocked {
			defer m.syncMx.RUnlock()
		}

		// sync mach record TODO skip saving states
		if err := m.Db.Save(machRec).Error; err != nil {
			m.onErr(fmt.Errorf("failed to save: %w", err))
			return err
		}
		// TODO optimize: parallel save?
		// times
		dbTimes := gorm.G[Time](m.Db)
		err := dbTimes.CreateInBatches(m.Mach.Ctx(), &times, 100)
		if err != nil {
			m.onErr(err)
			return err
		}

		// ticks
		dbTicks := gorm.G[Tick](m.Db)
		err = dbTicks.CreateInBatches(m.Mach.Ctx(), &ticks, 100)
		if err != nil {
			m.onErr(err)
			return err
		}

		return nil
	})
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

// NewSqlite returns a new SQLite DB for GORM.
func NewSqlite(name string, debug bool) (*gorm.DB, *sql.DB, error) {
	// TODO logger
	if name == "" {
		name = "amhist"
	}

	// TODO log slow queries to onErr
	cfg := logger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  logger.Silent,
		Colorful:                  true,
		IgnoreRecordNotFoundError: true,
	}
	if debug {
		cfg.LogLevel = logger.Info
		cfg.IgnoreRecordNotFoundError = false
	}

	// expose internal SQLite to share with other drivers
	dbG, err := gorm.Open(gormlite.Open(name+".sqlite"), &gorm.Config{
		Logger: logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), cfg),
	})
	if err != nil {
		return nil, nil, err
	} else if dbSql, err := dbG.DB(); err != nil {
		return nil, nil, err
	} else {
		if !vfs.SupportsSharedMemory {
			if err = dbG.Exec(`PRAGMA locking_mode=exclusive`).Error; err != nil {
				return nil, nil, err
			}
		}

		// enable WAL
		if err = dbG.Exec(`PRAGMA journal_mode=wal;`).Error; err != nil {
			return nil, nil, err
		}

		return dbG, dbSql, err
	}
}

// GetMachine returns a machine record for a given machine id.
func GetMachine(db *gorm.DB, id string, inclStates bool) (*Machine, error) {
	var m Machine
	q := db.Where("mach_id = ?", id)
	if inclStates {
		q = q.Preload("States")
	}
	// dont error on not found with Find
	if err := q.First(&m).Error; err != nil {

		return nil, err
	}

	return &m, nil
}

// ListMachines returns a list of all machines in a database. TODO
func ListMachines(db *gorm.DB) ([]*amhist.MachineRecord, error) {
	// TODO
	panic("not implemented")
}
