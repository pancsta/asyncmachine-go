package ark

import (
	"fmt"
	"slices"
	"testing"

	"github.com/mlange-42/ark/ecs"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

// TestSimPeers ports the core loop of the libp2p pubsub peer simulator: peers
// join topics, publish messages, and churn, with the peer graph mirrored into
// pkg/graph.
func TestSimPeers(t *testing.T) {
	ctx := t.Context()

	parent := am.New(ctx, nil, &am.Opts{Id: "srv-ark-sim"})
	t.Cleanup(parent.Dispose)

	world, g, err := New(parent, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if world == nil || g == nil {
		t.Fatal("world or graph is nil")
	}

	ps := NewPubSub(world, g)
	const (
		peerCount  = 80
		topicCount = 8
		subsPer    = 3
		msgsPer    = 10
	)

	for i := range peerCount {
		if _, err := ps.AddPeer(fmt.Sprintf("p%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := range topicCount {
		if _, err := ps.AddTopic(fmt.Sprintf("t%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	// deterministic subscriptions, 3 topics per peer
	for i := range peerCount {
		for j := range subsPer {
			name := fmt.Sprintf("t%d", (i+j)%topicCount)
			if err := ps.Subscribe(fmt.Sprintf("p%d", i), name); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got, want := ps.SubscriptionCount(), peerCount*subsPer; got != want {
		t.Fatalf("subscriptions: %d != %d", got, want)
	}
	if got := countEntities(ecs.NewFilter1[Peer](world)); got != peerCount {
		t.Fatalf("peer query: %d != %d", got, peerCount)
	}
	if got := countEntities(ecs.NewFilter1[Topic](world)); got != topicCount {
		t.Fatalf("topic query: %d != %d", got, topicCount)
	}
	if got := countEntities(
		ecs.NewFilter1[Subscription](world),
	); got != peerCount*subsPer {
		t.Fatalf("subscription query: %d", got)
	}

	// publish messages to every topic
	for i := range topicCount {
		for j := range msgsPer {
			delivered, err := ps.Publish(fmt.Sprintf("t%d", i),
				fmt.Sprintf("t%d-msg%d", i, j))
			if err != nil {
				t.Fatal(err)
			}
			if delivered == 0 {
				t.Fatalf("topic t%d has no subscribers", i)
			}
		}
	}
	if got, want := ps.DeliveryCount(), peerCount*subsPer*msgsPer; got != want {
		t.Fatalf("deliveries: %d != %d", got, want)
	}
	if got, want := countEntities(ecs.NewFilter1[Message](world)),
		topicCount*msgsPer; got != want {
		t.Fatalf("message query: %d != %d", got, want)
	}

	// every peer got all messages from its topics
	for i := range peerCount {
		inbox, err := ps.Inbox(fmt.Sprintf("p%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := len(inbox), subsPer*msgsPer; got != want {
			t.Fatalf("p%d inbox: %d != %d", i, got, want)
		}
	}

	// the graph mirrors peers and their topic tags
	inspect, err := g.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(inspect); got != peerCount {
		t.Fatalf("graph machines: %d != %d", got, peerCount)
	}
	for i := range peerCount {
		id := peerMachineID(fmt.Sprintf("p%d", i))
		data, ok := inspect[id]
		if !ok {
			t.Fatalf("%s missing from graph", id)
		}
		if got, want := len(data.Tags), subsPer; got != want {
			t.Fatalf("%s tags: %d != %d", id, got, want)
		}
		if got, want := len(data.States), 1; got != want {
			t.Fatalf("%s states: %d != %d", id, got, want)
		}
	}

	// unsubscribe and verify no further deliveries
	ps.Unsubscribe("p0", "t0")
	if got := ps.SubscriptionCount(); got != peerCount*subsPer-1 {
		t.Fatalf("unsubscribe count: %d", got)
	}
	if tags := ps.Tags("p0"); slices.Contains(tags, TopicTag+":t0") {
		t.Fatalf("stale tag: %v", tags)
	}
	if _, err := ps.Publish("t0", "after-unsub"); err != nil {
		t.Fatal(err)
	}
	inbox, _ := ps.Inbox("p0")
	if slices.Contains(inbox, "after-unsub") {
		t.Fatal("unsubscribed peer received a message")
	}
	// query the world directly for p0's inbox
	p0Q := ecs.NewFilter2[Peer, Inbox](world).Query()
	found := false
	for p0Q.Next() {
		peer, p0Inbox := p0Q.Get()
		if peer.ID != "p0" {
			continue
		}
		found = true
		if slices.Contains(p0Inbox.Messages, "after-unsub") {
			t.Fatal("p0 inbox received a message from an unsubscribed topic")
		}
	}
	p0Q.Close()
	if !found {
		t.Fatal("p0 not found by query")
	}

	// peer churn
	for i := range 10 {
		if err := ps.RemovePeer(fmt.Sprintf("p%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := ps.PeerCount(), peerCount-10; got != want {
		t.Fatalf("peers after churn: %d != %d", got, want)
	}
	inspect, err = g.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(inspect), peerCount-10; got != want {
		t.Fatalf("graph after churn: %d != %d", got, want)
	}

	// remove a topic, dropping its subscriptions
	before := ps.SubscriptionCount()
	if err := ps.RemoveTopic("t1"); err != nil {
		t.Fatal(err)
	}
	after := ps.SubscriptionCount()
	if after >= before {
		t.Fatalf("topic removal did not drop subscriptions: %d >= %d",
			after, before)
	}
	if got, want := ps.TopicCount(), topicCount-1; got != want {
		t.Fatalf("topics: %d != %d", got, want)
	}
}
