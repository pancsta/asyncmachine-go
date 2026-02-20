package relay

import (
	"context"
	"io"
	"net"

	"github.com/coder/websocket"

	"github.com/pancsta/asyncmachine-go/tools/relay/states"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
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
	idTun string,
	parent am.Api, debug bool,
) (*WsTcpTun, error) {
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
	_ = amhelp.MachDebugEnv(mach)

	// register disposal
	var dispose am.HandlerDispose = func(id string, ctx context.Context) {
		_ = t.WsConn.Close(websocket.StatusNormalClosure, "")
	}
	t.Mach.Add1(ssT.RegisterDisposal, am.A{
		ssam.DisposedArgHandler: dispose,
	})

	return t, nil
}

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

func (t *WsTcpTun) WebSocketEnd(e *am.Event) {
	// dispose on WS disconn
	t.Mach.EvAdd1(e, ssT.Disposing, nil)
}

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
			add := amhelp.EvAdd1Sync(ctx, e, t.Mach, ssT.TcpAccepted,
				types.Pass(&types.A{
					RemoteAddr: tcpConn.RemoteAddr().String(),
					Conn:       tcpConn,
				}))
			if add == am.Executed {
				<-t.Mach.WhenNot1(ssT.TcpAccepted, ctx)
			}
		}
	})
}

func (t *WsTcpTun) TcpAcceptedState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ssT.TcpAccepted)
	args := types.ParseArgs(e.Args)

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
