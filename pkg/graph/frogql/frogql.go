//go:build frogql

// Package frogql embeds the froGQL graph database (ISO GQL) translated from
// its WebAssembly build into Go source by
// github.com/ncruces/wasm2go (see frogql_gen.go).
//
// The wasm artifact is frogql-wasm 0.6.0 from
// https://github.com/pleiad/frogql (MIT, see LICENSE.frogql). This file
// implements the wasm-bindgen host ABI and exposes it as a Connection.
//
//go:generate go run github.com/ncruces/wasm2go@v0.4.20 -tags frogql -pkg frogql -o frogql_gen.go frogql_wasm_bg.wasm
package frogql

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// maxSafeInteger mirrors Number.MAX_SAFE_INTEGER, the JS glue's default
// for an omitted execute limit.
const maxSafeInteger = 9007199254740991

// jsError is a JS Error on the wasm-bindgen heap.
type jsError struct{ msg, stack string }

// jsUndefinedT and jsNullT are JS undefined / null sentinels.
type jsUndefinedT struct{}
type jsNullT struct{}

var (
	jsUndefined = jsUndefinedT{}
	jsNull      = jsNullT{}
)

// host implements the wasm-bindgen imports expected by the module.
type host struct {
	m *Module
}

// Init receives the module from New.
func (h *host) Init(v any) { h.m = v.(*Module) }

func (h *host) mem() []byte { return *h.m.Xmemory().Slice() }

func (h *host) readString(ptr, len int32) string {
	mem := h.mem()
	return string(mem[ptr : ptr+len])
}

// writeString allocates in the wasm memory, so Rust can free it later.
func (h *host) writeString(s string) int32 {
	buf := []byte(s)
	ptr := h.m.X__wbindgen_malloc(int32(len(buf)), 1)
	mem := h.mem()
	copy(mem[ptr:ptr+int32(len(buf))], buf)
	return ptr
}

func (h *host) writeRet(retptr int32, ptr, l int32) {
	mem := h.mem()
	binary.LittleEndian.PutUint32(mem[retptr+4:], uint32(l))
	binary.LittleEndian.PutUint32(mem[retptr:], uint32(ptr))
}

// take reads a result from the externref table and deallocates its slot.
func (h *host) take(idx int32) any {
	t := *h.m.X__wbindgen_externrefs()
	v := t[idx]
	h.m.X__externref_table_dealloc(idx)
	return v
}

func (h *host) throwErr(idx int32) error {
	v := h.take(idx)
	if e, ok := v.(*jsError); ok {
		return fmt.Errorf("frogql: %s", e.msg)
	}
	return fmt.Errorf("frogql: %v", v)
}

func jsString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "null"
	case jsUndefinedT:
		return "undefined"
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", x)
	}
}

// imports

func (h *host) X__wbg_Error_ef53bc310eb298a0(v0, v1 int32) any {
	return &jsError{msg: h.readString(v0, v1)}
}

func (h *host) X__wbg_String_8564e559799eccda(v0 int32, v1 any) {
	s := jsString(v1)
	ptr := h.writeString(s)
	h.writeRet(v0, ptr, int32(len(s)))
}

func (h *host) X__wbg___wbindgen_is_string_c236cabd84a4d769(v0 any) int32 {
	if _, ok := v0.(string); ok {
		return 1
	}
	return 0
}

func (h *host) X__wbg___wbindgen_throw_1506f2235d1bdba0(v0, v1 int32) {
	panic(fmt.Errorf("frogql throw: %s", h.readString(v0, v1)))
}

func (h *host) X__wbg_error_a6fa202b58aa1cd3(v0, v1 int32) {
	fmt.Println("frogql:", h.readString(v0, v1))
	h.m.X__wbindgen_free(v0, v1, 1)
}

func (h *host) X__wbg_new_227d7c05414eb861() any { return &jsError{} }
func (h *host) X__wbg_new_622fc80556be2e26() any { return map[any]any{} }
func (h *host) X__wbg_new_ce1ab61c1c2b300d() any { return map[string]any{} }
func (h *host) X__wbg_new_d90091b82fdf5b91() any { return &[]any{} }

func (h *host) X__wbg_now_190933fa139cc119() float64 {
	return float64(time.Now().UnixMilli())
}

func (h *host) X__wbg_set_52b1e1eb5bed906a(v0, v1, v2 any) any {
	v0.(map[any]any)[v1] = v2
	return v0
}

func (h *host) X__wbg_set_6be42768c690e380(v0, v1, v2 any) {
	v0.(map[string]any)[v1.(string)] = v2
}

func (h *host) X__wbg_set_dca99999bba88a9a(v0 any, v1 int32, v2 any) {
	arr := v0.(*[]any)
	if int(v1) >= len(*arr) {
		grown := make([]any, int(v1)+1)
		copy(grown, *arr)
		*arr = grown
	}
	(*arr)[v1] = v2
}

func (h *host) X__wbg_stack_3b0d974bbf31e44f(v0 int32, v1 any) {
	var stack string
	if e, ok := v1.(*jsError); ok {
		stack = e.stack
	}
	ptr := h.writeString(stack)
	h.writeRet(v0, ptr, int32(len(stack)))
}

func (h *host) X__wbindgen_cast_0000000000000001(v0 float64) any { return v0 }
func (h *host) X__wbindgen_cast_0000000000000002(v0 int64) any   { return v0 }
func (h *host) X__wbindgen_cast_0000000000000003(v0, v1 int32) any {
	return h.readString(v0, v1)
}
func (h *host) X__wbindgen_cast_0000000000000004(v0 int64) any { return uint64(v0) }

func (h *host) X__wbindgen_init_externref_table() {
	t := h.m.X__wbindgen_externrefs()
	offset := len(*t)
	*t = append(*t, nil, nil, nil, nil)
	(*t)[0] = jsUndefined
	(*t)[offset] = jsUndefined
	(*t)[offset+1] = jsNull
	(*t)[offset+2] = true
	(*t)[offset+3] = false
}

// ///// ///// /////

// ///// CONNECTION

// ///// ///// /////

// Connection is a single-threaded frogql database connection over an
// in-memory graph. All methods are safe for concurrent use.
type Connection struct {
	h   *host
	mx  sync.Mutex
	ptr int32
}

// OpenJSON opens a connection over a JSON graph document in the shape
// {"nodes": [...], "edges": [...]}, or an empty object for an empty DB.
func OpenJSON(json string) (*Connection, error) {
	h := &host{}
	m := New(h)
	m.X__wbindgen_start()

	c := &Connection{h: h}
	c.mx.Lock()
	defer c.mx.Unlock()

	ptr := h.writeString(json)
	conn, errIdx, isErr := m.Xopen_json(ptr, int32(len(json)))
	if isErr != 0 {
		return nil, h.throwErr(errIdx)
	}
	c.ptr = conn

	return c, nil
}

// Open opens a connection over an empty in-memory graph.
func Open() (*Connection, error) {
	return OpenJSON(`{"nodes":[],"edges":[]}`)
}

// Close releases the underlying connection.
func (c *Connection) Close() error {
	if c == nil {
		return nil
	}
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.ptr == 0 {
		return nil
	}
	c.h.m.X__wbg_connection_free(c.ptr, 0)
	c.ptr = 0

	return nil
}

// Exec executes one GQL statement. Read queries return a slice of row
// maps, data-modifying statements return a counters map. A negative or
// omitted limit uses the engine default.
func (c *Connection) Exec(gql string, limit ...int64) (any, error) {
	if c == nil {
		return nil, errors.New("frogql: connection is nil")
	}
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.ptr == 0 {
		return nil, errors.New("frogql: connection is closed")
	}

	l := float64(maxSafeInteger)
	if len(limit) > 0 && limit[0] >= 0 {
		l = float64(limit[0])
	}

	ptr := c.h.writeString(gql)
	rIdx, eIdx, isErr := c.h.m.Xconnection_execute(
		c.ptr, ptr, int32(len(gql)), l)
	if isErr != 0 {
		return nil, c.h.throwErr(eIdx)
	}

	return normalize(c.h.take(rIdx)), nil
}

// ToJSON serializes the live merged view to a JSON graph document.
func (c *Connection) ToJSON() (string, error) {
	if c == nil {
		return "", errors.New("frogql: connection is nil")
	}
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.ptr == 0 {
		return "", errors.New("frogql: connection is closed")
	}

	ptr, l := c.h.m.Xconnection_to_json(c.ptr)
	ret := c.h.readString(ptr, l)
	c.h.m.X__wbindgen_free(ptr, l, 1)

	return ret, nil
}

// NodeCount returns the number of nodes in the graph.
func (c *Connection) NodeCount() int {
	if c == nil {
		return 0
	}
	c.mx.Lock()
	defer c.mx.Unlock()

	return int(c.h.m.Xconnection_node_count(c.ptr))
}

// EdgeCount returns the number of edges in the graph.
func (c *Connection) EdgeCount() int {
	if c == nil {
		return 0
	}
	c.mx.Lock()
	defer c.mx.Unlock()

	return int(c.h.m.Xconnection_edge_count(c.ptr))
}

// Schema returns {node_labels, edge_labels, node_count, edge_count}.
func (c *Connection) Schema() (any, error) {
	return c.ref1(func(m *Module, p int32) (int32, int32, int32) {
		return m.Xconnection_schema(p)
	})
}

// GraphTypeJSON returns the active graph type as data, for diagrams.
func (c *Connection) GraphTypeJSON() (any, error) {
	return c.ref1(func(m *Module, p int32) (int32, int32, int32) {
		return m.Xconnection_graph_type_json(p)
	})
}

// Check runs the typechecker on a query without executing it.
func (c *Connection) Check(gql string) (any, error) {
	if c == nil {
		return nil, errors.New("frogql: connection is nil")
	}
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.ptr == 0 {
		return nil, errors.New("frogql: connection is closed")
	}

	ptr := c.h.writeString(gql)
	rIdx, eIdx, isErr := c.h.m.Xconnection_check(c.ptr, ptr, int32(len(gql)))
	if isErr != 0 {
		return nil, c.h.throwErr(eIdx)
	}

	return normalize(c.h.take(rIdx)), nil
}

// ref1 calls an externref-returning connection method.
func (c *Connection) ref1(
	f func(m *Module, ptr int32) (int32, int32, int32),
) (any, error) {
	if c == nil {
		return nil, errors.New("frogql: connection is nil")
	}
	c.mx.Lock()
	defer c.mx.Unlock()

	if c.ptr == 0 {
		return nil, errors.New("frogql: connection is closed")
	}

	rIdx, eIdx, isErr := f(c.h.m, c.ptr)
	if isErr != 0 {
		return nil, c.h.throwErr(eIdx)
	}

	return normalize(c.h.take(rIdx)), nil
}

// normalize converts the wasm-bindgen value tree into plain Go values.
func normalize(v any) any {
	switch x := v.(type) {
	case *[]any:
		ret := make([]any, len(*x))
		for i, item := range *x {
			ret[i] = normalize(item)
		}
		return ret
	case map[string]any:
		ret := make(map[string]any, len(x))
		for k, item := range x {
			ret[k] = normalize(item)
		}
		return ret
	case map[any]any:
		ret := make(map[string]any, len(x))
		for k, item := range x {
			ret[jsString(k)] = normalize(item)
		}
		return ret
	case jsUndefinedT, jsNullT:
		return nil
	default:
		return v
	}
}
