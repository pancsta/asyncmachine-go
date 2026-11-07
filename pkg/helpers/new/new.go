package new

import (
	"context"
	"fmt"

	"github.com/oleiade/reflections"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhist "github.com/pancsta/asyncmachine-go/pkg/history"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"
	amprom "github.com/pancsta/asyncmachine-go/pkg/telemetry/prometheus"
	amgen "github.com/pancsta/asyncmachine-go/tools/generator"
)

// export common types
type (
	S       = am.S
	A       = am.A
	Schema  = am.Schema
	Event   = am.Event
	Machine = am.Machine
)

// FullMachParams contains the parameters for [FullMach].
type FullMachParams[GStates am.States, GGroups any] struct {
	Ctx         context.Context
	Id          string
	Schema      am.Schema
	States      GStates
	Groups      GGroups
	Handlers    any
	Parent      am.Api
	MachineOpts *am.Opts
	Rpc         bool
	RpcAddr     string
	RpcOpts     *arpc.MuxOpts
	Repl        bool
	ReplOpts    *arpc.ReplOpts
	History     bool
	HistoryOpts amhist.BaseConfig
	Tracer      *am.Tracer
}

type FullMachRet struct {
	Mach *am.Machine
	Rpc  *arpc.Mux
}

// FullMach initializes a new machine with the supplied options.
func FullMach[GStates am.States, GGroups any](
	p FullMachParams[GStates, GGroups],
) (*FullMachRet, error) {

	// required states
	if !p.Schema.Implements(ssam.DisposedSchema) {
		return nil, fmt.Errorf("schema does not implement disposed states")
	}
	if !p.Schema.Implements(ssam.BasicSchema) {
		return nil, fmt.Errorf("schema does not implement basic states")
	}

	// init handlers
	if err := reflections.SetField(
		p.Handlers, ssam.DisposedStructField, &ssam.DisposedHandlers{},
	); err != nil {
		return nil, fmt.Errorf("initialize handlers %s field: %w",
			ssam.DisposedStructField, err)
	}

	mach, err := am.NewCommon(p.Ctx, p.Id, p.Schema, p.States.Names(),
		p.Handlers, p.Parent, p.MachineOpts)
	if err != nil {
		return nil, err
	}
	ret := &FullMachRet{
		Mach: mach,
	}

	// telemetry

	// inject groups and infer parents tree
	mach.SetGroups(p.Groups, p.States)
	mach.SemLogger().SetLevel(am.LogChanges)
	mach.SemLogger().SetArgsMapper(amhelp.LogArgsMapper)
	// connect to am-dbg
	if err = amhelp.MachDebugEnv(mach); err != nil {
		return nil, err
	}
	if p.Repl {
		// start a dedicated aRPC server for the REPL, create an addr file
		err, _ = arpc.MachReplEnv(mach, p.ReplOpts)
		if err != nil {
			return nil, err
		}
	}

	if p.Rpc {
		ret.Rpc, err = arpc.NewMux(mach.Context(), p.RpcAddr, p.Id, mach, p.RpcOpts)
		if err != nil {
			mach.AddErr(err, nil)
		}
	}

	// parent-only exporters

	// export metrics to prometheus
	amprom.MachMetricsEnv(mach)

	// grafana dashboard
	err = amgen.MachDashboardEnv(mach)
	if err != nil {
		mach.AddErr(err, nil)
	}

	// open telemetry traces
	if err = amtele.MachBindOtelEnv(mach); err != nil {
		mach.AddErr(err, nil)
	}

	if p.History {
		_, err = amhist.NewMemory(
			mach.Context(), nil, mach, p.HistoryOpts, func(err error) {
				mach.AddErr(err, nil)
			})
		if err != nil {
			return nil, err
		}
	}

	return ret, mach.Err()
}

// DynMach initializes a new machine with dynamic handlers (map-based).
// Useful for prototyping or when declaring handlers isn't feasible.
func DynMach(
	ctx context.Context, schema am.Schema,
	negs map[string]am.HandlerNegotiation,
	finals map[string]am.HandlerFinal, state S,
) (*am.Machine, error) {
	mach := am.New(ctx, schema, nil)
	_, err := mach.HandlersBindMaps(negs, finals)
	if err != nil {
		return nil, err
	}
	if len(state) > 0 {
		mach.Set(state, nil)
	}

	return mach, err
}
