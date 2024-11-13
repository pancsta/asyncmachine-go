package telemetry

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
	olog "go.opentelemetry.io/otel/log"
	ologsdk "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// OtelMachTracer implements machine.Tracer for OpenTelemetry.
// Support tracing of multiple state machines.
type OtelMachTracer struct {
	Tracer        trace.Tracer
	Machines      map[string]*OtelMachineData
	MachinesMx    sync.Mutex
	MachinesOrder []string
	// TODO bind to env var
	Logf func(format string, args ...any)
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
	// TODO env based logger
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

func (ot *OtelMachTracer) MachineInit(mach *am.Machine) context.Context {
	name := "mach:" + mach.Id()

	// nest under parent
	ctx := mach.Ctx()
	pid, ok := ot.parents[mach.Id()]
	if ok {
		if parentSpan, ok := ot.parentSpans[pid]; ok {
			ctx = trace.ContextWithSpan(ctx, parentSpan)
		}
	}

	// create a machine trace
	machCtx, _ := ot.Tracer.Start(ctx, name, trace.WithAttributes(
		attribute.String("id", mach.Id()),
	))

	// add tracing to machine's context
	ot.Logf("[otel] MachineInit: trace %s", mach.Id())
	data := ot.getMachineData(mach.Id())
	data.machTrace = machCtx

	data.ID = mach.Id()

	// create a group span for states
	stateGroupCtx, stateGroupSpan := ot.Tracer.Start(machCtx, "states",
		trace.WithAttributes(attribute.String("mach_id", mach.Id())))
	data.stateGroup = stateGroupCtx
	// groups are only for nesting, so end it right away
	stateGroupSpan.End()

	if !ot.opts.SkipTransitions {
		// create a group span for transitions
		txGroupCtx, txGroupSpan := ot.Tracer.Start(machCtx, "transitions",
			trace.WithAttributes(attribute.String("mach_id", mach.Id())))
		data.txGroup = txGroupCtx
		// groups are only for nesting, so end it right away
		txGroupSpan.End()
	}

	return machCtx
}

// NewSubmachine links 2 machines with a parent-child relationship.
func (ot *OtelMachTracer) NewSubmachine(parent, mach *am.Machine) {
	if ot.ended {
		ot.Logf("[otel] NewSubmachine: tracer already ended, ignoring %s",
			mach.Id())
		return
	}

	_, ok := ot.parents[mach.Id()]
	if ok {
		panic("Submachine already being traced (duplicate ID " + mach.Id() + ")")
	}
	ot.parents[mach.Id()] = parent.Id()
	data := ot.getMachineData(parent.Id())
	data.Lock.Lock()
	defer data.Lock.Unlock()

	if data.Ended {
		ot.Logf("[otel] NewSubmachine: parent %s already ended", parent.Id())
		return
	}

	if _, ok := ot.parentSpans[parent.Id()]; !ok {
		// create a group span for submachines
		_, submachGroupSpan := ot.Tracer.Start(
			data.machTrace, "submachines",
			trace.WithAttributes(attribute.String("mach_id", parent.Id())))
		ot.parentSpans[parent.Id()] = submachGroupSpan
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
			tx.Machine.Id())
		return
	}
	data := ot.getMachineData(tx.Machine.Id())
	if data.Ended {
		ot.Logf("[otel] TransitionInit: machine %s already ended", tx.Machine.Id())
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
	if lo.Contains(tx.TargetStates(), am.Exception) {
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
			tx.Machine.Id())
		return
	}
	data := ot.getMachineData(tx.Machine.Id())
	if data.Ended {
		ot.Logf("[otel] TransitionEnd: machine %s already ended", tx.Machine.Id())
		return
	}
	data.Lock.Lock()
	defer data.Lock.Unlock()

	statesAdded := am.DiffStates(tx.TargetStates(), tx.StatesBefore())
	statesRemoved := am.DiffStates(tx.StatesBefore(), tx.TargetStates())
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
			ot.Logf("[otel] TransitionEnd: machine %s already ended", tx.Machine.Id())
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
	data := ot.getMachineData(tx.Machine.Id())
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
	data := ot.getMachineData(tx.Machine.Id())
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
