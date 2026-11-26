// Package ark integrates the ark ECS with asyncmachine graphs and in-memory
// pubsub simulations.
package ark

import (
	"errors"

	"github.com/mlange-42/ark/ecs"

	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// New creates an ark world and a graph backed by it, so custom components and
// the graph can share the same entities.
func New(
	parent *am.Machine, capacity ...int,
) (*ecs.World, *amgraph.Graph, error) {
	world := ecs.NewWorld(capacity...)
	g, err := NewWithWorld(parent, world)
	if err != nil {
		return nil, nil, err
	}

	return world, g, nil
}

// NewWithWorld creates a graph backed by an existing ark world.
func NewWithWorld(
	parent *am.Machine, world *ecs.World,
) (*amgraph.Graph, error) {
	if world == nil {
		return nil, errors.New("nil ark world")
	}

	return amgraph.NewWithWorld(parent, world)
}
