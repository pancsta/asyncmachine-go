//go:build frogql

package frogql

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// dbGraph implements [amtypes.DbGraph] over an embedded frogql database.
// The GQL model:
//
//	(:Machine {id, parent, schema_hash, mtime_sum, queue_tick, mach_tick})
//	(:State {mach, name, tick, active})
//	(:Tag {mach, name, value})
//	(Machine)-[:HAS]->(State)
//	(Machine)-[:HAS_TAG]->(Tag)
//	(Machine)-[:CHILD_OF]->(Machine)
//	(Machine)-[:CONNECTED_TO]->(Machine) // RPC conns (src-id tags)
//	(State)-[:REQUIRE|ADD|REMOVE|AFTER]->(State)
type dbGraph struct {
	conn *Connection
	mx   sync.Mutex

	// machines maps machine IDs to their graph-derived edges and tags, used
	// to resolve cross-machine edges while machines arrive incrementally.
	machines map[string]*dbMachInfo
	// edges maps already-inserted cross-machine edges, so resolution only
	// touches missing ones. Wiped per machine by wipeMachine.
	edges map[dbEdge]struct{}
}

// dbEdge is a cross-machine edge tracked by dbGraph.
type dbEdge struct {
	label string
	from  string
	to    string
}

var _ amtypes.DbGraph = (*dbGraph)(nil)

// dbMachInfo is the DB-side bookkeeping for a mirrored machine.
type dbMachInfo struct {
	parent string
	src    string
	tags   []string

	// last synced clocks, used to skip unchanged states
	time      am.Time
	mtimeSum  uint64
	queueTick uint64
	machTick  uint32
}

// NewDbGraph opens an in-memory frogql database, optionally composed into
// NetGraph via NetGraph.Db. Requires the "frogql" build
// tag.
func NewDbGraph() (amtypes.DbGraph, error) {
	conn, err := Open()
	if err != nil {
		return nil, err
	}

	return &dbGraph{
		conn:     conn,
		machines: make(map[string]*dbMachInfo),
		edges:    make(map[dbEdge]struct{}),
	}, nil
}

// Close closes the underlying connection.
func (d *dbGraph) Close() error {
	if d == nil {
		return nil
	}
	d.mx.Lock()
	defer d.mx.Unlock()

	return d.conn.Close()
}

// Exec executes an arbitrary GQL statement against the database.
func (d *dbGraph) Exec(gql string) (any, error) {
	if d == nil {
		return nil, errors.New("dbgraph is nil")
	}
	d.mx.Lock()
	defer d.mx.Unlock()

	return d.conn.Exec(gql)
}

// NodeCount returns the number of nodes in the database.
func (d *dbGraph) NodeCount() int {
	if d == nil {
		return 0
	}
	d.mx.Lock()
	defer d.mx.Unlock()

	return d.conn.NodeCount()
}

// EdgeCount returns the number of edges in the database.
func (d *dbGraph) EdgeCount() int {
	if d == nil {
		return 0
	}
	d.mx.Lock()
	defer d.mx.Unlock()

	return d.conn.EdgeCount()
}

// UpsertMachine replaces a machine in the database with its current schema,
// states, relations, tags (as nodes), and graph edges. Idempotent.
func (d *dbGraph) UpsertMachine(m *amtypes.Machine) error {
	if d == nil || m == nil || m.Serialized == nil {
		return nil
	}
	d.mx.Lock()
	defer d.mx.Unlock()

	// wipe the previous version, incl its states, tags, and edges
	if err := d.wipeMachine(m.ID); err != nil {
		return err
	}

	// machine + states + tags + HAS/HAS_TAG/relation edges, in one statement
	var b strings.Builder
	b.WriteString(fmt.Sprintf("INSERT (m:Machine {%s})", machineProps(m)))
	for i, name := range m.StateNames {
		tick := uint64(0)
		if i < len(m.Time) {
			tick = m.Time[i]
		}
		b.WriteString(fmt.Sprintf(", (s%d:State {%s})",
			i, stateProps(m.ID, name, tick)))
		b.WriteString(fmt.Sprintf(", (m)-[:HAS]->(s%d)", i))
	}
	for i, tag := range m.Tags {
		b.WriteString(fmt.Sprintf(", (t%d:Tag {%s})", i, tagProps(m.ID, tag)))
		b.WriteString(fmt.Sprintf(", (m)-[:HAS_TAG]->(t%d)", i))
	}
	for i, name := range m.StateNames {
		state, ok := m.Schema[name]
		if !ok {
			continue
		}
		for rel, label := range amtypes.RelLabels {
			for _, to := range amtypes.RelStates(state, rel) {
				j := slices.Index(m.StateNames, to)
				if j < 0 || j == i {
					continue
				}
				b.WriteString(fmt.Sprintf(", (s%d)-[:%s]->(s%d)", i, label, j))
			}
		}
	}
	if _, err := d.conn.Exec(b.String()); err != nil {
		return err
	}

	// bookkeeping
	d.machines[m.ID] = &dbMachInfo{
		parent:    m.Parent,
		src:       amhelp.TagValue(m.Tags, ssrpc.TagKeySrcId),
		tags:      slices.Clone(m.Tags),
		time:      slices.Clone(m.Time),
		mtimeSum:  m.MTimeSum,
		queueTick: m.QueueTick,
		machTick:  m.MachineTick,
	}

	return d.resolveEdges()
}

// SyncMachine updates the clocks and tags of a machine in the database.
// Machines not yet upserted are upserted in full instead.
func (d *dbGraph) SyncMachine(m *amtypes.Machine) error {
	if d == nil || m == nil || m.Serialized == nil {
		return nil
	}

	d.mx.Lock()
	_, ok := d.machines[m.ID]
	d.mx.Unlock()
	if !ok {
		return d.UpsertMachine(m)
	}

	return d.syncMachine(m)
}

// PRIVATE

// wipeMachine deletes a machine with its states, tags, and edges. Callers
// hold d.mx.
func (d *dbGraph) wipeMachine(id string) error {
	// the DETACH DELETEs below remove the rows, keep the bookkeeping in sync
	for e := range d.edges {
		if e.from == id || e.to == id {
			delete(d.edges, e)
		}
	}

	for _, q := range []string{
		"MATCH (s:State {mach: %[1]s}) DETACH DELETE s",
		"MATCH (t:Tag {mach: %[1]s}) DETACH DELETE t",
		"MATCH (m:Machine {id: %[1]s}) DETACH DELETE m",
	} {
		if _, err := d.conn.Exec(fmt.Sprintf(q, gqlStr(id))); err != nil {
			return err
		}
	}

	return nil
}

// resolveEdges inserts all missing cross-machine edges, once both endpoints
// are known. Callers hold d.mx.
func (d *dbGraph) resolveEdges() error {
	var errs []error
	for id, info := range d.machines {
		if info.parent != "" && info.parent != id {
			if _, ok := d.machines[info.parent]; ok {
				errs = append(errs, d.addEdge(dbEdge{
					label: "CHILD_OF",
					from:  id,
					to:    info.parent,
				}))
			}
		}
		if info.src != "" && info.src != id {
			if _, ok := d.machines[info.src]; ok {
				errs = append(errs, d.addEdge(dbEdge{
					label: "CONNECTED_TO",
					from:  id,
					to:    info.src,
				}))
			}
		}
	}

	return errors.Join(errs...)
}

// addEdge inserts a cross-machine edge if it's not tracked yet. Callers hold
// d.mx.
func (d *dbGraph) addEdge(e dbEdge) error {
	if _, ok := d.edges[e]; ok {
		return nil
	}

	_, err := d.conn.Exec(fmt.Sprintf(
		"MATCH (a:Machine {id: %s}), (b:Machine {id: %s})"+
			" INSERT (a)-[:%s]->(b)", gqlStr(e.from), gqlStr(e.to), e.label))
	if err != nil {
		return err
	}

	d.edges[e] = struct{}{}

	return nil
}

// syncMachine updates a machine's clocks and, when changed, its tags.
// Callers hold d.mx.
func (d *dbGraph) syncMachine(m *amtypes.Machine) error {
	d.mx.Lock()
	defer d.mx.Unlock()

	info := d.machines[m.ID]

	// skip unchanged machine props
	if info.mtimeSum != m.MTimeSum || info.queueTick != m.QueueTick ||
		info.machTick != m.MachineTick {
		if _, err := d.conn.Exec(fmt.Sprintf(
			"MATCH (m:Machine {id: %s})"+
				" SET m.mtime_sum = %d, m.queue_tick = %d, m.mach_tick = %d",
			gqlStr(m.ID), m.MTimeSum, m.QueueTick,
			m.MachineTick)); err != nil {
			return err
		}
		info.mtimeSum, info.queueTick, info.machTick =
			m.MTimeSum, m.QueueTick, m.MachineTick
	}

	// sync only changed state ticks
	for i, name := range m.StateNames {
		if i >= len(m.Time) {
			break
		}
		if info.time[i] == m.Time[i] {
			continue
		}
		if _, err := d.conn.Exec(fmt.Sprintf(
			"MATCH (s:State {mach: %s, name: %s})"+
				" SET s.tick = %d, s.active = %t",
			gqlStr(m.ID), gqlStr(name), m.Time[i],
			am.IsActiveTick(m.Time[i]))); err != nil {
			return err
		}
		info.time[i] = m.Time[i]
	}

	// tags are separate nodes, re-sync only on change
	if slices.Equal(info.tags, m.Tags) {
		return nil
	}
	info.tags = slices.Clone(m.Tags)
	info.src = amhelp.TagValue(m.Tags, ssrpc.TagKeySrcId)

	// drop the old outgoing CONNECTED_TO edges, the src tag may have changed
	if _, err := d.conn.Exec(fmt.Sprintf(
		"MATCH (a:Machine {id: %s})-[r:CONNECTED_TO]->(b) DELETE r",
		gqlStr(m.ID))); err != nil {
		return err
	}
	for e := range d.edges {
		if e.label == "CONNECTED_TO" && e.from == m.ID {
			delete(d.edges, e)
		}
	}

	if _, err := d.conn.Exec(fmt.Sprintf(
		"MATCH (t:Tag {mach: %s}) DETACH DELETE t", gqlStr(m.ID))); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("MATCH (m:Machine {id: %s}) INSERT ", gqlStr(m.ID)))
	for i, tag := range m.Tags {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("(t%d:Tag {%s})", i, tagProps(m.ID, tag)))
		b.WriteString(fmt.Sprintf(", (m)-[:HAS_TAG]->(t%d)", i))
	}
	if len(m.Tags) > 0 {
		if _, err := d.conn.Exec(b.String()); err != nil {
			return err
		}
	}

	return d.resolveEdges()
}

// machineProps formats the GQL properties of a machine node.
func machineProps(m *amtypes.Machine) string {
	return fmt.Sprintf("id: %s, parent: %s, schema_hash: %s,"+
		" mtime_sum: %d, queue_tick: %d, mach_tick: %d",
		gqlStr(m.ID), gqlStr(m.Parent), gqlStr(m.SchemaHash),
		m.MTimeSum, m.QueueTick, m.MachineTick)
}

// stateProps formats the GQL properties of a state node.
func stateProps(machID, name string, tick uint64) string {
	return fmt.Sprintf("mach: %s, name: %s, tick: %d, active: %t",
		gqlStr(machID), gqlStr(name), tick, am.IsActiveTick(tick))
}

// tagProps formats the GQL properties of a tag node.
func tagProps(machID, name string) string {
	return fmt.Sprintf("mach: %s, name: %s, value: %s",
		gqlStr(machID), gqlStr(name), gqlStr(amtypes.TagValue(name)))
}

// gqlStr formats a GQL string literal. The frogql lexer has no escape
// syntax, values containing a single quote are unsupported.
func gqlStr(s string) string {
	return "'" + s + "'"
}
