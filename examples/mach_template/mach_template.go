package main

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
	amprom "github.com/pancsta/asyncmachine-go/pkg/telemetry/prometheus"
	amgen "github.com/pancsta/asyncmachine-go/tools/generator"
	"golang.org/x/sync/errgroup"

	"github.com/pancsta/asyncmachine-go/examples/mach_template/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
)

var ss = states.MachTemplateStates

type S = am.S

func init() {
	// am-dbg is required for debugging, go run it
	// go run github.com/pancsta/asyncmachine-go/tools/cmd/am-dbg@latest
	amhelp.EnableDebugging(true)
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// init
	mach, err := NewTemplate(ctx, 0)
	if err != nil {
		panic(err)
	}

	// add Start and Bar
	mach.Add(S{ss.Start, ss.Bar}, nil)

	// getting data back via a channel (needs to be buffered)
	ch := make(chan []string, 1)
	mach.Add1(ss.Channel, Pass(&A{
		ReturnCh: ch,
	}))
	fmt.Printf("%v", <-ch)

	bazDone := make(chan struct{})
	go func() {
		// wait for 100 BazDone
		done := mach.WhenTicks(ss.BazDone, 100, nil)

		for range 100 {
			mach.Add1(ss.Baz, Pass(&A{
				Addr: "localhost:8090",
			}))
		}
		<-done
		close(bazDone)
	}()

	// wait until Bar goes away
	<-mach.WhenNot1(ss.Bar, nil)

	// atomic dispose
	mach.Add1(ss.Disposing, nil)
	<-mach.When1(ss.Disposed, nil)

	// release resources
	mach.Dispose()
	<-mach.WhenDisposed()
}

// ///// ///// /////

// ///// STATE MACHINE

// ///// ///// /////

func NewTemplate(ctx context.Context, num int) (*am.Machine, error) {
	// init
	// use the schema from ./states
	// any struct can be used as handlers

	handlers := &TemplateHandlers{
		p:    amhelp.Pool(10),
		bazP: amhelp.Pool(1),
	}
	mach, err := am.NewCommon(ctx, "templ", states.MachTemplateSchema, ss.Names(),
		handlers, nil, &am.Opts{Tags: []string{"tag:val", "tag2"}})
	if err != nil {
		return nil, err
	}
	handlers.Mach = mach

	// inject groups and infer parents tree
	mach.SetGroups(states.MachTemplateGroups, states.MachTemplateStates)

	// telemetry

	mach.SemLogger().SetLevel(am.LogChanges)
	mach.SemLogger().SetArgsMapper(LogArgs)
	amhelp.MachDebugEnv(mach)
	// start a dedicated aRPC server for the REPL, create an addr file
	arpc.MachReplEnv(mach)

	// parent-only exporters

	// export metrics to prometheus
	amprom.MachMetricsEnv(mach)

	// grafana dashboard
	err = amgen.MachDashboardEnv(mach)
	if err != nil {
		mach.AddErr(err, nil)
	}

	// open telemetry traces
	err = amtele.MachBindOtelEnv(mach)
	if err != nil {
		mach.AddErr(err, nil)
	}

	// manual tracing

	tracer := &Tracer{}
	err = mach.BindTracer(tracer)
	if err != nil {
		return nil, err
	}

	// TODO history

	return mach, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

type TemplateHandlers struct {
	*am.ExceptionHandler
	Mach *am.Machine

	// general p for handlers
	p *errgroup.Group
	// multi states should be rate-limitted separately
	bazP *errgroup.Group
}

func (h *TemplateHandlers) FooState(e *am.Event) {
	ctx := h.Mach.NewStateCtx(ss.Bar)

	// unblock
	h.p.Go(func() error {
		if ctx.Err() != nil {
			return nil // expired
		}
		fmt.Println("FooState")
		return nil
	})
}

func (h *TemplateHandlers) BarExit(e *am.Event) bool {
	// accept de-activation only if Baz happened 10x more
	return h.Mach.Tick(ss.Baz) > h.Mach.Tick(ss.Bar)*10
}

func (h *TemplateHandlers) BazState(e *am.Event) {
	args := ParseArgs(e.Args)
	addr := args.Addr

	// multi states rely on context of other states
	ctx := h.Mach.NewStateCtx(ss.Start)

	// multi states should be rate-limitted
	h.bazP.Go(func() error {
		// like time.Wait, but with context
		if !amhelp.Wait(ctx, time.Second) {
			_ = AddErrExample(e, h.Mach, nil, nil)
			return nil
		}
		fmt.Println("BazState: " + addr)
		h.Mach.Add1(ss.BazDone, nil)

		return nil
	})
}

func (h *TemplateHandlers) BazDoneState(e *am.Event) {
	// new transition (will probably be canceled)
	h.Mach.Remove1(ss.Bar, nil)
}

func (h *TemplateHandlers) ChannelEnter(e *am.Event) bool {
	args := ParseArgs(e.Args)
	// only buffered channel can pass
	return args != nil && cap(args.ReturnCh) > 0
}

func (h *TemplateHandlers) ChannelState(e *am.Event) {
	// no validation needed
	ParseArgs(e.Args).ReturnCh <- []string{"hello", "machines"}
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////
// TODO add RPC args example from pkg/node

const APrefix = "template"

// A is a struct for node arguments. It's a typesafe alternative to [am.A].
type A struct {
	Id   string `log:"id"`
	Addr string `log:"addr"`

	// non-rpc fields

	ReturnCh chan<- []string
}

// ARpc is a subset of A, that can be passed over RPC.
type ARpc struct {
	Id   string `log:"id"`
	Addr string `log:"addr"`
}

// ParseArgs extracts A from [am.Event.Args][APrefix].
func ParseArgs(args am.A) *A {
	if r, ok := args[APrefix].(*ARpc); ok {
		return amhelp.ArgsToArgs(r, &A{})
	} else if r, ok := args[APrefix].(ARpc); ok {
		return amhelp.ArgsToArgs(&r, &A{})
	}
	if a, _ := args[APrefix].(*A); a != nil {
		return a
	}
	return &A{}
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	return am.A{APrefix: args}
}

// PassRpc prepares [am.A] from A to pass over RPC.
func PassRpc(args *ARpc) am.A {
	return am.A{APrefix: amhelp.ArgsToArgs(args, &ARpc{})}
}

// LogArgs is an args logger for A.
func LogArgs(args am.A) map[string]string {
	a := ParseArgs(args)
	if a == nil {
		return nil
	}

	return amhelp.ArgsToLogMap(a, 0)
}

// ///// ///// /////

// ///// ERRORS

// ///// ///// /////

var ErrExample = errors.New("error example")

// error mutations

// AddErrExample wraps an error in the ErrJoining sentinel and adds to a
// machine.
func AddErrExample(
	event *am.Event, mach *am.Machine, err error, args am.A,
) error {
	if err != nil {
		err = fmt.Errorf("%w: %w", ErrExample, err)
	} else {
		err = ErrExample
	}
	mach.EvAddErrState(event, ss.ErrExample, err, args)

	return err
}

// ///// ///// /////

// ///// TRACER

// ///// ///// /////

type Tracer struct {
	*am.TracerNoOp
	// This machine's clock has been updated and needs to be synced.
	dirty atomic.Bool
}

// TransitionEnd sends a message when a transition ends
func (t *Tracer) TransitionEnd(transition *am.Transition) {
	t.dirty.Store(true)
}
