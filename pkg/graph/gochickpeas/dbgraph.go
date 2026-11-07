// Package gochickpeas implements the [amtypes.DbGraph] backend over the
// in-memory gochickpeas graph database.
//
// The graph is mirrored incrementally: machine upserts rebuild the snapshot
// from the cached model (rare), while clock syncs use a thaw/refinalize
// copy-on-write loop (cheap, shares everything but the touched columns).
//
// The GQL engine is read-only; writes go through [amtypes.DbGraph]
// (UpsertMachine/SyncMachine), like the frogql backend.
package gochickpeas

import (
	"errors"
	"maps"
	"slices"
	"sync"

	chickpeas "github.com/freeeve/gochickpeas"
	"github.com/freeeve/gochickpeas/gql"
	gqlvalue "github.com/freeeve/gochickpeas/gql/value"

	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/shared"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	ssrpc "github.com/pancsta/asyncmachine-go/pkg/rpc/states"
)

// dbGraph implements [amtypes.DbGraph] over an immutable gochickpeas
// snapshot. The GQL model mirrors the frogql backend:
//
//	(:Machine {id, parent, schema_hash, mtime_sum, queue_tick, mach_tick})
//	(:State {mach, name, tick, active})
//	(:Tag {mach, name, value})
//	(Machine)-[:HAS {auto, multi}]->(State)
//	(Machine)-[:HAS_TAG]->(Tag)
//	(Machine)-[:CHILD_OF]->(Machine)
//	(Machine)-[:CONNECTED_TO]->(Machine) // RPC conns (src-id tags)
//	(State)-[:REQUIRE|ADD|REMOVE|AFTER]->(State)
type dbGraph struct {
	mx sync.RWMutex

	snap *chickpeas.Snapshot
	// ids maps model keys to node ids of the current snapshot.
	ids map[string]chickpeas.NodeID
	// machines is the cached mirror model.
	machines map[string]*dbMachInfo
}

// dbMachInfo is the mirror bookkeeping for a machine.
type dbMachInfo struct {
	mach *amtypes.Machine
}

var _ amtypes.DbGraph = (*dbGraph)(nil)

// NewDbGraph opens an in-memory gochickpeas database, optionally composed
// into NetGraph via NetGraph.Db. Requires the "gochickpeas" build tag.
func NewDbGraph() (amtypes.DbGraph, error) {
	return &dbGraph{
		snap:     chickpeas.NewBuilder(0, 0).Finalize(),
		ids:      make(map[string]chickpeas.NodeID),
		machines: make(map[string]*dbMachInfo),
	}, nil
}

// Close is a no-op, the in-memory snapshot has no resources.
func (d *dbGraph) Close() error {
	return nil
}

// NodeCount returns the number of nodes in the database.
func (d *dbGraph) NodeCount() int {
	if d == nil || d.snap == nil {
		return 0
	}
	d.mx.RLock()
	defer d.mx.RUnlock()

	return int(d.snap.NodeCount())
}

// EdgeCount returns the number of relationships in the database.
func (d *dbGraph) EdgeCount() int {
	if d == nil || d.snap == nil {
		return 0
	}
	d.mx.RLock()
	defer d.mx.RUnlock()

	return int(d.snap.RelCount())
}

// Exec runs a read-only GQL statement against the mirrored snapshot.
func (d *dbGraph) Exec(gqlQuery string) (any, error) {
	if d == nil {
		return nil, errors.New("dbgraph is nil")
	}
	d.mx.RLock()
	defer d.mx.RUnlock()

	rows, err := gql.Run(d.snap, gqlQuery)
	if err != nil {
		return nil, err
	}

	out := make([]any, 0, 16)
	for row := range rows.All() {
		rec := make(map[string]any, len(row.Columns()))
		for _, col := range row.Columns() {
			if v, ok := row.Get(col); ok {
				rec[col] = toAny(v)
			}
		}
		out = append(out, rec)
	}

	return out, nil
}

// UpsertMachine replaces a machine in the database with its current schema,
// states, relations, tags, and graph edges. Idempotent.
func (d *dbGraph) UpsertMachine(m *amtypes.Machine) error {
	if d == nil || m == nil || m.Serialized == nil {
		return nil
	}
	d.mx.Lock()
	defer d.mx.Unlock()

	d.machines[m.ID] = &dbMachInfo{mach: m.Clone()}

	return d.rebuild()
}

// SyncMachine updates the clocks and tags of a machine in the database.
// Machines not yet upserted are upserted in full instead.
func (d *dbGraph) SyncMachine(m *amtypes.Machine) error {
	if d == nil || m == nil || m.Serialized == nil {
		return nil
	}
	d.mx.Lock()
	defer d.mx.Unlock()

	info, ok := d.machines[m.ID]
	if !ok {
		d.machines[m.ID] = &dbMachInfo{mach: m.Clone()}
		return d.rebuild()
	}

	prev := info.mach
	tagsChanged := !slices.Equal(prev.Tags, m.Tags)
	schemaChanged := prev.SchemaHash != m.SchemaHash ||
		!slices.Equal(prev.StateNames, m.StateNames)
	info.mach = m.Clone()
	if tagsChanged || schemaChanged {
		// topology change: tag/state nodes and their edges
		return d.rebuild()
	}

	clocksChanged := !slices.Equal(prev.Time, m.Time) ||
		prev.MTimeSum != m.MTimeSum ||
		prev.QueueTick != m.QueueTick ||
		prev.MachineTick != m.MachineTick
	if !clocksChanged {
		return nil
	}

	return d.syncClocks(prev, m)
}

// PRIVATE

// syncClocks applies changed clocks via copy-on-write, sharing the untouched
// snapshot components. Callers hold d.mx.
func (d *dbGraph) syncClocks(prev, m *amtypes.Machine) error {
	b := chickpeas.NewBuilderFromSnapshot(d.snap)

	if prev.MTimeSum != m.MTimeSum || prev.QueueTick != m.QueueTick ||
		prev.MachineTick != m.MachineTick {
		if id, ok := d.ids[nodeKeyMachine(m.ID)]; ok {
			if err := b.UpdateProp(id, "mtime_sum", int64(m.MTimeSum)); err != nil {
				return err
			}
			if err := b.UpdateProp(id, "queue_tick", int64(m.QueueTick)); err != nil {
				return err
			}
			if err := b.UpdateProp(id, "mach_tick",
				int64(m.MachineTick)); err != nil {
				return err
			}
		}
	}

	for i, name := range m.StateNames {
		if i >= len(m.Time) {
			break
		}
		if i < len(prev.Time) && prev.Time[i] == m.Time[i] {
			continue
		}
		id, ok := d.ids[nodeKeyState(m.ID, name)]
		if !ok {
			continue
		}
		if err := b.UpdateProp(id, "tick", int64(m.Time[i])); err != nil {
			return err
		}
		err := b.UpdateProp(id, "active", am.IsActiveTick(m.Time[i]))
		if err != nil {
			return err
		}
	}

	d.snap = b.Finalize()

	return nil
}

// rebuild reconstructs the full snapshot from the cached model. Callers hold
// d.mx.
func (d *dbGraph) rebuild() error {
	b := chickpeas.NewBuilder(0, 0)
	d.ids = make(map[string]chickpeas.NodeID, len(d.machines)*7)

	var next chickpeas.NodeID
	addNode := func(key string, labels ...string) (chickpeas.NodeID, error) {
		id := next
		next++
		if _, err := b.AddNodeWithID(id, labels...); err != nil {
			return 0, err
		}
		d.ids[key] = id

		return id, nil
	}

	// deterministic order, for stable ids
	ids := slices.Sorted(maps.Keys(d.machines))

	// nodes + per-machine rels
	for _, id := range ids {
		info := d.machines[id]
		m := info.mach

		mID, err := addNode(nodeKeyMachine(id), "Machine")
		if err != nil {
			return err
		}
		for key, val := range map[string]any{
			"id":          m.ID,
			"parent":      m.Parent,
			"schema_hash": m.SchemaHash,
			"mtime_sum":   int64(m.MTimeSum),
			"queue_tick":  int64(m.QueueTick),
			"mach_tick":   int64(m.MachineTick),
		} {
			if err := b.SetProp(mID, key, val); err != nil {
				return err
			}
		}

		for i, name := range m.StateNames {
			tick := int64(0)
			if i < len(m.Time) {
				tick = int64(m.Time[i])
			}
			sID, err := addNode(nodeKeyState(id, name), "State")
			if err != nil {
				return err
			}
			if err := b.SetProp(sID, "mach", m.ID); err != nil {
				return err
			}
			if err := b.SetProp(sID, "name", name); err != nil {
				return err
			}
			if err := b.SetProp(sID, "tick", tick); err != nil {
				return err
			}
			if err := b.SetProp(sID, "active",
				am.IsActiveTick(uint64(tick))); err != nil {
				return err
			}

			relIdx, err := b.AddRel(mID, sID, "HAS")
			if err != nil {
				return err
			}
			state, ok := m.Schema[name]
			if !ok {
				continue
			}
			if err := b.SetRelPropAt(relIdx, "auto", state.Auto); err != nil {
				return err
			}
			if err := b.SetRelPropAt(relIdx, "multi", state.Multi); err != nil {
				return err
			}
		}

		for _, tag := range m.Tags {
			tID, err := addNode(nodeKeyTag(id, tag), "Tag")
			if err != nil {
				return err
			}
			if err := b.SetProp(tID, "mach", m.ID); err != nil {
				return err
			}
			if err := b.SetProp(tID, "name", tag); err != nil {
				return err
			}
			if err := b.SetProp(tID, "value", amtypes.TagValue(tag)); err != nil {
				return err
			}
			if _, err := b.AddRel(mID, tID, "HAS_TAG"); err != nil {
				return err
			}
		}
	}

	// relationships between nodes
	for _, id := range ids {
		info := d.machines[id]
		m := info.mach
		mID := d.ids[nodeKeyMachine(id)]

		// state relations
		for _, name := range m.StateNames {
			state, ok := m.Schema[name]
			if !ok {
				continue
			}
			fromID := d.ids[nodeKeyState(id, name)]
			for rel, label := range amtypes.RelLabels {
				for _, to := range amtypes.RelStates(state, rel) {
					toID, ok := d.ids[nodeKeyState(id, to)]
					if !ok {
						continue
					}
					if _, err := b.AddRel(fromID, toID, label); err != nil {
						return err
					}
				}
			}
		}

		// machine edges, resolved once both endpoints are known
		if m.Parent != "" && m.Parent != id {
			if pID, ok := d.ids[nodeKeyMachine(m.Parent)]; ok {
				if _, err := b.AddRel(mID, pID, "CHILD_OF"); err != nil {
					return err
				}
			}
		}
		if src := amhelp.TagValue(m.Tags, ssrpc.TagKeySrcId); src != "" &&
			src != id {
			if sID, ok := d.ids[nodeKeyMachine(src)]; ok {
				if _, err := b.AddRel(mID, sID, "CONNECTED_TO"); err != nil {
					return err
				}
			}
		}
	}

	d.snap = b.Finalize()

	return nil
}

// nodeKeyMachine keys a machine node in d.ids.
func nodeKeyMachine(id string) string { return "m:" + id }

// nodeKeyState keys a state node in d.ids.
func nodeKeyState(machID, name string) string {
	return "s:" + machID + ":" + name
}

// nodeKeyTag keys a tag node in d.ids.
func nodeKeyTag(machID, name string) string {
	return "t:" + machID + ":" + name
}

// toAny converts a GQL runtime value into a JSON-encodable Go value.
func toAny(v gqlvalue.Value) any {
	switch v.Kind() {
	case gqlvalue.KindNull:
		return nil
	case gqlvalue.KindBool:
		val, _ := v.AsBool()
		return val
	case gqlvalue.KindInt:
		val, _ := v.AsInt()
		return val
	case gqlvalue.KindFloat:
		val, _ := v.AsFloat()
		return val
	case gqlvalue.KindStr:
		val, _ := v.AsStr()
		return val
	case gqlvalue.KindList:
		list, _ := v.AsList()
		out := make([]any, len(list))
		for i, item := range list {
			out[i] = toAny(item)
		}
		return out
	case gqlvalue.KindMap:
		entries, _ := v.AsMap()
		out := make(map[string]any, len(entries))
		for _, entry := range entries {
			out[entry.Key] = toAny(entry.Val)
		}
		return out
	case gqlvalue.KindNode:
		id, _ := v.AsNode()
		return uint32(id)
	case gqlvalue.KindRel:
		pos, _ := v.AsRel()
		return pos
	default:
		return nil
	}
}
