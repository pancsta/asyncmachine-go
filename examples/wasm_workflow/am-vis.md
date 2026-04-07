# am-vis inspect-dump

## browser1

### States
- Completed
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- Exception
- Failed
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Retrying
- RpcReady
- Start
- Working

-----

## browser2

### States
- Browser3Completed
- Browser3Disposed
- Browser3Disposing
- Browser3ErrHandlerTimeout
- Browser3ErrNetwork
- Browser3ErrOnClient
- Browser3Exception
- Browser3Failed
- Browser3Healthcheck
- Browser3Heartbeat
- Browser3Ready
- Browser3RegisterDisposal
- Browser3Retry
- Browser3Retrying
- Browser3RpcReady
- Browser3Start
- Browser3Work
- Browser3Working
- Browser4Completed
- Browser4Disposed
- Browser4Disposing
- Browser4ErrHandlerTimeout
- Browser4ErrNetwork
- Browser4ErrOnClient
- Browser4Exception
- Browser4Failed
- Browser4Healthcheck
- Browser4Heartbeat
- Browser4Ready
- Browser4RegisterDisposal
- Browser4Retry
- Browser4Retrying
- Browser4RpcReady
- Browser4Start
- Browser4Work
- Browser4Working
- Completed
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- Exception
- Failed
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Retrying
- RpcReady
- ServerPayload
- Start
- Working

### Pipes

#### rnm-bro-browser3
- [add] Browser3Retry -> Retrying
- [add] Browser3Work -> Working
- [add] Start -> Start
- [remove] Browser3Retry -> Retrying
- [remove] Browser3Work -> Working
- [remove] Start -> Start

#### rnm-bro-browser4
- [add] Browser4Retry -> Retrying
- [add] Browser4Work -> Working
- [add] Start -> Start
- [remove] Browser4Retry -> Retrying
- [remove] Browser4Work -> Working
- [remove] Start -> Start

-----

## browser3

### States
- Completed
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- Exception
- Failed
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Retrying
- RpcReady
- Start
- Working

-----

## browser4

### States
- Completed
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- Exception
- Failed
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Retrying
- RpcReady
- Start
- Working

-----

## orchestrator

### States
- Browser1Completed
- Browser1Conn
- Browser1Delivered
- Browser1Failed
- Browser1Ready
- Browser1Work
- Browser1Working
- Browser2Completed
- Browser2Conn
- Browser2Delivered
- Browser2Failed
- Browser2Ready
- Browser2Work
- Browser2Working
- Browser3Completed
- Browser3Delivered
- Browser3Failed
- Browser3Ready
- Browser3Work
- Browser3Working
- Browser4Completed
- Browser4Delivered
- Browser4Failed
- Browser4Ready
- Browser4Work
- Browser4Working
- BrowserConn
- ErrBrowser1
- ErrBrowser2
- ErrBrowser3
- ErrBrowser4
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- ErrRpc
- Exception
- Healthcheck
- Heartbeat
- NetworkReady
- Ready
- ServerPayload
- Start
- StartWork
- WorkDelivered
- WorkersReady

-----

## rc-bro-browser3
Parent: browser2

### States
- CallRetryFailed
- ConnRetryFailed
- Connected
- Connecting
- Disconnected
- Disconnecting
- ErrConnecting
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RetryingCall
- RetryingConn
- ServerDelivering
- ServerPayload
- Start

### RPC
- rs-browser3

-----

## rc-bro-browser4
Parent: browser2

### States
- CallRetryFailed
- ConnRetryFailed
- Connected
- Connecting
- Disconnected
- Disconnecting
- ErrConnecting
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RetryingCall
- RetryingConn
- ServerDelivering
- ServerPayload
- Start

### RPC
- rs-browser4

-----

## rc-srv-browser1
Parent: orchestrator

### States
- CallRetryFailed
- ConnRetryFailed
- Connected
- Connecting
- Disconnected
- Disconnecting
- ErrConnecting
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RetryingCall
- RetryingConn
- ServerDelivering
- ServerPayload
- Start

### RPC
- rs-browser1

### Pipes

#### orchestrator
- [add] Exception -> ErrRpc
- [add] Ready -> Browser1Conn
- [remove] Exception -> ErrRpc
- [remove] Ready -> Browser1Conn

-----

## rc-srv-browser2
Parent: orchestrator

### States
- CallRetryFailed
- ConnRetryFailed
- Connected
- Connecting
- Disconnected
- Disconnecting
- ErrConnecting
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RetryingCall
- RetryingConn
- ServerDelivering
- ServerPayload
- Start

### RPC
- rs-browser2

### Pipes

#### orchestrator
- [add] Exception -> ErrRpc
- [add] Ready -> Browser2Conn
- [remove] Exception -> ErrRpc
- [remove] Ready -> Browser2Conn

-----

## relay-wasm-workflow
Parent: orchestrator

### States
- ClientMsg
- ConnectEvent
- DisconnectEvent
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- Exception
- Healthcheck
- Heartbeat
- HttpReady
- HttpStarting
- InitClient
- Ready
- RegisterDisposal
- Start
- WsDialConn
- WsDialDisconn
- WsTunListenConn
- WsTunListenDisconn

-----

## relay-wasm-workflow-wt-0
Parent: relay-wasm-workflow

### States
- Disposed
- Disposing
- ErrClient
- ErrHandlerTimeout
- ErrNetwork
- ErrServer
- Exception
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Start
- TcpAccepted
- TcpListen
- TcpListening
- WebSocket

-----

## relay-wasm-workflow-wt-1
Parent: relay-wasm-workflow

### States
- Disposed
- Disposing
- ErrClient
- ErrHandlerTimeout
- ErrNetwork
- ErrServer
- Exception
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Start
- TcpAccepted
- TcpListen
- TcpListening
- WebSocket

-----

## relay-wasm-workflow-wt-2
Parent: relay-wasm-workflow

### States
- Disposed
- Disposing
- ErrClient
- ErrHandlerTimeout
- ErrNetwork
- ErrServer
- Exception
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Start
- TcpAccepted
- TcpListen
- TcpListening
- WebSocket

-----

## relay-wasm-workflow-wt-3
Parent: relay-wasm-workflow

### States
- Disposed
- Disposing
- ErrClient
- ErrHandlerTimeout
- ErrNetwork
- ErrServer
- Exception
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Start
- TcpAccepted
- TcpListen
- TcpListening
- WebSocket

-----

## rm-repl-orchestrator
Parent: orchestrator

### States
- ClientConnected
- ErrHandlerTimeout
- ErrNetwork
- Exception
- HasClients
- Healthcheck
- Heartbeat
- NewServerErr
- Ready
- Start

-----

## rnm-bro-browser3
Parent: rc-bro-browser3

### States
- Completed
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- Exception
- Failed
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Retrying
- RpcReady
- Start
- Working

### Pipes

#### browser2
- [add] Completed -> Browser3Completed
- [add] Disposed -> Browser3Disposed
- [add] Disposing -> Browser3Disposing
- [add] ErrHandlerTimeout -> Browser3ErrHandlerTimeout
- [add] ErrNetwork -> Browser3ErrNetwork
- [add] ErrOnClient -> Browser3ErrOnClient
- [add] Exception -> Browser3Exception
- [add] Failed -> Browser3Failed
- [add] Healthcheck -> Browser3Healthcheck
- [add] Heartbeat -> Browser3Heartbeat
- [add] Ready -> Browser3Ready
- [add] RegisterDisposal -> Browser3RegisterDisposal
- [add] Retrying -> Browser3Retrying
- [add] RpcReady -> Browser3RpcReady
- [add] Start -> Browser3Start
- [add] Working -> Browser3Working
- [remove] Completed -> Browser3Completed
- [remove] Disposed -> Browser3Disposed
- [remove] Disposing -> Browser3Disposing
- [remove] ErrHandlerTimeout -> Browser3ErrHandlerTimeout
- [remove] ErrNetwork -> Browser3ErrNetwork
- [remove] ErrOnClient -> Browser3ErrOnClient
- [remove] Exception -> Browser3Exception
- [remove] Failed -> Browser3Failed
- [remove] Healthcheck -> Browser3Healthcheck
- [remove] Heartbeat -> Browser3Heartbeat
- [remove] Ready -> Browser3Ready
- [remove] RegisterDisposal -> Browser3RegisterDisposal
- [remove] Retrying -> Browser3Retrying
- [remove] RpcReady -> Browser3RpcReady
- [remove] Start -> Browser3Start
- [remove] Working -> Browser3Working

-----

## rnm-bro-browser4
Parent: rc-bro-browser4

### States
- Completed
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- Exception
- Failed
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Retrying
- RpcReady
- Start
- Working

### Pipes

#### browser2
- [add] Completed -> Browser4Completed
- [add] Disposed -> Browser4Disposed
- [add] Disposing -> Browser4Disposing
- [add] ErrHandlerTimeout -> Browser4ErrHandlerTimeout
- [add] ErrNetwork -> Browser4ErrNetwork
- [add] ErrOnClient -> Browser4ErrOnClient
- [add] Exception -> Browser4Exception
- [add] Failed -> Browser4Failed
- [add] Healthcheck -> Browser4Healthcheck
- [add] Heartbeat -> Browser4Heartbeat
- [add] Ready -> Browser4Ready
- [add] RegisterDisposal -> Browser4RegisterDisposal
- [add] Retrying -> Browser4Retrying
- [add] RpcReady -> Browser4RpcReady
- [add] Start -> Browser4Start
- [add] Working -> Browser4Working
- [remove] Completed -> Browser4Completed
- [remove] Disposed -> Browser4Disposed
- [remove] Disposing -> Browser4Disposing
- [remove] ErrHandlerTimeout -> Browser4ErrHandlerTimeout
- [remove] ErrNetwork -> Browser4ErrNetwork
- [remove] ErrOnClient -> Browser4ErrOnClient
- [remove] Exception -> Browser4Exception
- [remove] Failed -> Browser4Failed
- [remove] Healthcheck -> Browser4Healthcheck
- [remove] Heartbeat -> Browser4Heartbeat
- [remove] Ready -> Browser4Ready
- [remove] RegisterDisposal -> Browser4RegisterDisposal
- [remove] Retrying -> Browser4Retrying
- [remove] RpcReady -> Browser4RpcReady
- [remove] Start -> Browser4Start
- [remove] Working -> Browser4Working

-----

## rnm-srv-browser1
Parent: rc-srv-browser1

### States
- Completed
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- Exception
- Failed
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Retrying
- RpcReady
- Start
- Working

### Pipes

#### orchestrator
- [add] Completed -> Browser1Completed
- [add] Exception -> ErrBrowser1
- [add] Failed -> Browser1Failed
- [add] Ready -> Browser1Ready
- [add] Working -> Browser1Working
- [remove] Completed -> Browser1Completed
- [remove] Exception -> ErrBrowser1
- [remove] Failed -> Browser1Failed
- [remove] Ready -> Browser1Ready
- [remove] Working -> Browser1Working

-----

## rnm-srv-browser2
Parent: rc-srv-browser2

### States
- Browser3Completed
- Browser3Disposed
- Browser3Disposing
- Browser3ErrHandlerTimeout
- Browser3ErrNetwork
- Browser3ErrOnClient
- Browser3Exception
- Browser3Failed
- Browser3Healthcheck
- Browser3Heartbeat
- Browser3Ready
- Browser3RegisterDisposal
- Browser3Retry
- Browser3Retrying
- Browser3RpcReady
- Browser3Start
- Browser3Work
- Browser3Working
- Browser4Completed
- Browser4Disposed
- Browser4Disposing
- Browser4ErrHandlerTimeout
- Browser4ErrNetwork
- Browser4ErrOnClient
- Browser4Exception
- Browser4Failed
- Browser4Healthcheck
- Browser4Heartbeat
- Browser4Ready
- Browser4RegisterDisposal
- Browser4Retry
- Browser4Retrying
- Browser4RpcReady
- Browser4Start
- Browser4Work
- Browser4Working
- Completed
- Disposed
- Disposing
- ErrHandlerTimeout
- ErrNetwork
- ErrOnClient
- Exception
- Failed
- Healthcheck
- Heartbeat
- Ready
- RegisterDisposal
- Retrying
- RpcReady
- ServerPayload
- Start
- Working

### Pipes

#### orchestrator
- [add] Browser3Completed -> Browser3Completed
- [add] Browser3Exception -> ErrBrowser3
- [add] Browser3Failed -> Browser3Failed
- [add] Browser3Ready -> Browser3Ready
- [add] Browser3Working -> Browser3Working
- [add] Browser4Completed -> Browser4Completed
- [add] Browser4Exception -> ErrBrowser4
- [add] Browser4Failed -> Browser4Failed
- [add] Browser4Ready -> Browser4Ready
- [add] Browser4Working -> Browser4Working
- [add] Completed -> Browser2Completed
- [add] Exception -> ErrBrowser2
- [add] Failed -> Browser2Failed
- [add] Ready -> Browser2Ready
- [add] Working -> Browser2Working
- [remove] Browser3Completed -> Browser3Completed
- [remove] Browser3Exception -> ErrBrowser3
- [remove] Browser3Failed -> Browser3Failed
- [remove] Browser3Ready -> Browser3Ready
- [remove] Browser3Working -> Browser3Working
- [remove] Browser4Completed -> Browser4Completed
- [remove] Browser4Exception -> ErrBrowser4
- [remove] Browser4Failed -> Browser4Failed
- [remove] Browser4Ready -> Browser4Ready
- [remove] Browser4Working -> Browser4Working
- [remove] Completed -> Browser2Completed
- [remove] Exception -> ErrBrowser2
- [remove] Failed -> Browser2Failed
- [remove] Ready -> Browser2Ready
- [remove] Working -> Browser2Working

-----

## rs-browser1
Parent: browser1

### States
- ClientConnected
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RpcAccepting
- RpcReady
- RpcStarting
- Start
- WebSocketTunnel

### Pipes

#### browser1
- [add] Ready -> RpcReady
- [remove] Ready -> RpcReady

-----

## rs-browser2
Parent: browser2

### States
- ClientConnected
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RpcAccepting
- RpcReady
- RpcStarting
- Start
- WebSocketTunnel

### Pipes

#### browser2
- [add] Ready -> RpcReady
- [remove] Ready -> RpcReady

-----

## rs-browser3
Parent: browser3

### States
- ClientConnected
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RpcAccepting
- RpcReady
- RpcStarting
- Start
- WebSocketTunnel

### Pipes

#### browser3
- [add] Ready -> RpcReady
- [remove] Ready -> RpcReady

-----

## rs-browser4
Parent: browser4

### States
- ClientConnected
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RpcAccepting
- RpcReady
- RpcStarting
- Start
- WebSocketTunnel

### Pipes

#### browser4
- [add] Ready -> RpcReady
- [remove] Ready -> RpcReady

-----

## rs-repl-browser1
Parent: browser1

### States
- ClientConnected
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RpcAccepting
- RpcReady
- RpcStarting
- Start
- WebSocketTunnel

-----

## rs-repl-browser2
Parent: browser2

### States
- ClientConnected
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RpcAccepting
- RpcReady
- RpcStarting
- Start
- WebSocketTunnel

-----

## rs-repl-browser3
Parent: browser3

### States
- ClientConnected
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RpcAccepting
- RpcReady
- RpcStarting
- Start
- WebSocketTunnel

-----

## rs-repl-browser4
Parent: browser4

### States
- ClientConnected
- ErrDelivery
- ErrHandlerTimeout
- ErrNetwork
- ErrNetworkTimeout
- ErrOnClient
- ErrRpc
- Exception
- HandshakeDone
- Handshaking
- Healthcheck
- Heartbeat
- MetricSync
- Ready
- RpcAccepting
- RpcReady
- RpcStarting
- Start
- WebSocketTunnel

-----

