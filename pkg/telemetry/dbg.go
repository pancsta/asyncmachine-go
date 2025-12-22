// Package telemetry provides telemetry exporters for asyncmachine: am-dbg,
// Prometheus, and OpenTelemetry.
package telemetry

import (
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"net/rpc"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ///// ///// /////

// ///// AM-DBG

// ///// ///// /////

const (
	// DbgAddr is the default address of the am-dbg server.
	DbgAddr = "localhost:6831"
	// EnvAmDbgAddr is the address of a running am-dbg instance.
	// "1" expands to "localhost:6831"
	EnvAmDbgAddr = "AM_DBG_ADDR"
)

// DbgMsg is the interface for the messages to be sent to the am-dbg server.
type DbgMsg interface {
	// Clock returns the state's clock, using the passed index
	Clock(statesIndex am.S, state string) uint64
	// Is returns true if the state is active, using the passed index
	Is(statesIndex am.S, states am.S) bool
}

// ///// STRUCT

// DbgMsgStruct contains the state and relations data.
type DbgMsgStruct struct {
	// TODO refac: DbgMsgSchema
	// TODO add schema ver

	// Machine ID
	ID string
	// state names defining the indexes for diffs
	StatesIndex am.S
	// all the states with relations
	// TODO refac: Schema
	States am.Schema
	// list of group names and state indexes
	Groups map[string][]int
	// order of groups
	GroupsOrder []string
	// parent machine ID
	Parent string
	// machine tags
	Tags []string

	// TODO include the current mach time
	//  MTime am.Time
}

func (d *DbgMsgStruct) Clock(_ am.S, _ string) uint64 {
	return 0
}

func (d *DbgMsgStruct) Is(_ am.S, _ am.S) bool {
	return false
}

// ///// QUEUE

// type DbgMsgQueue struct {
// 	Mutation *am.Mutation
// }

// ///// TRANSITION

// DbgMsgTx contains transition data.
type DbgMsgTx struct {
	MachineID string
	// Transition ID
	// TODO refac: Id
	ID string
	// Clocks is represents the machine time [am.Time] from after the current
	// transition.
	// TODO refac to TimeAfter, re-gen all the assets
	Clocks am.Time
	// QueueTick is the current queue tick in the machine.
	// transition.
	QueueTick uint64
	// TODO QueueDebug with all string entries for comparison
	// MutQueueToken is the token of a prepended mutation, can be scheduled or
	// executed, depending on IsQueued.
	MutQueueToken uint64
	// MutQueueTick is the assigned queue tick when the tx will be executed.
	// Only for IsQueued.
	MutQueueTick uint64
	// mutation type
	Type am.MutationType
	// called states
	// TODO remove. Deprecated use CalledStateNames(index)
	CalledStates []string
	// TODO rename to CalledStates, re-gen all assets
	CalledStatesIdxs []int
	// all the transition steps
	Steps []*am.Step
	// log entries created during the transition
	LogEntries []*am.LogEntry
	// log entries before the transition, which happened after the prev one
	PreLogEntries []*am.LogEntry
	// queue length at the start of the transition
	// TODO rename to QueueLen
	// TODO change to int32
	Queue int
	// Time is human time. Don't send this over the wire.
	// TODO remove or skip in msgpack
	// TODO rename to HTime
	Time *time.Time
	// transition was triggered by an auto state
	IsAuto bool
	// result of the transition
	// TODO rename to IsAccepted
	Accepted bool
	// is this a check (Can*) tx or mutation?
	IsCheck bool
	// is this a queued mutation?
	IsQueued  bool
	Args      map[string]string
	QueueDump []string

	// TODO add Mutation.Source, only if semLogger.IsGraph() == true
	// Source *am.MutSource
	// TODO include missed mutations (?) because of the queue limit
	//  since the previous tx
	// TODO add Transition.PipesAdded
	// TODO add Transition.PipesRemoved
	// TODO add Transition.CtxAdded
	// TODO add Transition.CtxRemoved
	// TODO add time taken via tracer
}

func (m *DbgMsgTx) Clock(statesIndex am.S, state string) uint64 {
	idx := slices.Index(statesIndex, state)
	if len(m.Clocks) <= idx {
		return 0
	}
	return m.Clocks[idx]
}

func (m *DbgMsgTx) Is(statesIndex am.S, states am.S) bool {
	for _, state := range states {
		idx := m.Index(statesIndex, state)

		// new schema issue
		if idx == -1 {
			return false
		}

		if len(m.Clocks) <= idx || !am.IsActiveTick(m.Clocks[idx]) {
			return false
		}
	}

	return true
}

func (m *DbgMsgTx) Index(statesIndex am.S, state string) int {
	idx := slices.Index(statesIndex, state) //nolint:typecheck
	return idx
}

func (m *DbgMsgTx) ActiveStates(statesIndex am.S) am.S {
	ret := am.S{}

	for _, state := range statesIndex {
		idx := slices.Index(statesIndex, state)
		if len(m.Clocks) <= idx {
			continue
		}
		if am.IsActiveTick(m.Clocks[idx]) {
			ret = append(ret, state)
		}
	}

	return ret
}

func (m *DbgMsgTx) CalledStateNames(statesIndex am.S) am.S {
	// old compat
	if m.CalledStates != nil {
		return m.CalledStates
	}

	ret := make(am.S, len(m.CalledStatesIdxs))

	for i, idx := range m.CalledStatesIdxs {
		ret[i] = statesIndex[idx]
	}

	return ret
}

// TODO unify with Tx String
func (m *DbgMsgTx) TxString(statesIndex am.S) string {
	ret := "tx#" + m.ID + "\n[" + m.Type.String() + "] " +
		utils.J(m.CalledStateNames(statesIndex)) + "\n"
	// TODO add source from mutation
	for _, step := range m.Steps {
		ret += "- " + step.StringFromIndex(statesIndex) + "\n"
	}

	return ret
}

// TODO unify with Mut String
func (m *DbgMsgTx) MutString(statesIndex am.S) string {
	ret := "+"
	if m.Type == am.MutationRemove {
		ret = "-"
	}
	ret += utils.J(m.CalledStateNames(statesIndex))

	return ret
}

func (m *DbgMsgTx) Is1(statesIndex am.S, state string) bool {
	return m.Is(statesIndex, am.S{state})
}

// TODO Sum() and TimeSum(idxs []int)
func (m *DbgMsgTx) TimeSum() uint64 {
	sum := uint64(0)
	for _, clock := range m.Clocks {
		sum += clock
	}

	return sum
}

// ///// CLIENT

type dbgClient struct {
	addr string
	rpc  *rpc.Client
}

func newDbgClient(addr string) (*dbgClient, error) {
	// log.Printf("Connecting to %s", url)
	client, err := rpc.Dial("tcp4", addr)
	if err != nil {
		return nil, err
	}

	return &dbgClient{addr: addr, rpc: client}, nil
}

func (c *dbgClient) sendMsgTx(msg *DbgMsgTx) error {
	var reply string
	// DEBUG
	// fmt.Printf("sendMsgTx %v\n", msg.CalledStates)
	// TODO const name
	err := c.rpc.Call("RPCServer.DbgMsgTx", msg, &reply)
	if err != nil {
		return err
	}

	return nil
}

func (c *dbgClient) sendMsgSchema(msg *DbgMsgStruct) error {
	if c == nil {
		return nil
	}

	var reply string
	// TODO use Go() to not block
	// TODO const name
	err := c.rpc.Call("RPCServer.DbgMsgSchema", msg, &reply)
	if err != nil {
		return err
	}

	return nil
}

// ///// TRACER

type DbgTracer struct {
	*am.NoOpTracer

	Addr string
	Mach am.Api

	outbox   chan func()
	c        *dbgClient
	errCount atomic.Int32
	exited   atomic.Bool
	mx       sync.Mutex
	lastTx   string
	// number of queued mutations since lastTx
	queued    int
	lastMTime am.Time
}

var _ am.Tracer = &DbgTracer{}

func NewDbgTracer(mach am.Api, addr string) *DbgTracer {
	t := &DbgTracer{
		Addr:   addr,
		Mach:   mach,
		outbox: make(chan func(), 1000),
	}
	ctx := t.Mach.Ctx()

	// process the queue
	go func() {
		for {
			select {
			case f := <-t.outbox:
				f()
			case <-ctx.Done():
				return
			}
		}
	}()

	return t
}

func (t *DbgTracer) MachineInit(mach am.Api) context.Context {
	t.mx.Lock()
	defer t.mx.Unlock()

	gob.Register(am.Relation(0))
	var err error
	t.Mach = mach
	t.lastMTime = mach.Time(nil)

	// add to the queue
	t.outbox <- func() {
		if mach.IsDisposed() {
			return
		}
		t.c, err = newDbgClient(t.Addr)
		if err != nil && os.Getenv(am.EnvAmLog) != "" {
			log.Printf("%s: failed to connect to am-dbg: %s\n", mach.Id(), err)
			return
		}

		err = sendMsgSchema(mach, t.c)
		if err != nil && os.Getenv(am.EnvAmLog) != "" {
			log.Println(err, nil)
			return
		}
	}

	return nil
}

func (t *DbgTracer) SchemaChange(mach am.Api, _ am.Schema) {
	t.lastMTime = mach.Time(nil)

	// add to the queue
	t.outbox <- func() {
		err := sendMsgSchema(mach, t.c)
		if err != nil {
			err = fmt.Errorf("failed to send new struct to am-dbg: %w", err)
			mach.AddErr(err, nil)
			return
		}
	}
}

func (t *DbgTracer) TransitionEnd(tx *am.Transition) {
	mach := tx.Api
	if t.errCount.Load() > 10 && !t.exited.Load() {
		t.exited.Store(true)
		if os.Getenv(am.EnvAmLog) != "" {
			log.Println(mach.Id() + ": too many errors - detaching dbg tracer")
		}
		go func() {
			err := mach.DetachTracer(t)
			if err != nil && os.Getenv(am.EnvAmLog) != "" {
				log.Printf(mach.Id()+": failed to detach dbg tracer: %s\n", err)
			}
		}()

		return
	}
	mut := tx.Mutation

	// skip check mutations when not logging them
	if mut.IsCheck && !mach.SemLogger().IsCan() {
		return
	}

	tx.InternalLogEntriesLock.Lock()
	defer tx.InternalLogEntriesLock.Unlock()
	t.mx.Lock()
	defer t.mx.Unlock()

	msg := &DbgMsgTx{
		MachineID:        mach.Id(),
		ID:               tx.Id,
		Clocks:           tx.TimeAfter,
		Accepted:         tx.IsAccepted.Load(),
		IsCheck:          mut.IsCheck,
		Type:             mut.Type,
		CalledStatesIdxs: mut.Called,
		Steps:            tx.Steps,
		// no locking necessary, as the tx is finalized (read-only)
		LogEntries:    removeLogPrefix(mach, tx.LogEntries),
		PreLogEntries: removeLogPrefix(mach, tx.PreLogEntries),
		IsAuto:        mut.Auto,
		Queue:         int(tx.QueueLen),
		QueueTick:     mach.QueueTick(),
		MutQueueToken: mut.QueueToken,
		// debug
		// QueueDump: mach.QueueDump(),
	}

	// collect args
	semlog := mach.SemLogger()
	if semlog.IsArgs() {
		msg.Args = mut.MapArgs(semlog.ArgsMapper())
	}

	t.lastTx = tx.Id
	t.lastMTime = tx.TimeAfter
	t.queued = 0

	// add to the queue
	t.outbox <- func() {
		if t.c == nil {
			if os.Getenv(am.EnvAmLog) != "" {
				log.Println(mach.Id() + ": no connection to am-dbg")
			}
			t.errCount.Add(1)

			return
		}
		err := t.c.sendMsgTx(msg)
		if err != nil {
			if os.Getenv(am.EnvAmLog) != "" {
				log.Printf(mach.Id()+":failed to send a msg to am-dbg: %s", err)
			}
			t.errCount.Add(1)
		}
	}
}

func (t *DbgTracer) MachineDispose(id string) {
	// TODO lock & dispose?
	// t.Mach = nil

	go func() {
		// TODO push out pending m.logEntries using add:Healthcheck (if set)
		time.Sleep(100 * time.Millisecond)
		if t.c != nil && t.c.rpc != nil {
			_ = t.c.rpc.Close()
		}
	}()
}

func (t *DbgTracer) MutationQueued(mach am.Api, mut *am.Mutation) {
	// skip check mutations when not logging them
	if mut.IsCheck && !mach.SemLogger().IsCan() {
		return
	}

	t.mx.Lock()
	defer t.mx.Unlock()

	msg := &DbgMsgTx{
		MachineID:        mach.Id(),
		ID:               t.lastTx + "-" + strconv.Itoa(t.queued),
		Clocks:           t.lastMTime,
		Accepted:         true,
		IsCheck:          mut.IsCheck,
		Type:             mut.Type,
		CalledStatesIdxs: mut.Called,
		IsAuto:           mut.Auto,
		Queue:            int(mut.QueueLen),
		IsQueued:         true,
		QueueTick:        mach.QueueTick(),
		MutQueueTick:     mut.QueueTick,
		MutQueueToken:    mut.QueueToken,
		// debug
		// QueueDump: mach.QueueDump(),
	}

	// collect args
	semlog := mach.SemLogger()
	if semlog.IsArgs() {
		msg.Args = mut.MapArgs(semlog.ArgsMapper())
	}
	if err := am.ParseArgs(mut.Args).Err; err != nil {
		if msg.Args == nil {
			msg.Args = make(map[string]string)
		}
		msg.Args["err"] = err.Error()
	}
	t.queued++

	// add to the queue
	t.outbox <- func() {
		if t.c == nil {
			if os.Getenv(am.EnvAmLog) != "" {
				log.Println(mach.Id() + ": no connection to am-dbg")
			}
			t.errCount.Add(1)

			return
		}
		err := t.c.sendMsgTx(msg)
		if err != nil {
			if os.Getenv(am.EnvAmLog) != "" {
				log.Printf(mach.Id()+":failed to send a msg to am-dbg: %s", err)
			}
			t.errCount.Add(1)
		}
	}
}

// ///// FUNCS

// TransitionsToDbg sends transitions to the am-dbg server.
func TransitionsToDbg(mach am.Api, addr string) error {
	if addr == "" {
		addr = DbgAddr
	}

	// prevent double debugging
	tracers := mach.Tracers()
	for _, tracer := range tracers {
		if t, ok := tracer.(*DbgTracer); ok && t.Addr == addr {
			return nil
		}
	}

	// add the tracer
	tracer := NewDbgTracer(mach, addr)
	err := mach.BindTracer(tracer)
	if err != nil {
		return err
	}

	// call manually for existing machines
	tracer.MachineInit(mach)

	return nil
}

// sendMsgSchema sends the machine's states and relations
func sendMsgSchema(mach am.Api, client *dbgClient) error {
	groups, order := mach.Groups()
	msg := &DbgMsgStruct{
		ID:          mach.Id(),
		StatesIndex: mach.StateNames(),
		States:      mach.Schema(),
		Groups:      groups,
		GroupsOrder: order,
		Parent:      mach.ParentId(),
		Tags:        mach.Tags(),
	}

	// TODO retries
	err := client.sendMsgSchema(msg)
	if err != nil {
		return fmt.Errorf("failed to send a msg to am-dbg: %w", err)
	}

	return nil
}

func removeLogPrefix(mach am.Api, entries []*am.LogEntry) []*am.LogEntry {
	clone := slices.Clone(entries)
	if !mach.SemLogger().IsId() {
		return clone
	}

	maxIdLen := 5
	addChars := 3 // "[] "
	prefixLen := min(len(mach.Id())+addChars, maxIdLen+addChars)

	ret := make([]*am.LogEntry, len(clone))
	for i, le := range clone {
		if le == nil || len(le.Text) < prefixLen {
			continue
		}

		ret[i] = &am.LogEntry{
			Level: le.Level,
			Text:  le.Text[prefixLen:],
		}
	}

	return ret
}
