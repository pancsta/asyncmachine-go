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
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// ///// ///// /////

// ///// AM-DBG

// ///// ///// /////

const DbgAddr = "localhost:6831"

// DbgMsg is the interface for the messages to be sent to the am-dbg server.
type DbgMsg interface {
	// Clock returns the state's clock, using the passed index
	Clock(statesIndex am.S, state string) uint64
	// Is returns true if the state is active, using the passed index
	Is(statesIndex am.S, states am.S) bool
}

// DbgMsgStruct contains the state and relations data.
type DbgMsgStruct struct {
	// Machine ID
	ID string
	// state names defining the indexes for diffs
	StatesIndex am.S
	// all the states with relations
	States am.Struct
}

func (d *DbgMsgStruct) Clock(_ am.S, _ string) uint64 {
	return 0
}

func (d *DbgMsgStruct) Is(_ am.S, _ am.S) bool {
	return false
}

// DbgMsgTx contains transition data.
type DbgMsgTx struct {
	MachineID string
	// Transition ID
	ID string
	// map of positions from the index to state clocks
	// TODO refac to TimeAfter, re-gen all the assets
	Clocks am.Time
	// result of the transition
	Accepted bool
	// mutation type
	Type am.MutationType
	// called states
	CalledStates []string
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
	// Time is human time. Don't send this over the wire.
	// TODO refac to HTime, re-gen all the assets
	Time *time.Time
}

func (d *DbgMsgTx) Clock(statesIndex am.S, state string) uint64 {
	idx := lo.IndexOf(statesIndex, state)
	return d.Clocks[idx]
}

func (d *DbgMsgTx) Is(statesIndex am.S, states am.S) bool {
	for _, state := range states {
		idx := lo.IndexOf(statesIndex, state) //nolint:typecheck
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

type dbgClient struct {
	addr string
	rpc  *rpc.Client
}

func newDbgClient(url string) (*dbgClient, error) {
	// log.Printf("Connecting to %s", url)
	client, err := rpc.Dial("tcp", url)
	if err != nil {
		return nil, err
	}

	return &dbgClient{rpc: client}, nil
}

func (c *dbgClient) sendMsgTx(msg *DbgMsgTx) error {
	var reply string
	// TODO use Go() to not block
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

type DbgTracer struct {
	am.NoOpTracer

	c *dbgClient
}

func (t *DbgTracer) StructChange(mach *am.Machine, _ am.Struct) {
	// TODO support struct patches as DbgMsgTx.StructPatch
	err := sendStructMsg(mach, t.c)
	if err != nil {
		log.Println("failed to send a msg to am-dbg: " + t.c.addr + err.Error())
	}
}

func (t *DbgTracer) TransitionEnd(tx *am.Transition) {
	mach := tx.Machine

	msg := &DbgMsgTx{
		MachineID:    mach.ID,
		ID:           tx.ID,
		Clocks:       tx.TimeAfter,
		Accepted:     tx.Accepted,
		Type:         tx.Mutation.Type,
		CalledStates: tx.CalledStates(),
		Steps: lo.Map(tx.Steps,
			func(step *am.Step, _ int) *am.Step {
				return step
			}),
		// no locking necessary, as the tx is finalized (read-only)
		LogEntries:    removeLogPrefix(mach, tx.LogEntries),
		PreLogEntries: removeLogPrefix(mach, tx.PreLogEntries),
		IsAuto:        tx.IsAuto(),
		Queue:         tx.QueueLen,
	}

	// TODO retries
	err := t.c.sendMsgTx(msg)
	if err != nil {
		log.Printf("failed to send a msg to am-dbg: %s %s", t.c.addr, err)
		return
	}
}

// TransitionsToDbg sends transitions to the am-dbg server.
func TransitionsToDbg(mach *am.Machine, addr string) error {
	// TODO test TransitionsToDbg
	// TODO support changes to mach.StateNames
	// TODO make sure the mach isnt already traced
	// TODO prevent double debugging
	gob.Register(am.Relation(0))

	if addr == "" {
		addr = DbgAddr
	}
	client, err := newDbgClient(addr)
	if err != nil {
		return fmt.Errorf("failed to connect to am-dbg: %w", err)
	}

	err = sendStructMsg(mach, client)
	if err != nil {
		return err
	}

	// add the tracer
	mach.Tracers = append(mach.Tracers, &DbgTracer{c: client})

	return nil
}

// sendStructMsg sends the machine's states and relations
func sendStructMsg(mach *am.Machine, client *dbgClient) error {
	msg := &DbgMsgStruct{
		ID:          mach.ID,
		StatesIndex: mach.StateNames(),
		States:      mach.GetStruct(),
	}

	// TODO retries
	err := client.sendMsgStruct(msg)
	if err != nil {
		return fmt.Errorf("failed to send a msg to am-dbg: %w", err)
	}

	return nil
}

func removeLogPrefix(mach *am.Machine, entries []*am.LogEntry) []*am.LogEntry {
	clone := slices.Clone(entries)
	if !mach.LogID {
		return clone
	}

	maxIDlen := 5
	addChars := 3 // "[] "
	prefixLen := min(len(mach.ID)+addChars, maxIDlen+addChars)

	ret := make([]*am.LogEntry, len(clone))
	for i, le := range clone {
		if len(le.Text) < prefixLen {
			continue
		}

		ret[i] = &am.LogEntry{
			Level: le.Level,
			Text:  le.Text[prefixLen:],
		}
	}

	return ret
}

// ///// ///// /////

// ///// OPEN TELEMETRY

// ///// ///// /////

// OtelMachTracer implements machine.Tracer for OpenTelemetry.
// Support tracing multiple machines
type OtelMachTracer struct {
	Tracer        trace.Tracer
	Machines      map[string]*OtelMachineData
	MachinesMx    sync.Mutex
	MachinesOrder []string
	Logf          func(format string, args ...any)
	// map of parent Span for each submachine
	parentSpans map[string]trace.Span
	// child-parent map, used for parentSpans
	parents map[string]string

	opts  *OtelMachTracerOpts
	ended bool
}

// TODO runtime typecheck for am.Tracer

type OtelMachineData struct {
	ID string
	// handler ctx & span to be used for more detailed tracing inside handlers
	HandlerTrace context.Context
	HandlerSpan  trace.Span
	Lock         sync.Mutex
	Ended        bool

	machTrace context.Context
	txTrace   context.Context
	// per-state group traces
	stateNames map[string]context.Context
	// per-state traces
	stateInstances map[string]context.Context
	// group trace for all state name groups
	stateGroup context.Context
	// group trace for all the transitions
	txGroup context.Context
}

type OtelMachTracerOpts struct {
	// if true, only state changes will be traced
	SkipTransitions bool
	Logf            func(format string, args ...any)
}

// NewOtelMachTracer creates a new machine tracer from an OpenTelemetry tracer.
// Requires OtelMachTracer.Dispose to be called at the end.
func NewOtelMachTracer(tracer trace.Tracer, opts *OtelMachTracerOpts,
) *OtelMachTracer {
	if tracer == nil {
		panic("nil tracer")
	}
	if opts == nil {
		opts = &OtelMachTracerOpts{}
	}
	otel := &OtelMachTracer{
		Tracer:      tracer,
		Machines:    make(map[string]*OtelMachineData),
		opts:        opts,
		parentSpans: make(map[string]trace.Span),
		parents:     make(map[string]string),
	}
	if opts.Logf != nil {
		otel.Logf = opts.Logf
	} else {
		otel.Logf = func(format string, args ...any) {}
	}
	otel.Logf("[otel] NewOtelMachTracer")
	return otel
}

func (ot *OtelMachTracer) getMachineData(id string) *OtelMachineData {
	ot.MachinesMx.Lock()
	defer ot.MachinesMx.Unlock()

	if data, ok := ot.Machines[id]; ok {
		return data
	}
	data := &OtelMachineData{
		stateInstances: make(map[string]context.Context),
		stateNames:     make(map[string]context.Context),
	}
	if !ot.ended {
		ot.Logf("[otel] getMachineData: creating for %s", id)
		ot.Machines[id] = data
		ot.MachinesOrder = append(ot.MachinesOrder, id)
	}
	return data
}

func (ot *OtelMachTracer) MachineInit(mach *am.Machine) {
	name := "mach:" + mach.ID

	// nest under parent
	ctx := mach.Ctx
	pid, ok := ot.parents[mach.ID]
	if ok {
		if parentSpan, ok := ot.parentSpans[pid]; ok {
			ctx = trace.ContextWithSpan(ctx, parentSpan)
		}
	}

	// create a machine trace
	machCtx, _ := ot.Tracer.Start(ctx, name, trace.WithAttributes(
		attribute.String("id", mach.ID),
	))

	// add tracing to machine's context
	ot.Logf("[otel] MachineInit: trace %s", mach.ID)
	mach.Ctx = machCtx
	data := ot.getMachineData(mach.ID)
	data.machTrace = machCtx

	data.ID = mach.ID

	// create a group span for states
	stateGroupCtx, stateGroupSpan := ot.Tracer.Start(machCtx, "states",
		trace.WithAttributes(attribute.String("mach_id", mach.ID)))
	data.stateGroup = stateGroupCtx
	// groups are only for nesting, so end it right away
	stateGroupSpan.End()

	if !ot.opts.SkipTransitions {
		// create a group span for transitions
		txGroupCtx, txGroupSpan := ot.Tracer.Start(machCtx, "transitions",
			trace.WithAttributes(attribute.String("mach_id", mach.ID)))
		data.txGroup = txGroupCtx
		// groups are only for nesting, so end it right away
		txGroupSpan.End()
	}
}

// NewSubmachine links 2 machines with a parent-child relationship.
func (ot *OtelMachTracer) NewSubmachine(parent, mach *am.Machine) {
	if ot.ended {
		ot.Logf("[otel] NewSubmachine: tracer already ended, ignoring %s",
			mach.ID)
		return
	}

	_, ok := ot.parents[mach.ID]
	if ok {
		panic("Submachine already being traced (duplicate ID " + mach.ID + ")")
	}
	ot.parents[mach.ID] = parent.ID
	data := ot.getMachineData(parent.ID)
	data.Lock.Lock()
	defer data.Lock.Unlock()

	if data.Ended {
		ot.Logf("[otel] NewSubmachine: parent %s already ended", parent.ID)
		return
	}

	if _, ok := ot.parentSpans[parent.ID]; !ok {
		// create a group span for submachines
		_, submachGroupSpan := ot.Tracer.Start(
			data.machTrace, "submachines",
			trace.WithAttributes(attribute.String("mach_id", parent.ID)))
		ot.parentSpans[parent.ID] = submachGroupSpan
		// groups are only for nesting, so end it right away
		submachGroupSpan.End()
	}
}

func (ot *OtelMachTracer) MachineDispose(id string) {
	ot.MachinesMx.Lock()
	defer ot.MachinesMx.Unlock()

	ot.doDispose(id)
}

func (ot *OtelMachTracer) doDispose(id string) {
	data, ok := ot.Machines[id]
	if !ok {
		ot.Logf("[otel] MachineDispose: machine %s not found", id)
		return
	}
	ot.Logf("[otel] MachineDispose: disposing %s", id)
	data.Lock.Lock()

	delete(ot.parentSpans, id)
	delete(ot.Machines, id)
	ot.MachinesOrder = lo.Without(ot.MachinesOrder, id)
	data.Ended = true

	// transitions
	if data.HandlerSpan != nil {
		data.HandlerSpan.End()
	}
	if data.txTrace != nil {
		trace.SpanFromContext(data.txTrace).End()
	}

	// states
	for _, ctx := range data.stateInstances {
		trace.SpanFromContext(ctx).End()
	}
	for _, ctx := range data.stateNames {
		trace.SpanFromContext(ctx).End()
	}

	// groups
	trace.SpanFromContext(data.stateGroup).End()
	trace.SpanFromContext(data.txGroup).End()
	trace.SpanFromContext(data.machTrace).End()
	data.Lock.Unlock()
}

func (ot *OtelMachTracer) TransitionInit(tx *am.Transition) {
	if ot.ended {
		ot.Logf("[otel] TransitionInit: tracer already ended, ignoring %s",
			tx.Machine.ID)
		return
	}
	data := ot.getMachineData(tx.Machine.ID)
	if data.Ended {
		ot.Logf("[otel] TransitionInit: machine %s already ended", tx.Machine.ID)
		return
	}

	// if skipping transitions, only create the machine data for states
	if ot.opts.SkipTransitions {
		return
	}
	mutLabel := fmt.Sprintf("[%s] %s",
		tx.Mutation.Type, j(tx.Mutation.CalledStates))
	name := mutLabel

	// support exceptions along with the passed error
	var errAttr error
	if lo.Contains(tx.TargetStates, am.Exception) {
		name = "exception"
		if tx.Args() != nil {
			if err, ok := tx.Args()["err"].(error); ok {
				errAttr = err
			}
		}
	}

	// build a regular trace
	ctx, span := ot.Tracer.Start(data.txGroup, name, trace.WithAttributes(
		attribute.String("tx_id", tx.ID),
		attribute.Int64("time_before", timeSum(tx.TimeBefore)),
		attribute.String("mutation", mutLabel),
	))

	// decorate Exception trace
	if errAttr != nil {
		span.SetAttributes(
			attribute.String("error", errAttr.Error()),
		)
	}

	// trace logged args, if any
	argsMatcher := tx.Machine.GetLogArgs()
	if argsMatcher != nil {
		for param, val := range argsMatcher(tx.Args()) {
			span.SetAttributes(
				attribute.String("args."+param, val),
			)
		}
	}

	// expose
	data.txTrace = ctx
}

func (ot *OtelMachTracer) TransitionEnd(tx *am.Transition) {
	if ot.ended {
		ot.Logf("[otel] TransitionEnd: tracer already ended, ignoring %s",
			tx.Machine.ID)
		return
	}
	data := ot.getMachineData(tx.Machine.ID)
	if data.Ended {
		ot.Logf("[otel] TransitionEnd: machine %s already ended", tx.Machine.ID)
		return
	}
	data.Lock.Lock()
	defer data.Lock.Unlock()

	statesAdded := am.DiffStates(tx.TargetStates, tx.StatesBefore)
	statesRemoved := am.DiffStates(tx.StatesBefore, tx.TargetStates)
	// support multi states
	before := tx.ClockBefore()
	for name, tick := range tx.ClockAfter() {
		if tick > 1+before[name] && !lo.Contains(statesAdded, name) {
			statesAdded = append(statesAdded, name)
		}
	}

	// handle transition
	statesDiff := ""
	if len(statesAdded) > 0 {
		statesDiff += "+" + jw(statesAdded, " +")
	}
	if len(statesRemoved) > 0 {
		statesDiff += " -" + jw(statesRemoved, " -")
	}
	if !ot.opts.SkipTransitions && data.txTrace != nil {
		span := trace.SpanFromContext(data.txTrace)
		span.SetAttributes(
			attribute.String("states_diff", strings.Trim(statesDiff, " ")),
			attribute.Int64("time_after", timeSum(tx.TimeAfter)),
			attribute.Bool("accepted", tx.Accepted),
			attribute.Int("steps_count", len(tx.Steps)),
		)
		span.End()
		data.txTrace = nil
	}

	// handle state changes
	// remove old states
	for _, state := range statesRemoved {
		if ctx, ok := data.stateInstances[state]; ok {
			trace.SpanFromContext(ctx).End()
			delete(data.stateInstances, state)
		}
	}

	// add new state trace, with a group if needed
	for _, state := range statesAdded {
		if data.Ended {
			ot.Logf("[otel] TransitionEnd: machine %s already ended", tx.Machine.ID)
			break
		}
		// name group
		nameCtx, ok := data.stateNames[state]
		if !ok {
			// create a new state name group trace, but end it right away
			ctx, span := ot.Tracer.Start(data.stateGroup, state)
			nameCtx = ctx
			data.stateNames[state] = nameCtx
			span.End()
		}
		// multi state - end the prev instance
		_, ok = data.stateInstances[state]
		if ok {
			trace.SpanFromContext(data.stateInstances[state]).End()
		}
		// TODO use trace.WithLinks() to the tx span, which contains args
		ctx, _ := ot.Tracer.Start(nameCtx, state, trace.WithAttributes(
			attribute.String("tx_id", tx.ID),
		))
		data.stateInstances[state] = ctx
	}
}

func (ot *OtelMachTracer) HandlerStart(
	tx *am.Transition, emitter string, handler string,
) {
	if ot.ended {
		return
	}
	if ot.opts.SkipTransitions {
		return
	}
	data := ot.getMachineData(tx.Machine.ID)
	if data.Ended {
		return
	}
	name := handler
	ctx, span := ot.Tracer.Start(data.txTrace, name, trace.WithAttributes(
		attribute.String("name", handler),
		attribute.String("emitter", emitter),
	))
	data.HandlerTrace = ctx
	data.HandlerSpan = span
}

func (ot *OtelMachTracer) HandlerEnd(tx *am.Transition, _ string, _ string) {
	if ot.ended {
		return
	}
	if ot.opts.SkipTransitions {
		return
	}
	data := ot.getMachineData(tx.Machine.ID)
	if data.Ended {
		return
	}
	trace.SpanFromContext(data.HandlerTrace).End()
	data.HandlerTrace = nil
	data.HandlerSpan = nil
}

func (ot *OtelMachTracer) End() {
	ot.MachinesMx.Lock()
	defer ot.MachinesMx.Unlock()

	ot.Logf("[otel] End")
	ot.ended = true
	// end traces in reverse order
	slices.Reverse(ot.MachinesOrder)

	for _, id := range ot.MachinesOrder {
		ot.doDispose(id)
	}

	ot.Machines = nil
}

func (ot *OtelMachTracer) Inheritable() bool {
	return true
}

func (ot *OtelMachTracer) QueueEnd(*am.Machine) {}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

// j joins state names
func j(states []string) string {
	return strings.Join(states, " ")
}

// jw joins state names with `sep`.
func jw(states []string, sep string) string {
	return strings.Join(states, sep)
}

func timeSum(clocks am.Time) int64 {
	sum := int64(0)
	for _, clock := range clocks {
		sum += int64(clock)
	}
	return sum
}
