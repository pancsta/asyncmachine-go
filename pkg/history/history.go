// Package history provides machine history tracking and traversal using
// the process' memory and structs.
package history

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ///// ///// /////

// ///// TYPES

// ///// ///// /////

var (
	ErrIncompatibleType = fmt.Errorf("incompatible type")
	ErrCondition        = fmt.Errorf("incorrect condition")
)

// TODO bind ErrConfig
var ErrConfig = fmt.Errorf("incorrect config")

type MatcherFn func(now *am.TimeIndex, db []*MemoryRecord) []*MemoryRecord

// Query represents various conditions for a single mutation. All are
// optional, but at least one must be set.
type Query struct {
	// states

	// Active is a set of states that were active AFTER the mutation
	Active am.S
	// Activated is a set of states that were activated during the transition.
	Activated am.S
	// Inactive is a set of states that were inactive AFTER the mutation
	Inactive am.S
	// Deactivated is a set of states that were deactivated during the
	// transition.
	Deactivated am.S

	// times

	// Start is the beginning of a scalar time condition and requires an
	// equivalent in [Query.End].
	Start ConditionTime
	// End is the end of a scalar time condition and requires an equivalent in
	// [Query.Start].
	End ConditionTime

	// TODO support transitions
	// StartTx ConditionTx
	// EndTx ConditionTx
}

type ConditionTime struct {
	// MTimeStates is a set of states for MTime.
	MTimeStates am.S
	// MTime is the machine time for a mutation to match.
	MTime am.Time
	// HTime is the human time for a mutation to match.
	HTime time.Time
	// MTimeSum is a machine time sum after this transition.
	MTimeSum uint64
	// MTimeSum is a machine time sum after this transition for tracked states
	// only.
	MTimeTrackedSum uint64
	// MTimeDiff is a machine time difference for this transition, compared to the
	// previous transition.
	MTimeDiff uint64
	// MTimeDiff is a machine time difference for this transition for tracked
	// states only.
	MTimeTrackedDiff uint64
	// MTimeRecordDiff is a machine time difference since the previous
	// [TimeRecord].
	MTimeRecordDiff uint64
	// MachTick is the machine tick at the time of this transition.
	MachTick uint32
}

// TODO ConditionTx

// ConditionTx represents a condition for a single transition. Requires
// [BaseConfig.StoreTransitions] to be true.
type ConditionTx struct {
	Query

	// Called is a set of states that were called in the mutation.
	Called am.S

	// tx info

	SourceTx   string
	SourceMach string
	IsAuto     bool
	IsAccepted bool
	IsCheck    bool
	IsBroken   bool
	QueueLen   uint16

	// normal queue props

	QueuedAt   uint64
	ExecutedAt uint64
}

// BaseConfig describes the tracking configuration - conditions, list of tracked
// states and data expiration TTLs.
type BaseConfig struct {
	Log bool

	// tracking conditions

	// Called is a list of mutation states (called ones) required to track a
	// transition. See also CalledExclude. Optional.
	Called am.S
	// CalledExclude flips Called to be a blocklist.
	CalledExclude bool
	// Changed is a list of transition states which had clock changes, required to
	// track a transition. See also ChangedExclude. A state can be called, but not
	// changed. Optional.
	Changed am.S
	// ChangedExclude flips Changed to be a blocklist.
	ChangedExclude bool
	// TrackRejected is a flag to track rejected transitions.
	TrackRejected bool

	// stored data

	// TrackedStates is a list of states to store clock values of.
	TrackedStates am.S
	// StoreTransitions is a flag to store TransitionRecord, in addition to
	// TimeRecord, for each tracked transition.
	StoreTransitions bool
	// TODO StoreSchema keeps the latest machine schema and state names within
	// [MachineRecord]. Useful for dynamic machines.
	StoreSchema bool
	// TODO SkipHumanTime won't store [Time.HTime], to save space.
	// SkipHumanTime bool
	// TODO MachineOnly will store only [MachineRecord], making history queries
	// impossible.
	// MachineOnly bool

	// GC & TTL

	// MaxRecords is the maximum number of records to keep in the history before
	// a rotation begins.
	MaxRecords int
	// TODO MaxHumanAge is the maximum age of records in human time.
	// MaxHumanAge time.Duration
	// TODO MaxMachAge is the maximum machine time sum of the tracked machine.
	// MaxMachAge uint64
	// TODO MaxMachTrackedAge is the maximum machine time sum of the tracked
	// machine, counted from tracked states only.
	// MaxMachTrackedAge uint64
}

// Config for the in-process memory.
type Config = BaseConfig

// ///// ///// /////

// ///// RECORDS

// ///// ///// /////

type MachineRecord struct {
	// ID of the tracked machine
	MachId     string    `msgpack:"mi"`
	StateNames am.S      `msgpack:"sn"`
	Schema     am.Schema `msgpack:"s"`

	// human times

	// first time the machine has been tracked
	FirstTracking time.Time `msgpack:"ft"`
	// last time a tracking of this machine has started
	LastTracking time.Time `msgpack:"lt"`
	// last time a sync has been performed
	LastSync time.Time `msgpack:"ls"`

	// machine times

	// current (total) machine time
	MTime am.Time `msgpack:"mt"`
	// sum of the current machine time
	MTimeSum uint64 `msgpack:"mts"`
	// current machine start tick
	MachTick uint32 `msgpack:"mt2"`
	// next ID for time records
	NextId uint64 `msgpack:"ni"`
}

type MemoryRecord struct {
	Time       *TimeRecord
	Transition *TransitionRecord
}

type TimeRecord struct {
	// MutType is a mutation type.
	MutType am.MutationType `msgpack:"mt"`
	// MTimeSum is a machine time sum after this transition.
	MTimeSum uint64 `msgpack:"mts"`
	// MTimeSum is a machine time sum after this transition for tracked states
	// only.
	MTimeTrackedSum uint64 `msgpack:"mtts"`
	// MTimeDiffSum is a machine time difference for this transition.
	MTimeDiffSum uint64 `msgpack:"mtds"`
	// MTimeDiffSum is a machine time difference for this transition for tracked
	// states only.
	MTimeTrackedDiffSum uint64 `msgpack:"mttds"`
	// MTimeRecordDiffSum is a machine time difference since the previous
	// [TimeRecord].
	MTimeRecordDiffSum uint64 `msgpack:"mtrds"`
	// HTime is a human time in UTC.
	HTime time.Time `msgpack:"ht"`
	// MTime is a machine time after this mutation.
	MTimeTracked am.Time `msgpack:"mtt"`
	// MTimeTrackedDiff is a machine time diff compared to the previous mutation
	// (not a record).
	MTimeTrackedDiff am.Time `msgpack:"mttd"`
	// MachTick is the machine tick at the time of this transition.
	MachTick uint32 `msgpack:"mt2"`
	// TODO distance since the last change
	// DistanceLastChange am.Time eg [13, 23, 34] for [Foo, Bar, Baz] in
	// machine time ticks (not tracked-only ticks)
	// DistanceLastActive am.Time
}

type TransitionRecord struct {
	TransitionId string `msgpack:"ti"`
	TimeRecordId uint64 `msgpack:"tri"`
	SourceTx     string `msgpack:"st"`
	SourceMach   string `msgpack:"sm"`
	IsAuto       bool   `msgpack:"ia"`
	IsAccepted   bool   `msgpack:"ia2"`
	IsCheck      bool   `msgpack:"ic"`
	IsBroken     bool   `msgpack:"ib"`
	QueueLen     uint16 `msgpack:"ql"`

	// normal queue props

	QueuedAt   uint64 `msgpack:"qa"`
	ExecutedAt uint64 `msgpack:"ea"`

	// extra
	Called    []int             `msgpack:"c"`
	Arguments map[string]string `msgpack:"a"`
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
	if m.machRec == nil {
		m.machRec = &MachineRecord{
			MachId:        mach.Id(),
			FirstTracking: now,
			// compat only
			NextId: 1,
		}
	}

	rec := m.machRec
	mTime := mach.Time(nil)
	rec.MTimeSum = mTime.Sum(nil)
	rec.MTime = mTime
	rec.MachTick = mach.MachineTick()
	rec.LastTracking = now
	rec.LastSync = now

	// schema
	if m.Cfg.StoreSchema {
		rec.Schema = mach.Schema()
		rec.StateNames = mach.StateNames()
	}

	return nil
}

func (t *tracer) SchemaChange(machine am.Api, old am.Schema) {
	m := t.mem

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
}

func (t *tracer) TransitionEnd(tx *am.Transition) {
	m := t.mem
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

	// create record and count diffs
	var recordDiff uint64
	if len(m.db) > 0 {
		lastRec := m.db[len(m.db)-1]
		recordDiff = sum - lastRec.Time.MTimeSum
	}
	machTick := mach.MachineTick()
	now := time.Now().UTC()
	record := &MemoryRecord{
		Time: &TimeRecord{
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
		},
	}

	// optional tx record
	if cfg.StoreTransitions {
		record.Transition = &TransitionRecord{
			TransitionId: tx.Id,
			Called:       m.Index(called),
			IsAuto:       mut.Auto,
			IsAccepted:   tx.IsAccepted.Load(),
			IsCheck:      mut.IsCheck,
			IsBroken:     tx.IsBroken.Load(),
			QueueLen:     tx.QueueLen,
			QueuedAt:     mut.QueueTick,
			// TODO optimize?
			Arguments: tx.Mutation.MapArgs(mach.SemLogger().ArgsMapper()),
		}

		// optional fields
		if mut.Source != nil {
			record.Transition.SourceTx = mut.Source.TxId
			record.Transition.SourceMach = mut.Source.MachId
		}
		if mut.QueueTick > 0 {
			record.Transition.ExecutedAt = mach.QueueTick()
		}
	}

	// rotate and store
	if len(m.db) >= cfg.MaxRecords {
		m.db = m.db[1:]
	}
	m.db = append(m.db, record)

	// update machine record
	m.machRec.MTime = mTime
	m.machRec.MTimeSum = sum
	m.machRec.LastSync = now
	m.machRec.MachTick = machTick
	m.machRec.NextId++
}

// ///// ///// /////

// ///// BASE MEMORY

// ///// ///// /////

type MemoryApi interface {
	// predefined queries

	// ActivatedBetween returns true if the state become active withing the
	// passed conditions.
	ActivatedBetween(ctx context.Context, state string, start, end time.Time) bool
	// ActiveBetween returns true if the state was active at least once
	// within the passed conditions. Always true is ActivatedBetween is true.
	ActiveBetween(ctx context.Context, state string, start, end time.Time) bool
	DeactivatedBetween(
		ctx context.Context, state string, start, end time.Time,
	) bool
	InactiveBetween(ctx context.Context, state string, start, end time.Time) bool

	// DB queries

	FindLatest(
		ctx context.Context, retTx bool, limit int, query Query,
	) ([]*MemoryRecord, error)
	// Sync synchronizes the batch buffer with the underlying backend, making
	// those new records appear in queries. Doesn't guarantee persistence.
	// Useful when queries very fresh records.
	Sync() error

	// converters

	ToTimeRecord(format any) (*TimeRecord, error)
	ToMachineRecord(format any) (*MachineRecord, error)
	ToTransitionRecord(format any) (*TransitionRecord, error)

	// states

	IsTracked(states am.S) bool
	IsTracked1(state string) bool
	Index(states am.S) []int
	Index1(state string) int

	// misc

	Machine() am.Api
	Config() BaseConfig
	Context() context.Context
	MachineRecord() *MachineRecord
	Dispose() error
}

// BaseMemory are the common methods for all memory implementations, operating
// on common models, like [TimeRecord] or [Query].
type BaseMemory struct {
	// TODO move panic methods to an interface

	Ctx  context.Context
	Mach am.Api
	// read-only config for this history
	Cfg *BaseConfig

	// private

	// the top level memory implementing MemoryApi face
	memImpl MemoryApi

	// in-process mem only

	// lock for the machine record
	mx sync.RWMutex
	db []*MemoryRecord
	tr *tracer
}

var _ MemoryApi = &BaseMemory{}

func NewBaseMemory(
	ctx context.Context, mach am.Api, config BaseConfig, memImpl MemoryApi,
) *BaseMemory {
	return &BaseMemory{
		memImpl: memImpl,
		Mach:    mach,
		Cfg:     &config,
		Ctx:     ctx,
	}
}

// predefined queries

// ActivatedBetween returns true if the state was activated within the passed
// human time range.
func (m *BaseMemory) ActivatedBetween(
	ctx context.Context, state string, start, end time.Time,
) bool {
	ret, err := m.memImpl.FindLatest(ctx, false, 1, Query{
		Activated: am.S{state},
		Start: ConditionTime{
			HTime: start,
		},
		End: ConditionTime{
			HTime: end,
		},
	})

	return err == nil && ret != nil
}

func (m *BaseMemory) Sync() error {
	return nil
}

// ActiveBetween returns true if the state was activated all the time within
// the passed human time range.
func (m *BaseMemory) ActiveBetween(
	ctx context.Context, state string, start, end time.Time,
) bool {
	ret, err := m.memImpl.FindLatest(ctx, false, 1, Query{
		Active: am.S{state},
		Start: ConditionTime{
			HTime: start,
		},
		End: ConditionTime{
			HTime: end,
		},
	})

	return err == nil && ret != nil
}

func (m *BaseMemory) DeactivatedBetween(
	ctx context.Context, state string, start, end time.Time,
) bool {
	ret, err := m.memImpl.FindLatest(ctx, false, 1, Query{
		Deactivated: am.S{state},
		Start: ConditionTime{
			HTime: start,
		},
		End: ConditionTime{
			HTime: end,
		},
	})

	return err == nil && ret != nil
}

func (m *BaseMemory) InactiveBetween(
	ctx context.Context, state string, start, end time.Time,
) bool {
	ret, err := m.memImpl.FindLatest(ctx, false, 1, Query{
		Inactive: am.S{state},
		Start: ConditionTime{
			HTime: start,
		},
		End: ConditionTime{
			HTime: end,
		},
	})

	return err == nil && ret != nil
}

// DB queries

// FindLatest returns the latest records matching the given conditions, in order
// from the newest to the oldest. If the limit is 0, all records are returned.
func (m *BaseMemory) FindLatest(
	ctx context.Context, retTx bool, limit int, query Query,
) ([]*MemoryRecord, error) {
	panic("implement in subclass")
}

// state methods

// IsTracked returns true if the given states are all being tracked by this
// memory instance.
func (m *BaseMemory) IsTracked(states am.S) bool {
	for _, state := range states {
		if !slices.Contains(m.Cfg.TrackedStates, state) {
			return false
		}
	}

	return true
}

// IsTracked1 is IsTracked for a single state.
func (m *BaseMemory) IsTracked1(state string) bool {
	return m.IsTracked(am.S{state})
}

// Index returns the indexes of the given states in the history records, or -1
// if the state is not being tracked.
func (m *BaseMemory) Index(states am.S) []int {
	result := make([]int, len(states))
	for i, state := range states {
		result[i] = slices.Index(m.Cfg.TrackedStates, state)
	}
	return result
}

// Index1 is [BaseMemory.Index] for a single state.
func (m *BaseMemory) Index1(state string) int {
	return slices.Index(m.Cfg.TrackedStates, state)
}

// records methods

func (m *BaseMemory) ToTimeRecord(format any) (*TimeRecord, error) {
	tr, ok := format.(*TimeRecord)
	if !ok {
		return nil, ErrIncompatibleType
	}

	return tr, nil
}

func (m *BaseMemory) ToMachineRecord(format any) (*MachineRecord, error) {
	mr, ok := format.(*MachineRecord)
	if !ok {
		return nil, ErrIncompatibleType
	}

	return mr, nil
}

func (m *BaseMemory) ToTransitionRecord(format any) (*TransitionRecord, error) {
	tr, ok := format.(*TransitionRecord)
	if !ok {
		return nil, ErrIncompatibleType
	}

	return tr, nil
}

func (m *BaseMemory) ToMemoryRecord(format any) (*MemoryRecord, error) {
	mr, ok := format.(*MemoryRecord)
	if !ok {
		return nil, ErrIncompatibleType
	}

	return mr, nil
}

// misc

func (m *BaseMemory) Dispose() error {
	panic("implement in subclass")
}

// MachineRecord returns a copy of the history record for the tracked machine.
func (m *BaseMemory) MachineRecord() *MachineRecord {
	panic("implement in subclass")
}

func (m *BaseMemory) Machine() am.Api {
	panic("implement in subclass")
}

func (m *BaseMemory) Config() BaseConfig {
	panic("implement in subclass")
}

func (m *BaseMemory) Context() context.Context {
	return m.Ctx
}

func (m *BaseMemory) ValidateQuery(query Query) error {
	// validate states tracked
	names := slices.Concat(query.Active, query.Activated, query.Inactive,
		query.Deactivated, query.Start.MTimeStates, query.End.MTimeStates)
	for _, state := range names {
		if !m.IsTracked1(state) {
			return fmt.Errorf("%w: %w: %s not tracked",
				ErrCondition, am.ErrStateMissing, state)
		}
	}

	// validate MTime len == MTimeStates len
	if len(query.Start.MTimeStates) != len(query.Start.MTime) {
		return fmt.Errorf("%w: state list and machine time mismatch: %d != %d",
			ErrCondition, len(query.Start.MTimeStates), len(query.Start.MTime))
	}
	if len(query.End.MTimeStates) != len(query.End.MTime) {
		return fmt.Errorf("%w: state list and machine time mismatch: %d != %d",
			ErrCondition, len(query.End.MTimeStates), len(query.End.MTime))
	}

	return nil
}

// ///// ///// /////

// ///// IN-PROCESS MEMORY

// ///// ///// /////

type Memory struct {
	*BaseMemory

	onErr            func(err error)
	machRec          *MachineRecord
	cacheTrackedIdxs []int
}

// NewMemory returns a new memory instance that tracks the given machine
// according to the given tracking configuration. All states are tracked by
// default, which often is not desired. Keeps 1000 records by default.
func NewMemory(
	ctx context.Context, machRecord *MachineRecord, mach am.Api,
	config BaseConfig, onErr func(err error),
) (*Memory, error) {
	// TODO validate mach.Id() == machRecord.MachId

	c := config
	if c.MaxRecords <= 0 {
		c.MaxRecords = 1000
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

	// init and bind
	mem := &Memory{
		onErr:            onErr,
		cacheTrackedIdxs: mach.Index(c.TrackedStates),
	}
	mem.BaseMemory = NewBaseMemory(ctx, mach, c, mem)
	tr := &tracer{
		mem: mem,
	}
	mem.tr = tr
	if machRecord != nil {
		// clone
		cp := *machRecord
		tr.mem.machRec = &cp
	}
	tr.MachineInit(mach)
	mach.OnDispose(func(id string, ctx context.Context) {
		err := mem.Dispose()
		if err != nil {
			mem.onErr(err)
		}
	})

	return mem, mach.BindTracer(tr)
}

func (m *Memory) MachineRecord() *MachineRecord {
	m.mx.Lock()
	defer m.mx.Unlock()

	cp := *m.machRec
	return &cp
}

func (m *Memory) Machine() am.Api {
	return m.Mach
}

func (m *Memory) Config() BaseConfig {
	return *m.Cfg
}

func (m *Memory) Dispose() error {
	m.mx.Lock()
	defer m.mx.Unlock()

	m.db = nil
	return m.Mach.DetachTracer(m.tr)
}

// FindLatest is [BaseMemory.FindLatest] for in-process memory.
func (m *Memory) FindLatest(
	ctx context.Context, _ bool, limit int, query Query,
) ([]*MemoryRecord, error) {
	if err := m.ValidateQuery(query); err != nil {
		return nil, err
	}
	s := query.Start
	e := query.End
	mach := m.Mach

	return m.Match(ctx, func(
		now *am.TimeIndex, db []*MemoryRecord,
	) []*MemoryRecord {
		mTimeIdxs := m.Index(s.MTimeStates)
		var older *MemoryRecord
		var ret []*MemoryRecord

		for i := len(db) - 1; i >= 0; i-- {
			if ctx.Err() != nil {
				return nil
			}
			r := db[i]
			older = nil
			if i > 0 {
				older = db[i-1]
			}

			// states conditions

			// Active
			for _, state := range query.Active {
				if !am.IsActiveTick(r.Time.MTimeTracked[m.Index1(state)]) {
					continue
				}
			}
			// Activated
			for _, state := range query.Activated {
				idx := m.Index1(state)
				if !am.IsActiveTick(r.Time.MTimeTracked[idx]) {
					continue
				}
				// if has previously been active
				if older != nil && am.IsActiveTick(older.Time.MTimeTracked[idx]) {
					continue
				}
			}
			// Inactive
			for _, state := range query.Inactive {
				if am.IsActiveTick(r.Time.MTimeTracked[mach.Index1(state)]) {
					continue
				}
			}
			// Deactivated
			for _, state := range query.Deactivated {
				idx := m.Index1(state)
				if am.IsActiveTick(r.Time.MTimeTracked[idx]) {
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
				mTimeTrackedCond := r.Time.MTimeTracked.Filter(mTimeIdxs)
				if mTimeTrackedCond.Before(false, s.MTime) ||
					mTimeTrackedCond.After(false, e.MTime) {

					continue
				}
			}

			// time conditions
			t := r.Time

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

			// collect and return
			ret = append(ret, r)
			if limit > 0 && len(ret) >= limit {
				break
			}
		}

		return ret
	})
}

// Match returns the first record that matches the MatcherFn.
func (m *Memory) Match(
	ctx context.Context, matcherFn MatcherFn,
) ([]*MemoryRecord, error) {
	// clone DB index
	m.mx.RLock()
	db := make([]*MemoryRecord, len(m.db))
	copy(db, m.db)
	m.mx.RUnlock()

	ret := matcherFn(m.Mach.Time(nil).ToIndex(m.Mach.StateNames()), db)

	if ctx.Err() != nil || m.Ctx.Err() != nil {
		return nil, errors.Join(ctx.Err(), m.Ctx.Err())
	}
	return ret, nil
}

func (m *Memory) Export() []*MemoryRecord {
	m.mx.RLock()
	defer m.mx.RUnlock()

	// copy
	db := make([]*MemoryRecord, len(m.db))
	copy(db, m.db)

	return db
}
