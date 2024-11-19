package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/node/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	ampipe "github.com/pancsta/asyncmachine-go/pkg/states/pipes"
)

var ssC = states.ClientStates

// Client is a node client, connecting to a supervisor and then a worker.
type Client struct {
	*am.ExceptionHandler
	Mach *am.Machine

	Name       string
	SuperAddr  string
	LogEnabled bool
	// LeaveSuper is a flag to leave the supervisor after connecting to the
	// worker. TODO
	LeaveSuper bool
	// ConnTimeout is the time to wait for an outbound connection to be
	// established. Default is 5 seconds.
	ConnTimeout time.Duration

	// network

	SuperRpc  *rpc.Client
	WorkerRpc *rpc.Client

	// internal

	// current nodes list (set by Start)
	nodeList  []string
	stateDeps *ClientStateDeps
}

// implement ConsumerHandlers
var _ ssrpc.ConsumerHandlers = &Client{}

// NewClient creates a new Client instance with the provided context, id,
// workerKind, state dependencies, and options. Returns a pointer to the Client
// instance and an error if any validation or initialization fails.
func NewClient(ctx context.Context, id string, workerKind string,
	stateDeps *ClientStateDeps, opts *ClientOpts,
) (*Client, error) {
	// validate
	if id == "" {
		return nil, errors.New("client: workerStruct required")
	}
	if stateDeps == nil {
		return nil, errors.New("client: stateNames required")
	}
	if stateDeps.WorkerSStruct == nil {
		return nil, errors.New("client: workerStruct required")
	}
	if stateDeps.WorkerSNames == nil {
		return nil, errors.New("client: stateNames required")
	}
	if stateDeps.ClientSStruct == nil {
		return nil, errors.New("client: workerStruct required")
	}
	if stateDeps.ClientSNames == nil {
		return nil, errors.New("client: stateNames required")
	}
	if workerKind == "" {
		return nil, errors.New("client: workerKind required")
	}
	if opts == nil {
		opts = &ClientOpts{}
	}

	err := amhelp.Implements(stateDeps.WorkerSNames, states.WorkerStates.Names())
	if err != nil {
		err := fmt.Errorf(
			"worker has to implement am/node/states/WorkerStates: %w", err)
		return nil, err
	}

	name := fmt.Sprintf("%s-%s-%s", workerKind, id,
		time.Now().Format("150405"))

	c := &Client{
		Name:        name,
		ConnTimeout: 5 * time.Second,
		LogEnabled:  os.Getenv(EnvAmNodeLogClient) != "",
		stateDeps:   stateDeps,
	}
	if amhelp.IsDebug() {
		c.ConnTimeout = 10 * c.ConnTimeout
	}
	mach, err := am.NewCommon(ctx, "nc-"+name, stateDeps.ClientSStruct,
		stateDeps.ClientSNames, c, opts.Parent, nil)
	if err != nil {
		return nil, err
	}

	mach.SetLogArgs(LogArgs)
	c.Mach = mach
	amhelp.MachDebugEnv(mach)

	// check base states
	err = amhelp.Implements(mach.StateNames(), ssC.Names())
	if err != nil {
		err := fmt.Errorf(
			"client has to implement am/node/states/ClientStates: %w", err)
		return nil, err
	}

	return c, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (c *Client) StartEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && len(a.NodesList) > 0
}

func (c *Client) StartState(e *am.Event) {
	var err error
	ctx := c.Mach.NewStateCtx(ssC.Start)
	args := ParseArgs(e.Args)
	addr := args.NodesList[0]
	c.nodeList = args.NodesList

	// init super rpc (but dont connect just yet)
	c.SuperRpc, err = rpc.NewClient(ctx, addr, "nc-super-"+c.Name,
		states.SupervisorStruct, ssS.Names(), &rpc.ClientOpts{
			Parent:   c.Mach,
			Consumer: c.Mach,
		})
	if err != nil {
		err := fmt.Errorf("failed to connect to the Supervisor: %w", err)
		AddErrRpc(c.Mach, err, nil)
		return
	}
	amhelp.MachDebugEnv(c.SuperRpc.Mach)

	// bind to super rpc
	err = errors.Join(
		ampipe.BindConnected(c.SuperRpc.Mach, c.Mach, ssC.SuperDisconnected,
			ssC.SuperConnecting, ssC.SuperConnected, ssC.SuperDisconnecting),
		ampipe.BindErr(c.SuperRpc.Mach, c.Mach, ssC.ErrSupervisor),
		ampipe.BindReady(c.SuperRpc.Mach, c.Mach, ssC.SuperReady, ""),
	)
	if err != nil {
		c.Mach.AddErr(err, nil)
		return
	}

	// unblock
	go func() {
		// try all nodes TODO randomize
		for i, addr := range args.NodesList {
			if ctx.Err() != nil {
				return // expired
			}
			c.Mach.Log("trying node %d: %s", i, addr)

			// (re)start and wait
			// TODO handle in ExceptionState when SuperConnecting active
			c.Mach.Remove1(am.Exception, nil)
			c.SuperRpc.Addr = addr
			// fewer retries, bc of fallbacks
			// TODO config via a composable RetryPolicy from rpc-c
			c.SuperRpc.ConnRetries = 3
			c.SuperRpc.Start()
			err := amhelp.WaitForAny(ctx, c.ConnTimeout,
				c.Mach.When1(ssC.SuperReady, nil),
				c.SuperRpc.Mach.WhenNot1(ssrpc.ClientStates.Start, nil),
			)
			if ctx.Err() != nil {
				return // expired
			}

			// stopped rpc client is an error
			if c.SuperRpc.Mach.Not1(ssrpc.ClientStates.Start) {
				// TODO blacklist the node for X time
				// re-start
				c.SuperRpc.Stop(ctx, false)
				c.Mach.Remove1(ssC.Exception, nil)
				continue
			}

			if err != nil {
				err := errors.Join(err, c.SuperRpc.Mach.Err())
				AddErrRpc(c.Mach, err, nil)

				return
			}
		}
	}()
}

func (c *Client) StartEnd(e *am.Event) {
	if c.SuperRpc != nil {
		c.SuperRpc.Stop(context.TODO(), true)
	}
	if c.WorkerRpc != nil {
		c.WorkerRpc.Stop(context.TODO(), true)
	}
}

func (c *Client) WorkerRequestedEnter(e *am.Event) bool {
	return c.SuperRpc.Worker.Is1(ssS.WorkersAvailable)
}

func (c *Client) WorkerRequestedState(e *am.Event) {
	c.SuperRpc.Worker.Add1(ssS.ProvideWorker, PassRpc(&A{
		Id: GetRpcClientId(c.Name),
	}))
}

func (c *Client) WorkerPayloadEnter(e *am.Event) bool {
	a := rpc.ParseArgs(e.Args)
	return a != nil && a.Name != "" && a.Payload != nil
}

// WorkerPayloadState handles both Supervisor and Worker inbound payloads.
func (c *Client) WorkerPayloadState(e *am.Event) {
	c.Mach.Remove1(ssC.WorkerPayload, nil)
	args := rpc.ParseArgs(e.Args)
	c.log("worker %s delivered: %s", args.Payload.Source, args.Name)

	if args.Name != ssS.ProvideWorker {
		// worker impl will handle
		return
	}

	if c.Mach.Not1(ssC.WorkerRequested) {
		c.log("worker not requested")
		return
	}

	ctx := c.Mach.NewStateCtx(ssC.WorkerRequested)
	ctxStart := c.Mach.NewStateCtx(ssC.Start)
	addr, ok := args.Payload.Data.(string)
	if !ok || addr == "" {
		err := errors.New("invalid worker address")
		c.Mach.AddErrState(ssC.ErrSupervisor, err, nil)

		return
	}
	c.log("connecting to worker: %s", addr)

	// unblock
	go func() {
		// connect to the worker
		var err error
		c.WorkerRpc, err = rpc.NewClient(ctxStart, addr, GetClientId(c.Name),
			c.stateDeps.WorkerSStruct, c.stateDeps.WorkerSNames, &rpc.ClientOpts{
				Parent:   c.Mach,
				Consumer: c.Mach,
			})
		if err != nil {
			err := fmt.Errorf("failed to connect to the Worker: %w", err)
			AddErrRpc(c.Mach, err, nil)
			return
		}
		// delay for rpc/Mux
		c.WorkerRpc.HelloDelay = 100 * time.Millisecond
		amhelp.MachDebugEnv(c.WorkerRpc.Mach)

		// bind to worker rpc
		err = errors.Join(
			ampipe.BindConnected(c.WorkerRpc.Mach, c.Mach, ssC.WorkerDisconnected,
				ssC.WorkerConnecting, ssC.WorkerConnected, ssC.WorkerDisconnecting),
			ampipe.BindErr(c.WorkerRpc.Mach, c.Mach, ssC.ErrWorker),
			ampipe.BindReady(c.WorkerRpc.Mach, c.Mach, ssC.WorkerReady, ""),
		)
		if err != nil {
			c.Mach.AddErr(err, nil)
			return
		}

		// start and wait
		c.WorkerRpc.Start()
		err = amhelp.WaitForAny(ctx, c.ConnTimeout,
			c.Mach.When1(ssC.WorkerReady, nil),
			c.WorkerRpc.Mach.WhenErr(context.TODO()),
		)
		// TODO retry
		if err != nil {
			c.Mach.Remove1(ssC.WorkerRequested, nil)
		}
	}()
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

// Start initializes the client with a list of node addresses to connect to.
func (c *Client) Start(nodesList []string) {
	c.Mach.Add1(ssC.Start, Pass(&A{
		NodesList: nodesList,
	}))
}

// Stop halts the client's connection to both the supervisor and worker RPCs,
// and removes the client state from the state machine.
func (c *Client) Stop(ctx context.Context) {
	if c.SuperRpc != nil {
		c.SuperRpc.Stop(ctx, false)
	}
	if c.WorkerRpc != nil {
		c.WorkerRpc.Stop(ctx, false)
	}
	c.Mach.Remove1(ssC.Start, nil)
}

// ReqWorker sends a request to add a "WorkerRequested" state to the client's
// state machine and waits for "WorkerReady" state.
func (c *Client) ReqWorker(ctx context.Context) error {
	// failsafe worker request
	_, err := amhelp.NewReqAdd1(c.Mach, ssC.WorkerRequested, nil).Run(ctx)
	if err != nil {
		return err
	}

	err = amhelp.WaitForAll(ctx, c.ConnTimeout,
		c.Mach.When1(ssC.WorkerReady, nil))
	if err != nil {
		return err
	}

	c.log("worker connected: %s", c.WorkerRpc.Worker.ID)
	return nil
}

// Dispose deallocates resources and stops the client's RPC connections.
func (c *Client) Dispose(ctx context.Context) {
	c.Stop(ctx)
	c.SuperRpc = nil
	c.WorkerRpc = nil
	c.Mach.Dispose()
}

func (c *Client) log(msg string, args ...any) {
	if !c.LogEnabled {
		return
	}
	c.Mach.Log(msg, args...)
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

// ClientStateDeps contains the state definitions and names of the client and
// worker machines, needed to create a new client.
type ClientStateDeps struct {
	ClientSStruct am.Struct
	ClientSNames  am.S
	WorkerSStruct am.Struct
	WorkerSNames  am.S
}

// ClientOpts provides configuration options for creating a new client state
// machine.
type ClientOpts struct {
	// Parent is a parent state machine for a new client state machine. See
	// [am.Opts].
	Parent am.Api
}

// GetClientId returns a machine ID from a name.
func GetClientId(name string) string {
	return "nc-worker-" + name
}

// GetRpcClientId returns a machine ID from a name.
func GetRpcClientId(name string) string {
	return rpc.GetClientId("nc-worker-" + name)
}
