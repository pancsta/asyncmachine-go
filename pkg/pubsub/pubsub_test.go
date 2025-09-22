package pubsub

import (
	"context"
	"math/rand"
	"os"
	"runtime/trace"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"

	amtele "github.com/pancsta/asyncmachine-go/pkg/telemetry"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	amhelpt "github.com/pancsta/asyncmachine-go/pkg/helpers/testing"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
)

var workerSchema = am.Schema{
	"Foo":             {Require: am.S{"Bar"}},
	"Bar":             {},
	am.StateException: {},
}
var workerStates = am.S{am.StateException, "Foo", "Bar"}

// TODO automate in NewMirror, see pkg/rpc.Server
type mirrorHandlers struct {
	// err
	ExceptionState am.HandlerFinal
	ExceptionEnd   am.HandlerFinal

	// status
	JoinedState              am.HandlerFinal
	JoinedEnd                am.HandlerFinal
	MissPeersByUpdatesState  am.HandlerFinal
	MissPeersByUpdatesEnd    am.HandlerFinal
	MissPeersByGossipState   am.HandlerFinal
	MissPeersByGossipEnd     am.HandlerFinal
	MissUpdatesByGossipState am.HandlerFinal
	MissUpdatesByGossipEnd   am.HandlerFinal

	// actions
	SendUpdatesState       am.HandlerFinal
	SendUpdatesEnd         am.HandlerFinal
	SendGossipsState       am.HandlerFinal
	SendGossipsEnd         am.HandlerFinal
	SendInfoState          am.HandlerFinal
	SendInfoEnd            am.HandlerFinal
	ReqMissingUpdatesState am.HandlerFinal
	ReqMissingUpdatesEnd   am.HandlerFinal
	ReqMissingPeersState   am.HandlerFinal
	ReqMissingPeersEnd     am.HandlerFinal
	MsgReqUpdatesState     am.HandlerFinal
	MsgReqUpdatesEnd       am.HandlerFinal
	MsgReqInfoState        am.HandlerFinal
	MsgReqInfoEnd          am.HandlerFinal
	SendMsgState           am.HandlerFinal
	ProcessMsgsState       am.HandlerFinal
}

var mirrorStates = am.S{
	am.StateException,
	ss.Joined, ss.Ready, ss.MissPeersByUpdates, ss.MissPeersByGossip,
	ss.MissUpdatesByGossip, ss.SendGossips, ss.SendUpdates, ss.SendInfo,
	ss.MsgReqUpdates, ss.MsgReqInfo, ss.ProcessMsgs, ss.ReqMissingUpdates,
	ss.ReqMissingPeers, ss.SendMsg,
}

func init() {
	_ = godotenv.Load()

	if os.Getenv(am.EnvAmTestDebug) != "" {
		amhelp.EnableDebugging(true)
	}
}

func Test1Peer(t *testing.T) {
	// t.Parallel()
	// amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ps, err := NewTopic(ctx, t.Name(), "p0-root", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	addr, err := ma.NewMultiaddr("/ip4/127.0.0.1/udp/0/quic-v1")
	if err != nil {
		t.Fatal(err)
	}

	// start and wait
	ps.ListenAddrs = []ma.Multiaddr{addr}
	defer ps.Dispose()
	ps.Start()
	// root doesnt connect
	select {
	case <-ps.Mach.WhenErr(ctx):
		t.Fatalf("Err: %s", ps.Mach.Err())
	case <-ps.Mach.When1(ss.Started, nil):
		// pass
	}

	t.Logf("addrs: %s", *ps.Addrs.Load())
	assert.NotEmpty(t, *ps.Addrs.Load(), "addrs should not be empty")
}

func Test2Peers(t *testing.T) {
	// t.Parallel()
	amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// pubsub 0 (root)
	p0, err := NewTopic(ctx, t.Name(), "p0-root", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	addr, err := ma.NewMultiaddr("/ip4/127.0.0.1/udp/0/quic-v1")
	if err != nil {
		t.Fatal(err)
	}

	// start and wait
	p0.ListenAddrs = []ma.Multiaddr{addr}
	defer p0.Dispose()
	p0.Start()
	// root doesnt connect or join
	amhelpt.WaitForErrAll(t, "p0-started", ctx, p0.Mach, time.Second,
		p0.Mach.When1(ss.Started, nil))
	p0.Mach.Add1(ss.Connected, nil)
	p0.Mach.Add1(ss.Joining, nil)
	amhelpt.WaitForErrAll(t, "p0-joined", ctx, p0.Mach, time.Second,
		p0.Mach.When1(ss.Joined, nil))

	// pubsub 1
	p1, err := NewTopic(ctx, t.Name(), "p1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	p1.ConnAddrs = *p0.Addrs.Load()
	p1.ListenAddrs = []ma.Multiaddr{addr}
	t.Logf("p1 conn addrs: %s", p1.ConnAddrs)

	// start and wait
	defer p1.Dispose()
	p1.Start()
	_, _ = amhelp.NewReqAdd1(p1.Mach, ss.Joining, nil).Run(ctx)
	amhelpt.WaitForErrAll(t, "p1-joins", ctx, p1.Mach, time.Second,
		p1.Mach.When1(ss.Joined, nil))

	// wait for PeerJoined in p0
	amhelpt.WaitForErrAll(t, "p1-joins-p0", ctx, p0.Mach, time.Second,
		p0.Mach.WhenTime(am.S{ss.PeerJoined}, am.Time{1}, nil))
}

func TestExposing(t *testing.T) {
	if os.Getenv(amhelp.EnvAmTestRunner) != "" {
		// 100 peers with 3 workers each should finish in <1m
		t.Skip("FLAKY") // TODO FLAKY
		return
	}

	// t.Parallel()
	amhelp.EnableDebugging(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// pubsub 0 (root)
	machs0 := []*am.Machine{RandMach(ctx, "1", nil)}
	p0, err := NewTopic(ctx, t.Name(), "p0-root", machs0, nil)
	if err != nil {
		t.Fatal(err)
	}

	addr, err := ma.NewMultiaddr("/ip4/127.0.0.1/udp/0/quic-v1")
	if err != nil {
		t.Fatal(err)
	}

	// start and wait
	p0.ListenAddrs = []ma.Multiaddr{addr}
	defer p0.Dispose()
	p0.Start()
	// root doesnt connect or join
	amhelpt.WaitForErrAll(t, "p0-started", ctx, p0.Mach, time.Second,
		p0.Mach.When1(ss.Started, nil))
	p0.Mach.Add1(ss.Connected, nil)
	p0.Mach.Add1(ss.Joining, nil)
	amhelpt.WaitForErrAll(t, "p0-joined", ctx, p0.Mach, time.Second,
		p0.Mach.When1(ss.Joined, nil))

	// pubsub 1
	machs1 := []*am.Machine{RandMach(ctx, "1", nil)}
	p1, err := NewTopic(ctx, t.Name(), "p1", machs1, nil)
	if err != nil {
		t.Fatal(err)
	}
	p1.ConnAddrs = *p0.Addrs.Load()
	p1.ListenAddrs = []ma.Multiaddr{addr}
	t.Logf("p1 conn addrs: %s", p1.ConnAddrs)

	// start and wait
	defer p1.Dispose()
	p1.Start()
	_, _ = amhelp.NewReqAdd1(p1.Mach, ss.Joining, nil).Run(ctx)
	amhelpt.WaitForErrAll(t, "p1-joins", ctx, p1.Mach, time.Second,
		p1.Mach.When1(ss.Joined, nil))

	// wait for PeerJoined in p0
	amhelpt.WaitForErrAll(t, "p1-joins-p0", ctx, p0.Mach, time.Second,
		p0.Mach.WhenTime(am.S{ss.PeerJoined}, am.Time{1}, nil))

	// TEST

	// test update 1 -> 0
	machs1[0].Add1("Bar", nil)
	amhelpt.WaitForAll(t, "p0-gets-update", ctx, time.Second,
		p0.Mach.WhenTime(am.S{ss.MsgInfo}, am.Time{1}, nil))

	// test update 0 -> 1
	machs0[0].Add1("Bar", nil)
	amhelpt.WaitForAll(t, "p1-gets-update", ctx, time.Second,
		p1.Mach.WhenTime(am.S{ss.MsgInfo}, am.Time{1}, nil))

	// TODO list machs and assert clocks
	amhelpt.AssertNoErrEver(t, p0.Mach)
	amhelpt.AssertNoErrEver(t, p1.Mach)
	assert.Len(t, p0.workers[p1.host.ID().String()], 1)
	assert.Len(t, p1.workers[p0.host.ID().String()], 1)
}

// https://github.com/quic-go/quic-go/wiki/UDP-Buffer-Sizes
// replace prom/client_golang with a no-op to prevent a goroutine leak
func TestExposingMany(t *testing.T) {
	if os.Getenv(amhelp.EnvAmTestRunner) != "" {
		// 100 peers with 3 workers each should finish in <1m
		t.Skip("LONG TEST")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	metrics := false

	reg := trace.StartRegion(ctx, "init")

	// TODO DEBUG
	// IPFS_LOGGING=info;AM_PUBSUB_LOG=1
	amhelp.EnableDebugging(false)
	// os.Setenv(amtele.EnvAmDbgAddr, "localhost:6831")
	// os.Setenv("IPFS_LOGGING", "info")
	// os.Setenv(EnvAmPubsubLog, "1")
	// os.Setenv(am.EnvAmLog, strconv.Itoa(int(am.LogExternal)))
	// os.Setenv(EnvAmPubsubDbg, "1")
	metrics = true

	amountPeers := 20
	// amountPeers := 200
	// amountPeers := 20 // 220*4 + 57*20 + 20 == 1200
	amountSources := 3
	parallel := 10

	// init PubSub root peer
	root, err := newPsRoot(t, ctx)
	if err != nil {
		t.Fatal(err)
	}

	// init N-amount of other peers
	// TODO single Heartbeat for all peers
	mx := sync.Mutex{}
	machs := make(map[int][]*am.Machine)
	peers := make(map[int]*Topic)
	var addrs []ma.Multiaddr
	eg := errgroup.Group{}
	eg.SetLimit(parallel)
	started := 0

	// min 6 peers
	amountPeers = max(6, amountPeers)
	host2Peer := map[string]string{
		root.host.ID().String(): "P0",
	}
	for i := range amountPeers {
		// skip root
		if i == 0 {
			peers[i] = root
			continue
		}
		iS := strconv.Itoa(i)

		// fork
		eg.Go(func() error {
			mx.Lock()
			addrClone := slices.Clone(addrs)
			mx.Unlock()

			pMachs, ps, err := newPsPeer(t, ctx, "p"+iS,
				*root.Addrs.Load(), addrClone, amountSources)
			if err != nil {
				t.Logf("failed to create peer %d: %s", i, err)
				return nil
			}
			ps.Mach.OnError(func(mach *am.Machine, err error) {
				t.Logf("error: %s", err)
			})
			// TODO DEBUG make it faster
			// ps.Multiplayer = 1

			mx.Lock()
			defer mx.Unlock()
			machs[i] = pMachs
			peers[i] = ps
			addrs = append(addrs, *ps.Addrs.Load()...)
			host2Peer[ps.host.ID().String()] = "P" + iS

			t.Logf("Peer %d STARTED", i)
			started++

			return nil
		})
	}
	_ = eg.Wait()

	// inject peer names
	for i := range amountPeers {
		peers[i].HostToPeer = host2Peer
	}

	// METRICS MACHINE

	// inject metrics TODO extract
	if metrics {
		for i := range amountPeers {
			schema, names := genMetricMach(i, amountPeers)

			// mirror mach
			id := "p" + strconv.Itoa(i) + "-metrics"
			mirror, err := amhelp.NewMirror(id, false, peers[i].Mach,
				&mirrorHandlers{}, mirrorStates)
			if err != nil {
				t.Fatal(err)
			}

			// metrics states
			peers[i].HostToPeer = host2Peer
			err = amhelp.CopySchema(schema, mirror, names)
			if err != nil {
				t.Fatal(err)
			}

			// TODO add groups to metrics

			// TODO DEBUG
			// mirror.SemLogger().SetLevel(0)
			err = amtele.TransitionsToDbg(mirror, "")
			if err != nil {
				t.Fatal(err)
			}

			// set and do the initial sync
			peers[i].MachMetrics.Store(mirror)
			peers[i].syncMetrics()
		}
	}

	if started < amountPeers-1 {
		t.Fatalf("failed to start %d peers out of %d", started-amountPeers-1,
			amountPeers-1)
		panic("t.Fatal")
	}

	reg.End()
	reg = trace.StartRegion(ctx, "joining")

	t.Logf("Started %d peers, joining...", started)
	for i, ps := range peers {
		// skip root
		if i == 0 {
			continue
		}
		// dont flood
		if i%10 == 0 {
			time.Sleep(time.Second)
			t.Log("break...")
		}
		// join
		ps.Mach.Add1(ss.Joining, nil)
	}

	reg.End()
	reg = trace.StartRegion(ctx, "mutating")

	t.Logf("Mutating Peer 5 (%d machines)", len(machs[5]))
	for _, mach := range machs[5] {
		mach.Add1("Bar", nil)
	}

	// time of MachJoined in each peer after the network is settled
	// peers - root - self * exposed_machs
	expLocalWorkers := (amountPeers - 2) * amountSources
	peersSynced := 0
	p5Id := peers[5].host.ID().String()

	t.Log("Discovering...")
	// go func() {
	// 	time.Sleep(time.Second)
	// 	os.Exit(0)
	// }()

	// assert the propagation to other peers
	discovered := []int{2} // p0, p5
	round := 0
	for len(discovered) < len(peers)-2 {
		if round != 0 {
			t.Log("-----")
			t.Logf("Discovery round %d", round)
			t.Log("-----")
			time.Sleep(time.Second)
		}
		for i, ps := range peers {
			if i == 5 || i == 0 || slices.Contains(discovered, i) {
				continue
			}

			// wait for others to join this peer
			t.Logf("Peer %d: wait for %d workers...", i, expLocalWorkers)
			// get ALL local workers
			ch := make(chan []*rpc.Worker, 1)
			args := &A{WorkersCh: ch}
			reqListMachs := amhelp.NewReqAdd1(ps.Mach, ss.ListMachines, Pass(args))

			// execute
			res, _ := reqListMachs.Delay(time.Second).Run(ctx)
			if res == am.Canceled {
				t.Logf("Peer %d: unable to list machines", i)
				close(ch)
				continue
			}
			p5Workers := <-ch
			if len(p5Workers) >= expLocalWorkers {
				t.Logf("Peer %d: DISCOVERY OK", i)
				// mark in dbg
				if metrics := peers[i].MachMetrics.Load(); metrics != nil {
					(*metrics).Add1("Ready", nil)
				}
				// memorize to skip
				discovered = append(discovered, i)
			} else {
				t.Logf("Peer %d: has only %d workers", i, len(p5Workers))
				continue
			}
			close(ch)
		}

		round++
	}

	t.Log("Discovery OK")

	reg.End()
	reg = trace.StartRegion(ctx, "syncing")

	// assert the propagation to other peers
	for i, ps := range peers {
		if i == 5 || i == 0 {
			continue
		}
		t.Logf("Waiting for Peer %d", i)

		// get remote workers from peer5
		ch := make(chan []*rpc.Worker, 1)
		args := &A{
			WorkersCh: ch,
			ListFilters: &ListFilters{
				PeerId: p5Id,
			},
		}
		reqListMachs := amhelp.NewReqAdd1(ps.Mach, ss.ListMachines, Pass(args))

		// execute
		res, _ := reqListMachs.Delay(time.Second).Run(ctx)
		// t.Logf("Peer %d: getting p5 local workers", i)
		if res == am.Canceled {
			t.Logf("Peer %d: p5 peers CANCELED", i)
			close(ch)
			continue
		}
		p5Workers := <-ch
		close(ch)
		// t.Logf("Peer %d: got %d p5 local workers", i, len(p5Workers))
		if len(p5Workers) < amountSources {
			t.Fatalf("Peer %d: not enough p5 workers", i)
		}

		// check if all peer5 workers have state changes
		ok := true
		for _, mach := range p5Workers {
			// wait with a timeout
			_ = amhelp.WaitForAll(ctx, 10*time.Second, mach.When1("Bar", nil))
			if mach.Not1("Bar") {
				t.Logf("Peer %d NOT updated", i)
				ok = false
			}
		}
		if !ok {
			t.Logf("Peer %d NOT updated", i)
			continue
		}
		t.Logf("Peer %d UPDATED", i)
		peersSynced++
	}

	// TODO this can be flaky due to HandlerTimeout
	// for _, ps := range peers {
	// 	amhelpt.AssertNoErrNow(t, ps.Mach)
	// }

	assert.GreaterOrEqual(t, float64(peersSynced), float64(amountPeers-2)*0.9,
		">= 90% peers synced")

	reg.End()
	t.Log("DONE!")

	// dispose
	wg := sync.WaitGroup{}
	for _, ps := range peers {
		wg.Add(1)
		go func() {
			ps.Dispose()
			_ = amhelp.WaitForAll(ctx, 100*time.Millisecond,
				ps.Mach.When1(ss.Disposed, nil))
			ps.Mach.Dispose()
			wg.Done()
		}()
	}
	wg.Wait()

	// flush dbg
	time.Sleep(100 * time.Millisecond)
}

// ///// ///// /////

// ///// UTILS

// ///// ///// /////

func newPsRoot(t *testing.T, ctx context.Context) (*Topic, error) {
	// init pubsub
	ps, err := NewTopic(ctx, t.Name(), "p0-root", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ps.transportUds = true
	ps.TestSchema = workerSchema
	ps.TestStates = workerStates

	addrs := []string{
		"/unix//tmp/amtest-root",
		// "/ip4/127.0.0.1/udp/0/quic-v1",
		// "/ip4/127.0.0.1/udp/0/quic-v1/webtransport",
		// "/ip4/127.0.0.1/udp/0/webrtc-direct",
	}
	for _, a := range addrs {
		a, err := ma.NewMultiaddr(a)
		if err != nil {
			t.Fatal(err)
		}
		ps.ListenAddrs = append(ps.ListenAddrs, a)
	}

	// listen
	ps.Start()
	// root doesnt connect
	<-ps.Mach.When1(ss.Started, ctx)
	ps.Mach.Add1(ss.Connected, nil)
	ps.Mach.Add1(ss.Joining, nil)
	<-ps.Mach.When1(ss.Joined, ctx)

	return ps, err
}

func newPsPeer(
	t *testing.T, ctx context.Context, name string, rootAddrs []ma.Multiaddr,
	connAddrs []ma.Multiaddr, amount int,
) ([]*am.Machine, *Topic, error) {
	// init state sources (grouped)
	machRoot := RandMach(ctx, name, nil)
	machs := make([]*am.Machine, amount)
	for i := range amount {
		machs[i] = RandMach(ctx, name+"-"+strconv.Itoa(i), machRoot)
	}

	// init pubsub
	ps, err := NewTopic(ctx, t.Name(), name, machs, nil)
	if err != nil {
		t.Fatal(err)
	}
	ps.transportUds = true
	ps.TestSchema = workerSchema
	ps.TestStates = workerStates

	addrs := []string{
		"/unix//tmp/amtest-" + name,
		// "/ip4/127.0.0.1/udp/0/quic-v1",
		// "/ip4/127.0.0.1/udp/0/quic-v1",
		// "/ip4/127.0.0.1/tcp/0",
		// "/ip4/127.0.0.1/udp/0/quic-v1/webtransport",
		// "/ip4/127.0.0.1/udp/0/webrtc-direct",
	}
	for _, a := range addrs {
		a, err := ma.NewMultiaddr(a)
		if err != nil {
			t.Fatal(err)
		}
		ps.ListenAddrs = append(ps.ListenAddrs, a)
	}

	n := 10
	// pick N last addrs
	// if len(connAddrs) < n {
	// 	n = len(connAddrs)
	// }
	// pickedAddrs := connAddrs[len(connAddrs)-n:]

	// pick N random addrs
	pickedAddrs := []ma.Multiaddr{}
	pickedAddrsStr := []string{}
	for range n {
		if len(connAddrs) == 0 {
			break
		}
		a := connAddrs[rand.Intn(len(connAddrs))]
		if !slices.Contains(pickedAddrsStr, a.String()) {
			pickedAddrs = append(pickedAddrs, a)
			pickedAddrsStr = append(pickedAddrsStr, a.String())
		}
	}

	// always include root addr
	ps.ConnAddrs = slices.Concat(rootAddrs, pickedAddrs)
	// ps.ConnAddrs = randAddrs
	ps.Start()
	<-ps.Mach.When1(ss.Connected, ctx)
	// <-ps.Mach.When1(ss.Ready, ctx)

	return machs, ps, err
}

func genMetricMach(pid int, numPeers int) (am.Schema, am.S) {
	names := am.S{"Peer", "Ready"}
	schema := am.Schema{
		"Peer":  {Multi: true},
		"Ready": {},
	}
	for i := 0; i < numPeers; i++ {
		// skip self
		if i == pid {
			continue
		}

		id := "P" + strconv.Itoa(i)
		names = append(names, id+"Gossip", id+"Info", id+"Update")
		schema[id+"Gossip"] = am.State{Multi: true}
		schema[id+"Info"] = am.State{
			Multi: true,
			Add:   am.S{id + "Gossip"},
		}
		schema[id+"Update"] = am.State{
			Multi: true,
			Add:   am.S{id + "Info"},
		}
	}

	return schema, names
}

func RandMach(
	ctx context.Context, suffix string, parent *am.Machine,
) *am.Machine {
	opts := &am.Opts{
		Id: "rand-" + suffix,
	}
	if parent != nil {
		opts.Parent = parent
	}
	m := am.New(ctx, workerSchema, opts)
	if err := m.VerifyStates(workerStates); err != nil {
		panic(err)
	}
	// TODO DEBUG
	if strings.HasPrefix(suffix, "p5") {
		amhelp.MachDebugEnv(m)
		// amtele.TransitionsToDbg(m, "")
	}
	return m
}
