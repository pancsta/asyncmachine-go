package states

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

// Tags and prefixes shared between the tinygo and non-tinygo builds.
const (
	TagRpcHandler = "rpc-handler"
	TagRpcClient  = "rpc-client"
	TagRpcServer  = "rpc-server"
	TagRpcMux     = "rpc-mux"
	TagRpcNetmach = "rpc-mux"

	TagKeySrcId = "src-id"
	TagKeyAddr  = "addr"

	PrefixNetMach = "rnm-"
)

// ConsumerHandlers is the required interface for Consumer's state handlers.
type ConsumerHandlers interface {
	ServerPayloadState(e *am.Event)
}
