package relay

import (
	"context"
	"errors"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/tools/relay/types"
)

// startArpcToDbg connects an aRPC client to the server and proxies the client's
// NetMach telemetry to am-dbg. The client is created without a local schema
// definition, so the remote schema (including relations) is synced from the
// aRPC server during the hello handshake.
func (r *Relay) startArpcToDbg(
	ctx context.Context, e *am.Event, cmd *types.ArgsArpcToDbg,
) {
	r.out("connecting to aRPC %s, telemetry to %s\n", cmd.RpcAddr, cmd.DbgAddr)

	// no local schema, the remote one is synced via the hello handshake
	client, err := arpc.NewClient(ctx, cmd.RpcAddr, "arpc-to-dbg", nil,
		&arpc.ClientOpts{
			Parent: r.Mach,
			// prevent env-based auto-attach to AM_DBG_ADDR
			DebugDisable: true,
		})
	if err != nil {
		r.Mach.EvAddErr(e, err, nil)
		return
	}
	r.arpcClient = client

	client.Start(nil)

	r.Mach.Go(ctx, func() {
		select {
		case <-client.Mach.When1(ssrpc.ClientStates.Ready, ctx):
			if client.Mach.Not1(ssrpc.ClientStates.Ready) {
				return // expired
			}
		case <-client.Mach.When1(ssrpc.ClientStates.ConnRetryFailed, ctx):
			err := errors.New("aRPC connection failed")
			r.Mach.EvAddErr(e, err, nil)
			r.Mach.EvRemove1(e, ssR.Start, nil)
			return
		case <-ctx.Done():
			return
		}

		// Attach after the handshake, when the machines are idle: the NetMach
		// state names and schema are only populated then, and the dbg tracer
		// sends the schema once on MachineInit. This way am-dbg receives the
		// remote schema with rels. Attaching earlier would race with the
		// machines' queue processing.
		if client.NetMach == nil {
			r.Mach.EvAddErr(e, errors.New("aRPC netmach missing"), nil)
			return
		}
		logLvl := am.LogOps
		for _, mach := range []am.Api{r.Mach, client.Mach, client.NetMach} {
			if err := amhelp.MachDebug(mach, cmd.DbgAddr, logLvl, false,
				amhelp.SemConfigEnv(true)); err != nil {
				r.Mach.EvAddErr(e, err, nil)
				return
			}
		}
		r.out("connected, streaming telemetry to %s\n", cmd.DbgAddr)
	})
}
