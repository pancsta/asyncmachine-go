package graph

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/mlange-42/ark/ecs"
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

// store is an ark ECS-backed graph store. Vertices and edges are entities with
// a single component, indexed by their string hash.
type store struct {
	mu sync.RWMutex

	world *ecs.World
	verts map[string]ecs.Entity
	edges map[edgeKey]ecs.Entity
	vm    *ecs.Map1[Vertex]
	em    *ecs.Map1[Edge]
	vf    *ecs.Filter1[Vertex]
	ef    *ecs.Filter1[Edge]
}

func newStore(world *ecs.World) *store {
	if world == nil {
		world = ecs.NewWorld()
	}
	return &store{
		world: world,
		verts: make(map[string]ecs.Entity),
		edges: make(map[edgeKey]ecs.Entity),
		vm:    ecs.NewMap1[Vertex](world),
		em:    ecs.NewMap1[Edge](world),
		vf:    ecs.NewFilter1[Vertex](world),
		ef:    ecs.NewFilter1[Edge](world),
	}
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
			continue
		}
		if _, ok := hashes[key.target]; ok {
			s.world.RemoveEntity(e)
			delete(s.edges, key)
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
		delete(s.edges, key)
	}
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
	if e, ok := s.edges[key]; ok {
		edge := s.em.Get(e)
		edge.Properties.Data = mergeEdgeData(edge.data(), data)
		return nil
	}

	s.edges[key] = s.em.NewEntity(&Edge{
		Source:     src,
		Target:     dst,
		Properties: EdgeProperties{Data: data},
	})
	return nil
}

func (s *store) UpdateEdge(src, dst string, data *EdgeData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.edges[edgeKey{source: src, target: dst}]
	if !ok {
		return fmt.Errorf("%w: %q -> %q", ErrEdgeNotFound, src, dst)
	}
	s.em.Get(e).Properties.Data = data
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
	return nil
}

func (s *store) Edge(src, dst string) (Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.edgeUnlocked(src, dst)
}

func (s *store) edgeUnlocked(src, dst string) (Edge, error) {
	e, ok := s.edges[edgeKey{source: src, target: dst}]
	if !ok {
		return Edge{}, fmt.Errorf("%w: %q -> %q", ErrEdgeNotFound, src, dst)
	}
	return *s.em.Get(e), nil
}

func (s *store) Edges() []Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.edgesUnlocked()
}

func (s *store) edgesUnlocked() []Edge {
	ret := make([]Edge, 0, len(s.edges))
	q := s.ef.Query()
	for q.Next() {
		ret = append(ret, *q.Get())
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
	q := s.ef.Query()
	for q.Next() {
		edge := *q.Get()
		if targets, ok := ret[edge.Source]; ok {
			targets[edge.Target] = edge
		}
	}
	q.Close()
	return ret
}

func (s *store) PredecessorMap() map[string]map[string]Edge {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ret := make(map[string]map[string]Edge, len(s.verts))
	for hash := range s.verts {
		ret[hash] = make(map[string]Edge)
	}
	q := s.ef.Query()
	for q.Next() {
		edge := *q.Get()
		if sources, ok := ret[edge.Target]; ok {
			sources[edge.Source] = edge
		}
	}
	q.Close()
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
	q := s.ef.Query()
	for q.Next() {
		edge := *q.Get()
		adj[edge.Source] = append(adj[edge.Source], edge.Target)
		adj[edge.Target] = append(adj[edge.Target], edge.Source)
	}
	q.Close()

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
	known := make(map[string]struct{}, len(s.verts)+len(newVerts))
	for hash := range s.verts {
		known[hash] = struct{}{}
	}
	for _, v := range newVerts {
		known[hash(v)] = struct{}{}
	}
	for _, edge := range edges {
		if _, ok := known[edge.Source]; !ok {
			return fmt.Errorf("%w: %q", ErrVertexNotFound, edge.Source)
		}
		if _, ok := known[edge.Target]; !ok {
			return fmt.Errorf("%w: %q", ErrVertexNotFound, edge.Target)
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
	newEdges := make([]Edge, 0, len(edges))
	newIdx := make(map[edgeKey]int, len(edges))
	for _, edge := range edges {
		key := edgeKey{source: edge.Source, target: edge.Target}
		if e, ok := s.edges[key]; ok {
			stored := s.em.Get(e)
			stored.Properties.Data = mergeEdgeData(stored.data(), edge.data())
			continue
		}
		if idx, ok := newIdx[key]; ok {
			newEdges[idx].Properties.Data = mergeEdgeData(
				newEdges[idx].data(), edge.data())
			continue
		}
		newIdx[key] = len(newEdges)
		newEdges = append(newEdges, edge)
	}
	if len(newEdges) > 0 {
		idx := 0
		s.em.NewBatchFn(len(newEdges), func(e ecs.Entity, edge *Edge) {
			*edge = newEdges[idx]
			s.edges[edgeKey{source: edge.Source, target: edge.Target}] = e
			idx++
		})
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

// mergeEdgeData merges src into dst, preserving the existing metadata.
func mergeEdgeData(dst, src *EdgeData) *EdgeData {
	if src == nil {
		return dst
	}
	if dst == nil {
		return cloneEdgeData(src)
	}
	if src.MachHas != nil {
		dst.MachHas = src.MachHas
	}
	dst.MachConnectedTo = dst.MachConnectedTo || src.MachConnectedTo
	dst.MachChildOf = dst.MachChildOf || src.MachChildOf
	dst.MachHasTag = dst.MachHasTag || src.MachHasTag
	for _, pipe := range src.MachPipesTo {
		if pipe == nil {
			continue
		}
		if slices.ContainsFunc(dst.MachPipesTo, func(p *MachPipeTo) bool {
			return p != nil && p.MutType == pipe.MutType &&
				p.FromState == pipe.FromState && p.ToState == pipe.ToState
		}) {
			continue
		}
		dst.MachPipesTo = append(dst.MachPipesTo, pipe)
	}
	for _, rel := range src.StateRelation {
		if rel == nil {
			continue
		}
		if slices.ContainsFunc(dst.StateRelation, func(r *StateRelation) bool {
			return r != nil && r.RelType == rel.RelType
		}) {
			continue
		}
		dst.StateRelation = append(dst.StateRelation, rel)
	}
	return dst
}
