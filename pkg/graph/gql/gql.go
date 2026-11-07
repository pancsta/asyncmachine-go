// Package gql implements a GQL query engine over the in-memory graph of
// [github.com/pancsta/asyncmachine-go/pkg/graph]. It uses the parser front-end
// of the gochickpeas fork (gql/gqlparser) and a native executor that reads the
// live github.com/dominikbraun/graph instance directly, with no separate
// storage and no chickpeas snapshot.
//
// The exposed graph model mirrors the frogql backend, so queries are portable
// between the two:
//
//	(:Machine {id, parent, schema_hash, mtime_sum, queue_tick, mach_tick})
//	(:State {mach, name, tick, active})
//	(:Tag {mach, name, value})
//	(Machine)-[:HAS {auto, multi}]->(State)
//	(Machine)-[:HAS_TAG]->(Tag)
//	(Machine)-[:CHILD_OF]->(Machine)
//	(Machine)-[:CONNECTED_TO]->(Machine) // RPC conns (src-id tags)
//	(State)-[:REQUIRE|ADD|REMOVE|AFTER]->(State)
//
// This package is MIT-licensed (see LICENSE). The GQL parser front-end comes
// from the MIT-licensed gochickpeas fork (see LICENSE.gochickpeas).

package gql

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/pancsta/gochickpeas/gql/gqlparser"

	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// Db implements [amtypes.DbGraph] over a live in-memory graph instance. It
// keeps no data of its own: every Exec projects the current graph (through
// dominikbraun/graph's locked accessors, which return copies) and runs the
// native executor over it, so queries always see the latest state without any
// mirroring or sync loop.
//
// Wire it to the same graph the NetGraph maintains:
//
//	netGraph.Db = gql.NewDb(netGraph.Graph)
//
// The GQL engine is read-only: writes go through the graph itself (AddMachine,
// ParseMsgDbg, ParseMsgRpc), which is why UpsertMachine/SyncMachine are no-ops
// here.
type Db struct {
	graph *amgraph.Graph
}

var _ amtypes.DbGraph = (*Db)(nil)

// NewDb returns a [amtypes.DbGraph] reading g. g must not be nil.
func NewDb(g *amgraph.Graph) amtypes.DbGraph {
	return &Db{graph: g}
}

// Exec parses and executes one GQL read statement against a snapshot of the
// graph. It returns []any, each element a map[string]any of output columns, in
// the same shape as the frogql backend (JSON/msgpack encodable; numbers are
// int64/float64).
func (d *Db) Exec(statement string) (any, error) {
	q, err := gqlparser.Parse(statement)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, err)
	}
	if err := gqlparser.Desugar(q); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupported, err)
	}

	st, err := d.snapshot()
	if err != nil {
		return nil, err
	}

	e := &exec{store: st}
	rows, cols, err := e.query(q)
	if err != nil {
		return nil, err
	}

	out := make([]any, len(rows))
	for i, r := range rows {
		m := make(map[string]any, len(cols))
		for _, c := range cols {
			m[c] = r.out[c].Any()
		}
		out[i] = m
	}
	return out, nil
}

// snapshot projects the current graph into the queryable store. The store
// build reads through dominikbraun/graph's locked accessors, which return
// copies, so no full graph [amgraph.Graph.Clone] (and no Map mirror clone) is
// needed per query.
func (d *Db) snapshot() (*store, error) {
	if d == nil || d.graph == nil {
		return nil, errors.New("gql: nil graph")
	}
	return newStore(d.graph)
}

// NodeCount returns the number of vertices in the graph instance.
func (d *Db) NodeCount() int {
	st, err := d.snapshot()
	if err != nil {
		return 0
	}
	return len(st.nodes)
}

// EdgeCount returns the number of logical relationships in the graph
// instance (one physical edge may expand into several).
func (d *Db) EdgeCount() int {
	st, err := d.snapshot()
	if err != nil {
		return 0
	}
	return len(st.rels)
}

// UpsertMachine is a no-op: the queried store is the live graph instance,
// which the NetGraph updates before pushing to Db.
func (d *Db) UpsertMachine(*amtypes.Machine) error {
	return nil
}

// SyncMachine is a no-op: see UpsertMachine.
func (d *Db) SyncMachine(*amtypes.Machine) error {
	return nil
}

// Close is a no-op: the Db owns no resources.
func (d *Db) Close() error {
	return nil
}

// litVal converts a parsed literal to a runtime value. Named and auto-lifted
// parameters are unsupplied (Exec has no parameter binding), so they read as
// null.
func litVal(l gqlparser.Literal) Value {
	switch l.Kind {
	case gqlparser.LitInt:
		return Int(l.I)
	case gqlparser.LitFloat:
		return Float(l.F)
	case gqlparser.LitStr:
		return Str(l.S)
	case gqlparser.LitBool:
		return Bool(l.B)
	case gqlparser.LitNull, gqlparser.LitNamedParam, gqlparser.LitParam:
		return Null()
	}
	return Null()
}

// eqVal is two-valued equality used by inline property filters.
func eqVal(a, b Value) bool {
	c, ok := compare(a, b)
	return ok && c == 0
}

// eval evaluates one expression against a row.
func (e *exec) eval(r row, ex gqlparser.Expr) (Value, error) {
	switch n := ex.(type) {
	case nil:
		return Null(), nil

	case *gqlparser.Lit:
		return litVal(n.Value), nil

	case *gqlparser.Var:
		v, ok := r.vals[n.Name]
		if !ok {
			return Null(), fmt.Errorf("%w: unknown variable %q", ErrParse, n.Name)
		}
		return v, nil

	case *gqlparser.Prop:
		base, err := e.eval(r, &gqlparser.Var{Name: n.Var})
		if err != nil {
			return Null(), err
		}
		return propOf(base, n.Key), nil

	case *gqlparser.PropOf:
		base, err := e.eval(r, n.Base)
		if err != nil {
			return Null(), err
		}
		return propOf(base, n.Key), nil

	case *gqlparser.Unary:
		v, err := e.eval(r, n.Expr)
		if err != nil {
			return Null(), err
		}
		switch n.Op {
		case gqlparser.Not:
			if v.kind == kBool {
				return Bool(!v.b), nil
			}
			return Null(), nil
		case gqlparser.Neg:
			if v.kind == kInt {
				return Int(-v.i), nil
			}
			if v.kind == kFloat {
				return Float(-v.f), nil
			}
			return Null(), nil
		}
		return Null(), fmt.Errorf("%w: unary op %d", ErrUnsupported, n.Op)

	case *gqlparser.Binary:
		return e.evalBinary(r, n)

	case *gqlparser.Func:
		return e.evalFunc(r, n)

	case *gqlparser.ListExpr:
		list := make([]Value, len(n.Elems))
		for i, el := range n.Elems {
			v, err := e.eval(r, el)
			if err != nil {
				return Null(), err
			}
			list[i] = v
		}
		return List(list), nil

	case *gqlparser.In:
		v, err := e.eval(r, n.Expr)
		if err != nil {
			return Null(), err
		}
		lv, err := e.eval(r, n.List)
		if err != nil {
			return Null(), err
		}
		if lv.kind != kList {
			return Null(), nil
		}
		nullSeen := false
		for _, el := range lv.list {
			if el.kind == kNull || v.kind == kNull {
				nullSeen = true
				continue
			}
			if eqVal(v, el) {
				return Bool(true), nil
			}
		}
		if nullSeen {
			return Null(), nil
		}
		return Bool(false), nil

	case *gqlparser.IsNull:
		v, err := e.eval(r, n.Expr)
		if err != nil {
			return Null(), err
		}
		isNull := v.kind == kNull
		if n.Negated {
			isNull = !isNull
		}
		return Bool(isNull), nil

	case *gqlparser.IsTruth:
		v, err := e.eval(r, n.Expr)
		if err != nil {
			return Null(), err
		}
		var res bool
		if n.Want {
			res = v.kind == kBool && v.b
		} else {
			res = v.kind == kBool && !v.b
		}
		if n.Negated {
			res = !res
		}
		return Bool(res), nil

	case *gqlparser.IsTyped:
		v, err := e.eval(r, n.Expr)
		if err != nil {
			return Null(), err
		}
		res := typeMatches(v, n.Kind)
		if n.Negated {
			res = !res
		}
		return Bool(res), nil

	case *gqlparser.Case:
		return e.evalCase(r, n)

	case *gqlparser.Index:
		return e.evalIndex(r, n)

	case *gqlparser.Slice:
		return e.evalSlice(r, n)

	case *gqlparser.ListPred:
		return e.evalListPred(r, n)

	case *gqlparser.MapLit:
		m := make(map[string]Value, len(n.Fields))
		for _, f := range n.Fields {
			v, err := e.eval(r, f.Val)
			if err != nil {
				return Null(), err
			}
			m[f.Key] = v
		}
		return Map(m), nil

	case *gqlparser.HasLabelExpr:
		v, err := e.eval(r, &gqlparser.Var{Name: n.Var})
		if err != nil {
			return Null(), err
		}
		if v.kind != kNode {
			return Bool(false), nil
		}
		return Bool(labelMatches(v.node, n.Expr)), nil
	}

	return Null(), fmt.Errorf("%w: expression %T", ErrUnsupported, ex)
}

// evalBinary evaluates an infix operator.
func (e *exec) evalBinary(r row, n *gqlparser.Binary) (Value, error) {
	a, err := e.eval(r, n.LHS)
	if err != nil {
		return Null(), err
	}
	b, err := e.eval(r, n.RHS)
	if err != nil {
		return Null(), err
	}

	switch n.Op {
	case gqlparser.OpAnd:
		return andVal(a, b), nil
	case gqlparser.OpOr:
		return orVal(a, b), nil
	case gqlparser.OpXor:
		return xorVal(a, b), nil
	}

	if a.IsNum() || b.IsNum() {
		switch n.Op {
		case gqlparser.OpAdd, gqlparser.OpSub, gqlparser.OpMul, gqlparser.OpDiv:
			return arith(n.Op, a, b), nil
		}
	}

	switch n.Op {
	case gqlparser.OpEq, gqlparser.OpNeq, gqlparser.OpLt, gqlparser.OpLte,
		gqlparser.OpGt, gqlparser.OpGte:
		c, ok := compare(a, b)
		if !ok {
			return Null(), nil
		}
		switch n.Op {
		case gqlparser.OpEq:
			return Bool(c == 0), nil
		case gqlparser.OpNeq:
			return Bool(c != 0), nil
		case gqlparser.OpLt:
			return Bool(c < 0), nil
		case gqlparser.OpLte:
			return Bool(c <= 0), nil
		case gqlparser.OpGt:
			return Bool(c > 0), nil
		case gqlparser.OpGte:
			return Bool(c >= 0), nil
		}
	case gqlparser.OpAdd:
		return arith(n.Op, a, b), nil
	case gqlparser.OpSub, gqlparser.OpMul, gqlparser.OpDiv:
		return Null(), nil
	case gqlparser.OpConcat:
		return concatVal(a, b), nil
	case gqlparser.OpStartsWith, gqlparser.OpEndsWith, gqlparser.OpContains:
		if a.kind != kString || b.kind != kString {
			return Null(), nil
		}
		switch n.Op {
		case gqlparser.OpStartsWith:
			return Bool(strings.HasPrefix(a.s, b.s)), nil
		case gqlparser.OpEndsWith:
			return Bool(strings.HasSuffix(a.s, b.s)), nil
		}
		return Bool(strings.Contains(a.s, b.s)), nil
	}

	return Null(), fmt.Errorf("%w: binary op %d", ErrUnsupported, n.Op)
}

// evalFunc evaluates a scalar function call.
func (e *exec) evalFunc(r row, f *gqlparser.Func) (Value, error) {
	if isAggregate(f) {
		return Null(), fmt.Errorf(
			"%w: aggregate %q outside a projection", ErrUnsupported, f.Name)
	}

	args := make([]Value, len(f.Args))
	for i := range f.Args {
		v, err := e.eval(r, f.Args[i])
		if err != nil {
			return Null(), err
		}
		args[i] = v
	}

	switch strings.ToLower(f.Name) {
	case "labels":
		if len(args) != 1 || args[0].kind != kNode {
			return Null(), nil
		}
		return List([]Value{Str(args[0].node.label)}), nil
	case "type":
		if len(args) != 1 || args[0].kind != kRel {
			return Null(), nil
		}
		return Str(args[0].rel.typ), nil
	case "id":
		if len(args) != 1 {
			return Null(), nil
		}
		switch args[0].kind {
		case kNode:
			return Str(args[0].node.id), nil
		case kRel:
			return Str("r" + strconv.Itoa(args[0].rel.idx)), nil
		}
		return Null(), nil
	case "keys":
		if len(args) != 1 {
			return Null(), nil
		}
		var keys []string
		switch args[0].kind {
		case kNode:
			for k := range args[0].node.props {
				keys = append(keys, k)
			}
		case kRel:
			for k := range args[0].rel.props {
				keys = append(keys, k)
			}
		case kMap:
			for k := range args[0].mp {
				keys = append(keys, k)
			}
		default:
			return Null(), nil
		}
		sort.Strings(keys)
		list := make([]Value, len(keys))
		for i, k := range keys {
			list[i] = Str(k)
		}
		return List(list), nil
	case "size", "length":
		if len(args) != 1 {
			return Null(), nil
		}
		switch args[0].kind {
		case kList:
			return Int(int64(len(args[0].list))), nil
		case kString:
			return Int(int64(len(args[0].s))), nil
		}
		return Null(), nil
	case "coalesce":
		for _, a := range args {
			if a.kind != kNull {
				return a, nil
			}
		}
		return Null(), nil
	case "tostring":
		if len(args) != 1 {
			return Null(), nil
		}
		switch args[0].kind {
		case kNull:
			return Null(), nil
		case kString:
			return args[0], nil
		case kBool:
			return Str(strconv.FormatBool(args[0].b)), nil
		case kInt:
			return Str(strconv.FormatInt(args[0].i, 10)), nil
		case kFloat:
			return Str(strconv.FormatFloat(args[0].f, 'g', -1, 64)), nil
		}
		return Null(), nil
	case "tointeger":
		if len(args) != 1 {
			return Null(), nil
		}
		switch args[0].kind {
		case kInt:
			return args[0], nil
		case kFloat:
			return Int(int64(args[0].f)), nil
		case kString:
			n, err := strconv.ParseInt(args[0].s, 10, 64)
			if err != nil {
				return Null(), nil
			}
			return Int(n), nil
		}
		return Null(), nil
	case "tofloat":
		if len(args) != 1 {
			return Null(), nil
		}
		if fv, ok := args[0].Float64(); ok {
			return Float(fv), nil
		}
		if args[0].kind == kString {
			fv, err := strconv.ParseFloat(args[0].s, 64)
			if err != nil {
				return Null(), nil
			}
			return Float(fv), nil
		}
		return Null(), nil
	}

	return Null(), fmt.Errorf("%w: function %q", ErrUnsupported, f.Name)
}

// evalCase evaluates a CASE expression.
func (e *exec) evalCase(r row, n *gqlparser.Case) (Value, error) {
	if n.Operand != nil {
		op, err := e.eval(r, n.Operand)
		if err != nil {
			return Null(), err
		}
		for _, w := range n.Whens {
			c, err := e.eval(r, w.Cond)
			if err != nil {
				return Null(), err
			}
			if eqVal(op, c) {
				return e.eval(r, w.Result)
			}
		}
	} else {
		for _, w := range n.Whens {
			c, err := e.eval(r, w.Cond)
			if err != nil {
				return Null(), err
			}
			if truth(c) {
				return e.eval(r, w.Result)
			}
		}
	}
	if n.Else != nil {
		return e.eval(r, n.Else)
	}
	return Null(), nil
}

// evalIndex evaluates base[index] over lists and strings.
func (e *exec) evalIndex(r row, n *gqlparser.Index) (Value, error) {
	base, err := e.eval(r, n.Base)
	if err != nil {
		return Null(), err
	}
	idx, err := e.eval(r, n.Idx)
	if err != nil {
		return Null(), err
	}
	fi, ok := idx.Float64()
	if !ok {
		return Null(), nil
	}
	i := int(fi)

	switch base.kind {
	case kList:
		return listAt(base.list, i), nil
	case kString:
		runes := []rune(base.s)
		if v := listAtRunes(runes, i); v != nil {
			return Str(string(*v)), nil
		}
	}
	return Null(), nil
}

// evalSlice evaluates base[from..to].
func (e *exec) evalSlice(r row, n *gqlparser.Slice) (Value, error) {
	base, err := e.eval(r, n.Base)
	if err != nil {
		return Null(), err
	}
	if base.kind != kList {
		return Null(), nil
	}
	lo, hi := 0, len(base.list)
	if n.From != nil {
		v, err := e.eval(r, n.From)
		if err != nil {
			return Null(), err
		}
		if f, ok := v.Float64(); ok {
			lo = clampIndex(int(f), len(base.list))
		}
	}
	if n.To != nil {
		v, err := e.eval(r, n.To)
		if err != nil {
			return Null(), err
		}
		if f, ok := v.Float64(); ok {
			hi = clampIndex(int(f), len(base.list))
		}
	}
	if lo > hi {
		return List(nil), nil
	}
	out := make([]Value, hi-lo)
	copy(out, base.list[lo:hi])
	return List(out), nil
}

// evalListPred evaluates all/any/none/single predicates.
func (e *exec) evalListPred(r row, n *gqlparser.ListPred) (Value, error) {
	lv, err := e.eval(r, n.List)
	if err != nil {
		return Null(), err
	}
	if lv.kind != kList {
		return Null(), nil
	}

	hits := 0
	for _, el := range lv.list {
		sub := r.clone()
		sub.vals[n.Var] = el
		pv, err := e.eval(sub, n.Pred)
		if err != nil {
			return Null(), err
		}
		if truth(pv) {
			hits++
		}
	}

	switch n.Quant {
	case gqlparser.QuantAll:
		return Bool(hits == len(lv.list)), nil
	case gqlparser.QuantAny:
		return Bool(hits > 0), nil
	case gqlparser.QuantNone:
		return Bool(hits == 0), nil
	case gqlparser.QuantSingle:
		return Bool(hits == 1), nil
	}
	return Null(), nil
}

// propOf reads key from a node, relationship, or map value.
func propOf(base Value, key string) Value {
	switch base.kind {
	case kNode:
		if v, ok := base.node.props[key]; ok {
			return v
		}
	case kRel:
		if v, ok := base.rel.props[key]; ok {
			return v
		}
	case kMap:
		if v, ok := base.mp[key]; ok {
			return v
		}
	}
	return Null()
}

// andVal is three-valued AND.
func andVal(a, b Value) Value {
	if (a.kind == kBool && !a.b) || (b.kind == kBool && !b.b) {
		return Bool(false)
	}
	if a.kind == kNull || b.kind == kNull {
		return Null()
	}
	return Bool(truth(a) && truth(b))
}

// orVal is three-valued OR.
func orVal(a, b Value) Value {
	if (a.kind == kBool && a.b) || (b.kind == kBool && b.b) {
		return Bool(true)
	}
	if a.kind == kNull || b.kind == kNull {
		return Null()
	}
	return Bool(false)
}

// xorVal is three-valued XOR.
func xorVal(a, b Value) Value {
	if a.kind == kNull || b.kind == kNull {
		return Null()
	}
	return Bool(truth(a) != truth(b))
}

// arith evaluates + - * / with GQL null semantics; + also concatenates
// strings and lists.
func arith(op gqlparser.BinOp, a, b Value) Value {
	if op == gqlparser.OpAdd {
		switch {
		case a.kind == kString && b.kind == kString:
			return Str(a.s + b.s)
		case a.kind == kList && b.kind == kList:
			out := make([]Value, 0, len(a.list)+len(b.list))
			out = append(out, a.list...)
			out = append(out, b.list...)
			return List(out)
		}
	}

	af, aok := a.Float64()
	bf, bok := b.Float64()
	if !aok || !bok {
		return Null()
	}
	intBoth := a.kind == kInt && b.kind == kInt

	switch op {
	case gqlparser.OpAdd:
		if intBoth {
			return Int(a.i + b.i)
		}
		return Float(af + bf)
	case gqlparser.OpSub:
		if intBoth {
			return Int(a.i - b.i)
		}
		return Float(af - bf)
	case gqlparser.OpMul:
		if intBoth {
			return Int(a.i * b.i)
		}
		return Float(af * bf)
	case gqlparser.OpDiv:
		if bf == 0 {
			return Null()
		}
		if intBoth {
			return Int(a.i / b.i)
		}
		return Float(af / bf)
	}
	return Null()
}

// concatVal is the || operator: strings or lists only.
func concatVal(a, b Value) Value {
	switch {
	case a.kind == kString && b.kind == kString:
		return Str(a.s + b.s)
	case a.kind == kList && b.kind == kList:
		out := make([]Value, 0, len(a.list)+len(b.list))
		out = append(out, a.list...)
		out = append(out, b.list...)
		return List(out)
	}
	return Null()
}

// typeMatches implements IS TYPED.
func typeMatches(v Value, kind string) bool {
	switch strings.ToLower(kind) {
	case "integer":
		return v.kind == kInt
	case "float":
		return v.kind == kFloat
	case "string":
		return v.kind == kString
	case "boolean":
		return v.kind == kBool
	case "list":
		return v.kind == kList
	case "map":
		return v.kind == kMap
	case "node":
		return v.kind == kNode
	case "relationship":
		return v.kind == kRel
	}
	return false
}

// labelMatches tests a label expression against a node.
func labelMatches(n *node, le *gqlparser.LabelExpr) bool {
	if le == nil {
		return true
	}
	switch le.Kind {
	case gqlparser.LabelName:
		return n.label == le.Name
	case gqlparser.LabelAnd:
		return labelMatches(n, le.L) && labelMatches(n, le.R)
	case gqlparser.LabelOr:
		return labelMatches(n, le.L) || labelMatches(n, le.R)
	case gqlparser.LabelNot:
		return !labelMatches(n, le.L)
	case gqlparser.LabelWild:
		return n.label != ""
	}
	return false
}

// listAt returns the element at i, with negative indices from the end.
func listAt(list []Value, i int) Value {
	if i < 0 {
		i += len(list)
	}
	if i < 0 || i >= len(list) {
		return Null()
	}
	return list[i]
}

// listAtRunes is listAt over a rune slice, returning a pointer to the rune.
func listAtRunes(runes []rune, i int) *rune {
	if i < 0 {
		i += len(runes)
	}
	if i < 0 || i >= len(runes) {
		return nil
	}
	return &runes[i]
}

// clampIndex normalizes a possibly negative list index into [0, n].
func clampIndex(i, n int) int {
	if i < 0 {
		i += n
	}
	if i < 0 {
		return 0
	}
	if i > n {
		return n
	}
	return i
}

var (
	// ErrGraph marks failures while snapshotting the graph instance.
	ErrGraph = errors.New("gql graph error")
	// ErrParse marks GQL syntax errors.
	ErrParse = errors.New("gql parse error")
	// ErrUnsupported marks valid GQL constructs the native executor does not
	// implement yet.
	ErrUnsupported = errors.New("gql unsupported")
)

// exec runs a parsed query against one store snapshot.
type exec struct {
	store *store
}

// row is one binding environment flowing through the clauses.
type row struct {
	vals map[string]Value
	// used is the set of relationship indices taken in the current MATCH
	// clause (relationship uniqueness).
	used map[int]struct{}
}

// outRow pairs a projected row with its source row, so ORDER BY expressions
// can still reach pre-projection variables.
type outRow struct {
	src row
	out map[string]Value
}

func newRow() row { return row{vals: make(map[string]Value, 4)} }

// clone deep-copies the row's maps (bindings and the used-rel set).
func (r row) clone() row {
	n := row{vals: make(map[string]Value, len(r.vals))}
	for k, v := range r.vals {
		n.vals[k] = v
	}
	if r.used != nil {
		n.used = make(map[int]struct{}, len(r.used))
		for k := range r.used {
			n.used[k] = struct{}{}
		}
	}
	return n
}

func (r row) bind(name string, v Value) row {
	n := r.clone()
	n.vals[name] = v
	return n
}

func (r row) markUsed(idx int) row {
	n := r.clone()
	if n.used == nil {
		n.used = make(map[int]struct{}, 2)
	}
	n.used[idx] = struct{}{}
	return n
}

// query executes a whole query and returns projected rows plus column names
// in output order.
func (e *exec) query(q *gqlparser.Query) ([]outRow, []string, error) {
	if q == nil || len(q.Parts) == 0 {
		return nil, nil, fmt.Errorf("%w: empty query", ErrParse)
	}

	if q.Mode == gqlparser.Explain || q.Mode == gqlparser.Profile {
		return []outRow{{out: map[string]Value{"plan": Str(explainQuery(q))}}},
			[]string{"plan"}, nil
	}

	var combined []outRow
	var cols []string

	for i := range q.Parts {
		paginate := len(q.Parts) == 1
		rows, partCols, err := e.runPart(&q.Parts[i], paginate)
		if err != nil {
			return nil, nil, err
		}

		if i == 0 {
			combined, cols = rows, partCols
			continue
		}
		if !sameStrings(cols, partCols) {
			return nil, nil, fmt.Errorf(
				"%w: UNION parts project different columns: %v vs %v",
				ErrUnsupported, cols, partCols)
		}

		switch q.Union[i-1] {
		case gqlparser.UnionAll:
			combined = append(combined, rows...)
		case gqlparser.UnionDistinct:
			combined = dedupeOut(append(combined, rows...))
		default:
			return nil, nil, fmt.Errorf("%w: UNION kind %d",
				ErrUnsupported, q.Union[i-1])
		}
	}

	if len(q.Parts) > 1 {
		var err error
		combined, err = e.orderPage(combined, &q.Parts[len(q.Parts)-1].Ret)
		if err != nil {
			return nil, nil, err
		}
	}

	return combined, cols, nil
}

// runPart executes one UNION branch: clauses, then the final projection.
func (e *exec) runPart(
	part *gqlparser.QueryPart, paginate bool,
) ([]outRow, []string, error) {
	rows := []row{newRow()}

	for _, clause := range part.Clauses {
		var err error
		switch n := clause.(type) {
		case *gqlparser.Match:
			rows, err = e.match(rows, n)
		case *gqlparser.With:
			rows, err = e.with(rows, n)
		default:
			err = fmt.Errorf("%w: clause %T", ErrUnsupported, clause)
		}
		if err != nil {
			return nil, nil, err
		}
	}

	return e.project(rows, &part.Ret, paginate)
}

// match applies one MATCH / OPTIONAL MATCH clause.
func (e *exec) match(rows []row, m *gqlparser.Match) ([]row, error) {
	var out []row

	for _, r0 := range rows {
		// relationship uniqueness is scoped to one MATCH clause
		r0.used = nil

		cur := []row{r0}
		for pi := range m.Patterns {
			var err error
			cur, err = e.expandPattern(cur, &m.Patterns[pi], pi)
			if err != nil {
				return nil, err
			}
		}

		if m.Where != nil {
			var err error
			cur, err = e.filter(cur, m.Where)
			if err != nil {
				return nil, err
			}
		}

		if len(cur) == 0 && m.Optional {
			// left-join semantics: keep the row, null the clause's variables
			nr := r0.clone()
			for _, name := range patternVars(m.Patterns) {
				if _, ok := nr.vals[name]; !ok {
					nr.vals[name] = Null()
				}
			}
			out = append(out, nr)
			continue
		}

		out = append(out, cur...)
	}

	return out, nil
}

// with applies a projection boundary (WITH / RETURN ... NEXT / FILTER).
func (e *exec) with(rows []row, w *gqlparser.With) ([]row, error) {
	projected, cols, err := e.project(rows, &w.Proj, true)
	if err != nil {
		return nil, err
	}

	out := make([]row, 0, len(projected))
	for _, p := range projected {
		nr := newRow()
		for _, c := range cols {
			nr.vals[c] = p.out[c]
		}
		out = append(out, nr)
	}

	if w.Where != nil {
		out, err = e.filter(out, w.Where)
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

// expandPattern matches one linear path pattern, joining onto rows.
func (e *exec) expandPattern(
	rows []row, p *gqlparser.Pattern, pidx int,
) ([]row, error) {
	startVar := p.Start.Var
	if startVar == "" {
		startVar = fmt.Sprintf("__n%d_0", pidx)
	}

	rows, err := e.matchNode(rows, &p.Start, startVar)
	if err != nil {
		return nil, err
	}

	curVar := startVar
	for hi := range p.Hops {
		hop := &p.Hops[hi]
		nodeVar := hop.Node.Var
		if nodeVar == "" {
			nodeVar = fmt.Sprintf("__n%d_%d", pidx, hi+1)
		}

		rows, err = e.matchHop(rows, curVar, &hop.Rel, hop.Rel.Var,
			&hop.Node, nodeVar)
		if err != nil {
			return nil, err
		}
		curVar = nodeVar
	}

	return rows, nil
}

// matchNode joins the node pattern np, binding varName.
func (e *exec) matchNode(
	rows []row, np *gqlparser.NodePat, varName string,
) ([]row, error) {
	var out []row

	for _, r := range rows {
		if v, ok := r.vals[varName]; ok {
			if v.kind != kNode {
				return nil, fmt.Errorf("variable %q is not a node", varName)
			}
			if nodeMatches(v.node, np) {
				out = append(out, r)
			}
			continue
		}

		for _, idx := range e.nodeCandidates(np) {
			n := e.store.nodes[idx]
			if !nodeMatches(n, np) {
				continue
			}
			out = append(out, r.bind(varName, nodeVal(n)))
		}
	}

	return out, nil
}

// matchHop expands one relationship step from srcVar.
func (e *exec) matchHop(
	rows []row, srcVar string, rp *gqlparser.RelPat, relVar string,
	np *gqlparser.NodePat, nodeVar string,
) ([]row, error) {
	if rp.Length != nil {
		return nil, fmt.Errorf("%w: variable-length relationships", ErrUnsupported)
	}

	var out []row

	for _, r := range rows {
		src, ok := r.vals[srcVar]
		if !ok || src.kind != kNode {
			return nil, fmt.Errorf("variable %q is not a node", srcVar)
		}
		srcIdx := e.store.byID[src.node.id]

		for _, rr := range e.store.neighbors(srcIdx, rp.Dir) {
			if !e.relMatches(rr, rp) {
				continue
			}
			if _, used := r.used[rr.idx]; used {
				continue
			}

			// rel variable already bound: constraint, not a binding
			if relVar != "" {
				if v, ok := r.vals[relVar]; ok {
					if v.kind != kRel || v.rel.idx != rr.idx {
						continue
					}
				}
			}

			nbIdx := rr.other(srcIdx)
			nb := e.store.nodes[nbIdx]
			if !nodeMatches(nb, np) {
				continue
			}
			if v, ok := r.vals[nodeVar]; ok {
				if v.kind != kNode || v.node.id != nb.id {
					continue
				}
			}

			nr := r.markUsed(rr.idx)
			if relVar != "" {
				if _, ok := nr.vals[relVar]; !ok {
					nr = nr.bind(relVar, relVal(rr))
				}
			}
			if _, ok := nr.vals[nodeVar]; !ok {
				nr = nr.bind(nodeVar, nodeVal(nb))
			}
			out = append(out, nr)
		}
	}

	return out, nil
}

// nodeCandidates returns the label-scan candidates for a node pattern.
func (e *exec) nodeCandidates(np *gqlparser.NodePat) []int {
	if len(np.Labels) == 0 {
		all := make([]int, len(e.store.nodes))
		for i := range e.store.nodes {
			all[i] = i
		}
		return all
	}
	return e.store.byLabel[np.Labels[0]]
}

// nodeMatches applies label and inline-property filters.
func nodeMatches(n *node, np *gqlparser.NodePat) bool {
	for _, label := range np.Labels {
		if n.label != label {
			return false
		}
	}
	for _, pe := range np.Props {
		if !propMatches(n.props, pe.Key, pe.Val) {
			return false
		}
	}
	return true
}

// relMatches applies type and inline-property filters.
func (e *exec) relMatches(r *rel, rp *gqlparser.RelPat) bool {
	if len(rp.Types) > 0 {
		found := false
		for _, t := range rp.Types {
			if r.typ == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, pe := range rp.Props {
		if !propMatches(r.props, pe.Key, pe.Val) {
			return false
		}
	}
	return true
}

// propMatches is inline-property equality: an absent property equals only a
// null literal.
func propMatches(
	props map[string]Value, key string, lit gqlparser.Literal,
) bool {
	actual, ok := props[key]
	want := litVal(lit)
	if !ok {
		return want.kind == kNull
	}
	return eqVal(actual, want)
}

// filter keeps the rows whose WHERE expression is true.
func (e *exec) filter(rows []row, where gqlparser.Expr) ([]row, error) {
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		v, err := e.eval(r, where)
		if err != nil {
			return nil, err
		}
		if truth(v) {
			out = append(out, r)
		}
	}
	return out, nil
}

// project applies a projection (final or WITH) with ORDER BY / SKIP / LIMIT.
func (e *exec) project(
	rows []row, p *gqlparser.Projection, paginate bool,
) ([]outRow, []string, error) {
	names, exprs, err := e.projectionItems(rows, p)
	if err != nil {
		return nil, nil, err
	}

	hasAgg := false
	for _, ex := range exprs {
		if isAggregate(ex) {
			hasAgg = true
			continue
		}
		if containsAggregate(ex) {
			return nil, nil, fmt.Errorf(
				"%w: aggregate nested in an expression", ErrUnsupported)
		}
	}

	if hasAgg {
		return e.projectAggregate(rows, names, exprs, p, paginate)
	}

	projected := make([]outRow, 0, len(rows))
	for _, r := range rows {
		out := make(map[string]Value, len(names))
		for i, ex := range exprs {
			v, err := e.eval(r, ex)
			if err != nil {
				return nil, nil, err
			}
			out[names[i]] = v
		}
		projected = append(projected, outRow{src: r, out: out})
	}

	if p.Distinct {
		projected = dedupeOut(projected)
	}
	if paginate {
		projected, err = e.orderPage(projected, p)
		if err != nil {
			return nil, nil, err
		}
	}

	return projected, names, nil
}

// projectionItems expands `*` and computes output column names.
func (e *exec) projectionItems(
	rows []row, p *gqlparser.Projection,
) ([]string, []gqlparser.Expr, error) {
	var names []string
	var exprs []gqlparser.Expr

	if p.Star {
		scope := e.scopeOf(rows)
		for _, name := range scope {
			names = append(names, name)
			exprs = append(exprs, &gqlparser.Var{Name: name})
		}
	}

	for i, item := range p.Items {
		name := item.Alias
		if name == "" {
			if v, ok := item.Expr.(*gqlparser.Var); ok {
				name = v.Name
			}
		}
		if name == "" {
			name = fmt.Sprintf("expr_%d", i+1)
		}
		if _, dup := indexOf(names, name); dup {
			return nil, nil, fmt.Errorf(
				"%w: duplicate output column %q", ErrUnsupported, name)
		}
		names = append(names, name)
		exprs = append(exprs, item.Expr)
	}

	if len(names) == 0 {
		return nil, nil, fmt.Errorf("%w: empty projection", ErrUnsupported)
	}
	return names, exprs, nil
}

// projectAggregate runs an implicit GROUP BY over the non-aggregate items.
func (e *exec) projectAggregate(
	rows []row, names []string, exprs []gqlparser.Expr,
	p *gqlparser.Projection, paginate bool,
) ([]outRow, []string, error) {
	type group struct {
		src  row
		rows []row
	}

	groups := make(map[string]*group)
	var order []string

	for _, r := range rows {
		var key strings.Builder
		for i, ex := range exprs {
			if isAggregate(ex) {
				key.WriteString("#")
				key.WriteString(fmt.Sprint(i))
				key.WriteByte(':')
				continue
			}
			v, err := e.eval(r, ex)
			if err != nil {
				return nil, nil, err
			}
			key.WriteString(v.key())
			key.WriteByte(0)
		}
		k := key.String()
		g, ok := groups[k]
		if !ok {
			g = &group{src: r}
			groups[k] = g
			order = append(order, k)
		}
		g.rows = append(g.rows, r)
	}

	projected := make([]outRow, 0, len(order))
	for _, k := range order {
		g := groups[k]
		out := make(map[string]Value, len(names))
		for i, ex := range exprs {
			if isAggregate(ex) {
				v, err := e.evalAggregate(g.rows, ex.(*gqlparser.Func))
				if err != nil {
					return nil, nil, err
				}
				out[names[i]] = v
				continue
			}
			v, err := e.eval(g.src, ex)
			if err != nil {
				return nil, nil, err
			}
			out[names[i]] = v
		}
		projected = append(projected, outRow{src: g.src, out: out})
	}

	if p.Distinct {
		projected = dedupeOut(projected)
	}
	if paginate {
		var err error
		projected, err = e.orderPage(projected, p)
		if err != nil {
			return nil, nil, err
		}
	}

	return projected, names, nil
}

// evalAggregate computes one aggregate function over a group.
func (e *exec) evalAggregate(rows []row, f *gqlparser.Func) (Value, error) {
	name := strings.ToLower(f.Name)

	switch name {
	case "count":
		if f.Star {
			return Int(int64(len(rows))), nil
		}
		if len(f.Args) != 1 {
			return Null(), fmt.Errorf("%w: count() arity", ErrUnsupported)
		}
		var n int64
		seen := make(map[string]struct{})
		for _, r := range rows {
			v, err := e.eval(r, f.Args[0])
			if err != nil {
				return Null(), err
			}
			if v.kind == kNull {
				continue
			}
			if f.Distinct {
				k := v.key()
				if _, ok := seen[k]; ok {
					continue
				}
				seen[k] = struct{}{}
			}
			n++
		}
		return Int(n), nil

	case "sum", "avg", "min", "max":
		if len(f.Args) != 1 {
			return Null(), fmt.Errorf("%w: %s() arity", ErrUnsupported, name)
		}
		var vals []Value
		for _, r := range rows {
			v, err := e.eval(r, f.Args[0])
			if err != nil {
				return Null(), err
			}
			if v.kind == kNull {
				continue
			}
			if !v.IsNum() {
				continue
			}
			vals = append(vals, v)
		}
		switch name {
		case "sum":
			if len(vals) == 0 {
				return Int(0), nil
			}
			allInt := true
			var fi float64
			var ii int64
			for _, v := range vals {
				if v.kind == kInt {
					ii += v.i
				} else {
					allInt = false
				}
				f, _ := v.Float64()
				fi += f
			}
			if allInt {
				return Int(ii), nil
			}
			return Float(fi), nil
		case "avg":
			if len(vals) == 0 {
				return Null(), nil
			}
			var fi float64
			for _, v := range vals {
				f, _ := v.Float64()
				fi += f
			}
			return Float(fi / float64(len(vals))), nil
		case "min":
			if len(vals) == 0 {
				return Null(), nil
			}
			m := vals[0]
			for _, v := range vals[1:] {
				if c, ok := compare(v, m); ok && c < 0 {
					m = v
				}
			}
			return m, nil
		case "max":
			if len(vals) == 0 {
				return Null(), nil
			}
			m := vals[0]
			for _, v := range vals[1:] {
				if c, ok := compare(v, m); ok && c > 0 {
					m = v
				}
			}
			return m, nil
		}

	case "collect":
		if len(f.Args) != 1 {
			return Null(), fmt.Errorf("%w: collect() arity", ErrUnsupported)
		}
		list := make([]Value, 0, len(rows))
		seen := make(map[string]struct{})
		for _, r := range rows {
			v, err := e.eval(r, f.Args[0])
			if err != nil {
				return Null(), err
			}
			if v.kind == kNull {
				continue
			}
			if f.Distinct {
				k := v.key()
				if _, ok := seen[k]; ok {
					continue
				}
				seen[k] = struct{}{}
			}
			list = append(list, v)
		}
		return List(list), nil
	}

	return Null(), fmt.Errorf("%w: aggregate %q", ErrUnsupported, f.Name)
}

// orderPage applies ORDER BY, then SKIP/OFFSET and LIMIT.
func (e *exec) orderPage(
	rows []outRow, p *gqlparser.Projection,
) ([]outRow, error) {
	if len(p.OrderBy) > 0 {
		type keyed struct {
			row  outRow
			keys []Value
		}

		ks := make([]keyed, len(rows))
		for i := range rows {
			ks[i].row = rows[i]
			ks[i].keys = make([]Value, len(p.OrderBy))
			for j, si := range p.OrderBy {
				v, err := e.sortKey(rows[i], si.Expr)
				if err != nil {
					return nil, err
				}
				ks[i].keys[j] = v
			}
		}

		sort.SliceStable(ks, func(a, b int) bool {
			for j, si := range p.OrderBy {
				c := orderCompare(ks[a].keys[j], ks[b].keys[j])
				if c == 0 {
					continue
				}
				if si.Desc {
					return c > 0
				}
				return c < 0
			}
			return false
		})

		for i := range ks {
			rows[i] = ks[i].row
		}
	}

	if p.Skip != nil {
		n := int(*p.Skip)
		if n >= len(rows) {
			rows = nil
		} else {
			rows = rows[n:]
		}
	}
	if p.Limit != nil {
		n := int(*p.Limit)
		if n < len(rows) {
			rows = rows[:n]
		}
	}

	return rows, nil
}

// sortKey resolves one ORDER BY key: an output column alias first, then the
// source row scope.
func (e *exec) sortKey(r outRow, ex gqlparser.Expr) (Value, error) {
	if v, ok := ex.(*gqlparser.Var); ok {
		if val, ok := r.out[v.Name]; ok {
			return val, nil
		}
	}
	if r.src.vals == nil {
		return Null(), fmt.Errorf(
			"%w: ORDER BY after UNION may only reference output columns",
			ErrUnsupported)
	}
	return e.eval(r.src, ex)
}

// scopeOf returns the visible variable names (sorted, hidden ones excluded).
func (e *exec) scopeOf(rows []row) []string {
	if len(rows) == 0 {
		return nil
	}
	names := make([]string, 0, len(rows[0].vals))
	for name := range rows[0].vals {
		if strings.HasPrefix(name, "__") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// dedupeOut removes duplicate projected rows, keeping the first occurrence.
func dedupeOut(rows []outRow) []outRow {
	seen := make(map[string]struct{}, len(rows))
	out := rows[:0]
	for _, r := range rows {
		keys := make([]string, 0, len(r.out))
		for k := range r.out {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			b.WriteString(k)
			b.WriteByte(0)
			b.WriteString(r.out[k].key())
			b.WriteByte(0)
		}
		key := b.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	return out
}

// patternVars lists the non-hidden variables a pattern introduces, in order.
func patternVars(patterns []gqlparser.Pattern) []string {
	seen := make(map[string]struct{})
	var ret []string
	add := func(name string) {
		if name == "" || strings.HasPrefix(name, "__") {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		ret = append(ret, name)
	}

	for i := range patterns {
		p := &patterns[i]
		add(p.Start.Var)
		for _, hop := range p.Hops {
			add(hop.Rel.Var)
			add(hop.Node.Var)
		}
	}
	return ret
}

// isAggregate reports whether e is an aggregate function call.
func isAggregate(e gqlparser.Expr) bool {
	f, ok := e.(*gqlparser.Func)
	if !ok {
		return false
	}
	switch strings.ToLower(f.Name) {
	case "count", "sum", "avg", "min", "max", "collect":
		return true
	}
	return false
}

// containsAggregate reports whether e contains an aggregate call anywhere.
func containsAggregate(e gqlparser.Expr) bool {
	found := false
	walkExpr(e, func(x gqlparser.Expr) bool {
		if isAggregate(x) {
			found = true
			return false
		}
		return true
	})
	return found
}

// walkExpr visits an expression tree (a local, non-mutating copy of the
// upstream walker that does not descend into pattern subqueries).
func walkExpr(e gqlparser.Expr, f func(gqlparser.Expr) bool) {
	if e == nil || !f(e) {
		return
	}
	switch n := e.(type) {
	case *gqlparser.Unary:
		walkExpr(n.Expr, f)
	case *gqlparser.Binary:
		walkExpr(n.LHS, f)
		walkExpr(n.RHS, f)
	case *gqlparser.Func:
		for _, a := range n.Args {
			walkExpr(a, f)
		}
	case *gqlparser.ListExpr:
		for _, el := range n.Elems {
			walkExpr(el, f)
		}
	case *gqlparser.In:
		walkExpr(n.Expr, f)
		walkExpr(n.List, f)
	case *gqlparser.IsNull:
		walkExpr(n.Expr, f)
	case *gqlparser.IsTruth:
		walkExpr(n.Expr, f)
	case *gqlparser.IsTyped:
		walkExpr(n.Expr, f)
	case *gqlparser.ListPred:
		walkExpr(n.List, f)
		walkExpr(n.Pred, f)
	case *gqlparser.Case:
		walkExpr(n.Operand, f)
		for _, w := range n.Whens {
			walkExpr(w.Cond, f)
			walkExpr(w.Result, f)
		}
		walkExpr(n.Else, f)
	case *gqlparser.Index:
		walkExpr(n.Base, f)
		walkExpr(n.Idx, f)
	case *gqlparser.Slice:
		walkExpr(n.Base, f)
		walkExpr(n.From, f)
		walkExpr(n.To, f)
	case *gqlparser.PropOf:
		walkExpr(n.Base, f)
	case *gqlparser.MapLit:
		for _, fl := range n.Fields {
			walkExpr(fl.Val, f)
		}
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// explainQuery renders a short structural plan for EXPLAIN/PROFILE.
func explainQuery(q *gqlparser.Query) string {
	var b strings.Builder
	b.WriteString("native netgraph executor (in-memory, no planner)\n")
	for i, part := range q.Parts {
		fmt.Fprintf(&b, "part %d:\n", i+1)
		for _, clause := range part.Clauses {
			switch n := clause.(type) {
			case *gqlparser.Match:
				opt := ""
				if n.Optional {
					opt = "OPTIONAL "
				}
				fmt.Fprintf(&b, "  %sMATCH %d pattern(s)", opt, len(n.Patterns))
				if n.Where != nil {
					b.WriteString(" WHERE <expr>")
				}
				b.WriteString("\n")
			case *gqlparser.With:
				fmt.Fprintf(&b, "  WITH %d item(s)\n", len(n.Proj.Items))
			}
		}
		fmt.Fprintf(&b, "  RETURN %d item(s)\n", len(part.Ret.Items))
	}
	return b.String()
}

// label constants of the graph model.
const (
	labelMachine = "Machine"
	labelState   = "State"
	labelTag     = "Tag"
)

// node is one queryable vertex.
type node struct {
	// id is the dominikbraun/graph vertex hash.
	id    string
	label string
	props map[string]Value
}

// rel is one logical relationship. A single dominikbraun/graph edge may
// expand into several (state relations are lists on one edge).
type rel struct {
	idx    int
	from   int // source node index
	to     int // target node index
	fromID string
	toID   string
	typ    string
	props  map[string]Value
}

// store is a read-only snapshot view of the in-memory graph instance.
type store struct {
	nodes []*node
	byID  map[string]int
	rels  []*rel
	out   [][]*rel // node index -> outgoing rels
	in    [][]*rel // node index -> incoming rels

	byLabel map[string][]int
}

// newStore snapshots g into a queryable store. The caller passes an already
// cloned graph, so no locking is needed.
func newStore(g *amgraph.Graph) (*store, error) {
	if g == nil || g.G == nil {
		return nil, fmt.Errorf("%w: nil graph", ErrGraph)
	}

	adj, err := g.G.AdjacencyMap()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGraph, err)
	}

	s := &store{
		byID:    make(map[string]int, len(adj)),
		byLabel: make(map[string][]int),
	}

	// vertices; the adjacency map covers isolated ones too
	for hash := range adj {
		v, err := g.G.Vertex(hash)
		if err != nil {
			continue
		}
		s.addVertex(hash, v, g.Machines)
	}

	// edges, expanded into logical relationships
	for srcHash, targets := range adj {
		src, ok := s.byID[srcHash]
		if !ok {
			continue
		}
		for tgtHash, edge := range targets {
			dst, ok := s.byID[tgtHash]
			if !ok {
				continue
			}
			data, _ := edge.Properties.Data.(*amgraph.EdgeData)
			s.addEdgeData(src, dst, data)
		}
	}

	// adjacency indices
	s.out = make([][]*rel, len(s.nodes))
	s.in = make([][]*rel, len(s.nodes))
	for _, r := range s.rels {
		s.out[r.from] = append(s.out[r.from], r)
		s.in[r.to] = append(s.in[r.to], r)
	}

	return s, nil
}

// addVertex appends one vertex in the graph model.
func (s *store) addVertex(
	hash string, v *amgraph.Vertex, machines map[string]*amgraph.Machine,
) {
	if v == nil {
		return
	}

	n := &node{id: hash, props: make(map[string]Value, 6)}
	switch {
	case v.StateName != "":
		n.label = labelState
		n.props["mach"] = Str(v.MachId)
		n.props["name"] = Str(v.StateName)
	case v.TagName != "":
		n.label = labelTag
		n.props["mach"] = Str(v.MachId)
		n.props["name"] = Str(v.TagName)
		n.props["value"] = Str(amtypes.TagValue(v.TagName))
	default:
		n.label = labelMachine
		n.props["id"] = Str(v.MachId)
	}

	if mach := machines[v.MachId]; mach != nil && mach.Serialized != nil {
		switch n.label {
		case labelMachine:
			n.props["id"] = Str(mach.ID)
			n.props["parent"] = Str(mach.Parent)
			n.props["schema_hash"] = Str(mach.SchemaHash)
			n.props["mtime_sum"] = Int(int64(mach.MTimeSum))
			n.props["queue_tick"] = Int(int64(mach.QueueTick))
			n.props["mach_tick"] = Int(int64(mach.MachineTick))
		case labelState:
			n.props["mach"] = Str(mach.ID)
			n.props["tick"] = Int(0)
			n.props["active"] = Bool(false)
			if i, ok := indexOf(mach.StateNames, v.StateName); ok &&
				i < len(mach.Time) {
				n.props["tick"] = Int(int64(mach.Time[i]))
				n.props["active"] = Bool(am.IsActiveTick(mach.Time[i]))
			}
		case labelTag:
			n.props["mach"] = Str(mach.ID)
		}
	}

	idx := len(s.nodes)
	s.nodes = append(s.nodes, n)
	s.byID[n.id] = idx
	s.byLabel[n.label] = append(s.byLabel[n.label], idx)
}

// addEdgeData appends the logical relationships carried by one edge between
// two node indices.
func (s *store) addEdgeData(src, dst int, data *amgraph.EdgeData) {
	if data == nil {
		return
	}
	from, to := s.nodes[src], s.nodes[dst]

	add := func(typ string, props map[string]Value) {
		s.rels = append(s.rels, &rel{
			idx:    len(s.rels),
			from:   src,
			to:     dst,
			fromID: from.id,
			toID:   to.id,
			typ:    typ,
			props:  props,
		})
	}

	if data.MachHas != nil {
		props := make(map[string]Value, 3)
		props["auto"] = Bool(data.MachHas.Auto)
		props["multi"] = Bool(data.MachHas.Multi)
		add("HAS", props)
	}
	if data.MachHasTag {
		add("HAS_TAG", nil)
	}
	if data.MachChildOf {
		add("CHILD_OF", nil)
	}
	if data.MachConnectedTo {
		add("CONNECTED_TO", nil)
	}
	for _, relation := range data.StateRelation {
		add(amtypes.RelLabel(relation.RelType), nil)
	}
}

// neighbors returns the relationships incident to node in the traversal
// direction dir, deduplicated (DIR_BOTH sees a self-loop once).
func (s *store) neighbors(nodeIdx int, dir gqlparser.Dir) []*rel {
	switch dir {
	case gqlparser.DirOut:
		return s.out[nodeIdx]
	case gqlparser.DirIn:
		return s.in[nodeIdx]
	default:
		seen := make(map[*rel]struct{}, len(s.out[nodeIdx])+len(s.in[nodeIdx]))
		ret := make([]*rel, 0, len(s.out[nodeIdx])+len(s.in[nodeIdx]))
		for _, r := range s.out[nodeIdx] {
			seen[r] = struct{}{}
			ret = append(ret, r)
		}
		for _, r := range s.in[nodeIdx] {
			if _, ok := seen[r]; ok {
				continue
			}
			ret = append(ret, r)
		}
		return ret
	}
}

// other returns the far endpoint of r seen from node index idx.
func (r *rel) other(idx int) int {
	if r.from == idx {
		return r.to
	}
	return r.from
}

// indexOf returns the index of v in list.
func indexOf[T comparable](list []T, v T) (int, bool) {
	for i := range list {
		if list[i] == v {
			return i, true
		}
	}
	return 0, false
}

// vKind discriminates a runtime [Value].
type vKind uint8

const (
	kNull vKind = iota
	kBool
	kInt
	kFloat
	kString
	kList
	kMap
	kNode
	kRel
)

// Value is a GQL runtime value: a scalar, a list/map, or a graph element
// (node/relationship). It is the unit rows are made of.
type Value struct {
	kind vKind
	b    bool
	i    int64
	f    float64
	s    string
	list []Value
	mp   map[string]Value
	node *node
	rel  *rel
}

// Null is the null value.
func Null() Value { return Value{kind: kNull} }

// Bool is a boolean value.
func Bool(b bool) Value { return Value{kind: kBool, b: b} }

// Int is an integer value.
func Int(i int64) Value { return Value{kind: kInt, i: i} }

// Float is a float value.
func Float(f float64) Value { return Value{kind: kFloat, f: f} }

// Str is a string value.
func Str(s string) Value { return Value{kind: kString, s: s} }

// List is a list value.
func List(l []Value) Value { return Value{kind: kList, list: l} }

// Map is a map value.
func Map(m map[string]Value) Value { return Value{kind: kMap, mp: m} }

func nodeVal(n *node) Value { return Value{kind: kNode, node: n} }
func relVal(r *rel) Value   { return Value{kind: kRel, rel: r} }

// IsNull reports whether v is null (also true for the zero Value).
func (v Value) IsNull() bool { return v.kind == kNull }

// IsNum reports whether v is an int or float.
func (v Value) IsNum() bool { return v.kind == kInt || v.kind == kFloat }

// Float64 returns v as a float64; ok is false for non-numerics.
func (v Value) Float64() (float64, bool) {
	switch v.kind {
	case kInt:
		return float64(v.i), true
	case kFloat:
		return v.f, true
	}
	return 0, false
}

// String renders v for error messages.
func (v Value) String() string {
	switch v.kind {
	case kNull:
		return "null"
	case kBool:
		return strconv.FormatBool(v.b)
	case kInt:
		return strconv.FormatInt(v.i, 10)
	case kFloat:
		return strconv.FormatFloat(v.f, 'g', -1, 64)
	case kString:
		return strconv.Quote(v.s)
	case kList:
		parts := make([]string, len(v.list))
		for i, el := range v.list {
			parts[i] = el.String()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case kMap:
		keys := make([]string, 0, len(v.mp))
		for k := range v.mp {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+": "+v.mp[k].String())
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case kNode:
		return "node(" + v.node.id + ")"
	case kRel:
		return "rel(" + v.rel.typ + ")"
	}
	return "?"
}

// Any converts v to a plain Go value for JSON/msgpack encoding.
func (v Value) Any() any {
	switch v.kind {
	case kNull:
		return nil
	case kBool:
		return v.b
	case kInt:
		return v.i
	case kFloat:
		return v.f
	case kString:
		return v.s
	case kList:
		if v.list == nil {
			return []any{}
		}
		out := make([]any, len(v.list))
		for i, el := range v.list {
			out[i] = el.Any()
		}
		return out
	case kMap:
		out := make(map[string]any, len(v.mp))
		for k, el := range v.mp {
			out[k] = el.Any()
		}
		return out
	case kNode:
		out := make(map[string]any, len(v.node.props)+1)
		for k, el := range v.node.props {
			out[k] = el.Any()
		}
		out["label"] = v.node.label
		return out
	case kRel:
		out := make(map[string]any, len(v.rel.props)+3)
		for k, el := range v.rel.props {
			out[k] = el.Any()
		}
		out["type"] = v.rel.typ
		out["start"] = v.rel.fromID
		out["end"] = v.rel.toID
		return out
	}
	return nil
}

// key returns a canonical grouping/distinct key. Numbers share one key space
// across int and float (GQL numeric equality), so Int(1) and Float(1) group
// together.
func (v Value) key() string {
	switch v.kind {
	case kNull:
		return "z"
	case kBool:
		return "b:" + strconv.FormatBool(v.b)
	case kInt:
		return "d:" + strconv.FormatUint(math.Float64bits(float64(v.i)), 16)
	case kFloat:
		return "d:" + strconv.FormatUint(math.Float64bits(v.f), 16)
	case kString:
		return "s:" + v.s
	case kList:
		parts := make([]string, len(v.list))
		for i, el := range v.list {
			parts[i] = el.key()
		}
		return "l:[" + strings.Join(parts, ",") + "]"
	case kMap:
		keys := make([]string, 0, len(v.mp))
		for k := range v.mp {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("m:{")
		for _, k := range keys {
			b.WriteString(k)
			b.WriteByte(':')
			b.WriteString(v.mp[k].key())
			b.WriteByte(',')
		}
		b.WriteString("}")
		return b.String()
	case kNode:
		return "n:" + v.node.id
	case kRel:
		return "r:" + strconv.Itoa(v.rel.idx)
	}
	return "?"
}

// compare is the three-valued comparison behind =, <>, <, <=, >, >=. ok is
// false when either side is null or the types are not comparable. Numbers
// compare across int/float by coercing through float64 (like the reference
// engine).
func compare(a, b Value) (int, bool) {
	if a.kind == kNull || b.kind == kNull {
		return 0, false
	}
	if a.IsNum() && b.IsNum() {
		af, _ := a.Float64()
		bf, _ := b.Float64()
		switch {
		case af < bf:
			return -1, true
		case af > bf:
			return 1, true
		}
		return 0, true
	}
	if a.kind != b.kind {
		return 0, false
	}
	switch a.kind {
	case kString:
		return strings.Compare(a.s, b.s), true
	case kBool:
		switch {
		case a.b == b.b:
			return 0, true
		case !a.b:
			return -1, true
		}
		return 1, true
	case kNode:
		return strings.Compare(a.node.id, b.node.id), true
	case kRel:
		switch {
		case a.rel.idx < b.rel.idx:
			return -1, true
		case a.rel.idx > b.rel.idx:
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// orderCompare is a total order for ORDER BY: null sorts last, then numbers,
// strings, booleans, lists, maps, graph elements; incomparable values of the
// same rank sort stable.
func orderCompare(a, b Value) int {
	rank := func(v Value) int {
		switch v.kind {
		case kNull:
			return 9
		case kInt, kFloat:
			return 0
		case kString:
			return 1
		case kBool:
			return 2
		case kList:
			return 3
		case kMap:
			return 4
		case kNode:
			return 5
		case kRel:
			return 6
		}
		return 7
	}
	ra, rb := rank(a), rank(b)
	if ra != rb {
		if ra < rb {
			return -1
		}
		return 1
	}
	if c, ok := compare(a, b); ok {
		return c
	}
	return strings.Compare(a.key(), b.key())
}

// truth reports whether v is the boolean true; null and non-booleans are
// false in a WHERE position.
func truth(v Value) bool { return v.kind == kBool && v.b }
