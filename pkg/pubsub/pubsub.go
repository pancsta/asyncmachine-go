package pubsub

import (
	"errors"
	"fmt"
	"regexp"
	"sync/atomic"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/multiformats/go-multiaddr"

	amhelp "github.com/pancsta/asyncmachine-go/pkg/helpers"
	am "github.com/pancsta/asyncmachine-go/pkg/machine"
	"github.com/pancsta/asyncmachine-go/pkg/pubsub/states"
	"github.com/pancsta/asyncmachine-go/pkg/rpc"
)

const (
	// EnvAmPubsubLog enables machine logging for pubsub.
	EnvAmPubsubLog = "AM_PUBSUB_LOG"
	// EnvAmPubsubDbg exposes PubSub workers over dbg telemetry.
	EnvAmPubsubDbg = "AM_PUBSUB_DBG"
)

var ss = states.TopicStates

// ///// ///// /////

// ///// ARGS

// ///// ///// /////
// TODO keep with states

// PARTIALS

type MsgType string

const (
	MsgTypeUpdates    MsgType = "updates"
	MsgTypeBye        MsgType = "bye"
	MsgTypeInfo       MsgType = "info"
	MsgTypeReqInfo    MsgType = "reqinfo"
	MsgTypeReqUpdates MsgType = "requpdates"
	MsgTypeGossip     MsgType = "gossip"
)

type Msg struct {
	Type MsgType
}

type (
	Gossips map[int]uint64
	// PeerGossips is peer ID => machine time sum
	// TODO check local time sum
	PeerGossips map[string]Gossips
)

// Info is sent when a peer exposes state machines in the topic.
type Info struct {
	Id     string
	Schema am.Schema
	States am.S
	MTime  am.Time
	Tags   []string
	Parent string
}

type (
	MachInfo map[int]*Info
	PeerInfo map[string]MachInfo
)

// Machine clocks indexed by a local peer index.
type (
	MachClocks map[int]am.Time
	PeerClocks map[string]MachClocks
)

// WRAPPERS

// MsgByeMach is sent when a peer un-exposes state machines in the topic.
type MsgByeMach struct {
	PeerBye bool
	Id      string
	MTime   am.Time
}

type MsgInfo struct {
	Msg
	// peer ID => mach idx => schema
	PeerInfo PeerInfo
	// Number of workers from each peer
	PeerGossips PeerGossips
}

type MsgUpdates struct {
	Msg
	PeerClocks PeerClocks
	// TODO removed machs
}

type MsgGossip struct {
	Msg
	// Number of workers from each peer
	PeerGossips PeerGossips
}

type MsgReqUpdates struct {
	Msg
	// TODO list specific mach indexes
	PeerIds []string
}

type MsgReqInfo struct {
	Msg
	PeerIds []string
}

// TODO implement
type MsgBye struct {
	Msg
	Machs map[int]MsgByeMach
}

// ///// ///// /////

// ///// TRACER

// ///// ///// /////

type Tracer struct {
	*am.NoOpTracer
	// This machine's clock has been updated and needs to be synced.
	dirty atomic.Bool
}

// TransitionEnd sends a message when a transition ends
func (t *Tracer) TransitionEnd(transition *am.Transition) {
	t.dirty.Store(true)
}

type TopicOpts struct {
	// Parent is a parent state machine for a new Topic state machine. See
	// [am.Opts].
	Parent am.Api
}

// ///// ///// /////

// ///// ARGS

// ///// ///// /////

const APrefix = "am_pubsub"

// A is a struct for node arguments. It's a typesafe alternative to [am.A].
type A struct {
	// MsgInfo happens when a peer introduces it's exposed state machines.
	MsgInfo       *MsgInfo
	MsgUpdates    *MsgUpdates
	MsgBye        *MsgBye
	MsgReqInfo    *MsgReqInfo
	MsgReqUpdates *MsgReqUpdates
	MsgGossip     *MsgGossip
	// Msgs is a list of PubSub messages.
	Msgs []*pubsub.Message
	// Length is a general length used for logging
	Length int `log:"length"`
	// Msg is a raw msg.
	Msg []byte
	// PeerId is a peer ID
	Peer    string `log:"peer"`
	PeerId  string
	PeerIds []string
	// MachId is a state machine ID
	MachId string `log:"mach_id"`
	// MTime is machine time
	MTime am.Time `log:"mtime"`
	// HTime is human time
	HTime time.Time
	// Addrs is a list of addresses.
	Addrs []multiaddr.Multiaddr `log:"addrs"`
	// WorkersCh is a return channel for a list of [rpc.Worker]. It has to be
	// buffered or the mutation will fail.
	WorkersCh   chan<- []*rpc.Worker
	ListFilters *ListFilters
	MachClocks  MachClocks
	MsgType     string `log:"msg_type"`
	PeersGossip PeerGossips
}

// TODO merge with REPL, add tag-based queries (to find workers)
type ListFilters struct {
	IdExact   string
	IdPartial string
	IdRegexp  *regexp.Regexp
	Parent    string
	PeerId    string
	// Level to traverse towards the tree root.
	DepthLevel  int
	ChildrenMax int
	ChildrenMin int
}

// ParseArgs extracts A from [am.Event.Args][APrefix].
func ParseArgs(args am.A) *A {
	if a, _ := args[APrefix].(*A); a != nil {
		return a
	}
	return &A{}
}

// Pass prepares [am.A] from A to pass to further mutations.
func Pass(args *A) am.A {
	return am.A{APrefix: args}
}

// LogArgs is an args logger for A and pubsub.A.
func LogArgs(args am.A) map[string]string {
	a := ParseArgs(args)
	if a == nil {
		return nil
	}

	return amhelp.ArgsToLogMap(a, 0)
}

// ///// ///// /////

// ///// ERRORS

// ///// ///// /////

// sentinel errors

var (
	ErrJoining   = errors.New("error joining")
	ErrListening = errors.New("error listening")
)

// error mutations

// AddErrJoining wraps an error in the ErrJoining sentinel and adds to a
// machine.
func AddErrJoining(
	event *am.Event, mach *am.Machine, err error, args am.A,
) error {
	err = fmt.Errorf("%w: %w", ErrJoining, err)
	mach.EvAddErrState(event, ss.ErrJoining, err, args)

	return err
}

// AddErrListening wraps an error in the ErrListening sentinel and adds to a
// machine.
func AddErrListening(
	event *am.Event, mach *am.Machine, err error, args am.A,
) error {
	err = fmt.Errorf("%w: %w", ErrListening, err)
	mach.EvAddErrState(event, ss.ErrListening, err, args)

	return err
}

// ///// ///// /////

// ///// DISCOVERY

// ///// ///// /////

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
// type discoveryNotifee struct {
// 	h host.Host
// }
//
// // TODO enable
// func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
// 	fmt.Printf("discovered new peer %s", pi.ID.String())
// 	err := n.h.Connect(context.Background(), pi)
// 	if err != nil {
// 		fmt.Printf("error connecting to peer %s: %s", pi.ID.String(), err)
// 	}
// }
