// Package telemetry provides telemetry exporters for asyncmachine: am-dbg,
// Prometheus, and OpenTelemetry.
package telemetry

import (
	"context"
	"encoding/gob"
	"fmt"
	"log"
	"net/rpc"
	"slices"
	"sync/atomic"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ///// ///// /////

// ///// AM-DBG

// ///// ///// /////

const (
	// DbgAddr is the default address of the am-dbg server.
	DbgAddr = "localhost:6831"
	// EnvAmDbgAddr set the address of a running am-dbg instance.
	// "localhost:6831" | "" (default)
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
	// parent machine ID
	Parent string
	// machine tags
	Tags []string
}

func (d *DbgMsgStruct) Clock(_ am.S, _ string) uint64 {
	return 0
}

func (d *DbgMsgStruct) Is(_ am.S, _ am.S) bool {
	return false
}

// ///// TRANSITION

// DbgMsgTx contains transition data.
type DbgMsgTx struct {
	MachineID string
	// Transition ID
	ID string
	// Clocks is represents the machine time [am.Time] from after the current
	// transition.
	// TODO refac to TimeAfter, re-gen all the assets
	Clocks am.Time
	// result of the transition
	Accepted bool
	// mutation type
	Type am.MutationType
	// called states
	// TODO index optimization
	CalledStates []string
	// TODO rename to CalledStates, re-gen all assets
	CalledStatesIdxs []int
	// all the transition steps
	Steps []*am.Step
	// log entries created during the transition
	LogEntries []*am.LogEntry
	// log entries before the transition, which happened after the prev one
	PreLogEntries []*am.LogEntry
	// transition was triggered by an auto state
	IsAuto bool
	// queue length at the start of the transition
	Queue int
	// TODO include missed mutations (?) because of the queue limit
	//  since the previous tx
	// Time is human time. Don't send this over the wire.
	// TODO remove, re-gen all the assets
	Time *time.Time
}

func (d *DbgMsgTx) Clock(statesIndex am.S, state string) uint64 {
	idx := slices.Index(statesIndex, state)
	return d.Clocks[idx]
}

func (d *DbgMsgTx) Is(statesIndex am.S, states am.S) bool {
	for _, state := range states {
		idx := d.Index(statesIndex, state)
		if idx == -1 {
			// TODO handle err (log?)
			panic("unknown state: " + state)
		}

		// TODO fix out of range panic, coming from am-dbg (not telemetry)
		if !am.IsActiveTick(d.Clocks[idx]) {
			return false
		}
	}

	return true
}

func (d *DbgMsgTx) Index(statesIndex am.S, state string) int {
	idx := slices.Index(statesIndex, state) //nolint:typecheck
	return idx
}

func (d *DbgMsgTx) ActiveStates(statesIndex am.S) am.S {
	ret := am.S{}

	for _, state := range statesIndex {
		if am.IsActiveTick(d.Clocks[slices.Index(statesIndex, state)]) {
			ret = append(ret, state)
		}
	}

	return ret
}

func (d *DbgMsgTx) CalledStateNames(statesIndex am.S) am.S {
	// old compat
	if d.CalledStates != nil {
		return d.CalledStates
	}

	ret := make(am.S, len(d.CalledStatesIdxs))

	for i, idx := range d.CalledStatesIdxs {
		ret[i] = statesIndex[idx]
	}

	return ret
}

func (d *DbgMsgTx) String(statesIndex am.S) string {
	ret := "("
	for _, state := range d.ActiveStates(statesIndex) {
		if ret != "(" {
			ret += " "
		}
		idx := d.Index(statesIndex, state)
		ret += fmt.Sprintf("%s:%d", state, d.Clocks[idx])
	}

	return ret + ")"
}

func (d *DbgMsgTx) Is1(statesIndex am.S, state string) bool {
	return d.Is(statesIndex, am.S{state})
}

func (d *DbgMsgTx) TimeSum() uint64 {
	sum := uint64(0)
	for _, clock := range d.Clocks {
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
	err := c.rpc.Call("RPCServer.DbgMsgTx", msg, &reply)
	if err != nil {
		return err
	}

	return nil
}

func (c *dbgClient) sendMsgStruct(msg *DbgMsgStruct) error {
	var reply string
	// TODO use Go() to not block
	err := c.rpc.Call("RPCServer.DbgMsgStruct", msg, &reply)
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

	queue    chan func()
	c        *dbgClient
	errCount atomic.Int32
	exited   atomic.Bool
}

func NewDbgTracer(mach am.Api, addr string) *DbgTracer {
	t := &DbgTracer{
		Addr:  addr,
		Mach:  mach,
		queue: make(chan func(), 1000),
	}
	ctx := t.Mach.Ctx()

	// process the queue
	go func() {
		for {
			select {
			case f := <-t.queue:
				f()
			case <-ctx.Done():
				return
			}
		}
	}()

	return t
}

func (t *DbgTracer) MachineInit(mach am.Api) context.Context {
	gob.Register(am.Relation(0))
	var err error
	t.Mach = mach

	// add to the queue
	t.queue <- func() {
		if mach.IsDisposed() {
			return
		}
		t.c, err = newDbgClient(t.Addr)
		if err != nil {
			log.Printf("failed to connect to am-dbg: %s\n", err)
			return
		}

		err = sendStructMsg(mach, t.c)
		if err != nil {
			log.Println(err, nil)
			return
		}
	}

	return nil
}

func (t *DbgTracer) SchemaChange(mach am.Api, _ am.Schema) {
	// TODO support struct patches as DbgMsgTx.StructPatch
	// add to the queue
	t.queue <- func() {
		err := sendStructMsg(mach, t.c)
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
		log.Println("too many errors - detaching dbg tracer")
		go mach.DetachTracer(t)

		return
	}

	tx.LogEntriesLock.Lock()
	defer tx.LogEntriesLock.Unlock()

	msg := &DbgMsgTx{
		MachineID:    mach.Id(),
		ID:           tx.ID,
		Clocks:       tx.TimeAfter,
		Accepted:     tx.Accepted,
		Type:         tx.Mutation.Type,
		CalledStates: tx.CalledStates(),
		Steps:        tx.Steps,
		// no locking necessary, as the tx is finalized (read-only)
		LogEntries:    removeLogPrefix(mach, tx.LogEntries),
		PreLogEntries: removeLogPrefix(mach, tx.PreLogEntries),
		IsAuto:        tx.IsAuto(),
		Queue:         tx.QueueLen,
	}

	// add to the queue
	t.queue <- func() {
		if t.c == nil {
			log.Println("no connection to am-dbg")
			t.errCount.Add(1)

			return
		}
		err := t.c.sendMsgTx(msg)
		if err != nil {
			log.Printf("failed to send a msg to am-dbg: %s", err)
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
	_ = mach.BindTracer(tracer)
	// call manually for existing machines
	tracer.MachineInit(mach)

	return nil
}

// sendStructMsg sends the machine's states and relations
func sendStructMsg(mach am.Api, client *dbgClient) error {
	msg := &DbgMsgStruct{
		ID:          mach.Id(),
		StatesIndex: mach.StateNames(),
		States:      mach.Schema(),
		Parent:      mach.ParentId(),
		Tags:        mach.Tags(),
	}

	// TODO retries
	err := client.sendMsgStruct(msg)
	if err != nil {
		return fmt.Errorf("failed to send a msg to am-dbg: %w", err)
	}

	return nil
}

func removeLogPrefix(mach am.Api, entries []*am.LogEntry) []*am.LogEntry {
	clone := slices.Clone(entries)
	if !mach.GetLogId() {
		return clone
	}

	maxIDlen := 5
	addChars := 3 // "[] "
	prefixLen := min(len(mach.Id())+addChars, maxIDlen+addChars)

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
