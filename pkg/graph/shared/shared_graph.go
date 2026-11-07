// Package shared contains graph types and helpers shared by the DbGraph
// backends: the [DbGraph] interface, the mirror model types, and GQL model
// helpers. Implementations: pkg/graph/gql (native, untagged),
// pkg/graph/gochickpeas (native, untagged), and pkg/graph/frogql (wasm,
// "frogql" build tag).
package shared

import (
	"slices"
	"strings"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// Machine represents a single state machine withing the network graph.
type Machine struct {
	*am.Serialized

	// Current schema hash.
	SchemaHash string
	// Current machine schema.
	Schema am.Schema
	// Parent is the parent machine ID.
	Parent string
	// Current machine time.
	MTimeSum uint64
	// The last update of tags happened during this machine time.
	TagsMTimeSum uint64
}

// Clone returns a deep copy of the machine, safe for concurrent reads.
func (m *Machine) Clone() *Machine {
	if m == nil {
		return nil
	}
	cp := *m
	if m.Serialized != nil {
		ser := *m.Serialized
		ser.StateNames = slices.Clone(m.StateNames)
		ser.Time = slices.Clone(m.Time)
		ser.Tags = slices.Clone(m.Tags)
		cp.Serialized = &ser
	}
	cp.Schema = m.Schema.Clone()

	return &cp
}

// DbGraph mirrors graph changes into a graph database. It's optionally composed
// into NetGraph via NetGraph.Db.
//
// The embedded frogql implementation lives in pkg/graph/frogql and requires
// the "frogql" build tag. Its GQL model:
//
//	(:Machine {id, parent, schema_hash, mtime_sum, queue_tick, mach_tick})
//	(:State {mach, name, tick, active})
//	(:Tag {mach, name, value})
//	(Machine)-[:HAS]->(State)
//	(Machine)-[:HAS_TAG]->(Tag)
//	(Machine)-[:CHILD_OF]->(Machine)
//	(Machine)-[:CONNECTED_TO]->(Machine) // RPC conns (src-id tags)
//	(State)-[:REQUIRE|ADD|REMOVE|AFTER]->(State)
type DbGraph interface {
	// UpsertMachine replaces a machine in the database with its current
	// schema, states, relations, tags, and cross-machine edges. Idempotent.
	UpsertMachine(m *Machine) error
	// SyncMachine updates the clocks and tags of an existing machine in the
	// database. Machines not yet upserted should be upserted in full.
	SyncMachine(m *Machine) error
	// Exec executes an arbitrary GQL statement against the database.
	Exec(gql string) (any, error)
	// NodeCount returns the number of nodes in the database.
	NodeCount() int
	// EdgeCount returns the number of edges in the database.
	EdgeCount() int
	// Close closes the underlying connection.
	Close() error
}

// RelLabels maps machine relations to GQL edge labels.
var RelLabels = map[am.Relation]string{
	am.RelationRequire: "REQUIRE",
	am.RelationAdd:     "ADD",
	am.RelationRemove:  "REMOVE",
	am.RelationAfter:   "AFTER",
}

// RelLabel returns the GQL edge label of a machine relation.
func RelLabel(rel am.Relation) string {
	if label, ok := RelLabels[rel]; ok {
		return label
	}

	return "RELATION"
}

// RelStates returns the related states of a specific relation type.
func RelStates(state am.State, rel am.Relation) am.S {
	switch rel {
	case am.RelationRequire:
		return state.Require
	case am.RelationAdd:
		return state.Add
	case am.RelationRemove:
		return state.Remove
	case am.RelationAfter:
		return state.After
	default:
		return nil
	}
}

// TagValue returns the value part of a tag, or the tag name when valueless.
func TagValue(tag string) string {
	key, _, _ := strings.Cut(tag, ":")

	return amhelp.TagValue([]string{tag}, key)
}
