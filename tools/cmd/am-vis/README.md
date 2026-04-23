# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo-25.png" /> /tools/cmd/arpc

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a pathless control-flow graph with a consensus (AOP, actor model, state-machine).

## am-vis

`am-vis` renders diagrams from [dbg telemetry](/pkg/telemetry/README.md#dbg) by creating a graph of interconnected
state machines. The graph is based on fragmented data they provide via:

- state schema
  - states
  - relations
  - tags
- `LogOps` (level `3`)
  - pipes
  - RPC connections
- transitions
  - active states

The rendering uses ELK ([Eclipse Layout Kernel](https://eclipse.dev/elk/)) and is done by [D2](https://d2lang.com/)
(embedded). At the moment it can only render from file dumps, with the `live-server` version is yet to come.

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/am-vis/diagram-1.svg">
        <img alt="single machine"
            src="https://pancsta.github.io/assets/asyncmachine-go/am-vis/diagram-1.svg">
    </a>
</div>

## Installation

Available options:

1. [Download a release binary](https://github.com/pancsta/asyncmachine-go/releases/latest)
2. Install with Go

   ```bash
   go install github.com/pancsta/asyncmachine-go/tools/cmd/am-vis@latest
   ```

3. Run directly

   ```bash
   go run github.com/pancsta/asyncmachine-go/tools/cmd/am-vis@latest
   ```

## Features

`$ am-vis render-dump --help`

```bash
Render diagrams of interconnected state machines.

Examples:

# single machine
am-vis render-dump mymach.gob.br mach://MyMach1/t234

# bird's view with distance of 2 from MyMach1
am-vis --bird -d 2 \
        render-dump mymach.gob.br mach://MyMach1/TX-ID

# bird's view with grouped pipes
am-vis --bird --render-pipes --render-detailed-pipes=false \
        render-dump mymach.gob.br mach://MyMach1/TX-ID

# map view with inherited states
am-vis --render-inherited \
        render-dump mymach.gob.br mach://MyMach1/t234

# map view
am-vis --map render-dump mymach.gob.br

# inspect dump as Markdown
am-vis inspect-dump mymach.gob.br

Usage: am-vis [--render-distance RENDER-DISTANCE] [--render-depth RENDER-DEPTH] [--render-start] [--render-ready] [--render-exception] [--render-states] [--render-inherited] [--render-pipes] [--render-detailed-pipes] [--render-relations] [--render-conns] [--render-parent-rel] [--render-half-conns] [--render-half-pipes] [--render-nest-submachines] [--render-tags] [--bird] [--map] [--output-elk] [--output-filename OUTPUT-FILENAME] [--debug] [--version] <command> [<args>]

Options:
  --render-distance RENDER-DISTANCE, -d RENDER-DISTANCE [default: 0]
  --render-depth RENDER-DEPTH [default: 0]
  --render-start [default: false]
  --render-ready [default: false]
  --render-exception [default: false]
  --render-states [default: true]
  --render-inherited [default: true]
  --render-pipes [default: false]
  --render-detailed-pipes [default: true]
  --render-relations [default: true]
  --render-conns [default: true]
  --render-parent-rel [default: true]
  --render-half-conns [default: true]
  --render-half-pipes [default: true]
  --render-nest-submachines [default: false]
  --render-tags [default: false]
  --bird, -b             Use the bird's view preset
  --map, -b              Use the map view preset
  --output-elk           Use ELK layout [default: true]
  --output-filename OUTPUT-FILENAME, -o OUTPUT-FILENAME [default: am-vis]
  --debug                Enable debugging for asyncmachine
  --version, -v          Print version and exit
  --help, -h             display this help and exit

Commands:
  render-dump            Render from a debugger dump file
  inspect-dump           Render text form from a debugger dump file
```

Graphs can be programmatically filtered using the rendering config:

```go
type Renderer struct {
    // Render only these machines as starting points.
    RenderMachs []string
    // Render only machines matching the regular expressions as starting points.
    RenderMachsRe []*regexp.Regexp
    // Skip rendering of these machines.
    RenderSkipMachs []string
    // Distance to render from starting machines.
    RenderDistance int
    // How deep to render from starting machines. Same as RenderDistance, but only
    // for submachines.
    RenderDepth int

    // Render states bubbles.
    RenderStates bool
    // With RenderStates, false will hide Start, and without RenderStates true
    // will render Start.
    RenderStart bool
    // With RenderStates, false will hide Exception, and without RenderStates true
    // will render Exception.
    RenderException bool
    // With RenderStates, false will hide Ready, and without RenderStates true
    // will render Ready.
    RenderReady bool
    // Render states which have pipes being rendered, even if the state should
    // not be rendered.
    RenderPipeStates bool
    // Render group of pipes as mach->mach
    RenderPipes bool
    // Render pipes to non-rendered machines / states.
    RenderHalfPipes bool
    // Render detailed pipes as state -> state
    RenderDetailedPipes bool
    // Render relation between states.
    RenderRelations bool
    // Style currently active states.
    RenderActive bool
    // Render the parent relation. Ignored when RenderNestSubmachines.
    RenderParentRel bool
    // Render submachines nested inside their parents. See also RenderDepth.
    RenderNestSubmachines bool
    // Render a tags box for machines having some.
    RenderTags bool
    // Render RPC connections
    RenderConns bool
    // Render RPC connections to non-rendered machines.
    RenderHalfConns bool
    // Render a parent relation to and from non-rendered machines.
    RenderHalfHierarchy bool
    // Render inherited states.
    RenderInherited bool
    // Mark inherited states.
    RenderMarkInherited bool
}
```

## TODO

- rendering from live connections
- browser viewer with auto-reload
- add a TUI version of the rendering config
- use D2 API instead of gluing strings
- support GC, store machine time for enter / exit
- add [G6 renderer](https://github.com/antvis/G6) for large graphs

## Legend

- yellow border = machine requested by ID
- white border = machine close or deep enough from a requested one
- no border = not fully rendered machine for grouped inbound and outbound edges
- yellow state = active
- double border state = own / non-inherited state
- blue state = Ready active
- green state = Start active

## Gallery

It's recommended to view the SVGs using [SVG Navigator](https://github.com/pRizz/SVG-Navigator---Chrome-Extension).

![diagram1](https://pancsta.github.io/assets/asyncmachine-go/am-vis/diagram-1.svg)
![diagram2](https://pancsta.github.io/assets/asyncmachine-go/am-vis/diagram-2.svg)
![diagram3](https://pancsta.github.io/assets/asyncmachine-go/am-vis/diagram-3.svg)
![diagram4](https://pancsta.github.io/assets/asyncmachine-go/am-vis/diagram-4.svg)
![diagram5](https://pancsta.github.io/assets/asyncmachine-go/am-vis/diagram-5.svg)

## Transition Sequence

Transitions can be plotted as sequence diagrams and rendered to SVG and ASCII. Currently [`am-dbg`](/tools/cmd/am-dbg/README.md)
does it automatically with `--output-tx` and places files inside `--dir`. Support in `am-vis` CLI coming soon.

This is the same steps diagram as the second timeline of `am-dbg` and includes:

- all called and touched states
- called steps
- relation steps
- handler calls
- cancellations

![large transition](https://pancsta.github.io/assets/asyncmachine-go/am-vis/tx.svg)

```text
┌────────────────┐     ┌───────┐     ┌───────┐
│ ReplaceStories │     │ Start │     │ Ready │
└────────┬───────┘     └───┬───┘     └───┬───┘
         │                 │             │
         │ called          │             │
         ├──┐              │             │
         │  │              │             │
         │◄─┘              │             │
         │                 │             │
         │ activate        │             │
         ├──┐              │             │
         │  │              │             │
         │◄─┘              │             │
         │                 │             │
         │                 │ require     │
         │                 │◄────────────┤
         │                 │             │
         │                 │ require     │
         │                 │◄────────────┤
         │                 │             │
         │ ReplaceStoriesState           │
         ├──┐              │             │
         │  │              │             │
         │◄─┘              │             │
         │                 │             │
```

## Graphs as Markdown

The `am-vis inspect-dump` command outputs a Markdown form of the graph, for easier processing.

<details>

<summary>See machine schema and relations</summary>

```markdown
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
```

</details>

## Credits

- [D2](https://d2lang.com/)
- [mermaid-ascii](https://github.com/AlexanderGrooff/mermaid-ascii)

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
