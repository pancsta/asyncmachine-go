package graph

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/mlange-42/ark/ecs"

	amtypes "github.com/pancsta/asyncmachine-go/pkg/graph/types"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

var (
	ErrVertexNotFound = errors.New("vertex not found")
	ErrEdgeNotFound   = errors.New("edge not found")
	ErrPathNotFound   = errors.New("path not found")
)

// Edge is a directed graph edge between two vertex hashes.
type Edge struct {
	Source     string
	Target     string
	Properties EdgeProperties
}

// EdgeProperties contains edge metadata.
type EdgeProperties struct {
	Data any
}

func (e Edge) data() *EdgeData {
	data, _ := e.Properties.Data.(*EdgeData)
	return data
}

type edgeKey struct {
	source string
	target string
}

// edgeBase is the required component of an edge entity.
type edgeBase struct {
	source string
	target string
}

// Typed edge metadata components. Each GQL relationship label is a separate
// optional component, so edges can be queried by kind using ark ECS filters.

type (
	// edgeMachHas is the (Machine)-[:HAS]->(State) metadata.
	edgeMachHas struct{ MachineHas }
	// edgeMachHasTag is the (Machine)-[:HAS_TAG]->(Tag) metadata.
	edgeMachHasTag struct{}
	// edgeMachChildOf is the (Machine)-[:CHILD_OF]->(Machine) metadata.
	edgeMachChildOf struct{}
	// edgeMachConnectedTo is the (Machine)-[:CONNECTED_TO]->(Machine) metadata.
	edgeMachConnectedTo struct{}
	// edgeMachPipe is the (Machine)-[:PIPE]->(Machine) metadata. Each pipe is
	// a separate edge entity.
	edgeMachPipe struct{ MachPipeTo }

	// TODO perf: make these optional (dont index)

	// edgeStateRequire is the (State)-[:REQUIRE]->(State) metadata.
	edgeStateRequire struct{}
	// edgeStateAdd is the (State)-[:ADD]->(State) metadata.
	edgeStateAdd struct{}
	// edgeStateRemove is the (State)-[:REMOVE]->(State) metadata.
	edgeStateRemove struct{}
	// edgeStateAfter is the (State)-[:AFTER]->(State) metadata.
	edgeStateAfter struct{}
)

// edgeComp is a typed ark ECS map and filter of one optional edge component,
// bound to the owning store for projections.
type edgeComp[A any] struct {
	s *store
	m *ecs.Map1[A]
	f *ecs.Filter1[A]
}

func newEdgeComp[A any](s *store) edgeComp[A] {
	return edgeComp[A]{
		s: s,
		m: ecs.NewMap1[A](s.world),
		f: ecs.NewFilter1[A](s.world),
	}
}

func (c edgeComp[A]) get(e ecs.Entity) *A {
	return c.m.Get(e)
}

func (c edgeComp[A]) has(e ecs.Entity) bool {
	return c.m.HasAll(e)
}

// set upserts the component, or removes it for a nil value. The caller must
// hold the lock.
func (c edgeComp[A]) set(e ecs.Entity, v *A) {
	if v == nil {
		c.clear(e)
		return
	}
	if cur := c.m.Get(e); cur != nil {
		*cur = *v
		return
	}
	c.m.Add(e, v)
}

// clear removes the component. The caller must hold the lock.
func (c edgeComp[A]) clear(e ecs.Entity) {
	if c.m.HasAll(e) {
		c.m.Remove(e)
	}
}

// edges returns all graph edges having the component, optionally filtered by
// source.
func (c edgeComp[A]) edges(source string) []Edge {
	var ret []Edge
	q := c.f.Query()
	for q.Next() {
		e := q.Entity()
		base := c.s.em.Get(e)
		if source != "" && base.source != source {
			continue
		}
		key := edgeKey{source: base.source, target: base.target}
		ret = append(ret, c.s.edgeValue(key, e))
	}
	q.Close()

	return ret
}

// store is an ark ECS-backed graph store. Vertices and edges are entities with
// typed components, indexed by their string hash.
type store struct {
	mu sync.RWMutex

	world *ecs.World
	verts map[string]ecs.Entity
	edges map[edgeKey]ecs.Entity
	// pipes indexes pipe entities by their machine pair.
	pipes map[edgeKey][]ecs.Entity

	vm *ecs.Map1[Vertex]
	vf *ecs.Filter1[Vertex]
	em *ecs.Map1[edgeBase]

	// Typed edge metadata, one component per GQL relationship label.

	has     edgeComp[edgeMachHas]
	hasTag  edgeComp[edgeMachHasTag]
	childOf edgeComp[edgeMachChildOf]
	connTo  edgeComp[edgeMachConnectedTo]
	pipe    edgeComp[edgeMachPipe]
	require edgeComp[edgeStateRequire]
	add     edgeComp[edgeStateAdd]
	remove  edgeComp[edgeStateRemove]
	after   edgeComp[edgeStateAfter]
}

func newStore(world *ecs.World) *store {
	if world == nil {
		world = ecs.NewWorld()
	}
	s := &store{
		world: world,
		verts: make(map[string]ecs.Entity),
		edges: make(map[edgeKey]ecs.Entity),
		pipes: make(map[edgeKey][]ecs.Entity),
		vm:    ecs.NewMap1[Vertex](world),
		vf:    ecs.NewFilter1[Vertex](world),
		em:    ecs.NewMap1[edgeBase](world),
	}
	s.has = newEdgeComp[edgeMachHas](s)
	s.hasTag = newEdgeComp[edgeMachHasTag](s)
	s.childOf = newEdgeComp[edgeMachChildOf](s)
	s.connTo = newEdgeComp[edgeMachConnectedTo](s)
	s.pipe = newEdgeComp[edgeMachPipe](s)
	s.require = newEdgeComp[edgeStateRequire](s)
	s.add = newEdgeComp[edgeStateAdd](s)
	s.remove = newEdgeComp[edgeStateRemove](s)
	s.after = newEdgeComp[edgeStateAfter](s)

	return s
}

// ///// ///// /////

// ///// VERTICES

// ///// ///// /////

func (s *store) addVertex(v *Vertex) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.addVertexUnlocked(v)
	return nil
}

// addVertexUnlocked upserts a vertex. The caller must hold the lock.
func (s *store) addVertexUnlocked(v *Vertex) bool {
	h := hash(v)
	if _, ok := s.verts[h]; ok {
		return false
	}
	s.verts[h] = s.vm.NewEntity(v)
	return true
}

func (s *store) Vertex(hash string) (*Vertex, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.vertexUnlocked(hash)
}

func (s *store) vertexUnlocked(hash string) (*Vertex, error) {
	e, ok := s.verts[hash]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrVertexNotFound, hash)
	}
	v := *s.vm.Get(e)
	return &v, nil
}

func (s *store) Vertices() []*Vertex {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.verticesUnlocked()
}

func (s *store) verticesUnlocked() []*Vertex {
	ret := make([]*Vertex, 0, len(s.verts))
	q := s.vf.Query()
	for q.Next() {
		v := *q.Get()
		ret = append(ret, &v)
	}
	q.Close()
	return ret
}

// Snapshot returns all vertices and edges in one batch.
func (s *store) Snapshot() ([]*Vertex, []Edge) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.verticesUnlocked(), s.edgesUnlocked()
}

// removeVertices removes vertices and all their inbound and outbound edges.
func (s *store) removeVertices(hashes map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, e := range s.edges {
		if _, ok := hashes[key.source]; ok {
			s.world.RemoveEntity(e)
			delete(s.edges, key)
			s.removePipesUnlocked(key)
			continue
		}
		if _, ok := hashes[key.target]; ok {
			s.world.RemoveEntity(e)
			delete(s.edges, key)
			s.removePipesUnlocked(key)
		}
	}
	for hash := range hashes {
		if e, ok := s.verts[hash]; ok {
			s.world.RemoveEntity(e)
			delete(s.verts, hash)
		}
	}
}

// reset removes all graph entities, keeping the world and its custom entities.
func (s *store) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, e := range s.edges {
		s.world.RemoveEntity(e)
		s.removePipesUnlocked(key)
	}
	clear(s.edges)
	for hash, e := range s.verts {
		s.world.RemoveEntity(e)
		delete(s.verts, hash)
	}
}

// removeIncidentEdges removes all inbound and outbound edges of a vertex.
func (s *store) removeIncidentEdges(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, e := range s.edges {
		if key.source == hash || key.target == hash {
			s.world.RemoveEntity(e)
			delete(s.edges, key)
			s.removePipesUnlocked(key)
		}
	}
}

// ///// ///// /////

// ///// EDGES

// ///// ///// /////

func (s *store) addEdge(src, dst string, data *EdgeData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.addEdgeUnlocked(src, dst, data)
}

func (s *store) addEdgeUnlocked(src, dst string, data *EdgeData) error {
	if _, ok := s.verts[src]; !ok {
		return fmt.Errorf("%w: %q", ErrVertexNotFound, src)
	}
	if _, ok := s.verts[dst]; !ok {
		return fmt.Errorf("%w: %q", ErrVertexNotFound, dst)
	}

	key := edgeKey{source: src, target: dst}
	e, ok := s.edges[key]
	if !ok {
		e = s.em.NewEntity(&edgeBase{source: src, target: dst})
		s.edges[key] = e
	}
	s.mergeEdgeData(e, key, data)

	return nil
}

// UpdateEdge replaces all metadata of an edge.
func (s *store) UpdateEdge(src, dst string, data *EdgeData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := edgeKey{source: src, target: dst}
	e, ok := s.edges[key]
	if !ok {
		return fmt.Errorf("%w: %q -> %q", ErrEdgeNotFound, src, dst)
	}
	s.clearEdgeData(e)
	s.removePipesUnlocked(key)
	s.mergeEdgeData(e, key, data)

	return nil
}

func (s *store) RemoveEdge(src, dst string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := edgeKey{source: src, target: dst}
	e, ok := s.edges[key]
	if !ok {
		return nil
	}
	s.world.RemoveEntity(e)
	delete(s.edges, key)
	s.removePipesUnlocked(key)
	return nil
}

func (s *store) Edge(src, dst string) (Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.edgeUnlocked(src, dst)
}

func (s *store) edgeUnlocked(src, dst string) (Edge, error) {
	key := edgeKey{source: src, target: dst}
	e, ok := s.edges[key]
	if !ok {
		return Edge{}, fmt.Errorf("%w: %q -> %q", ErrEdgeNotFound, src, dst)
	}
	return s.edgeValue(key, e), nil
}

func (s *store) Edges() []Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.edgesUnlocked()
}

func (s *store) edgesUnlocked() []Edge {
	ret := make([]Edge, 0, len(s.edges))
	for key, e := range s.edges {
		ret = append(ret, s.edgeValue(key, e))
	}
	return ret
}

// edgeValue projects an edge entity and its typed components into the public
// [Edge] type.
func (s *store) edgeValue(key edgeKey, e ecs.Entity) Edge {
	return Edge{
		Source:     key.source,
		Target:     key.target,
		Properties: EdgeProperties{Data: s.edgeData(e, key)},
	}
}

// edgesOfKind returns all edges of a GQL relationship label, optionally
// filtered by source. Pipes are returned one edge per pipe.
func (s *store) edgesOfKind(kind amtypes.RelKind, source string) []Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch kind {
	case amtypes.RelHas:
		return s.has.edges(source)
	case amtypes.RelHasTag:
		return s.hasTag.edges(source)
	case amtypes.RelChildOf:
		return s.childOf.edges(source)
	case amtypes.RelConnectedTo:
		return s.connTo.edges(source)
	case amtypes.RelPipe:
		return s.pipeEdgesUnlocked(source)
	case amtypes.RelRequire:
		return s.require.edges(source)
	case amtypes.RelAdd:
		return s.add.edges(source)
	case amtypes.RelRemove:
		return s.remove.edges(source)
	case amtypes.RelAfter:
		return s.after.edges(source)
	}

	return nil
}

// pipeEdgesUnlocked returns one edge per pipe. The caller must hold the lock.
func (s *store) pipeEdgesUnlocked(source string) []Edge {
	var ret []Edge
	q := s.pipe.f.Query()
	for q.Next() {
		e := q.Entity()
		base := s.em.Get(e)
		if source != "" && base.source != source {
			continue
		}
		pipe := q.Get().MachPipeTo
		ret = append(ret, Edge{
			Source: base.source,
			Target: base.target,
			Properties: EdgeProperties{Data: &EdgeData{
				MachPipesTo: []*MachPipeTo{&pipe},
			}},
		})
	}
	q.Close()

	return ret
}

func (s *store) AdjacencyMap() map[string]map[string]Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ret := make(map[string]map[string]Edge, len(s.verts))
	for hash := range s.verts {
		ret[hash] = make(map[string]Edge)
	}
	for key, e := range s.edges {
		if targets, ok := ret[key.source]; ok {
			targets[key.target] = s.edgeValue(key, e)
		}
	}
	return ret
}

func (s *store) PredecessorMap() map[string]map[string]Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ret := make(map[string]map[string]Edge, len(s.verts))
	for hash := range s.verts {
		ret[hash] = make(map[string]Edge)
	}
	for key, e := range s.edges {
		if sources, ok := ret[key.target]; ok {
			sources[key.source] = s.edgeValue(key, e)
		}
	}
	return ret
}

// shortestPath performs an undirected BFS between two vertices.
func (s *store) shortestPath(from, to string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.verts[from]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrVertexNotFound, from)
	}
	if _, ok := s.verts[to]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrVertexNotFound, to)
	}

	adj := make(map[string][]string, len(s.verts))
	for key := range s.edges {
		adj[key.source] = append(adj[key.source], key.target)
		adj[key.target] = append(adj[key.target], key.source)
	}

	prev := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		if curr == to {
			break
		}
		for _, next := range adj[curr] {
			if _, ok := prev[next]; ok {
				continue
			}
			prev[next] = curr
			queue = append(queue, next)
		}
	}

	if _, ok := prev[to]; !ok {
		return nil, fmt.Errorf("%w: %q -> %q", ErrPathNotFound, from, to)
	}
	path := []string{to}
	for curr := to; curr != from; {
		curr = prev[curr]
		path = append(path, curr)
	}
	slices.Reverse(path)
	return path, nil
}

// ///// ///// /////

// ///// BATCH

// ///// ///// /////

// addBatch creates missing vertices and edges in batches. Existing edges are
// merged with the new metadata.
func (s *store) addBatch(vertices []*Vertex, edges []Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// vertices
	newVerts := make([]*Vertex, 0, len(vertices))
	seen := make(map[string]struct{}, len(vertices))
	for _, v := range vertices {
		if v == nil {
			continue
		}
		h := hash(v)
		if _, ok := s.verts[h]; ok {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		newVerts = append(newVerts, v)
	}
	// validate edge endpoints before creating anything
	for _, edge := range edges {
		if _, ok := seen[edge.Source]; !ok {
			if _, ok := s.verts[edge.Source]; !ok {
				return fmt.Errorf("%w: %q", ErrVertexNotFound, edge.Source)
			}
		}
		if _, ok := seen[edge.Target]; !ok {
			if _, ok := s.verts[edge.Target]; !ok {
				return fmt.Errorf("%w: %q", ErrVertexNotFound, edge.Target)
			}
		}
	}

	if len(newVerts) > 0 {
		idx := 0
		s.vm.NewBatchFn(len(newVerts), func(e ecs.Entity, v *Vertex) {
			*v = *newVerts[idx]
			s.verts[hash(newVerts[idx])] = e
			idx++
		})
	}

	// edges
	for _, edge := range edges {
		key := edgeKey{source: edge.Source, target: edge.Target}
		e, ok := s.edges[key]
		if !ok {
			e = s.em.NewEntity(&edgeBase{source: key.source, target: key.target})
			s.edges[key] = e
		}
		s.mergeEdgeData(e, key, edge.data())
	}

	return nil
}

func (s *store) clone() *store {
	s.mu.RLock()
	verts := s.verticesUnlocked()
	edges := s.edgesUnlocked()
	s.mu.RUnlock()

	ret := newStore(nil)
	_ = ret.addBatch(verts, edges)
	return ret
}

// ///// ///// /////

// ///// EDGE DATA

// ///// ///// /////

// mergeEdgeData applies metadata to an edge entity, preserving the existing
// metadata. The caller must hold the lock.
func (s *store) mergeEdgeData(e ecs.Entity, key edgeKey, data *EdgeData) {
	if data == nil {
		return
	}
	if data.MachHas != nil {
		s.has.set(e, &edgeMachHas{*data.MachHas})
	}
	if data.MachConnectedTo {
		s.connTo.set(e, &edgeMachConnectedTo{})
	}
	if data.MachChildOf {
		s.childOf.set(e, &edgeMachChildOf{})
	}
	if data.MachHasTag {
		s.hasTag.set(e, &edgeMachHasTag{})
	}
	for _, pipe := range data.MachPipesTo {
		if pipe != nil {
			s.addPipeUnlocked(key, pipe)
		}
	}
	for _, rel := range data.StateRelation {
		if rel == nil {
			continue
		}
		switch rel.RelType {
		case am.RelationRequire:
			s.require.set(e, &edgeStateRequire{})
		case am.RelationAdd:
			s.add.set(e, &edgeStateAdd{})
		case am.RelationRemove:
			s.remove.set(e, &edgeStateRemove{})
		case am.RelationAfter:
			s.after.set(e, &edgeStateAfter{})
		}
	}
}

// clearEdgeData removes all metadata components of an edge entity. The caller
// must hold the lock.
func (s *store) clearEdgeData(e ecs.Entity) {
	s.has.clear(e)
	s.connTo.clear(e)
	s.childOf.clear(e)
	s.hasTag.clear(e)
	s.require.clear(e)
	s.add.clear(e)
	s.remove.clear(e)
	s.after.clear(e)
}

// edgeData projects the typed components of an edge entity into [EdgeData].
func (s *store) edgeData(e ecs.Entity, key edgeKey) *EdgeData {
	data := &EdgeData{}

	if c := s.has.get(e); c != nil {
		data.MachHas = &MachineHas{Auto: c.Auto, Multi: c.Multi}
	}
	if s.connTo.has(e) {
		data.MachConnectedTo = true
	}
	if s.childOf.has(e) {
		data.MachChildOf = true
	}
	if s.hasTag.has(e) {
		data.MachHasTag = true
	}
	for _, ent := range s.pipes[key] {
		if c := s.pipe.get(ent); c != nil {
			pipe := c.MachPipeTo
			data.MachPipesTo = append(data.MachPipesTo, &pipe)
		}
	}
	if s.require.has(e) {
		data.StateRelation = append(data.StateRelation,
			&StateRelation{RelType: am.RelationRequire})
	}
	if s.add.has(e) {
		data.StateRelation = append(data.StateRelation,
			&StateRelation{RelType: am.RelationAdd})
	}
	if s.remove.has(e) {
		data.StateRelation = append(data.StateRelation,
			&StateRelation{RelType: am.RelationRemove})
	}
	if s.after.has(e) {
		data.StateRelation = append(data.StateRelation,
			&StateRelation{RelType: am.RelationAfter})
	}

	return data
}

// addPipeUnlocked upserts a pipe entity for a machine pair. The caller must
// hold the lock.
func (s *store) addPipeUnlocked(key edgeKey, pipe *MachPipeTo) {
	for _, e := range s.pipes[key] {
		if c := s.pipe.get(e); c != nil &&
			c.FromState == pipe.FromState && c.ToState == pipe.ToState &&
			c.MutType == pipe.MutType {
			return
		}
	}
	e := s.em.NewEntity(&edgeBase{source: key.source, target: key.target})
	s.pipe.set(e, &edgeMachPipe{MachPipeTo: *pipe})
	s.pipes[key] = append(s.pipes[key], e)
}

// removePipesUnlocked removes all pipe entities of a machine pair. The caller
// must hold the lock.
func (s *store) removePipesUnlocked(key edgeKey) {
	for _, e := range s.pipes[key] {
		s.world.RemoveEntity(e)
	}
	delete(s.pipes, key)
}
