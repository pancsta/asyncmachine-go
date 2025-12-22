package relay

import (
	"context"
	"encoding/gob"
	"os"
	"path/filepath"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/joho/godotenv"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/telemetry"
	"github.com/pancsta/asyncmachine-go/tools/debugger/server"
	"github.com/pancsta/asyncmachine-go/tools/relay/states"
	"github.com/pancsta/asyncmachine-go/tools/relay/types"
)

var ss = states.RelayStates

type Relay struct {
	Mach *am.Machine
	Args *types.Args

	clients    map[string]*server.Client
	out        types.OutputFunc
	lastExport time.Time
}

// New creates a new Relay - state machine, RPC server.
func New(
	ctx context.Context, args *types.Args, out types.OutputFunc,
) (*Relay, error) {
	if args.Debug {
		out("debugging enabled\n")
		// load .env
		_ = godotenv.Load()
	}

	r := &Relay{
		Args:       args,
		lastExport: time.Now(),
		out:        out,
		clients:    make(map[string]*server.Client),
	}
	mach, err := am.NewCommon(ctx, "relay", states.RelaySchema, ss.Names(),
		r, nil, &am.Opts{
			DontPanicToException: args.Debug,
		})
	if err != nil {
		return nil, err
	}
	if args.Debug {
		amhelp.MachDebugEnv(mach)
	}
	r.Mach = mach

	return r, nil
}

func (r *Relay) StartState(e *am.Event) {
	a := r.Args.RotateDbg
	r.out("starting relay server on %s\n", a.ListenAddr)
	for _, addr := range a.FwdAddr {
		r.out("forwarding to %s\n", addr)
	}
	go server.StartRpc(r.Mach, a.ListenAddr, nil, a.FwdAddr, false)
}

// TODO StartEnd

// RPC SERVER

func (r *Relay) ClientMsgEnter(e *am.Event) bool {
	_, ok1 := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)
	_, ok2 := e.Args["conn_ids"].([]string)
	return ok1 && ok2
}

func (r *Relay) ClientMsgState(e *am.Event) {
	msgs := e.Args["msgs_tx"].([]*telemetry.DbgMsgTx)
	connIds := e.Args["conn_ids"].([]string)

	for i, msg := range msgs {

		// TODO check tokens
		machId := msg.MachineID
		c := r.clients[machId]
		if _, ok := r.clients[machId]; !ok {
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
	msg, ok1 := e.Args["msg_struct"].(*telemetry.DbgMsgStruct)
	_, ok2 := e.Args["conn_id"].(string)
	if !ok1 || !ok2 || msg.ID == "" {
		r.Mach.Log("Error: msg_struct malformed\n")
		return false
	}

	return true
}

func (r *Relay) ConnectEventState(e *am.Event) {
	// initial structure data
	msg := e.Args["msg_struct"].(*telemetry.DbgMsgStruct)
	connId := e.Args["conn_id"].(string)
	var c *server.Client

	// update existing client
	if existing, ok := r.clients[msg.ID]; ok {
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
		r.clients[msg.ID] = c
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

	r.Mach.Add1(ss.InitClient, am.A{"id": msg.ID})
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
	for _, c := range r.clients {
		if c.ConnId != "" && c.ConnId == connID {
			// mark as disconnected
			c.Connected.Store(false)
			r.Mach.Log("client %s disconnected", c.Id)
			r.out("client %s disconnected\n", c.Id)
			break
		}
	}

	// export on last client
	if len(r.clients) == 0 {
		if err := r.hExportData(); err != nil {
			// TODO err handler
			r.out("Error: export failed %s\n", err)
			r.Mach.AddErr(err, nil)
		}
	}
}

// TODO SetArgs

// methods

// TODO ExportDataState
func (r *Relay) hExportData() error {
	// TODO export text logs
	// TODO configurable suffix
	count := r.msgsCount()
	r.out("exporting %d transitions for %d clients\n", count, len(r.clients))

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
	data := make([]*server.Exportable, len(r.clients))
	i := 0
	for _, c := range r.clients {
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
	for _, c := range r.clients {
		c.Exportable.MsgTxs = nil
	}

	return nil
}

func (r *Relay) msgsCount() int {
	count := 0
	for _, c := range r.clients {
		count += len(c.MsgTxs)
	}

	return count
}
