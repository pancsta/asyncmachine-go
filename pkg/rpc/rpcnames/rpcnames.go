package rpcnames

// TODO use enum pkg github.com/xybor-x/enum
// TODO separate ClientNames and ServerNames
// TODO keep in /pkg/rpc?

type Name int

const (
	// Server

	Add Name = iota + 1
	AddNS
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
	ClientBye
	ClientSchemaChange
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
	case Bye:
		return "Close"
	case ClientSetClock:
		return "ClientSetClock"
	case ClientPushAllTicks:
		return "ClientPushAllTicks"
	case ClientSendPayload:
		return "ClientSendPayload"
	case ClientBye:
		return "ClientBye"
	case ClientSchemaChange:
		return "SchemaChange"
	}

	return "!UNKNOWN!"
}

func Decode(s string) Name {
	return Name(s[0])
}
