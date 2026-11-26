//go:build tinygo

package rpc

import (
	"bufio"
	"context"
	"encoding/gob"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gobwas/ws"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// listenTCP is a TinyGo variant of net.ListenConfig.Listen, which is not
// implemented by TinyGo.
func listenTCP(_ context.Context, network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}

// dialWebSocket dials a WebSocket endpoint and returns a net.Conn carrying
// binary messages as a byte stream. TinyGo uses the syscall/js / gobwas
// adapter.
func dialWebSocket(
	ctx context.Context, url string, protocols []string, timeout time.Duration,
) (net.Conn, error) {
	return utils.DialWebSocket(ctx, url, protocols, timeout)
}

var errRpcShutdown = errors.New("rpc: shutdown")

// rpcRequest, rpcResponse, and rpcMessage mirror the rpc2 gob wire format, so
// TinyGo peers can talk to regular rpc2 peers.
type rpcRequest struct {
	Seq    uint64
	Method string
}

type rpcResponse struct {
	Seq   uint64
	Error string
}

type rpcMessage struct {
	Seq    uint64
	Method string
	Error  string
}

// rpcCodec is a gob codec compatible with rpc2's gobCodec.
type rpcCodec struct {
	rwc    io.ReadWriteCloser
	dec    *gob.Decoder
	enc    *gob.Encoder
	encBuf *bufio.Writer
	mutex  sync.Mutex
}

func newRpcCodec(conn io.ReadWriteCloser) *rpcCodec {
	buf := bufio.NewWriter(conn)
	return &rpcCodec{
		rwc:    conn,
		dec:    gob.NewDecoder(conn),
		enc:    gob.NewEncoder(buf),
		encBuf: buf,
	}
}

func (c *rpcCodec) readHeader(req *rpcRequest, resp *rpcResponse) error {
	var msg rpcMessage
	if err := c.dec.Decode(&msg); err != nil {
		return err
	}

	if msg.Method != "" {
		req.Seq = msg.Seq
		req.Method = msg.Method
	} else {
		resp.Seq = msg.Seq
		resp.Error = msg.Error
	}

	return nil
}

func (c *rpcCodec) readRequestBody(body any) error {
	return c.dec.Decode(body)
}

func (c *rpcCodec) readResponseBody(body any) error {
	return c.dec.Decode(body)
}

func (c *rpcCodec) writeRequest(r *rpcRequest, body any) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if err := c.enc.Encode(r); err != nil {
		return err
	}
	if err := c.enc.Encode(body); err != nil {
		return err
	}

	return c.encBuf.Flush()
}

func (c *rpcCodec) writeResponse(r *rpcResponse, body any) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if err := c.enc.Encode(r); err != nil {
		return err
	}
	if err := c.enc.Encode(body); err != nil {
		return err
	}

	return c.encBuf.Flush()
}

func (c *rpcCodec) Close() error {
	return c.rwc.Close()
}

// tinyHandler dispatches a concrete RPC method without reflection.
type tinyHandler struct {
	newArgs  func() any
	newReply func() any
	invoke   func(c *rpcClient, args any, reply any) error
}

func (h *tinyHandler) handle(
	c *rpcClient, codec *rpcCodec, seq uint64, args any,
) {
	reply := h.newReply()
	err := h.invoke(c, args, reply)
	if seq == 0 {
		return
	}

	resp := &rpcResponse{Seq: seq}
	if err != nil {
		resp.Error = err.Error()
	}
	_ = codec.writeResponse(resp, reply)
}

// tinyHandlerFor builds a reflection-free handler from a typed method value.
func tinyHandlerFor[A, B any](
	fn func(*rpcClient, *A, *B) error,
) *tinyHandler {
	return &tinyHandler{
		newArgs:  func() any { return new(A) },
		newReply: func() any { return new(B) },
		invoke: func(c *rpcClient, args any, reply any) error {
			return fn(c, args.(*A), reply.(*B))
		},
	}
}

func makeTinyHandler(handler any) *tinyHandler {
	switch fn := handler.(type) {
	case func(*rpcClient, *MsgCliHello, *MsgSrvHello) error:
		return tinyHandlerFor(fn)
	case func(*rpcClient, *MsgCliMutation, *MsgSrvMutation) error:
		return tinyHandlerFor(fn)
	case func(*rpcClient, *MsgCliMutation, *MsgEmpty) error:
		return tinyHandlerFor(fn)
	case func(*rpcClient, *MsgEmpty, *MsgSrvSync) error:
		return tinyHandlerFor(fn)
	case func(*rpcClient, *MsgEmpty, *MsgSrvArgs) error:
		return tinyHandlerFor(fn)
	case func(*rpcClient, *MsgSrvUpdate, *MsgEmpty) error:
		return tinyHandlerFor(fn)
	case func(*rpcClient, *MsgSrvUpdateMuts, *MsgEmpty) error:
		return tinyHandlerFor(fn)
	case func(*rpcClient, *MsgSrvPayload, *MsgEmpty) error:
		return tinyHandlerFor(fn)
	case func(*rpcClient, *MsgSrvHello, *MsgEmpty) error:
		return tinyHandlerFor(fn)
	case func(*rpcClient, *MsgEmpty, *MsgEmpty) error:
		return tinyHandlerFor(fn)
	default:
		panic("rpc: unsupported handler signature")
	}
}

// rpcServer is a reflection-free replacement for rpc2.Server.
type rpcServer struct {
	handlers     map[string]*tinyHandler
	onConnect    func(*rpcClient)
	onDisconnect func(*rpcClient)
}

func newRpcServer() *rpcServer {
	return &rpcServer{handlers: make(map[string]*tinyHandler)}
}

func (s *rpcServer) Handle(method string, handler any) {
	s.handlers[method] = makeTinyHandler(handler)
}

func (s *rpcServer) OnConnect(f func(*rpcClient)) {
	s.onConnect = f
}

func (s *rpcServer) OnDisconnect(f func(*rpcClient)) {
	s.onDisconnect = f
}

func (s *rpcServer) ServeConn(conn net.Conn) {
	c := newRpcClientWithCodec(newRpcCodec(conn))
	c.server = true
	c.handlers = s.handlers

	if s.onConnect != nil {
		s.onConnect(c)
	}
	c.Run()
	if s.onDisconnect != nil {
		s.onDisconnect(c)
	}
}

func (s *rpcServer) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Printf("rpc: accept: %v", err)
			}
			return
		}
		go s.ServeConn(conn)
	}
}

// rpcCall is a single in-flight RPC call.
type rpcCall struct {
	Method string
	Args   any
	Reply  any
	Error  error
	Done   chan *rpcCall
}

func (c *rpcCall) done() {
	select {
	case c.Done <- c:
	default:
	}
}

// rpcClient is a reflection-free replacement for rpc2.Client.
type rpcClient struct {
	codec *rpcCodec

	mutex   sync.Mutex
	sending sync.Mutex
	seq     uint64
	pending map[uint64]*rpcCall

	closing  bool
	shutdown bool
	server   bool
	blocking bool

	handlers   map[string]*tinyHandler
	disconnect chan struct{}
	discOnce   sync.Once
}

func newRpcClient(conn net.Conn) *rpcClient {
	return newRpcClientWithCodec(newRpcCodec(conn))
}

func newRpcClientWithCodec(codec *rpcCodec) *rpcClient {
	return &rpcClient{
		codec:      codec,
		seq:        1,
		pending:    make(map[uint64]*rpcCall),
		handlers:   make(map[string]*tinyHandler),
		disconnect: make(chan struct{}),
	}
}

func (c *rpcClient) Handle(method string, handler any) {
	c.handlers[method] = makeTinyHandler(handler)
}

func (c *rpcClient) SetBlocking(blocking bool) {
	c.blocking = blocking
}

func (c *rpcClient) DisconnectNotify() chan struct{} {
	return c.disconnect
}

func (c *rpcClient) Run() {
	var err error
	for err == nil {
		req := rpcRequest{}
		resp := rpcResponse{}
		if err = c.codec.readHeader(&req, &resp); err != nil {
			break
		}

		if req.Method != "" {
			err = c.readRequest(&req)
		} else {
			err = c.readResponse(&resp)
		}
	}

	c.mutex.Lock()
	c.shutdown = true
	closing := c.closing
	if err == io.EOF && !closing {
		err = io.ErrUnexpectedEOF
	}
	for seq, call := range c.pending {
		delete(c.pending, seq)
		call.Error = err
		call.done()
	}
	c.mutex.Unlock()

	c.discOnce.Do(func() { close(c.disconnect) })
	if !closing {
		_ = c.codec.Close()
	}
}

func (c *rpcClient) readRequest(req *rpcRequest) error {
	h, ok := c.handlers[req.Method]
	if !ok {
		resp := &rpcResponse{
			Seq:   req.Seq,
			Error: "rpc: can't find method " + req.Method,
		}
		return c.codec.writeResponse(resp, resp)
	}

	args := h.newArgs()
	if err := c.codec.readRequestBody(args); err != nil {
		return err
	}

	if c.blocking {
		h.handle(c, c.codec, req.Seq, args)
	} else {
		go h.handle(c, c.codec, req.Seq, args)
	}

	return nil
}

func (c *rpcClient) readResponse(resp *rpcResponse) error {
	c.mutex.Lock()
	call := c.pending[resp.Seq]
	delete(c.pending, resp.Seq)
	c.mutex.Unlock()

	switch {
	case call == nil:
		// no pending call; discard the body
		return c.codec.readResponseBody(nil)
	case resp.Error != "":
		call.Error = errors.New(resp.Error)
		_ = c.codec.readResponseBody(nil)
		call.done()
	default:
		if err := c.codec.readResponseBody(call.Reply); err != nil {
			call.Error = err
		}
		call.done()
	}

	return nil
}

// CallWithContext invokes method and waits for the response or ctx expiry.
func (c *rpcClient) CallWithContext(
	ctx context.Context, method string, args, reply any,
) error {
	call := c.goCall(method, args, reply)
	select {
	case <-call.Done:
		return call.Error
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Call invokes method and waits for the response.
func (c *rpcClient) Call(method string, args, reply any) error {
	return c.CallWithContext(context.Background(), method, args, reply)
}

func (c *rpcClient) goCall(method string, args, reply any) *rpcCall {
	c.mutex.Lock()
	seq := c.seq
	c.seq++
	call := &rpcCall{
		Method: method,
		Args:   args,
		Reply:  reply,
		Done:   make(chan *rpcCall, 1),
	}
	c.pending[seq] = call
	c.mutex.Unlock()

	c.sending.Lock()
	err := c.codec.writeRequest(&rpcRequest{Seq: seq, Method: method}, args)
	c.sending.Unlock()

	if err != nil {
		c.mutex.Lock()
		delete(c.pending, seq)
		c.mutex.Unlock()
		call.Error = err
		call.done()
	}

	return call
}

// Notify sends a request without waiting for a response.
func (c *rpcClient) Notify(method string, args any) error {
	c.sending.Lock()
	defer c.sending.Unlock()

	return c.codec.writeRequest(&rpcRequest{Method: method}, args)
}

func (c *rpcClient) Close() error {
	c.mutex.Lock()
	if c.shutdown || c.closing {
		c.mutex.Unlock()
		return errRpcShutdown
	}
	c.closing = true
	c.mutex.Unlock()

	return c.codec.Close()
}

func setRpcDebugLog(_ bool) {
	// rpc2.DebugLog is not available in TinyGo
}

func BindHandlersClient(h *Client, mach *am.Machine) (string, error) {
	return mach.HandlersBindMaps(
		ClientNegotiations(h), ClientFinals(h))
}

func BindHandlersServer(h *Server, mach *am.Machine) (string, error) {
	return mach.HandlersBindMaps(
		ServerNegotiations(h), ServerFinals(h))
}

// ///// ///// /////

// ///// NETWORK MACHINE

// ///// ///// /////

// HandlersBind binds a struct of handler methods to machine's states, based on
// the naming convention, eg `FooState(e *Event)`. Negotiation handlers can
// optionally return bool.
//
// # TinyGo
//
// Not supported in TinyGo, use [am.Machine.HandlersBindMaps] instead.
func (m *NetworkMachine) HandlersBind(
	handlers any, opts ...am.BindOpts,
) (string, error) {
	panic("HandlersBind() not available in tinygo, use HandlersBindMaps()")
}

// handle runs a single handler method (currently only pipes).
func (m *NetworkMachine) handle(h *handler, i int, state, suffix string) {
	// TODO only maps
	h.mx.Lock()
	methodName := state + suffix
	e := am.NewEvent(nil, m)
	e.Name = methodName
	e.MachineId = m.remoteId

	// TODO descriptive name
	handlerName := strconv.Itoa(i) + ":" + h.id
	// handlerNeg, ok1 := h.methods

	if m.semLogger.Level() >= am.LogEverything {
		emitterId := utils.TruncateStr(handlerName, 15)
		emitterId = utils.PadString(strings.ReplaceAll(
			emitterId, " ", "_",
		), 15, "_")
		m.log(am.LogEverything, "[handle:%-15s] %s", emitterId, methodName)
	}

	// map-based handlers
	if h.isMap {
		m.handleMap(h, state, suffix, e, handlerName)
		h.mx.Unlock()
		return
	}

	// ret := true
	// if c.negotiation != nil {
	// 	ret = c.negotiation(c.event)
	// } else if c.final != nil {
	// 	c.final(c.event)
	// } else {
	// 	panic("invalid handler call " + c.name)
	// }

	// cache
	_, ok := h.missingCache[methodName]
	if ok {
		h.mx.Unlock()

		return
	}
	method, ok := h.methodCache[methodName]
	if !ok {
		method = h.methods.MethodByName(methodName)

		// support field handlers
		if !method.IsValid() {
			method = h.methods.Elem().FieldByName(methodName)
		}
		if !method.IsValid() {
			h.missingCache[methodName] = struct{}{}
			h.mx.Unlock()
			return
		}
		h.methodCache[methodName] = method
	}

	// call the handler (pipes dont block)
	m.log(am.LogOps, "[handler:%d] %s", h.num, methodName)

	// tracers
	// m.tracersMx.RLock()
	tx := m.t.Load()
	for i := range m.tracers {
		m.tracers[i].HandlerStart(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// TODO remove
	// call TODO should go-fork to avoid nested deadlocks?
	// _ = method.Call([]reflect.Value{reflect.ValueOf(e)})

	// tracers
	// m.tracersMx.RLock()
	for i := range m.tracers {
		m.tracers[i].HandlerEnd(tx, handlerName, methodName)
	}
	// m.tracersMx.RUnlock()

	// locks
	h.mx.Unlock()
}

// WEBSOCKET SERVER (not tunnel)

type wsHandlerServer struct {
	s     *Server
	event *am.Event
}

// ServeHTTP continues [Server.RpcStartingState].
func (h *wsHandlerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mach := h.s.Mach

	connWs, rw, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	conn, err := utils.NewServerWSConn(connWs, rw)
	if err != nil {
		_ = connWs.Close()
		log.Printf("WebSocket conn error: %v", err)
		return
	}
	h.s.Conn = conn

	// next and stay alive TODO JSON-RPC compat via RpcAcceptingJson
	// TODO add Opts.JsonRpc (req Opts.WebSocket)
	mach.EvAdd1(h.event, ssS.RpcAccepting, nil)
	select {
	case <-mach.WhenNot1(ss.Start, nil):
	case <-r.Context().Done():
	}
}
