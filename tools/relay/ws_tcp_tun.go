package relay

import (
	"context"
	"io"
	"net"

	"github.com/coder/websocket"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
	"github.com/pancsta/asyncmachine-go/tools/relay/states"
	"github.com/pancsta/asyncmachine-go/tools/relay/types"
)

type WsTcpTun struct {
	*ssam.DisposedHandlers

	Mach *am.Machine
	// Incoming WebSocket connection
	WsConn *websocket.Conn
	// TCP addr to listen on
	TcpAddr    string
	TcpLn      net.Listener
	RemoteAddr string
	RemoteId   string
}

func NewWsTcpTun(
	ctx context.Context, wsConn *websocket.Conn, idRemote, addr, remoteAddr,
	idTun string, parent am.Api, debug bool,
) (*WsTcpTun, error) {
	// TODO validate wsConn
	// TODO generate ID

	t := &WsTcpTun{
		DisposedHandlers: &ssam.DisposedHandlers{},

		WsConn:     wsConn,
		TcpAddr:    addr,
		RemoteAddr: remoteAddr,
		RemoteId:   idRemote,
	}

	mach, err := am.NewCommon(ctx, idTun, states.WsTcpTunSchema,
		ssT.Names(), t, parent, &am.Opts{
			DontPanicToException: debug,
		})
	if err != nil {
		return nil, err
	}
	t.Mach = mach
	if debug {
		_ = amhelp.MachDebugEnv(mach)
	}
	// debug
	mach.AddBreakpoint1(ssT.Disposing, "", true)
	mach.AddBreakpoint1(ssT.Disposing, "", false)

	return t, nil
}

var _ = ssT.Disposing

func (t *WsTcpTun) DisposingState(e *am.Event) {
	_ = t.WsConn.Close(websocket.StatusNormalClosure, "")
	t.DisposedHandlers.DisposingState(e)
}

var _ = ssT.Start

func (t *WsTcpTun) StartEnter(e *am.Event) bool {
	return t.WsConn != nil && t.TcpAddr != ""
}

func (t *WsTcpTun) StartState(e *am.Event) {
	t.Mach.Log("WS tun for %s (%s) at %s", t.RemoteId, t.RemoteAddr, t.TcpAddr)
}

func (t *WsTcpTun) StartEnd(e *am.Event) {
	if t.TcpLn != nil {
		_ = t.TcpLn.Close()
	}
}

var _ = ssT.WebSocket

func (t *WsTcpTun) WebSocketEnd(e *am.Event) {
	// dispose on WS disconn
	t.Mach.EvAdd1(e, ssT.Disposing, nil)
}

var _ = ssT.TcpListen

func (t *WsTcpTun) TcpListenState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ssR.Start)

	t.Mach.Fork(ctx, e, func() {
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, "tcp", t.TcpAddr)
		if err != nil {
			t.Mach.EvAddErr(e, err, nil)
			return
		}
		if ctx.Err() != nil {
			// close early
			ln.Close()
			return // expired
		}
		t.TcpLn = ln
		t.Mach.EvAdd1(e, ssT.TcpListening, nil)
	})
}

var _ = ssT.TcpListening

func (t *WsTcpTun) TcpListeningState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ssR.Start)
	tcpLn := t.TcpLn

	t.Mach.Fork(ctx, e, func() {
		for ctx.Err() == nil {
			tcpConn, err := tcpLn.Accept()
			if err != nil {
				t.Mach.Log("TCP accept err (%s)", tcpLn.Addr())
				continue
			}

			// accept
			ok := amhelp.EvAdd1Sync(ctx, e, t.Mach, ssT.TcpAccepted,
				Pass(&types.A{
					RemoteAddr: tcpConn.RemoteAddr().String(),
					Conn:       tcpConn,
				}))
			if ctx.Err() != nil {
				return // expired
			}
			if ok {
				<-t.Mach.WhenNot1(ssT.TcpAccepted, ctx)
			}
		}
	})
}

var _ = ssT.TcpAccepted

func (t *WsTcpTun) TcpAcceptedState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ssT.TcpAccepted)
	args := am.ParseArgs[A](e.Args)

	t.Mach.Fork(ctx, e, func() {
		// start
		wsNetConn := websocket.NetConn(ctx, t.WsConn, websocket.MessageBinary)

		// TCP -> WebSocket (WASM)
		go func() {
			_, err := io.Copy(wsNetConn, args.Conn)
			t.Mach.EvAddErrState(e, ssT.ErrServer, err, Pass(&A{
				RemoteAddr: args.RemoteAddr,
			}))
		}()

		// WebSocket (WASM) -> TCP
		go func() {
			_, err := io.Copy(args.Conn, wsNetConn)
			t.Mach.EvAddErrState(e, ssT.ErrClient, err, Pass(&A{
				RemoteAddr: args.RemoteAddr,
			}))
		}()

		// wait
		<-ctx.Done()
	})
}
