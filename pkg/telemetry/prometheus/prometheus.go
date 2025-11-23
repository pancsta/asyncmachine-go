// Package prometheus provides Prometheus metrics for asyncmachine.
// Metrics are collected from machine's transitions and states.
package prometheus

// TODO measure auto added states
// TODO collect also total numbers?
// TODO race in NewSUbmachine BindToPusher

import (
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
)

const EnvPromPushUrl = "AM_PROM_PUSH_URL"

type PromInheritTracer struct {
	*am.NoOpTracer

	m *Metrics
}

// PromTracer is [am.Tracer] for tracing state machines.
type PromTracer struct {
	*am.NoOpTracer

	m           *Metrics
	txStartTime time.Time
	prevTime    uint64
}

func (t *PromTracer) SchemaChange(machine am.Api, old am.Schema) {
	// TODO refresh state and relation metrics
}

func (t *PromTracer) MachineDispose(machId string) {
	t.m.Close()
}

func (t *PromTracer) NewSubmachine(parent, mach am.Api) {

	// skip RPC machines
	dbgRpc := os.Getenv("AM_RPC_DBG") != ""
	for _, tag := range mach.Tags() {
		if strings.HasPrefix(tag, "rpc-") && !dbgRpc {
			return
		}
	}

	// bind metrics and configured exporters
	m2 := BindMach(mach)
	if t.m.pusher != nil {
		BindToPusher(m2, t.m.pusher)
	}
	if t.m.registry != nil {
		BindToRegistry(m2, t.m.registry)
	}

	// bind inheritance tracer
	err := mach.BindTracer(&PromInheritTracer{
		m: m2,
	})
	if err != nil {
		mach.AddErr(err, nil)
		return
	}

	t.m.childMetrics = append(t.m.childMetrics, m2)
}

func (t *PromTracer) TransitionInit(tx *am.Transition) {
	t.txStartTime = time.Now()
}

func (t *PromTracer) TransitionEnd(tx *am.Transition) {
	if t.m.closed || !tx.IsAccepted.Load() {
		return
	}

	statesIndex := tx.Api.StateNames()
	t.m.mx.Lock()
	defer t.m.mx.Unlock()

	queueLen := tx.QueueLen
	t.m.queueSize = uint64(queueLen)
	t.m.queueSizeLen++
	t.m.stepsAmount += uint64(len(tx.Steps))
	t.m.stepsAmountLen++
	// TODO log slow txs (Opts and default to 1ms)
	t.m.txTime += uint64(time.Since(t.txStartTime).Microseconds())
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
	added, removed, touched := amhelp.GetTransitionStates(tx, statesIndex)
	switch tx.Mutation.Type {
	case am.MutationAdd:
		t.m.statesAdded += uint64(len(added))
		t.m.statesAddedLen++
	case am.MutationRemove:
		t.m.statesRemoved += uint64(len(removed))
		t.m.statesRemovedLen++
	case am.MutationSet:
		t.m.statesAdded += uint64(len(added))
		t.m.statesAddedLen++
		t.m.statesRemoved += uint64(len(removed))
		t.m.statesRemovedLen++
	}
	t.m.statesTouched += uint64(len(touched))
	t.m.statesTouchedLen++

	// time sum
	currTime := tx.Api.Time(nil).Sum(nil)
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
	t.m.statesInactiveAmountLen++

	// Exception
	if slices.Contains(tx.TargetStates(), am.StateException) {
		t.m.exceptionsCount++
	}

	t.m.transitionsCount++
}

// Metrics is a set of Prometheus metrics for asyncmachine.
type Metrics struct {
	// Tracer is a Prometheus tracer for the machine. You can detach it any time
	// using Machine.DetachTracer.
	Tracer am.Tracer

	mx     sync.Mutex
	closed bool

	// mach definition

	// number of registered states
	StatesAmount prometheus.Gauge

	// number of relations for all registered states
	RelAmount prometheus.Gauge

	// number of state referenced by relations for all registered states
	RefStatesAmount prometheus.Gauge

	// tx data

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
	ExceptionsCount prometheus.Gauge
	exceptionsCount uint64

	// number of transitions
	TransitionsCount prometheus.Gauge
	transitionsCount uint64

	// stats

	// steps per transition
	StepsAmount    prometheus.Gauge
	stepsAmount    uint64
	stepsAmountLen uint

	// amount of executed handlers per tx
	HandlersAmount    prometheus.Gauge
	handlersAmount    uint64
	handlersAmountLen uint

	// transition time
	TxTime       prometheus.Gauge
	txTime       uint64
	txTimeLen    uint
	pusher       *push.Pusher
	registry     *prometheus.Registry
	childMetrics []*Metrics
}

func newMetrics(mach am.Api) *Metrics {
	machId := telemetry.NormalizeId(mach.Id())

	return &Metrics{

		// mach definition

		StatesAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_" + machId,
			Help:      "Number of registered states",
			Namespace: "am",
		}),
		RelAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "relations_" + machId,
			Help:      "Number of relations for all states",
			Namespace: "am",
		}),
		RefStatesAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "ref_states_" + machId,
			Help:      "Number of states referenced by relations",
			Namespace: "am",
		}),

		// tx data

		QueueSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "queue_size_" + machId,
			Help:      "Average queue size",
			Namespace: "am",
		}),
		TxTick: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "tx_ticks_" + machId,
			Help:      "Average transition machine time taken (ticks)",
			Namespace: "am",
		}),
		ExceptionsCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "exceptions_" + machId,
			Help:      "Number of transitions with Exception active",
			Namespace: "am",
		}),
		TransitionsCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "transitions_" + machId,
			Help:      "Number of transitions",
			Namespace: "am",
		}),
		StatesAdded: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_added_" + machId,
			Help:      "Average amount of states added",
			Namespace: "am",
		}),
		StatesRemoved: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_removed_" + machId,
			Help:      "Average amount of states removed",
			Namespace: "am",
		}),
		StatesTouched: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_touched_" + machId,
			Help:      "Average amount of states touched",
			Namespace: "am",
		}),
		StepsAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "steps_" + machId,
			Help:      "Average amount of steps per transition",
			Namespace: "am",
		}),
		HandlersAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "handlers_" + machId,
			Help:      "Average amount of executed handlers per tx",
			Namespace: "am",
		}),
		TxTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "tx_time_" + machId,
			Help:      "Average transition human time taken (μs)",
			Namespace: "am",
		}),
		StatesActiveAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_active_" + machId,
			Help:      "Average amount of active states",
			Namespace: "am",
		}),
		StatesInactiveAmount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:      "states_inactive_" + machId,
			Help:      "Average amount of inactive states",
			Namespace: "am",
		}),
	}
}

// Sync synchronizes the metrics with the current counters, averaging certain
// values and using totals of others. After that the counters are reset to 0.
//
// Sync should be called right before pushing or scraping.
func (m *Metrics) Sync() {
	if m.closed {
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
	m.ExceptionsCount.Set(float64(m.exceptionsCount))
	m.TransitionsCount.Set(float64(m.transitionsCount))
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
	m.stepsAmount = 0
	m.stepsAmountLen = 0
	m.handlersAmount = 0
	m.handlersAmountLen = 0
	m.txTime = 0
	m.txTimeLen = 0
	m.transitionsCount = 0

	// sync children
	for _, child := range m.childMetrics {
		child.Sync()
	}
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
	m.TransitionsCount.Set(0)
	m.StepsAmount.Set(0)
	m.HandlersAmount.Set(0)
	m.TxTime.Set(0)
}

func (m *Metrics) SetPusher(pusher *push.Pusher) {
	m.pusher = pusher
}

func (m *Metrics) SetRegistry(registry *prometheus.Registry) {
	m.registry = registry
}

// BindMach bind transitions to Prometheus metrics.
func BindMach(mach am.Api) *Metrics {
	metrics := newMetrics(mach)
	mach.Log("[bind] prometheus metrics")

	// state & relations
	// TODO bind in SchemaChange
	metrics.StatesAmount.Set(float64(len(mach.StateNames())))
	relCount := 0
	stateRefCount := 0
	for _, state := range mach.Schema() {

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
	metrics.Tracer = &PromTracer{m: metrics}

	_ = mach.BindTracer(metrics.Tracer)

	return metrics
}

func BindToPusher(metrics *Metrics, pusher *push.Pusher) {

	metrics.SetPusher(pusher)

	// transition
	pusher.Collector(metrics.TransitionsCount)
	pusher.Collector(metrics.QueueSize)
	pusher.Collector(metrics.TxTick)
	pusher.Collector(metrics.StepsAmount)
	pusher.Collector(metrics.HandlersAmount)
	pusher.Collector(metrics.StatesAdded)
	pusher.Collector(metrics.StatesRemoved)
	pusher.Collector(metrics.StatesTouched)

	// machine states
	pusher.Collector(metrics.StatesAmount)
	pusher.Collector(metrics.RelAmount)
	pusher.Collector(metrics.RefStatesAmount)
	pusher.Collector(metrics.StatesActiveAmount)
	pusher.Collector(metrics.StatesInactiveAmount)

	// tx time
	pusher.Collector(metrics.TxTime)

	// errors
	pusher.Collector(metrics.ExceptionsCount)
}

func BindToRegistry(metrics *Metrics, registry *prometheus.Registry) {

	metrics.SetRegistry(registry)

	// transition
	registry.MustRegister(metrics.TransitionsCount)
	registry.MustRegister(metrics.QueueSize)
	registry.MustRegister(metrics.TxTick)
	registry.MustRegister(metrics.StepsAmount)
	registry.MustRegister(metrics.HandlersAmount)
	registry.MustRegister(metrics.StatesAdded)
	registry.MustRegister(metrics.StatesRemoved)
	registry.MustRegister(metrics.StatesTouched)

	// machine states
	registry.MustRegister(metrics.StatesAmount)
	registry.MustRegister(metrics.RelAmount)
	registry.MustRegister(metrics.RefStatesAmount)
	registry.MustRegister(metrics.StatesActiveAmount)
	registry.MustRegister(metrics.StatesInactiveAmount)

	// tx time
	registry.MustRegister(metrics.TxTime)

	// errors
	registry.MustRegister(metrics.ExceptionsCount)
}

func average(sum uint64, sampleLen uint) float64 {
	if sampleLen == 0 {
		return 0
	}

	return float64(sum / uint64(sampleLen))
}

// MachMetricsEnv bind an OpenTelemetry tracer to [mach], based on environment
// variables:
// - AM_SERVICE (required)
// - AM_PROM_PUSH_URL (required)
//
// This tracer is inherited by submachines, and this function applies only to
// top-level machines.
func MachMetricsEnv(mach am.Api) *Metrics {

	if mach.ParentId() != "" {
		return nil
	}

	promPushUrl := os.Getenv(EnvPromPushUrl)
	promService := os.Getenv(telemetry.EnvService)

	// return early if any required variables are empty
	if promPushUrl == "" || promService == "" {
		return nil
	}

	// bind metrics via a pusher
	// TODO global ticker
	p := push.New(promPushUrl, telemetry.NormalizeId(promService))

	// bind transition to metrics
	m := BindMach(mach)
	BindToPusher(m, p)
	// TODO config
	t := time.NewTicker(15 * time.Second)
	go func() {
		for {

			select {
			// sync every 15 secs
			case <-t.C:
				m.Sync()
				err := p.Push()
				if err != nil {
					mach.AddErr(err, nil)
				}

			case <-mach.Ctx().Done():
				// pass
			}
		}
	}()

	return m
}
