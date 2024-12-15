package rpc

import (
	"context"
	"log"
	"net"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/soheilhy/cmux"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// MuxNewServer is a function to create a new RPC server for each incoming
// connection.
type MuxNewServer func(num int, conn net.Conn) (*Server, error)

const EnvAmRpcLogMux = "AM_RPC_LOG_MUX"

var ssM = states.MuxStates

// Mux creates a new RPC server for each incoming connection.
type Mux struct {
	*am.ExceptionHandler
	Mach *am.Machine

	Name        string
	Addr        string
	Listener    net.Listener
	LogEnabled  bool
	NewServerFn MuxNewServer
	// The last error returned by NewServerFn.
	NewServerErr error

	clients   []net.Conn
	cmux      cmux.CMux
	connCount atomic.Int64
}

func NewMux(
	ctx context.Context, name string, newServer MuxNewServer, opts *MuxOpts,
) (*Mux, error) {
	d := &Mux{
		Name:        name,
		LogEnabled:  os.Getenv(EnvAmRpcLogMux) != "",
		NewServerFn: newServer,
	}
	if opts == nil {
		opts = &MuxOpts{}
	}

	mach, err := am.NewCommon(ctx, "rm-"+name, states.MuxStruct, ssM.Names(),
		d, opts.Parent, nil)
	if err != nil {
		return nil, err
	}
	mach.SetLogArgs(LogArgs)
	d.Mach = mach

	return d, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (m *Mux) ExceptionState(e *am.Event) {
	m.ExceptionHandler.ExceptionState(e)
	// TODO restart
}

func (m *Mux) NewServerErrEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.Err != nil
}

func (m *Mux) NewServerErrState(e *am.Event) {
	args := ParseArgs(e.Args)
	m.NewServerErr = args.Err
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
		conn, err := l.Accept()
		if err != nil {
			log.Println(err)
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
				m.Name+"-"+strconv.Itoa(int(num)), m.Mach, &ServerOpts{
					Parent: m.Mach,
				})
		} else {
			server, err = m.NewServerFn(int(num), conn)
		}

		// inject net.Conn
		if err != nil {
			_ = conn.Close()
			mach.Log("failed to create a new server: %s", err)
			continue
		}
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
