# <img src="https://pancsta.github.io/assets/asyncmachine-go/logo.png" height="25"/> /tools/cmd/arpc

[`cd /`](/README.md)

> [!NOTE]
> **asyncmachine-go** is a batteries-included graph control flow library (AOP, actor model, state-machine).

## am-vis

`am-vis` renders diagrams from [dbg telemetry](/pkg/telemetry/README.md#dbg). It creates a graph of interconnected
machines, based on fragmented data they provide via:
- state schema
  - states
  - relations
  - tags
- logOps (level 3)
  - pipes
  - RPC connections
- transitions
  - active states

The rendering uses [Eclipse Layout Kernel](https://eclipse.dev/elk/) (ELK) and is done by [D2](https://d2lang.com/)
(shipped). At the moment it can only render form file dump, with the live-server version yet to come.

<div align="center">
    <a href="https://pancsta.github.io/assets/asyncmachine-go/am-vis/diagram-1.svg">
        <img style="min-height: 264px" alt="single machine"
            src="https://pancsta.github.io/assets/asyncmachine-go/am-vis/diagram-1.svg">
    </a>
</div>

## Installation

- [Download a release binary](https://github.com/pancsta/asyncmachine-go/releases/latest)
- Install `go install github.com/pancsta/asyncmachine-go/tools/cmd/am-vis@latest`
- Run directly `go run github.com/pancsta/asyncmachine-go/tools/cmd/am-vis@latest`

## Features

Graphs can be filtered using the rendering config:

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

`$ am-vis render-dump --help`

```bash
Render diagrams of interconnected state machines.

Examples:

# single machine
am-vis render-dump mymach.gob.br mach://MyMach1/t234

# bird's view with distance of 2 from MyMach1
am-vis --bird -d 2 \
        render-dump mymach.gob.br mach://MyMach1/TX-ID

# bird's view with detailed pipes
am-vis --bird --render-detailed-pipes \
        render-dump mymach.gob.br mach://MyMach1/TX-ID

# map view with inherited states
am-vis --render-inherited \
        render-dump mymach.gob.br mach://MyMach1/t234

# map view
am-vis --map render-dump mymach.gob.br

Usage: am-vis render-dump [--dump-file DUMP-FILE] [MACHURL]

Positional arguments:
  MACHURL

Options:
  --dump-file DUMP-FILE, -f DUMP-FILE
                         Input dbg dump file [default: am-dbg-dump.gob.br]

Global options:
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
  --help, -h             display this help and exit
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

## Credits

- [D2](https://d2lang.com/)

## monorepo

[Go back to the monorepo root](/README.md) to continue reading.
