package pubsub

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p"
	ps "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	quicTransport "github.com/libp2p/go-libp2p/p2p/transport/quic"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/vmihailenco/msgpack"
	"golang.org/x/sync/errgroup"

	"github.com/pancsta/asyncmachine-go/internal/utils"
	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/pubsub/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
	ssam "github.com/pancsta/asyncmachine-go/pkg/states"
)

// Topic is a single-topic PubSub based on lib2p-pubsub with manual discovery.
// Each peer can have many exposed state machines and broadcasts their clock
// changes on the channel, introduces them when joining and via private messages
// to newly joined peers.
// TODO optimizations:
//   - predefined schemas
//   - random delay for initial hello
//   - reduce number of messages (max total rcv/snt msgs per a time window)
//   - https://github.com/pancsta/asyncmachine-go/issues/220
//   - dont gossip about missing updates
type Topic struct {
	*am.ExceptionHandler

	// T represents the PubSub topic used for communication.
	T *ps.Topic
	// Mach is a state machine for this PubSub.
	Mach *am.Machine
	// ListenAddrs contains the list of multiaddresses that this topic listens on.
	// None will allocate one automatically.
	ListenAddrs []ma.Multiaddr
	// Addrs is a list of addresses for this peer.
	Addrs atomic.Pointer[[]ma.Multiaddr]
	// Name indicates the name of the channel or topic instance,
	// typically associated with a given process.
	Name string
	// ConnAddrs contains a list of addresses for initial
	// connections to discovery nodes in the network.
	ConnAddrs []ma.Multiaddr
	// List of exposed state machines, index => mach_id, indexes are used on the
	// channel. Indexes should NOT change, as they are used for addressing. `nil`
	// value means empty.
	ExposedMachs []*am.Machine
	// Debounce for clock broadcasts (separate per each exposed state machine).
	Debounce   time.Time
	LogEnabled bool
	// HeartbeatFreq broadcasts changed clocks of exposed machines.
	HeartbeatFreq time.Duration
	// Maximum msgs per minute in the network,
	MaxMsgsPerWin int
	// DebugWorkerTelemetry exposes local workers to am-dbg
	DebugWorkerTelemetry bool
	ConnectionsToReady   int

	// THIS PEER

	// host represents the local libp2p host used for handling
	// peer-to-peer connections.
	host host.Host
	// ps is the underlying Gossipsub instance, used for managing
	// PubSub operations like subscriptions and publishing.
	ps                *ps.PubSub
	sub               *ps.Subscription
	handler           *ps.TopicEventHandler
	sendHelloDebounce time.Duration

	// OTHER PEERS

	// workers is a list of machines from other peers, indexed by peer ID and
	// [Info.Index1]. Use [states.TopicStatesDef.ListMachines] to list
	// them.
	workers map[string]map[int]*rpc.Worker
	info    map[string]map[int]*Info
	pool    *errgroup.Group
	// gossips about local workers heard from other peers
	missingUpdates PeerGossips
	// tracers attached to ExposedMachs.
	tracers []*Tracer
	// clock updated which arrived before MsgInfo TODO GC
	pendingMachUpdates map[string]map[int]am.Time
	// TODO verify number of exposed workers
	missingPeers  map[string]struct{}
	lastReqHello  time.Time
	lastReqUpdate time.Time
	// last MsgInfo request seen per peer
	reqInfo map[string]time.Time
	// last MsgUpdates request seen per peer
	reqUpdate map[string]time.Time
	// TODO proper retry
	retried bool
	// current messages time window (10s).
	msgsWin int
	// msgs send in the current messages time window
	msgsWinCount int
}

func NewTopic(
	ctx context.Context, name, suffix string, exposedMachs []*am.Machine,
	opts *TopicOpts,
) (*Topic, error) {
	if opts == nil {
		opts = &TopicOpts{}
	}

	t := &Topic{
		Name:                 name,
		ExposedMachs:         exposedMachs,
		LogEnabled:           os.Getenv(EnvAmPubsubLog) != "",
		DebugWorkerTelemetry: os.Getenv(EnvAmPubsubDbg) != "",
		HeartbeatFreq:        time.Second,
		MaxMsgsPerWin:        100,
		ConnectionsToReady:   5,

		tracers: make([]*Tracer, len(exposedMachs)),
		workers: make(map[string]map[int]*rpc.Worker),
		// TODO config
		pool:               amhelp.Pool(10),
		info:               make(map[string]map[int]*Info),
		sendHelloDebounce:  time.Millisecond * 500,
		pendingMachUpdates: make(map[string]map[int]am.Time),
		missingPeers:       make(map[string]struct{}),
		missingUpdates:     make(PeerGossips),
		reqInfo:            make(map[string]time.Time),
		reqUpdate:          make(map[string]time.Time),
	}

	// attach tracers
	for i, mach := range exposedMachs {
		tracer := &Tracer{}
		err := mach.BindTracer(tracer)
		if err != nil {
			return nil, err
		}
		t.tracers[i] = tracer
	}

	if suffix == "" {
		suffix = utils.RandId(2)
	}

	mach, err := am.NewCommon(ctx, "ps-"+name+"-"+suffix, states.TopicSchema,
		ss.Names(), t, opts.Parent, &am.Opts{
			Tags: []string{"pubsub:" + name},

			// TODO replace the low queue limit with timeouts
			//  once [am.Event.AcceptTimeout] is implemented
			//  https://github.com/pancsta/asyncmachine-go/issues/220
			HandlerTimeout: time.Minute,
			QueueLimit:     10,
		})
	if err != nil {
		return nil, err
	}

	mach.SetLogArgs(LogArgs)
	t.Mach = mach

	return t, nil
}

// ///// ///// /////

// ///// HANDLERS

// ///// ///// /////

func (t *Topic) ExceptionState(e *am.Event) {
	// super
	t.ExceptionHandler.ExceptionState(e)
	args := am.ParseArgs(e.Args)
	err := args.Err
	target := args.TargetStates

	// ignore ErrEvalTimeout
	if errors.Is(err, am.ErrEvalTimeout) {
		t.Mach.EvRemove1(e, ss.Exception, nil)
		return
	}

	// retry JoiningState timeouts (once)
	if errors.Is(err, am.ErrHandlerTimeout) &&
		slices.Contains(target, ss.ErrJoining) && !t.retried {
		t.retried = true
		t.Mach.EvRemove1(e, ss.Exception, nil)
	}
}

// TODO exit
func (t *Topic) ReadyEnter(e *am.Event) bool {
	return t.ConnCount() >= t.ConnectionsToReady
}

func (t *Topic) StartEnter(e *am.Event) bool {
	return len(t.ListenAddrs) > 0
}

func (t *Topic) StartState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ss.Start)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}
		t.Mach.PanicToErrState(ss.ErrJoining, nil)

		// TODO cache
		privk, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 0)
		if err != nil {
			_ = AddErrJoining(e, t.Mach, err, nil)
			return
		}

		// new libp2p host
		host, err := libp2p.New(
			libp2p.Transport(quicTransport.NewTransport),
			// libp2p.Transport(webtransport.New),
			// libp2p.Transport(webrtc.New),
			libp2p.ListenAddrs(t.ListenAddrs...),
			// TODO debug
			// libp2p.ResourceManager(&network.NullResourceManager{}),
			libp2p.Identity(privk))

		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			_ = AddErrJoining(e, t.Mach, err, nil)
			return
		}
		t.host = host

		// TODO late tags, init earlier and append tags
		self := host.ID().String()
		tags := t.Mach.Tags()
		tags = append(tags, "peer:"+self)
		t.Mach.SetTags(tags)
		amhelp.MachDebugEnv(t.Mach)

		resources := relayv2.DefaultResources()
		resources.MaxReservations = 256
		_, err = relayv2.New(t.host, relayv2.WithResources(resources))
		if err != nil {
			_ = AddErrJoining(e, t.Mach, err, nil)
			return
		}

		// new gossipsub
		gossip, err := ps.NewGossipSub(ctx, host)
		if err != nil {
			_ = AddErrJoining(e, t.Mach, err, nil)
			return
		}
		t.ps = gossip

		// setup mDNS discovery to find local peers
		// TODO doesnt trigger any discoveries
		// s := mdns.NewMdnsService(t.host, "am-"+t.Name,
		//   &discoveryNotifee{h: t.host})
		// err = s.Start()
		// if err != nil {
		// 	_ = AddErrJoining(e, t.Mach, err, nil)
		// 	return
		// }

		// address
		addrs, err := t.GetPeerAddrs()
		if err != nil {
			_ = AddErrJoining(e, t.Mach, err, nil)
			return
		}
		t.Addrs.Store(&addrs)
		t.Mach.Log("Listening on %s", addrs)
		t.Mach.EvAdd1(e, ss.Connecting, nil)

		// mark as completed
		t.Mach.EvAdd1(e, ss.Started, Pass(&A{
			PeerId: host.ID().String(),
		}))

		// start Heartbeat
		tick := time.NewTicker(t.HeartbeatFreq)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return // expired
			case <-tick.C:
				t.Mach.EvRemove1(e, ss.Heartbeat, nil)
				t.Mach.EvAdd1(e, ss.Heartbeat, nil)
			}
		}
	}()
}

func (t *Topic) StartEnd(e *am.Event) {
	if t.host != nil {
		t.host.Close()
	}
	t.host = nil
	t.ps = nil
}

func (t *Topic) ConnectingEnter(e *am.Event) bool {
	return len(t.ConnAddrs) > 0
}

func (t *Topic) ConnectingState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ss.Connecting)

	// TODO
	// t.host.Peerstore().AddAddrs()
	// t.ConnAddrs[0].Equal()

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// errgroup with conc limit
		g := errgroup.Group{}
		// TODO config
		g.SetLimit(10)

		// try all the addrs
		for _, addr := range t.ConnAddrs {
			g.Go(func() error {
				// stop if state ctx expired
				if ctx.Err() != nil {
					return ctx.Err()
				}

				// extract infos and connect
				infos, err := peer.AddrInfosFromP2pAddrs(addr)
				if err != nil {
					err := fmt.Errorf("%w: %s", err, addr)
					_ = ssam.AddErrConnecting(e, t.Mach, err, nil)
					// dont stop, no err
					return nil
				}
				for _, addrInfo := range infos {
					t.log("Trying %s", addrInfo)
					err := t.host.Connect(ctx, addrInfo)
					if err == nil {
						return nil // connected
					}
				}

				// dont stop, no err
				return nil
			})
		}

		// block
		_ = g.Wait()
		if ctx.Err() != nil {
			return // expired
		}

		// check if successful
		if t.ConnCount() <= 0 {
			err := errors.New("failed to establish any connections")
			_ = ssam.AddErrConnecting(e, t.Mach, err, nil)
			return
		}

		// next
		t.Mach.EvAdd1(e, ss.Connected, nil)
	}()
}

func (t *Topic) ConnCount() int {
	return t.host.ConnManager().(*connmgr.BasicConnMgr).GetInfo().ConnCount
}

func (t *Topic) DisconnectingStart(e *am.Event) {
}

func (t *Topic) JoiningState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ss.Joining)

	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// join topic
		topic, err := t.ps.Join(t.Name)
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			_ = AddErrJoining(e, t.Mach, err, nil)
			return
		}
		t.T = topic

		// msg subscription
		subscription, err := t.T.Subscribe()
		if ctx.Err() != nil {
			return // expired
		}
		if err != nil {
			_ = AddErrJoining(e, t.Mach, err, nil)
			return
		}
		t.sub = subscription

		// next
		t.Mach.EvAdd1(e, ss.Joined, nil)
	}()
}

func (t *Topic) JoinedState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ss.Joined)
	t.Mach.InternalLog(am.LogOps, "[pubsub:joined] "+t.Name)
	self := t.host.ID().String()

	t.retried = false

	// receive msgs TODO extract
	go func() {
		for {
			psMsg, err := t.sub.Next(ctx)
			if ctx.Err() != nil {
				return // expired
			}
			if err != nil {
				_ = AddErrListening(e, t.Mach, err, nil)
				continue
			}
			// no self msgs
			fromId := psMsg.GetFrom().String()
			if fromId == self {
				continue
			}

			// fork
			go t.pool.Go(func() error {
				if ctx.Err() != nil {
					return nil // expired
				}

				var base Msg
				// TODO error support
				if err := msgpack.Unmarshal(psMsg.Data, &base); err != nil {
					// generic msg
					t.Mach.EvAdd1(e, ss.MsgReceived, Pass(&A{Msgs: []*ps.Message{psMsg}}))
				} else {
					switch base.Type {

					case MsgTypeInfo:
						var msg MsgInfo
						if err := msgpack.Unmarshal(psMsg.Data, &msg); err == nil {
							// handle schemas
							t.Mach.EvAdd1(e, ss.MsgInfo, Pass(&A{
								MsgInfo: &msg,
								PeerId:  fromId,
							}))
							// handle gossips TODO handle in MsgInfo
							t.Mach.EvAdd1(e, ss.MissPeersByGossip, Pass(&A{
								PeersGossip: msg.PeerGossips,
								PeerId:      fromId,
							}))
							t.Mach.EvAdd1(e, ss.MissUpdatesByGossip, Pass(&A{
								PeersGossip: msg.PeerGossips,
								PeerId:      fromId,
							}))
						}

					case MsgTypeBye:
						var msg MsgBye
						if err := msgpack.Unmarshal(psMsg.Data, &msg); err == nil {
							t.Mach.EvAdd1(e, ss.MsgBye, Pass(&A{
								MsgBye: &msg,
								PeerId: fromId,
							}))
						}

					case MsgTypeUpdates:
						var msg MsgUpdates
						if err := msgpack.Unmarshal(psMsg.Data, &msg); err == nil {
							t.Mach.EvAdd1(e, ss.MsgUpdates, Pass(&A{
								MsgUpdates: &msg,
								PeerId:     fromId,
							}))
						}

					case MsgTypeGossip:
						var msg MsgGossip
						if err := msgpack.Unmarshal(psMsg.Data, &msg); err == nil {
							// TODO handle both in MissPeersState
							t.Mach.EvAdd1(e, ss.MissPeersByGossip, Pass(&A{
								PeersGossip: msg.PeerGossips,
								PeerId:      fromId,
							}))
							t.Mach.EvAdd1(e, ss.MissUpdatesByGossip, Pass(&A{
								PeersGossip: msg.PeerGossips,
								PeerId:      fromId,
							}))
						}

					// requests

					case MsgTypeReqInfo:
						var msg MsgReqInfo
						if err := msgpack.Unmarshal(psMsg.Data, &msg); err == nil {
							t.Mach.EvAdd1(e, ss.MsgReqInfo, Pass(&A{
								MsgReqInfo: &msg,
								PeerId:     fromId,
								HTime:      time.Now(),
							}))
						}

					case MsgTypeReqUpdates:
						var msg MsgReqUpdates
						if err := msgpack.Unmarshal(psMsg.Data, &msg); err == nil {
							t.Mach.EvAdd1(e, ss.MsgReqUpdates, Pass(&A{
								MsgReqUpdates: &msg,
								PeerId:        fromId,
							}))
							// memorize requested peer IDs as recently requested (de-dup)
							// TODO update in MsgReqUpdates
							peerIds := slices.Clone(msg.PeerIds)
							t.Mach.Eval("MsgTypeReqUpdate", func() {
								for _, pid := range peerIds {
									t.reqUpdate[pid] = time.Now()
								}
							}, ctx)
						}

					default:
						// Log an error for unsupported message types
						err := fmt.Errorf("unsupported msg type: %s", base.Type)
						_ = AddErrListening(e, t.Mach, err, nil)
					}
				}

				return nil
			})
		}
	}()

	// peer events TODO extract
	go func() {
		if ctx.Err() != nil {
			return // expired
		}

		// peer subscription
		handler, err := t.T.EventHandler()
		if err != nil {
			_ = AddErrJoining(e, t.Mach, err, nil)
			return
		}
		t.handler = handler

		for {
			pEv, err := t.handler.NextPeerEvent(ctx)
			if ctx.Err() != nil {
				return // expired
			}
			if err != nil {
				_ = AddErrListening(e, t.Mach, err, nil)
				continue
			}

			switch pEv.Type {
			case ps.PeerJoin:
				t.Mach.EvAdd1(e, ss.PeerJoined, Pass(&A{
					PeerId: pEv.Peer.String(),
				}))
			case ps.PeerLeave:
				t.Mach.EvAdd1(e, ss.PeerLeft, Pass(&A{
					PeerId: pEv.Peer.String(),
				}))
			}
		}
	}()

	if len(t.ExposedMachs) > 0 {
		go func() {
			// TODO config HelloDelay
			minDelay := 1
			maxDelay := 30
			delay := minDelay + rand.Intn(maxDelay-minDelay)
			if !amhelp.Wait(ctx, time.Duration(delay*100*int(time.Millisecond))) {
				return // expired
			}

			// send Hello
			t.Mach.EvAdd1(e, ss.SendInfo, Pass(&A{
				PeerIds: []string{t.host.ID().String()},
			}))
		}()
	}
}

func (t *Topic) JoinedEnd(e *am.Event) {
	t.Mach.InternalLog(am.LogOps, "[pubsub:left] "+t.Name)
	if t.handler != nil {
		t.handler.Cancel()
	}
}

// EVENTS

func (t *Topic) PeerLeftEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.PeerId != ""
}

func (t *Topic) PeerLeftState(e *am.Event) {
	// TODO direct all owned local workers to MachLeft
}

func (t *Topic) MsgInfoEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	if a == nil || a.MsgInfo == nil || a.PeerId == "" {
		return false
	}

	for peerId, machs := range a.MsgInfo.PeerInfo {
		// missing always pass
		if _, ok := t.missingPeers[peerId]; ok {
			return true
		}
		if _, ok := t.workers[peerId]; !ok {
			return true
		}

		// check if theres a new one
		for machIdx := range machs {
			if _, ok := t.workers[peerId][machIdx]; !ok {
				t.log("Missing mach %d for peer %s", machIdx, peerId)
				return true
			}
		}
	}

	return false
}

func (t *Topic) MsgInfoState(e *am.Event) {
	// t.Mach.EvRemove1(e, ss.MsgHello, nil)

	ctx := t.Mach.NewStateCtx(ss.Start)
	args := ParseArgs(e.Args)
	added := 0
	self := t.host.ID().String()

	for peerId, machs := range args.MsgInfo.PeerInfo {
		// skip self info
		if peerId == self {
			continue
		}

		// init peer
		if _, ok := t.workers[peerId]; !ok {
			t.workers[peerId] = make(map[int]*rpc.Worker)
			t.info[peerId] = make(map[int]*Info)
		}

		// remove from missing
		if _, ok := t.missingPeers[peerId]; ok {
			delete(t.missingPeers, peerId)
			t.Mach.EvRemove1(e, ss.MissPeersByGossip, nil)
		}

		// create local workers
		for machIdx, info := range machs {
			// check already exists
			if w, ok := t.workers[peerId][machIdx]; ok {
				// update to a newest clock
				if info.MTime.Sum() > w.TimeSum(nil) {
					w.UpdateClock(info.MTime, true)
				}
				// TODO check schema?
				continue
			}

			// compose a unique ID
			tags := []string{"pubsub-worker", "src-id:" + info.Id}
			id := "ps-" + info.Id + "-" + utils.RandId(4)
			worker, err := rpc.NewWorker(ctx, id, nil, info.Schema, info.States,
				t.Mach, tags)
			if err != nil {
				// TODO custom error?
				t.Mach.EvAddErr(e, err, nil)
				continue
			}
			if t.DebugWorkerTelemetry {
				amhelp.MachDebugEnv(worker)
			}

			// check for ahead-of-time MsgUpdates
			// add ctx to go-s
			if mtime, ok := t.pendingMachUpdates[peerId][machIdx]; ok {
				// TODO unsafe?
				go worker.UpdateClock(mtime, true)
				// GC
				delete(t.pendingMachUpdates[peerId], machIdx)
				if len(t.pendingMachUpdates[peerId]) == 0 {
					delete(t.pendingMachUpdates, peerId)
				}
				t.Mach.EvRemove1(e, ss.MissPeersByUpdates, nil)

			} else {
				// TODO unsafe?
				go worker.UpdateClock(info.MTime, true)
			}

			t.workers[peerId][machIdx] = worker
			// save the info for re-broadcast
			t.info[peerId][machIdx] = info
			added++
		}
	}

	// confirm we know the sender
	fromPeerId := args.PeerId
	if _, ok := t.workers[fromPeerId]; !ok {
		t.missingPeers[fromPeerId] = struct{}{}
	}

	if added > 0 {
		t.log("Added %d new workers (total %d)", added, t.workersCount())
		t.log("Known peers == %d (missing == %d)",
			len(t.workers), len(t.missingPeers))

		for pid := range t.missingPeers {
			t.log("Missing example: %s", pid)
			break
		}
	}
}

func (t *Topic) MissPeersByGossipEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	if a == nil || a.PeersGossip == nil {
		return false
	}

	// process peer gossips
	self := t.host.ID().String()
	for peerId, machs := range a.PeersGossip {
		// skip self
		if peerId == self {
			continue
		}
		// already known
		if _, ok := t.missingPeers[peerId]; ok {
			continue
		}

		// pass if unknown or the amount of exposed machs differs
		if _, ok := t.workers[peerId]; !ok || len(machs) != len(t.workers[peerId]) {
			return true
		}
	}

	return false
}

func (t *Topic) MissPeersByGossipState(e *am.Event) {
	args := ParseArgs(e.Args)

	// process peer gossips
	self := t.host.ID().String()
	for peerId := range args.PeersGossip {
		// skip self
		if peerId == self {
			continue
		}
		// already known
		if _, ok := t.missingPeers[peerId]; ok {
			continue
		}

		// TODO support `count` from gossip
		// if p, ok := t.workers[peerId]; !ok || len(p) != count {
		if _, ok := t.workers[peerId]; !ok {
			t.missingPeers[peerId] = struct{}{}
			t.log("New missing %s", peerId)
			t.log("Total: %d; Missing: %d",
				len(t.workers), len(t.missingPeers))
		}
	}
}

func (t *Topic) MissPeersByGossipExit(e *am.Event) bool {
	return len(t.missingPeers) == 0
}

func (t *Topic) MissUpdatesByGossipEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	if a == nil || a.PeersGossip == nil {
		return false
	}

	// process peer gossips
	self := t.host.ID().String()
	for peerId, machs := range a.PeersGossip {
		// skip self
		if peerId == self {
			continue
		}
		// skip missing
		if _, ok := t.workers[peerId]; !ok {
			continue
		}

		for machIdx, mtime := range machs {
			m, ok := t.workers[peerId][machIdx]
			if !ok {
				continue
			}

			// accept when received time is higher
			if mtime > m.TimeSum(nil) {
				return true
			}
		}
	}

	return false
}

func (t *Topic) MissUpdatesByGossipState(e *am.Event) {
	args := ParseArgs(e.Args)

	// process peer gossips
	self := t.host.ID().String()
	for peerId, machs := range args.PeersGossip {
		// skip self
		if peerId == self {
			continue
		}
		// skip missing
		if _, ok := t.workers[peerId]; !ok {
			continue
		}
		// init
		if _, ok := t.missingUpdates[peerId]; !ok {
			t.missingUpdates[peerId] = make(Gossips)
		}

		for machIdx, mtime := range machs {
			m, ok := t.workers[peerId][machIdx]
			if !ok {
				t.missingUpdates[peerId][machIdx] = mtime
				continue
			}

			// accept when received time is higher
			if mtime > m.TimeSum(nil) {
				t.missingUpdates[peerId][machIdx] = mtime
			}
		}

		// prevent empty entries
		if len(t.missingUpdates[peerId]) == 0 {
			delete(t.missingUpdates, peerId)
		}
	}
}

func (t *Topic) MissUpdatesByGossipExit(e *am.Event) bool {
	return len(t.missingUpdates) == 0
}

func (t *Topic) PeerJoinedEnter(e *am.Event) bool {
	return ParseArgs(e.Args).PeerId != ""
}

func (t *Topic) PeerJoinedState(e *am.Event) {
	t.Mach.EvRemove1(e, ss.PeerJoined, nil)
	args := ParseArgs(e.Args)
	peerId := args.PeerId

	// mark as missing
	// TODO add MissPeerByJoin ?
	t.missingPeers[peerId] = struct{}{}
	// say hellp
	t.Mach.EvAdd1(e, ss.SendInfo, Pass(&A{
		PeerIds: []string{t.host.ID().String()},
	}))
}

// MSGS

func (t *Topic) MsgByeEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.MsgBye != nil
}

func (t *Topic) MsgByeState(e *am.Event) {
	// TODO
}

func (t *Topic) MsgReceivedEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.Msgs != nil
}

func (t *Topic) SendMsgEnter(e *am.Event) bool {
	win := time.Now().Second() % 10
	if t.msgsWin != win {
		t.msgsWin = win
		t.msgsWinCount = 0
	}

	t.msgsWinCount++
	if t.msgsWinCount > t.MaxMsgsPerWin {
		t.log("Too many messages in last time window, dropping..")
		return false
	}

	a := ParseArgs(e.Args)
	return a != nil && len(a.Msg) > 0
}

func (t *Topic) SendMsgState(e *am.Event) {
	t.Mach.EvRemove1(e, ss.SendMsg, nil)
	ctx := t.Mach.NewStateCtx(ss.Start)
	args := ParseArgs(e.Args)
	msg := args.Msg

	go t.pool.Go(func() error {
		if ctx.Err() != nil {
			return nil // expired
		}
		err := t.T.Publish(ctx, msg)
		if ctx.Err() != nil {
			return nil // expired
		}
		t.Mach.EvAddErr(e, err, nil)

		return nil
	})
}

func (t *Topic) SendInfoEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && len(a.PeerIds) > 0
}

func (t *Topic) SendInfoState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ss.SendInfo)
	debounce := t.sendHelloDebounce
	args := ParseArgs(e.Args)

	go func() {
		defer t.Mach.EvRemove1(e, ss.SendInfo, nil)
		if !amhelp.Wait(ctx, debounce) {
			return // expired
		}
		t.Mach.EvAdd1(e, ss.DoSendInfo, Pass(&A{
			PeerIds: args.PeerIds,
		}))
	}()
}

func (t *Topic) MsgUpdatesEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.MsgUpdates != nil && a.PeerId != ""
}

func (t *Topic) MsgUpdatesState(e *am.Event) {
	// t.Mach.EvRemove1(e, ss.MachUpdate, nil)
	args := ParseArgs(e.Args)
	self := t.host.ID().String()

	for peerId, machs := range args.MsgUpdates.PeerClocks {
		// skip self info
		if peerId == self {
			continue
		}

		// delay if peer unknown
		workers, ok := t.workers[peerId]
		if !ok {
			t.Mach.EvAdd1(e, ss.MissPeersByUpdates, Pass(&A{
				MachClocks: machs,
				PeerId:     peerId,
			}))

			// process on ss.MsgInfo
			continue
		}

		missing := t.missingUpdates[peerId] //lint:ignore gosimple

		// for all updated machines
		for machIdx, mtime := range machs {

			w, ok := workers[machIdx]
			if !ok {
				// TODO missing schema per peer
				t.log("worker %s:%d not found, delaying..", peerId, machIdx)
				continue
			}

			// skip old updates
			if mtime.Sum() > w.TimeSum(nil) {
				w.UpdateClock(mtime, true)
			}

			// clean up
			if missing == nil {
				continue
			}

			// apply a newer update
			if missingMTime, ok := missing[machIdx]; ok &&
				missingMTime <= mtime.Sum() {

				delete(missing, machIdx)
				if len(missing) == 0 {
					delete(t.missingUpdates, peerId)
				}
			}
		}
	}

	// confirm we know the sender
	fromPeerId := args.PeerId
	if _, ok := t.workers[fromPeerId]; !ok {
		t.missingPeers[fromPeerId] = struct{}{}
	}
}

func (t *Topic) MsgReqInfoEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	if a == nil || a.MsgReqInfo == nil || a.PeerId == "" {
		return false
	}

	// check if we know any of the requested peers
	wCount := len(t.workers)
	self := t.host.ID().String()
	for _, peerId := range a.MsgReqInfo.PeerIds {
		if _, ok := t.workers[peerId]; ok || peerId == self {
			// only 1 known peer answers (randomly)
			// TODO increase the probability with repeated requests
			return wCount == 0 || rand.Intn(wCount) != 0
		}
	}

	return false
}

func (t *Topic) MsgReqInfoState(e *am.Event) {
	// t.Mach.EvRemove1(e, ss.MsgReqInfo, nil)
	args := ParseArgs(e.Args)

	// collect peers we know (incl this peer)
	self := t.host.ID().String()
	pids := []string{}
	for _, peerId := range args.MsgReqInfo.PeerIds {
		// TODO skip info for peers we just sent out / received
		if _, ok := t.workers[peerId]; ok || peerId == self {
			pids = append(pids, peerId)
		}

		// memorize requested peer IDs as recently requested (de-dup)
		t.reqInfo[peerId] = time.Now()
	}

	// confirm we know the sender
	fromPeerId := args.PeerId
	if _, ok := t.workers[fromPeerId]; !ok {
		t.missingPeers[fromPeerId] = struct{}{}
	}

	t.Mach.Add1(ss.SendInfo, Pass(&A{
		PeerIds: pids,
	}))
}

func (t *Topic) MsgReqUpdatesEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	if a == nil || a.MsgReqInfo == nil || a.PeerId == "" {
		return false
	}

	// check if we know any the requested peers
	self := t.host.ID().String()
	for _, peerId := range a.MsgReqInfo.PeerIds {
		if _, ok := t.workers[peerId]; ok || peerId == self {
			// only 1 known peer answers (randomly)
			// TODO increase the probability with repeated requests
			return rand.Intn(len(t.workers)) != 0
		}
	}

	return false
}

func (t *Topic) MsgReqUpdatesState(e *am.Event) {
	// TODO per-mach index requests (partial)
	// t.Mach.EvRemove1(e, ss.MsgReqInfo, nil)
	args := ParseArgs(e.Args)
	ctx := t.Mach.NewStateCtx(ss.Start)

	// collect updates from peers we know
	self := t.host.ID().String()
	update := &MsgUpdates{
		Msg:        Msg{MsgTypeUpdates},
		PeerClocks: make(map[string]MachClocks),
	}

	for _, peerId := range args.MsgReqUpdates.PeerIds {
		// this peer
		if peerId == self {
			update.PeerClocks[peerId] = make(MachClocks)
			for i, mach := range t.ExposedMachs {
				update.PeerClocks[peerId][i] = mach.Time(nil)
			}

		}

		// remote peer
		if machs, ok := t.workers[peerId]; ok {
			update.PeerClocks[peerId] = make(MachClocks)
			for i, mach := range machs {
				update.PeerClocks[peerId][i] = mach.Time(nil)
			}
		}
	}

	// comfirm we know the sender
	fromPeerId := args.PeerId
	if _, ok := t.workers[fromPeerId]; !ok {
		t.missingPeers[fromPeerId] = struct{}{}
	}

	// send updates
	if len(update.PeerClocks) <= 0 {
		return
	}
	go t.pool.Go(func() error {
		if ctx.Err() != nil {
			return nil // expired
		}

		encoded, err := msgpack.Marshal(update)
		if err != nil {
			t.Mach.EvAddErr(e, err, nil)
			return nil
		}
		t.Mach.EvAdd1(e, ss.SendMsg, Pass(&A{
			Msg:     encoded,
			MsgType: string(MsgTypeUpdates),
		}))

		return nil
	})
}

func (t *Topic) MissPeersByUpdatesEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.MachClocks != nil && a.PeerId != ""
}

func (t *Topic) MissPeersByUpdatesState(e *am.Event) {
	// t.Mach.EvRemove1(e, ss.MissPeersByUpdates, nil)
	args := ParseArgs(e.Args)
	pending := args.MachClocks
	peerId := args.PeerId

	_, ok := t.pendingMachUpdates[peerId]
	if !ok {
		t.pendingMachUpdates[peerId] = pending

		// process on ss.MsgInfo
		return
	}

	// some already delayed, merge (keep higher clocks)
	for machIdx, mtimeNew := range pending {
		if mtimeOld, ok := t.pendingMachUpdates[peerId][machIdx]; ok {
			// merge
			if mtimeNew.Sum() > mtimeOld.Sum() {
				t.pendingMachUpdates[peerId][machIdx] = mtimeNew
			}
		} else {
			// add
			t.pendingMachUpdates[peerId][machIdx] = mtimeNew
		}
	}
}

func (t *Topic) MissPeersByUpdatesExit(e *am.Event) bool {
	return len(t.pendingMachUpdates) == 0
}

// ACTIONS

func (t *Topic) ListMachinesEnter(e *am.Event) bool {
	a := ParseArgs(e.Args)
	return a != nil && a.WorkersCh != nil &&
		// check buffered channel
		cap(a.WorkersCh) > 0
}

func (t *Topic) ListMachinesState(e *am.Event) {
	t.Mach.EvRemove1(e, ss.ListMachines, nil)

	args := ParseArgs(e.Args)
	filters := args.ListFilters
	if filters == nil {
		filters = &ListFilters{}
	}
	retCh := args.WorkersCh
	ret := make([]*rpc.Worker, 0)

	// TODO use amhelp.Group
	for peerId, workers := range t.workers {
		for _, w := range workers {

			// ID
			if filters.IdExact != "" && w.Id() != filters.IdExact {
				continue
			}
			// ID regexp
			if filters.IdRegexp != nil && !filters.IdRegexp.MatchString(w.Id()) {
				continue
			}
			// ID substring
			if filters.IdPartial != "" &&
				!strings.Contains(w.Id(), filters.IdPartial) {

				continue
			}

			// peer ID
			if filters.PeerId != "" && peerId != filters.PeerId {
				continue
			}

			// parent ID
			if filters.Parent != "" && w.ParentId() != filters.Parent {
				continue
			}

			// TODO filers.DepthLevel
			// TODO filers.ChildrenMax
			// TODO filers.ChildrenMin

			ret = append(ret, w)
		}
	}

	select {
	case retCh <- ret:
	default:
		t.log("ListMachines: closed chan, dropping")
	}
}

func (t *Topic) HeartbeatState(e *am.Event) {
	mach := t.Mach
	mach.EvRemove1(e, ss.Heartbeat, nil)

	// make sure these are in sync
	mach.EvRemove1(e, ss.MissPeersByGossip, nil)
	mach.EvRemove1(e, ss.MissPeersByUpdates, nil)

	// delegate work
	mach.EvAdd1(e, ss.SendGossips, nil)
	mach.EvAdd1(e, ss.ReqMissingUpdates, nil)
	mach.EvAdd1(e, ss.ReqMissingPeers, nil)
	mach.EvAdd1(e, ss.SendUpdates, nil)
}

func (t *Topic) SendUpdatesState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ss.SendUpdates)

	// collect updates
	clocks := make(MachClocks)
	for i, mach := range t.ExposedMachs {
		if !t.tracers[i].dirty.Load() {
			continue
		}
		mtime := mach.Time(nil)
		t.tracers[i].dirty.Store(false)
		clocks[i] = mtime
	}

	// no updates
	if len(clocks) <= 0 {
		t.Mach.EvRemove1(e, ss.SendUpdates, nil)
		return
	}

	// send updates
	self := t.host.ID().String()
	update := &MsgUpdates{
		Msg:        Msg{MsgTypeUpdates},
		PeerClocks: make(map[string]MachClocks),
	}
	update.PeerClocks[self] = clocks
	// send updates
	go t.pool.Go(func() error {
		defer t.Mach.EvRemove1(e, ss.SendUpdates, nil)
		if ctx.Err() != nil {
			return nil // expired
		}

		encoded, err := msgpack.Marshal(update)
		if err != nil {
			t.Mach.EvAddErr(e, err, nil)
			return nil
		}
		t.Mach.EvAdd1(e, ss.SendMsg, Pass(&A{
			Msg:     encoded,
			MsgType: string(MsgTypeUpdates),
		}))

		return nil
	})
}

func (t *Topic) SendGossipsEnter(e *am.Event) bool {
	if len(t.workers) == 0 {
		return false
	}
	// randomize gossip to 1 peer per 1 Heartbeat
	if rand.Intn(len(t.workers)) != 0 {
		return false
	}

	return true
}

func (t *Topic) SendGossipsState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ss.SendGossips)

	allPids := slices.Collect(maps.Keys(t.workers))
	sendPids := PeerGossips{}
	for range 5 {
		pid := allPids[rand.Intn(len(allPids))]
		// skip empty peers
		if len(t.workers[pid]) == 0 {
			continue
		}
		sums := map[int]uint64{}
		for i, w := range t.workers[pid] {
			sums[i] = w.Time(nil).Sum()
		}
		sendPids[pid] = sums
	}

	// unblock
	go t.pool.Go(func() error {
		defer t.Mach.EvRemove1(e, ss.SendGossips, nil)
		if ctx.Err() != nil {
			return nil // expired
		}

		randPeers := &MsgGossip{
			Msg:         Msg{MsgTypeGossip},
			PeerGossips: sendPids,
		}
		encoded, err := msgpack.Marshal(randPeers)
		if err != nil {
			t.Mach.EvAddErr(e, err, nil)
			return nil
		}
		t.Mach.EvAdd1(e, ss.SendMsg, Pass(&A{
			Msg:     encoded,
			MsgType: string(MsgTypeGossip),
		}))

		return nil
	})
}

// how often to look for missing peers
// TODO config
var reqMissPeersFreq = time.Second * 5

func (t *Topic) ReqMissingPeersEnter(e *am.Event) bool {
	// nothing is missing
	if len(t.missingPeers) == 0 && len(t.pendingMachUpdates) == 0 {
		return false
	}

	// too early
	if time.Since(t.lastReqHello) <= reqMissPeersFreq {
		return false
	}

	return true
}

func (t *Topic) ReqMissingPeersState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ss.ReqMissingPeers)

	// TODO config
	amount := 5
	maxTries := 15
	// how often to request MsgHello per 1 peer
	reqHelloFreq := time.Second * 5

	// req missing peers
	// TODO fair request dist per peer IDs
	reqPids := []string{}
	pids := slices.Concat(
		slices.Collect(maps.Keys(t.missingPeers)),
		slices.Collect(maps.Keys(t.pendingMachUpdates)))
	for i := 0; i < maxTries && len(reqPids) < amount; i++ {
		// random
		pid := pids[rand.Intn(len(pids))]
		// dup, skip
		if slices.Contains(reqPids, pid) {
			continue
		}
		// too early, skip
		if t, ok := t.reqInfo[pid]; ok && time.Since(t) < reqHelloFreq {
			continue
		}
		reqPids = append(reqPids, pid)
		t.reqInfo[pid] = time.Now()
	}

	if len(reqPids) <= 0 {
		return
	}

	// unblock
	go t.pool.Go(func() error {
		defer t.Mach.EvRemove1(e, ss.ReqMissingPeers, nil)
		if ctx.Err() != nil {
			return nil // expired
		}

		reqHello := &MsgReqInfo{
			Msg:     Msg{MsgTypeReqInfo},
			PeerIds: reqPids,
		}
		encoded, err := msgpack.Marshal(reqHello)
		if err != nil {
			t.Mach.EvAddErr(e, err, nil)
			return nil
		}
		// TODO update in SendMsgState
		t.Mach.Eval("reqMissingPeers", func() {
			t.lastReqHello = time.Now()
		}, ctx)
		t.Mach.EvAdd1(e, ss.SendMsg, Pass(&A{
			Msg: encoded,
			// debug
			MsgType: string(reqHello.Type),
			PeerId:  reqPids[0],
		}))

		return nil
	})
}

// how often to look for missing updates
// TODO config
var reqMissUpdatesFreq = time.Second * 5

func (t *Topic) ReqMissingUpdatesEnter(e *am.Event) bool {
	// nothing is missing
	if len(t.missingUpdates) == 0 {
		return false
	}
	// too early
	if time.Since(t.lastReqUpdate) <= reqMissUpdatesFreq {
		return false
	}

	return true
}

func (t *Topic) ReqMissingUpdatesState(e *am.Event) {
	ctx := t.Mach.NewStateCtx(ss.ReqMissingUpdates)

	// TODO config
	amount := 5
	maxTries := 15
	// how often to request MsgUpdates per 1 peer
	reqUpdateFreq := time.Second * 5

	// req missing peers
	// TODO fair request dist per peer IDs
	reqPids := []string{}
	pids := slices.Collect(maps.Keys(t.missingUpdates))
	for i := 0; i < maxTries && len(reqPids) < amount; i++ {
		// random
		pid := pids[rand.Intn(len(pids))]
		// dup, skip
		if slices.Contains(reqPids, pid) {
			continue
		}
		// too early, skip
		if t, ok := t.reqUpdate[pid]; ok && time.Since(t) < reqUpdateFreq {
			continue
		}
		reqPids = append(reqPids, pid)
		t.reqUpdate[pid] = time.Now()
	}

	if len(reqPids) <= 0 {
		return
	}

	// unblock
	go t.pool.Go(func() error {
		defer t.Mach.EvRemove1(e, ss.ReqMissingUpdates, nil)
		if ctx.Err() != nil {
			return nil // expired
		}

		reqUpdate := &MsgReqUpdates{
			Msg:     Msg{MsgTypeReqUpdates},
			PeerIds: reqPids,
		}
		encoded, err := msgpack.Marshal(reqUpdate)
		if err != nil {
			t.Mach.EvAddErr(e, err, nil)
			return nil
		}
		// TODO update in SendMsgState
		t.Mach.Eval("reqMissingUpdates", func() {
			t.lastReqUpdate = time.Now()
		}, ctx)
		t.Mach.EvAdd1(e, ss.SendMsg, Pass(&A{
			Msg: encoded,
			// debug
			MsgType: string(reqUpdate.Type),
			PeerId:  reqPids[0],
		}))

		return nil
	})
}

// ///// ///// /////

// ///// METHODS

// ///// ///// /////

func (t *Topic) Start() am.Result {
	return t.Mach.Add1(ss.Start, nil)
}

func (t *Topic) Join() am.Result {
	return t.Mach.Add1(ss.Joining, nil)
}

func (t *Topic) StartAndJoin(ctx context.Context) am.Result {
	if t.Mach.Add1(ss.Start, nil) == am.Canceled {
		return am.Canceled
	}
	err := amhelp.WaitForAll(ctx, time.Second, t.Mach.When1(ss.Connected, ctx))
	if ctx.Err() != nil {
		return am.Canceled
	}
	if err != nil {
		return am.Canceled
	}
	return t.Join()
}

func (t *Topic) Dispose() am.Result {
	return t.Mach.Add1(ss.Disposing, nil)
}

func (t *Topic) GetPeerAddrs() ([]ma.Multiaddr, error) {
	if t.Mach.Not1(ss.Start) {
		return nil, fmt.Errorf("%w: %s", am.ErrStateInactive, ss.Start)
	}
	h := t.host

	// Get the peer ID
	peerID := h.ID()

	// Get all listen addresses
	addrs := h.Addrs()
	if len(addrs) == 0 {
		return nil, errors.New("no listen addresses available")
	}

	// Create a slice to hold the encapsulated multiaddresses
	peerAddrs := make([]ma.Multiaddr, len(addrs))
	for i, addr := range addrs {
		// Create a new multiaddress by combining the network address and peer ID
		peerAddrs[i] = addr.Encapsulate(ma.StringCast("/p2p/" + peerID.String()))
	}

	return peerAddrs, nil
}

func (t *Topic) DoSendInfoEnter(e *am.Event) bool {
	return len(ParseArgs(e.Args).PeerIds) > 0
}

func (t *Topic) DoSendInfoState(e *am.Event) {
	t.Mach.EvRemove1(e, ss.DoSendInfo, nil)
	args := ParseArgs(e.Args)
	peerIds := args.PeerIds

	// collect gossips and info
	gossips := PeerGossips{}
	self := t.host.ID().String()
	exposed := slices.Clone(t.ExposedMachs)

	// gossip about 5 random peers
	for range min(5, len(t.workers)) {
		ids := slices.Collect(maps.Keys(t.workers))
		pid := ids[rand.Intn(len(t.workers))]
		// skip empty peers
		if len(t.workers[pid]) == 0 {
			continue
		}
		sums := map[int]uint64{}
		for i, w := range t.workers[pid] {
			sums[i] = w.Time(nil).Sum()
		}
		gossips[pid] = sums
	}

	// try again if didnt go through
	// if !ok {
	// 	t.Mach.Remove1(ss.SendHello, nil)
	// 	t.Mach.Add1(ss.SendHello, nil)
	// 	// TODO detect too many retires (compare PeerJoined ticks)
	// 	return
	// }

	// list all requested machs
	msg := &MsgInfo{
		Msg:         Msg{MsgTypeInfo},
		PeerInfo:    PeerInfo{},
		PeerGossips: gossips,
	}
	for _, peerId := range peerIds {
		machs := MachInfo{}

		// say hello
		if peerId == self {
			for machIdx, mach := range exposed {
				machs[machIdx] = &Info{
					Id: mach.Id(),
					// TODO pre-shared schemas
					Schema: mach.Schema(),
					States: mach.StateNames(),
					MTime:  mach.Time(nil),
					Tags:   mach.Tags(),
					Parent: mach.ParentId(),
				}
			}
		} else {

			// fwd what we know
			if _, ok := t.info[peerId]; !ok {
				continue
			}
			// TODO send only requested ones
			machs = t.info[peerId]
		}

		msg.PeerInfo[peerId] = machs
	}

	// send
	encoded, err := msgpack.Marshal(msg)
	if err != nil {
		t.Mach.EvAddErr(e, err, nil)
		return
	}
	t.Mach.EvAdd1(e, ss.SendMsg, Pass(&A{
		Msg:     encoded,
		MsgType: string(MsgTypeInfo),
	}))
}

func (t *Topic) workersCount() int {
	ret := 0
	for _, workers := range t.workers {
		ret += len(workers)
	}
	return ret
}

func (t *Topic) log(msg string, args ...any) {
	if !t.LogEnabled {
		return
	}
	t.Mach.Log(msg, args...)
}
