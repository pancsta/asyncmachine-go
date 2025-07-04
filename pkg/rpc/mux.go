package rpc

import (
	"context"
	"net"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/soheilhy/cmux"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// MuxNewServerFn is a function to create a new RPC server for each incoming
// connection.
type MuxNewServerFn func(num int, conn net.Conn) (*Server, error)

var ssM = states.MuxStates

// Mux creates a new RPC server for each incoming connection.
type Mux struct {
	*am.ExceptionHandler
	Mach *am.Machine
	// Source is the state source to expose via RPC. Required if NewServerFn
	// isnt provided.
	Source am.Api
	// NewServerFn creates a new instance of Server and is called for every new
	// connection.
	NewServerFn MuxNewServerFn

	Name string
	Addr string
	// The listener used by this Mux, can be set manually before Start().
	Listener   net.Listener
	LogEnabled bool
	// The last error returned by NewServerFn.
	NewServerErr error

	clients   []net.Conn
	cmux      cmux.CMux
	connCount atomic.Int64
}

// NewMux initializes a Mux instance to handle RPC server creation for incoming
// connections with the given parameters.
//
// newServerFn: when nil, [Mux.Source] needs to be set manually before calling
// [Mux.Start].
func NewMux(
	ctx context.Context, name string, newServerFn MuxNewServerFn, opts *MuxOpts,
) (*Mux, error) {
	d := &Mux{
		Name:        name,
		LogEnabled:  os.Getenv(EnvAmRpcLogMux) != "",
		NewServerFn: newServerFn,
	}
	if opts == nil {
		opts = &MuxOpts{}
	}

	mach, err := am.NewCommon(ctx, "rm-"+name, states.MuxSchema, ssM.Names(),
		d, opts.Parent, &am.Opts{Tags: []string{"rpc-mux"}})
	if err != nil {
		return nil, err
	}
	mach.SetLogArgs(LogArgs)
	d.Mach = mach
	// optional env debug
	if os.Getenv(EnvAmRpcDbg) != "" {
		amhelp.MachDebugEnv(mach)
	}

	return d, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (m *Mux) ExceptionState(e *am.Event) {
	m.ExceptionHandler.ExceptionState(e)
	// TODO restart depending on Start, err, and backoff
	// errors.Is(err, cmux.ErrListenerClosed)
	// errors.Is(err, cmux.ErrServerClosed)
}

func (m *Mux) NewServerErrEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.Err != nil
}

func (m *Mux) NewServerErrState(e *am.Event) {
	args := ParseArgs(e.Args)
	m.NewServerErr = args.Err
}

func (m *Mux) StartEnter(e *am.Event) bool {
	return m.NewServerFn != nil || m.Source != nil
}

func (m *Mux) StartState(e *am.Event) {
	ctx := m.Mach.NewStateCtx(ssM.Start)
	addr := m.Addr
	mach := m.Mach

	// unblock
	go func() {
		if ctx.Err() != nil {
			return // expired
		}
		// create a listener if not provided
		if m.Listener == nil {
			// use Start as the context
			cfg := net.ListenConfig{}
			lis, err := cfg.Listen(ctx, "tcp4", addr)
			if err != nil {
				// add err to mach
				AddErrNetwork(e, mach, err)
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
		go m.accept(l)

		// TODO healthcheck loop

		// start cmux
		if err := m.cmux.Serve(); err != nil {
			mach.AddErr(err, nil)
			mach.EvRemove1(e, ssM.Start, nil)
		}
	}()
}

func (m *Mux) StartEnd(e *am.Event) {
	if m.Listener != nil {
		_ = m.Listener.Close()
		m.Listener = nil
	}
}

func (m *Mux) ClientConnectedState(e *am.Event) {
	m.Mach.EvRemove1(e, ssM.ClientConnected, nil)
}

func (m *Mux) HasClientsEnd(e *am.Event) bool {
	return len(m.clients) == 0
}

func (m *Mux) HealthcheckState(e *am.Event) {
	// TODO remove stale clients
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

func (m *Mux) accept(l net.Listener) {
	mach := m.Mach
	defer mach.PanicToErr(nil)

	go m.Mach.Add1(ssM.Ready, Pass(&A{
		Addr: l.Addr().String(),
	}))

	for {
		// TODO handle ErrListenerClosed and ErrServerClosed
		conn, err := l.Accept()
		if err != nil {

			mach.AddErr(err, nil)
			continue
		}
		m.Mach.Add1(ssM.ClientConnected, Pass(&A{
			Addr: conn.RemoteAddr().String(),
		}))

		// get a new conn number
		var num int64 = -1
		for {
			num = m.connCount.Load()
			if m.connCount.CompareAndSwap(num, num+1) {
				break
			}
		}

		// new instance
		var server *Server
		if m.NewServerFn == nil {
			server, err = NewServer(m.Mach.Ctx(), ":0",
				m.Name+"-"+strconv.Itoa(int(num)), m.Source, &ServerOpts{
					Parent: m.Mach,
				})
		} else {
			server, err = m.NewServerFn(int(num), conn)
		}
		// TODO return this err to the RPC client
		if err != nil {
			_ = conn.Close()
			mach.Log("failed to create a new server: %s", err)
			continue
		}

		// inject net.Conn
		server.Conn = conn
		server.Start()

		// TODO re-use old instances
		// TODO handle with a state, not a goroutine
		go func() {
			// dispose on disconnect
			muxCtx := m.Mach.NewStateCtx(ssM.Start)
			<-server.Mach.When1(ssS.ClientConnected, muxCtx)
			<-server.Mach.WhenNot1(ssS.ClientConnected, muxCtx)
			server.Stop(true)
		}()
	}
}

func (m *Mux) Start() am.Result {
	return m.Mach.Add1(ssM.Start, nil)
}

func (m *Mux) Stop(dispose bool) am.Result {
	res := m.Mach.Remove1(ssM.Start, nil)
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

// ///// MUX

// ///// ///// /////

type MuxOpts struct {
	// Parent is a parent state machine for a new Mux state machine. See
	// [am.Opts].
	Parent am.Api
}
