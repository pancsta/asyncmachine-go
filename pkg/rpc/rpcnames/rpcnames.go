package rpcnames

type Name int

// TODO separate and reserve IDs forever

const (
	// Server

	Add   Name = iota + 1
	AddNS Name = iota + 1
	Remove
	Set
	Handshake
	HandshakeAck
	Log
	Sync
	Get
	Bye

	// Client

	ClientSetClock
	ClientSendPayload
)

func (n Name) Encode() string {
	return string(rune(n))
}

func (n Name) String() string {
	switch n {
	case Add:
		return "Add"
	case AddNS:
		return "AddNS"
	case Remove:
		return "Remove"
	case Set:
		return "Set"
	case Handshake:
		return "Handshake"
	case HandshakeAck:
		return "HandshakeAck"
	case Log:
		return "Log"
	case Sync:
		return "Sync"
	case Get:
		return "Get"
	case ClientSetClock:
		return "ClientSetClock"
	case ClientSendPayload:
		return "ClientSendPayload"
	case Bye:
		return "Close"
	}

	return "!UNKNOWN!"
}

func Decode(s string) Name {
	return Name(s[0])
}
