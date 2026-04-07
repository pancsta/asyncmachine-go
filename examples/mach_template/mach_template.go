package main

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/pancsta/asyncmachine-go/examples/mach_template/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	amss "github.com/pancsta/asyncmachine-go/pkg/states"
	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
	amprom "github.com/pancsta/asyncmachine-go/pkg/telemetry/prometheus"
	amgen "github.com/pancsta/asyncmachine-go/tools/generator"
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

	// wait until Bar deactivates
	<-mach.WhenNot1(ss.Bar, nil)

	// async dispose
	mach.Add1(ss.Disposing, nil)
	<-mach.When1(ss.Disposed, nil)
	// soft dispose done
	<-mach.WhenDisposed()
	// fully disposed
}

// ///// ///// /////

// ///// STATE MACHINE

// ///// ///// /////

func NewTemplate(ctx context.Context, num int) (*am.Machine, error) {
	// init
	// use the schema from ./states
	// any struct can be used as handlers

	handlers := &TemplateHandlers{
		DisposedHandlers: &amss.DisposedHandlers{},
	}
	mach, err := am.NewCommon(ctx, "templ", states.MachTemplateSchema, ss.Names(),
		handlers, nil, &am.Opts{Tags: []string{"tag:val", "tag2"}})
	if err != nil {
		return nil, err
	}
	handlers.Mach = mach
	// max 10 concurrent forks in BazState
	mach.PoolSetLimit(ss.Baz+am.SuffixState, 10)

	// telemetry

	// inject groups and infer parents tree
	mach.SetGroups(states.MachTemplateGroups, states.MachTemplateStates)

	mach.SemLogger().SetLevel(am.LogChanges)
	mach.SemLogger().SetArgsMapper(LogArgs)
	// connect to am-dbg
	amhelp.MachDebugEnv(mach)
	// start a dedicated aRPC server for the REPL, create an addr file
	arpc.MachReplEnv(mach, &arpc.ReplOpts{
		AddrDir:  ".",
		Args:     ARpc{},
		ParseRpc: ParseRpc,
	})

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
	*amss.DisposedHandlers

	Mach *am.Machine
}

func (h *TemplateHandlers) FooState(e *am.Event) {
	ctx := h.Mach.NewStateCtx(ss.Bar)

	// unblock
	h.Mach.Fork(ctx, e, func() {
		fmt.Println("FooState")

		// nested unblocking goes without [e], which is not valid at this point
		h.Mach.Go(ctx, func() {
			fmt.Println("FooState.Go")
		})
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

	// very frequent multi-states should be rate-limited
	h.Mach.PoolFork(ctx, e, func() {
		// like time.Wait, but with context
		if !amhelp.Wait(ctx, time.Second) {
			_ = AddErrExample(e, h.Mach, nil, nil)
			return
		}
		fmt.Println("BazState: " + addr)
		h.Mach.Add1(ss.BazDone, nil)
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

func init() {
	gob.Register(ARpc{})
}

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
func PassRpc(args *A) am.A {
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

// ParseRpc parses [am.A] to *ARpc namespaced in [am.A]. Useful for REPLs.
func ParseRpc(args am.A) am.A {
	ret := am.A{APrefix: &ARpc{}}
	jsonArgs, err := json.Marshal(args)
	if err == nil {
		json.Unmarshal(jsonArgs, ret[APrefix])
	}

	return ret
}

// ///// ///// /////

// ///// ERRORS

// ///// ///// /////

var ErrExample = errors.New("error example")

// error mutations

// AddErrExample wraps an error in the ErrJoining sentinel and adds to a
// machine. If no err was provided, the function is no-op.
func AddErrExample(
	event *am.Event, mach *am.Machine, err error, args am.A,
) am.Result {
	if err == nil {
		return am.Executed
	}
	err = fmt.Errorf("%w: %w", ErrExample, err)

	return mach.EvAddErrState(event, ss.ErrExample, err, args)
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
