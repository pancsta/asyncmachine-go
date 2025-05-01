package telemetry

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	olog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/propagation"
	ologsdk "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.25.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/pancsta/asyncmachine-go/internal/utils"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

// TODO config
const maxHist = 300

type OtelMachineData struct {
	ID string
	// Index is a unique number for this machine withing the Otel tracer.
	Index int
	Ended bool

	mx        sync.Mutex
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
	// history of recent transitions
	txHist []OtelTxHist
}

type OtelTxHist struct {
	Id  string
	Ctx context.Context
}

type OtelMachTracerOpts struct {
	// if true, only state changes will be traced
	SkipTransitions bool
	// if true, only Healthcheck and Heartbeat will be skipped
	IncludeHealth bool
	// if true, transition traces won't include [am.Machine.GetLogArgs]
	SkipLogArgs bool
	// if true, auto transitions won't be traced
	SkipAuto bool

	// TODO skipping empty and canceled txs requires a custom Processor to
	//  discard an open span
	// SkipCanceled bool
	// SkipEmpty    bool

	Logf func(format string, args ...any)
}

// OtelMachTracer implements machine.Tracer for OpenTelemetry. Supports tracing
// of multiple state machines, resulting in a single trace. This tracer is
// automatically bound to new sub-machines.
type OtelMachTracer struct {
	*am.NoOpTracer

	Tracer        trace.Tracer
	Machines      map[string]*OtelMachineData
	MachinesMx    sync.Mutex
	MachinesOrder []string
	RootSpan      trace.Span

	// TODO bind to env var
	Logf func(format string, args ...any)
	// map of parent Span for each submachine
	parentSpans map[string]trace.Span
	// child-parent map, used for parentSpans
	parents map[string]string

	opts      *OtelMachTracerOpts
	ended     bool
	NextIndex int
}

var _ am.Tracer = (*OtelMachTracer)(nil)

// NewOtelMachTracer creates a new machine tracer from an OpenTelemetry tracer.
// Requires OtelMachTracer.Dispose to be called at the end.
func NewOtelMachTracer(
	rootMach am.Api, rootSpan trace.Span, otelTracer trace.Tracer,
	opts *OtelMachTracerOpts,
) *OtelMachTracer {
	if otelTracer == nil {
		panic("nil tracer")
	}
	if opts == nil {
		opts = &OtelMachTracerOpts{}
	}

	mt := &OtelMachTracer{
		Tracer:   otelTracer,
		Machines: make(map[string]*OtelMachineData),
		RootSpan: rootSpan,

		opts:        opts,
		parentSpans: make(map[string]trace.Span),
		parents:     make(map[string]string),
	}

	mt.RootSpan.End()

	if opts.Logf != nil {
		mt.Logf = opts.Logf
	} else if am.EnvLogLevel("") >= am.LogDecisions {
		mt.Logf = rootMach.Log
	} else {
		mt.Logf = func(format string, args ...any) {}
	}

	return mt
}

func (mt *OtelMachTracer) getMachineData(id string) *OtelMachineData {
	mt.MachinesMx.Lock()
	defer mt.MachinesMx.Unlock()

	if data, ok := mt.Machines[id]; ok {
		return data
	}
	data := &OtelMachineData{
		stateInstances: make(map[string]context.Context),
		stateNames:     make(map[string]context.Context),
	}
	if !mt.ended {
		mt.Logf("[otel] getMachineData: creating for %s", id)
		mt.Machines[id] = data
		mt.MachinesOrder = append(mt.MachinesOrder, id)
	}
	return data
}

func (mt *OtelMachTracer) MachineInit(mach am.Api) context.Context {
	id := mach.Id()
	index := mt.NextIndex
	mt.NextIndex++
	name := "mach:" + strconv.Itoa(index) + ":" + id
	mach.Log("[bind] otel traces")
	mt.Logf("[otel] MachineInit: trace %s", id)

	// nest under parent
	ctx := mach.Ctx()
	pid, ok := mt.parents[id]
	if ok {
		if parentSpan, ok := mt.parentSpans[pid]; ok {
			ctx = trace.ContextWithSpan(ctx, parentSpan)
		}
	} else {
		ctx = trace.ContextWithSpan(ctx, mt.RootSpan)
	}

	// create a machine trace
	machCtx, machSpan := mt.Tracer.Start(ctx, name, trace.WithAttributes(
		attribute.String("id", id),
	))

	// create a machine trace
	data := mt.getMachineData(id)
	data.machTrace = machCtx
	data.ID = id
	data.Index = index
	machSpan.End()

	// create a group span for states
	stateGroupCtx, stateGroupSpan := mt.Tracer.Start(machCtx,
		strconv.Itoa(index)+":states",
		trace.WithAttributes(attribute.String("mach_id", id)))
	data.stateGroup = stateGroupCtx
	// groups are only for nesting, so end it right away
	stateGroupSpan.End()

	if !mt.opts.SkipTransitions {
		// create a group span for transitions
		txGroupCtx, txGroupSpan := mt.Tracer.Start(machCtx,
			strconv.Itoa(index)+":transitions",
			trace.WithAttributes(attribute.String("mach_id", id)))
		data.txGroup = txGroupCtx
		// groups are only for nesting, so end it right away
		txGroupSpan.End()
	}

	return machCtx
}

// NewSubmachine links 2 machines with a parent-child relationship.
func (mt *OtelMachTracer) NewSubmachine(parent, mach am.Api) {
	if mt.ended {
		mt.Logf("[otel] NewSubmachine: tracer already ended, ignoring %s",
			mach.Id())
		return
	}

	// skip RPC machines
	dbgRpc := os.Getenv("AM_RPC_DBG") != ""
	for _, tag := range mach.Tags() {
		if strings.HasPrefix(tag, "rpc-") && !dbgRpc {
			return
		}
	}

	err := mach.BindTracer(mt)
	if err != nil {
		mt.Logf("[otel] NewSubmachine: err binding tracer", mach.Id())
		return
	}

	_, ok := mt.parents[mach.Id()]
	if ok {
		mt.Logf("Submachine already being traced (duplicate ID %s)", mach.Id())
		return
	}
	mt.parents[mach.Id()] = parent.Id()
	data := mt.getMachineData(parent.Id())
	data.mx.Lock()
	defer data.mx.Unlock()

	if data.Ended {
		mt.Logf("[otel] NewSubmachine: parent %s already ended", parent.Id())
		return
	}

	if _, ok := mt.parentSpans[parent.Id()]; !ok {
		// create a group span for submachines
		_, submachGroupSpan := mt.Tracer.Start(
			data.machTrace, strconv.Itoa(data.Index)+":submachines",
			trace.WithAttributes(attribute.String("mach_id", parent.Id())))
		mt.parentSpans[parent.Id()] = submachGroupSpan
		// groups are only for nesting, so end it right away
		submachGroupSpan.End()
	}
}

func (mt *OtelMachTracer) MachineDispose(id string) {
	mt.MachinesMx.Lock()
	defer mt.MachinesMx.Unlock()

	mt.doDispose(id)
}

func (mt *OtelMachTracer) doDispose(id string) {
	data, ok := mt.Machines[id]
	if !ok {
		mt.Logf("[otel] MachineDispose: machine %s not found", id)
		return
	}
	mt.Logf("[otel] MachineDispose: disposing %s", id)
	data.mx.Lock()

	delete(mt.parentSpans, id)
	delete(mt.Machines, id)
	mt.MachinesOrder = utils.SlicesWithout(mt.MachinesOrder, id)
	data.Ended = true

	// transitions
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
	data.mx.Unlock()
}

func (mt *OtelMachTracer) TransitionInit(tx *am.Transition) {
	if mt.ended {
		mt.Logf("[otel] TransitionInit: tracer already ended, ignoring %s",
			tx.Machine.Id())
		return
	}

	// skip health txs
	called := tx.CalledStates()
	isHealth := slices.Contains(called, "Healthcheck") ||
		slices.Contains(called, "Heartbeat")
	if !mt.opts.IncludeHealth && isHealth {
		return
	}

	data := mt.getMachineData(tx.Machine.Id())
	if data.Ended {
		mt.Logf("[otel] TransitionInit: machine %s already ended", tx.Machine.Id())
		return
	}

	// if skipping transitions, only create machine data for the states trace
	if mt.opts.SkipTransitions {
		return
	}
	if mt.opts.SkipAuto && tx.IsAuto() {
		return
	}

	// source event
	src := tx.Mutation.Source
	var srcSpan trace.Span
	if src != nil && src.TxId != "" && src.MachId != "" {
		// TODO get span from that tx (GC via LRU) and link to it
		if srcData, ok := mt.Machines[src.MachId]; ok {
			// TODO optimize with an index
			for _, srcTx := range srcData.txHist {
				if srcTx.Id == src.TxId {
					srcSpan = trace.SpanFromContext(srcTx.Ctx)
					break
				}
			}
		}
	}

	// label
	mutLabel := fmt.Sprintf("%d: %s", data.Index, tx.Mutation)
	name := mutLabel

	// exception support
	var errAttr error
	if slices.Contains(tx.TargetStates(), am.Exception) {
		name = "!" + name
		errAttr = am.ParseArgs(tx.Args()).Err
	}

	// build a regular trace
	ctx, span := mt.Tracer.Start(data.txGroup, name, trace.WithAttributes(
		attribute.String("tx_id", tx.ID),
		attribute.Int64("time_before", int64(tx.TimeBefore.Sum())),
		attribute.String("mutation", mutLabel),
	))

	// decorate Exception trace
	if errAttr != nil {
		span.SetAttributes(
			attribute.String("error", errAttr.Error()),
		)
	}

	// trace logged args, if any and enabled
	argsMatcher := tx.Machine.GetLogArgs()
	if !mt.opts.SkipLogArgs && argsMatcher != nil {
		for param, val := range argsMatcher(tx.Args()) {
			span.SetAttributes(
				attribute.String("args."+param, val),
			)
		}
	}

	if srcSpan != nil {
		span.AddLink(trace.Link{
			SpanContext: srcSpan.SpanContext(),
			Attributes:  nil,
		})
	}

	// expose
	data.txTrace = ctx
	data.txHist = append(data.txHist, OtelTxHist{
		Id:  tx.ID,
		Ctx: ctx,
	})
	if len(data.txHist) > maxHist {
		data.txHist = data.txHist[1:]
	}
}

func (mt *OtelMachTracer) TransitionEnd(tx *am.Transition) {
	if mt.ended {
		mt.Logf("[otel] TransitionEnd: tracer already ended, ignoring %s",
			tx.Machine.Id())
		return
	}

	// skip health txs
	target := tx.TargetStates()
	called := tx.CalledStates()
	isHealth := slices.Contains(called, "Healthcheck") ||
		slices.Contains(called, "Heartbeat")
	if !mt.opts.IncludeHealth && isHealth {
		return
	}

	data := mt.getMachineData(tx.Machine.Id())
	if data.Ended {
		mt.Logf("[otel] TransitionEnd: machine %s already ended", tx.Machine.Id())
		return
	}
	data.mx.Lock()
	defer data.mx.Unlock()

	// parse states collected from resolving relations
	statesAdded := am.DiffStates(target, tx.StatesBefore())
	statesRemoved := am.DiffStates(tx.StatesBefore(), target)

	// support multi states
	before := tx.ClockBefore()
	for name, tick := range tx.ClockAfter() {
		if tick > 1+before[name] && !slices.Contains(statesAdded, name) {
			statesAdded = append(statesAdded, name)
		}
	}

	var (
		// handle transition
		txSpan     trace.Span
		statesDiff string
	)
	if !mt.opts.SkipTransitions && data.txTrace != nil {
		if len(statesAdded) > 0 {
			statesDiff += "+" + utils.Jw(statesAdded, " +")
		}
		if len(statesRemoved) > 0 {
			statesDiff += " -" + utils.Jw(statesRemoved, " -")
		}

		txSpan = trace.SpanFromContext(data.txTrace)
		txSpan.SetAttributes(
			attribute.String("states_diff", strings.Trim(statesDiff, " ")),
			attribute.Int64("time_after", int64(tx.TimeAfter.Sum())),
			attribute.Bool("accepted", tx.IsAccepted()),
			attribute.Bool("auto", tx.IsAuto()),
			attribute.Int("steps_count", len(tx.Steps)),
		)

		// link to old states
		for _, state := range statesRemoved {
			if ctx, ok := data.stateInstances[state]; ok {
				txSpan.AddLink(trace.Link{
					SpanContext: trace.SpanFromContext(ctx).SpanContext(),
					Attributes:  nil,
				})
			}
		}

		defer txSpan.End()
		data.txTrace = nil
	}

	// handle state changes
	if !tx.IsAccepted() {
		return
	}

	// remove old states
	for _, state := range statesRemoved {
		if ctx, ok := data.stateInstances[state]; ok {
			trace.SpanFromContext(ctx).End()
			delete(data.stateInstances, state)
		}
	}

	// add a new state trace with a group if needed
	for _, state := range statesAdded {
		if data.Ended {
			mt.Logf("[otel] TransitionEnd: machine %s already ended", tx.Machine.Id())
			break
		}

		// name group
		nameCtx, ok := data.stateNames[state]
		if !ok {
			// create a new state name group trace, but end it right away
			ctx, span := mt.Tracer.Start(data.stateGroup,
				strconv.Itoa(data.Index)+":"+state)
			nameCtx = ctx
			data.stateNames[state] = nameCtx
			span.End()
		}

		// multi state - add as an event
		_, ok = data.stateInstances[state]
		var ctx context.Context
		var instanceSpan trace.Span
		if ok {
			instanceSpan = trace.SpanFromContext(data.stateInstances[state])
			instanceSpan.AddEvent(tx.Mutation.String())
		} else {
			ctx, instanceSpan = mt.Tracer.Start(nameCtx,
				strconv.Itoa(data.Index)+":"+state, trace.WithAttributes(
					attribute.String("tx_id", tx.ID),
				))
			data.stateInstances[state] = ctx
			instanceSpan.AddEvent(tx.Mutation.String())
		}

		// link with the source tx
		if txSpan != nil {
			txSpan.AddLink(trace.Link{
				SpanContext: instanceSpan.SpanContext(),
				Attributes:  nil,
			})
		}
	}
}

func (mt *OtelMachTracer) HandlerEnd(
	tx *am.Transition, emitter string, handler string,
) {
	if mt.ended {
		return
	}
	if mt.opts.SkipTransitions {
		return
	}
	data := mt.getMachineData(tx.Machine.Id())
	if data.Ended {
		return
	}

	// add an event to the tx trace
	trace.SpanFromContext(data.txTrace).AddEvent(handler, trace.WithAttributes(
		attribute.String("emitter", emitter),
	))
}

func (mt *OtelMachTracer) End() {
	mt.MachinesMx.Lock()
	defer mt.MachinesMx.Unlock()

	mt.Logf("[otel] End")
	mt.ended = true
	// end traces in reverse order
	slices.Reverse(mt.MachinesOrder)

	for _, id := range mt.MachinesOrder {
		mt.doDispose(id)
	}

	// TODO remove?
	mt.RootSpan.End()
	mt.Machines = nil
}

func (mt *OtelMachTracer) QueueEnd(mach am.Api) {}

// NewOtelLoggerProvider creates a new OpenTelemetry logger provider bound to
// the given exporter.
func NewOtelLoggerProvider(exporter ologsdk.Exporter) *ologsdk.LoggerProvider {
	// TODO mem limiter?
	provider := ologsdk.NewLoggerProvider(
		ologsdk.WithProcessor(ologsdk.NewBatchProcessor(exporter)),
	)

	return provider
}

// BindOtelLogger binds an OpenTelemetry logger to a machine.
func BindOtelLogger(
	mach am.Api, provider *ologsdk.LoggerProvider, service string,
) {
	l := provider.Logger(mach.Id())
	mach.SetLogId(false)

	amlog := func(level am.LogLevel, msg string, args ...any) {
		r := olog.Record{}
		r.SetTimestamp(time.Now())
		if strings.HasPrefix(msg, "[error") {
			r.SetSeverity(olog.SeverityError)
		} else {
			switch level {

			case am.LogChanges:
				r.SetSeverity(olog.SeverityInfo4)
				r.SetSeverityText(am.LogChanges.String())
			case am.LogOps:
				r.SetSeverity(olog.SeverityInfo1)
				r.SetSeverityText(am.LogOps.String())
			case am.LogDecisions:
				r.SetSeverity(olog.SeverityTrace4)
				r.SetSeverityText(am.LogDecisions.String())
			case am.LogEverything:
				r.SetSeverity(olog.SeverityTrace1)
				r.SetSeverityText(am.LogEverything.String())
			default:
			}
		}
		r.SetBody(olog.StringValue(fmt.Sprintf(msg, args...)))

		if service != "" {
			r.AddAttributes(
				olog.String("service.name", service),
			)
		}

		r.AddAttributes(
			olog.String("asyncmachine.id", mach.Id()),
		)

		l.Emit(mach.Ctx(), r)
	}

	mach.SetLogger(amlog)
}

// MachBindOtelEnv bind an OpenTelemetry tracer to [mach], based on environment
// variables:
// - AM_SERVICE (required)
// - AM_OTEL_TRACE (required)
// - AM_OTEL_TRACE_TXS
// - OTEL_EXPORTER_OTLP_ENDPOINT
// - OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
// This tracer is inherited by submachines.
func MachBindOtelEnv(mach am.Api) error {
	service := os.Getenv(EnvService)
	if os.Getenv(EnvOtelTrace) == "" || service == "" {
		return nil
	}

	// init the tracer and provider
	ctx := mach.Ctx()
	t, p, err := NewOtelProvider(service, ctx)
	if err != nil {
		return err
	}
	_, rootSpan := t.Start(mach.Ctx(), "root")

	// dedicated machine tracer
	mt := NewOtelMachTracer(mach, rootSpan, t, &OtelMachTracerOpts{
		SkipTransitions: os.Getenv(EnvOtelTraceTxs) == "",
		SkipLogArgs:     os.Getenv(EnvOtelTraceArgs) == "",

		SkipAuto: os.Getenv(EnvOtelTraceNoauto) != "",
	})

	// flush and close
	var dispose am.HandlerDispose = func(id string, _ context.Context) {
		tracerCooldown := 100 * time.Millisecond

		mt.End()
		time.Sleep(tracerCooldown)

		// flush tracing
		err = p.ForceFlush(ctx)
		if err != nil {
			log.Printf("Error flushing tracer: %v", err)
		}

		time.Sleep(tracerCooldown)

		// finish tracing
		if err := p.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down tracer: %v", err)
		}
	}

	// dispose somehow
	register := ssam.DisposedStates.RegisterDisposal
	if mach.Has1(register) {
		mach.Add1(register, am.A{
			ssam.DisposedArgHandler: dispose,
		})
	} else {
		func() {
			<-mach.WhenDisposed()
			dispose(mach.Id(), nil)
		}()
	}

	// bind the Otel tracer
	err = mach.BindTracer(mt)
	if err != nil {
		return err
	}

	// run the root init manually
	mt.MachineInit(mach)
	return nil
}

func NewOtelProvider(
	source string, ctx context.Context,
) (trace.Tracer, *sdktrace.TracerProvider, error) {
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

	exporter, err := otlptrace.New(ctx,
		otlptracegrpc.NewClient(
			otlptracegrpc.WithInsecure(),
		),
	)
	if err != nil {
		return nil, nil, err
	}

	serviceName := source
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithMaxExportBatchSize(50),
			sdktrace.WithBatchTimeout(100*time.Millisecond),
		),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Create a named tracer with package path as its name.
	return otel.Tracer(source), tp, nil
}
