package relay

import (
	"context"
	"encoding/gob"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/coder/websocket"
	"github.com/joho/godotenv"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	arpc "github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry/dbg"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	typesDbg "github.com/pancsta/asyncmachine-go/tools/debugger/types"
	"github.com/pancsta/asyncmachine-go/tools/relay/states"
	"github.com/pancsta/asyncmachine-go/tools/relay/types"
)

var (
	ssR  = states.RelayStates
	ssT  = states.WsTcpTunStates
	Pass = types.Pass
)

type A = types.A

type Relay struct {
	Mach    *am.Machine
	Args    types.Args
	HttpMux *http.ServeMux

	// WS TCP tunnels
	wsTcpTuns map[string]*WsTcpTun
	// counter of WS TCP tunnels
	cWsTcpTuns int
	dbgClients map[string]*server.Client
	out        types.OutputFunc
	lastExport time.Time
	http       *http.Server
}

// New creates a new Relay - state machine, RPC server.
func New(ctx context.Context, args types.Args) (*Relay, error) {
	out := args.Output
	if out == nil {
		out = func(format string, v ...any) {
			fmt.Printf(format, v...)
		}
	}

	if args.Debug {
		out("WASM relay debug, loading .env\n")
		// load .env
		_ = godotenv.Load()
	}

	r := &Relay{
		Args: args,

		lastExport: time.Now(),
		out:        out,
		dbgClients: make(map[string]*server.Client),
		wsTcpTuns:  make(map[string]*WsTcpTun),
	}
	id := "relay"
	if args.Name != "" {
		id += "-" + args.Name
	}
	mach, err := am.NewCommon(ctx, id, states.RelaySchema, ssR.Names(),
		r, args.Parent, &am.Opts{
			DontPanicToException: args.Debug,
		})
	if err != nil {
		return nil, err
	}
	if args.Debug {
		_ = amhelp.MachDebugEnv(mach)
	}
	r.Mach = mach
	_, _ = arpc.MachReplEnv(mach, &arpc.ReplOpts{
		Args: types.ARpc{},
		// TODO ParseRpc
	})
	// mach.SemLogger().SetArgsMapperDef("remote_addr")
	mach.SemLogger().SetArgsMapper(types.LogArgs)

	return r, nil
}

// ///// ///// /////

// ///// HANDLERS (COMMON)

// ///// ///// /////

func (r *Relay) StartState(e *am.Event) {
	a := r.Args

	if a.RotateDbg != nil {
		cmd := a.RotateDbg
		r.out("starting relay server on %s\n", cmd.ListenAddr)
		for _, addr := range cmd.FwdAddr {
			r.out("forwarding to %s\n", addr)
		}
		dbgParams := typesDbg.Params{
			FwdData: cmd.FwdAddr,
		}
		go server.StartRpc(r.Mach, cmd.ListenAddr, nil, dbgParams)
	}

	if a.Wasm != nil {
		r.Mach.Add1(ssR.HttpStarting, nil)
	}
}

// TODO StartEnd

// ///// ///// /////

// ///// METHODS (COMMON)

// ///// ///// /////

func (r *Relay) Start(e *am.Event) am.Result {
	return r.Mach.EvAdd1(e, ssR.Start, nil)
}

func (r *Relay) Stop(e *am.Event) am.Result {
	return r.Mach.EvRemove1(e, ssR.Start, nil)
}

// ///// ///// /////

// ///// HANDLERS (WASM)

// ///// ///// /////

func (r *Relay) HttpStartingState(e *am.Event) {
	ctx := r.Mach.NewStateCtx(ssR.HttpStarting)

	addr := r.Args.Wasm.ListenAddr
	r.HttpMux = http.NewServeMux()
	r.http = &http.Server{
		Addr:    addr,
		Handler: r.HttpMux,
	}
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		go r.Mach.Add1(ssR.HttpReady, nil)
		err := r.http.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			// TODO retry?
			r.Mach.AddErr(err, nil)
		}
		r.Mach.Remove1(ssR.HttpReady, nil)
	}()
}

func (r *Relay) HttpReadyState(e *am.Event) {
	// TODO /dial - RPC clients (WS to TCP dial)
	// ctx := r.Mach.NewStateCtx(ssR.HttpReady)

	r.out("WASM relay listening on http://%s\n", r.Args.Wasm.ListenAddr)

	// static
	if dir := r.Args.Wasm.StaticDir; dir != "" {
		r.out("WASM relay serving %s\n", dir)
		r.HttpMux.Handle("/", http.FileServer(http.Dir(dir)))
	}

	// tun listen
	r.HttpMux.HandleFunc("/listen/",
		func(w http.ResponseWriter, req *http.Request) {
			r.HandleWsTcpListen(e, w, req)
		})
}

func (r *Relay) HandleWsTcpListen(
	e *am.Event, w http.ResponseWriter, req *http.Request,
) {
	// TODO loop guard to detect flood (fix thread safety)
	// TODO create a REPL file for repl- machs

	// TODO req.Context() needs body to be read to close
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	// metric
	r.Mach.EvAdd1(e, ssR.WsTunListenConn, nil)

	// parse "machid/localhost:1234"
	uri, ok := strings.CutPrefix(req.URL.Path, arpc.WsPathListen)
	if !ok {
		r.Mach.EvAddErrState(e, ssR.ErrNetwork, fmt.Errorf(
			"invalid /listen path: %s", req.URL.Path), nil)
		return
	}
	id, tcpAddr := path.Split(uri)
	id = strings.TrimSuffix(id, "/")
	r.Mach.Log("WS TCP tunnel for %s at %s", id, tcpAddr)

	// TODO origin security
	// TODO ID security

	// websocket
	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		r.Mach.EvAddErrState(e, ssR.ErrNetwork, err, Pass(&A{
			Addr: tcpAddr,
			Id:   id,
		}))
		return
	}

	// check client matchers
	for _, m := range r.Args.Wasm.ClientMatchers {
		if !m.Id.MatchString(id) {
			continue
		}
		r.Mach.Log("WS tunnel for %s accepted by client matcher", id)

		netConn := websocket.NetConn(ctx, conn, websocket.MessageBinary)
		client, err := m.NewClient(ctx, id, netConn)
		if err != nil {
			r.Mach.EvAddErr(e,
				fmt.Errorf("failed to create RPC client for %s: %s", id, err), nil)
		} else {
			// stay alive until disconn
			<-client.Mach.When1(ssrpc.ClientStates.Connected, ctx)
			<-client.Mach.WhenNot1(ssrpc.ClientStates.Connected, ctx)
			client.Stop(nil, e, true)
			return
		}
	}

	// init tun
	var tunMach *am.Machine
	ok = r.Mach.Eval("listen_init", func() {
		// rm old TODO also by duped IDs
		// TODO races and causes 2 tunnels per 1 client
		if tun, ok2 := r.wsTcpTuns[tcpAddr]; ok2 {
			r.Mach.Log("disposing existing WS TCP tunnel for %s at %s", id, tcpAddr)
			amhelp.Dispose(tun.Mach)
			time.Sleep(100 * time.Millisecond)
		}

		// init mach
		idTun := fmt.Sprintf("%s-wtt-%d", r.Mach.Id(), r.cWsTcpTuns)
		r.cWsTcpTuns++
		tun, err := NewWsTcpTun(ctx, conn, id, tcpAddr, req.RemoteAddr, idTun,
			r.Mach, r.Args.Debug)
		if err != nil {
			r.Mach.EvAddErr(e,
				fmt.Errorf("failed to create tun mach %s: %s", idTun, err), nil)
			return
		}

		// ok
		tunMach = tun.Mach
		r.wsTcpTuns[tcpAddr] = tun
	}, ctx)
	// err
	if !ok || tunMach == nil {
		_ = conn.Close(websocket.StatusInternalError, "tun creation failed")
		return
	}

	// REPL addr file
	if d := r.Args.Wasm.ReplAddrDir; d != "" &&
		strings.HasPrefix(id, "repl-") {

		name := id + ".addr"
		r.Mach.Log("REPL detected, creating %s", name)
		err := os.WriteFile(filepath.Join(d, name), []byte(tcpAddr), 0o644)
		if err != nil {
			r.Mach.EvAddErr(e, fmt.Errorf(
				"failed to write REPL addr file %s: %s", name, err), nil)
		}
	}

	// start and wait
	tunMach.EvAdd1(e, ssT.Start, nil)
	<-tunMach.WhenDisposed()

	// clean up
	r.Mach.Eval("listen_end", func() {
		amhelp.Dispose(tunMach)
		r.Mach.EvAdd1(e, ssR.WsTunListenDisconn, nil)
	}, ctx)
}

func (r *Relay) HttpReadyEnd(e *am.Event) {
	if s := r.http; s != nil {
		r.Mach.AddErr(s.Close(), nil)
	}
}

// ///// ///// /////

// ///// HANDLERS (DBG)

// ///// ///// /////
// TODO typed args for dbg server

func (r *Relay) ClientMsgEnter(e *am.Event) bool {
	_, ok1 := e.Args["msgs_tx"].([]*dbg.DbgMsgTx)
	_, ok2 := e.Args["conn_ids"].([]string)
	return ok1 && ok2
}

func (r *Relay) ClientMsgState(e *am.Event) {
	msgs := e.Args["msgs_tx"].([]*dbg.DbgMsgTx)
	connIds := e.Args["conn_ids"].([]string)

	for i, msg := range msgs {

		// TODO check tokens
		machId := msg.MachineID
		c := r.dbgClients[machId]
		if _, ok := r.dbgClients[machId]; !ok {
			r.Mach.Log("Error: client not found: %s\n", machId)
			continue
		}

		if c.MsgStruct == nil {
			r.Mach.Log("Error: schema missing for %s, ignoring tx\n", machId)
			continue
		}

		// verify it's from the same client
		if c.ConnId != connIds[i] {
			r.Mach.Log("Error: conn_id mismatch for %s, ignoring tx\n", machId)
			continue
		}

		// append the msg
		c.MsgTxs = append(c.MsgTxs, msg)
	}

	a := r.Args.RotateDbg
	if time.Since(r.lastExport) > a.IntervalTime || r.msgsCount() > a.IntervalTx {
		r.lastExport = time.Now()
		if err := r.hExportData(); err != nil {
			// TODO err handler
			r.out("Error: export failed %s\n", err)
			r.Mach.AddErr(err, nil)
		}
	}
}

func (r *Relay) ConnectEventEnter(e *am.Event) bool {
	msg, ok1 := e.Args["msg_struct"].(*dbg.DbgMsgStruct)
	_, ok2 := e.Args["conn_id"].(string)
	if !ok1 || !ok2 || msg.ID == "" {
		r.Mach.Log("Error: msg_struct malformed\n")
		return false
	}

	return true
}

func (r *Relay) ConnectEventState(e *am.Event) {
	// initial structure data
	msg := e.Args["msg_struct"].(*dbg.DbgMsgStruct)
	connId := e.Args["conn_id"].(string)
	var c *server.Client

	// update existing client
	if existing, ok := r.dbgClients[msg.ID]; ok {
		if existing.ConnId != "" && existing.ConnId == connId {
			r.Mach.Log("schema changed for %s", msg.ID)
			r.out("schema changed for %s\n", msg.ID)
			// TODO use MsgStructPatch
			// TODO keep old revisions
			existing.MsgStruct = msg
			c = existing
			c.ParseSchema()

		} else {
			r.Mach.Log("client %s already exists, overriding", msg.ID)
			r.out("client %s already exists, overriding\n", msg.ID)
		}
	}

	// create a new client
	if c == nil {
		r.out("new client %s\n", msg.ID)
		// TODO server.NewClient
		c = &server.Client{
			Id:         msg.ID,
			ConnId:     connId,
			SchemaHash: amhelp.SchemaHash(msg.States),
			Exportable: &server.Exportable{
				MsgStruct: msg,
			},
		}
		c.Connected.Store(true)
		r.dbgClients[msg.ID] = c
	}

	// TODO remove the last active client if over the limit
	// if len(r.clients) > maxClients {
	// 	var (
	// 		lastActiveTime time.Time
	// 		lastActiveID   string
	// 	)
	// 	// TODO get time from msgs
	// 	for id, c := range r.clients {
	// 		active := c.LastActive()
	// 		if active.After(lastActiveTime) || lastActiveID == "" {
	// 			lastActiveTime = active
	// 			lastActiveID = id
	// 		}
	// 	}
	// 	r.Mach.Add1(ss.RemoveClient, am.A{"Client.id": lastActiveID})
	// }

	r.Mach.Add1(ssR.InitClient, am.A{"id": msg.ID})
}

func (r *Relay) DisconnectEventEnter(e *am.Event) bool {
	_, ok := e.Args["conn_id"].(string)
	if !ok {
		r.Mach.Log("Error: DisconnectEvent malformed\n")
		return false
	}

	return true
}

func (r *Relay) DisconnectEventState(e *am.Event) {
	connID := e.Args["conn_id"].(string)
	for _, c := range r.dbgClients {
		if c.ConnId != "" && c.ConnId == connID {
			// mark as disconnected
			c.Connected.Store(false)
			r.Mach.Log("client %s disconnected", c.Id)
			r.out("client %s disconnected\n", c.Id)
			break
		}
	}

	// export on last client
	if len(r.dbgClients) == 0 {
		if err := r.hExportData(); err != nil {
			// TODO err handler
			r.out("Error: export failed %s\n", err)
			r.Mach.AddErr(err, nil)
		}
	}
}

// TODO SetArgs

// ///// ///// /////

// ///// METHODS (DBG)

// ///// ///// /////

// TODO ExportDataState
func (r *Relay) hExportData() error {
	// TODO export text logs
	// TODO configurable suffix
	count := r.msgsCount()
	r.out("exporting %d transitions for %d clients\n", count, len(r.dbgClients))

	a := r.Args.RotateDbg
	filename := a.Filename + "-" + time.Now().Format("2006-01-02T15-04-05")

	// create file
	gobPath := filepath.Join(a.Dir, filename+".gob.br")
	fh, err := os.Create(gobPath)
	if err != nil {
		return err
	}
	defer func() {
		r.Mach.AddErr(fh.Close(), nil)
	}()

	// prepare the format
	data := make([]*server.Exportable, len(r.dbgClients))
	i := 0
	for _, c := range r.dbgClients {
		data[i] = c.Exportable
		i++
	}

	// create a new brotli writer
	brCompress := brotli.NewWriter(fh)
	defer func() {
		r.Mach.AddErr(brCompress.Close(), nil)
	}()

	// encode
	encoder := gob.NewEncoder(brCompress)
	err = encoder.Encode(data)
	if err != nil {
		return err
	}

	// flush old msgs
	for _, c := range r.dbgClients {
		c.Exportable.MsgTxs = nil
	}

	return nil
}

func (r *Relay) msgsCount() int {
	count := 0
	for _, c := range r.dbgClients {
		count += len(c.MsgTxs)
	}

	return count
}
