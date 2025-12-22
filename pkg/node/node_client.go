package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
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

	RpcSuper  *rpc.Client
	RpcWorker *rpc.Client

	// internal

	// current nodes list (set by Start)
	nodeList     []string
	schemaClient am.Schema
	schemaWorker am.Schema
}

// implement ConsumerHandlers
var _ ssrpc.ConsumerHandlers = &Client{}

// NewClient creates a new Client instance with the provided context, id,
// workerKind, state dependencies, and options. Returns a pointer to the Client
// instance and an error if any validation or initialization fails.
//
// workerKind: any string used to build IDs and address workers
func NewClient(ctx context.Context, clientId string, workerKind string,
	workerSchema am.Schema, opts *ClientOpts,
) (*Client, error) {
	// defaults
	if opts == nil {
		opts = &ClientOpts{}
	}
	if opts.ClientSchema == nil {
		opts.ClientSchema = states.ClientSchema
	}
	if opts.ClientStates == nil {
		opts.ClientStates = states.ClientStates.Names()
	}

	// validate
	if clientId == "" {
		return nil, errors.New("client: clientId required")
	}
	if workerSchema == nil {
		return nil, errors.New("client: workerSchema required")
	}
	if workerKind == "" {
		return nil, errors.New("client: workerKind required")
	}
	err := amhelp.SchemaImplements(workerSchema, states.WorkerStates.Names())
	if err != nil {
		err := fmt.Errorf(
			"worker has to implement am/node/states/WorkerStates: %w", err)
		return nil, err
	}

	name := fmt.Sprintf("%s-%s-%s", workerKind, clientId,
		time.Now().Format("150405"))

	c := &Client{
		Name:         name,
		ConnTimeout:  5 * time.Second,
		LogEnabled:   os.Getenv(EnvAmNodeLogClient) != "",
		schemaClient: opts.ClientSchema,
		schemaWorker: workerSchema,
	}
	if amhelp.IsDebug() {
		c.ConnTimeout = 10 * c.ConnTimeout
	}
	mach, err := am.NewCommon(ctx, "nc-"+name, c.schemaClient,
		opts.ClientStates, c, opts.Parent, &am.Opts{
			Tags: slices.Concat([]string{"node-client"}, opts.Tags),
		})
	if err != nil {
		return nil, err
	}

	mach.SemLogger().SetArgsMapper(LogArgs)
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
	c.RpcSuper, err = rpc.NewClient(ctx, addr, GetSuperClientId(c.Name),
		states.SupervisorSchema, &rpc.ClientOpts{
			Parent:   c.Mach,
			Consumer: c.Mach,
		})
	if err != nil {
		err := fmt.Errorf("failed to connect to the Supervisor: %w", err)
		_ = AddErrRpc(c.Mach, err, nil)
		return
	}

	// bind to super rpc
	err = errors.Join(
		ampipe.BindConnected(c.RpcSuper.Mach, c.Mach, ssC.SuperDisconnected,
			ssC.SuperConnecting, ssC.SuperConnected, ssC.SuperDisconnecting),
		ampipe.BindErr(c.RpcSuper.Mach, c.Mach, ssC.ErrSupervisor),
		ampipe.BindReady(c.RpcSuper.Mach, c.Mach, ssC.SuperReady, ""),
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
			c.Mach.Remove1(am.StateException, nil)
			c.RpcSuper.Addr = addr
			// fewer retries, bc of fallbacks
			// TODO config via a composable RetryPolicy from rpc-c
			c.RpcSuper.ConnRetries = 3
			c.RpcSuper.Start()
			err := amhelp.WaitForAny(ctx, c.ConnTimeout,
				c.Mach.When1(ssC.SuperReady, nil),
				c.RpcSuper.Mach.WhenNot1(ssrpc.ClientStates.Start, nil),
			)
			if ctx.Err() != nil {
				return // expired
			}

			// stopped rpc client is an error
			if c.RpcSuper.Mach.Not1(ssrpc.ClientStates.Start) {
				// TODO blacklist the node for X time
				// re-start
				c.RpcSuper.Stop(ctx, false)
				c.Mach.Remove1(ssC.Exception, nil)
				continue
			}

			if err != nil {
				err := errors.Join(err, c.RpcSuper.Mach.Err())
				_ = AddErrRpc(c.Mach, err, nil)

				return
			}
		}
	}()
}

func (c *Client) StartEnd(e *am.Event) {
	if c.RpcSuper != nil {
		c.RpcSuper.Stop(context.TODO(), true)
	}
	if c.RpcWorker != nil {
		c.RpcWorker.Stop(context.TODO(), true)
	}
}

func (c *Client) WorkerRequestedEnter(e *am.Event) bool {
	return c.RpcSuper.NetMach.Is1(ssS.WorkersAvailable)
}

func (c *Client) WorkerRequestedState(e *am.Event) {
	// supervisor needs IDs of RPC clients for routing and ACL
	c.RpcSuper.NetMach.Add1(ssS.ProvideWorker, PassRpc(&A{
		SuperRpcId:  c.RpcSuper.Mach.Id(),
		WorkerRpcId: rpc.GetClientId(GetWorkerClientId(c.Name)),
	}))
}

func (c *Client) WorkerPayloadEnter(e *am.Event) bool {
	a := rpc.ParseArgs(e.Args)
	return a != nil && a.Name != "" && a.Payload != nil
}

// WorkerPayloadState handles both Supervisor and Worker inbound payloads, but
// this shared code only deals with [states.ClientStatesDef.WorkerRequested].
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
		c.RpcWorker, err = rpc.NewClient(ctxStart, addr, GetWorkerClientId(c.Name),
			c.schemaWorker, &rpc.ClientOpts{
				Parent:   c.Mach,
				Consumer: c.Mach,
			})
		if err != nil {
			err := fmt.Errorf("failed to connect to the NetworkMachine: %w", err)
			_ = AddErrRpc(c.Mach, err, nil)
			return
		}
		// delay for rpc/Mux
		c.RpcWorker.HelloDelay = 100 * time.Millisecond

		// bind to worker rpc
		err = errors.Join(
			ampipe.BindConnected(c.RpcWorker.Mach, c.Mach, ssC.WorkerDisconnected,
				ssC.WorkerConnecting, ssC.WorkerConnected, ssC.WorkerDisconnecting),
			ampipe.BindErr(c.RpcWorker.Mach, c.Mach, ssC.ErrWorker),
			ampipe.BindReady(c.RpcWorker.Mach, c.Mach, ssC.WorkerReady, ""),
		)
		if err != nil {
			c.Mach.AddErr(err, nil)
			return
		}

		// start and wait
		c.RpcWorker.Start()
		err = amhelp.WaitForAny(ctx, c.ConnTimeout,
			c.Mach.When1(ssC.WorkerReady, nil),
			c.RpcWorker.Mach.WhenErr(ctxStart),
		)
		// TODO retry
		if err != nil {
			c.Mach.Remove1(ssC.WorkerRequested, nil)
			return
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
	if c.RpcSuper != nil {
		c.RpcSuper.Stop(ctx, false)
	}
	if c.RpcWorker != nil {
		c.RpcWorker.Stop(ctx, false)
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

	c.log("worker connected: %s", c.RpcWorker.NetMach.Id())
	return nil
}

// Dispose deallocates resources and stops the client's RPC connections.
func (c *Client) Dispose(ctx context.Context) {
	c.Stop(ctx)
	c.RpcSuper = nil
	c.RpcWorker = nil
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

// ClientOpts provides configuration options for creating a new client state
// machine.
type ClientOpts struct {
	// Parent is a parent state machine for a new client state machine. See
	// [am.Opts].
	Parent am.Api
	Tags   []string
	// Optional schema for the client. Should extend [states.ClientStatesDef].
	ClientSchema am.Schema
	// Optional state names for ClientSchema.
	ClientStates am.S
}

// GetWorkerClientId returns a Node Client machine ID from a name.
func GetWorkerClientId(name string) string {
	return "nc-worker-" + name
}

// GetSuperClientId returns a Node Supervisor machine ID from a name.
func GetSuperClientId(name string) string {
	return "nc-super-" + name
}
