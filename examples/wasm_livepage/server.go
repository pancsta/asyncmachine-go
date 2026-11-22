//go:build !js

package example

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/pancsta/go-app/pkg/app"

	"github.com/pancsta/livepage"

	"github.com/pancsta/asyncmachine-go/examples/wasm_livepage/states"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// Server is a running livepage + WASM example server.
type Server struct {
	Page *livepage.Page[*View]
	Mach *am.Machine
	// Addr is the actual HTTP address.
	Addr string

	handlers *Handlers
	http     *http.Server
}

// StartServer starts the example HTTP server on addr (eg "127.0.0.1:0") and
// returns once it's accepting connections.
func StartServer(ctx context.Context, addr string) (*Server, error) {
	h := &Handlers{}
	mach, err := am.NewCommon(ctx, "server-livepage", states.DemoSchema,
		ssD.Names(), h, nil, nil)
	if err != nil {
		return nil, err
	}
	h.mach = mach
	amhelp.MachDebugEnv(mach)
	mach.SemLogger().SetArgsMapper(amhelp.LogArgsMapper)

	view := &View{mach: mach, h: h}
	page, err := livepage.New(ctx, view, livepage.Opts{
		Debounce: 20 * time.Millisecond,
		DiffHtml: true,
		HtmlPage: htmlPage,
		WsMux:    h.handleWsMux,
	})
	if err != nil {
		return nil, err
	}
	h.page = page

	mux := http.NewServeMux()
	mux.Handle("/", page.NewHandler())
	mux.Handle("/client/", http.StripPrefix("/client/",
		http.FileServer(http.Dir("./client"))))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	srv := &Server{
		Page:     page,
		Mach:     mach,
		Addr:     ln.Addr().String(),
		handlers: h,
	}
	srv.http = &http.Server{Handler: mux}
	go func() {
		if err := srv.http.Serve(ln); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %s", err)
		}
	}()

	mach.Add1(ssD.Start, nil)

	return srv, nil
}

// Close stops the HTTP server.
func (s *Server) Close() error {
	return s.http.Close()
}

func htmlPage(body string) string {
	return `<!DOCTYPE html><html><head><meta charset="utf-8">` +
		`<title>livepage + tinygo wasm</title>` +
		`<script src="/client/wasm_exec.js"></script>` +
		`</head><body>` + body +
		`<script>
const go = new Go();
WebAssembly.instantiateStreaming(
  fetch("/client/main.wasm?" + Date.now()), go.importObject
).then(r => go.run(r.instance));
</script></body></html>`
}

// ///// ///// /////

// ///// RPC

// ///// ///// /////

// handleWsMux multiplexes two aRPC streams over the page's WebSocket:
//
//   - stream 0: the browser is the aRPC client, the server accepts
//   - stream 1: the server is the aRPC client, the browser accepts
//
// This allows mutations with args in both directions, on a single WebSocket.
func (h *Handlers) handleWsMux(ctx context.Context, mux *livepage.WsMux) {
	go h.handleRpcServer(ctx, mux.Open(0))
	go h.handleRpcClient(ctx, mux.Open(1))
}

// handleRpcServer accepts a browser aRPC client.
func (h *Handlers) handleRpcServer(ctx context.Context, conn net.Conn) {
	id := "browser-" + amhelp.RandId(3)
	srv, err := arpc.NewServer(ctx, "", id, h.mach,
		&arpc.ServerOpts{Parent: h.mach})
	if err != nil {
		log.Printf("rpc server: %s", err)
		_ = conn.Close()
		return
	}

	srv.Conn = conn
	srv.Start(nil)
	h.clients.Add(1)
	defer h.clients.Add(-1)
	log.Printf("aRPC client %s connected over the livepage WebSocket", id)

	<-srv.Mach.When1(ssrpc.ServerStates.HandshakeDone, ctx)
	<-srv.Mach.WhenNot1(ssrpc.ServerStates.ClientConnected, ctx)
	log.Printf("aRPC client %s disconnected", id)
}

// handleRpcClient dials the browser's aRPC server.
func (h *Handlers) handleRpcClient(ctx context.Context, conn net.Conn) {
	id := "browser-" + amhelp.RandId(3)
	client, err := arpc.NewClient(ctx, "", id, states.DemoSchema,
		&arpc.ClientOpts{Parent: h.mach})
	if err != nil {
		log.Printf("rpc client: %s", err)
		_ = conn.Close()
		return
	}

	client.Conn.Store(&conn)
	client.Start(nil)

	select {
	case <-client.Mach.When1(ssrpc.ClientStates.Ready, ctx):
	case <-ctx.Done():
		return
	}
	h.browserClient.Store(client)
	defer h.browserClient.CompareAndSwap(client, nil)
	log.Printf("aRPC client (server -> wasm) %s connected", id)

	<-client.Mach.WhenNot1(ssrpc.ClientStates.Ready, ctx)
	log.Printf("aRPC client (server -> wasm) %s disconnected", id)
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

type Handlers struct {
	*am.ExceptionHandler

	mach  *am.Machine
	page  *livepage.Page[*View]
	count atomic.Int64
	// connected browsers
	clients atomic.Int32
	// aRPC client to the latest browser, for mutations with args
	browserClient atomic.Pointer[arpc.Client]

	mu   sync.Mutex
	msgs []string
}

var _ = ssD.Start

func (h *Handlers) StartState(e *am.Event) {
	ctx := h.mach.NewStateCtx(ssD.Start)

	h.mach.Fork(ctx, e, func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		i := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				i++
				// server -> browser, with args
				if c := h.browserClient.Load(); c != nil {
					c.NetMach.Add1(ssD.Msg, am.Pass(NewMsg(fmt.Sprintf(
						"hello %d from the server", i))))
				}
			}
		}
	})
}

var _ = ssD.Incr

func (h *Handlers) IncrState(_ *am.Event) {
	h.count.Add(1)
	h.page.HRefresh()
}

var _ = ssD.Msg

func (h *Handlers) MsgState(e *am.Event) {
	args := am.ParseArgs[AMsg](e.Args)
	h.addMsg("server: " + args.Msg)
}

func (h *Handlers) addMsg(msg string) {
	h.mu.Lock()
	h.msgs = append(h.msgs, msg)
	h.mu.Unlock()

	h.page.HRefresh()
}

func (h *Handlers) log() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return strings.Join(h.msgs, "\n")
}

// ///// ///// /////

// ///// COMPO

// ///// ///// /////

type View struct {
	livepage.Compo

	mach *am.Machine
	h    *Handlers
}

func (v *View) Mach() *am.Machine {
	return v.mach
}

func (v *View) Render() UI {
	return Div().Body(
		H1().Text("livepage + tinygo wasm"),

		P().Body(
			Span().Text("counter: "),
			Span().ID("count").Text(
				strconv.FormatInt(v.h.count.Load(), 10)),
			Button().ID("incr").Text("+").
				OnClick(func(Context, Event) {
					v.mach.Add1(ssD.Incr, nil)
				}),
		),

		P().Body(
			Span().Text("aRPC clients: "),
			Span().ID("clients").Text(
				strconv.Itoa(int(v.h.clients.Load()))),
		),

		P().Text("wasm log (aRPC over the same WebSocket):"),
		Pre().ID("rpc-log").Text("..."),

		P().Text("server log (aRPC from the wasm):"),
		Pre().ID("server-log").Text(v.h.log()),
	)
}
