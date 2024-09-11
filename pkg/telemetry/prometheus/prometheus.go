// Package prometheus provides Prometheus metrics for asyncam.
// The metrics are collected from the machine's transitions and states.
//
// Exported metrics:
// - states amount
// - relations amount
// - rel referenced states
package prometheus

// import "github.com/pancsta/asyncmachine-go/pkg/telemetry/prometheus"

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type promTracer struct {
	am.NoOpTracer

	m           *Metrics
	txStartTime time.Time
	prevTime    uint64
}

func (t *promTracer) StructChange(machine *am.Machine, old am.Struct) {
	// TODO refresh state and relation metrics
}

func (t *promTracer) TransitionInit(tx *am.Transition) {
	t.txStartTime = time.Now()
}

func (t *promTracer) TransitionEnd(tx *am.Transition) {
	if t.m.closed || !tx.Accepted {
		return
	}

	statesIndex := tx.Machine.StateNames()

	// try to refresh, then lock
	t.m.Refresh()
	t.m.mx.Lock()
	defer t.m.mx.Unlock()

	queueLen := tx.QueueLen
	t.m.queueSize = uint64(queueLen)
	t.m.queueSizeLen++
	t.m.stepsAmount += uint64(len(tx.Steps))
	t.m.stepsAmountLen++
	// TODO log slow txs (Opts and default to 1ms)
	t.m.txTime += uint64(time.Since(t.txStartTime).Milliseconds())
	t.m.txTimeLen++

	// executed handlers
	handlersCount := 0
	for _, step := range tx.Steps {
		if step.Type == am.StepHandler {
			handlersCount++
		}
	}
	t.m.handlersAmount += uint64(handlersCount)
	t.m.handlersAmountLen++

	// tx states
	added, removed, touched := getTxStates(tx, statesIndex)
	t.m.statesAdded += uint64(len(added))
	t.m.statesAddedLen++
	t.m.statesRemoved += uint64(len(removed))
	t.m.statesRemovedLen++
	t.m.statesTouched += uint64(len(touched))

	// time sum
	currTime := tx.Machine.TimeSum(nil)
	t.m.txTick += currTime - t.prevTime
	t.m.txTickLen++
	t.prevTime = currTime

	// active / inactive states
	active := 0
	inactive := 0
	for _, t := range tx.TimeBefore {
		if am.IsActiveTick(t) {
			active++
		} else {
			inactive++
		}
	}
	t.m.statesActiveAmount += uint64(active)
	t.m.statesActiveAmountLen++
	t.m.statesInactiveAmount += uint64(inactive)
}

func (t *promTracer) ExceptionState(err error) {
	t.m.ExceptionsCount.Inc()
}

// Metrics is a set of Prometheus metrics for a am.
type Metrics struct {
	mx         sync.Mutex
	closed     bool
	lastUpdate time.Time
	interval   time.Duration
	tracer     am.Tracer

	// //// mach definition

	// number of registered states
	StatesAmount prometheus.Gauge

	// number of relations for all registered states
	RelAmount prometheus.Gauge

	// number of state referenced by relations for all registered states
	RefStatesAmount prometheus.Gauge

	// //// tx data

	// current number of queued transitions (per transition)
	QueueSize    prometheus.Gauge
	queueSize    uint64
	queueSizeLen uint

	// transition duration in machine's clock (ticks per tx)
	TxTick    prometheus.Gauge
	txTick    uint64
	txTickLen uint

	// number of active states (per transition)
	StatesActiveAmount    prometheus.Gauge
	statesActiveAmount    uint64
	statesActiveAmountLen uint

	// number of inactive states (per transition)
	StatesInactiveAmount    prometheus.Gauge
	statesInactiveAmount    uint64
	statesInactiveAmountLen uint

	// number of states added (per transition)
	StatesAdded    prometheus.Gauge
	statesAdded    uint64
	statesAddedLen uint

	// number of states removed (per transition)
	StatesRemoved    prometheus.Gauge
	statesRemoved    uint64
	statesRemovedLen uint

	// number of states touched (per transition)
	StatesTouched    prometheus.Gauge
	statesTouched    uint64
	statesTouchedLen uint

	// number of errors
	ExceptionsCount    prometheus.Gauge
	exceptionsCount    uint64
	exceptionsCountLen uint

	// //// stats

	// steps per transition
	StepsAmount    prometheus.Gauge
	stepsAmount    uint64
	stepsAmountLen uint

	// amount of executed handlers per tx
	HandlersAmount    prometheus.Gauge
	handlersAmount    uint64
	handlersAmountLen uint

	// transition time
	TxTime    prometheus.Gauge
	txTime    uint64
	txTimeLen uint
}

func newMetrics(mach *am.Machine, interval time.Duration) *Metrics {
	machID := am.NormalizeID(mach.ID)

	return &Metrics{
		interval:   interval,
		lastUpdate: time.Now(),

		// /// mach definition

		StatesAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_amount",
			Help:      "Number of registered states",
			Subsystem: machID,
			Namespace: "mach",
		}),
		RelAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "relations_amount",
			Help:      "Number of relations for all registered states",
			Subsystem: machID,
			Namespace: "mach",
		}),
		RefStatesAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ref_states_amount",
			Help: "Number of states referenced by relations for all" +
				" registered states",
			Subsystem: machID,
			Namespace: "mach",
		}),

		// /// tx data

		QueueSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "queue_size",
			Help:      "Current number of queued transitions",
			Subsystem: machID,
			Namespace: "mach",
		}),
		TxTick: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "tx_tick",
			Help:      "Tick size of this tx (sum of all changed clocks)",
			Subsystem: machID,
			Namespace: "mach",
		}),
		ExceptionsCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "exceptions_count",
			Help:      "Number of errors",
			Subsystem: machID,
			Namespace: "mach",
		}),
		StatesAdded: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_added",
			Help:      "Struct added",
			Subsystem: machID,
			Namespace: "mach",
		}),
		StatesRemoved: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_removed",
			Help:      "Struct removed",
			Subsystem: machID,
			Namespace: "mach",
		}),
		StatesTouched: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_touched",
			Help:      "Struct touched",
			Subsystem: machID,
			Namespace: "mach",
		}),
		StepsAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "steps_amount",
			Help:      "Steps per transition",
			Subsystem: machID,
			Namespace: "mach",
		}),
		HandlersAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "handlers_amount",
			Help:      "Amount of executed handlers per tx",
			Subsystem: machID,
			Namespace: "mach",
		}),
		TxTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "tx_time",
			Help:      "Transition time",
			Subsystem: machID,
			Namespace: "mach",
		}),
		StatesActiveAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_active_amount",
			Help:      "Active states amount",
			Subsystem: machID,
			Namespace: "mach",
		}),
		StatesInactiveAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_inactive_amount",
			Help:      "Inactive states amount",
			Subsystem: machID,
			Namespace: "mach",
		}),
	}
}

// Refresh updates averages values from the interval and updates the gauges.
func (m *Metrics) Refresh() {
	if m.closed || m.lastUpdate.Add(m.interval).After(time.Now()) {
		return
	}

	m.mx.Lock()
	defer m.mx.Unlock()

	// update the gauges
	m.QueueSize.Set(average(m.queueSize, m.queueSizeLen))
	m.TxTick.Set(average(m.txTick, m.txTickLen))
	m.StatesActiveAmount.Set(
		average(m.statesActiveAmount, m.statesActiveAmountLen))
	m.StatesInactiveAmount.Set(
		average(m.statesInactiveAmount, m.statesInactiveAmountLen))
	m.StatesAdded.Set(average(m.statesAdded, m.statesAddedLen))
	m.StatesRemoved.Set(average(m.statesRemoved, m.statesRemovedLen))
	m.StatesTouched.Set(average(m.statesTouched, m.statesTouchedLen))
	m.ExceptionsCount.Set(average(m.exceptionsCount, m.exceptionsCountLen))
	m.StepsAmount.Set(average(m.stepsAmount, m.stepsAmountLen))
	m.HandlersAmount.Set(average(m.handlersAmount, m.handlersAmountLen))
	m.TxTime.Set(average(m.txTime, m.txTimeLen))

	// reset buffers
	m.queueSize = 0
	m.queueSizeLen = 0
	m.txTick = 0
	m.txTickLen = 0
	m.statesActiveAmount = 0
	m.statesActiveAmountLen = 0
	m.statesInactiveAmount = 0
	m.statesInactiveAmountLen = 0
	m.statesAdded = 0
	m.statesAddedLen = 0
	m.statesRemoved = 0
	m.statesRemovedLen = 0
	m.statesTouched = 0
	m.statesTouchedLen = 0
	m.exceptionsCount = 0
	m.exceptionsCountLen = 0
	m.stepsAmount = 0
	m.stepsAmountLen = 0
	m.handlersAmount = 0
	m.handlersAmountLen = 0
	m.txTime = 0
	m.txTimeLen = 0

	// tag it
	m.lastUpdate = time.Now()
}

// Close sets all gauges to 0.
func (m *Metrics) Close() {

	// close only once
	if m.closed {
		return
	}
	m.closed = true

	// set all gauges to 0
	m.StatesAmount.Set(0)
	m.RelAmount.Set(0)
	m.RefStatesAmount.Set(0)
	m.QueueSize.Set(0)
	m.TxTick.Set(0)
	m.StatesActiveAmount.Set(0)
	m.StatesInactiveAmount.Set(0)
	m.StatesAdded.Set(0)
	m.StatesRemoved.Set(0)
	m.StatesTouched.Set(0)
	m.ExceptionsCount.Set(0)
	m.StepsAmount.Set(0)
	m.HandlersAmount.Set(0)
	m.TxTime.Set(0)
}

func average(sum uint64, sampleLen uint) float64 {
	if sampleLen == 0 {
		return 0
	}

	return float64(sum / uint64(sampleLen))
}

// TransitionsToPrometheus bind transitions to Prometheus metrics.
func TransitionsToPrometheus(
	mach *am.Machine, interval time.Duration,
) *Metrics {
	metrics := newMetrics(mach, interval)

	// state & relations
	// TODO bind to EventStatesChange
	metrics.StatesAmount.Set(float64(len(mach.StateNames())))
	relCount := 0
	stateRefCount := 0
	for _, state := range mach.GetStruct() {

		if state.Add != nil {
			relCount++
			stateRefCount += len(state.Add)
		}
		if state.Remove != nil {
			relCount++
			stateRefCount += len(state.Remove)
		}
		if state.Require != nil {
			relCount++
			stateRefCount += len(state.Require)
		}
		if state.After != nil {
			relCount++
			stateRefCount += len(state.After)
		}
	}
	metrics.RelAmount.Set(float64(relCount))
	metrics.RefStatesAmount.Set(float64(stateRefCount))

	mach.Tracers = append(mach.Tracers, &promTracer{m: metrics})

	return metrics
}

// TODO move to helpers
func getTxStates(
	tx *am.Transition, index am.S,
) (added am.S, removed am.S, touched am.S) {

	before := tx.TimeBefore
	after := tx.TimeAfter

	is := func(time am.Time, i int) bool {
		return am.IsActiveTick(time[i])
	}

	for i, name := range index {
		if is(before, i) && !is(after, i) {
			removed = append(removed, name)
		} else if !is(before, i) && is(after, i) {
			added = append(added, name)
		} else if before[i] != after[i] {
			// treat multi states as added
			added = append(added, name)
		}
	}

	// touched
	touched = am.S{}
	for _, step := range tx.Steps {
		if step.FromState != "" {
			touched = append(touched, step.FromState)
		}
		if step.ToState != "" {
			touched = append(touched, step.ToState)
		}
	}

	return added, removed, touched
}
