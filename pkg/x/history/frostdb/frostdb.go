package frostdb

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/polarsignals/frostdb"
	"golang.org/x/sync/errgroup"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// type Machine struct {
// 	Id        string
// 	CreatedAt time.Time
// 	UpdatedAt time.Time
// 	Time      *datatypes.JSON
// 	TimeSum   uint64
// }

type Time struct {
	Id uint64

	// special fields

	MutType      uint64
	TimeSum      uint64
	TimeDiff     uint64
	TransitionId string

	// state ticks

	States map[string]uint64 `frostdb:",rle_dict,asc(1),null_first"`
}

type Transition struct {
	Id string

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

	// extra
	Called    []uint64
	Arguments map[string]string
}

type Memory struct {
	Store *frostdb.ColumnStore
	Db    *frostdb.DB
}

func NewMemory(ctx context.Context) (*Memory, error) {
	store, err := frostdb.New(frostdb.WithWAL(), frostdb.WithStoragePath("."))
	if err != nil {
		return nil, err
	}
	db, err := store.DB(ctx, "amhist")
	if err != nil {
		return nil, err
	}

	return &Memory{
		Db:    db,
		Store: store,
	}, nil
}

func (m *Memory) Close() error {
	return m.Db.Close()
}

// TODO better params
func (m *Memory) Track(
	onErr func(err error), mach *am.Machine, calledAllowlist,
	changedAllowlist am.S, maxEntries int,
) (*Tracer, error) {

	t := &Tracer{
		NoOpTracer:       &am.NoOpTracer{},
		loop:             &errgroup.Group{},
		db:               m.Db,
		lastActivated:    make(map[string]time.Time),
		calledAllowlist:  calledAllowlist,
		changedAllowlist: changedAllowlist,
		maxEntries:       maxEntries,
		onErr:            onErr,
		tTimes:           make(map[string]*frostdb.GenericTable[*Time]),
		tTransitions:     make(map[string]*frostdb.GenericTable[*Transition]),
	}
	t.loop.SetLimit(1)

	t.MachineInit(mach)

	return t, mach.BindTracer(t)
}

type Tracer struct {
	*am.NoOpTracer

	StoreChecks bool
	Active      atomic.Int32

	db *frostdb.DB
	// LastActivated is a map of state names to the last time they were activated
	// TODO keep?
	lastActivated map[string]time.Time
	// limits tracked states
	calledAllowlist am.S
	// limits tracked states
	changedAllowlist am.S
	maxEntries       int
	loop             *errgroup.Group
	onErr            func(err error)

	// tables

	// tMachines    *frostdb.GenericTable[Machine]
	tTimes       map[string]*frostdb.GenericTable[*Time]
	tTransitions map[string]*frostdb.GenericTable[*Transition]
}

func (t *Tracer) TransitionEnd(tx *am.Transition) {
	if tx.Mutation.IsCheck && !t.StoreChecks {
		return
	}

	mach := tx.Machine
	called := tx.CalledStates()

	// TODO optional
	// TODO track both called and changed
	match := true
	for _, name := range t.calledAllowlist {
		if slices.Contains(called, name) {
			match = true
			break
		}
	}
	if !match {
		return
	}

	calledIdxs := make([]uint64, len(called))
	for i, idx := range tx.Mutation.Called {
		calledIdxs[i] = uint64(idx)
	}

	// time and tx records
	recTime := &Time{
		MutType:      uint64(tx.Mutation.Type),
		TimeSum:      tx.Machine.Time(nil).Sum(nil),
		TimeDiff:     tx.TimeAfter.Sum(nil) - tx.TimeBefore.Sum(nil),
		TransitionId: tx.Id,
		States:       make(map[string]uint64, len(ssB.Names())),
	}
	recTx := &Transition{
		Id:         tx.Id,
		IsAuto:     tx.Mutation.Auto,
		IsAccepted: tx.IsAccepted.Load(),
		IsCheck:    tx.Mutation.IsCheck,
		IsBroken:   tx.IsBroken.Load(),
		QueuedAt:   tx.Mutation.QueueTick,
		Called:     calledIdxs,
		Arguments:  tx.Mutation.MapArgs(mach.SemLogger().ArgsMapper()),
		QueueLen:   mach.QueueLen(),
	}

	// optional fields
	if tx.Mutation.QueueTick > 0 {
		qt := mach.QueueTick()
		recTx.ExecutedAt = qt
	}
	if tx.Mutation.Source != nil {
		recTx.SourceTx = tx.Mutation.Source.TxId
		recTx.SourceMach = tx.Mutation.Source.MachId
	}

	// collect state ticks
	for _, state := range ssB.Names() {
		recTime.States[state] = tx.Machine.Tick(state)
	}

	// fork
	t.Active.Add(1)
	go t.loop.Go(func() error {
		// create
		// err := gorm.G[TimeMyMach1](t.db).Create(mach.Ctx(), record)
		// if err != nil {
		// 	t.onErr(err)
		// }
		_, err := t.tTransitions[mach.Id()].Write(mach.Ctx(), recTx)
		if err != nil {
			t.onErr(err)
			return err
		}
		_, err = t.tTimes[mach.Id()].Write(mach.Ctx(), recTime)
		if err != nil {
			t.onErr(err)
			return err
		}

		t.Active.Add(-1)

		return nil
	})
}

func (t *Tracer) MachineInit(mach am.Api) context.Context {

	// TODO append new machine state?
	// machine := Machine{
	// 	Id:        mach.Id(),
	// 	CreatedAt: time.Now(),
	// 	UpdatedAt: time.Now(),
	// }
	//
	// // machines
	// var err error
	// t.tMachines, err = frostdb.NewGenericTable[Machine](
	// 	t.db, "machines", memory.DefaultAllocator,
	// )
	// if err != nil {
	// 	t.onErr(err)
	// 	return nil
	// }

	// times
	times, err := frostdb.NewGenericTable[*Time](
		t.db, "times"+mach.Id(), memory.DefaultAllocator,
	)
	if err != nil {
		t.onErr(err)
		return nil
	}
	t.tTimes[mach.Id()] = times

	// transitions
	transitions, err := frostdb.NewGenericTable[*Transition](
		t.db, "transitions"+mach.Id(), memory.DefaultAllocator,
	)
	if err != nil {
		t.onErr(err)
		return nil
	}
	t.tTransitions[mach.Id()] = transitions

	return nil
}

// MOCKS TODO import from basic

// BasicStatesDef contains all the basic states.
type BasicStatesDef struct {
	*am.StatesBase

	// ErrNetwork indicates a generic network error.
	ErrNetwork string
	// ErrHandlerTimeout indicates one of the state machine handlers has timed
	// out.
	ErrHandlerTimeout string

	// Start indicates the machine should be working. Removing start can force
	// stop the machine.
	Start string
	// Ready indicates the machine meets criteria to perform work.
	Ready string
	// Healthcheck is a periodic request making sure that the machine is still
	// alive.
	Healthcheck string
	// Heartbeat is a periodic state that ensures the integrity of the machine.
	Heartbeat string
}

var BasicSchema = am.Schema{
	// Errors

	ssB.Exception: {Multi: true},
	ssB.ErrNetwork: {
		Multi:   true,
		Require: am.S{am.StateException},
	},
	ssB.ErrHandlerTimeout: {
		Multi:   true,
		Require: am.S{am.StateException},
	},

	// Basics

	ssB.Start:       {},
	ssB.Ready:       {Require: am.S{ssB.Start}},
	ssB.Healthcheck: {Multi: true},
	ssB.Heartbeat:   {},
}

// EXPORTS AND GROUPS

var (
	ssB = am.NewStates(BasicStatesDef{})

	// BasicStates contains all the states for the Basic machine.
	BasicStates = ssB
)
