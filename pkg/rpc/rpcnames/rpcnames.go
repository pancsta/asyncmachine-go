package rpcnames

type Name int

// TODO separate and reserve IDs forever

const (
	// Server

	Add   Name = iota + 1
	AddNS Name = iota + 1
	Remove
	Set
	Hello
	Handshake
	Log
	Sync
	Bye

	// Client

	ClientSetClock
	ClientPushAllTicks
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
	case Hello:
		return "Hello"
	case Handshake:
		return "Handshake"
	case Log:
		return "Log"
	case Sync:
		return "Sync"
	case ClientSetClock:
		return "ClientSetClock"
	case ClientPushAllTicks:
		return "ClientPushAllTicks"
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
