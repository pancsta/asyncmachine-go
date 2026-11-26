package ark

import (
	"fmt"
	"slices"

	"github.com/mlange-42/ark/ecs"

	amgraph "github.com/pancsta/asyncmachine-go/pkg/graph"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

const (
	// PeerPrefix is the prefix of peer machine IDs in the graph.
	PeerPrefix = "peer"
	// TopicTag is the peer tag prefix representing a topic subscription.
	TopicTag = "topic"
)

var peerSchema = am.Schema{"Ready": {}}

// Peer is a pubsub peer entity.
type Peer struct {
	ID string
}

// Topic is a pubsub topic entity.
type Topic struct {
	Name string
}

// Subscription connects a peer to a topic.
type Subscription struct {
	Peer  ecs.Entity
	Topic ecs.Entity
}

// Message is a published topic message.
type Message struct {
	Topic ecs.Entity
	Seq   uint64
	Body  string
}

// Inbox holds message bodies delivered to a peer.
type Inbox struct {
	Messages []string
}

// PubSub is an in-memory pubsub simulation backed by an ark world. Peers and
// topics are entities, subscriptions and messages are entities too. When a
// graph is passed, peer machines are mirrored into it with topic tags.
type PubSub struct {
	world *ecs.World
	graph *amgraph.Graph

	peers  *ecs.Map2[Peer, Inbox]
	inbox  *ecs.Map1[Inbox]
	topics *ecs.Map1[Topic]
	subs   *ecs.Map1[Subscription]
	msgs   *ecs.Map1[Message]

	peerFilter  *ecs.Filter1[Peer]
	topicFilter *ecs.Filter1[Topic]
	subFilter   *ecs.Filter1[Subscription]
	msgFilter   *ecs.Filter1[Message]

	peerIDs  map[string]ecs.Entity
	topicIDs map[string]ecs.Entity
	subIDs   map[subKey]ecs.Entity
	peerTags map[string][]string

	seq        uint64
	deliveries int
}

type subKey struct {
	peer  string
	topic string
}

// NewPubSub creates an in-memory pubsub over world. A nil world creates a new
// one. A nil graph disables graph mirroring.
func NewPubSub(world *ecs.World, graph *amgraph.Graph) *PubSub {
	if world == nil {
		world = ecs.NewWorld()
	}
	return &PubSub{
		world:       world,
		graph:       graph,
		peers:       ecs.NewMap2[Peer, Inbox](world),
		inbox:       ecs.NewMap1[Inbox](world),
		topics:      ecs.NewMap1[Topic](world),
		subs:        ecs.NewMap1[Subscription](world),
		msgs:        ecs.NewMap1[Message](world),
		peerFilter:  ecs.NewFilter1[Peer](world),
		topicFilter: ecs.NewFilter1[Topic](world),
		subFilter:   ecs.NewFilter1[Subscription](world),
		msgFilter:   ecs.NewFilter1[Message](world),
		peerIDs:     make(map[string]ecs.Entity),
		topicIDs:    make(map[string]ecs.Entity),
		subIDs:      make(map[subKey]ecs.Entity),
		peerTags:    make(map[string][]string),
	}
}

// AddPeer adds a peer by ID. Returns an error if the peer already exists.
func (p *PubSub) AddPeer(id string) (ecs.Entity, error) {
	if id == "" {
		return ecs.Entity{}, fmt.Errorf("empty peer ID")
	}
	if _, ok := p.peerIDs[id]; ok {
		return ecs.Entity{}, fmt.Errorf("peer %q already exists", id)
	}

	e := p.peers.NewEntity(&Peer{ID: id}, &Inbox{})
	p.peerIDs[id] = e
	p.updatePeerGraph(id)
	return e, nil
}

// RemovePeer removes a peer, its subscriptions, and its graph machine.
func (p *PubSub) RemovePeer(id string) error {
	e, ok := p.peerIDs[id]
	if !ok {
		return fmt.Errorf("peer %q not found", id)
	}

	for key, sub := range p.subIDs {
		if key.peer != id {
			continue
		}
		p.world.RemoveEntity(sub)
		delete(p.subIDs, key)
	}
	delete(p.peerIDs, id)
	delete(p.peerTags, id)
	p.world.RemoveEntity(e)

	if p.graph != nil {
		_ = p.graph.RemoveMachine(peerMachineID(id))
	}
	return nil
}

// AddTopic adds a topic by name. Returns an error if it already exists.
func (p *PubSub) AddTopic(name string) (ecs.Entity, error) {
	if name == "" {
		return ecs.Entity{}, fmt.Errorf("empty topic name")
	}
	if _, ok := p.topicIDs[name]; ok {
		return ecs.Entity{}, fmt.Errorf("topic %q already exists", name)
	}

	e := p.topics.NewEntity(&Topic{Name: name})
	p.topicIDs[name] = e
	return e, nil
}

// RemoveTopic removes a topic and all its subscriptions.
func (p *PubSub) RemoveTopic(name string) error {
	e, ok := p.topicIDs[name]
	if !ok {
		return fmt.Errorf("topic %q not found", name)
	}

	for key, sub := range p.subIDs {
		if key.topic == name {
			p.world.RemoveEntity(sub)
			delete(p.subIDs, key)
			p.removePeerTag(key.peer, name)
		}
	}
	delete(p.topicIDs, name)
	p.world.RemoveEntity(e)
	return nil
}

// Subscribe subscribes a peer to a topic. Re-subscribing is a no-op.
func (p *PubSub) Subscribe(peerID, topicName string) error {
	pe, ok := p.peerIDs[peerID]
	if !ok {
		return fmt.Errorf("peer %q not found", peerID)
	}
	te, ok := p.topicIDs[topicName]
	if !ok {
		return fmt.Errorf("topic %q not found", topicName)
	}

	key := subKey{peer: peerID, topic: topicName}
	if _, ok := p.subIDs[key]; ok {
		return nil
	}
	p.subIDs[key] = p.subs.NewEntity(&Subscription{Peer: pe, Topic: te})
	p.addPeerTag(peerID, topicName)
	return nil
}

// Unsubscribe removes a peer's subscription. Missing subscriptions are a
// no-op.
func (p *PubSub) Unsubscribe(peerID, topicName string) {
	key := subKey{peer: peerID, topic: topicName}
	sub, ok := p.subIDs[key]
	if !ok {
		return
	}
	p.world.RemoveEntity(sub)
	delete(p.subIDs, key)
	p.removePeerTag(peerID, topicName)
}

// Publish publishes a message to a topic and delivers it to all current
// subscribers. It returns the number of deliveries.
func (p *PubSub) Publish(topicName, body string) (int, error) {
	te, ok := p.topicIDs[topicName]
	if !ok {
		return 0, fmt.Errorf("topic %q not found", topicName)
	}

	p.seq++
	p.msgs.NewEntity(&Message{Topic: te, Seq: p.seq, Body: body})

	delivered := 0
	q := p.subFilter.Query()
	for q.Next() {
		sub := q.Get()
		if sub.Topic != te {
			continue
		}
		inbox := p.inbox.Get(sub.Peer)
		inbox.Messages = append(inbox.Messages, body)
		delivered++
	}
	q.Close()
	p.deliveries += delivered
	return delivered, nil
}

// Inbox returns a copy of a peer's delivered messages.
func (p *PubSub) Inbox(peerID string) ([]string, error) {
	e, ok := p.peerIDs[peerID]
	if !ok {
		return nil, fmt.Errorf("peer %q not found", peerID)
	}
	return slices.Clone(p.inbox.Get(e).Messages), nil
}

// Tags returns a copy of a peer's topic tags.
func (p *PubSub) Tags(peerID string) []string {
	return slices.Clone(p.peerTags[peerID])
}

// PeerCount returns the number of peers.
func (p *PubSub) PeerCount() int {
	return countEntities(p.peerFilter)
}

// TopicCount returns the number of topics.
func (p *PubSub) TopicCount() int {
	return countEntities(p.topicFilter)
}

// SubscriptionCount returns the number of subscriptions.
func (p *PubSub) SubscriptionCount() int {
	return countEntities(p.subFilter)
}

// MessageCount returns the number of published messages.
func (p *PubSub) MessageCount() int {
	return countEntities(p.msgFilter)
}

// DeliveryCount returns the total number of deliveries.
func (p *PubSub) DeliveryCount() int {
	return p.deliveries
}

func countEntities[T any](filter *ecs.Filter1[T]) int {
	q := filter.Query()
	count := q.Count()
	q.Close()
	return count
}

// addPeerTag adds a topic tag and mirrors the peer into the graph.
func (p *PubSub) addPeerTag(peerID, topicName string) {
	tag := TopicTag + ":" + topicName
	if slices.Contains(p.peerTags[peerID], tag) {
		return
	}
	p.peerTags[peerID] = append(p.peerTags[peerID], tag)
	p.updatePeerGraph(peerID)
}

// removePeerTag removes a topic tag and mirrors the peer into the graph.
func (p *PubSub) removePeerTag(peerID, topicName string) {
	tag := TopicTag + ":" + topicName
	tags := p.peerTags[peerID]
	if idx := slices.Index(tags, tag); idx >= 0 {
		p.peerTags[peerID] = slices.Delete(tags, idx, idx+1)
	}
	p.updatePeerGraph(peerID)
}

// updatePeerGraph mirrors a peer into the graph as a machine with topic tags.
func (p *PubSub) updatePeerGraph(peerID string) {
	if p.graph == nil {
		return
	}
	_ = p.graph.AddMachine(&amgraph.Machine{
		Serialized: &am.Serialized{
			ID:         peerMachineID(peerID),
			StateNames: am.S{"Ready"},
			Time:       am.Time{1},
			Tags:       slices.Clone(p.peerTags[peerID]),
		},
		Schema:     peerSchema,
		SchemaHash: "ark-peer",
		MTimeSum:   1,
	})
}

func peerMachineID(peerID string) string {
	return PeerPrefix + "-" + peerID
}
