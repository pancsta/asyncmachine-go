//go:generate go run ./gen
package mux

// TODO
// type wsHandlerMux struct {
// 	m     *Mux
// 	event *am.Event
// }
//
// // ServeHTTP continues [Server.RpcStartingState].
// func (h *wsHandlerMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
// 	mach := h.m.Mach
//
// 	connWs, err := websocket.Accept(w, r, &websocket.AcceptOptions{
// 		// TODO security
// 		InsecureSkipVerify: true,
// 	})
// 	if err != nil {
// 		log.Printf("Upgrade error: %v", err)
// 		return
// 	}
// 	conn := websocket.NetConn(mach.Context(), connWs, websocket.MessageBinary)
//
// 	// TODO RpcAcceptingState
// 	// next and stay alive
// 	mach.EvAdd1(h.event, ssM.RpcAccepting, nil)
// 	<-mach.WhenNot1(ss.Start, nil)
// 	print()
// }

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/pancsta/asyncmachine-go/pkg/helpers"
	"github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/states/pipes"
	"github.com/soheilhy/cmux"
)

// MuxNewServerFn is a function to create a new RPC server for each incoming
// connection.
type MuxNewServerFn func(mux *Mux, id string, conn net.Conn) (*rpc.Server, error)

var ssM = states.MuxStates
var ssS = states.ServerStates
var ssC = states.ClientStates

// Mux creates a new RPC server for each incoming connection.
type Mux struct {
	*machine.ExceptionHandler

	Mach *machine.Machine
	// Source is the state source to expose via RPC. Required if NewServerFn
	// isnt provided.
	Source machine.Api
	// Typed arguments struct value
	Args any

	Name string
	Addr string
	// Listener used by this [Mux], can be set manually before Start().
	Listener   net.Listener
	LogEnabled bool
	// The last error returned by NewServerFn.
	NewServerErr error
	Opts         MuxOpts

	cmux          cmux.CMux
	countConns    atomic.Int64
	countDisconns atomic.Int64
}

// NewMux initializes a Mux instance to handle RPC server creation for incoming
// connections with the given parameters.
//
// addr: can be empty if [Mux.Listener] is set later.
// stateSource: optional, can be replaced with [opts.NewServerFn].
func NewMux(
	ctx context.Context, addr string, name string, stateSource machine.Api,
	opts *MuxOpts,
) (*Mux, error) {
	// TODO allow muxers without listening on a port (for relay matchers)

	if opts == nil {
		opts = &MuxOpts{}
	}
	m := &Mux{
		Name:       name,
		LogEnabled: os.Getenv(rpc.EnvAmRpcLogMux) != "",
		Source:     stateSource,
		Addr:       addr,
		Args:       opts.Args,
		Opts:       *opts,
	}

	mach, err := machine.NewCommon(ctx, "rm-"+name, states.MuxSchema, ssM.Names(),
		nil, opts.Parent, &machine.Opts{Tags: []string{"rpc-mux"}})
	if err != nil {
		return nil, err
	}
	if err = BindHandlers(m, mach); err != nil {
		return nil, err
	}
	mach.SemLogger().SetArgsMapper(rpc.LogArgs)
	mach.SetGroups(states.MuxGroups, ssC)
	m.Mach = mach
	// optional env debug
	if os.Getenv(rpc.EnvAmRpcDbg) != "" {
		_ = helpers.MachDebugEnv(mach)
	}

	return m, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (m *Mux) ExceptionState(e *machine.Event) {
	m.ExceptionHandler.ExceptionState(e)
	// TODO restart depending on Start, err, and backoff
	// errors.Is(err, cmux.ErrListenerClosed)
	// errors.Is(err, cmux.ErrServerClosed)
}

func (m *Mux) NewServerErrEnter(e *machine.Event) bool {
	a := rpc.ParseArgs(e.Args)
	return a != nil && a.Err != nil
}

func (m *Mux) NewServerErrState(e *machine.Event) {
	args := rpc.ParseArgs(e.Args)
	m.NewServerErr = args.Err
}

func (m *Mux) StartEnter(e *machine.Event) bool {
	// require either a source or factory
	return m.Opts.NewServerFn != nil || m.Source != nil
}

func (m *Mux) StartState(e *machine.Event) {
	ctx := m.Mach.NewStateCtx(ssM.Start)
	addr := m.Addr
	mach := m.Mach

	// TODO websocket srv, RpcMuxState

	// unblock
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// create a listener if not provided TODO websockets
		if m.Listener == nil {
			// use Start as the context
			cfg := net.ListenConfig{}
			lis, err := cfg.Listen(ctx, "tcp4", addr)
			if err != nil {
				// add err to mach
				rpc.AddErrNetwork(e, mach, err)
				// add outcome to mach
				mach.EvRemove1(e, ssM.Start, nil)

				return
			}

			m.Listener = lis
		}

		// Create a new cmux instance.
		m.cmux = cmux.New(m.Listener)

		// update Addr from listener (support for external and :0)
		m.Addr = m.Listener.Addr().String()
		m.log("mux started on %s", m.Addr)

		// fork
		l := m.cmux.Match(cmux.Any())
		go m.accept(e, l)

		// TODO healthcheck loop

		// start cmux
		if err := m.cmux.Serve(); err != nil {
			mach.AddErr(err, nil)
			mach.EvRemove1(e, ssM.Start, nil)
		}
	}()
}

func (m *Mux) StartEnd(e *machine.Event) {
	if m.Listener != nil {
		_ = m.Listener.Close()
		m.Listener = nil
	}
}

func (m *Mux) ClientConnectedState(e *machine.Event) {
	m.Mach.EvRemove1(e, ssM.ClientConnected, nil)
}

func (m *Mux) HasClientsEnd(e *machine.Event) bool {
	return m.countConns.Load() == m.countDisconns.Load()
}

func (m *Mux) HealthcheckState(e *machine.Event) {
	// TODO remove stale clients
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

// TODO RpcAcceptingState
func (m *Mux) accept(e *machine.Event, l net.Listener) {
	mach := m.Mach
	defer mach.PanicToErr(nil)

	go m.Mach.Add1(ssM.Ready, rpc.Pass(&rpc.A{
		Addr: l.Addr().String(),
	}))

	for {
		// TODO handle ErrListenerClosed and ErrServerClosed
		conn, err := l.Accept()
		if err != nil {
			mach.AddErr(err, nil)
			continue
		}
		m.Mach.Add1(ssM.ClientConnected, rpc.Pass(&rpc.A{
			Addr: conn.RemoteAddr().String(),
		}))

		// get a new conn number
		nextId := m.countConns.Add(1)

		// new instance
		server, err := m.NewServer(e, strconv.Itoa(int(nextId)), conn)
		// TODO return this err to the RPC client
		if err != nil {
			_ = conn.Close()
			mach.Log("failed to create a new server: %s", err)
			m.countDisconns.Add(1)
			continue
		}
		mach.EvAdd1(e, ssM.HasClients, nil)

		// TODO optimize: re-use old instances?
		// TODO handle with a state, not a goroutine
		go func() {
			// dispose on disconnect
			muxCtx := m.Mach.NewStateCtx(ssM.Start)
			<-server.Mach.When1(ssS.ClientConnected, muxCtx)
			<-server.Mach.WhenNot1(ssS.ClientConnected, muxCtx)
			server.Stop(e, true)
			m.countDisconns.Add(1)
			mach.EvRemove1(e, ssM.HasClients, nil)
		}()
	}
}

// NewServer creates a new server instance for this muxer.
func (m *Mux) NewServer(
	e *machine.Event, id string, conn net.Conn,
) (*rpc.Server, error) {
	var srv *rpc.Server
	var err error
	if m.Opts.NewServerFn == nil {
		srv, err = m.NewDefaultServer(id)
	} else {
		srv, err = m.Opts.NewServerFn(m, id, conn)
	}
	if err != nil {
		return nil, err
	}

	srv.Conn = conn
	srv.Start(e)

	return srv, nil
}

func (m *Mux) NewDefaultServer(id string) (*rpc.Server, error) {
	return rpc.NewServer(m.Mach.Context(), "",
		m.Name+"-"+id, m.Source, &rpc.ServerOpts{
			Parent:   m.Mach,
			Args:     m.Args,
			ParseRpc: m.Opts.ParseRpc,
		})
}

func (m *Mux) Start(e *machine.Event) machine.Result {
	return m.Mach.EvAdd1(e, ssM.Start, nil)
}

func (m *Mux) Stop(e *machine.Event, dispose bool) machine.Result {
	res := m.Mach.EvRemove1(e, ssM.Start, nil)
	if dispose {
		m.Mach.Dispose()
	}

	return res
}

func (m *Mux) log(msg string, args ...any) {
	if !m.LogEnabled {
		return
	}
	m.Mach.Log(msg, args...)
}

// ///// ///// /////

// ///// MISC

// ///// ///// /////

type MuxOpts struct {
	// NewServerFn is a function to create a new RPC server for each incoming
	// connection. Optional.
	NewServerFn MuxNewServerFn
	// Parent is a parent state machine for a new Mux state machine. See
	// [am.Opts].
	Parent machine.Api
	// Typed arguments struct value
	Args any
	// optional RPC args parser
	ParseRpc func(args machine.A) machine.A
}

// BindMux binds the HasClients state with Add/Remove to custom states.
func BindMux(
	source, target *machine.Machine, activeState, inactiveState string,
) error {
	if activeState == "" {
		return fmt.Errorf("active state must be set")
	}
	if inactiveState == "" {
		inactiveState = activeState
	}

	return pipes.Bind(source, target, ssM.HasClients, activeState, inactiveState)
}
