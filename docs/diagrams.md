# Diagrams

## Machine Basics

Features are explained using Mermaid flow diagrams, and headers link to relevant sections of the [manual](/docs/manual.md).

### [Multi-state](/docs/manual.md#introduction)

Many states can be active at the same time.

```mermaid
flowchart LR
    subgraph ActiveBefore[Before]
        A1([A:1])
        B1[B:0]
        C1[C:0]
    end
    ActiveMutation([add B])
    subgraph ActiveAfter[After]
        A2([A:1])
        B2([B:1])
        C2[C:0]
    end
    ActiveBefore --> ActiveMutation
    ActiveMutation --> ActiveAfter
```

### [Clock and state contexts](/docs/manual.md#clock-and-context)

States have clocks that produce contexts (odd = active; even = inactive).

```mermaid
flowchart LR
    subgraph ClockStep1[ ]
            A1([A:1])
            B1([B:1])
            C1[C:0]
        end
        ClockMutation1[remove B]
        subgraph ClockStep2[ ]
            A2([A:1])
            B2[B:2]
            C2[C:0]
        end
        ClockMutation2([add B])
        subgraph ClockStep3[ ]
          A3([A:1])
          B3([B:3])
          C3[C:0]
        end
        subgraph ClockCtxs[State contexts of B]
          CtxB1([B:1])
          CtxB3([B:3])
        end
    ClockStep1 --> ClockMutation1 --> ClockStep2
    ClockStep2 --> ClockMutation2 --> ClockStep3
%%    B1 --> CtxB1
%%    B2 --> CtxB2
%%    B3 --> CtxB3
```

### [Queue](/docs/manual.md#queue-and-history)

Queue of mutations enable lock-free [Actor Model](https://en.wikipedia.org/wiki/Actor_model).

```mermaid
flowchart LR
    Machine[[Machine]]
    Add1([add A])
    Add2([add A])
    Add3([add B])
    Add1 -- 1 --> Machine
    Add2 -- 2 --> Machine
    Add3 -- 3 --> Machine
    subgraph Queue
        direction LR
        QAdd1([add A])
        QAdd2([add A])
        QAdd3([add B])
        QAdd1 --> QAdd2 --> QAdd3
    end
    Machine --> Queue
```

### [AOP handlers](/docs/manual.md#transition-handlers)

States are [Aspects](https://en.wikipedia.org/wiki/Aspect-oriented_programming) with Enter, State, Exit, and End handlers.

```mermaid
flowchart LR
    subgraph HandlersBefore[ ]
        A1([A:1])
        B1[B:0]
        C1[C:0]
    end
    subgraph HandlersAfter[ ]
        A2([A:1])
        B2([B:1])
        C2[C:0]
    end
    HandlersMutation([add B])
    HandlersBefore --> HandlersMutation
    HandlersMutation --> HandlersAfter
    HandlersMutation --> AnyB
    HandlersMutation --> BEnter
    HandlersMutation --> AB
    HandlersMutation --> BState
    subgraph Handlers
        AnyB[["AnyB()"]]
        BEnter[["BEnter()"]]
        AB[["AB()"]]
        BState[["BState()"]]
    end
```

### [Negotiation](/docs/manual.md#transition-lifecycle)

Transitions are cancellable (during the negotiation phase).

```mermaid
flowchart LR
    subgraph NegotiationBefore[ ]
        A1([A:1])
        B1[B:0]
        C1[C:0]
    end
    subgraph NegotiationAfter[ ]
        A2([A:1])
        B2[B:0]
        C2[C:0]
    end
    NegotiationMutation([add B])
    NegotiationBefore --> NegotiationMutation
    NegotiationMutation --> NegotiationAfter
    NegotiationMutation --> AnyB
    NegotiationMutation --> BEnter
    BEnter == return ==> false
    NegotiationMutation  -. canceled .-x  AB
    NegotiationMutation -. canceled .-x BState
    subgraph Negotiation
        false[[false]]
        AnyB[["AnyB()"]]
        BEnter[["BEnter()"]]
        AB[["AB()"]]
        BState[["BState()"]]
    end
```

### [Relations](/docs/manual.md#relations)

States are connected via Require, Remove, and Add relations.

```mermaid
flowchart LR
    Wet(Wet)
    Dry(Dry)
    Water(Water)
    Wet -- Require --> Water
    Water -- Add --> Wet
    Water -- Remove --> Dry
    Dry -- Remove --> Water
```

### [Subscriptions](/docs/manual.md#waiting)

Channel-broadcast waiting on clock values.

```mermaid
flowchart LR
    subgraph SubStates[ ]
      direction LR
        subgraph SubStep1[ ]
      direction BT
            A1([A:1])
            B1([B:1])
            C1[C:0]
        end
        SubMutation1[remove B]
        subgraph SubStep2[ ]
            A2([A:1])
            B2[B:2]
            C2[C:0]
        end
        SubMutation2([add B])
        subgraph SubStep3[ ]
          A3([A:1])
          B3([B:3])
          C3[C:0]
        end
    end
    SubStep1 --> SubMutation1 --> SubStep2
    SubStep2 --> SubMutation2 --> SubStep3
    B2 .-> WhenNotB
    A3 .-> WhenTimeB3
    B3 .-> WhenTimeB3
    subgraph Subs[ ]
        WhenNotB>"WhenNot B"]
        WhenTimeB3>"WhenTime A:1 B:3"]
    end
```

## aRPC

### aRPC Client Server

```mermaid
flowchart TB

    subgraph s [Server Host]
        direction TB
        Server[aRPC Server]
        Worker[Worker Mach]
    end
    subgraph c [Client Host]
        direction TB
        Consumer[Consumer]
        Client[aRPC Client]
        RemoteWorker[Remote Worker Mach]
    end

    Client -- WorkerPayload state --> Consumer
    Client --> RemoteWorker
    Worker --> Server
    Client -- Mutations --> Server
    Server -. Clock Updates .-> Client
```

### aRPC Architecture

```mermaid
flowchart TB

    subgraph s [Server Host]
        direction TB
        Server[aRPC Server]
        Worker[Worker Mach]
    end
    subgraph c [Client Host]
        direction TB
        Consumer[Consumer]
        Client[aRPC Client]
        RemoteWorker[Remote Worker Mach]
    end

    Client -- WorkerPayload state --> Consumer
    Client --> RemoteWorker
    Worker --> Server
    Client -- Mutations --> Server
    Server -. Clock Updates .-> Client
```

## Worker Pool Architecture

```mermaid
flowchart LR
    c1-RemoteWorker -- aRPC --> w1-rpcPub
    c1-RemoteSupervisor -- aRPC --> s1-rpcPub

    subgraph Client 1
        c1-Client[Client]
        c1-RemoteWorker[RemoteWorker]
        c1-RemoteSupervisor[RemoteSupervisor]
        c1-Client --> c1-RemoteWorker
        c1-Client --> c1-RemoteSupervisor
    end

    subgraph Client 2
        c2-Client[Client]
        c2-RemoteWorker[RemoteWorker]
        c2-RemoteSupervisor[RemoteSupervisor]
        c2-Client --> c2-RemoteWorker
        c2-Client --> c2-RemoteSupervisor
    end

    subgraph Node Host

        subgraph Worker Pool
            w1-rpcPub --> Worker1
            w1-rpcPub([Public aRPC])
            w1-rpcPriv --> Worker1
            w1-rpcPriv([Private aRPC])
            w2-rpcPub --> Worker2
            w2-rpcPub([Public aRPC])
            w2-rpcPriv --> Worker2
            w2-rpcPriv([Private aRPC])
            w3-rpcPub --> Worker3
            w3-rpcPub([Public aRPC])
            w3-rpcPriv --> Worker3
            w3-rpcPriv([Private aRPC])
        end

        s1-rpcPub([Public aRPC])
        s1-rpcPub --> Supervisor1
        Supervisor1 --> RemoteWorker1
        Supervisor1[Supervisor]
        RemoteWorker1 -- aRPC --> w1-rpcPriv
        Supervisor1 -- fork --> Worker1
        Supervisor1 --> RemoteWorker2
        RemoteWorker2 -- aRPC --> w2-rpcPriv
        Supervisor1 -- fork --> Worker2
        Supervisor1 --> RemoteWorker3
        RemoteWorker3 -- aRPC --> w3-rpcPriv
        Supervisor1 -- fork --> Worker3
    end

    c2-RemoteWorker -- aRPC --> w2-rpcPub
    c2-RemoteSupervisor -- aRPC --> s1-rpcPub
```

## Flows

### Legend

```mermaid
flowchart
        leg-State([State])
        leg-HandlerOrMethod["HandlerOrMethod()"]
        subscription[["subscription to A"]]

        subgraph LegendAdd [State A adds state B]
            leg-A1([A]) -- Add --> leg-B1([B])
        end

        subgraph LegendCall [Method call]
            leg-HandlerOrMethod21["HandlerOrMethod1()"] -. call .-> leg-MethodName22["HandlerOrMethod2()"]
        end

        subgraph LegendHandle [State handler]
            leg-A3([A]) == handle ==> leg-HandlerOrMethod3["AState()"]
        end
```

### RPC Getter Flow

Consumer requests payload from a remote worker.

```mermaid
flowchart TB

    subgraph ClientHost [Client Host]
        subgraph Consumer
            con-MethodOrHandler["MethodOrHandler()"]
            con-WorkerPayload(["WorkerPayload"])
        end

        subgraph RemoteWorkerMach [Remote Worker Mach]
        %% Provide Worker is requested to provide some (named) data.
            rw-Provide([ProvideABC])
        end

        subgraph RPCClientMach [RPC Client Mach]
            c-WorkerPayload([WorkerPayload])
            c-RemoteSendPayload["RemoteSendPayload()"]
            c-WorkerPayloadState["WorkerPayloadState()"]
        end
    end

    subgraph ServerHost [Server Host]
        subgraph RPCServer [RPC Server]
            srv-SendPayloadState["SendPayloadState()"]
        end

        subgraph SourceWorkerMach [Source Worker Mach]
        %% Provide Worker is requested to provide some (named) data.
            sw-Provide([ProvideABC])
            sw-ProvideState["ProvideABCState()"]
            sw-SendPayload(["SendPayload"])
        end
    end

    %% steps
    con-MethodOrHandler -- Add --> rw-Provide
    rw-Provide -- Add --> sw-Provide
    sw-Provide == handle ==> sw-ProvideState
    sw-ProvideState -- Add --> sw-SendPayload
    sw-SendPayload == handle ==> srv-SendPayloadState
    srv-SendPayloadState -. call .-> c-RemoteSendPayload
    c-RemoteSendPayload -- Add --> c-WorkerPayload
    c-WorkerPayload == handle ==> c-WorkerPayloadState
    c-WorkerPayloadState -- Add --> con-WorkerPayload
```

### Worker Bootstrap

[Node Supervisor](/pkg/node/README.md) forks a new worker process for the pool.

```mermaid
flowchart TB

    subgraph SupervisorMach [Supervisor State Mach]
        ForkWorker([ForkWorker])
        ForkingWorker([ForkingWorker])
        AwaitingWorker([AwaitingWorker])
        WorkerForked([WorkerForked])
        exec["exec.Command()"]
        newBootstrap["newBootstrap()"]
        whenWorkerAddr[["when:WorkerAddr"]]

        ForkWorker == handle ==> ForkWorkerState["ForkWorkerState()"] -. call .->  newBootstrap
        ForkWorker -- Add --> ForkingWorker
        ForkingWorker == handle ==> ForkingWorkerState["ForkingWorkerState()"] -. call .-> exec
        ForkingWorker -- Add --> AwaitingWorker
        AwaitingWorker -- wait --> whenWorkerAddr
        whenWorkerAddr --> WorkerForked
    end
    exec .-> Worker
    newBootstrap .-> BootstrapMach
    subgraph BootstrapMach [Bootstrap State Mach]
        WorkerAddr([WorkerAddr])
        WorkerAddr -- tick --> whenWorkerAddr
    end
    subgraph Worker [Worker State Mach]
        RpcReadyState["RpcReadyState()"]
        RpcReady([RpcReady])  == handle ==> RpcReadyState
    end
    RpcReadyState -- aRPC Add --> WorkerAddr
```

### Transition Lifecycle

```d2
direction: right

Mutation -> Transition.Consensus.Relations -> Transition.Consensus.Negotiation -> Transition.Execution
Transition: {
  Consensus: {
    Relations
    Negotiation
  }
  Execution
}

Transition.Execution -> Mutation: queue
Transition.Execution -> Auto states: state changed
Auto states -> Mutation: queue
```
