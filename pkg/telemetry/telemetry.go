package telemetry

import (
	"encoding/gob"
	"log"
	"net/rpc"

	"github.com/samber/lo"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

const RpcHost = "localhost:9823"

type Msg interface {
	// Clock returns the state's clock, using the passed index
	Clock(statesIndex am.S, state string) uint64
	// Is true if the state is active, using the passed index
	Is(statesIndex am.S, states am.S) bool
}

// MsgStruct contains the state and relations data.
type MsgStruct struct {
	// Machine ID
	ID string
	// state names defining the indexes for diffs
	StatesIndex am.S
	// all the states with relations
	States am.States
	// log level of the machine
	LogLevel am.LogLevel
}

func (d *MsgStruct) Clock(_ am.S, _ string) uint64 {
	return 0
}

func (d *MsgStruct) Is(_ am.S, _ am.S) bool {
	return false
}

// MsgTx contains transaction data.
type MsgTx struct {
	// Transition ID
	ID string
	// map of positions from the index to the active state
	StatesActive []bool
	// map of positions from the index to state clocks
	Clocks []uint64
	// result of the transition
	Accepted bool
	// all the transition steps
	Steps []*am.TransitionStep
	// log entries since the last diff
	LogEntries []string
	// log entries before the transition, which happened after the prev one
	PreLogEntries []string
	// tx was triggered by an auto state
	IsAuto bool
}

func (d *MsgTx) Clock(statesIndex am.S, state string) uint64 {
	idx := lo.IndexOf(statesIndex, state)
	return d.Clocks[idx]
}

func (d *MsgTx) Is(statesIndex am.S, states am.S) bool {
	for _, state := range states {
		idx := lo.IndexOf(statesIndex, state) //nolint:typecheck
		if !d.StatesActive[idx] {
			return false
		}
	}
	return true
}

type rpcClient struct {
	client *rpc.Client
}

func newClient(url string) (*rpcClient, error) {
	log.Printf("Connecting to %s", url)
	client, err := rpc.Dial("tcp", url)
	if err != nil {
		return nil, err
	}
	return &rpcClient{client: client}, nil
}

func (c *rpcClient) sendMsgTx(msg *MsgTx) error {
	var reply string
	// TODO use Go() to not block
	err := c.client.Call("RPCServer.MsgTx", msg, &reply)
	if err != nil {
		return err
	}
	return nil
}

func (c *rpcClient) sendMsgStruct(msg *MsgStruct) error {
	var reply string
	// TODO use Go() to not block
	err := c.client.Call("RPCServer.MsgStruct", msg, &reply)
	if err != nil {
		return err
	}
	return nil
}

func MonitorTransitions(m *am.Machine, url string) error {
	var err error
	gob.Register(am.Relation(0))
	client, err := newClient(url)
	if err != nil {
		return err
	}
	msg := &MsgStruct{
		ID:          m.ID,
		StatesIndex: m.StateNames,
		States:      m.States,
		LogLevel:    m.LogLevel,
	}
	// TODO retries
	err = client.sendMsgStruct(msg)
	if err != nil {
		log.Println(err)
	}
	go func() {
		// bind to transitions
		txEndCh := m.On([]string{"transition-end"}, nil)
		// send incoming transitions
		for event := range txEndCh {
			tx := event.Args["transition"].(*am.Transition)
			preLogs := event.Args["pre_logs"].([]string)
			msg := &MsgTx{
				ID:           tx.ID,
				StatesActive: make([]bool, len(m.StateNames)),
				Clocks:       make([]uint64, len(m.StateNames)),
				Accepted:     tx.Accepted,
				Steps: lo.Map(tx.Steps,
					func(step *am.TransitionStep, _ int) *am.TransitionStep {
						return step
					}),
				LogEntries:    tx.LogEntries,
				PreLogEntries: preLogs,
				IsAuto:        tx.IsAuto(),
			}
			for i, state := range m.StateNames {
				msg.StatesActive[i] = m.Is(am.S{state})
				msg.Clocks[i] = m.Clock(state)
			}
			// TODO retries
			err := client.sendMsgTx(msg)
			if err != nil {
				log.Println(err)
				return
			}
		}
	}()
	return nil
}