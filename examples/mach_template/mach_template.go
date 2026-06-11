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
	// load dotenv
	// err := godotenv.Load("./examples/mach_template/example.env")
	// if err != nil {
	// 	panic(err)
	// }

	// manual logging
	// amhelp.SetEnvLogLevel(am.LogOps)
	// os.Setenv(amhelp.EnvAmLogPrint, "2")

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
	mach.Add1(ss.Channel, am.Pass(&AChannel{
		ReturnCh: ch,
	}))
	fmt.Printf("%v\n", <-ch)

	go func() {
		// wait for 100 BazDone
		done := mach.WhenTicks(ss.BazDone, 100, nil)

		for range 100 {
			mach.Add1(ss.Baz, am.Pass(&ABaz{
				Addr: "localhost:8090",
			}))
		}
		<-done
	}()

	// wait until Bar deactivates
	<-mach.WhenNot1(ss.Bar, nil)

	// async dispose
	mach.Add1(ss.Disposing, nil)
	<-mach.When1(ss.Disposed, nil)
	// soft dispose done
	<-mach.WhenDisposed()
	fmt.Printf("done")
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

var _ = ss.Foo

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

var _ = ss.Bar

func (h *TemplateHandlers) BarExit(e *am.Event) bool {
	// accept de-activation only if Baz happened 10x more
	return h.Mach.Tick(ss.Baz) > h.Mach.Tick(ss.Bar)*10
}

var _ = ss.Baz

func (h *TemplateHandlers) BazState(e *am.Event) {
	args := am.ParseArgs[ABaz](e.Args)
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
		// traced mutation
		h.Mach.EvAdd1(e, ss.BazDone, nil)
	})
}

var _ = ss.BazDone

func (h *TemplateHandlers) BazDoneState(e *am.Event) {
	// new transition (will probably be canceled)
	// traced mutation
	h.Mach.EvRemove1(e, ss.Bar, nil)
}

var _ = ss.Channel

func (h *TemplateHandlers) ChannelEnter(e *am.Event) bool {
	args := am.ParseArgs[AChannel](e.Args)
	// only buffered channel can pass
	return args != nil && cap(args.ReturnCh) > 0
}

var _ = ss.Channel

func (h *TemplateHandlers) ChannelState(e *am.Event) {
	// no validation needed
	am.ParseArgs[AChannel](e.Args).ReturnCh <- []string{"hello", "machines"}
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "template"

// Args is shared pkg args for Any state
type Args struct {
	am.ArgsBase `json:"-"`
}

func (Args) ArgsPrefix() string {
	return APrefix
}

// -----

type AChannel struct {
	Args `json:"-"`

	// Return chan.
	ReturnCh chan<- []string
}

func (AChannel) ArgsState() string {
	return ss.Channel
}

// -----

type ABaz struct {
	Args `json:"-"`

	// Address with logging.
	Addr string `log:"addr"`
}

func (ABaz) ArgsState() string {
	return ss.Baz
}

// ----- RPC

func init() {
	for _, arg := range ArgsRpc {
		gob.Register(arg)
	}
}

// ArgsRpc will be available in the REPL.
var ArgsRpc = []am.ArgsApi{ABaz{}}

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
