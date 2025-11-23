package types

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	am "github.com/pancsta/asyncmachine-go/pkg/machine"
)

type MachAddress struct {
	MachId    string
	TxId      string
	MachTime  uint64
	HumanTime time.Time
	// TODO support step
	Step int
	// TODO support queue ticks
	QueueTick uint64
}

type MachTime struct {
	Id   string
	Time uint64
}

func (a *MachAddress) Clone() *MachAddress {
	return &MachAddress{
		MachId:   a.MachId,
		TxId:     a.TxId,
		Step:     a.Step,
		MachTime: a.MachTime,
	}
}

func (a *MachAddress) String() string {
	if a.TxId != "" {
		u := fmt.Sprintf("mach://%s/%s", a.MachId, a.TxId)
		if a.Step != 0 {
			u += fmt.Sprintf("/%d", a.Step)
		}
		if a.MachTime != 0 {
			u += fmt.Sprintf("/t%d", a.MachTime)
		}

		return u
	}
	if a.MachTime != 0 {
		return fmt.Sprintf("mach://%s/t%d", a.MachId, a.MachTime)
	}
	if !a.HumanTime.IsZero() {
		return fmt.Sprintf("mach://%s/%s", a.MachId, a.HumanTime)
	}

	return fmt.Sprintf("mach://%s", a.MachId)
}

func ParseMachUrl(u string) (*MachAddress, error) {
	// TODO merge parsing with addr bar

	up, err := url.Parse(u)
	if err != nil {
		return nil, err
	} else if up.Host == "" {
		return nil, fmt.Errorf("host missing in: %s", u)
	}

	addr := &MachAddress{
		MachId: up.Host,
	}
	p := strings.Split(up.Path, "/")
	if len(p) > 1 {
		addr.TxId = p[1]
	}
	if len(p) > 2 {
		if s, err := strconv.Atoi(p[2]); err == nil {
			addr.Step = s
		}
	}

	return addr, nil
}

type MsgTxParsed struct {
	StatesAdded   []int
	StatesRemoved []int
	StatesTouched []int
	// TimeSum is machine time after this transition.
	TimeSum       uint64
	TimeDiff      uint64
	ReaderEntries []*LogReaderEntryPtr
	// Transitions which reported this one as their source
	Forks       []MachAddress
	ForksLabels []string
	// QueueTick when this tx should be executed
	ResultTick uint64
}

type MsgSchemaParsed struct {
	Groups      map[string]am.S
	GroupsOrder []string
}

type LogReaderEntry struct {
	Kind LogReaderKind
	// states are empty for logReaderWhenQueue
	States []int
	// CreatedAt is machine time when this entry was created
	CreatedAt uint64
	// ClosedAt is human time when this entry was closed, so it can be disposed.
	ClosedAt time.Time

	// per-type fields

	// Pipe is for logReaderPipeIn, logReaderPipeOut
	Pipe am.MutationType
	// Mach is for logReaderPipeIn, logReaderPipeOut, logReaderMention
	Mach string
	// Ticks is for logReaderWhenTime only
	Ticks am.Time
	// Args is for logReaderWhenArgs only
	Args string
	// QueueTick is for logReaderWhenQueue only
	QueueTick int
}

type LogReaderEntryPtr struct {
	TxId     string
	EntryIdx int
}

type LogReaderKind int

const (
	LogReaderCtx LogReaderKind = iota + 1
	LogReaderWhen
	LogReaderWhenNot
	LogReaderWhenTime
	LogReaderWhenArgs
	LogReaderWhenQueue
	LogReaderPipeIn
	LogReaderPipeOut
	// TODO mentions of machine IDs
	// logReaderMention
)
